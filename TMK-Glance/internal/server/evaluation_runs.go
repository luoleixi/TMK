package server

import (
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"net/http"
	"time"
	"tmk-glance/internal/model"
)

func (a *Application) handleCreateEvaluationRun(c *gin.Context) {
	var req struct {
		DatasetID         string `json:"dataset_id"`
		MaxRunes          int    `json:"max_runes"`
		MaxDurationMS     int    `json:"max_duration_ms"`
		SoftCommitDelayMS int    `json:"soft_commit_delay_ms"`
		MinRunes          int    `json:"min_runes"`
		SemanticEnabled   bool   `json:"semantic_enabled"`
	}
	if c.ShouldBindJSON(&req) != nil || req.DatasetID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"code": 400, "message": "dataset_id is required"})
		return
	}
	defaults := a.cfg.ASR.Segmenter
	if req.MaxRunes < 1 {
		req.MaxRunes = defaults.MaxRunes
	}
	if req.MaxDurationMS < 1 {
		req.MaxDurationMS = defaults.MaxDurationMS
	}
	if req.SoftCommitDelayMS < 1 {
		req.SoftCommitDelayMS = defaults.SoftCommitDelayMS
	}
	if req.MinRunes < 1 {
		req.MinRunes = defaults.MinRunes
	}
	run := &model.EvaluationRun{ID: uuid.NewString(), DatasetID: req.DatasetID, CreatedBy: currentUser(c).ID, CreatedAt: time.Now().UTC(), Config: model.EvaluationConfig{ASRProvider: a.cfg.ASR.Provider, SegmenterVersion: defaults.Version, SegmenterStrategy: "hybrid", MaxRunes: req.MaxRunes, MaxDurationMS: req.MaxDurationMS, SoftCommitDelayMS: req.SoftCommitDelayMS, MinRunes: req.MinRunes, SemanticEnabled: req.SemanticEnabled}}
	on := &model.EvaluationJob{ID: uuid.NewString(), RunID: run.ID, Variant: "segmenter_on", PairKey: run.ID, RequestedBy: run.CreatedBy, CreatedAt: run.CreatedAt, MaxAttempts: a.cfg.Evaluation.MaxAttempts, Config: run.Config}
	off := &model.EvaluationJob{ID: uuid.NewString(), RunID: run.ID, Variant: "segmenter_off", PairKey: run.ID, RequestedBy: run.CreatedBy, CreatedAt: run.CreatedAt, MaxAttempts: a.cfg.Evaluation.MaxAttempts, Config: run.Config}
	on.Config.SegmenterEnabled = true
	off.Config.SegmenterEnabled = false
	if err := a.store.CreateEvaluationABRun(run, []*model.EvaluationJob{on, off}); err != nil {
		c.JSON(http.StatusConflict, gin.H{"code": 409, "message": "create evaluation run failed"})
		return
	}
	c.JSON(http.StatusAccepted, gin.H{"code": 0, "message": "queued", "data": gin.H{"run": run, "jobs": []*model.EvaluationJob{on, off}}})
}
func (a *Application) handleGetEvaluationRun(c *gin.Context) {
	run, ok, err := a.store.GetEvaluationRun(c.Param("id"))
	if err != nil || !ok {
		c.JSON(http.StatusNotFound, gin.H{"code": 404, "message": "evaluation run not found"})
		return
	}
	jobs, _ := a.store.ListEvaluationRunJobs(run.ID)
	c.JSON(http.StatusOK, gin.H{"code": 0, "message": "ok", "data": gin.H{"run": run, "jobs": jobs}})
}
func (a *Application) handleCompareEvaluationRun(c *gin.Context) {
	run, ok, err := a.store.GetEvaluationRun(c.Param("id"))
	if err != nil || !ok {
		c.JSON(http.StatusNotFound, gin.H{"code": 404, "message": "evaluation run not found"})
		return
	}
	jobs, err := a.store.ListEvaluationRunJobs(run.ID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "message": "list evaluation variants failed"})
		return
	}
	type metrics struct {
		Variant        string  `json:"variant"`
		Status         string  `json:"status"`
		CER            float64 `json:"cer"`
		WER            float64 `json:"wer"`
		SegmentF1      float64 `json:"segment_f1"`
		CompletedItems int     `json:"completed_items"`
	}
	out := make([]metrics, 0, 2)
	var baseline, experiment *metrics
	for _, job := range jobs {
		value := metrics{job.Variant, job.Status, job.ASRCER(), job.ASRWER(), job.SegmentF1(), job.CompletedItems}
		out = append(out, value)
		if job.Variant == "segmenter_off" {
			baseline = &out[len(out)-1]
		} else if job.Variant == "segmenter_on" {
			experiment = &out[len(out)-1]
		}
	}
	comparison := gin.H{"run": run, "variants": out}
	if baseline != nil && experiment != nil {
		comparison["baseline"] = baseline
		comparison["experiment"] = experiment
		comparison["delta"] = gin.H{"cer": experiment.CER - baseline.CER, "wer": experiment.WER - baseline.WER, "segment_f1": experiment.SegmentF1 - baseline.SegmentF1}
	}
	c.JSON(http.StatusOK, gin.H{"code": 0, "message": "ok", "data": comparison})
}
