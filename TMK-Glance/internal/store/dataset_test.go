package store

import (
	"errors"
	"path/filepath"
	"testing"
	"time"

	"tmk-glance/internal/model"
)

func TestDatasetLifecycleAndObjectReferences(t *testing.T) {
	database, err := NewSessionStore(DriverSQLite, filepath.Join(t.TempDir(), "dataset.db"), "")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })
	now := time.Now().UTC()
	user := &model.User{ID: "admin", Email: "admin@example.com", DisplayName: "Admin", PasswordHash: "unused",
		Role: model.RoleAdmin, Status: model.UserStatusActive, CreatedAt: now, UpdatedAt: now}
	if err := database.CreateUser(user); err != nil {
		t.Fatal(err)
	}
	audio := &model.StorageObject{ID: "audio", OwnerUserID: user.ID, Kind: model.ObjectKindAudio, OriginalName: "sample.wav",
		StorageKey: "2026/08/audio.wav", ContentType: "audio/wav", SizeBytes: 12, SHA256: "audio-digest", Status: model.ObjectStatusReady, CreatedAt: now}
	text := &model.StorageObject{ID: "text", OwnerUserID: user.ID, Kind: model.ObjectKindText, OriginalName: "sample.txt",
		StorageKey: "2026/08/text.txt", ContentType: "text/plain", SizeBytes: 10, SHA256: "text-digest", Status: model.ObjectStatusReady, CreatedAt: now}
	for _, object := range []*model.StorageObject{audio, text} {
		if err := database.CreateStorageObject(object); err != nil {
			t.Fatal(err)
		}
	}
	dataset := &model.Dataset{ID: "dataset", Name: "Mandarin evaluation", Language: "zh", Status: model.DatasetStatusDraft,
		Revision: 1, CreatedBy: user.ID, CreatedAt: now, UpdatedAt: now}
	if err := database.CreateDataset(dataset); err != nil {
		t.Fatal(err)
	}
	item := &model.DatasetItem{ID: "item", DatasetID: dataset.ID, AudioObjectID: audio.ID,
		ReferenceTextObjectID: text.ID, CreatedBy: user.ID, CreatedAt: now}
	if err := database.AddDatasetItem(item); err != nil {
		t.Fatal(err)
	}
	if item.Sequence != 1 || item.AudioOriginalName != audio.OriginalName || item.TextOriginalName != text.OriginalName {
		t.Fatalf("unexpected populated item: %+v", item)
	}
	duplicate := *item
	duplicate.ID = "duplicate"
	if err := database.AddDatasetItem(&duplicate); !errors.Is(err, ErrDuplicateAudio) {
		t.Fatalf("duplicate audio error=%v", err)
	}
	if _, _, err := database.BeginDeleteStorageObject(audio.ID); !errors.Is(err, ErrObjectInUse) {
		t.Fatalf("referenced object deletion error=%v", err)
	}
	updated, err := database.MarkDatasetReady(dataset.ID)
	if err != nil || !updated {
		t.Fatalf("mark ready updated=%v err=%v", updated, err)
	}
	if changed, err := database.UpdateDatasetDraft(dataset.ID, "changed", "", "zh"); err != nil || changed {
		t.Fatalf("ready dataset changed=%v err=%v", changed, err)
	}
	got, ok, err := database.GetDataset(dataset.ID)
	if err != nil || !ok || got.Status != model.DatasetStatusReady || got.ItemCount != 1 || got.Revision != 3 {
		t.Fatalf("dataset=%+v ok=%v err=%v", got, ok, err)
	}
	if archived, err := database.ArchiveDataset(dataset.ID); err != nil || !archived {
		t.Fatalf("archive updated=%v err=%v", archived, err)
	}
}

func TestStorageObjectDeletionCanResume(t *testing.T) {
	database, err := NewSessionStore(DriverSQLite, filepath.Join(t.TempDir(), "delete.db"), "")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })
	now := time.Now().UTC()
	user := &model.User{ID: "admin", Email: "admin@example.com", PasswordHash: "unused", Role: model.RoleAdmin,
		Status: model.UserStatusActive, CreatedAt: now, UpdatedAt: now}
	if err := database.CreateUser(user); err != nil {
		t.Fatal(err)
	}
	object := &model.StorageObject{ID: "object", OwnerUserID: user.ID, Kind: model.ObjectKindText, OriginalName: "a.txt",
		StorageKey: "a.txt", ContentType: "text/plain", SizeBytes: 1, SHA256: "digest", Status: model.ObjectStatusReady, CreatedAt: now}
	if err := database.CreateStorageObject(object); err != nil {
		t.Fatal(err)
	}
	if _, ok, err := database.BeginDeleteStorageObject(object.ID); err != nil || !ok {
		t.Fatalf("first begin delete ok=%v err=%v", ok, err)
	}
	if _, ok, err := database.BeginDeleteStorageObject(object.ID); err != nil || !ok {
		t.Fatalf("resumed begin delete ok=%v err=%v", ok, err)
	}
	if err := database.DeleteStorageObjectRecord(object.ID); err != nil {
		t.Fatal(err)
	}
}
