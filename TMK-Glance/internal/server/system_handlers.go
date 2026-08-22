package server

import (
	"time"

	"tmk-glance/internal/buildinfo"
	"tmk-glance/internal/health"
	"tmk-glance/internal/language"

	"github.com/gin-gonic/gin"
)

func handleHealth(c *gin.Context) {
	ready, status, services, serviceStates := health.Snapshot()
	c.JSON(200, gin.H{
		"status": status, "ready": ready, "timestamp": time.Now().Unix(),
		"version": buildinfo.Version, "commit": buildinfo.Commit, "services": services, "service_states": serviceStates,
	})
}

func handleHealthLive(c *gin.Context) {
	c.JSON(200, gin.H{"status": "alive", "timestamp": time.Now().Unix(), "version": buildinfo.Version, "commit": buildinfo.Commit})
}

func handleHealthReady(c *gin.Context) {
	ready, status, services, serviceStates := health.Snapshot()
	code := 200
	if !ready {
		code = 503
	}
	c.JSON(code, gin.H{
		"status": status, "ready": ready, "timestamp": time.Now().Unix(),
		"version": buildinfo.Version, "commit": buildinfo.Commit, "services": services, "service_states": serviceStates,
	})
}

func handleLanguages(c *gin.Context) {
	c.JSON(200, gin.H{"languages": language.All})
}
