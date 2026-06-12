package audio

import (
	"bytes"
	"encoding/binary"
	"strings"
	"testing"
)

func TestWAVDurationParsesPCMDataDuration(t *testing.T) {
	data := testWAVBytes(t, 44100, 2, 16, 44100*2*2)

	got, err := WAVDuration(bytes.NewReader(data))
	if err != nil {
		t.Fatalf("WAVDuration() error = %v", err)
	}
	if got != 1.0 {
		t.Fatalf("WAVDuration() = %.3f, want 1.000", got)
	}
}

func TestWAVDurationRejectsInvalidHeader(t *testing.T) {
	_, err := WAVDuration(bytes.NewReader([]byte("short")))
	if err == nil || !strings.Contains(err.Error(), "read RIFF header") {
		t.Fatalf("WAVDuration() error = %v, want RIFF header error", err)
	}

	_, err = WAVDuration(bytes.NewReader([]byte("RIFFxxxxNOPE")))
	if err == nil || !strings.Contains(err.Error(), "not a WAV file") {
		t.Fatalf("WAVDuration() error = %v, want not WAV error", err)
	}
}

func TestWAVDurationRejectsMissingOrIncompleteFormat(t *testing.T) {
	_, err := WAVDuration(bytes.NewReader(testWAVWithDataOnly(t)))
	if err == nil || !strings.Contains(err.Error(), "no fmt chunk") {
		t.Fatalf("WAVDuration() error = %v, want missing fmt", err)
	}

	_, err = WAVDuration(bytes.NewReader(testWAVWithSmallFmt(t)))
	if err == nil || !strings.Contains(err.Error(), "fmt chunk too small") {
		t.Fatalf("WAVDuration() error = %v, want small fmt", err)
	}
}

func testWAVBytes(t *testing.T, sampleRate uint32, channels uint16, bitsPerSample uint16, dataSize uint32) []byte {
	t.Helper()
	var buf bytes.Buffer
	buf.WriteString("RIFF")
	writeLE(t, &buf, uint32(36+dataSize))
	buf.WriteString("WAVE")
	buf.WriteString("fmt ")
	writeLE(t, &buf, uint32(16))
	writeLE(t, &buf, uint16(1))
	writeLE(t, &buf, channels)
	writeLE(t, &buf, sampleRate)
	byteRate := sampleRate * uint32(channels) * uint32(bitsPerSample) / 8
	writeLE(t, &buf, byteRate)
	blockAlign := channels * bitsPerSample / 8
	writeLE(t, &buf, blockAlign)
	writeLE(t, &buf, bitsPerSample)
	buf.WriteString("data")
	writeLE(t, &buf, dataSize)
	buf.Write(make([]byte, dataSize))
	return buf.Bytes()
}

func testWAVWithDataOnly(t *testing.T) []byte {
	t.Helper()
	var buf bytes.Buffer
	buf.WriteString("RIFF")
	writeLE(t, &buf, uint32(40))
	buf.WriteString("WAVE")
	buf.WriteString("data")
	writeLE(t, &buf, uint32(4))
	buf.Write([]byte{0, 0, 0, 0})
	return buf.Bytes()
}

func testWAVWithSmallFmt(t *testing.T) []byte {
	t.Helper()
	var buf bytes.Buffer
	buf.WriteString("RIFF")
	writeLE(t, &buf, uint32(16))
	buf.WriteString("WAVE")
	buf.WriteString("fmt ")
	writeLE(t, &buf, uint32(8))
	buf.Write(make([]byte, 8))
	return buf.Bytes()
}

func writeLE(t *testing.T, buf *bytes.Buffer, v any) {
	t.Helper()
	if err := binary.Write(buf, binary.LittleEndian, v); err != nil {
		t.Fatalf("binary write: %v", err)
	}
}
