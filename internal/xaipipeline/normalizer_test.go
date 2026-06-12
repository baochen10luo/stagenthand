package xaipipeline_test

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/baochen10luo/stagenthand/internal/xaipipeline"
)

func TestFFmpegShotNormalizer_UsesSilentH264YUV420PContract(t *testing.T) {
	binDir := t.TempDir()
	argsLog := filepath.Join(t.TempDir(), "ffmpeg.args")
	ffmpegPath := filepath.Join(binDir, "ffmpeg")
	script := "#!/bin/sh\n" +
		"printf '%s\\n' \"$@\" > \"$FFMPEG_ARGS_LOG\"\n" +
		"last=''\n" +
		"for arg do last=\"$arg\"; done\n" +
		"printf normalized > \"$last\"\n"
	if err := os.WriteFile(ffmpegPath, []byte(script), 0755); err != nil {
		t.Fatalf("write stub ffmpeg: %v", err)
	}
	t.Setenv("PATH", binDir)
	t.Setenv("FFMPEG_ARGS_LOG", argsLog)

	workDir := t.TempDir()
	inputPath := filepath.Join(workDir, "shots", "shot_001.mp4")
	outputPath := filepath.Join(workDir, "normalized", "shot_001.mp4")
	if err := os.MkdirAll(filepath.Dir(inputPath), 0755); err != nil {
		t.Fatalf("mkdir input dir: %v", err)
	}
	if err := os.WriteFile(inputPath, []byte("raw"), 0644); err != nil {
		t.Fatalf("write input shot: %v", err)
	}

	normalizer := xaipipeline.NewFFmpegShotNormalizer()
	err := normalizer.Normalize(context.Background(), inputPath, outputPath, xaipipeline.RenderSpec{
		Width:  720,
		Height: 1280,
		FPS:    24,
	})
	if err != nil {
		t.Fatalf("Normalize: %v", err)
	}

	data, err := os.ReadFile(argsLog)
	if err != nil {
		t.Fatalf("read ffmpeg args log: %v", err)
	}
	gotArgs := strings.Split(strings.TrimSpace(string(data)), "\n")
	wantArgs := []string{
		"-i", inputPath,
		"-map", "0:v:0",
		"-vf", "scale=720:1280:force_original_aspect_ratio=increase,crop=720:1280,fps=24,format=yuv420p",
		"-c:v", "libx264",
		"-preset", "medium",
		"-crf", "20",
		"-an",
		"-movflags", "+faststart",
		"-y",
		outputPath,
	}
	if !sameStrings(gotArgs, wantArgs) {
		t.Fatalf("ffmpeg args = %#v, want %#v", gotArgs, wantArgs)
	}
	if _, err := os.Stat(outputPath); err != nil {
		t.Fatalf("normalized output missing: %v", err)
	}
}

