package server

import (
	"net/http"
	"strings"

	"tmk-glance/internal/events"

	"github.com/gin-gonic/gin"
)

func (a *Application) handleInternalEvaluationEvent(c *gin.Context) {
	var request struct {
		EventType   string         `json:"event_type"`
		AggregateID string         `json:"aggregate_id"`
		RequestID   string         `json:"request_id"`
		Payload     map[string]any `json:"payload"`
	}
	if err := c.ShouldBindJSON(&request); err != nil || strings.TrimSpace(request.EventType) == "" {
		c.JSON(http.StatusBadRequest, gin.H{"code": "INVALID_EVENT", "message": "event_type is required"})
		return
	}
	a.eventBus.Publish(events.New(request.EventType, request.AggregateID, request.RequestID, request.Payload))
	c.JSON(http.StatusAccepted, gin.H{"code": "ACCEPTED", "message": "event accepted"})
}
