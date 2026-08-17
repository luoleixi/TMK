package store

import (
	"database/sql"
	"strings"
	"time"

	"tmk-glance/internal/model"
)

func (s *SessionStore) DashboardSnapshot(windowDays int, now time.Time) (*model.DashboardSnapshot, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	now = now.UTC()
	since := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC).AddDate(0, 0, -(windowDays - 1))
	snapshot := &model.DashboardSnapshot{GeneratedAt: now, WindowDays: windowDays, Daily: dashboardDays(since, now)}

	if err := s.db.QueryRow(`SELECT COUNT(*),
		COALESCE(SUM(CASE WHEN status='active' THEN 1 ELSE 0 END),0),
		COALESCE(SUM(CASE WHEN status='disabled' THEN 1 ELSE 0 END),0),
		COALESCE(SUM(CASE WHEN role='admin' THEN 1 ELSE 0 END),0) FROM users`).Scan(
		&snapshot.Users.Total, &snapshot.Users.Active, &snapshot.Users.Disabled, &snapshot.Users.Administrators); err != nil {
		return nil, err
	}
	if err := s.db.QueryRow(`SELECT COUNT(*),
		COALESCE(SUM(CASE WHEN created_at>=? THEN 1 ELSE 0 END),0),
		COALESCE(SUM(CASE WHEN status='ready' THEN 1 ELSE 0 END),0),
		COALESCE(SUM(CASE WHEN status='active' THEN 1 ELSE 0 END),0),
		COALESCE(SUM(CASE WHEN status='completed' THEN 1 ELSE 0 END),0),
		COALESCE(SUM(CASE WHEN status='error' THEN 1 ELSE 0 END),0),
		COALESCE(SUM(record_count),0) FROM sessions`, formatTime(since)).Scan(
		&snapshot.Sessions.Total, &snapshot.Sessions.InWindow, &snapshot.Sessions.Ready,
		&snapshot.Sessions.Active, &snapshot.Sessions.Completed, &snapshot.Sessions.Failed, &snapshot.Sessions.Records); err != nil {
		return nil, err
	}
	if err := s.db.QueryRow(`SELECT COUNT(*),
		COALESCE(SUM(CASE WHEN kind='audio' THEN 1 ELSE 0 END),0),
		COALESCE(SUM(CASE WHEN kind='text' THEN 1 ELSE 0 END),0), COALESCE(SUM(size_bytes),0)
		FROM storage_objects WHERE status='ready'`).Scan(&snapshot.Storage.Objects, &snapshot.Storage.AudioFiles,
		&snapshot.Storage.TextFiles, &snapshot.Storage.Bytes); err != nil {
		return nil, err
	}
	if err := s.db.QueryRow(`SELECT COUNT(*),
		COALESCE(SUM(CASE WHEN status='draft' THEN 1 ELSE 0 END),0),
		COALESCE(SUM(CASE WHEN status='ready' THEN 1 ELSE 0 END),0),
		COALESCE(SUM(CASE WHEN status='archived' THEN 1 ELSE 0 END),0), COALESCE(SUM(item_count),0)
		FROM datasets`).Scan(&snapshot.Datasets.Total, &snapshot.Datasets.Draft, &snapshot.Datasets.Ready,
		&snapshot.Datasets.Archived, &snapshot.Datasets.Items); err != nil {
		return nil, err
	}
	if err := s.db.QueryRow(`SELECT COUNT(*),
		COALESCE(SUM(CASE WHEN created_at>=? THEN 1 ELSE 0 END),0),
		COALESCE(SUM(CASE WHEN status='queued' THEN 1 ELSE 0 END),0),
		COALESCE(SUM(CASE WHEN status='running' THEN 1 ELSE 0 END),0),
		COALESCE(SUM(CASE WHEN status='succeeded' THEN 1 ELSE 0 END),0),
		COALESCE(SUM(CASE WHEN status='completed_with_errors' THEN 1 ELSE 0 END),0),
		COALESCE(SUM(CASE WHEN status='failed' THEN 1 ELSE 0 END),0),
		COALESCE(SUM(CASE WHEN status='cancelled' THEN 1 ELSE 0 END),0),
		COALESCE(SUM(CASE WHEN status='dead_lettered' THEN 1 ELSE 0 END),0),
		COALESCE(SUM(completed_items),0), COALESCE(SUM(failed_items),0),
		COALESCE(SUM(asr_char_distance),0), COALESCE(SUM(asr_char_units),0),
		COALESCE(SUM(segmented_char_distance),0), COALESCE(SUM(segmented_char_units),0),
		COALESCE(SUM(asr_word_distance),0), COALESCE(SUM(asr_word_units),0),
		COALESCE(SUM(segmented_word_distance),0), COALESCE(SUM(segmented_word_units),0),
		COALESCE(SUM(segment_matched),0), COALESCE(SUM(segment_predicted),0), COALESCE(SUM(segment_reference),0)
		FROM evaluation_jobs`, formatTime(since)).Scan(&snapshot.Evaluations.Total, &snapshot.Evaluations.InWindow,
		&snapshot.Evaluations.Queued, &snapshot.Evaluations.Running, &snapshot.Evaluations.Succeeded,
		&snapshot.Evaluations.CompletedWithErrors, &snapshot.Evaluations.Failed, &snapshot.Evaluations.Cancelled,
		&snapshot.Evaluations.DeadLettered,
		&snapshot.Evaluations.CompletedItems, &snapshot.Evaluations.FailedItems,
		&snapshot.Evaluations.ASRCharDistance, &snapshot.Evaluations.ASRCharUnits,
		&snapshot.Evaluations.SegmentedCharDistance, &snapshot.Evaluations.SegmentedCharUnits,
		&snapshot.Evaluations.ASRWordDistance, &snapshot.Evaluations.ASRWordUnits,
		&snapshot.Evaluations.SegmentedWordDistance, &snapshot.Evaluations.SegmentedWordUnits,
		&snapshot.Evaluations.SegmentMatched, &snapshot.Evaluations.SegmentPredicted, &snapshot.Evaluations.SegmentReference); err != nil {
		return nil, err
	}
	points := make(map[string]*model.DashboardDailyPoint, len(snapshot.Daily))
	for i := range snapshot.Daily {
		points[snapshot.Daily[i].Date] = &snapshot.Daily[i]
	}
	if err := scanDailyCounts(s.db, `SELECT SUBSTR(created_at,1,10), COUNT(*) FROM sessions WHERE created_at>=? GROUP BY SUBSTR(created_at,1,10)`, since, func(point *model.DashboardDailyPoint, count int64) {
		point.Sessions = count
	}, points); err != nil {
		return nil, err
	}
	rows, err := s.db.Query(`SELECT SUBSTR(created_at,1,10), COUNT(*), COALESCE(SUM(completed_items),0), COALESCE(SUM(failed_items),0)
		FROM evaluation_jobs WHERE created_at>=? GROUP BY SUBSTR(created_at,1,10)`, formatTime(since))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var date string
		var jobs, items, failed int64
		if err := rows.Scan(&date, &jobs, &items, &failed); err != nil {
			return nil, err
		}
		if point := points[date]; point != nil {
			point.EvaluationJobs, point.EvaluationItems, point.FailedItems = jobs, items, failed
		}
	}
	return snapshot, rows.Err()
}

