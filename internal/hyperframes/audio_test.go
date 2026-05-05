package hyperframes

import (
	"strings"
	"testing"
)

func TestBuildAudioFilterComplex_noAudio(t *testing.T) {
	inputs, filter, label := buildAudioFilterComplex(nil, "", audioDirectives{
		BGMFadeInSec: 2, BGMFadeOutSec: 3, BGMVolume: 0.6, DuckingDepth: 0.15,
	}, 9.0)
	if len(inputs) != 0 {
		t.Errorf("expected no inputs, got %v", inputs)
	}
	if filter != "" {
		t.Errorf("expected empty filter, got %q", filter)
	}
	if label != "" {
		t.Errorf("expected empty label, got %q", label)
	}
}

func TestBuildAudioFilterComplex_ttsOnly(t *testing.T) {
	tracks := []audioTrack{
		{path: "/a/tts0.mp3", startSec: 0, endSec: 3},
		{path: "/a/tts1.mp3", startSec: 3, endSec: 6},
	}
	inputs, filter, label := buildAudioFilterComplex(tracks, "", audioDirectives{
		BGMVolume: 0.6,
	}, 6.0)

	if len(inputs) != 4 { // 2 tracks × ("-i", path)
		t.Errorf("expected 4 input args, got %d: %v", len(inputs), inputs)
	}
	if !strings.Contains(filter, "adelay=0|0") {
		t.Errorf("expected adelay=0|0 for track 0, filter=%q", filter)
	}
	if !strings.Contains(filter, "adelay=3000|3000") {
		t.Errorf("expected adelay=3000|3000 for track 1, filter=%q", filter)
	}
	if !strings.Contains(filter, "amix=inputs=2") {
		t.Errorf("expected amix=inputs=2, filter=%q", filter)
	}
	if label != "[tts_mix]" {
		t.Errorf("expected [tts_mix], got %q", label)
	}
}

func TestBuildAudioFilterComplex_bgmOnly(t *testing.T) {
	inputs, filter, label := buildAudioFilterComplex(nil, "/bgm.mp3", audioDirectives{
		BGMFadeInSec: 2, BGMFadeOutSec: 3, BGMVolume: 0.6, DuckingDepth: 0.15,
	}, 10.0)

	if len(inputs) != 2 { // ("-i", "/bgm.mp3")
		t.Errorf("expected 2 input args, got %d", len(inputs))
	}
	if !strings.Contains(filter, "aloop") {
		t.Errorf("expected aloop in filter, got %q", filter)
	}
	if !strings.Contains(filter, "afade=t=in") {
		t.Errorf("expected afade fade-in, got %q", filter)
	}
	if label != "[bgm_out]" {
		t.Errorf("expected [bgm_out], got %q", label)
	}
}

func TestBuildAudioFilterComplex_mixedDucking(t *testing.T) {
	tracks := []audioTrack{
		{path: "/tts0.mp3", startSec: 0, endSec: 3},
	}
	_, filter, label := buildAudioFilterComplex(tracks, "/bgm.mp3", audioDirectives{
		BGMFadeInSec: 2, BGMFadeOutSec: 3, BGMVolume: 0.6, DuckingDepth: 0.15,
	}, 6.0)

	if !strings.Contains(filter, "between(t") {
		t.Errorf("expected ducking between(t,...) in filter, got %q", filter)
	}
	if label != "[final_mix]" {
		t.Errorf("expected [final_mix], got %q", label)
	}
}

func TestBuildAudioFilterComplex_singleTTS(t *testing.T) {
	tracks := []audioTrack{
		{path: "/tts0.mp3", startSec: 1.5, endSec: 4.5},
	}
	inputs, filter, label := buildAudioFilterComplex(tracks, "", audioDirectives{BGMVolume: 0.6}, 6.0)

	if len(inputs) != 2 {
		t.Errorf("expected 2 input args for single track, got %d", len(inputs))
	}
	if !strings.Contains(filter, "adelay=1500|1500") {
		t.Errorf("expected adelay=1500|1500, filter=%q", filter)
	}
	// Single TTS → no amix needed
	if strings.Contains(filter, "amix") {
		t.Errorf("unexpected amix for single TTS track, filter=%q", filter)
	}
	if label != "[tts0]" {
		t.Errorf("expected [tts0], got %q", label)
	}
}
