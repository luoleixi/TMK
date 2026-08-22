package server

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"strings"
	"time"
	"tmk-glance/internal/model"

	"github.com/gin-gonic/gin"
)

type serviceAuthConfig struct{ ServiceID, ServiceSecret string }

func requireAdminAPI(cfg serviceAuthConfig) gin.HandlerFunc {
	return func(c *gin.Context) {
		if cfg.ServiceSecret == "" {
			c.AbortWithStatusJSON(http.StatusServiceUnavailable, gin.H{"code": "SERVICE_AUTH_NOT_CONFIGURED", "message": "admin api service authentication is not configured"})
			return
		}
		timestamp := c.GetHeader("X-Service-Timestamp")
		provided := c.GetHeader("X-Service-Signature")
		parsed, err := time.Parse(time.RFC3339, timestamp)
		if err != nil || time.Since(parsed).Abs() > 2*time.Minute || provided == "" {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"code": "SERVICE_UNAUTHORIZED", "message": "invalid service credentials"})
			return
		}
		mac := hmac.New(sha256.New, []byte(cfg.ServiceSecret))
		_, _ = mac.Write([]byte(cfg.ServiceID + "\n" + timestamp + "\n" + c.Request.Method + "\n" + c.Request.URL.Path))
		expected := hex.EncodeToString(mac.Sum(nil))
		if !hmac.Equal([]byte(strings.ToLower(provided)), []byte(expected)) {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"code": "SERVICE_UNAUTHORIZED", "message": "invalid service signature"})
			return
		}
		c.Set("admin_api_service", cfg.ServiceID)
		c.Set(contextUserKey, &model.User{ID: c.GetHeader("X-User-ID"), Role: model.RoleAdmin, Status: model.UserStatusActive})
		c.Next()
	}
}
