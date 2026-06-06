package server

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"os"
	"time"

	"tmk-glance/internal/asr"
	"tmk-glance/internal/health"
	"tmk-glance/internal/language"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
)

var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool { return true },
}

func SetupRouter() *gin.Engine {
	r := gin.Default()

	r.Use(corsMiddleware())

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

	return r
}

// ---------- health ----------

func handleHealth(c *gin.Context) {
	c.JSON(200, gin.H{
		"status":    health.Status(),
		"timestamp": time.Now().Unix(),
		"version":   "1.0.0",
		"services":  health.Services(),
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

	var (
		asrEngine asr.ASR
		asrCtx    context.Context
		asrCancel context.CancelFunc = func() {}
		audioCh   chan []byte
	)
	defer func() {
		asrCancel()
		if asrEngine != nil {
			asrEngine.Close()
		}
	}()

	for {
		_, msg, err := conn.ReadMessage()
		if err != nil {
			asrCancel()
			break
		}
		var wsMsg struct {
			Type       string `json:"type"`
			SourceLang string `json:"source_lang"`
			TargetLang string `json:"target_lang"`
		}
		json.Unmarshal(msg, &wsMsg)

		switch wsMsg.Type {
		case "start":
			asrCtx, asrCancel = context.WithCancel(context.Background())
			asrEngine = newASR()
			audioCh = make(chan []byte, 8)

			resultCh, err := asrEngine.Recognize(asrCtx, audioCh)
			if err != nil {
				conn.WriteJSON(gin.H{"type": "error", "message": err.Error()})
				continue
			}

			conn.WriteJSON(gin.H{"type": "started", "timestamp_ms": time.Now().UnixMilli()})

			go func() {
				for r := range resultCh {
					conn.WriteJSON(gin.H{
						"type":      "transcript",
						"text":      r.Text,
						"is_final":  r.IsFinal,
						"timestamp": time.Now().UnixMilli(),
					})
				}
			}()

		case "audio":
			if audioCh != nil {
				audioCh <- msg
			}

		case "ping":
			conn.WriteJSON(gin.H{"type": "pong", "timestamp_ms": time.Now().UnixMilli()})

		case "stop":
			asrCancel()
			conn.WriteJSON(gin.H{"type": "stopped", "timestamp_ms": time.Now().UnixMilli()})
			return
		}
	}
}

// ---------- ASR factory ----------

func newASR() asr.ASR {
	switch os.Getenv("ASR_PROVIDER") {
	case "bailian":
		key := os.Getenv("DASHSCOPE_API_KEY")
		if key == "" {
			log.Fatal("[asr] DASHSCOPE_API_KEY required when ASR_PROVIDER=bailian")
		}
		log.Println("[asr] using Bailian (DashScope)")
		return asr.NewBailian(key)
	default:
		log.Println("[asr] using Mock")
		return asr.NewMock()
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
