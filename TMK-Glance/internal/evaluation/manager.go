package evaluation

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"log/slog"
	"strings"
	"sync"
	"time"
	"unicode"

	"tmk-glance/internal/asr"
	"tmk-glance/internal/model"
	"tmk-glance/internal/objectstore"
	"tmk-glance/internal/observability"
	"tmk-glance/internal/segmenter"
	"tmk-glance/internal/store"

	"github.com/google/uuid"
)

type Config struct {
	Workers           int
	PollInterval      time.Duration
	ItemTimeout       time.Duration
	ChunkInterval     time.Duration
	MaxTextBytes      int64
	Metrics           *observability.Metrics
	LeaseDuration     time.Duration
	HeartbeatInterval time.Duration
	RetryBase         time.Duration
	ReaperInterval    time.Duration
}

type Manager struct {
	store      *store.SessionStore
	objects    *objectstore.Local
	asrFactory func(string, model.EvaluationConfig) asr.ASR
	config     Config
	ctx        context.Context
	cancel     context.CancelCauseFunc
	wg         sync.WaitGroup
	activeMu   sync.Mutex
	activeJobs map[string]context.CancelCauseFunc
	instanceID string
}

var (
	errManagerShutdown = errors.New("evaluation manager shutting down")
	errUserCancelled   = errors.New("evaluation job cancelled")
)

func NewManager(database *store.SessionStore, objects *objectstore.Local, asrFactory func(string, model.EvaluationConfig) asr.ASR, config Config) *Manager {
	if config.Workers < 1 || config.Workers > 8 {
		config.Workers = 1
	}
	if config.PollInterval <= 0 {
		config.PollInterval = 500 * time.Millisecond
	}
	if config.ItemTimeout <= 0 {
		config.ItemTimeout = 10 * time.Minute
	}
	if config.MaxTextBytes <= 0 {
		config.MaxTextBytes = 10 << 20
	}
	if config.LeaseDuration <= 0 {
		config.LeaseDuration = time.Minute
	}
	if config.HeartbeatInterval <= 0 || config.HeartbeatInterval >= config.LeaseDuration {
		config.HeartbeatInterval = config.LeaseDuration / 4
	}
	if config.RetryBase <= 0 {
		config.RetryBase = 5 * time.Second
	}
	if config.ReaperInterval <= 0 {
		config.ReaperInterval = 10 * time.Second
	}
	ctx, cancel := context.WithCancelCause(context.Background())
	return &Manager{store: database, objects: objects, asrFactory: asrFactory, config: config,
		ctx: ctx, cancel: cancel, activeJobs: make(map[string]context.CancelCauseFunc), instanceID: uuid.NewString()}
}

func (m *Manager) Start() error {
	requeued, deadLettered, err := m.store.RecoverExpiredEvaluationJobs(time.Now().UTC(), m.config.RetryBase)
	if err != nil {
		return err
	}
	if m.config.Metrics != nil {
		m.config.Metrics.EvaluationTransition("lease_requeued", requeued)
		m.config.Metrics.EvaluationTransition("dead_lettered", deadLettered)
	}
	m.wg.Add(1)
	go m.reaper()
	for i := 0; i < m.config.Workers; i++ {
		m.wg.Add(1)
		go m.worker(i + 1)
	}
	return nil
}

func (m *Manager) Close() {
	m.cancel(errManagerShutdown)
	m.activeMu.Lock()
	for _, cancel := range m.activeJobs {
		cancel(errManagerShutdown)
	}
	m.activeMu.Unlock()
	m.wg.Wait()
}

func (m *Manager) Cancel(jobID string) (bool, error) {
	cancelled, err := m.store.CancelEvaluationJob(jobID)
	if err != nil || !cancelled {
		return cancelled, err
	}
	m.activeMu.Lock()
	if cancel := m.activeJobs[jobID]; cancel != nil {
		cancel(errUserCancelled)
	}
	m.activeMu.Unlock()
	return true, nil
}

