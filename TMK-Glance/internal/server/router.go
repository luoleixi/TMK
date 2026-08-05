package server

import (
	"log"

	"tmk-glance/internal/config"
	"tmk-glance/internal/store"
	"tmk-glance/internal/translator"

	"github.com/gin-gonic/gin"
)

var sessionStore *store.SessionStore
var translateSvc translator.Translator

func SetupRouter(cfg *config.Config) *gin.Engine {
	asrCfg = cfg
	translateSvc = newTranslator(cfg)

	var err error
	sessionStore, err = store.NewSessionStore(cfg.Storage.Driver, cfg.Storage.DBPath, cfg.Storage.DSN)
	if err != nil {
		log.Fatalf("[db] init failed: %v", err)
	}

	r := gin.Default()

	r.Use(corsMiddleware())

	r.GET("/api/health", handleHealth)

	v1 := r.Group("/api/v1")
	{
		v1.GET("/languages", handleLanguages)
		v1.GET("/audio/devices", handleAudioDevices)
		v1.POST("/sessions", handleCreateSession)
		v1.GET("/sessions/:id", handleGetSession)
		v1.POST("/sessions/:id/stop", handleStopSession)

		v1.GET("/history", handleListHistory)
		v1.GET("/history/:id", handleGetHistory)
		v1.POST("/history/:id/summary", handleSummarizeHistory)
		v1.DELETE("/history/:id", handleDeleteHistory)
		v1.POST("/history/delete", handleDeleteHistoryBatch)
		v1.POST("/translate", handleTranslate)
		v1.GET("/interpret", handleInterpret)
	}

	return r
}
