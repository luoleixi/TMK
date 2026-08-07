package server

import (
	"time"

	"tmk-glance/internal/health"
	"tmk-glance/internal/language"

	"github.com/gin-gonic/gin"
)

func handleHealth(c *gin.Context) {
	c.JSON(200, gin.H{
		"status": health.Status(), "timestamp": time.Now().Unix(),
		"version": "1.0.0", "services": health.Services(),
	})
}

func handleLanguages(c *gin.Context) {
	c.JSON(200, gin.H{"languages": language.All})
}
