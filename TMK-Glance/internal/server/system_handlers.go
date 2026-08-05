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

func handleAudioDevices(c *gin.Context) {
	c.JSON(200, gin.H{
		"inputs": []gin.H{
			{"id": "mic_0", "name": "内置麦克风", "type": "microphone", "is_default": true},
			{"id": "sys_0", "name": "系统音频 (Stereo Mix)", "type": "system_audio", "is_default": false},
		},
		"outputs": []gin.H{{"id": "spk_0", "name": "内置扬声器", "type": "speaker", "is_default": true}},
	})
}
