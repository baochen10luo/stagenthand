package xaipipeline_test

import (
	"context"
	"image/jpeg"
	"os"
	"path/filepath"
	"testing"

	"github.com/baochen10luo/stagenthand/internal/xaipipeline"
)

func TestDryRunShotGenerator_ReturnsPlaceholderBytes(t *testing.T) {
	generator := xaipipeline.NewDryRunShotGenerator()
	data, err := generator.GenerateShot(context.Background(), xaipipeline.Shot{Index: 1})
	if err != nil {
		t.Fatalf("GenerateShot: %v", err)
	}
	if len(data) == 0 {
		t.Fatal("dry-run shot bytes should not be empty")
	}
	if !dryRunTestHasMP4Magic(data) {
		t.Fatalf("dry-run shot bytes should have MP4 ftyp magic, got %q", string(data))
	}
}

func TestDryRunShotGenerator_ReturnsDeterministicMetadata(t *testing.T) {
	generator := xaipipeline.NewDryRunShotGenerator()
	result, err := generator.GenerateShotResult(context.Background(), xaipipeline.Shot{Index: 7})
	if err != nil {
		t.Fatalf("GenerateShotResult: %v", err)
	}
	if len(result.Data) == 0 {
		t.Fatal("dry-run shot bytes should not be empty")
	}
	if result.RequestID != "dry-run-shot-007" {
		t.Fatalf("RequestID = %q, want dry-run-shot-007", result.RequestID)
	}
	if result.Status != "dry_run" {
		t.Fatalf("Status = %q, want dry_run", result.Status)
	}
}

func TestDryRunShotValidator_AcceptsOnlyMP4ShapedFiles(t *testing.T) {
	validator := xaipipeline.DryRunShotValidator{}
	path := filepath.Join(t.TempDir(), "shot.mp4")
	if validator.ValidShot(context.Background(), path, xaipipeline.RenderValidationSpec{}) {
		t.Fatal("missing file should be invalid")
	}
	if err := os.WriteFile(path, nil, 0644); err != nil {
		t.Fatalf("write empty file: %v", err)
	}
	if validator.ValidShot(context.Background(), path, xaipipeline.RenderValidationSpec{}) {
		t.Fatal("empty file should be invalid")
	}
	if err := os.WriteFile(path, []byte("placeholder"), 0644); err != nil {
		t.Fatalf("write placeholder file: %v", err)
	}
	if validator.ValidShot(context.Background(), path, xaipipeline.RenderValidationSpec{}) {
		t.Fatal("non-MP4-shaped placeholder should be invalid")
	}
	generator := xaipipeline.NewDryRunShotGenerator()
	data, err := generator.GenerateShot(context.Background(), xaipipeline.Shot{Index: 1})
	if err != nil {
		t.Fatalf("GenerateShot: %v", err)
	}
	if err := os.WriteFile(path, data, 0644); err != nil {
		t.Fatalf("write MP4-shaped placeholder: %v", err)
	}
	if !validator.ValidShot(context.Background(), path, xaipipeline.RenderValidationSpec{}) {
		t.Fatal("MP4-shaped dry-run placeholder should be valid")
	}
}

func TestDryRunFinalizer_WritesPlaceholderOutput(t *testing.T) {
	finalizer := xaipipeline.DryRunFinalizer{}
	outputPath := filepath.Join(t.TempDir(), "output.mp4")
	if err := finalizer.Finalize(context.Background(), "timeline.mp4", outputPath); err != nil {
		t.Fatalf("Finalize: %v", err)
	}
	if data, err := os.ReadFile(outputPath); err != nil || len(data) == 0 {
		t.Fatalf("dry-run final output missing: err=%v data=%q", err, string(data))
	} else if !dryRunTestHasMP4Magic(data) {
		t.Fatalf("dry-run final output should have MP4 ftyp magic, got %q", string(data))
	}
}

func TestDryRunShotNormalizer_WritesPlaceholderOutput(t *testing.T) {
	normalizer := xaipipeline.DryRunShotNormalizer{}
	outputPath := filepath.Join(t.TempDir(), "normalized", "shot_001.mp4")

	if err := normalizer.Normalize(context.Background(), "shots/shot_001.mp4", outputPath, xaipipeline.RenderSpec{}); err != nil {
		t.Fatalf("Normalize: %v", err)
	}
	if data, err := os.ReadFile(outputPath); err != nil || len(data) == 0 {
		t.Fatalf("normalized dry-run output missing: err=%v data=%q", err, string(data))
	} else if !dryRunTestHasMP4Magic(data) {
		t.Fatalf("normalized dry-run output should have MP4 ftyp magic, got %q", string(data))
	}
}

func TestDryRunOutputValidator_ReturnsDeterministicMetadata(t *testing.T) {
	validator := xaipipeline.DryRunOutputValidator{}
	metadata, err := validator.Validate(context.Background(), "output_xai.mp4", xaipipeline.RenderValidationSpec{
		Width:               720,
		Height:              1280,
		FPS:                 24,
		ExpectedDurationSec: 8,
	})
	if err != nil {
		t.Fatalf("Validate: %v", err)
	}
	if metadata.Path != "output_xai.mp4" || metadata.Width != 720 || metadata.Height != 1280 || metadata.FPS != 24 || metadata.DurationSec != 8 {
		t.Fatalf("metadata = %+v", metadata)
	}
	if metadata.CodecName != "h264" || metadata.PixelFormat != "yuv420p" || metadata.SizeBytes != 1 || metadata.HasAudio {
		t.Fatalf("metadata = %+v", metadata)
	}
}

func TestDryRunPreviewExtractor_WritesPlaceholderOutput(t *testing.T) {
	extractor := xaipipeline.DryRunPreviewExtractor{}
	outputPath := filepath.Join(t.TempDir(), "preview_frame.jpg")

	if err := extractor.Extract(context.Background(), "output_xai.mp4", outputPath); err != nil {
		t.Fatalf("Extract: %v", err)
	}
	if data, err := os.ReadFile(outputPath); err != nil || len(data) == 0 {
		t.Fatalf("dry-run preview output missing: err=%v data=%q", err, string(data))
	}
	file, err := os.Open(outputPath)
	if err != nil {
		t.Fatalf("open dry-run preview: %v", err)
	}
	defer file.Close()
	config, err := jpeg.DecodeConfig(file)
	if err != nil {
		t.Fatalf("dry-run preview should be a readable JPEG: %v", err)
	}
	if config.Width != 720 || config.Height != 1280 {
		t.Fatalf("dry-run preview dimensions = %dx%d, want 720x1280", config.Width, config.Height)
	}
}

func dryRunTestHasMP4Magic(data []byte) bool {
	return len(data) >= 8 && string(data[4:8]) == "ftyp"
}
