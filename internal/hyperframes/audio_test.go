package hyperframes

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/baochen10luo/stagenthand/internal/domain"
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

func TestEffectiveDirectivesUsesDefaultsAndOverrides(t *testing.T) {
	defaults := effectiveDirectives(nil)
	if defaults.BGMFadeInSec != 2 || defaults.BGMFadeOutSec != 3 || defaults.BGMVolume != 0.6 || defaults.DuckingDepth != 0.15 {
		t.Fatalf("defaults = %+v", defaults)
	}

	got := effectiveDirectives(&domain.Directives{
		BGMFadeInSec:  0.5,
		BGMFadeOutSec: 1.25,
		BGMVolume:     0.8,
		DuckingDepth:  0.25,
	})
	if got.BGMFadeInSec != 0.5 || got.BGMFadeOutSec != 1.25 || got.BGMVolume != 0.8 || got.DuckingDepth != 0.25 {
		t.Fatalf("overrides = %+v", got)
	}
}

func TestCollectTracksResolvesVirtualPathsAndDefaultDurations(t *testing.T) {
	tracks, total := collectTracks(domain.RemotionProps{
		Panels: []domain.Panel{
			{AudioURL: "/shand/project/audio/one.mp3", DurationSec: 0},
			{DurationSec: 2},
			{AudioURL: "/abs/two.mp3", DurationSec: 4},
		},
	}, "/home/.shand")

	if total != 9 {
		t.Fatalf("total = %.1f, want 9.0", total)
	}
	if len(tracks) != 2 {
		t.Fatalf("tracks = %#v, want 2", tracks)
	}
	if tracks[0].path != "/home/.shand/projects/project/audio/one.mp3" || tracks[0].startSec != 0 || tracks[0].endSec != 3 {
		t.Fatalf("first track = %+v", tracks[0])
	}
	if tracks[1].path != "/abs/two.mp3" || tracks[1].startSec != 5 || tracks[1].endSec != 9 {
		t.Fatalf("second track = %+v", tracks[1])
	}
}

func TestMixAudioReturnsEmptyWhenNoAudio(t *testing.T) {
	got, err := MixAudio(context.Background(), domain.RemotionProps{}, Config{ShandHome: "/home/.shand"}, t.TempDir())
	if err != nil {
		t.Fatalf("MixAudio() error = %v", err)
	}
	if got != "" {
		t.Fatalf("MixAudio() = %q, want empty path", got)
	}
}

func TestMixAudioDryRunDoesNotRequireFFmpeg(t *testing.T) {
	t.Setenv("PATH", t.TempDir())

	got, err := MixAudio(context.Background(), domain.RemotionProps{
		BGMURL: "/shand/project/audio/bgm.mp3",
		Panels: []domain.Panel{
			{AudioURL: "/shand/project/audio/tts.mp3", DurationSec: 3},
		},
	}, Config{ShandHome: "/home/.shand", DryRun: true}, t.TempDir())
	if err != nil {
		t.Fatalf("MixAudio() dry-run error = %v", err)
	}
	if got != "" {
		t.Fatalf("MixAudio() dry-run = %q, want empty path", got)
	}
}

func TestMixAudioRunsFFmpegWithExpectedInputsAndFilter(t *testing.T) {
	binDir := t.TempDir()
	argsLog := filepath.Join(t.TempDir(), "ffmpeg.args")
	writeHyperframesTestFFmpeg(t, filepath.Join(binDir, "ffmpeg"))
	t.Setenv("PATH", binDir)
	t.Setenv("FFMPEG_ARGS_LOG", argsLog)

	outputDir := t.TempDir()
	got, err := MixAudio(context.Background(), domain.RemotionProps{
		BGMURL: "/shand/project/audio/bgm.mp3",
		Directives: &domain.Directives{
			BGMFadeInSec:  1,
			BGMFadeOutSec: 2,
			BGMVolume:     0.7,
			DuckingDepth:  0.2,
		},
		Panels: []domain.Panel{
			{AudioURL: "/shand/project/audio/tts_1.mp3", DurationSec: 3},
			{AudioURL: "/abs/tts_2.mp3", DurationSec: 2},
		},
	}, Config{ShandHome: "/home/.shand"}, outputDir)
	if err != nil {
		t.Fatalf("MixAudio() error = %v", err)
	}
	wantPath := filepath.Join(outputDir, "audio_mix.aac")
	if got != wantPath {
		t.Fatalf("MixAudio() = %q, want %q", got, wantPath)
	}
	data, err := os.ReadFile(argsLog)
	if err != nil {
		t.Fatalf("read args log: %v", err)
	}
	args := string(data)
	for _, want := range []string{
		"/home/.shand/projects/project/audio/tts_1.mp3",
		"/abs/tts_2.mp3",
		"/home/.shand/projects/project/audio/bgm.mp3",
		"-filter_complex",
		"adelay=3000|3000",
		"volume='if(between",
		"-map\n[final_mix]",
		"-c:a\naac",
		wantPath,
	} {
		if !strings.Contains(args, want) {
			t.Fatalf("ffmpeg args missing %q:\n%s", want, args)
		}
	}
}

func TestMuxVideoAudioRunsFFmpegWithCopyVideoContract(t *testing.T) {
	binDir := t.TempDir()
	argsLog := filepath.Join(t.TempDir(), "ffmpeg.args")
	writeHyperframesTestFFmpeg(t, filepath.Join(binDir, "ffmpeg"))
	t.Setenv("PATH", binDir)
	t.Setenv("FFMPEG_ARGS_LOG", argsLog)

	outputPath := filepath.Join(t.TempDir(), "muxed.mp4")
	err := MuxVideoAudio(context.Background(), "/video.mp4", "/audio.aac", outputPath)
	if err != nil {
		t.Fatalf("MuxVideoAudio() error = %v", err)
	}
	data, err := os.ReadFile(argsLog)
	if err != nil {
		t.Fatalf("read args log: %v", err)
	}
	wantArgs := []string{
		"-i",
		"/video.mp4",
		"-i",
		"/audio.aac",
		"-c:v",
		"copy",
		"-c:a",
		"aac",
		"-shortest",
		"-y",
		outputPath,
	}
	if got := strings.Split(strings.TrimSpace(string(data)), "\n"); strings.Join(got, "\x00") != strings.Join(wantArgs, "\x00") {
		t.Fatalf("ffmpeg args = %#v, want %#v", got, wantArgs)
	}
}

func writeHyperframesTestFFmpeg(t *testing.T, path string) {
	t.Helper()
	script := "#!/bin/sh\n" +
		"printf '%s\\n' \"$@\" > \"$FFMPEG_ARGS_LOG\"\n" +
		"last=''\n" +
		"for arg do last=\"$arg\"; done\n" +
		"printf artifact > \"$last\"\n"
	if err := os.WriteFile(path, []byte(script), 0755); err != nil {
		t.Fatalf("write fake ffmpeg: %v", err)
	}
}
