package server

import (
	"database/sql"
	"encoding/json"
	"errors"
	"log"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"tmk-glance/internal/model"
	"tmk-glance/internal/store"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

type evaluationJobView struct {
	model.EvaluationJob
	Progress          float64 `json:"progress"`
	ASRCER            float64 `json:"asr_cer"`
	SegmentedCER      float64 `json:"segmented_cer"`
	ASRWER            float64 `json:"asr_wer"`
	SegmentedWER      float64 `json:"segmented_wer"`
	SegmentEvaluable  bool    `json:"segment_evaluable"`
	SegmentPrecision  float64 `json:"segment_precision"`
	SegmentRecall     float64 `json:"segment_recall"`
	SegmentF1         float64 `json:"segment_f1"`
	SegmentCountDelta int64   `json:"segment_count_delta"`
}

type evaluationResultView struct {
	model.EvaluationResult
	Segments          json.RawMessage `json:"segments"`
	ASRCER            float64         `json:"asr_cer"`
	SegmentedCER      float64         `json:"segmented_cer"`
	ASRWER            float64         `json:"asr_wer"`
	SegmentedWER      float64         `json:"segmented_wer"`
	SegmentEvaluable  bool            `json:"segment_evaluable"`
	SegmentPrecision  float64         `json:"segment_precision"`
	SegmentRecall     float64         `json:"segment_recall"`
	SegmentF1         float64         `json:"segment_f1"`
	SegmentCountDelta int64           `json:"segment_count_delta"`
}

func (a *Application) handleCreateEvaluationJob(c *gin.Context) {
	var req struct {
		DatasetID         string `json:"dataset_id"`
		SegmenterEnabled  *bool  `json:"segmenter_enabled"`
		MaxRunes          int    `json:"max_runes"`
		MaxDurationMS     int    `json:"max_duration_ms"`
		SoftCommitDelayMS int    `json:"soft_commit_delay_ms"`
	}
	if err := c.ShouldBindJSON(&req); err != nil || strings.TrimSpace(req.DatasetID) == "" {
		c.JSON(http.StatusBadRequest, gin.H{"code": 400, "message": "dataset_id is required"})
		return
	}
	enabled := a.cfg.ASR.Segmenter.Enabled
	if req.SegmenterEnabled != nil {
		enabled = *req.SegmenterEnabled
	}
	maxRunes := defaultInt(req.MaxRunes, a.cfg.ASR.Segmenter.MaxRunes)
	maxDuration := defaultInt(req.MaxDurationMS, a.cfg.ASR.Segmenter.MaxDurationMS)
	softDelay := defaultInt(req.SoftCommitDelayMS, a.cfg.ASR.Segmenter.SoftCommitDelayMS)
	if maxRunes < 1 || maxRunes > 500 || maxDuration < 100 || maxDuration > 60000 || softDelay < 10 || softDelay > 10000 {
		c.JSON(http.StatusBadRequest, gin.H{"code": 400, "message": "invalid segmenter configuration"})
		return
	}
	user := currentUser(c)
	job := &model.EvaluationJob{ID: uuid.NewString(), DatasetID: strings.TrimSpace(req.DatasetID),
		Status: model.EvaluationJobQueued, RequestedBy: user.ID, CreatedAt: time.Now().UTC(), MaxAttempts: a.cfg.Evaluation.MaxAttempts,
		Config: model.EvaluationConfig{ASRProvider: a.cfg.ASR.Provider, SegmenterEnabled: enabled,
			MaxSentenceSilenceMS:         a.cfg.ASR.Bailian.MaxSentenceSilenceMS,
			SemanticPunctuationEnabled:   a.cfg.ASR.Bailian.SemanticPunctuationEnabled,
			MultiThresholdModeEnabled:    a.cfg.ASR.Bailian.MultiThresholdModeEnabled,
			PunctuationPredictionEnabled: a.cfg.ASR.Bailian.PunctuationPredictionEnabled,
			MaxRunes:                     maxRunes, MaxDurationMS: maxDuration, SoftCommitDelayMS: softDelay}}
	if err := a.store.CreateEvaluationJob(job); err != nil {
		switch {
		case errors.Is(err, sql.ErrNoRows):
			c.JSON(http.StatusNotFound, gin.H{"code": 404, "message": "dataset not found"})
		case errors.Is(err, store.ErrDatasetNotReady):
			c.JSON(http.StatusConflict, gin.H{"code": 409, "message": "only a non-empty ready dataset can be evaluated"})
		default:
			log.Printf("[evaluation] create job failed: %v", err)
			c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "message": "create evaluation job failed"})
		}
		return
	}
	slog.InfoContext(c.Request.Context(), "evaluation task queued", "component", "evaluation", "job_id", job.ID,
		"dataset_id", job.DatasetID, "total_items", job.TotalItems, "max_attempts", job.MaxAttempts)
	a.audit(c, user.ID, "evaluation.job.create", "evaluation_job", job.ID, "success", gin.H{"dataset_id": job.DatasetID, "revision": job.DatasetRevision})
	c.JSON(http.StatusAccepted, gin.H{"code": 0, "message": "queued", "data": newEvaluationJobView(job)})
}

func (a *Application) handleListEvaluationJobs(c *gin.Context) {
	status := strings.TrimSpace(c.Query("status"))
	if status != "" && !validEvaluationStatus(status) {
		c.JSON(http.StatusBadRequest, gin.H{"code": 400, "message": "invalid evaluation job status"})
		return
	}
	offset, limit := pagination(c, 20)
	jobs, total, err := a.store.ListEvaluationJobs(status, limit, offset)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "message": "list evaluation jobs failed"})
		return
	}
	views := make([]evaluationJobView, 0, len(jobs))
	for i := range jobs {
		views = append(views, newEvaluationJobView(&jobs[i]))
	}
	c.JSON(http.StatusOK, gin.H{"code": 0, "message": "ok", "data": gin.H{"jobs": views, "total": total, "offset": offset, "limit": limit}})
}

