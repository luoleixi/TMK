package server

import (
	"context"
	"encoding/json"
	"log"
	"sync/atomic"
	"time"

	"tmk-glance/internal/asr"
	"tmk-glance/internal/observability"
	"tmk-glance/internal/segmenter"
	"tmk-glance/internal/store"
	"tmk-glance/internal/translator"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
)

const (
	audioQueueSize = 32
	sendQueueSize  = 128
	wsWriteWait    = 10 * time.Second
	wsPongWait     = 60 * time.Second
	wsPingPeriod   = (wsPongWait * 9) / 10
	wsReadLimit    = 1 << 20
)

type sessionActor struct {
	conn          *websocket.Conn
	sessionID     string
	store         *store.SessionStore
	translatorSvc translator.Translator
	asrFactory    func(string) asr.ASR
	queueBrief    func(string)

	asrEngine       asr.ASR
	asrCtx          context.Context
	asrCancel       context.CancelFunc
	scheduler       *translationScheduler
	audioCh         chan []byte
	segmenterConfig segmenter.Config
	pipelineDone    chan struct{}

	sendCh           chan any
	done             chan struct{}
	audioCount       atomic.Int64
	droppedAudioCnt  atomic.Int64
	droppedSendCount atomic.Int64
	metrics          *observability.Metrics
	seq              int64
}

// providerStream preserves provider sentence boundaries when local segmentation
// is disabled. It does not split text; it only adapts cumulative ASR events to
// the existing segment protocol.
type providerStream struct {
	segmentID int64
	revision  int64
	text      string
}

func newProviderStream() *providerStream { return &providerStream{segmentID: 1} }

func (p *providerStream) push(result asr.Result) []segmenter.Segment {
	if result.Text == "" {
		return nil
	}
	p.revision++
	segment := segmenter.Segment{
		ID: p.segmentID, Revision: p.revision, Text: result.Text,
		IsFinal: result.IsFinal, Reason: segmenter.ReasonPartial,
	}
	p.text = result.Text
	if result.IsFinal {
		segment.Reason = segmenter.ReasonProviderFinal
		p.segmentID++
		p.revision = 0
		p.text = ""
	}
	return []segmenter.Segment{segment}
}

func (p *providerStream) flush() []segmenter.Segment {
	if p.text == "" {
		return nil
	}
	p.revision++
	segment := segmenter.Segment{
		ID: p.segmentID, Revision: p.revision, Text: p.text,
		IsFinal: true, Reason: segmenter.ReasonFlush,
	}
	p.segmentID++
	p.revision = 0
	p.text = ""
	return []segmenter.Segment{segment}
}

func newSessionActor(conn *websocket.Conn, sessionID string, sessionStore *store.SessionStore, translatorSvc translator.Translator, segmenterConfig segmenter.Config, asrFactory func(string) asr.ASR, queueBrief func(string), metrics ...*observability.Metrics) *sessionActor {
	actor := &sessionActor{
		conn:            conn,
		sessionID:       sessionID,
		store:           sessionStore,
		translatorSvc:   translatorSvc,
		asrFactory:      asrFactory,
		queueBrief:      queueBrief,
		segmenterConfig: segmenterConfig,
		asrCancel:       func() {},
		sendCh:          make(chan any, sendQueueSize),
		done:            make(chan struct{}),
	}
	if len(metrics) > 0 {
		actor.metrics = metrics[0]
	}
	return actor
}

func (a *sessionActor) run() {
	writerDone := make(chan struct{})
	go a.writePump(writerDone)

	a.readPump()
	a.cleanup()
	close(a.done)
	<-writerDone
}

func (a *sessionActor) readPump() {
	a.conn.SetReadLimit(wsReadLimit)
	a.conn.SetReadDeadline(time.Now().Add(wsPongWait))
	a.conn.SetPongHandler(func(string) error {
		a.conn.SetReadDeadline(time.Now().Add(wsPongWait))
		return nil
	})
	for {
		msgType, msg, err := a.conn.ReadMessage()
		if err != nil {
			a.asrCancel()
			return
		}

		if msgType == websocket.BinaryMessage {
			a.handleAudio(msg)
			continue
		}

		var wsMsg struct {
			Type       string `json:"type"`
			SourceLang string `json:"source_lang"`
			TargetLang string `json:"target_lang"`
		}
		if err := json.Unmarshal(msg, &wsMsg); err != nil {
			a.writeJSON(gin.H{"type": "error", "message": "invalid websocket message"})
			continue
		}

		if !a.handleControl(wsMsg.Type) {
			return
		}
	}
}

