package server

import (
	"context"
	"errors"
	"log"
	"time"

	"tmk-glance/internal/model"
	"tmk-glance/internal/observability"
	"tmk-glance/internal/store"
	"tmk-glance/internal/translator"

	"github.com/gin-gonic/gin"
)

const (
	partialTranslateInterval = 800 * time.Millisecond
	partialTranslateTimeout  = 3 * time.Second
	finalTranslateTimeout    = 10 * time.Second
)

type translationJob struct {
	Seq       int64
	SegmentID int64
	Revision  int64
	Text      string
	IsFinal   bool
	Reason    string
	barrier   chan struct{}
}

type translationScheduler struct {
	ctx        context.Context
	cancel     context.CancelFunc
	sessionID  string
	sourceLang string
	targetLang string
	translator translator.Translator
	store      *store.SessionStore
	send       func(any)
	partialCh  chan translationJob
	finalCh    chan translationJob
	done       chan struct{}
	metrics    *observability.Metrics
}

func newTranslationScheduler(
	parent context.Context,
	sessionID string,
	sourceLang string,
	targetLang string,
	translatorSvc translator.Translator,
	sessionStore *store.SessionStore,
	send func(any), metrics ...*observability.Metrics,
) *translationScheduler {
	ctx, cancel := context.WithCancel(parent)
	scheduler := &translationScheduler{
		ctx:        ctx,
		cancel:     cancel,
		sessionID:  sessionID,
		sourceLang: sourceLang,
		targetLang: targetLang,
		translator: translatorSvc,
		store:      sessionStore,
		send:       send,
		partialCh:  make(chan translationJob, 1),
		finalCh:    make(chan translationJob, 16),
		done:       make(chan struct{}),
	}
	if len(metrics) > 0 {
		scheduler.metrics = metrics[0]
	}
	return scheduler
}

func (s *translationScheduler) start() {
	go s.run()
}

func (s *translationScheduler) stop() {
	s.cancel()
	<-s.done
}

func (s *translationScheduler) submit(job translationJob) {
	if job.barrier != nil {
		select {
		case s.finalCh <- job:
		case <-s.ctx.Done():
			close(job.barrier)
		}
		return
	}
	if job.Text == "" {
		return
	}
	if job.IsFinal {
		select {
		case s.finalCh <- job:
		case <-s.ctx.Done():
		}
		return
	}

	select {
	case s.partialCh <- job:
		return
	default:
	}
	select {
	case <-s.partialCh:
	default:
	}
	select {
	case s.partialCh <- job:
	case <-s.ctx.Done():
	default:
		log.Printf("[translate] drop stale partial translation, session=%s seq=%d", s.sessionID, job.Seq)
	}
}

func (s *translationScheduler) drainFinals(timeout time.Duration) bool {
	barrier := make(chan struct{})
	s.submit(translationJob{IsFinal: true, barrier: barrier})
	select {
	case <-barrier:
		return true
	case <-time.After(timeout):
		return false
	}
}

func (s *translationScheduler) run() {
	defer close(s.done)

	ticker := time.NewTicker(partialTranslateInterval)
	defer ticker.Stop()

	var pendingPartial *translationJob
	for {
		select {
		case finalJob := <-s.finalCh:
			pendingPartial = nil
			s.drainPartials()
			if finalJob.barrier != nil {
				close(finalJob.barrier)
				continue
			}
			s.translate(finalJob)
			continue
		default:
		}

		select {
		case <-s.ctx.Done():
			return
		case finalJob := <-s.finalCh:
			pendingPartial = nil
			s.drainPartials()
			if finalJob.barrier != nil {
				close(finalJob.barrier)
				continue
			}
			s.translate(finalJob)
		case partialJob := <-s.partialCh:
			pendingPartial = &partialJob
		case <-ticker.C:
			if pendingPartial == nil {
				continue
			}
			job := *pendingPartial
			pendingPartial = nil
			s.translate(job)
		}
	}
}

func (s *translationScheduler) drainPartials() {
	for {
		select {
		case <-s.partialCh:
		default:
			return
		}
	}
}

func (s *translationScheduler) translate(job translationJob) {
	started := time.Now()
	timeout := partialTranslateTimeout
	if job.IsFinal {
		timeout = finalTranslateTimeout
	}

	ctx, cancel := context.WithTimeout(s.ctx, timeout)
	defer cancel()

	translated, err := s.translator.Translate(ctx, s.sourceLang, s.targetLang, job.Text)
	mode := "partial"
	if job.IsFinal {
		mode = "final"
	}
	if err != nil {
		if errors.Is(err, context.Canceled) && s.ctx.Err() != nil {
			return
		}
		log.Printf("[translate] fallback to source text, session=%s seq=%d final=%v err=%v", s.sessionID, job.Seq, job.IsFinal, err)
		translated = job.Text
	}
	if s.metrics != nil {
		outcome := "success"
		if err != nil {
			outcome = "fallback"
		}
		s.metrics.Translation(mode, outcome, time.Since(started))
	}

	if s.ctx.Err() != nil {
		return
	}

	payload := gin.H{
		"type":       "translation",
		"seq":        job.Seq,
		"segment_id": job.SegmentID,
		"revision":   job.Revision,
		"text":       translated,
		"is_final":   job.IsFinal,
		"reason":     job.Reason,
		"timestamp":  time.Now().UnixMilli(),
	}
	if err != nil {
		payload["warning"] = "translate_failed_fallback_to_source"
	}
	s.send(payload)

	if job.IsFinal {
		if err := s.store.AddRecord(s.sessionID, model.Record{
			SessionID:      s.sessionID,
			SourceText:     job.Text,
			TranslatedText: translated,
			CreatedAt:      time.Now(),
		}); err != nil {
			log.Printf("[db] add record failed: %v", err)
			s.send(gin.H{"type": "error", "message": "database error"})
		}
	}
}
