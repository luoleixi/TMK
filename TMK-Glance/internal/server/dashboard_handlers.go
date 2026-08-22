package server

import (
	"encoding/json"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	"tmk-glance/internal/model"

	"github.com/gin-gonic/gin"
)

type dashboardEvaluationView struct {
	model.DashboardEvaluations
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

type auditEventView struct {
	model.AuditEvent
	Details json.RawMessage `json:"details"`
}

func (a *Application) handleDashboard(c *gin.Context) {
	days, err := boundedQueryInt(c, "days", 30, 7, 90)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 400, "message": "days must be between 7 and 90"})
		return
	}
	snapshot, err := a.store.DashboardSnapshot(days, time.Now())
	if err != nil {
		log.Printf("[dashboard] load snapshot failed: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "message": "load dashboard failed"})
		return
	}
	freeBytes, err := a.objectStore.FreeBytes()
	if err != nil {
		log.Printf("[dashboard] load disk space failed: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "message": "load dashboard failed"})
		return
	}
	recent, _, err := a.store.ListEvaluationJobs("", 10, 0)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "message": "load dashboard failed"})
		return
	}
	recentViews := make([]evaluationJobView, 0, len(recent))
	for i := range recent {
		recentViews = append(recentViews, newEvaluationJobView(&recent[i]))
	}
	evaluations := dashboardEvaluationView{DashboardEvaluations: snapshot.Evaluations,
		ASRCER: snapshot.Evaluations.ASRCER(), SegmentedCER: snapshot.Evaluations.SegmentedCER(),
		ASRWER: snapshot.Evaluations.ASRWER(), SegmentedWER: snapshot.Evaluations.SegmentedWER(),
		SegmentEvaluable: snapshot.Evaluations.SegmentReference > 0,
		SegmentPrecision: snapshot.Evaluations.SegmentPrecision(), SegmentRecall: snapshot.Evaluations.SegmentRecall(),
		SegmentF1:         snapshot.Evaluations.SegmentF1(),
		SegmentCountDelta: snapshot.Evaluations.SegmentPredicted - snapshot.Evaluations.SegmentReference}
	c.JSON(http.StatusOK, gin.H{"code": 0, "message": "ok", "data": gin.H{
		"generated_at": snapshot.GeneratedAt, "window_days": snapshot.WindowDays, "users": snapshot.Users,
		"sessions": snapshot.Sessions, "storage": gin.H{"objects": snapshot.Storage.Objects,
			"audio_files": snapshot.Storage.AudioFiles, "text_files": snapshot.Storage.TextFiles,
			"bytes": snapshot.Storage.Bytes, "disk_free_bytes": freeBytes,
			"quota_bytes": a.totalObjectQuota(), "reserve_bytes": a.minObjectFreeBytes()},
		"datasets": snapshot.Datasets, "evaluations": evaluations, "daily": snapshot.Daily,
		"recent_evaluation_jobs": recentViews,
	}})
}

func (a *Application) handleGovernanceReport(c *gin.Context) {
	policy := a.cfg.Governance
	report, err := a.store.GovernanceReport(time.Now(), policy.SessionRetentionDays,
		policy.EvaluationRetentionDays, policy.AuditRetentionDays, policy.StaleDraftDays, policy.StuckJobMinutes)
	if err != nil {
		log.Printf("[governance] load report failed: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "message": "load governance report failed"})
		return
	}
	freeBytes, err := a.objectStore.FreeBytes()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "message": "load governance report failed"})
		return
	}
	report.DiskFreeBytes, report.DiskReserveBytes = freeBytes, a.minObjectFreeBytes()
	c.JSON(http.StatusOK, gin.H{"code": 0, "message": "ok", "data": report})
}

func (a *Application) handleListAuditEvents(c *gin.Context) {
	filter := model.AuditFilter{Action: strings.TrimSpace(c.Query("action")), Result: strings.TrimSpace(c.Query("result")),
		ActorUserID: strings.TrimSpace(c.Query("actor_user_id")), ResourceType: strings.TrimSpace(c.Query("resource_type"))}
	if len(filter.Action) > 80 || len(filter.ActorUserID) > 36 || len(filter.ResourceType) > 40 ||
		(filter.Result != "" && filter.Result != "success" && filter.Result != "denied") {
		c.JSON(http.StatusBadRequest, gin.H{"code": 400, "message": "invalid audit filter"})
		return
	}
	var err error
	if filter.CreatedFrom, err = optionalRFC3339(c.Query("date_from")); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 400, "message": "date_from must be RFC3339"})
		return
	}
	if filter.CreatedTo, err = optionalRFC3339(c.Query("date_to")); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 400, "message": "date_to must be RFC3339"})
		return
	}
	if filter.CreatedFrom != nil && filter.CreatedTo != nil && filter.CreatedFrom.After(*filter.CreatedTo) {
		c.JSON(http.StatusBadRequest, gin.H{"code": 400, "message": "date_from must not be after date_to"})
		return
	}
	offset, limit := pagination(c, 50)
	events, total, err := a.store.ListAuditEvents(filter, limit, offset)
	if err != nil {
		log.Printf("[audit] list failed: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "message": "list audit events failed"})
		return
	}
	views := make([]auditEventView, 0, len(events))
	for _, event := range events {
		details := json.RawMessage(event.DetailsJSON)
		if !json.Valid(details) {
			details = json.RawMessage("{}")
		}
		views = append(views, auditEventView{AuditEvent: event, Details: details})
	}
	c.JSON(http.StatusOK, gin.H{"code": 0, "message": "ok", "data": gin.H{
		"events": views, "total": total, "offset": offset, "limit": limit,
	}})
}

func boundedQueryInt(c *gin.Context, name string, fallback, minimum, maximum int) (int, error) {
	value := strings.TrimSpace(c.Query(name))
	if value == "" {
		return fallback, nil
	}
	parsed, err := strconv.Atoi(value)
	if err != nil || parsed < minimum || parsed > maximum {
		return 0, strconv.ErrSyntax
	}
	return parsed, nil
}

func optionalRFC3339(value string) (*time.Time, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil, nil
	}
	parsed, err := time.Parse(time.RFC3339, value)
	if err != nil {
		return nil, err
	}
	parsed = parsed.UTC()
	return &parsed, nil
}