func (a *sessionActor) cleanup() {
	a.stopPipeline()
	if ended, err := a.store.End(a.sessionID); err != nil {
		log.Printf("[db] end session failed: %v", err)
	} else if ended {
		if a.queueBrief != nil {
			a.queueBrief(a.sessionID)
		}
	}
}

func (a *sessionActor) handleAudio(msg []byte) {
	if a.audioCh == nil {
		return
	}
	select {
	case a.audioCh <- msg:
		if a.metrics != nil {
			a.metrics.AudioChunk("accepted")
		}
		count := a.audioCount.Add(1)
		if count%50 == 1 {
			log.Printf("[ws] received %d audio chunks, session=%s", count, a.sessionID)
		}
	default:
		if a.metrics != nil {
			a.metrics.AudioChunk("dropped")
		}
		count := a.droppedAudioCnt.Add(1)
		if count%50 == 1 {
			log.Printf("[ws] drop audio chunk, session=%s dropped=%d", a.sessionID, count)
		}
	}
}

func (a *sessionActor) handleControl(msgType string) bool {
	switch msgType {
	case "start":
		a.startInterpret()
	case "audio":
		a.writeJSON(gin.H{
			"type":    "error",
			"message": "audio must be sent as binary PCM frames, not JSON text",
		})
	case "ping":
		a.writeJSON(gin.H{"type": "pong", "timestamp_ms": time.Now().UnixMilli()})
	case "stop":
		a.stopPipeline()
		a.writeJSON(gin.H{"type": "stopped", "timestamp_ms": time.Now().UnixMilli()})
		return false
	default:
		a.writeJSON(gin.H{"type": "error", "message": "unknown message type"})
	}
	return true
}

func (a *sessionActor) startInterpret() {
	ok, err := a.store.Activate(a.sessionID)
	if err != nil {
		log.Printf("[db] activate session failed: %v", err)
		a.writeJSON(gin.H{"type": "error", "message": "database error"})
		return
	}
	if !ok {
		a.writeJSON(gin.H{"type": "error", "message": "session not ready"})
		return
	}

	ses, _, err := a.store.Get(a.sessionID)
	if err != nil {
		log.Printf("[db] get active session failed: %v", err)
		a.writeJSON(gin.H{"type": "error", "message": "database error"})
		return
	}

	a.asrCtx, a.asrCancel = context.WithCancel(context.Background())
	a.asrEngine = a.asrFactory(ses.SourceLang)
	a.audioCh = make(chan []byte, audioQueueSize)

	resultCh, err := a.asrEngine.Recognize(a.asrCtx, a.audioCh)
	if err != nil {
		if _, dbErr := a.store.Fail(a.sessionID); dbErr != nil {
			log.Printf("[db] fail session failed: %v", dbErr)
		}
		a.writeJSON(gin.H{"type": "error", "message": err.Error()})
		return
	}

	a.scheduler = newTranslationScheduler(context.Background(), a.sessionID, ses.SourceLang, ses.TargetLang, a.translatorSvc, a.store, a.writeJSON, a.metrics)
	a.scheduler.start()
	a.pipelineDone = make(chan struct{})

	a.writeJSON(gin.H{"type": "started", "timestamp_ms": time.Now().UnixMilli()})

	scheduler := a.scheduler
	done := a.pipelineDone
	go a.consumeASRResults(resultCh, scheduler, done)
}

