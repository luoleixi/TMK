package evaluation

import (
	"bytes"
	"context"
	"encoding/binary"
	"testing"
)

func pcmWAV(samples []byte) []byte {
	var output bytes.Buffer
	output.WriteString("RIFF")
	_ = binary.Write(&output, binary.LittleEndian, uint32(36+len(samples)))
	output.WriteString("WAVEfmt ")
	_ = binary.Write(&output, binary.LittleEndian, uint32(16))
	_ = binary.Write(&output, binary.LittleEndian, uint16(1))
	_ = binary.Write(&output, binary.LittleEndian, uint16(1))
	_ = binary.Write(&output, binary.LittleEndian, uint32(16000))
	_ = binary.Write(&output, binary.LittleEndian, uint32(32000))
	_ = binary.Write(&output, binary.LittleEndian, uint16(2))
	_ = binary.Write(&output, binary.LittleEndian, uint16(16))
	output.WriteString("data")
	_ = binary.Write(&output, binary.LittleEndian, uint32(len(samples)))
	output.Write(samples)
	return output.Bytes()
}

func TestStreamWAVPCM(t *testing.T) {
	pcm := bytes.Repeat([]byte{1, 2}, 2000)
	chunks := make(chan []byte, 4)
	done := make(chan error, 1)
	go func() {
		defer close(chunks)
		done <- streamWAVPCM(context.Background(), bytes.NewReader(pcmWAV(pcm)), chunks, 0)
	}()
	var got bytes.Buffer
	for chunk := range chunks {
		_, _ = got.Write(chunk)
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got.Bytes(), pcm) {
		t.Fatalf("decoded %d bytes, want %d", got.Len(), len(pcm))
	}
}

func TestStreamWAVRequiresConversionForWrongRate(t *testing.T) {
	wav := pcmWAV([]byte{1, 2})
	binary.LittleEndian.PutUint32(wav[24:28], 8000)
	err := streamWAVPCM(context.Background(), bytes.NewReader(wav), make(chan []byte, 1), 0)
	if err != errNeedsFFmpeg {
		t.Fatalf("error=%v", err)
	}
}
