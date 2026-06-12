package xaipipeline_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/baochen10luo/stagenthand/internal/xaipipeline"
)

func TestFFmpegPreviewExtractor_UsesPreviewFrameContract(t *testing.T) {
	binDir := t.TempDir()
	argsLog := filepath.Join(t.TempDir(), "ffmpeg.args")
	ffmpegPath := filepath.Join(binDir, "ffmpeg")
	script := "#!/bin/sh\n" +
		"printf '%s\\n' \"$@\" > \"$FFMPEG_ARGS_LOG\"\n" +
		"last=''\n" +
		"for arg do last=\"$arg\"; done\n" +
		"printf preview > \"$last\"\n"
	if err := os.WriteFile(ffmpegPath, []byte(script), 0755); err != nil {
		t.Fatalf("write stub ffmpeg: %v", err)
	}
	t.Setenv("PATH", binDir)
	t.Setenv("FFMPEG_ARGS_LOG", argsLog)

	workDir := t.TempDir()
	inputPath := filepath.Join(workDir, "output_xai.mp4")
	outputPath := filepath.Join(workDir, "preview", "preview_frame.jpg")
	if err := os.WriteFile(inputPath, []byte("final"), 0644); err != nil {
		t.Fatalf("write final output: %v", err)
	}

	extractor := xaipipeline.NewFFmpegPreviewExtractor()
	if err := extractor.Extract(context.Background(), inputPath, outputPath); err != nil {
		t.Fatalf("Extract: %v", err)
	}

	data, err := os.ReadFile(argsLog)
	if err != nil {
		t.Fatalf("read ffmpeg args log: %v", err)
	}
	gotArgs := strings.Split(strings.TrimSpace(string(data)), "\n")
	wantArgs := []string{
		"-i", inputPath,
		"-ss", "0.25",
		"-frames:v", "1",
		"-q:v", "2",
		"-y",
		outputPath,
	}
	if !sameStrings(gotArgs, wantArgs) {
		t.Fatalf("ffmpeg args = %#v, want %#v", gotArgs, wantArgs)
	}
	if _, err := os.Stat(outputPath); err != nil {
		t.Fatalf("preview output missing: %v", err)
	}
}

func TestFFmpegPreviewExtractor_RejectsCanceledContextBeforeOutputDir(t *testing.T) {
	binDir := t.TempDir()
	argsLog := filepath.Join(t.TempDir(), "ffmpeg.args")
	ffmpegPath := filepath.Join(binDir, "ffmpeg")
	script := "#!/bin/sh\n" +
		"printf '%s\\n' \"$@\" > \"$FFMPEG_ARGS_LOG\"\n"
	if err := os.WriteFile(ffmpegPath, []byte(script), 0755); err != nil {
		t.Fatalf("write stub ffmpeg: %v", err)
	}
	t.Setenv("PATH", binDir)
	t.Setenv("FFMPEG_ARGS_LOG", argsLog)

	workDir := t.TempDir()
	outputDir := filepath.Join(workDir, "preview")
	outputPath := filepath.Join(outputDir, "preview_frame.jpg")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	extractor := xaipipeline.NewFFmpegPreviewExtractor()
	err := extractor.Extract(ctx, filepath.Join(workDir, "output_xai.mp4"), outputPath)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Extract() error = %v, want context.Canceled", err)
	}
	if _, statErr := os.Stat(outputDir); !os.IsNotExist(statErr) {
		t.Fatalf("preview output dir should not be created after canceled context, stat err=%v", statErr)
	}
	if _, statErr := os.Stat(argsLog); !os.IsNotExist(statErr) {
		t.Fatalf("ffmpeg should not be called after canceled context, stat err=%v", statErr)
	}
}

func TestFFmpegPreviewExtractor_NilContextUsesBackground(t *testing.T) {
	binDir := t.TempDir()
	argsLog := filepath.Join(t.TempDir(), "ffmpeg.args")
	ffmpegPath := filepath.Join(binDir, "ffmpeg")
	script := "#!/bin/sh\n" +
		"printf '%s\\n' \"$@\" > \"$FFMPEG_ARGS_LOG\"\n" +
		"last=''\n" +
		"for arg do last=\"$arg\"; done\n" +
		"printf preview > \"$last\"\n"
	if err := os.WriteFile(ffmpegPath, []byte(script), 0755); err != nil {
		t.Fatalf("write stub ffmpeg: %v", err)
	}
	t.Setenv("PATH", binDir)
	t.Setenv("FFMPEG_ARGS_LOG", argsLog)

	workDir := t.TempDir()
	outputPath := filepath.Join(workDir, "preview", "preview_frame.jpg")
	extractor := xaipipeline.NewFFmpegPreviewExtractor()
	if err := extractor.Extract(nil, filepath.Join(workDir, "output_xai.mp4"), outputPath); err != nil {
		t.Fatalf("Extract(nil): %v", err)
	}
	if _, err := os.Stat(outputPath); err != nil {
		t.Fatalf("preview output missing: %v", err)
	}
}

func TestFFmpegPreviewExtractor_ExtractsJPEG(t *testing.T) {
	requireFFmpegAndFFprobe(t)

	tmpDir := t.TempDir()
	inputPath := filepath.Join(tmpDir, "final.mp4")
	outputPath := filepath.Join(tmpDir, "preview_frame.jpg")
	createFinalTestMP4(t, inputPath, "720x1280", 24, 1)

	extractor := xaipipeline.NewFFmpegPreviewExtractor()
	if err := extractor.Extract(context.Background(), inputPath, outputPath); err != nil {
		t.Fatalf("Extract: %v", err)
	}

	data, err := os.ReadFile(outputPath)
	if err != nil {
		t.Fatalf("read preview: %v", err)
	}
	if len(data) < 2 || data[0] != 0xff || data[1] != 0xd8 {
		t.Fatalf("preview should be a JPEG, first bytes: % x", data[:min(len(data), 8)])
	}
}