func (a *sessionActor) consumeASRResults(resultCh <-chan asr.Result, scheduler *translationScheduler, done chan<- struct{}) {
	defer close(done)
	stream := segmenter.New(a.segmenterConfig)
	provider := newProviderStream()
	ticker := time.NewTicker(50 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case result, ok := <-resultCh:
			now := time.Now()
			if !ok {
				if a.segmenterConfig.Enabled {
					a.publishSegments(stream.Flush(now), scheduler)
				} else {
					a.publishSegments(provider.flush(), scheduler)
				}
				return
			}
			if a.segmenterConfig.Enabled {
				a.publishSegments(stream.Push(segmenter.Input{
					Text: result.Text, IsFinal: result.IsFinal,
					BeginTimeMS: result.BeginTimeMS, EndTimeMS: result.EndTimeMS,
				}, now), scheduler)
			} else {
				a.publishSegments(provider.push(result), scheduler)
			}
		case now := <-ticker.C:
			if a.segmenterConfig.Enabled {
				a.publishSegments(stream.Tick(now), scheduler)
			}
		case <-a.asrCtx.Done():
			if a.segmenterConfig.Enabled {
				a.publishSegments(stream.Flush(time.Now()), scheduler)
			} else {
				a.publishSegments(provider.flush(), scheduler)
			}
			return
		}
	}
}

func (a *sessionActor) publishSegments(segments []segmenter.Segment, scheduler *translationScheduler) {
	for _, result := range segments {
		a.seq++
		seq := a.seq
		a.writeJSON(gin.H{
			"type":       "transcript",
			"seq":        seq,
			"segment_id": result.ID,
			"revision":   result.Revision,
			"text":       result.Text,
			"is_final":   result.IsFinal,
			"reason":     result.Reason,
			"timestamp":  time.Now().UnixMilli(),
		})
		scheduler.submit(translationJob{
			Seq: seq, SegmentID: result.ID, Revision: result.Revision,
			Text: result.Text, IsFinal: result.IsFinal, Reason: result.Reason,
		})
	}
}

func (a *sessionActor) stopPipeline() {
	if a.asrEngine == nil && a.scheduler == nil {
		return
	}

	a.asrCancel()
	if a.asrEngine != nil {
		if err := a.asrEngine.Close(); err != nil {
			log.Printf("[asr] close failed, session=%s err=%v", a.sessionID, err)
		}
		a.asrEngine = nil
	}
	if a.pipelineDone != nil {
		select {
		case <-a.pipelineDone:
		case <-time.After(time.Second):
			log.Printf("[segmenter] flush timeout, session=%s", a.sessionID)
		}
		a.pipelineDone = nil
	}
	if a.scheduler != nil {
		if !a.scheduler.drainFinals(15 * time.Second) {
			log.Printf("[translate] final drain timeout, session=%s", a.sessionID)
		}
		a.scheduler.stop()
		a.scheduler = nil
	}
	a.audioCh = nil
}

func (a *sessionActor) writeJSON(v any) {
	select {
	case a.sendCh <- v:
	case <-a.done:
	default:
		count := a.droppedSendCount.Add(1)
		if count%50 == 1 {
			log.Printf("[ws] drop outbound message, session=%s dropped=%d", a.sessionID, count)
		}
	}
}

func (a *sessionActor) writePump(done chan<- struct{}) {
	defer close(done)

	ticker := time.NewTicker(wsPingPeriod)
	defer ticker.Stop()

	for {
		select {
		case msg := <-a.sendCh:
			if !a.writeQueuedJSON(msg) {
				return
			}
		case <-ticker.C:
			a.conn.SetWriteDeadline(time.Now().Add(wsWriteWait))
			if err := a.conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				log.Printf("[ws] ping failed, session=%s err=%v", a.sessionID, err)
				return
			}
		case <-a.done:
			a.drainSendQueue()
			return
		}
	}
}

func (a *sessionActor) drainSendQueue() {
	for {
		select {
		case msg := <-a.sendCh:
			if !a.writeQueuedJSON(msg) {
				return
			}
		default:
			return
		}
	}
}

func (a *sessionActor) writeQueuedJSON(msg any) bool {
	a.conn.SetWriteDeadline(time.Now().Add(wsWriteWait))
	if err := a.conn.WriteJSON(msg); err != nil {
		log.Printf("[ws] write failed, session=%s err=%v", a.sessionID, err)
		return false
	}
	return true
}
