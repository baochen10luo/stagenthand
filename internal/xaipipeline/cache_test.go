package xaipipeline_test

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/baochen10luo/stagenthand/internal/xaipipeline"
)

func TestFFprobeShotValidator_RejectsMissingOrEmpty(t *testing.T) {
	validator := xaipipeline.NewFFprobeShotValidator()

	if validator.ValidShot(context.Background(), filepath.Join(t.TempDir(), "missing.mp4"), xaipipeline.RenderValidationSpec{}) {
		t.Fatal("missing file should be invalid")
	}

	emptyPath := filepath.Join(t.TempDir(), "empty.mp4")
	if err := os.WriteFile(emptyPath, nil, 0644); err != nil {
		t.Fatalf("write empty file: %v", err)
	}
	if validator.ValidShot(context.Background(), emptyPath, xaipipeline.RenderValidationSpec{}) {
		t.Fatal("empty file should be invalid")
	}
}

func TestFFprobeShotValidator_RejectsInvalidNonEmptyFile(t *testing.T) {
	validator := xaipipeline.NewFFprobeShotValidator()
	path := filepath.Join(t.TempDir(), "invalid.mp4")
	if err := os.WriteFile(path, []byte("not a video"), 0644); err != nil {
		t.Fatalf("write invalid file: %v", err)
	}

	if validator.ValidShot(context.Background(), path, xaipipeline.RenderValidationSpec{}) {
		t.Fatal("invalid mp4 should be invalid")
	}
}

func TestFFprobeShotValidator_AcceptsValidMP4(t *testing.T) {
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		t.Skip("ffmpeg not available")
	}
	if _, err := exec.LookPath("ffprobe"); err != nil {
		t.Skip("ffprobe not available")
	}

	path := filepath.Join(t.TempDir(), "valid.mp4")
	cmd := exec.Command("ffmpeg",
		"-f", "lavfi",
		"-i", "color=c=black:s=16x16:d=0.1",
		"-c:v", "libx264",
		"-pix_fmt", "yuv420p",
		"-y",
		path,
	)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("ffmpeg create test mp4: %v\n%s", err, string(out))
	}

	validator := xaipipeline.NewFFprobeShotValidator()
	if !validator.ValidShot(context.Background(), path, xaipipeline.RenderValidationSpec{}) {
		t.Fatal("valid mp4 should be valid")
	}
}

func TestFFprobeShotValidator_RejectsMP4WithWrongDuration(t *testing.T) {
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		t.Skip("ffmpeg not available")
	}
	if _, err := exec.LookPath("ffprobe"); err != nil {
		t.Skip("ffprobe not available")
	}

	path := filepath.Join(t.TempDir(), "wrong-duration.mp4")
	cmd := exec.Command("ffmpeg",
		"-f", "lavfi",
		"-i", "color=c=black:s=16x16:d=0.1",
		"-c:v", "libx264",
		"-pix_fmt", "yuv420p",
		"-y",
		path,
	)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("ffmpeg create test mp4: %v\n%s", err, string(out))
	}

	validator := xaipipeline.NewFFprobeShotValidator()
	if validator.ValidShot(context.Background(), path, xaipipeline.RenderValidationSpec{ExpectedDurationSec: 8}) {
		t.Fatal("wrong-duration mp4 should be invalid for xAI shot cache reuse")
	}
}

func TestFFprobeShotValidator_RejectsDecodableNonMP4Container(t *testing.T) {
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		t.Skip("ffmpeg not available")
	}
	if _, err := exec.LookPath("ffprobe"); err != nil {
		t.Skip("ffprobe not available")
	}

	path := filepath.Join(t.TempDir(), "wrong-container.mp4")
	cmd := exec.Command("ffmpeg",
		"-f", "lavfi",
		"-i", "color=c=black:s=16x16:d=0.1",
		"-c:v", "libx264",
		"-pix_fmt", "yuv420p",
		"-f", "matroska",
		"-y",
		path,
	)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("ffmpeg create test matroska: %v\n%s", err, string(out))
	}

	validator := xaipipeline.NewFFprobeShotValidator()
	if validator.ValidShot(context.Background(), path, xaipipeline.RenderValidationSpec{}) {
		t.Fatal("decodable non-MP4 container should be invalid for xAI shot cache reuse")
	}
}
