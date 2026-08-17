package evaluation

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
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
	Workers       int
	PollInterval  time.Duration
	ItemTimeout   time.Duration
	ChunkInterval time.Duration
	MaxTextBytes  int64
	Metrics       *observability.Metrics
}

type Manager struct {
	store      *store.SessionStore
	objects    *objectstore.Local
	asrFactory func(string, model.EvaluationConfig) asr.ASR
	config     Config
	ctx        context.Context
	cancel     context.CancelFunc
	wg         sync.WaitGroup
	activeMu   sync.Mutex
	activeJobs map[string]context.CancelFunc
}

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
	ctx, cancel := context.WithCancel(context.Background())
	return &Manager{store: database, objects: objects, asrFactory: asrFactory, config: config,
		ctx: ctx, cancel: cancel, activeJobs: make(map[string]context.CancelFunc)}
}

func (m *Manager) Start() error {
	if err := m.store.RecoverEvaluationJobs(); err != nil {
		return err
	}
	for i := 0; i < m.config.Workers; i++ {
		m.wg.Add(1)
		go m.worker(i + 1)
	}
	return nil
}

func (m *Manager) Close() {
	m.cancel()
	m.activeMu.Lock()
	for _, cancel := range m.activeJobs {
		cancel()
	}
	m.activeMu.Unlock()
	m.wg.Wait()
	_ = m.store.RecoverEvaluationJobs()
}

func (m *Manager) Cancel(jobID string) (bool, error) {
	cancelled, err := m.store.CancelEvaluationJob(jobID)
	if err != nil || !cancelled {
		return cancelled, err
	}
	m.activeMu.Lock()
	if cancel := m.activeJobs[jobID]; cancel != nil {
		cancel()
	}
	m.activeMu.Unlock()
	return true, nil
}

func (m *Manager) worker(number int) {
	defer m.wg.Done()
	ticker := time.NewTicker(m.config.PollInterval)
	defer ticker.Stop()
	for {
		job, ok, err := m.store.ClaimNextEvaluationJob()
		if err != nil {
			log.Printf("[evaluation] worker=%d claim failed: %v", number, err)
		} else if ok {
			m.runJob(job)
			continue
		}
		select {
		case <-m.ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func (m *Manager) runJob(job *model.EvaluationJob) {
	outcome := model.EvaluationJobFailed
	defer func() {
		if m.config.Metrics != nil {
			m.config.Metrics.EvaluationJob(string(outcome))
		}
	}()
	ctx, cancel := context.WithCancel(m.ctx)
	m.activeMu.Lock()
	m.activeJobs[job.ID] = cancel
	m.activeMu.Unlock()
	defer func() {
		cancel()
		m.activeMu.Lock()
		delete(m.activeJobs, job.ID)
		m.activeMu.Unlock()
	}()

	items, err := m.store.ListEvaluationWorkItems(job.DatasetID)
	if err != nil || len(items) != job.TotalItems {
		message := "dataset items changed or cannot be loaded"
		if err != nil {
			message = err.Error()
		}
		_, _ = m.store.FinishEvaluationJob(job.ID, true, message)
		return
	}
	for _, item := range items {
		if ctx.Err() != nil {
			outcome = model.EvaluationJobCancelled
			return
		}
		result := m.evaluateItem(ctx, job, item)
		if result.Status == model.EvaluationResultFailed {
			outcome = model.EvaluationJobCompletedWithErrors
		}
		if err := m.store.SaveEvaluationResult(result); err != nil {
			if !errors.Is(err, store.ErrJobNotRunning) {
				log.Printf("[evaluation] save result job=%s item=%s: %v", job.ID, item.DatasetItemID, err)
				_, _ = m.store.FinishEvaluationJob(job.ID, true, "save evaluation result failed")
			}
			return
		}
	}
	_, _ = m.store.FinishEvaluationJob(job.ID, false, "")
	if outcome != model.EvaluationJobCompletedWithErrors {
		outcome = model.EvaluationJobSucceeded
	}
}

func (m *Manager) evaluateItem(parent context.Context, job *model.EvaluationJob, item model.EvaluationWorkItem) *model.EvaluationResult {
	started := time.Now().UTC()
	result := &model.EvaluationResult{ID: uuid.NewString(), JobID: job.ID, DatasetItemID: item.DatasetItemID,
		Sequence: item.Sequence, Status: model.EvaluationResultFailed, SegmentsJSON: "[]", StartedAt: started}
	ctx, cancel := context.WithTimeout(parent, m.config.ItemTimeout)
	defer cancel()
	if err := m.processItem(ctx, job, item, result); err != nil {
		result.ErrorMessage = err.Error()
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

	audioFile, err := m.objects.Open(item.AudioStorageKey)
	if err != nil {
		return fmt.Errorf("open audio: %w", err)
	}
	defer audioFile.Close()
	engine := m.asrFactory(job.DatasetLanguage, job.Config)
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
		SoftCommitDelay: time.Duration(job.Config.SoftCommitDelayMS) * time.Millisecond})
	var finalASR []string
	var lastPartial string
	segments := make([]segmenter.Segment, 0)
	for {
		select {
		case <-ctx.Done():
			stopFeed()
			<-feedErr
			return ctx.Err()
		case recognized, ok := <-resultCh:
			if !ok {
				stopFeed()
				goto complete
			}
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
