package model

import "time"

type DashboardUsers struct {
	Total          int64 `json:"total"`
	Active         int64 `json:"active"`
	Disabled       int64 `json:"disabled"`
	Administrators int64 `json:"administrators"`
}

type DashboardSessions struct {
	Total     int64 `json:"total"`
	InWindow  int64 `json:"in_window"`
	Ready     int64 `json:"ready"`
	Active    int64 `json:"active"`
	Completed int64 `json:"completed"`
	Failed    int64 `json:"failed"`
	Records   int64 `json:"records"`
}

type DashboardStorage struct {
	Objects    int64 `json:"objects"`
	AudioFiles int64 `json:"audio_files"`
	TextFiles  int64 `json:"text_files"`
	Bytes      int64 `json:"bytes"`
}

type DashboardDatasets struct {
	Total    int64 `json:"total"`
	Draft    int64 `json:"draft"`
	Ready    int64 `json:"ready"`
	Archived int64 `json:"archived"`
	Items    int64 `json:"items"`
}

type DashboardEvaluations struct {
	Total                 int64 `json:"total"`
	InWindow              int64 `json:"in_window"`
	Queued                int64 `json:"queued"`
	Running               int64 `json:"running"`
	Succeeded             int64 `json:"succeeded"`
	CompletedWithErrors   int64 `json:"completed_with_errors"`
	Failed                int64 `json:"failed"`
	Cancelled             int64 `json:"cancelled"`
	DeadLettered          int64 `json:"dead_lettered"`
	CompletedItems        int64 `json:"completed_items"`
	FailedItems           int64 `json:"failed_items"`
	ASRCharDistance       int64 `json:"-"`
	ASRCharUnits          int64 `json:"-"`
	SegmentedCharDistance int64 `json:"-"`
	SegmentedCharUnits    int64 `json:"-"`
	ASRWordDistance       int64 `json:"-"`
	ASRWordUnits          int64 `json:"-"`
	SegmentedWordDistance int64 `json:"-"`
	SegmentedWordUnits    int64 `json:"-"`
	SegmentMatched        int64 `json:"segment_matched"`
	SegmentPredicted      int64 `json:"segment_predicted"`
	SegmentReference      int64 `json:"segment_reference"`
}

func (d DashboardEvaluations) ASRCER() float64 { return metricRate(d.ASRCharDistance, d.ASRCharUnits) }
func (d DashboardEvaluations) SegmentedCER() float64 {
	return metricRate(d.SegmentedCharDistance, d.SegmentedCharUnits)
}
func (d DashboardEvaluations) ASRWER() float64 { return metricRate(d.ASRWordDistance, d.ASRWordUnits) }
func (d DashboardEvaluations) SegmentedWER() float64 {
	return metricRate(d.SegmentedWordDistance, d.SegmentedWordUnits)
}
func (d DashboardEvaluations) SegmentPrecision() float64 {
	return metricRate(d.SegmentMatched, d.SegmentPredicted)
}
func (d DashboardEvaluations) SegmentRecall() float64 {
	return metricRate(d.SegmentMatched, d.SegmentReference)
}
func (d DashboardEvaluations) SegmentF1() float64 { return f1(d.SegmentPrecision(), d.SegmentRecall()) }

type DashboardDailyPoint struct {
	Date            string `json:"date"`
	Sessions        int64  `json:"sessions"`
	EvaluationJobs  int64  `json:"evaluation_jobs"`
	EvaluationItems int64  `json:"evaluation_items"`
	FailedItems     int64  `json:"failed_items"`
}

type DashboardSnapshot struct {
	GeneratedAt time.Time             `json:"generated_at"`
	WindowDays  int                   `json:"window_days"`
	Users       DashboardUsers        `json:"users"`
	Sessions    DashboardSessions     `json:"sessions"`
	Storage     DashboardStorage      `json:"storage"`
	Datasets    DashboardDatasets     `json:"datasets"`
	Evaluations DashboardEvaluations  `json:"evaluations"`
	Daily       []DashboardDailyPoint `json:"daily"`
}

type GovernanceReport struct {
	GeneratedAt                 time.Time `json:"generated_at"`
	StaleDraftDatasets          int64     `json:"stale_draft_datasets"`
	UnreferencedObjects         int64     `json:"unreferenced_objects"`
	UnreferencedObjectBytes     int64     `json:"unreferenced_object_bytes"`
	ReadyDatasetsWithoutSuccess int64     `json:"ready_datasets_without_successful_evaluation"`
	StuckEvaluationJobs         int64     `json:"stuck_evaluation_jobs"`
	ExpiredActiveTokens         int64     `json:"expired_active_tokens"`
	RevokedOrExpiredTokens      int64     `json:"revoked_or_expired_tokens"`
	SessionsPastRetention       int64     `json:"sessions_past_retention"`
	EvaluationJobsPastRetention int64     `json:"evaluation_jobs_past_retention"`
	AuditEventsPastRetention    int64     `json:"audit_events_past_retention"`
	DiskFreeBytes               uint64    `json:"disk_free_bytes"`
	DiskReserveBytes            int64     `json:"disk_reserve_bytes"`
	SessionRetentionDays        int       `json:"session_retention_days"`
	EvaluationRetentionDays     int       `json:"evaluation_retention_days"`
	AuditRetentionDays          int       `json:"audit_retention_days"`
	StaleDraftDays              int       `json:"stale_draft_days"`
	StuckJobMinutes             int       `json:"stuck_job_minutes"`
}

type AuditFilter struct {
	Action       string
	Result       string
	ActorUserID  string
	ResourceType string
	CreatedFrom  *time.Time
	CreatedTo    *time.Time
}