func dashboardDays(since, now time.Time) []model.DashboardDailyPoint {
	result := make([]model.DashboardDailyPoint, 0, int(now.Sub(since).Hours()/24)+1)
	for day := since; !day.After(now); day = day.AddDate(0, 0, 1) {
		result = append(result, model.DashboardDailyPoint{Date: day.Format("2006-01-02")})
	}
	return result
}

func scanDailyCounts(db *sql.DB, query string, since time.Time, apply func(*model.DashboardDailyPoint, int64), points map[string]*model.DashboardDailyPoint) error {
	rows, err := db.Query(query, formatTime(since))
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var date string
		var count int64
		if err := rows.Scan(&date, &count); err != nil {
			return err
		}
		if point := points[date]; point != nil {
			apply(point, count)
		}
	}
	return rows.Err()
}

func (s *SessionStore) GovernanceReport(now time.Time, sessionDays, evaluationDays, auditDays, staleDraftDays, stuckJobMinutes int) (*model.GovernanceReport, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	now = now.UTC()
	report := &model.GovernanceReport{GeneratedAt: now, SessionRetentionDays: sessionDays,
		EvaluationRetentionDays: evaluationDays, AuditRetentionDays: auditDays, StaleDraftDays: staleDraftDays,
		StuckJobMinutes: stuckJobMinutes}
	staleDraftCutoff := formatTime(now.AddDate(0, 0, -staleDraftDays))
	stuckCutoff := formatTime(now.Add(-time.Duration(stuckJobMinutes) * time.Minute))
	sessionCutoff := formatTime(now.AddDate(0, 0, -sessionDays))
	evaluationCutoff := formatTime(now.AddDate(0, 0, -evaluationDays))
	auditCutoff := formatTime(now.AddDate(0, 0, -auditDays))
	nowText := formatTime(now)
	queries := []struct {
		query  string
		args   []any
		target *int64
	}{
		{`SELECT COUNT(*) FROM datasets WHERE status='draft' AND updated_at<?`, []any{staleDraftCutoff}, &report.StaleDraftDatasets},
		{`SELECT COUNT(*) FROM datasets d WHERE d.status='ready' AND NOT EXISTS
			(SELECT 1 FROM evaluation_jobs j WHERE j.dataset_id=d.id AND j.dataset_revision=d.revision AND j.status='succeeded')`, nil, &report.ReadyDatasetsWithoutSuccess},
		{`SELECT COUNT(*) FROM evaluation_jobs WHERE (status='queued' AND created_at<?) OR
			(status='running' AND COALESCE(started_at,created_at)<?)`, []any{stuckCutoff, stuckCutoff}, &report.StuckEvaluationJobs},
		{`SELECT COUNT(*) FROM auth_tokens WHERE revoked_at IS NULL AND expires_at<?`, []any{nowText}, &report.ExpiredActiveTokens},
		{`SELECT COUNT(*) FROM auth_tokens WHERE revoked_at IS NOT NULL OR expires_at<?`, []any{nowText}, &report.RevokedOrExpiredTokens},
		{`SELECT COUNT(*) FROM sessions WHERE status IN ('completed','error') AND created_at<?`, []any{sessionCutoff}, &report.SessionsPastRetention},
		{`SELECT COUNT(*) FROM evaluation_jobs WHERE status IN ('succeeded','completed_with_errors','failed','cancelled','dead_lettered') AND created_at<?`, []any{evaluationCutoff}, &report.EvaluationJobsPastRetention},
		{`SELECT COUNT(*) FROM audit_logs WHERE created_at<?`, []any{auditCutoff}, &report.AuditEventsPastRetention},
	}
	for _, item := range queries {
		if err := s.db.QueryRow(item.query, item.args...).Scan(item.target); err != nil {
			return nil, err
		}
	}
	if err := s.db.QueryRow(`SELECT COUNT(*), COALESCE(SUM(o.size_bytes),0) FROM storage_objects o
		WHERE o.status='ready' AND NOT EXISTS (SELECT 1 FROM dataset_items i
		WHERE i.audio_object_id=o.id OR i.reference_text_object_id=o.id)`).Scan(
		&report.UnreferencedObjects, &report.UnreferencedObjectBytes); err != nil {
		return nil, err
	}
	return report, nil
}

