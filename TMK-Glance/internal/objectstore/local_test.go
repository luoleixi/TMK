package objectstore

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"testing"
)

func TestLocalPutOpenDelete(t *testing.T) {
	storage, err := NewLocal(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	content := []byte("reference transcript")
	size, digest, err := storage.Put(context.Background(), "2026/08/object.txt", bytes.NewReader(content), 1024)
	if err != nil {
		t.Fatal(err)
	}
	wantDigest := sha256.Sum256(content)
	if size != int64(len(content)) || digest != hex.EncodeToString(wantDigest[:]) {
		t.Fatalf("size=%d digest=%q", size, digest)
	}
	file, err := storage.Open("2026/08/object.txt")
	if err != nil {
		t.Fatal(err)
	}
	got, err := io.ReadAll(file)
	_ = file.Close()
	if err != nil || !bytes.Equal(got, content) {
		t.Fatalf("read content=%q err=%v", got, err)
	}
	if err := storage.Delete("2026/08/object.txt"); err != nil {
		t.Fatal(err)
	}
	if _, err := storage.Open("2026/08/object.txt"); err == nil {
		t.Fatal("deleted object can still be opened")
	}
}

func TestLocalRejectsOversizeAndEscapingKeys(t *testing.T) {
	storage, err := NewLocal(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := storage.Put(context.Background(), "large.bin", bytes.NewReader([]byte("12345")), 4); !errors.Is(err, ErrTooLarge) {
		t.Fatalf("oversize error=%v", err)
	}
	for _, key := range []string{"../outside", "/absolute", `folder\\escape`} {
		if _, _, err := storage.Put(context.Background(), key, bytes.NewReader([]byte("x")), 1); err == nil {
			t.Fatalf("unsafe key %q was accepted", key)
		}
	}
}
