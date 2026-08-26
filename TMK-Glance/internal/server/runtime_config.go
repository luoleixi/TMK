package server

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"tmk-glance/internal/segmenter"
)

type segmenterRuntimeState struct {
	config  segmenter.Config
	version string
}
type segmenterRuntimeRequest struct {
	Enabled        bool           `json:"enabled"`
	RolloutPercent int            `json:"rollout_percent"`
	Version        string         `json:"version"`
	Config         map[string]any `json:"config"`
	Revision       int64          `json:"revision"`
}

func (a *Application) currentSegmenterConfig() segmenter.Config {
	return a.segmenterRuntime.Load().(segmenterRuntimeState).config
}

func (a *Application) handleGetSegmenterRuntime(c *gin.Context) {
	state := a.segmenterRuntime.Load().(segmenterRuntimeState)
	c.JSON(http.StatusOK, gin.H{"code": "OK", "message": "ok", "data": gin.H{"enabled": state.config.Enabled, "version": state.version, "applied_revision": a.segmenterAppliedRevision.Load()}})
}

func (a *Application) handleApplySegmenterRuntime(c *gin.Context) {
	var req segmenterRuntimeRequest
	if c.ShouldBindJSON(&req) != nil || req.Revision < 1 || req.RolloutPercent < 0 || req.RolloutPercent > 100 || strings.TrimSpace(req.Version) == "" {
		c.JSON(http.StatusBadRequest, gin.H{"code": "INVALID_REQUEST", "message": "invalid segmenter runtime configuration"})
		return
	}
	if req.Revision <= a.segmenterAppliedRevision.Load() {
		c.JSON(http.StatusConflict, gin.H{"code": "STALE_REVISION", "message": "segmenter configuration revision is stale"})
		return
	}
	state := a.segmenterRuntime.Load().(segmenterRuntimeState)
	config := state.config
	config.Enabled = req.Enabled
	a.segmenterRuntime.Store(segmenterRuntimeState{config: config, version: req.Version})
	a.segmenterAppliedRevision.Store(req.Revision)
	c.JSON(http.StatusOK, gin.H{"code": "OK", "message": "segmenter configuration applied", "data": gin.H{"enabled": config.Enabled, "version": req.Version, "applied_revision": req.Revision}})
}
