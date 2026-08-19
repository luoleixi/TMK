package server

import (
	"fmt"
	"log"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"tmk-glance/internal/asr"
	"tmk-glance/internal/config"
	"tmk-glance/internal/evaluation"
	"tmk-glance/internal/events"
	"tmk-glance/internal/health"
	"tmk-glance/internal/model"
	"tmk-glance/internal/objectstore"
	"tmk-glance/internal/observability"
	"tmk-glance/internal/store"
	"tmk-glance/internal/translator"

	"github.com/gin-gonic/gin"
)

type Application struct {
	cfg             *config.Config
	store           *store.SessionStore
	translator      translator.Translator
	briefJobs       sync.Map
	loginLimiter    *loginLimiter
	objectStore     *objectstore.Local
	evaluations     *evaluation.Manager
	uploadMu        sync.Mutex
	asrFactory      func(string) asr.ASR
	metrics         *observability.Metrics
	samplerStop     chan struct{}
	samplerDone     chan struct{}
	databaseHealthy atomic.Bool
	storageHealthy  atomic.Bool
	eventBus        *events.Bus
}

func SetupRouter(cfg *config.Config) *gin.Engine {
	app, err := NewApplication(cfg)
	if err != nil {
		log.Fatalf("[startup] init failed: %v", err)
	}
	return app.Router()
}

func NewApplication(cfg *config.Config) (*Application, error) {
	health.SetReady(false)
	sessionStore, err := store.NewSessionStore(cfg.Storage.Driver, cfg.Storage.DBPath, cfg.Storage.DSN)
	if err != nil {
		return nil, err
	}
	app := &Application{
		cfg: cfg, store: sessionStore, translator: newTranslator(cfg),
		loginLimiter: newLoginLimiter(),
		asrFactory:   func(language string) asr.ASR { return newASR(cfg, language) },
		metrics:      observability.NewMetrics(), samplerStop: make(chan struct{}), eventBus: events.NewBus(),
	}
	if err := app.bootstrapAdmin(); err != nil {
		_ = sessionStore.Close()
		return nil, err
	}
	if cfg.ObjectStorage.Driver != "" && cfg.ObjectStorage.Driver != "local" {
		_ = sessionStore.Close()
		return nil, fmt.Errorf("unsupported object storage driver %q", cfg.ObjectStorage.Driver)
	}
	objectRoot := strings.TrimSpace(cfg.ObjectStorage.Root)
	if objectRoot == "" && cfg.Storage.Driver == store.DriverSQLite && cfg.Storage.DBPath != "" {
		objectRoot = filepath.Join(filepath.Dir(cfg.Storage.DBPath), "objects")
	}
	if objectRoot == "" {
		objectRoot = "./data/objects"
	}
	app.objectStore, err = objectstore.NewLocal(objectRoot)
	if err != nil {
		_ = sessionStore.Close()
		return nil, err
	}
	chunkInterval := time.Duration(cfg.Evaluation.ChunkIntervalMS) * time.Millisecond
	if cfg.ASR.Provider == "mock" {
		chunkInterval = 0
	}
	app.evaluations = evaluation.NewManager(app.store, app.objectStore,
		func(language string, snapshot model.EvaluationConfig) asr.ASR {
			return newEvaluationASR(cfg, language, snapshot)
		}, evaluation.Config{
			Workers: cfg.Evaluation.Workers, PollInterval: time.Duration(cfg.Evaluation.PollIntervalMS) * time.Millisecond,
			ItemTimeout: time.Duration(cfg.Evaluation.ItemTimeoutSeconds) * time.Second, ChunkInterval: chunkInterval,
			MaxTextBytes: cfg.ObjectStorage.MaxTextBytes, Metrics: app.metrics,
			LeaseDuration:     time.Duration(cfg.Evaluation.LeaseSeconds) * time.Second,
			HeartbeatInterval: time.Duration(cfg.Evaluation.HeartbeatSeconds) * time.Second,
			RetryBase:         time.Duration(cfg.Evaluation.RetryBaseSeconds) * time.Second,
			ReaperInterval:    time.Duration(cfg.Evaluation.ReaperIntervalSeconds) * time.Second,
		})
	if err := app.evaluations.Start(); err != nil {
		_ = sessionStore.Close()
		return nil, fmt.Errorf("start evaluation workers: %w", err)
	}
	app.startRuntimeSampler()
	app.registerHealthChecks()
	return app, nil
}

func (a *Application) Close() error {
	health.SetReady(false)
	a.metrics.SetReady(false)
	close(a.samplerStop)
	if a.samplerDone != nil {
		<-a.samplerDone
	}
	health.Register("database", nil)
	health.Register("object_storage", nil)
	if a.evaluations != nil {
		a.evaluations.Close()
	}
	return a.store.Close()
}

func (a *Application) Router() *gin.Engine {
	r := gin.New()
	r.MaxMultipartMemory = 8 << 20

	_ = r.SetTrustedProxies([]string{"127.0.0.1", "::1"})
	r.Use(gin.Recovery(), a.observabilityMiddleware(), corsMiddleware(a.cfg.Server.AllowedOrigins))

	r.GET("/metrics", a.metricsHandler)
	r.GET("/api/health", handleHealth)
	r.GET("/api/health/live", handleHealthLive)
	r.GET("/api/health/ready", handleHealthReady)
	internal := r.Group("/internal/v1", requireAdminAPI(serviceAuthConfig{ServiceID: a.cfg.AdminAPI.ServiceID, ServiceSecret: a.cfg.AdminAPI.ServiceSecret}))
	internal.GET("/health", func(c *gin.Context) {
		c.JSON(200, gin.H{"code": "OK", "message": "ok", "data": gin.H{"service": "glance"}})
	})
	internal.POST("/events/evaluation", a.handleInternalEvaluationEvent)
	a.registerAdminRoutes(internal.Group("/admin"))
	v1 := r.Group("/api/v1")
	{
		protected := v1.Group("")
		protected.Use(a.authenticate(), requirePasswordReady())
		protected.GET("/languages", handleLanguages)
		protected.POST("/sessions", a.handleCreateSession)
		protected.GET("/sessions/:id", a.handleGetSession)
		protected.POST("/sessions/:id/stop", a.handleStopSession)
		protected.GET("/history", a.handleListHistory)
		protected.GET("/history/:id", a.handleGetHistory)
		protected.POST("/history/:id/summary", a.handleSummarizeHistory)
		protected.DELETE("/history/:id", a.handleDeleteHistory)
		protected.POST("/history/delete", a.handleDeleteHistoryBatch)
		protected.POST("/translate", a.handleTranslate)
		protected.GET("/interpret", a.handleInterpret)

	}

	return r
}
