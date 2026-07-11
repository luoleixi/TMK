package server

import (
	"context"
	"errors"
	"log"
	"time"

	"tmk-glance/internal/model"
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
	Seq     int64
	Text    string
	IsFinal bool
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
}

func newTranslationScheduler(
	parent context.Context,
	sessionID string,
	sourceLang string,
	targetLang string,
	translatorSvc translator.Translator,
	sessionStore *store.SessionStore,
	send func(any),
) *translationScheduler {
	ctx, cancel := context.WithCancel(parent)
	return &translationScheduler{
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
}

func (s *translationScheduler) start() {
	go s.run()
}

func (s *translationScheduler) stop() {
	s.cancel()
	<-s.done
}

func (s *translationScheduler) submit(job translationJob) {
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
	timeout := partialTranslateTimeout
	if job.IsFinal {
		timeout = finalTranslateTimeout
	}

	ctx, cancel := context.WithTimeout(s.ctx, timeout)
	defer cancel()

	translated, err := s.translator.Translate(ctx, s.sourceLang, s.targetLang, job.Text)
	if err != nil {
		if errors.Is(err, context.Canceled) && s.ctx.Err() != nil {
			return
		}
		log.Printf("[translate] fallback to source text, session=%s seq=%d final=%v err=%v", s.sessionID, job.Seq, job.IsFinal, err)
		translated = job.Text
	}

	if s.ctx.Err() != nil {
		return
	}

	payload := gin.H{
		"type":      "translation",
		"seq":       job.Seq,
		"text":      translated,
		"is_final":  job.IsFinal,
		"timestamp": time.Now().UnixMilli(),
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
