package server

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"tmk-glance/internal/model"
	"tmk-glance/internal/store"

	"github.com/gin-gonic/gin"
)

type schedulerTranslator struct{}

func (schedulerTranslator) Translate(_ context.Context, _, _, text string) (string, error) {
	return "translated:" + text, nil
}

func (schedulerTranslator) Generate(_ context.Context, _, text string) (string, error) {
	return text, nil
}

func TestTranslationSchedulerDrainsFinalsAndPreservesSegmentMetadata(t *testing.T) {
	sessionStore, err := store.NewSessionStore(store.DriverSQLite, filepath.Join(t.TempDir(), "scheduler.db"), "")
	if err != nil {
		t.Fatalf("create store: %v", err)
	}
	t.Cleanup(func() { _ = sessionStore.Close() })

	sessionID := "scheduler-test"
	if err := sessionStore.Create(&model.Session{
		ID: sessionID, SourceLang: "zh", TargetLang: "en", InputType: "system_audio",
		Status: "active", CreatedAt: time.Now(),
	}); err != nil {
		t.Fatalf("create session: %v", err)
	}

	events := make(chan any, 4)
	scheduler := newTranslationScheduler(context.Background(), sessionID, "zh", "en", schedulerTranslator{}, sessionStore, func(event any) {
		events <- event
	})
	scheduler.start()
	scheduler.submit(translationJob{Seq: 7, SegmentID: 3, Revision: 2, Text: "final text", IsFinal: true, Reason: "max_length"})

	if !scheduler.drainFinals(2 * time.Second) {
		t.Fatal("final queue did not drain")
	}
	scheduler.stop()

	records, _, err := sessionStore.Records(sessionID)
	if err != nil {
		t.Fatalf("load records: %v", err)
	}
	if len(records) != 1 || records[0].SourceText != "final text" || records[0].TranslatedText != "translated:final text" {
		t.Fatalf("unexpected persisted records: %+v", records)
	}

	select {
	case event := <-events:
		payload, ok := event.(gin.H)
		if !ok {
			t.Fatalf("unexpected payload type: %T", event)
		}
		if payload["segment_id"] != int64(3) || payload["revision"] != int64(2) || payload["reason"] != "max_length" {
			t.Fatalf("segment metadata missing from payload: %+v", payload)
		}
	default:
		t.Fatal("translation event was not emitted")
	}
}
