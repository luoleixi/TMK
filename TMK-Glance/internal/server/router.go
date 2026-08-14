package server

import (
	"fmt"
	"log"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"tmk-glance/internal/asr"
	"tmk-glance/internal/config"
	"tmk-glance/internal/evaluation"
	"tmk-glance/internal/model"
	"tmk-glance/internal/objectstore"
	"tmk-glance/internal/store"
	"tmk-glance/internal/translator"

	"github.com/gin-gonic/gin"
)

type Application struct {
	cfg          *config.Config
	store        *store.SessionStore
	translator   translator.Translator
	briefJobs    sync.Map
	loginLimiter *loginLimiter
	objectStore  *objectstore.Local
	evaluations  *evaluation.Manager
	uploadMu     sync.Mutex
	asrFactory   func(string) asr.ASR
}

func SetupRouter(cfg *config.Config) *gin.Engine {
	app, err := NewApplication(cfg)
	if err != nil {
		log.Fatalf("[startup] init failed: %v", err)
	}
	return app.Router()
}

func NewApplication(cfg *config.Config) (*Application, error) {
	sessionStore, err := store.NewSessionStore(cfg.Storage.Driver, cfg.Storage.DBPath, cfg.Storage.DSN)
	if err != nil {
		return nil, err
	}
	app := &Application{
		cfg: cfg, store: sessionStore, translator: newTranslator(cfg),
		loginLimiter: newLoginLimiter(),
		asrFactory:   func(language string) asr.ASR { return newASR(cfg, language) },
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
			MaxTextBytes: cfg.ObjectStorage.MaxTextBytes,
		})
	if err := app.evaluations.Start(); err != nil {
		_ = sessionStore.Close()
		return nil, fmt.Errorf("start evaluation workers: %w", err)
	}
	return app, nil
}

func (a *Application) Close() error {
	if a.evaluations != nil {
		a.evaluations.Close()
	}
	return a.store.Close()
}

func (a *Application) Router() *gin.Engine {
	r := gin.Default()
	r.MaxMultipartMemory = 8 << 20

	_ = r.SetTrustedProxies([]string{"127.0.0.1", "::1"})
	r.Use(corsMiddleware(a.cfg.Server.AllowedOrigins))

	r.GET("/api/health", handleHealth)

	v1 := r.Group("/api/v1")
	{
		v1.POST("/auth/login", a.handleLogin)
		v1.POST("/auth/refresh", a.handleRefresh)

		auth := v1.Group("/auth", a.authenticate())
		auth.GET("/me", a.handleMe)
		auth.POST("/logout", a.handleLogout)
		auth.POST("/change-password", a.handleChangePassword)

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

		admin := v1.Group("/admin", a.authenticate(), requirePasswordReady(), requireAdmin())
		admin.GET("/users", a.handleListUsers)
		admin.POST("/users", a.handleCreateUser)
		admin.PATCH("/users/:id", a.handleUpdateUser)
		admin.POST("/users/:id/reset-password", a.handleResetUserPassword)
		admin.POST("/legacy-sessions/claim", a.handleClaimLegacySessions)
		admin.GET("/dashboard", a.handleDashboard)
		admin.GET("/governance/report", a.handleGovernanceReport)
		admin.GET("/audit-logs", a.handleListAuditEvents)
		admin.POST("/objects", a.handleUploadObject)
		admin.GET("/objects", a.handleListObjects)
		admin.GET("/objects/usage", a.handleStorageUsage)
		admin.GET("/objects/:id", a.handleGetObject)
		admin.GET("/objects/:id/content", a.handleDownloadObject)
		admin.DELETE("/objects/:id", a.handleDeleteObject)
		admin.POST("/datasets", a.handleCreateDataset)
		admin.GET("/datasets", a.handleListDatasets)
		admin.GET("/datasets/:id", a.handleGetDataset)
		admin.PATCH("/datasets/:id", a.handleUpdateDataset)
		admin.DELETE("/datasets/:id", a.handleDeleteDataset)
		admin.POST("/datasets/:id/items", a.handleAddDatasetItem)
		admin.DELETE("/datasets/:id/items/:item_id", a.handleDeleteDatasetItem)
		admin.POST("/datasets/:id/ready", a.handleMarkDatasetReady)
		admin.POST("/datasets/:id/archive", a.handleArchiveDataset)
		admin.POST("/evaluation-jobs", a.handleCreateEvaluationJob)
		admin.GET("/evaluation-jobs", a.handleListEvaluationJobs)
		admin.GET("/evaluation-jobs/:id", a.handleGetEvaluationJob)
		admin.GET("/evaluation-jobs/:id/results", a.handleListEvaluationResults)
		admin.POST("/evaluation-jobs/:id/cancel", a.handleCancelEvaluationJob)
	}

	return r
}
