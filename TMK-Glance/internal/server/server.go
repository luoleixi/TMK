package server

import (
	"encoding/json"
	"log"
	"net/http"
	"time"

	"tmk-glance/internal/language"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
)

var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool { return true },
}

func Start(addr string) error {
	r := gin.Default()

	// CORS
	r.Use(corsMiddleware())

	// health
	r.GET("/api/health", handleHealth)

	v1 := r.Group("/api/v1")
	{
		v1.GET("/languages", handleLanguages)
		v1.GET("/audio/devices", handleAudioDevices)
		v1.POST("/sessions", handleCreateSession)
		v1.GET("/sessions/:id", handleGetSession)
		v1.DELETE("/sessions/:id", handleDeleteSession)
		v1.GET("/sessions/:id/records", handleGetRecords)
		v1.GET("/history", handleListHistory)
		v1.GET("/history/:id", handleGetHistory)
		v1.GET("/interpret", handleInterpret)
	}

	return r.Run(addr)
}

// ---------- health ----------

func handleHealth(c *gin.Context) {
	c.JSON(200, gin.H{
		"status": "ok", "timestamp": time.Now().Unix(), "version": "1.0.0",
		"services": gin.H{"asr": true, "translator": true, "tts": true},
	})
}

// ---------- languages ----------

func handleLanguages(c *gin.Context) {
	c.JSON(200, gin.H{"languages": language.All})
}

// ---------- audio devices ----------

func handleAudioDevices(c *gin.Context) {
	c.JSON(200, gin.H{
		"inputs": []gin.H{
			{"id": "mic_0", "name": "内置麦克风", "type": "microphone", "is_default": true},
			{"id": "sys_0", "name": "系统音频 (Stereo Mix)", "type": "system_audio", "is_default": false},
		},
		"outputs": []gin.H{
			{"id": "spk_0", "name": "内置扬声器", "type": "speaker", "is_default": true},
		},
	})
}

// ---------- sessions ----------

func handleCreateSession(c *gin.Context) {
	c.JSON(201, gin.H{"message": "stub"})
}

func handleGetSession(c *gin.Context) {
	c.JSON(200, gin.H{"message": "stub"})
}

func handleDeleteSession(c *gin.Context) {
	c.JSON(200, gin.H{"message": "stub"})
}

func handleGetRecords(c *gin.Context) {
	c.JSON(200, gin.H{"message": "stub"})
}

// ---------- history ----------

func handleListHistory(c *gin.Context) {
	c.JSON(200, gin.H{"message": "stub"})
}

func handleGetHistory(c *gin.Context) {
	c.JSON(200, gin.H{"message": "stub"})
}

// ---------- WebSocket ----------

func handleInterpret(c *gin.Context) {
	conn, err := upgrader.Upgrade(c.Writer, c.Request, nil)
	if err != nil {
		log.Printf("[ws] upgrade failed: %v", err)
		return
	}
	defer conn.Close()
	log.Printf("[ws] client connected")

	for {
		_, msg, err := conn.ReadMessage()
		if err != nil {
			break
		}
		var wsMsg struct{ Type string }
		json.Unmarshal(msg, &wsMsg)

		switch wsMsg.Type {
		case "start":
			conn.WriteJSON(gin.H{"type": "started", "timestamp_ms": time.Now().UnixMilli()})
		case "ping":
			conn.WriteJSON(gin.H{"type": "pong", "timestamp_ms": time.Now().UnixMilli()})
		case "stop":
			conn.WriteJSON(gin.H{"type": "stopped", "timestamp_ms": time.Now().UnixMilli()})
			return
		}
	}
}

// ---------- middleware ----------

func corsMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Header("Access-Control-Allow-Origin", "*")
		c.Header("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
		c.Header("Access-Control-Allow-Headers", "Content-Type, Authorization")
		if c.Request.Method == http.MethodOptions {
			c.AbortWithStatus(200)
			return
		}
		c.Next()
	}
}
