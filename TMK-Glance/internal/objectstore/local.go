package objectstore

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

var ErrTooLarge = errors.New("object exceeds size limit")

type Local struct {
	root string
}

func NewLocal(root string) (*Local, error) {
	if strings.TrimSpace(root) == "" {
		return nil, errors.New("local object storage requires a root directory")
	}
	absolute, err := filepath.Abs(root)
	if err != nil {
		return nil, fmt.Errorf("resolve object storage root: %w", err)
	}
	if err := os.MkdirAll(filepath.Join(absolute, ".tmp"), 0o750); err != nil {
		return nil, fmt.Errorf("create object storage root: %w", err)
	}
	return &Local{root: absolute}, nil
}

func (s *Local) Put(ctx context.Context, key string, source io.Reader, maxBytes int64) (int64, string, error) {
	if maxBytes < 1 {
		return 0, "", errors.New("invalid object size limit")
	}
	target, err := s.resolve(key)
	if err != nil {
		return 0, "", err
	}
	if err := os.MkdirAll(filepath.Dir(target), 0o750); err != nil {
		return 0, "", err
	}
	temporary, err := os.CreateTemp(filepath.Join(s.root, ".tmp"), "upload-*")
	if err != nil {
		return 0, "", err
	}
	temporaryName := temporary.Name()
	committed := false
	defer func() {
		_ = temporary.Close()
		if !committed {
			_ = os.Remove(temporaryName)
		}
	}()

	hash := sha256.New()
	written, err := copyWithContext(ctx, io.MultiWriter(temporary, hash), io.LimitReader(source, maxBytes+1))
	if err != nil {
		return 0, "", err
	}
	if written > maxBytes {
		return 0, "", ErrTooLarge
	}
	if err := temporary.Sync(); err != nil {
		return 0, "", err
	}
	if err := temporary.Close(); err != nil {
		return 0, "", err
	}
	if err := os.Chmod(temporaryName, 0o640); err != nil {
		return 0, "", err
	}
	if _, err := os.Stat(target); err == nil {
		return 0, "", errors.New("object key already exists")
	} else if !os.IsNotExist(err) {
		return 0, "", err
	}
	if err := os.Rename(temporaryName, target); err != nil {
		return 0, "", err
	}
	committed = true
	return written, hex.EncodeToString(hash.Sum(nil)), nil
}

func (s *Local) Open(key string) (*os.File, error) {
	path, err := s.resolve(key)
	if err != nil {
		return nil, err
	}
	return os.Open(path)
}

func (s *Local) Delete(key string) error {
	path, err := s.resolve(key)
	if err != nil {
		return err
	}
	err = os.Remove(path)
	if os.IsNotExist(err) {
		return nil
	}
	return err
}

func (s *Local) resolve(key string) (string, error) {
	key = strings.TrimSpace(key)
	if key == "" || strings.HasPrefix(key, "/") || strings.Contains(key, "\\") {
		return "", errors.New("invalid object key")
	}
	key = filepath.ToSlash(key)
	clean := filepath.Clean(filepath.FromSlash(key))
	if clean == "." || clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
		return "", errors.New("invalid object key")
	}
	resolved := filepath.Join(s.root, clean)
	relative, err := filepath.Rel(s.root, resolved)
	if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return "", errors.New("object key escapes storage root")
	}
	return resolved, nil
}

func copyWithContext(ctx context.Context, destination io.Writer, source io.Reader) (int64, error) {
	buffer := make([]byte, 64<<10)
	var total int64
	for {
		if err := ctx.Err(); err != nil {
			return total, err
		}
		read, readErr := source.Read(buffer)
		if read > 0 {
			written, writeErr := destination.Write(buffer[:read])
			total += int64(written)
			if writeErr != nil {
				return total, writeErr
			}
			if written != read {
				return total, io.ErrShortWrite
			}
		}
		if errors.Is(readErr, io.EOF) {
			return total, nil
		}
		if readErr != nil {
			return total, readErr
		}
	}
}
