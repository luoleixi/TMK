package server

import (
	"log"
	"sync"

	"tmk-glance/internal/asr"
	"tmk-glance/internal/config"
	"tmk-glance/internal/store"
	"tmk-glance/internal/translator"

	"github.com/gin-gonic/gin"
)

type Application struct {
	cfg        *config.Config
	store      *store.SessionStore
	translator translator.Translator
	briefJobs  sync.Map
	asrFactory func(string) asr.ASR
}

func SetupRouter(cfg *config.Config) *gin.Engine {
	app, err := NewApplication(cfg)
	if err != nil {
		log.Fatalf("[db] init failed: %v", err)
	}
	return app.Router()
}

func NewApplication(cfg *config.Config) (*Application, error) {
	sessionStore, err := store.NewSessionStore(cfg.Storage.Driver, cfg.Storage.DBPath, cfg.Storage.DSN)
	if err != nil {
		return nil, err
	}
	return &Application{
		cfg: cfg, store: sessionStore, translator: newTranslator(cfg),
		asrFactory: func(language string) asr.ASR { return newASR(cfg, language) },
	}, nil
}

func (a *Application) Close() error { return a.store.Close() }

func (a *Application) Router() *gin.Engine {
	r := gin.Default()

	r.Use(corsMiddleware())

	r.GET("/api/health", handleHealth)

	v1 := r.Group("/api/v1")
	{
		v1.GET("/languages", handleLanguages)
		v1.POST("/sessions", a.handleCreateSession)
		v1.GET("/sessions/:id", a.handleGetSession)
		v1.POST("/sessions/:id/stop", a.handleStopSession)

		v1.GET("/history", a.handleListHistory)
		v1.GET("/history/:id", a.handleGetHistory)
		v1.POST("/history/:id/summary", a.handleSummarizeHistory)
		v1.DELETE("/history/:id", a.handleDeleteHistory)
		v1.POST("/history/delete", a.handleDeleteHistoryBatch)
		v1.POST("/translate", a.handleTranslate)
		v1.GET("/interpret", a.handleInterpret)
	}

	return r
}
