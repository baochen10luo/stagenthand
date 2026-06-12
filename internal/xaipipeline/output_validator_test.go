package xaipipeline_test

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/baochen10luo/stagenthand/internal/xaipipeline"
)

func TestFFprobeOutputValidator_AcceptsValidFinalMP4AndReportsMetadata(t *testing.T) {
	requireFFmpegAndFFprobe(t)

	path := filepath.Join(t.TempDir(), "final.mp4")
	createFinalTestMP4(t, path, "720x1280", 24, 0.5)

	validator := xaipipeline.NewFFprobeOutputValidator()
	meta, err := validator.Validate(context.Background(), path, xaipipeline.RenderValidationSpec{
		Width:               720,
		Height:              1280,
		FPS:                 24,
		PixelFormat:         "yuv420p",
		ExpectedDurationSec: 0.5,
	})
	if err != nil {
		t.Fatalf("Validate: %v", err)
	}

	if meta.Path != path {
		t.Fatalf("path = %q, want %q", meta.Path, path)
	}
	if meta.Width != 720 || meta.Height != 1280 {
		t.Fatalf("size = %dx%d, want 720x1280", meta.Width, meta.Height)
	}
	if meta.CodecName != "h264" {
		t.Fatalf("codec = %q, want h264", meta.CodecName)
	}
	if meta.PixelFormat != "yuv420p" {
		t.Fatalf("pixel format = %q, want yuv420p", meta.PixelFormat)
	}
	if meta.FPS != 24 {
		t.Fatalf("fps = %v, want 24", meta.FPS)
	}
	if meta.DurationSec <= 0 {
		t.Fatalf("duration = %v, want positive", meta.DurationSec)
	}
	if meta.SizeBytes <= 0 {
		t.Fatalf("size bytes = %d, want positive", meta.SizeBytes)
	}
}