func TestFFmpegShotNormalizer_RejectsCanceledContextBeforeOutputDir(t *testing.T) {
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
	outputDir := filepath.Join(workDir, "normalized")
	outputPath := filepath.Join(outputDir, "shot_001.mp4")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	normalizer := xaipipeline.NewFFmpegShotNormalizer()
	err := normalizer.Normalize(ctx, filepath.Join(workDir, "shots", "shot_001.mp4"), outputPath, xaipipeline.RenderSpec{
		Width:  720,
		Height: 1280,
		FPS:    24,
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Normalize() error = %v, want context.Canceled", err)
	}
	if _, statErr := os.Stat(outputDir); !os.IsNotExist(statErr) {
		t.Fatalf("normalized output dir should not be created after canceled context, stat err=%v", statErr)
	}
	if _, statErr := os.Stat(argsLog); !os.IsNotExist(statErr) {
		t.Fatalf("ffmpeg should not be called after canceled context, stat err=%v", statErr)
	}
}

func TestFFmpegShotNormalizer_NilContextUsesBackground(t *testing.T) {
	binDir := t.TempDir()
	argsLog := filepath.Join(t.TempDir(), "ffmpeg.args")
	ffmpegPath := filepath.Join(binDir, "ffmpeg")
	script := "#!/bin/sh\n" +
		"printf '%s\\n' \"$@\" > \"$FFMPEG_ARGS_LOG\"\n" +
		"last=''\n" +
		"for arg do last=\"$arg\"; done\n" +
		"printf normalized > \"$last\"\n"
	if err := os.WriteFile(ffmpegPath, []byte(script), 0755); err != nil {
		t.Fatalf("write stub ffmpeg: %v", err)
	}
	t.Setenv("PATH", binDir)
	t.Setenv("FFMPEG_ARGS_LOG", argsLog)

	workDir := t.TempDir()
	outputPath := filepath.Join(workDir, "normalized", "shot_001.mp4")
	normalizer := xaipipeline.NewFFmpegShotNormalizer()
	if err := normalizer.Normalize(nil, filepath.Join(workDir, "shots", "shot_001.mp4"), outputPath, xaipipeline.RenderSpec{}); err != nil {
		t.Fatalf("Normalize(nil): %v", err)
	}
	if _, err := os.Stat(outputPath); err != nil {
		t.Fatalf("normalized output missing: %v", err)
	}
}

func TestFFmpegShotNormalizer_NormalizesVideoShape(t *testing.T) {
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		t.Skip("ffmpeg not available")
	}
	if _, err := exec.LookPath("ffprobe"); err != nil {
		t.Skip("ffprobe not available")
	}

	tmpDir := t.TempDir()
	inputPath := filepath.Join(tmpDir, "input.mp4")
	outputPath := filepath.Join(tmpDir, "normalized", "shot_001.mp4")

	cmd := exec.Command("ffmpeg",
		"-f", "lavfi",
		"-i", "color=c=blue:s=640x640:d=0.5:r=12",
		"-f", "lavfi",
		"-i", "anullsrc=r=44100:cl=mono",
		"-t", "0.5",
		"-c:v", "libx264",
		"-pix_fmt", "yuv420p",
		"-c:a", "aac",
		"-y",
		inputPath,
	)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("create input mp4: %v\n%s", err, string(out))
	}

	normalizer := xaipipeline.NewFFmpegShotNormalizer()
	err := normalizer.Normalize(context.Background(), inputPath, outputPath, xaipipeline.RenderSpec{
		Width:  720,
		Height: 1280,
		FPS:    24,
	})
	if err != nil {
		t.Fatalf("Normalize: %v", err)
	}

	meta := probeMedia(t, outputPath)
	if len(meta.Streams) != 1 {
		t.Fatalf("streams = %+v, want one video stream only", meta.Streams)
	}
	stream := meta.Streams[0]
	if stream.CodecType != "video" || stream.CodecName != "h264" {
		t.Fatalf("stream = %+v, want h264 video", stream)
	}
	if stream.Width != 720 || stream.Height != 1280 {
		t.Fatalf("size = %dx%d, want 720x1280", stream.Width, stream.Height)
	}
	if stream.FrameRate != "24/1" {
		t.Fatalf("frame rate = %q, want 24/1", stream.FrameRate)
	}
}

type probedMedia struct {
	Streams []struct {
		CodecName string `json:"codec_name"`
		CodecType string `json:"codec_type"`
		Width     int    `json:"width"`
		Height    int    `json:"height"`
		FrameRate string `json:"r_frame_rate"`
	} `json:"streams"`
}

func probeMedia(t *testing.T, path string) probedMedia {
	t.Helper()
	cmd := exec.Command("ffprobe",
		"-v", "error",
		"-show_entries", "stream=codec_type,codec_name,width,height,r_frame_rate",
		"-of", "json",
		path,
	)
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("ffprobe %s: %v", path, err)
	}
	var meta probedMedia
	if err := json.Unmarshal(out, &meta); err != nil {
		t.Fatalf("parse ffprobe json: %v\n%s", err, string(out))
	}
	return meta
}
