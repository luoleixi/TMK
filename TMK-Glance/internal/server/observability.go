package server

import (
	"log/slog"
	"strings"
	"time"

	"tmk-glance/internal/health"
	"tmk-glance/internal/observability"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

const requestIDHeader = "X-Request-ID"
const traceIDHeader = "X-Trace-ID"

func (a *Application) observabilityMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		requestID := strings.TrimSpace(c.GetHeader(requestIDHeader))
		if len(requestID) < 8 || len(requestID) > 128 {
			requestID = uuid.NewString()
		}
		c.Header(requestIDHeader, requestID)
		traceID := strings.TrimSpace(c.GetHeader(traceIDHeader))
		if len(traceID) < 8 || len(traceID) > 128 {
			traceID = uuid.NewString()
		}
		c.Header(traceIDHeader, traceID)
		ctx := observability.WithRequestID(c.Request.Context(), requestID)
		c.Request = c.Request.WithContext(observability.WithTraceID(ctx, traceID))
		started := time.Now()
		a.metrics.BeginHTTP()
		c.Next()
		route := c.FullPath()
		if route == "" {
			route = "unmatched"
		}
		elapsed := time.Since(started)
		a.metrics.EndHTTP(c.Request.Method, route, c.Writer.Status(), elapsed)
		slog.InfoContext(c.Request.Context(), "http request",
			"method", c.Request.Method, "route", route, "status", c.Writer.Status(),
			"duration_ms", elapsed.Milliseconds(), "response_bytes", c.Writer.Size(), "client_ip", c.ClientIP(), "trace_id", traceID)
	}
}

func (a *Application) startRuntimeSampler() {
	a.sampleRuntime()
	a.samplerDone = make(chan struct{})
	go func() {
		defer close(a.samplerDone)
		ticker := time.NewTicker(15 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				a.sampleRuntime()
			case <-a.samplerStop:
				return
			}
		}
	}()
}

func (a *Application) sampleRuntime() {
	dbOK := a.store.Ping() == nil
	queued, running, storageBytes, snapshotErr := a.store.ObservabilitySnapshot()
	freeBytes, storageErr := a.objectStore.FreeBytes()
	a.databaseHealthy.Store(dbOK && snapshotErr == nil)
	a.storageHealthy.Store(storageErr == nil)
	ready := a.databaseHealthy.Load() && a.storageHealthy.Load()
	a.metrics.SetReady(ready)
	health.SetReady(ready)
	if snapshotErr == nil && storageErr == nil {
		a.metrics.SetRuntime(a.store.DBStats(), queued, running, storageBytes, freeBytes)
	}
}

func (a *Application) metricsHandler(c *gin.Context) {
	a.metrics.Handler().ServeHTTP(c.Writer, c.Request)
}

func (a *Application) registerHealthChecks() {
	health.Register("database", func() bool { return a.databaseHealthy.Load() })
	health.Register("object_storage", func() bool { return a.storageHealthy.Load() })
}