func TestFFprobeOutputValidator_RejectsCanceledContextBeforeStat(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	validator := xaipipeline.NewFFprobeOutputValidator()
	_, err := validator.Validate(ctx, filepath.Join(t.TempDir(), "missing.mp4"), xaipipeline.RenderValidationSpec{
		Width:  720,
		Height: 1280,
		FPS:    24,
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Validate() error = %v, want context.Canceled", err)
	}
}

func TestFFprobeOutputValidator_UsesRenderMetadataProbeContract(t *testing.T) {
	binDir := t.TempDir()
	argsLog := filepath.Join(t.TempDir(), "ffprobe.args")
	ffprobePath := filepath.Join(binDir, "ffprobe")
	script := `#!/bin/sh
printf '%s\n' "$@" > "$FFPROBE_ARGS_LOG"
printf '%s\n' '{
  "streams": [
    {
      "codec_type": "video",
      "codec_name": "h264",
      "width": 720,
      "height": 1280,
      "r_frame_rate": "24/1",
      "pix_fmt": "yuv420p"
    }
  ],
  "format": {
    "duration": "8.000000"
  }
}'
`
	if err := os.WriteFile(ffprobePath, []byte(script), 0755); err != nil {
		t.Fatalf("write stub ffprobe: %v", err)
	}
	t.Setenv("PATH", binDir)
	t.Setenv("FFPROBE_ARGS_LOG", argsLog)

	path := filepath.Join(t.TempDir(), "output_xai.mp4")
	if err := os.WriteFile(path, []byte("mp4-bytes"), 0644); err != nil {
		t.Fatalf("write output: %v", err)
	}

	validator := xaipipeline.NewFFprobeOutputValidator()
	meta, err := validator.Validate(context.Background(), path, xaipipeline.RenderValidationSpec{
		Width:               720,
		Height:              1280,
		FPS:                 24,
		CodecName:           "h264",
		PixelFormat:         "yuv420p",
		ExpectedDurationSec: 8,
		RequireNoAudio:      true,
	})
	if err != nil {
		t.Fatalf("Validate: %v", err)
	}

	data, err := os.ReadFile(argsLog)
	if err != nil {
		t.Fatalf("read ffprobe args log: %v", err)
	}
	gotArgs := strings.Split(strings.TrimSpace(string(data)), "\n")
	wantArgs := []string{
		"-v", "error",
		"-show_entries", "stream=codec_type,codec_name,width,height,r_frame_rate,pix_fmt",
		"-show_entries", "format=duration",
		"-of", "json",
		path,
	}
	if !sameStrings(gotArgs, wantArgs) {
		t.Fatalf("ffprobe args = %#v, want %#v", gotArgs, wantArgs)
	}
	if meta.Path != path || meta.Width != 720 || meta.Height != 1280 || meta.FPS != 24 {
		t.Fatalf("metadata shape = %+v", meta)
	}
	if meta.CodecName != "h264" || meta.PixelFormat != "yuv420p" || meta.HasAudio {
		t.Fatalf("metadata codec/audio = %+v", meta)
	}
	if meta.DurationSec != 8 || meta.SizeBytes != int64(len("mp4-bytes")) {
		t.Fatalf("metadata duration/size = %+v", meta)
	}
}

func TestFFprobeOutputValidator_NilContextUsesBackground(t *testing.T) {
	binDir := t.TempDir()
	ffprobePath := filepath.Join(binDir, "ffprobe")
	script := `#!/bin/sh
printf '%s\n' '{
  "streams": [
    {
      "codec_type": "video",
      "codec_name": "h264",
      "width": 720,
      "height": 1280,
      "r_frame_rate": "24/1",
      "pix_fmt": "yuv420p"
    }
  ],
  "format": {
    "duration": "8.000000"
  }
}'
`
	if err := os.WriteFile(ffprobePath, []byte(script), 0755); err != nil {
		t.Fatalf("write stub ffprobe: %v", err)
	}
	t.Setenv("PATH", binDir)

	path := filepath.Join(t.TempDir(), "output_xai.mp4")
	if err := os.WriteFile(path, []byte("mp4-bytes"), 0644); err != nil {
		t.Fatalf("write output: %v", err)
	}

	validator := xaipipeline.NewFFprobeOutputValidator()
	meta, err := validator.Validate(nil, path, xaipipeline.RenderValidationSpec{
		Width:       720,
		Height:      1280,
		FPS:         24,
		CodecName:   "h264",
		PixelFormat: "yuv420p",
	})
	if err != nil {
		t.Fatalf("Validate(nil): %v", err)
	}
	if meta.Path != path || meta.Width != 720 || meta.Height != 1280 {
		t.Fatalf("metadata = %+v", meta)
	}
}

func TestFFprobeOutputValidator_RejectsMissingEmptyAndInvalidFiles(t *testing.T) {
	validator := xaipipeline.NewFFprobeOutputValidator()
	spec := xaipipeline.RenderValidationSpec{Width: 720, Height: 1280, FPS: 24}

	if _, err := validator.Validate(context.Background(), filepath.Join(t.TempDir(), "missing.mp4"), spec); err == nil {
		t.Fatal("missing file should fail")
	}

	emptyPath := filepath.Join(t.TempDir(), "empty.mp4")
	if err := os.WriteFile(emptyPath, nil, 0644); err != nil {
		t.Fatalf("write empty file: %v", err)
	}
	if _, err := validator.Validate(context.Background(), emptyPath, spec); err == nil {
		t.Fatal("empty file should fail")
	}

	invalidPath := filepath.Join(t.TempDir(), "invalid.mp4")
	if err := os.WriteFile(invalidPath, []byte("not a video"), 0644); err != nil {
		t.Fatalf("write invalid file: %v", err)
	}
	if _, err := validator.Validate(context.Background(), invalidPath, spec); err == nil {
		t.Fatal("invalid file should fail")
	}
}

func TestFFprobeOutputValidator_RejectsWrongDimensions(t *testing.T) {
	requireFFmpegAndFFprobe(t)

	path := filepath.Join(t.TempDir(), "wrong-size.mp4")
	createFinalTestMP4(t, path, "640x640", 24, 0.5)

	validator := xaipipeline.NewFFprobeOutputValidator()
	_, err := validator.Validate(context.Background(), path, xaipipeline.RenderValidationSpec{
		Width:               720,
		Height:              1280,
		FPS:                 24,
		ExpectedDurationSec: 0.5,
	})
	if err == nil {
		t.Fatal("wrong dimensions should fail")
	}
	if !strings.Contains(err.Error(), "dimensions") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestFFprobeOutputValidator_RejectsAudioWhenRequiredSilent(t *testing.T) {
	requireFFmpegAndFFprobe(t)

	path := filepath.Join(t.TempDir(), "with-audio.mp4")
	createFinalTestMP4WithAudio(t, path, "720x1280", 24, 0.5)

	validator := xaipipeline.NewFFprobeOutputValidator()
	_, err := validator.Validate(context.Background(), path, xaipipeline.RenderValidationSpec{
		Width:               720,
		Height:              1280,
		FPS:                 24,
		ExpectedDurationSec: 0.5,
		RequireNoAudio:      true,
	})
	if err == nil {
		t.Fatal("audio stream should fail when RequireNoAudio is true")
	}
	if !strings.Contains(err.Error(), "audio") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestFFprobeOutputValidator_RejectsWrongPixelFormat(t *testing.T) {
	requireFFmpegAndFFprobe(t)

	path := filepath.Join(t.TempDir(), "wrong-pixel-format.mp4")
	createFinalTestMP4WithPixelFormat(t, path, "720x1280", 24, 0.5, "yuv444p")

	validator := xaipipeline.NewFFprobeOutputValidator()
	_, err := validator.Validate(context.Background(), path, xaipipeline.RenderValidationSpec{
		Width:               720,
		Height:              1280,
		FPS:                 24,
		ExpectedDurationSec: 0.5,
		PixelFormat:         "yuv420p",
	})
	if err == nil {
		t.Fatal("wrong pixel format should fail")
	}
	if !strings.Contains(err.Error(), "pixel format") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func requireFFmpegAndFFprobe(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		t.Skip("ffmpeg not available")
	}
	if _, err := exec.LookPath("ffprobe"); err != nil {
		t.Skip("ffprobe not available")
	}
}

func createFinalTestMP4(t *testing.T, path string, size string, fps int, duration float64) {
	t.Helper()
	createFinalTestMP4WithPixelFormat(t, path, size, fps, duration, "yuv420p")
}

func createFinalTestMP4WithPixelFormat(t *testing.T, path string, size string, fps int, duration float64, pixelFormat string) {
	t.Helper()
	cmd := exec.Command("ffmpeg",
		"-f", "lavfi",
		"-i", fmt.Sprintf("color=c=black:s=%s:d=%.3f:r=%d", size, duration, fps),
		"-t", fmt.Sprintf("%.3f", duration),
		"-r", fmt.Sprintf("%d", fps),
		"-c:v", "libx264",
		"-pix_fmt", pixelFormat,
		"-y",
		path,
	)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("create final mp4: %v\n%s", err, string(out))
	}
}

func createFinalTestMP4WithAudio(t *testing.T, path string, size string, fps int, duration float64) {
	t.Helper()
	cmd := exec.Command("ffmpeg",
		"-f", "lavfi",
		"-i", fmt.Sprintf("color=c=black:s=%s:d=%.3f:r=%d", size, duration, fps),
		"-f", "lavfi",
		"-i", "anullsrc=r=44100:cl=mono",
		"-t", fmt.Sprintf("%.3f", duration),
		"-r", fmt.Sprintf("%d", fps),
		"-c:v", "libx264",
		"-pix_fmt", "yuv420p",
		"-c:a", "aac",
		"-shortest",
		"-y",
		path,
	)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("create final mp4 with audio: %v\n%s", err, string(out))
	}
}