func (m *Manager) worker(number int) {
	defer m.wg.Done()
	ticker := time.NewTicker(m.config.PollInterval)
	defer ticker.Stop()
	workerID := fmt.Sprintf("%s-%d", m.instanceID, number)
	for {
		if m.ctx.Err() != nil {
			return
		}
		job, ok, err := m.store.ClaimNextEvaluationJob(workerID, time.Now().UTC(), m.config.LeaseDuration)
		if err != nil {
			log.Printf("[evaluation] worker=%d claim failed: %v", number, err)
		} else if ok {
			m.runJob(job, workerID)
			continue
		}
		select {
		case <-m.ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func (m *Manager) reaper() {
	defer m.wg.Done()
	ticker := time.NewTicker(m.config.ReaperInterval)
	defer ticker.Stop()
	for {
		select {
		case <-m.ctx.Done():
			return
		case now := <-ticker.C:
			requeued, deadLettered, err := m.store.RecoverExpiredEvaluationJobs(now.UTC(), m.config.RetryBase)
			if err != nil {
				log.Printf("[evaluation] lease reaper failed: %v", err)
			} else if requeued > 0 || deadLettered > 0 {
				log.Printf("[evaluation] recovered expired leases requeued=%d dead_lettered=%d", requeued, deadLettered)
				if m.config.Metrics != nil {
					m.config.Metrics.EvaluationTransition("lease_requeued", requeued)
					m.config.Metrics.EvaluationTransition("dead_lettered", deadLettered)
				}
			}
		}
	}
}

func (m *Manager) runJob(job *model.EvaluationJob, workerID string) {
	started := time.Now()
	if wait := started.Sub(job.CreatedAt); wait > 0 && m.config.Metrics != nil {
		m.config.Metrics.EvaluationQueueWait(wait)
	}
	outcome := model.EvaluationJobFailed
	if job.FailedItems > 0 {
		outcome = model.EvaluationJobCompletedWithErrors
	}
	defer func() {
		if recovered := recover(); recovered != nil {
			outcome = m.retryAttempt(job.ID, workerID, fmt.Sprintf("worker panic: %v", recovered))
			log.Printf("[evaluation] recovered worker panic job=%s: %v", job.ID, recovered)
		}
		if m.config.Metrics != nil {
			m.config.Metrics.EvaluationJob(string(outcome), time.Since(started))
		}
		attrs := []any{"component", "evaluation", "job_id", job.ID, "dataset_id", job.DatasetID,
			"worker_id", workerID, "attempt", job.AttemptCount, "outcome", outcome, "duration_ms", time.Since(started).Milliseconds()}
		if outcome == model.EvaluationJobSucceeded || outcome == model.EvaluationJobCompletedWithErrors {
			slog.Info("evaluation task attempt completed", attrs...)
		} else {
			slog.Warn("evaluation task attempt ended", attrs...)
		}
	}()
	slog.Info("evaluation task attempt started", "component", "evaluation", "job_id", job.ID,
		"dataset_id", job.DatasetID, "worker_id", workerID, "attempt", job.AttemptCount)
	ctx, cancel := context.WithCancelCause(m.ctx)
	m.activeMu.Lock()
	m.activeJobs[job.ID] = cancel
	m.activeMu.Unlock()
	defer func() {
		cancel(nil)
		m.activeMu.Lock()
		delete(m.activeJobs, job.ID)
		m.activeMu.Unlock()
	}()
	heartbeatDone := make(chan struct{})
	go m.heartbeat(ctx, cancel, job, workerID, heartbeatDone)
	defer func() {
		cancel(nil)
		<-heartbeatDone
	}()
	runLeaseDone := make(chan struct{})
	if job.RunID != "" && job.Variant == "segmenter_on" {
		_, claimed, err := m.store.ClaimEvaluationRun(job.RunID, workerID, time.Now().UTC(), m.config.LeaseDuration)
		if err != nil {
			outcome = m.retryAttempt(job.ID, workerID, "claim evaluation run lease failed: "+err.Error())
			return
		}
		if !claimed {
			outcome = model.EvaluationJobRunning
			return
		}
		go m.runHeartbeat(ctx, cancel, job.RunID, workerID, runLeaseDone)
		defer func() {
			cancel(nil)
			<-runLeaseDone
			_, _ = m.store.ReleaseEvaluationRun(job.RunID, workerID, time.Now().UTC())
		}()
	}

	items, err := m.store.ListEvaluationWorkItems(job.DatasetID)
	if err != nil {
		if ctx.Err() != nil {
			outcome = m.handleInterrupted(job.ID, workerID, context.Cause(ctx))
		} else {
			outcome = m.retryAttempt(job.ID, workerID, err.Error())
		}
		return
	}
	if len(items) != job.TotalItems {
		message := "dataset items changed or cannot be loaded"
		_, _ = m.store.FinishEvaluationJob(job.ID, workerID, true, message, time.Now().UTC())
		return
	}
	completed, err := m.store.CompletedEvaluationItemIDs(job.ID)
	if err != nil {
		if ctx.Err() != nil {
			outcome = m.handleInterrupted(job.ID, workerID, context.Cause(ctx))
		} else {
			outcome = m.retryAttempt(job.ID, workerID, err.Error())
		}
		return
	}
	for _, item := range items {
		if completed[item.DatasetItemID] {
			continue
		}
		if ctx.Err() != nil {
			outcome = m.handleInterrupted(job.ID, workerID, context.Cause(ctx))
			return
		}
		result := m.evaluateItem(ctx, job, item)
		if ctx.Err() != nil {
			outcome = m.handleInterrupted(job.ID, workerID, context.Cause(ctx))
			return
		}
		if result.Status == model.EvaluationResultFailed {
			outcome = model.EvaluationJobCompletedWithErrors
		}
		if err := m.store.SaveEvaluationResult(result, workerID, time.Now().UTC()); err != nil {
			if ctx.Err() != nil {
				outcome = m.handleInterrupted(job.ID, workerID, context.Cause(ctx))
				return
			}
			if !errors.Is(err, store.ErrLeaseLost) {
				log.Printf("[evaluation] save result job=%s item=%s: %v", job.ID, item.DatasetItemID, err)
				outcome = m.retryAttempt(job.ID, workerID, "save evaluation result failed")
			}
			return
		}
	}
	if job.RunID != "" && job.Variant == "segmenter_on" {
		if err := m.runABVariant(ctx, job, items, workerID); err != nil {
			outcome = m.retryAttempt(job.ID, workerID, err.Error())
			return
		}
		if err := m.store.AggregateEvaluationRun(job.RunID, time.Now().UTC()); err != nil {
			outcome = m.retryAttempt(job.ID, workerID, "aggregate evaluation run failed: "+err.Error())
			return
		}
	}
	finished, err := m.store.FinishEvaluationJob(job.ID, workerID, false, "", time.Now().UTC())
	if err != nil || !finished {
		if err != nil && !errors.Is(err, store.ErrLeaseLost) {
			outcome = m.retryAttempt(job.ID, workerID, err.Error())
		}
		return
	}
	if outcome != model.EvaluationJobCompletedWithErrors {
		outcome = model.EvaluationJobSucceeded
	}
}

func (m *Manager) runABVariant(ctx context.Context, primary *model.EvaluationJob, items []model.EvaluationWorkItem, workerID string) error {
	jobs, err := m.store.ListEvaluationRunJobs(primary.RunID)
	if err != nil {
		return err
	}
	var variant *model.EvaluationJob
	for i := range jobs {
		if jobs[i].Variant == "segmenter_off" {
			variant = &jobs[i]
			break
		}
	}
	if variant == nil {
		return errors.New("paired segmenter_off job is missing")
	}
	if variant.Status == model.EvaluationJobSucceeded || variant.Status == model.EvaluationJobCompletedWithErrors {
		return nil
	}
	variant, ok, err := m.store.ActivateEvaluationJob(variant.ID, workerID, time.Now().UTC(), m.config.LeaseDuration)
	if err != nil {
		return err
	}
	if !ok {
		return errors.New("paired segmenter_off job is already claimed")
	}
	completed, err := m.store.CompletedEvaluationItemIDs(variant.ID)
	if err != nil {
		return err
	}
	outcomeErr := false
	for _, item := range items {
		if completed[item.DatasetItemID] {
			continue
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
		result := m.evaluateItem(ctx, variant, item)
		if result.Status == model.EvaluationResultFailed {
			outcomeErr = true
		}
		if err := m.store.SaveEvaluationResult(result, workerID, time.Now().UTC()); err != nil {
			return err
		}
	}
	_, err = m.store.FinishEvaluationJob(variant.ID, workerID, outcomeErr, "", time.Now().UTC())
	return err
}

func (m *Manager) heartbeat(ctx context.Context, cancel context.CancelCauseFunc, job *model.EvaluationJob, workerID string, done chan<- struct{}) {
	defer close(done)
	ticker := time.NewTicker(m.config.HeartbeatInterval)
	defer ticker.Stop()
	leaseExpires := time.Now().Add(m.config.LeaseDuration)
	if job.LeaseExpiresAt != nil {
		leaseExpires = *job.LeaseExpiresAt
	}
	for {
		select {
		case <-ctx.Done():
			return
		case now := <-ticker.C:
			ok, err := m.store.HeartbeatEvaluationJob(job.ID, workerID, now.UTC(), m.config.LeaseDuration)
			if err == nil && ok {
				leaseExpires = now.Add(m.config.LeaseDuration)
				continue
			}
			if err == nil || now.After(leaseExpires) {
				cancel(store.ErrLeaseLost)
				return
			}
			log.Printf("[evaluation] heartbeat failed job=%s: %v", job.ID, err)
		}
	}
}

func (m *Manager) runHeartbeat(ctx context.Context, cancel context.CancelCauseFunc, runID, workerID string, done chan<- struct{}) {
	defer close(done)
	ticker := time.NewTicker(m.config.HeartbeatInterval)
	defer ticker.Stop()
	leaseExpires := time.Now().Add(m.config.LeaseDuration)
	for {
		select {
		case <-ctx.Done():
			return
		case now := <-ticker.C:
			ok, err := m.store.HeartbeatEvaluationRun(runID, workerID, now.UTC(), m.config.LeaseDuration)
			if err == nil && ok {
				leaseExpires = now.Add(m.config.LeaseDuration)
				continue
			}
			if err == nil || now.After(leaseExpires) {
				cancel(store.ErrLeaseLost)
				return
			}
			log.Printf("[evaluation] run heartbeat failed run=%s: %v", runID, err)
		}
	}
}

func (m *Manager) retryAttempt(jobID, workerID, message string) string {
	status, changed, err := m.store.RetryEvaluationJob(jobID, workerID, message, time.Now().UTC(), m.config.RetryBase)
	if err != nil {
		log.Printf("[evaluation] retry transition failed job=%s: %v", jobID, err)
		return model.EvaluationJobFailed
	}
	if !changed {
		return model.EvaluationJobCancelled
	}
	if m.config.Metrics != nil {
		transition := "retry_scheduled"
		if status == model.EvaluationJobDeadLettered {
			transition = "dead_lettered"
		}
		m.config.Metrics.EvaluationTransition(transition, 1)
	}
	return status
}

func (m *Manager) handleInterrupted(jobID, workerID string, cause error) string {
	if errors.Is(cause, errManagerShutdown) {
		_, _ = m.store.ReleaseEvaluationJob(jobID, workerID, time.Now().UTC())
		return model.EvaluationJobQueued
	}
	if errors.Is(cause, errUserCancelled) {
		return model.EvaluationJobCancelled
	}
	if errors.Is(cause, store.ErrLeaseLost) {
		return model.EvaluationJobRunning
	}
	if cause == nil {
		cause = errors.New("evaluation worker interrupted")
	}
	return m.retryAttempt(jobID, workerID, cause.Error())
}

func (m *Manager) evaluateItem(parent context.Context, job *model.EvaluationJob, item model.EvaluationWorkItem) *model.EvaluationResult {
	started := time.Now().UTC()
	result := &model.EvaluationResult{ID: uuid.NewString(), JobID: job.ID, DatasetItemID: item.DatasetItemID,
		Sequence: item.Sequence, Status: model.EvaluationResultFailed, SegmentsJSON: "[]", StartedAt: started}
	ctx, cancel := context.WithTimeout(parent, m.config.ItemTimeout)
	defer cancel()
	if err := m.processItem(ctx, job, item, result); err != nil {
		result.ErrorMessage = err.Error()
		if job.RunID != "" && job.Variant == "segmenter_on" {
			_ = m.store.MarkEvaluationInputFailed(job.RunID, item.DatasetItemID, result.ErrorMessage, time.Now().UTC())
		}
	} else {
		result.Status = model.EvaluationResultSucceeded
	}
	result.CompletedAt = time.Now().UTC()
	if m.config.Metrics != nil {
		m.config.Metrics.EvaluationItem(string(result.Status), time.Since(started))
	}
	return result
}

func (m *Manager) processItem(ctx context.Context, job *model.EvaluationJob, item model.EvaluationWorkItem, result *model.EvaluationResult) error {
	referenceFile, err := m.objects.Open(item.TextStorageKey)
	if err != nil {
		return fmt.Errorf("open reference text: %w", err)
	}
	reference, err := io.ReadAll(io.LimitReader(referenceFile, m.config.MaxTextBytes+1))
	_ = referenceFile.Close()
	if err != nil {
		return fmt.Errorf("read reference text: %w", err)
	}
	if int64(len(reference)) > m.config.MaxTextBytes {
		return errors.New("reference text exceeds evaluation limit")
	}
	result.ReferenceText = strings.TrimSpace(string(reference))
	if len(item.ReferenceSegments) > 0 {
		parts := make([]string, 0, len(item.ReferenceSegments))
		for _, value := range item.ReferenceSegments {
			parts = append(parts, value.Text)
		}
		annotationMetrics := Compare(result.ReferenceText, joinTranscript(parts))
		if annotationMetrics.CharDistance != 0 {
			return errors.New("reference segments do not reconstruct the reference text")
		}
	}
	if job.RunID != "" {
		input, ok, err := m.store.GetEvaluationInput(job.RunID, item.DatasetItemID)
		if err != nil {
			return fmt.Errorf("load shared ASR input: %w", err)
		}
		if !ok || input.Status != "succeeded" {
			return errors.New("shared ASR input is unavailable")
		}
		var transcript struct {
			Results   []asr.Result `json:"results"`
			FinalText string       `json:"final_text"`
		}
		if err := json.Unmarshal([]byte(input.TranscriptJSON), &transcript); err != nil {
			return fmt.Errorf("decode shared ASR input: %w", err)
		}
		return m.processTranscript(job, item, result, transcript.Results)
	}

	audioFile, err := m.objects.Open(item.AudioStorageKey)
	if err != nil {
		return fmt.Errorf("open audio: %w", err)
	}
	defer audioFile.Close()
	engine := m.asrFactory(job.DatasetLanguage, job.Config)
	asrStarted := time.Now()
	asrOutcome := "error"
	asrRecorded := false
	recordASR := func() {
		if asrRecorded {
			return
		}
		asrRecorded = true
		if m.config.Metrics != nil {
			m.config.Metrics.ASR("evaluation", asrOutcome, time.Since(asrStarted))
		}
		slog.Debug("asr evaluation request completed", "component", "evaluation", "mode", "evaluation",
			"outcome", asrOutcome, "duration_ms", time.Since(asrStarted).Milliseconds())
	}
	defer recordASR()
	if engine == nil {
		return errors.New("ASR engine is unavailable")
	}
	defer engine.Close()
	audioCh := make(chan []byte, 8)
	resultCh, err := engine.Recognize(ctx, audioCh)
	if err != nil {
		return fmt.Errorf("start ASR: %w", err)
	}
	feedCtx, stopFeed := context.WithCancel(ctx)
	feedErr := make(chan error, 1)
	go func() {
		defer close(audioCh)
		feedErr <- streamAudioPCM(feedCtx, audioFile, item.AudioOriginalName, audioCh, m.config.ChunkInterval)
	}()

	stream := segmenter.New(segmenter.Config{Enabled: job.Config.SegmenterEnabled, MaxRunes: job.Config.MaxRunes,
		MaxDuration:     time.Duration(job.Config.MaxDurationMS) * time.Millisecond,
		SoftCommitDelay: time.Duration(job.Config.SoftCommitDelayMS) * time.Millisecond,
		MinRunes:        job.Config.MinRunes, SemanticEnabled: job.Config.SemanticEnabled})
	var finalASR []string
	var lastPartial string
	transcriptResults := make([]asr.Result, 0)
	segments := make([]segmenter.Segment, 0)
	for {
		select {
		case <-ctx.Done():
			asrOutcome = "cancelled"
			recordASR()
			stopFeed()
			<-feedErr
			return ctx.Err()
		case recognized, ok := <-resultCh:
			if !ok {
				asrOutcome = "success"
				recordASR()
				stopFeed()
				goto complete
			}
			if recognized.Error != "" {
				asrOutcome = "error"
				recordASR()
				stopFeed()
				<-feedErr
				return errors.New(recognized.Error)
			}
			transcriptResults = append(transcriptResults, recognized)
			lastPartial = recognized.Text
			if recognized.IsFinal {
				finalASR = append(finalASR, recognized.Text)
				lastPartial = ""
			}
			if job.Config.SegmenterEnabled {
				segments = appendFinalSegments(segments, stream.Push(segmenter.Input{Text: recognized.Text,
					IsFinal: recognized.IsFinal, BeginTimeMS: recognized.BeginTimeMS, EndTimeMS: recognized.EndTimeMS}, time.Now()))
			} else if recognized.IsFinal {
				segments = append(segments, segmenter.Segment{ID: int64(len(segments) + 1), Revision: 1,
					Text: recognized.Text, IsFinal: true, Reason: segmenter.ReasonProviderFinal})
			}
		}
	}

complete:
	if feedResult := <-feedErr; feedResult != nil && !errors.Is(feedResult, context.Canceled) {
		return fmt.Errorf("decode audio: %w", feedResult)
	}
	if job.Config.SegmenterEnabled {
		segments = appendFinalSegments(segments, stream.Flush(time.Now()))
	}
	if len(finalASR) == 0 && strings.TrimSpace(lastPartial) != "" {
		finalASR = append(finalASR, lastPartial)
		if !job.Config.SegmenterEnabled {
			segments = append(segments, segmenter.Segment{ID: int64(len(segments) + 1), Revision: 1,
				Text: lastPartial, IsFinal: true, Reason: segmenter.ReasonFlush})
		}
	}
	result.ASRText = joinTranscript(finalASR)
	if strings.TrimSpace(result.ASRText) == "" {
		asrOutcome = "error"
		recordASR()
		return errors.New("ASR returned no transcript")
	}
	segmentTexts := make([]string, 0, len(segments))
	for _, value := range segments {
		segmentTexts = append(segmentTexts, value.Text)
	}
	result.SegmentedText = joinTranscript(segmentTexts)
	result.SegmentCount = len(segments)
	encoded, err := json.Marshal(segments)
	if err != nil {
		return err
	}
	result.SegmentsJSON = string(encoded)
	asrMetrics := Compare(result.ReferenceText, result.ASRText)
	segmentedMetrics := Compare(result.ReferenceText, result.SegmentedText)
	result.ASRCharDistance, result.ASRCharUnits = asrMetrics.CharDistance, asrMetrics.CharUnits
	result.ASRWordDistance, result.ASRWordUnits = asrMetrics.WordDistance, asrMetrics.WordUnits
	result.SegmentedCharDistance, result.SegmentedCharUnits = segmentedMetrics.CharDistance, segmentedMetrics.CharUnits
	result.SegmentedWordDistance, result.SegmentedWordUnits = segmentedMetrics.WordDistance, segmentedMetrics.WordUnits
	segmentMetrics := CompareSegments(item.ReferenceSegments, segments)
	result.SegmentMatched, result.SegmentPredicted, result.SegmentReference = segmentMetrics.Matched, segmentMetrics.Predicted, segmentMetrics.Reference
	if job.RunID != "" {
		raw, _ := json.Marshal(struct {
			Results   []asr.Result `json:"results"`
			FinalText string       `json:"final_text"`
		}{transcriptResults, result.ASRText})
		hash, _ := json.Marshal(job.Config)
		digest := sha256.Sum256(hash)
		if err := m.store.SaveEvaluationInput(&model.EvaluationInput{ID: uuid.NewString(), RunID: job.RunID, DatasetItemID: item.DatasetItemID, ASRProvider: job.Config.ASRProvider, ASRConfigHash: fmt.Sprintf("%x", digest[:]), TranscriptJSON: string(raw), Status: "succeeded"}, &result.CompletedAt); err != nil {
			return fmt.Errorf("save shared ASR input: %w", err)
		}
	}
	return nil
}

func (m *Manager) processTranscript(job *model.EvaluationJob, item model.EvaluationWorkItem, result *model.EvaluationResult, transcript []asr.Result) error {
	stream := segmenter.New(segmenter.Config{Enabled: job.Config.SegmenterEnabled, MaxRunes: job.Config.MaxRunes, MaxDuration: time.Duration(job.Config.MaxDurationMS) * time.Millisecond, SoftCommitDelay: time.Duration(job.Config.SoftCommitDelayMS) * time.Millisecond, MinRunes: job.Config.MinRunes, SemanticEnabled: job.Config.SemanticEnabled})
	var finalASR []string
	var lastPartial string
	segments := make([]segmenter.Segment, 0)
	for _, recognized := range transcript {
		lastPartial = recognized.Text
		if recognized.IsFinal {
			finalASR = append(finalASR, recognized.Text)
			lastPartial = ""
		}
		if job.Config.SegmenterEnabled {
			segments = appendFinalSegments(segments, stream.Push(segmenter.Input{Text: recognized.Text, IsFinal: recognized.IsFinal, BeginTimeMS: recognized.BeginTimeMS, EndTimeMS: recognized.EndTimeMS}, time.Now()))
		} else if recognized.IsFinal {
			segments = append(segments, segmenter.Segment{ID: int64(len(segments) + 1), Revision: 1, Text: recognized.Text, IsFinal: true, Reason: segmenter.ReasonProviderFinal})
		}
	}
	if job.Config.SegmenterEnabled {
		segments = appendFinalSegments(segments, stream.Flush(time.Now()))
	}
	if len(finalASR) == 0 && strings.TrimSpace(lastPartial) != "" {
		finalASR = append(finalASR, lastPartial)
		if !job.Config.SegmenterEnabled {
			segments = append(segments, segmenter.Segment{ID: int64(len(segments) + 1), Revision: 1, Text: lastPartial, IsFinal: true, Reason: segmenter.ReasonFlush})
		}
	}
	result.ASRText = joinTranscript(finalASR)
	if result.ASRText == "" {
		return errors.New("shared ASR input returned no transcript")
	}
	texts := make([]string, 0, len(segments))
	for _, s := range segments {
		texts = append(texts, s.Text)
	}
	result.SegmentedText = joinTranscript(texts)
	result.SegmentCount = len(segments)
	raw, _ := json.Marshal(segments)
	result.SegmentsJSON = string(raw)
	a := Compare(result.ReferenceText, result.ASRText)
	b := Compare(result.ReferenceText, result.SegmentedText)
	result.ASRCharDistance, result.ASRCharUnits, result.ASRWordDistance, result.ASRWordUnits = a.CharDistance, a.CharUnits, a.WordDistance, a.WordUnits
	result.SegmentedCharDistance, result.SegmentedCharUnits, result.SegmentedWordDistance, result.SegmentedWordUnits = b.CharDistance, b.CharUnits, b.WordDistance, b.WordUnits
	sm := CompareSegments(item.ReferenceSegments, segments)
	result.SegmentMatched, result.SegmentPredicted, result.SegmentReference = sm.Matched, sm.Predicted, sm.Reference
	return nil
}

func appendFinalSegments(destination, values []segmenter.Segment) []segmenter.Segment {
	for _, value := range values {
		if value.IsFinal {
			destination = append(destination, value)
		}
	}
	return destination
}

func joinTranscript(parts []string) string {
	var builder strings.Builder
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		if builder.Len() > 0 {
			left, _ := lastRune(builder.String())
			right, _ := firstRune(part)
			if unicode.IsLetter(left) && unicode.IsLetter(right) && left <= unicode.MaxASCII && right <= unicode.MaxASCII {
				builder.WriteByte(' ')
			}
		}
		builder.WriteString(part)
	}
	return builder.String()
}

func firstRune(value string) (rune, int) {
	for _, r := range value {
		return r, 1
	}
	return 0, 0
}

func lastRune(value string) (rune, int) {
	var last rune
	for _, r := range value {
		last = r
	}
	if last == 0 {
		return 0, 0
	}
	return last, 1
}