func (a *Application) handleGetEvaluationJob(c *gin.Context) {
	job, ok, err := a.store.GetEvaluationJob(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "message": "get evaluation job failed"})
		return
	}
	if !ok {
		c.JSON(http.StatusNotFound, gin.H{"code": 404, "message": "evaluation job not found"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"code": 0, "message": "ok", "data": newEvaluationJobView(job)})
}

func (a *Application) handleListEvaluationResults(c *gin.Context) {
	if _, ok, err := a.store.GetEvaluationJob(c.Param("id")); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "message": "list evaluation results failed"})
		return
	} else if !ok {
		c.JSON(http.StatusNotFound, gin.H{"code": 404, "message": "evaluation job not found"})
		return
	}
	offset, limit := pagination(c, 50)
	results, total, err := a.store.ListEvaluationResults(c.Param("id"), limit, offset)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "message": "list evaluation results failed"})
		return
	}
	views := make([]evaluationResultView, 0, len(results))
	for i := range results {
		views = append(views, newEvaluationResultView(&results[i]))
	}
	c.JSON(http.StatusOK, gin.H{"code": 0, "message": "ok", "data": gin.H{"results": views, "total": total, "offset": offset, "limit": limit}})
}

func (a *Application) handleCancelEvaluationJob(c *gin.Context) {
	job, ok, err := a.store.GetEvaluationJob(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "message": "cancel evaluation job failed"})
		return
	}
	if !ok {
		c.JSON(http.StatusNotFound, gin.H{"code": 404, "message": "evaluation job not found"})
		return
	}
	if job.Status != model.EvaluationJobQueued && job.Status != model.EvaluationJobRunning {
		c.JSON(http.StatusConflict, gin.H{"code": 409, "message": "evaluation job is already terminal"})
		return
	}
	cancelled, err := a.evaluations.Cancel(job.ID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "message": "cancel evaluation job failed"})
		return
	}
	if !cancelled {
		c.JSON(http.StatusConflict, gin.H{"code": 409, "message": "evaluation job changed while being cancelled"})
		return
	}
	user := currentUser(c)
	a.audit(c, user.ID, "evaluation.job.cancel", "evaluation_job", job.ID, "success", nil)
	c.JSON(http.StatusOK, gin.H{"code": 0, "message": "cancelled"})
}

func (a *Application) handleRetryEvaluationJob(c *gin.Context) {
	job, ok, err := a.store.GetEvaluationJob(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "message": "get evaluation job failed"})
		return
	}
	if !ok {
		c.JSON(http.StatusNotFound, gin.H{"code": 404, "message": "evaluation job not found"})
		return
	}
	if job.Status != model.EvaluationJobDeadLettered && job.Status != model.EvaluationJobFailed {
		c.JSON(http.StatusConflict, gin.H{"code": 409, "message": "only failed or dead-lettered jobs can be retried"})
		return
	}
	retried, err := a.store.RetryDeadLetteredEvaluationJob(job.ID, time.Now().UTC(), a.cfg.Evaluation.MaxAttempts)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "message": "retry evaluation job failed"})
		return
	}
	if !retried {
		c.JSON(http.StatusConflict, gin.H{"code": 409, "message": "evaluation job changed while being retried"})
		return
	}
	a.audit(c, currentUser(c).ID, "evaluation.job.retry", "evaluation_job", job.ID, "success", nil)
	c.JSON(http.StatusAccepted, gin.H{"code": 0, "message": "queued"})
}

func newEvaluationJobView(job *model.EvaluationJob) evaluationJobView {
	progress := float64(0)
	if job.TotalItems > 0 {
		progress = float64(job.CompletedItems) / float64(job.TotalItems)
	}
	return evaluationJobView{EvaluationJob: *job, Progress: progress, ASRCER: job.ASRCER(), SegmentedCER: job.SegmentedCER(),
		ASRWER: job.ASRWER(), SegmentedWER: job.SegmentedWER(), SegmentEvaluable: job.SegmentReference > 0,
		SegmentPrecision: job.SegmentPrecision(), SegmentRecall: job.SegmentRecall(), SegmentF1: job.SegmentF1(),
		SegmentCountDelta: job.SegmentPredicted - job.SegmentReference}
}

func newEvaluationResultView(result *model.EvaluationResult) evaluationResultView {
	segments := json.RawMessage(result.SegmentsJSON)
	if !json.Valid(segments) {
		segments = json.RawMessage("[]")
	}
	return evaluationResultView{EvaluationResult: *result, Segments: segments, ASRCER: result.ASRCER(), SegmentedCER: result.SegmentedCER(),
		ASRWER: result.ASRWER(), SegmentedWER: result.SegmentedWER(), SegmentEvaluable: result.SegmentReference > 0,
		SegmentPrecision: result.SegmentPrecision(), SegmentRecall: result.SegmentRecall(), SegmentF1: result.SegmentF1(),
		SegmentCountDelta: result.SegmentPredicted - result.SegmentReference}
}

func validEvaluationStatus(value string) bool {
	switch value {
	case model.EvaluationJobQueued, model.EvaluationJobRunning, model.EvaluationJobSucceeded,
		model.EvaluationJobCompletedWithErrors, model.EvaluationJobFailed, model.EvaluationJobCancelled,
		model.EvaluationJobDeadLettered:
		return true
	default:
		return false
	}
}

func defaultInt(value, fallback int) int {
	if value == 0 {
		return fallback
	}
	return value
}
