package evaluation

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

const pcmChunkBytes = 3200

var errNeedsFFmpeg = errors.New("audio requires ffmpeg conversion")

func streamAudioPCM(ctx context.Context, file *os.File, originalName string, output chan<- []byte, interval time.Duration) error {
	if strings.EqualFold(filepath.Ext(originalName), ".wav") {
		if err := streamWAVPCM(ctx, file, output, interval); err == nil {
			return nil
		} else if !errors.Is(err, errNeedsFFmpeg) {
			return err
		}
		if _, err := file.Seek(0, io.SeekStart); err != nil {
			return err
		}
	}
	return streamFFmpegPCM(ctx, file.Name(), output, interval)
}

func streamWAVPCM(ctx context.Context, file io.ReadSeeker, output chan<- []byte, interval time.Duration) error {
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		return err
	}
	header := make([]byte, 12)
	if _, err := io.ReadFull(file, header); err != nil {
		return fmt.Errorf("read WAV header: %w", err)
	}
	if string(header[:4]) != "RIFF" || string(header[8:]) != "WAVE" {
		return errors.New("invalid WAV container")
	}
	var formatOK bool
	for {
		chunkHeader := make([]byte, 8)
		if _, err := io.ReadFull(file, chunkHeader); err != nil {
			return fmt.Errorf("find WAV data: %w", err)
		}
		size := int64(binary.LittleEndian.Uint32(chunkHeader[4:]))
		switch string(chunkHeader[:4]) {
		case "fmt ":
			if size < 16 || size > 1<<20 {
				return errors.New("invalid WAV format chunk")
			}
			format := make([]byte, size)
			if _, err := io.ReadFull(file, format); err != nil {
				return err
			}
			formatOK = binary.LittleEndian.Uint16(format[0:2]) == 1 &&
				binary.LittleEndian.Uint16(format[2:4]) == 1 &&
				binary.LittleEndian.Uint32(format[4:8]) == 16000 &&
				binary.LittleEndian.Uint16(format[14:16]) == 16
		case "data":
			if !formatOK {
				return errNeedsFFmpeg
			}
			if size%2 != 0 {
				return errors.New("invalid 16-bit PCM data size")
			}
			return streamPCMReader(ctx, io.LimitReader(file, size), output, interval)
		default:
			if _, err := file.Seek(size, io.SeekCurrent); err != nil {
				return err
			}
		}
		if size%2 == 1 {
			if _, err := file.Seek(1, io.SeekCurrent); err != nil {
				return err
			}
		}
	}
}

func streamFFmpegPCM(ctx context.Context, path string, output chan<- []byte, interval time.Duration) error {
	binaryPath, err := exec.LookPath("ffmpeg")
	if err != nil {
		return errors.New("ffmpeg is required for compressed or non-16kHz PCM audio")
	}
	command := exec.CommandContext(ctx, binaryPath, "-nostdin", "-v", "error", "-i", path,
		"-f", "s16le", "-acodec", "pcm_s16le", "-ac", "1", "-ar", "16000", "pipe:1")
	stdout, err := command.StdoutPipe()
	if err != nil {
		return err
	}
	var stderr limitedBuffer
	command.Stderr = &stderr
	if err := command.Start(); err != nil {
		return err
	}
	streamErr := streamPCMReader(ctx, stdout, output, interval)
	waitErr := command.Wait()
	if streamErr != nil {
		return streamErr
	}
	if waitErr != nil {
		return fmt.Errorf("ffmpeg conversion failed: %s", strings.TrimSpace(stderr.String()))
	}
	return nil
}

func streamPCMReader(ctx context.Context, reader io.Reader, output chan<- []byte, interval time.Duration) error {
	buffer := make([]byte, pcmChunkBytes)
	for {
		read, err := io.ReadFull(reader, buffer)
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil && !errors.Is(err, io.ErrUnexpectedEOF) {
			return err
		}
		if read > 0 {
			chunk := append([]byte(nil), buffer[:read]...)
			select {
			case output <- chunk:
			case <-ctx.Done():
				return ctx.Err()
			}
			if interval > 0 {
				timer := time.NewTimer(interval)
				select {
				case <-timer.C:
				case <-ctx.Done():
					if !timer.Stop() {
						select {
						case <-timer.C:
						default:
						}
					}
					return ctx.Err()
				}
			}
		}
		if errors.Is(err, io.ErrUnexpectedEOF) {
			return nil
		}
	}
}

type limitedBuffer struct{ bytes.Buffer }

func (b *limitedBuffer) Write(value []byte) (int, error) {
	original := len(value)
	if b.Len() < 4096 {
		remaining := 4096 - b.Len()
		if len(value) > remaining {
			value = value[:remaining]
		}
		_, _ = b.Buffer.Write(value)
	}
	return original, nil
}