func (s *SessionStore) ListAuditEvents(filter model.AuditFilter, limit, offset int) ([]model.AuditEvent, int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	clauses := make([]string, 0, 6)
	args := make([]any, 0, 6)
	add := func(clause string, value any) {
		clauses, args = append(clauses, clause), append(args, value)
	}
	if filter.Action != "" {
		add("action=?", filter.Action)
	}
	if filter.Result != "" {
		add("result=?", filter.Result)
	}
	if filter.ActorUserID != "" {
		add("actor_user_id=?", filter.ActorUserID)
	}
	if filter.ResourceType != "" {
		add("resource_type=?", filter.ResourceType)
	}
	if filter.CreatedFrom != nil {
		add("created_at>=?", formatTime(*filter.CreatedFrom))
	}
	if filter.CreatedTo != nil {
		add("created_at<=?", formatTime(*filter.CreatedTo))
	}
	where := ""
	if len(clauses) > 0 {
		where = " WHERE " + strings.Join(clauses, " AND ")
	}
	var total int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM audit_logs`+where, args...).Scan(&total); err != nil {
		return nil, 0, err
	}
	rows, err := s.db.Query(`SELECT id, actor_user_id, action, resource_type, resource_id, ip_address,
		user_agent, result, details_json, created_at FROM audit_logs`+where+` ORDER BY id DESC LIMIT ? OFFSET ?`, append(args, limit, offset)...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	events := make([]model.AuditEvent, 0, limit)
	for rows.Next() {
		var event model.AuditEvent
		var actor sql.NullString
		var createdAt string
		if err := rows.Scan(&event.ID, &actor, &event.Action, &event.ResourceType, &event.ResourceID,
			&event.IPAddress, &event.UserAgent, &event.Result, &event.DetailsJSON, &createdAt); err != nil {
			return nil, 0, err
		}
		if actor.Valid {
			event.ActorUserID = actor.String
		}
		parsed, err := time.Parse(time.RFC3339Nano, createdAt)
		if err != nil {
			return nil, 0, err
		}
		event.CreatedAt = parsed
		events = append(events, event)
	}
	return events, total, rows.Err()
}
