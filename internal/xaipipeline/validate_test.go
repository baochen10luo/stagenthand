package xaipipeline_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"image"
	"image/jpeg"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/baochen10luo/stagenthand/internal/xaipipeline"
)

const validValidationStoryHash = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"

func TestValidateOutputDir_AcceptsCompleteProductionOutput(t *testing.T) {
	t.Parallel()

	outputDir := t.TempDir()
	writeProductionValidationArtifacts(t, outputDir, true)
	validator := &validateStubOutputValidator{
		metadata: xaipipeline.RenderMetadata{
			Path:        filepath.Join(outputDir, "output_xai.mp4"),
			Width:       720,
			Height:      1280,
			FPS:         24,
			DurationSec: 8,
			CodecName:   "h264",
			PixelFormat: "yuv420p",
			SizeBytes:   validationMP4SizeBytes(),
			HasAudio:    false,
		},
	}

	summary, err := xaipipeline.ValidateOutputDir(context.Background(), outputDir, validator)
	if err != nil {
		t.Fatalf("ValidateOutputDir() error = %v", err)
	}

	if summary.Status != xaipipeline.ValidationStatusValid {
		t.Fatalf("Status = %q, want %q; issues=%#v", summary.Status, xaipipeline.ValidationStatusValid, summary.Issues)
	}
	if summary.Inspect.Status != xaipipeline.InspectStatusComplete {
		t.Fatalf("Inspect.Status = %q", summary.Inspect.Status)
	}
	if summary.StoryHash != validValidationStoryHash {
		t.Fatalf("StoryHash = %q, want %q", summary.StoryHash, validValidationStoryHash)
	}
	if summary.VideoModel != "grok-imagine-video" {
		t.Fatalf("VideoModel = %q, want grok-imagine-video", summary.VideoModel)
	}
	if summary.FFprobeMetadata == nil {
		t.Fatal("FFprobeMetadata = nil")
	}
	if len(summary.Issues) != 0 {
		t.Fatalf("Issues = %#v, want none", summary.Issues)
	}
	if !validator.called {
		t.Fatal("ffprobe validator was not called")
	}
	if validator.path != filepath.Join(outputDir, "output_xai.mp4") {
		t.Fatalf("validator path = %q", validator.path)
	}
	if validator.spec.Width != 720 || validator.spec.Height != 1280 || validator.spec.FPS != 24 {
		t.Fatalf("validator spec = %#v", validator.spec)
	}
	if validator.spec.ExpectedDurationSec != 8 {
		t.Fatalf("ExpectedDurationSec = %.3f, want 8", validator.spec.ExpectedDurationSec)
	}
	if !validator.spec.RequireNoAudio {
		t.Fatal("RequireNoAudio = false, want true")
	}
	if validator.spec.PixelFormat != "yuv420p" {
		t.Fatalf("PixelFormat = %q, want yuv420p", validator.spec.PixelFormat)
	}
}

func TestValidateOutputDir_NilContextUsesBackground(t *testing.T) {
	t.Parallel()

	outputDir := t.TempDir()
	writeProductionValidationArtifacts(t, outputDir, true)
	validator := validationOutputValidator(t, outputDir)

	summary, err := xaipipeline.ValidateOutputDir(nil, outputDir, validator)
	if err != nil {
		t.Fatalf("ValidateOutputDir() error = %v", err)
	}
	if summary.Status != xaipipeline.ValidationStatusValid {
		t.Fatalf("Status = %q, want valid; issues=%#v", summary.Status, summary.Issues)
	}
	if validator.callCount == 0 {
		t.Fatal("validator was not called")
	}
}

func TestValidateOutputDir_RejectsCanceledContextBeforeInspect(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	validator := &validateStubOutputValidator{}
	summary, err := xaipipeline.ValidateOutputDir(ctx, filepath.Join(t.TempDir(), "missing-output"), validator)

	if !errors.Is(err, context.Canceled) {
		t.Fatalf("ValidateOutputDir() error = %v, want context.Canceled", err)
	}
	if summary.OutputDir != "" || summary.Status != "" {
		t.Fatalf("ValidateOutputDir() summary = %#v, want zero summary", summary)
	}
	if validator.called {
		t.Fatal("validator should not be called after canceled context")
	}
}

func TestValidateOutputDir_RejectsCanceledContextAfterFFprobeValidationBeforeSummary(t *testing.T) {
	t.Parallel()

	outputDir := t.TempDir()
	writeProductionValidationArtifacts(t, outputDir, true)
	ctx, cancel := context.WithCancel(context.Background())
	validator := validationOutputValidator(t, outputDir)
	validator.afterValidate = func(string) {
		cancel()
	}

	summary, err := xaipipeline.ValidateOutputDir(ctx, outputDir, validator)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("ValidateOutputDir() error = %v, want context.Canceled", err)
	}
	if summary.OutputDir != "" || summary.Status != "" {
		t.Fatalf("ValidateOutputDir() summary = %#v, want zero summary", summary)
	}
	if validator.callCount != 1 {
		t.Fatalf("validator calls = %d, want 1 before cancellation stops validation", validator.callCount)
	}
}

func TestValidateOutputDir_RejectsMismatchedReturnedFFprobeMetadata(t *testing.T) {
	t.Parallel()

	outputDir := t.TempDir()
	writeProductionValidationArtifacts(t, outputDir, true)
	validator := &validateStubOutputValidator{
		metadata: xaipipeline.RenderMetadata{
			Path:        filepath.Join(outputDir, "output_xai.mp4"),
			Width:       720,
			Height:      1280,
			FPS:         24,
			DurationSec: 8,
			CodecName:   "h264",
			PixelFormat: "yuv444p",
			SizeBytes:   validationMP4SizeBytes(),
			HasAudio:    false,
		},
	}

	summary, err := xaipipeline.ValidateOutputDir(context.Background(), outputDir, validator)
	if err != nil {
		t.Fatalf("ValidateOutputDir() error = %v", err)
	}

	if summary.Status != xaipipeline.ValidationStatusInvalid {
		t.Fatalf("Status = %q, want invalid", summary.Status)
	}
	if !validationIssuesContain(summary.Issues, `ffprobe metadata: final output pixel format "yuv444p", want "yuv420p"`) {
		t.Fatalf("Issues = %#v, want returned ffprobe pixel format issue", summary.Issues)
	}
	if summary.FFprobeMetadata != nil {
		t.Fatalf("FFprobeMetadata = %#v, want nil for invalid returned metadata", summary.FFprobeMetadata)
	}
}

func TestValidateOutputDir_RejectsMismatchedReturnedStageFFprobeMetadata(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		targetPath func(outputDir string) string
		mutate     func(metadata xaipipeline.RenderMetadata) xaipipeline.RenderMetadata
		wantIssue  string
	}{
		{
			name: "raw shot duration",
			targetPath: func(outputDir string) string {
				return filepath.Join(outputDir, "shots", "shot_001.mp4")
			},
			mutate: func(metadata xaipipeline.RenderMetadata) xaipipeline.RenderMetadata {
				metadata.DurationSec = 10
				return metadata
			},
			wantIssue: "raw shot ffprobe metadata for shot 1",
		},
		{
			name: "normalized shot dimensions",
			targetPath: func(outputDir string) string {
				return filepath.Join(outputDir, "normalized", "shot_001.mp4")
			},
			mutate: func(metadata xaipipeline.RenderMetadata) xaipipeline.RenderMetadata {
				metadata.Width = 640
				return metadata
			},
			wantIssue: "normalized shot ffprobe metadata for shot 1",
		},
		{
			name: "hyperframes timeline audio",
			targetPath: func(outputDir string) string {
				return filepath.Join(outputDir, "timeline_hyperframes.mp4")
			},
			mutate: func(metadata xaipipeline.RenderMetadata) xaipipeline.RenderMetadata {
				metadata.HasAudio = true
				return metadata
			},
			wantIssue: "hyperframes timeline ffprobe metadata",
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			outputDir := t.TempDir()
			writeProductionValidationArtifacts(t, outputDir, true)
			outputPath := filepath.Join(outputDir, "output_xai.mp4")
			validMetadata := xaipipeline.RenderMetadata{
				Path:        outputPath,
				Width:       720,
				Height:      1280,
				FPS:         24,
				DurationSec: 8,
				CodecName:   "h264",
				PixelFormat: "yuv420p",
				SizeBytes:   validationMP4SizeBytes(),
				HasAudio:    false,
			}
			targetPath := tt.targetPath(outputDir)
			targetMetadata := validMetadata
			targetMetadata.Path = targetPath
			targetMetadata = tt.mutate(targetMetadata)
			validator := &validateStubOutputValidator{
				metadata: validMetadata,
				metadataByPath: map[string]xaipipeline.RenderMetadata{
					targetPath: targetMetadata,
				},
			}

			summary, err := xaipipeline.ValidateOutputDir(context.Background(), outputDir, validator)
			if err != nil {
				t.Fatalf("ValidateOutputDir() error = %v", err)
			}

			if summary.Status != xaipipeline.ValidationStatusInvalid {
				t.Fatalf("Status = %q, want invalid", summary.Status)
			}
			if !validationIssuesContain(summary.Issues, tt.wantIssue) {
				t.Fatalf("Issues = %#v, want %q issue", summary.Issues, tt.wantIssue)
			}
		})
	}
}

func TestValidateOutputDir_RejectsReturnedFFprobeMetadataForWrongArtifactEvidence(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		targetPath func(outputDir string) string
		mutate     func(metadata xaipipeline.RenderMetadata, outputDir string) xaipipeline.RenderMetadata
		wantIssue  string
	}{
		{
			name: "final output path",
			targetPath: func(outputDir string) string {
				return filepath.Join(outputDir, "output_xai.mp4")
			},
			mutate: func(metadata xaipipeline.RenderMetadata, outputDir string) xaipipeline.RenderMetadata {
				metadata.Path = filepath.Join(outputDir, "timeline_hyperframes.mp4")
				return metadata
			},
			wantIssue: "ffprobe metadata: path:",
		},
		{
			name: "normalized shot size",
			targetPath: func(outputDir string) string {
				return filepath.Join(outputDir, "normalized", "shot_001.mp4")
			},
			mutate: func(metadata xaipipeline.RenderMetadata, _ string) xaipipeline.RenderMetadata {
				metadata.SizeBytes = 1
				return metadata
			},
			wantIssue: "normalized shot ffprobe metadata for shot 1: size_bytes:",
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			outputDir := t.TempDir()
			writeProductionValidationArtifacts(t, outputDir, true)
			outputPath := filepath.Join(outputDir, "output_xai.mp4")
			validMetadata := xaipipeline.RenderMetadata{
				Path:        outputPath,
				Width:       720,
				Height:      1280,
				FPS:         24,
				DurationSec: 8,
				CodecName:   "h264",
				PixelFormat: "yuv420p",
				SizeBytes:   validationMP4SizeBytes(),
				HasAudio:    false,
			}
			targetPath := tt.targetPath(outputDir)
			targetMetadata := validMetadata
			targetMetadata.Path = targetPath
			targetMetadata = tt.mutate(targetMetadata, outputDir)
			validator := &validateStubOutputValidator{
				metadata: validMetadata,
				metadataByPath: map[string]xaipipeline.RenderMetadata{
					targetPath: targetMetadata,
				},
			}

			summary, err := xaipipeline.ValidateOutputDir(context.Background(), outputDir, validator)
			if err != nil {
				t.Fatalf("ValidateOutputDir() error = %v", err)
			}

			if summary.Status != xaipipeline.ValidationStatusInvalid {
				t.Fatalf("Status = %q, want invalid", summary.Status)
			}
			if !validationIssuesContain(summary.Issues, tt.wantIssue) {
				t.Fatalf("Issues = %#v, want %q issue", summary.Issues, tt.wantIssue)
			}
		})
	}
}

func TestValidateOutputDir_AcceptsCompleteProductionOutputWithSubtitle(t *testing.T) {
	t.Parallel()

	outputDir := t.TempDir()
	writeProductionValidationArtifactsWithSubtitle(t, outputDir, "第一個字幕")
	writeValidationHyperFramesIndexWithSubtitle(t, filepath.Join(outputDir, "hyperframes", "index.html"), "第一個字幕")
	validator := &validateStubOutputValidator{
		metadata: xaipipeline.RenderMetadata{
			Path:        filepath.Join(outputDir, "output_xai.mp4"),
			Width:       720,
			Height:      1280,
			FPS:         24,
			DurationSec: 8,
			CodecName:   "h264",
			PixelFormat: "yuv420p",
			SizeBytes:   validationMP4SizeBytes(),
			HasAudio:    false,
		},
	}

	summary, err := xaipipeline.ValidateOutputDir(context.Background(), outputDir, validator)
	if err != nil {
		t.Fatalf("ValidateOutputDir() error = %v", err)
	}

	if summary.Status != xaipipeline.ValidationStatusValid {
		t.Fatalf("Status = %q, want %q; issues=%#v", summary.Status, xaipipeline.ValidationStatusValid, summary.Issues)
	}
}

func TestValidateOutputDir_RejectsHyperFramesProjectSubtitleTrackIndexMismatch(t *testing.T) {
	t.Parallel()

	outputDir := t.TempDir()
	writeProductionValidationArtifactsWithSubtitle(t, outputDir, "第一個字幕")
	indexPath := filepath.Join(outputDir, "hyperframes", "index.html")
	writeValidationHyperFramesIndexWithSubtitle(t, indexPath, "第一個字幕")
	replaceValidationHyperFramesIndex(t, indexPath, `data-track-index="1">第一個字幕`, `data-track-index="4">第一個字幕`)
	validator := &validateStubOutputValidator{
		metadata: xaipipeline.RenderMetadata{
			Path:        filepath.Join(outputDir, "output_xai.mp4"),
			Width:       720,
			Height:      1280,
			FPS:         24,
			DurationSec: 8,
			CodecName:   "h264",
			PixelFormat: "yuv420p",
			SizeBytes:   validationMP4SizeBytes(),
			HasAudio:    false,
		},
	}

	summary, err := xaipipeline.ValidateOutputDir(context.Background(), outputDir, validator)
	if err != nil {
		t.Fatalf("ValidateOutputDir() error = %v", err)
	}

	if summary.Status != xaipipeline.ValidationStatusInvalid {
		t.Fatalf("Status = %q, want %q", summary.Status, xaipipeline.ValidationStatusInvalid)
	}
	if !validationIssuesContain(summary.Issues, `hyperframes project subtitle clip for shot 1 missing data-track-index="1"`) {
		t.Fatalf("Issues = %#v, want HyperFrames subtitle track issue", summary.Issues)
	}
}

func TestValidateOutputDir_RejectsLegacyRemotionPropsArtifact(t *testing.T) {
	t.Parallel()

	outputDir := t.TempDir()
	writeProductionValidationArtifacts(t, outputDir, true)
	writeInspectFile(t, filepath.Join(outputDir, "remotion_props.json"))
	validator := &validateStubOutputValidator{
		metadata: xaipipeline.RenderMetadata{
			Path:        filepath.Join(outputDir, "output_xai.mp4"),
			Width:       720,
			Height:      1280,
			FPS:         24,
			DurationSec: 8,
			CodecName:   "h264",
			PixelFormat: "yuv420p",
			SizeBytes:   validationMP4SizeBytes(),
			HasAudio:    false,
		},
	}

	summary, err := xaipipeline.ValidateOutputDir(context.Background(), outputDir, validator)
	if err != nil {
		t.Fatalf("ValidateOutputDir() error = %v", err)
	}

	if summary.Status != xaipipeline.ValidationStatusInvalid {
		t.Fatalf("Status = %q, want %q", summary.Status, xaipipeline.ValidationStatusInvalid)
	}
	if !validationIssuesContain(summary.Issues, "legacy remotion_props.json artifact is not allowed") {
		t.Fatalf("Issues = %#v, want legacy remotion_props issue", summary.Issues)
	}
}

func TestValidateOutputDir_RejectsMissingStoryHash(t *testing.T) {
	t.Parallel()

	outputDir := t.TempDir()
	writeProductionValidationArtifacts(t, outputDir, true)
	promptHash := validationPromptHash("shot", 8, "9:16", "720p")
	writeInspectJSON(t, filepath.Join(outputDir, "xai_manifest.json"), xaipipeline.Manifest{
		ProjectID: "production",
		Format:    "portrait",
		FPS:       24,
		Width:     720,
		Height:    1280,
		Shots: []xaipipeline.Shot{
			{
				Index:        1,
				Prompt:       "shot",
				PromptHash:   promptHash,
				XAIRequestID: "req_123",
				XAIStatus:    "done",
				DurationSec:  8,
				VideoPath:    "shots/shot_001.mp4",
			},
		},
	})
	validator := &validateStubOutputValidator{
		metadata: xaipipeline.RenderMetadata{
			Path:        filepath.Join(outputDir, "output_xai.mp4"),
			Width:       720,
			Height:      1280,
			FPS:         24,
			DurationSec: 8,
			CodecName:   "h264",
			PixelFormat: "yuv420p",
			SizeBytes:   validationMP4SizeBytes(),
			HasAudio:    false,
		},
	}

	summary, err := xaipipeline.ValidateOutputDir(context.Background(), outputDir, validator)
	if err != nil {
		t.Fatalf("ValidateOutputDir() error = %v", err)
	}

	if summary.Status != xaipipeline.ValidationStatusInvalid {
		t.Fatalf("Status = %q, want %q", summary.Status, xaipipeline.ValidationStatusInvalid)
	}
	if !validationIssuesContain(summary.Issues, "story_hash") {
		t.Fatalf("Issues = %#v, want missing story_hash issue", summary.Issues)
	}
}

func TestValidateOutputDir_RejectsInvalidStoryHash(t *testing.T) {
	t.Parallel()

	outputDir := t.TempDir()
	writeProductionValidationArtifacts(t, outputDir, true)
	promptHash := validationPromptHash("shot", 8, "9:16", "720p")
	writeInspectJSON(t, filepath.Join(outputDir, "xai_manifest.json"), xaipipeline.Manifest{
		ProjectID: "production",
		StoryHash: "story-hash",
		Format:    "portrait",
		FPS:       24,
		Width:     720,
		Height:    1280,
		Shots: []xaipipeline.Shot{
			{
				Index:        1,
				Prompt:       "shot",
				PromptHash:   promptHash,
				XAIRequestID: "req_123",
				XAIStatus:    "done",
				DurationSec:  8,
				VideoPath:    "shots/shot_001.mp4",
			},
		},
	})
	validator := &validateStubOutputValidator{
		metadata: xaipipeline.RenderMetadata{
			Path:        filepath.Join(outputDir, "output_xai.mp4"),
			Width:       720,
			Height:      1280,
			FPS:         24,
			DurationSec: 8,
			CodecName:   "h264",
			PixelFormat: "yuv420p",
			SizeBytes:   validationMP4SizeBytes(),
			HasAudio:    false,
		},
	}

	summary, err := xaipipeline.ValidateOutputDir(context.Background(), outputDir, validator)
	if err != nil {
		t.Fatalf("ValidateOutputDir() error = %v", err)
	}

	if summary.Status != xaipipeline.ValidationStatusInvalid {
		t.Fatalf("Status = %q, want %q", summary.Status, xaipipeline.ValidationStatusInvalid)
	}
	if !validationIssuesContain(summary.Issues, "story_hash") {
		t.Fatalf("Issues = %#v, want invalid story_hash issue", summary.Issues)
	}
}

func TestValidateOutputDir_RejectsUnsafeProjectID(t *testing.T) {
	t.Parallel()

	outputDir := t.TempDir()
	writeProductionValidationArtifacts(t, outputDir, true)
	promptHash := validationPromptHash("shot", 8, "9:16", "720p")
	writeInspectJSON(t, filepath.Join(outputDir, "xai_manifest.json"), xaipipeline.Manifest{
		ProjectID: "../escaped",
		StoryHash: validValidationStoryHash,
		Format:    "portrait",
		FPS:       24,
		Width:     720,
		Height:    1280,
		Shots: []xaipipeline.Shot{
			{
				Index:        1,
				Prompt:       "shot",
				PromptHash:   promptHash,
				XAIRequestID: "req_123",
				XAIStatus:    "done",
				DurationSec:  8,
				AspectRatio:  "9:16",
				Resolution:   "720p",
				VideoPath:    "shots/shot_001.mp4",
			},
		},
	})
	validator := &validateStubOutputValidator{
		metadata: xaipipeline.RenderMetadata{
			Path:        filepath.Join(outputDir, "output_xai.mp4"),
			Width:       720,
			Height:      1280,
			FPS:         24,
			DurationSec: 8,
			CodecName:   "h264",
			PixelFormat: "yuv420p",
			SizeBytes:   validationMP4SizeBytes(),
			HasAudio:    false,
		},
	}

	summary, err := xaipipeline.ValidateOutputDir(context.Background(), outputDir, validator)
	if err != nil {
		t.Fatalf("ValidateOutputDir() error = %v", err)
	}

	if summary.Status != xaipipeline.ValidationStatusInvalid {
		t.Fatalf("Status = %q, want %q", summary.Status, xaipipeline.ValidationStatusInvalid)
	}
	if !validationIssuesContain(summary.Issues, "project_id") {
		t.Fatalf("Issues = %#v, want unsafe project_id issue", summary.Issues)
	}
}

func TestValidateOutputDir_RejectsPartialInspect(t *testing.T) {
	t.Parallel()

	outputDir := t.TempDir()
	writeInspectJSON(t, filepath.Join(outputDir, "xai_manifest.json"), xaipipeline.Manifest{
		ProjectID: "partial",
		Format:    "portrait",
		FPS:       24,
		Width:     720,
		Height:    1280,
		Shots: []xaipipeline.Shot{
			{Index: 1, Prompt: "shot", DurationSec: 8},
		},
	})
	validator := &validateStubOutputValidator{}

	summary, err := xaipipeline.ValidateOutputDir(context.Background(), outputDir, validator)
	if err != nil {
		t.Fatalf("ValidateOutputDir() error = %v", err)
	}

	if summary.Status != xaipipeline.ValidationStatusInvalid {
		t.Fatalf("Status = %q, want %q", summary.Status, xaipipeline.ValidationStatusInvalid)
	}
	if len(summary.Issues) == 0 {
		t.Fatal("Issues is empty, want inspect issue")
	}
	if validator.called {
		t.Fatal("ffprobe validator should not run when inspect is incomplete")
	}
}

func TestValidateOutputDir_RejectsMissingRequestMetadata(t *testing.T) {
	t.Parallel()

	outputDir := t.TempDir()
	writeProductionValidationArtifacts(t, outputDir, false)
	validator := &validateStubOutputValidator{
		metadata: xaipipeline.RenderMetadata{
			Path:        filepath.Join(outputDir, "output_xai.mp4"),
			Width:       720,
			Height:      1280,
			FPS:         24,
			DurationSec: 8,
			CodecName:   "h264",
			PixelFormat: "yuv420p",
			SizeBytes:   validationMP4SizeBytes(),
			HasAudio:    false,
		},
	}

	summary, err := xaipipeline.ValidateOutputDir(context.Background(), outputDir, validator)
	if err != nil {
		t.Fatalf("ValidateOutputDir() error = %v", err)
	}

	if summary.Status != xaipipeline.ValidationStatusInvalid {
		t.Fatalf("Status = %q, want %q", summary.Status, xaipipeline.ValidationStatusInvalid)
	}
	if len(summary.Issues) == 0 {
		t.Fatal("Issues is empty, want metadata issue")
	}
}

func TestValidateOutputDir_RejectsNonCanonicalXAIRequestID(t *testing.T) {
	t.Parallel()

	outputDir := t.TempDir()
	writeValidationArtifactsWithProviderMetadata(t, outputDir, " req_123 ", "done")
	validator := &validateStubOutputValidator{
		metadata: xaipipeline.RenderMetadata{
			Path:        filepath.Join(outputDir, "output_xai.mp4"),
			Width:       720,
			Height:      1280,
			FPS:         24,
			DurationSec: 8,
			CodecName:   "h264",
			PixelFormat: "yuv420p",
			SizeBytes:   validationMP4SizeBytes(),
			HasAudio:    false,
		},
	}

	summary, err := xaipipeline.ValidateOutputDir(context.Background(), outputDir, validator)
	if err != nil {
		t.Fatalf("ValidateOutputDir() error = %v", err)
	}

	if summary.Status != xaipipeline.ValidationStatusInvalid {
		t.Fatalf("Status = %q, want %q", summary.Status, xaipipeline.ValidationStatusInvalid)
	}
	if !validationIssuesContain(summary.Issues, `shot 1 xai_request_id " req_123 " is not canonical, want "req_123"`) {
		t.Fatalf("Issues = %#v, want manifest xai_request_id canonical issue", summary.Issues)
	}
}

func TestValidateOutputDir_RejectsNonCanonicalXAIStatus(t *testing.T) {
	t.Parallel()

	outputDir := t.TempDir()
	writeValidationArtifactsWithProviderMetadata(t, outputDir, "req_123", " Done ")
	validator := &validateStubOutputValidator{
		metadata: xaipipeline.RenderMetadata{
			Path:        filepath.Join(outputDir, "output_xai.mp4"),
			Width:       720,
			Height:      1280,
			FPS:         24,
			DurationSec: 8,
			CodecName:   "h264",
			PixelFormat: "yuv420p",
			SizeBytes:   validationMP4SizeBytes(),
			HasAudio:    false,
		},
	}

	summary, err := xaipipeline.ValidateOutputDir(context.Background(), outputDir, validator)
	if err != nil {
		t.Fatalf("ValidateOutputDir() error = %v", err)
	}

	if summary.Status != xaipipeline.ValidationStatusInvalid {
		t.Fatalf("Status = %q, want %q", summary.Status, xaipipeline.ValidationStatusInvalid)
	}
	if !validationIssuesContain(summary.Issues, `shot 1 xai_status " Done " is not canonical, want "done"`) {
		t.Fatalf("Issues = %#v, want manifest xai_status canonical issue", summary.Issues)
	}
}

func TestValidateOutputDir_RejectsStaleRunMetadataDecision(t *testing.T) {
	t.Parallel()

	outputDir := t.TempDir()
	writeProductionValidationArtifacts(t, outputDir, true)
	writeInspectJSON(t, filepath.Join(outputDir, "xai_run_metadata.json"), xaipipeline.RunMetadata{
		Planned:        true,
		GeneratedShots: []int{1},
		ShotDecisions: []xaipipeline.ShotDecision{
			{
				Index:        1,
				Decision:     "generated",
				VideoPath:    "shots/shot_001.mp4",
				PromptHash:   "stale-prompt-hash",
				XAIRequestID: "req_123",
				XAIStatus:    "done",
			},
		},
	})
	validator := &validateStubOutputValidator{
		metadata: xaipipeline.RenderMetadata{
			Path:        filepath.Join(outputDir, "output_xai.mp4"),
			Width:       720,
			Height:      1280,
			FPS:         24,
			DurationSec: 8,
			CodecName:   "h264",
			PixelFormat: "yuv420p",
			SizeBytes:   validationMP4SizeBytes(),
			HasAudio:    false,
		},
	}

	summary, err := xaipipeline.ValidateOutputDir(context.Background(), outputDir, validator)
	if err != nil {
		t.Fatalf("ValidateOutputDir() error = %v", err)
	}

	if summary.Status != xaipipeline.ValidationStatusInvalid {
		t.Fatalf("Status = %q, want %q", summary.Status, xaipipeline.ValidationStatusInvalid)
	}
	if !validationIssuesContain(summary.Issues, "prompt_hash") {
		t.Fatalf("Issues = %#v, want stale run metadata prompt_hash issue", summary.Issues)
	}
}

func TestValidateOutputDir_RejectsNonCanonicalRunMetadataDecision(t *testing.T) {
	t.Parallel()

	outputDir := t.TempDir()
	writeProductionValidationArtifacts(t, outputDir, true)
	promptHash := validationPromptHash("shot", 8, "9:16", "720p")
	writeInspectJSON(t, filepath.Join(outputDir, "xai_run_metadata.json"), xaipipeline.RunMetadata{
		Planned:        true,
		GeneratedShots: []int{1},
		ShotDecisions: []xaipipeline.ShotDecision{
			{
				Index:        1,
				Decision:     " generated ",
				VideoPath:    "shots/shot_001.mp4",
				PromptHash:   promptHash,
				XAIRequestID: "req_123",
				XAIStatus:    "done",
			},
		},
	})
	validator := &validateStubOutputValidator{
		metadata: xaipipeline.RenderMetadata{
			Path:        filepath.Join(outputDir, "output_xai.mp4"),
			Width:       720,
			Height:      1280,
			FPS:         24,
			DurationSec: 8,
			CodecName:   "h264",
			PixelFormat: "yuv420p",
			SizeBytes:   validationMP4SizeBytes(),
			HasAudio:    false,
		},
	}

	summary, err := xaipipeline.ValidateOutputDir(context.Background(), outputDir, validator)
	if err != nil {
		t.Fatalf("ValidateOutputDir() error = %v", err)
	}

	if summary.Status != xaipipeline.ValidationStatusInvalid {
		t.Fatalf("Status = %q, want %q", summary.Status, xaipipeline.ValidationStatusInvalid)
	}
	if !validationIssuesContain(summary.Issues, `run metadata shot 1 decision " generated " is not canonical, want "generated"`) {
		t.Fatalf("Issues = %#v, want run metadata decision canonical issue", summary.Issues)
	}
}

func TestValidateOutputDir_RejectsNonCanonicalRunMetadataVideoPath(t *testing.T) {
	t.Parallel()

	outputDir := t.TempDir()
	writeProductionValidationArtifacts(t, outputDir, true)
	promptHash := validationPromptHash("shot", 8, "9:16", "720p")
	writeInspectJSON(t, filepath.Join(outputDir, "xai_run_metadata.json"), xaipipeline.RunMetadata{
		Planned:        true,
		GeneratedShots: []int{1},
		ShotDecisions: []xaipipeline.ShotDecision{
			{
				Index:        1,
				Decision:     "generated",
				VideoPath:    " shots/shot_001.mp4 ",
				PromptHash:   promptHash,
				XAIRequestID: "req_123",
				XAIStatus:    "done",
			},
		},
	})
	validator := &validateStubOutputValidator{
		metadata: xaipipeline.RenderMetadata{
			Path:        filepath.Join(outputDir, "output_xai.mp4"),
			Width:       720,
			Height:      1280,
			FPS:         24,
			DurationSec: 8,
			CodecName:   "h264",
			PixelFormat: "yuv420p",
			SizeBytes:   validationMP4SizeBytes(),
			HasAudio:    false,
		},
	}

	summary, err := xaipipeline.ValidateOutputDir(context.Background(), outputDir, validator)
	if err != nil {
		t.Fatalf("ValidateOutputDir() error = %v", err)
	}

	if summary.Status != xaipipeline.ValidationStatusInvalid {
		t.Fatalf("Status = %q, want %q", summary.Status, xaipipeline.ValidationStatusInvalid)
	}
	if !validationIssuesContain(summary.Issues, `run metadata shot 1 video_path " shots/shot_001.mp4 " is not canonical, want "shots/shot_001.mp4"`) {
		t.Fatalf("Issues = %#v, want run metadata video_path canonical issue", summary.Issues)
	}
}

func TestValidateOutputDir_RejectsNonCanonicalRunMetadataPromptHash(t *testing.T) {
	t.Parallel()

	outputDir := t.TempDir()
	writeProductionValidationArtifacts(t, outputDir, true)
	promptHash := validationPromptHash("shot", 8, "9:16", "720p")
	writeInspectJSON(t, filepath.Join(outputDir, "xai_run_metadata.json"), xaipipeline.RunMetadata{
		Planned:        true,
		GeneratedShots: []int{1},
		ShotDecisions: []xaipipeline.ShotDecision{
			{
				Index:        1,
				Decision:     "generated",
				VideoPath:    "shots/shot_001.mp4",
				PromptHash:   " " + promptHash + " ",
				XAIRequestID: "req_123",
				XAIStatus:    "done",
			},
		},
	})
	validator := &validateStubOutputValidator{
		metadata: xaipipeline.RenderMetadata{
			Path:        filepath.Join(outputDir, "output_xai.mp4"),
			Width:       720,
			Height:      1280,
			FPS:         24,
			DurationSec: 8,
			CodecName:   "h264",
			PixelFormat: "yuv420p",
			SizeBytes:   validationMP4SizeBytes(),
			HasAudio:    false,
		},
	}

	summary, err := xaipipeline.ValidateOutputDir(context.Background(), outputDir, validator)
	if err != nil {
		t.Fatalf("ValidateOutputDir() error = %v", err)
	}

	if summary.Status != xaipipeline.ValidationStatusInvalid {
		t.Fatalf("Status = %q, want %q", summary.Status, xaipipeline.ValidationStatusInvalid)
	}
	if !validationIssuesContain(summary.Issues, `run metadata shot 1 prompt_hash " `+promptHash+` " is not canonical, want "`+promptHash+`"`) {
		t.Fatalf("Issues = %#v, want run metadata prompt_hash canonical issue", summary.Issues)
	}
}

func TestValidateOutputDir_RejectsMissingPromptHash(t *testing.T) {
	t.Parallel()

	outputDir := t.TempDir()
	writeProductionValidationArtifacts(t, outputDir, true)
	writeInspectJSON(t, filepath.Join(outputDir, "xai_manifest.json"), xaipipeline.Manifest{
		ProjectID: "production",
		Format:    "portrait",
		FPS:       24,
		Width:     720,
		Height:    1280,
		Shots: []xaipipeline.Shot{
			{
				Index:        1,
				Prompt:       "shot",
				XAIRequestID: "req_123",
				XAIStatus:    "done",
				DurationSec:  8,
				AspectRatio:  "9:16",
				Resolution:   "720p",
				VideoPath:    "shots/shot_001.mp4",
			},
		},
	})
	writeInspectJSON(t, filepath.Join(outputDir, "xai_run_metadata.json"), xaipipeline.RunMetadata{
		Planned:        true,
		GeneratedShots: []int{1},
		ShotDecisions: []xaipipeline.ShotDecision{
			{
				Index:        1,
				Decision:     "generated",
				VideoPath:    "shots/shot_001.mp4",
				XAIRequestID: "req_123",
				XAIStatus:    "done",
			},
		},
	})
	validator := &validateStubOutputValidator{
		metadata: xaipipeline.RenderMetadata{
			Path:        filepath.Join(outputDir, "output_xai.mp4"),
			Width:       720,
			Height:      1280,
			FPS:         24,
			DurationSec: 8,
			CodecName:   "h264",
			PixelFormat: "yuv420p",
			SizeBytes:   validationMP4SizeBytes(),
			HasAudio:    false,
		},
	}

	summary, err := xaipipeline.ValidateOutputDir(context.Background(), outputDir, validator)
	if err != nil {
		t.Fatalf("ValidateOutputDir() error = %v", err)
	}

	if summary.Status != xaipipeline.ValidationStatusInvalid {
		t.Fatalf("Status = %q, want %q", summary.Status, xaipipeline.ValidationStatusInvalid)
	}
	if !validationIssuesContain(summary.Issues, "prompt_hash") {
		t.Fatalf("Issues = %#v, want missing prompt_hash issue", summary.Issues)
	}
}

func TestValidateOutputDir_RejectsNonCanonicalManifestPromptHash(t *testing.T) {
	t.Parallel()

	outputDir := t.TempDir()
	writeProductionValidationArtifacts(t, outputDir, true)
	promptHash := validationPromptHash("shot", 8, "9:16", "720p")
	writeInspectJSON(t, filepath.Join(outputDir, "xai_manifest.json"), xaipipeline.Manifest{
		ProjectID: "production",
		StoryHash: validValidationStoryHash,
		Format:    "portrait",
		FPS:       24,
		Width:     720,
		Height:    1280,
		Shots: []xaipipeline.Shot{
			{
				Index:        1,
				Prompt:       "shot",
				PromptHash:   " " + promptHash + " ",
				XAIRequestID: "req_123",
				XAIStatus:    "done",
				DurationSec:  8,
				AspectRatio:  "9:16",
				Resolution:   "720p",
				VideoPath:    "shots/shot_001.mp4",
			},
		},
	})
	writeInspectJSON(t, filepath.Join(outputDir, "xai_run_metadata.json"), xaipipeline.RunMetadata{
		Planned:        true,
		GeneratedShots: []int{1},
		ShotDecisions: []xaipipeline.ShotDecision{
			{
				Index:        1,
				Decision:     "generated",
				VideoPath:    "shots/shot_001.mp4",
				PromptHash:   promptHash,
				XAIRequestID: "req_123",
				XAIStatus:    "done",
			},
		},
	})
	validator := &validateStubOutputValidator{
		metadata: xaipipeline.RenderMetadata{
			Path:        filepath.Join(outputDir, "output_xai.mp4"),
			Width:       720,
			Height:      1280,
			FPS:         24,
			DurationSec: 8,
			CodecName:   "h264",
			PixelFormat: "yuv420p",
			SizeBytes:   validationMP4SizeBytes(),
			HasAudio:    false,
		},
	}

	summary, err := xaipipeline.ValidateOutputDir(context.Background(), outputDir, validator)
	if err != nil {
		t.Fatalf("ValidateOutputDir() error = %v", err)
	}

	if summary.Status != xaipipeline.ValidationStatusInvalid {
		t.Fatalf("Status = %q, want %q", summary.Status, xaipipeline.ValidationStatusInvalid)
	}
	if !validationIssuesContain(summary.Issues, `shot 1 prompt_hash " `+promptHash+` " is not canonical, want "`+promptHash+`"`) {
		t.Fatalf("Issues = %#v, want manifest prompt_hash canonical issue", summary.Issues)
	}
}

func TestValidateOutputDir_RejectsInvalidPromptHash(t *testing.T) {
	t.Parallel()

	outputDir := t.TempDir()
	writeProductionValidationArtifacts(t, outputDir, true)
	writeInspectJSON(t, filepath.Join(outputDir, "xai_manifest.json"), xaipipeline.Manifest{
		ProjectID: "production",
		Format:    "portrait",
		FPS:       24,
		Width:     720,
		Height:    1280,
		Shots: []xaipipeline.Shot{
			{
				Index:        1,
				Prompt:       "shot",
				PromptHash:   "fake-prompt-hash",
				XAIRequestID: "req_123",
				XAIStatus:    "done",
				DurationSec:  8,
				AspectRatio:  "9:16",
				Resolution:   "720p",
				VideoPath:    "shots/shot_001.mp4",
			},
		},
	})
	writeInspectJSON(t, filepath.Join(outputDir, "xai_run_metadata.json"), xaipipeline.RunMetadata{
		Planned:        true,
		GeneratedShots: []int{1},
		ShotDecisions: []xaipipeline.ShotDecision{
			{
				Index:        1,
				Decision:     "generated",
				VideoPath:    "shots/shot_001.mp4",
				PromptHash:   "fake-prompt-hash",
				XAIRequestID: "req_123",
				XAIStatus:    "done",
			},
		},
	})
	validator := &validateStubOutputValidator{
		metadata: xaipipeline.RenderMetadata{
			Path:        filepath.Join(outputDir, "output_xai.mp4"),
			Width:       720,
			Height:      1280,
			FPS:         24,
			DurationSec: 8,
			CodecName:   "h264",
			PixelFormat: "yuv420p",
			SizeBytes:   validationMP4SizeBytes(),
			HasAudio:    false,
		},
	}

	summary, err := xaipipeline.ValidateOutputDir(context.Background(), outputDir, validator)
	if err != nil {
		t.Fatalf("ValidateOutputDir() error = %v", err)
	}

	if summary.Status != xaipipeline.ValidationStatusInvalid {
		t.Fatalf("Status = %q, want %q", summary.Status, xaipipeline.ValidationStatusInvalid)
	}
	if !validationIssuesContain(summary.Issues, "prompt_hash") {
		t.Fatalf("Issues = %#v, want invalid prompt_hash issue", summary.Issues)
	}
}

func TestValidateOutputDir_RejectsPromptHashFromDifferentVideoModel(t *testing.T) {
	t.Parallel()

	outputDir := t.TempDir()
	manifest := writeProductionValidationArtifacts(t, outputDir, true)
	manifest.VideoModel = "grok-imagine-video-v2"
	writeInspectJSON(t, filepath.Join(outputDir, "xai_manifest.json"), manifest)
	writeValidationRenderMetadata(t, outputDir, manifest, 8)

	summary, err := xaipipeline.ValidateOutputDir(context.Background(), outputDir, validationOutputValidator(t, outputDir))
	if err != nil {
		t.Fatalf("ValidateOutputDir() error = %v", err)
	}
	if summary.Status != xaipipeline.ValidationStatusInvalid {
		t.Fatalf("Status = %q, want invalid", summary.Status)
	}
	if !validationIssuesContain(summary.Issues, "prompt_hash") {
		t.Fatalf("Issues = %#v, want prompt_hash model mismatch issue", summary.Issues)
	}
}

func TestValidateOutputDir_RejectsMissingManifestVideoModel(t *testing.T) {
	t.Parallel()

	outputDir := t.TempDir()
	manifest := writeProductionValidationArtifacts(t, outputDir, true)
	manifest.VideoModel = ""
	writeInspectJSON(t, filepath.Join(outputDir, "xai_manifest.json"), manifest)
	writeValidationRenderMetadata(t, outputDir, manifest, 8)

	summary, err := xaipipeline.ValidateOutputDir(context.Background(), outputDir, validationOutputValidator(t, outputDir))
	if err != nil {
		t.Fatalf("ValidateOutputDir() error = %v", err)
	}
	if summary.Status != xaipipeline.ValidationStatusInvalid {
		t.Fatalf("Status = %q, want invalid", summary.Status)
	}
	if !validationIssuesContain(summary.Issues, "manifest missing video_model") {
		t.Fatalf("Issues = %#v, want missing manifest video_model issue", summary.Issues)
	}
}

func TestValidateOutputDir_RejectsRunMetadataVideoModelMismatch(t *testing.T) {
	t.Parallel()

	outputDir := t.TempDir()
	manifest := writeProductionValidationArtifacts(t, outputDir, true)
	writeInspectJSON(t, filepath.Join(outputDir, "xai_run_metadata.json"), xaipipeline.RunMetadata{
		Planned:        true,
		VideoModel:     "grok-imagine-video-other",
		GeneratedShots: []int{1},
		ShotDecisions: []xaipipeline.ShotDecision{
			{
				Index:        1,
				Decision:     "generated",
				VideoPath:    "shots/shot_001.mp4",
				PromptHash:   manifest.Shots[0].PromptHash,
				XAIRequestID: "req_123",
				XAIStatus:    "done",
			},
		},
	})

	summary, err := xaipipeline.ValidateOutputDir(context.Background(), outputDir, validationOutputValidator(t, outputDir))
	if err != nil {
		t.Fatalf("ValidateOutputDir() error = %v", err)
	}
	if summary.Status != xaipipeline.ValidationStatusInvalid {
		t.Fatalf("Status = %q, want invalid", summary.Status)
	}
	if !validationIssuesContain(summary.Issues, `run metadata video_model="grok-imagine-video-other", want manifest video_model "grok-imagine-video"`) {
		t.Fatalf("Issues = %#v, want run metadata video_model mismatch issue", summary.Issues)
	}
}

func TestValidateOutputDir_RejectsMissingRunMetadataVideoModel(t *testing.T) {
	t.Parallel()

	outputDir := t.TempDir()
	manifest := writeProductionValidationArtifacts(t, outputDir, true)
	writeInspectJSON(t, filepath.Join(outputDir, "xai_run_metadata.json"), xaipipeline.RunMetadata{
		Planned:        true,
		GeneratedShots: []int{1},
		ShotDecisions: []xaipipeline.ShotDecision{
			{
				Index:        1,
				Decision:     "generated",
				VideoPath:    "shots/shot_001.mp4",
				PromptHash:   manifest.Shots[0].PromptHash,
				XAIRequestID: "req_123",
				XAIStatus:    "done",
			},
		},
	})

	summary, err := xaipipeline.ValidateOutputDir(context.Background(), outputDir, validationOutputValidator(t, outputDir))
	if err != nil {
		t.Fatalf("ValidateOutputDir() error = %v", err)
	}
	if summary.Status != xaipipeline.ValidationStatusInvalid {
		t.Fatalf("Status = %q, want invalid", summary.Status)
	}
	if !validationIssuesContain(summary.Issues, "run metadata missing video_model") {
		t.Fatalf("Issues = %#v, want missing run metadata video_model issue", summary.Issues)
	}
}

func TestValidateOutputDir_RejectsNonCanonicalManifestPrompt(t *testing.T) {
	t.Parallel()

	outputDir := t.TempDir()
	writeProductionValidationArtifacts(t, outputDir, true)
	promptHash := validationPromptHash("shot", 8, "9:16", "720p")
	writeInspectJSON(t, filepath.Join(outputDir, "xai_manifest.json"), xaipipeline.Manifest{
		ProjectID: "production",
		StoryHash: validValidationStoryHash,
		Format:    "portrait",
		FPS:       24,
		Width:     720,
		Height:    1280,
		Shots: []xaipipeline.Shot{
			{
				Index:        1,
				Prompt:       " shot ",
				PromptHash:   promptHash,
				XAIRequestID: "req_123",
				XAIStatus:    "done",
				DurationSec:  8,
				AspectRatio:  "9:16",
				Resolution:   "720p",
				VideoPath:    "shots/shot_001.mp4",
			},
		},
	})
	validator := &validateStubOutputValidator{
		metadata: xaipipeline.RenderMetadata{
			Path:        filepath.Join(outputDir, "output_xai.mp4"),
			Width:       720,
			Height:      1280,
			FPS:         24,
			DurationSec: 8,
			CodecName:   "h264",
			PixelFormat: "yuv420p",
			SizeBytes:   validationMP4SizeBytes(),
			HasAudio:    false,
		},
	}

	summary, err := xaipipeline.ValidateOutputDir(context.Background(), outputDir, validator)
	if err != nil {
		t.Fatalf("ValidateOutputDir() error = %v", err)
	}

	if summary.Status != xaipipeline.ValidationStatusInvalid {
		t.Fatalf("Status = %q, want %q", summary.Status, xaipipeline.ValidationStatusInvalid)
	}
	if !validationIssuesContain(summary.Issues, `manifest shot 1 prompt " shot " is not canonical, want "shot"`) {
		t.Fatalf("Issues = %#v, want manifest prompt canonical issue", summary.Issues)
	}
}

func TestValidateOutputDir_RejectsNonCanonicalManifestFormat(t *testing.T) {
	t.Parallel()

	outputDir := t.TempDir()
	writeProductionValidationArtifacts(t, outputDir, true)
	promptHash := validationPromptHash("shot", 8, "9:16", "720p")
	writeInspectJSON(t, filepath.Join(outputDir, "xai_manifest.json"), xaipipeline.Manifest{
		ProjectID: "production",
		StoryHash: validValidationStoryHash,
		Format:    " portrait ",
		FPS:       24,
		Width:     720,
		Height:    1280,
		Shots: []xaipipeline.Shot{
			{
				Index:        1,
				Prompt:       "shot",
				PromptHash:   promptHash,
				XAIRequestID: "req_123",
				XAIStatus:    "done",
				DurationSec:  8,
				AspectRatio:  "9:16",
				Resolution:   "720p",
				VideoPath:    "shots/shot_001.mp4",
			},
		},
	})
	validator := &validateStubOutputValidator{
		metadata: xaipipeline.RenderMetadata{
			Path:        filepath.Join(outputDir, "output_xai.mp4"),
			Width:       720,
			Height:      1280,
			FPS:         24,
			DurationSec: 8,
			CodecName:   "h264",
			PixelFormat: "yuv420p",
			SizeBytes:   validationMP4SizeBytes(),
			HasAudio:    false,
		},
	}

	summary, err := xaipipeline.ValidateOutputDir(context.Background(), outputDir, validator)
	if err != nil {
		t.Fatalf("ValidateOutputDir() error = %v", err)
	}

	if summary.Status != xaipipeline.ValidationStatusInvalid {
		t.Fatalf("Status = %q, want %q", summary.Status, xaipipeline.ValidationStatusInvalid)
	}
	if !validationIssuesContain(summary.Issues, `manifest format " portrait " is not canonical, want "portrait"`) {
		t.Fatalf("Issues = %#v, want manifest format canonical issue", summary.Issues)
	}
}

func TestValidateOutputDir_RejectsNonPositiveManifestDuration(t *testing.T) {
	t.Parallel()

	outputDir := t.TempDir()
	writeProductionValidationArtifacts(t, outputDir, true)
	promptHash := validationPromptHash("shot", 8, "9:16", "720p")
	writeInspectJSON(t, filepath.Join(outputDir, "xai_manifest.json"), xaipipeline.Manifest{
		ProjectID: "production",
		StoryHash: validValidationStoryHash,
		Format:    "portrait",
		FPS:       24,
		Width:     720,
		Height:    1280,
		Shots: []xaipipeline.Shot{
			{
				Index:        1,
				Prompt:       "shot",
				PromptHash:   promptHash,
				XAIRequestID: "req_123",
				XAIStatus:    "done",
				DurationSec:  0,
				AspectRatio:  "9:16",
				Resolution:   "720p",
				VideoPath:    "shots/shot_001.mp4",
			},
		},
	})
	validator := &validateStubOutputValidator{
		metadata: xaipipeline.RenderMetadata{
			Path:        filepath.Join(outputDir, "output_xai.mp4"),
			Width:       720,
			Height:      1280,
			FPS:         24,
			DurationSec: 8,
			CodecName:   "h264",
			PixelFormat: "yuv420p",
			SizeBytes:   validationMP4SizeBytes(),
			HasAudio:    false,
		},
	}

	summary, err := xaipipeline.ValidateOutputDir(context.Background(), outputDir, validator)
	if err != nil {
		t.Fatalf("ValidateOutputDir() error = %v", err)
	}

	if summary.Status != xaipipeline.ValidationStatusInvalid {
		t.Fatalf("Status = %q, want %q", summary.Status, xaipipeline.ValidationStatusInvalid)
	}
	if !validationIssuesContain(summary.Issues, `manifest shot 1 duration_sec 0.000, want positive duration`) {
		t.Fatalf("Issues = %#v, want manifest duration issue", summary.Issues)
	}
}

func TestValidateOutputDir_RejectsInconsistentRunMetadataShotSets(t *testing.T) {
	t.Parallel()

	outputDir := t.TempDir()
	writeProductionValidationArtifacts(t, outputDir, true)
	writeInspectJSON(t, filepath.Join(outputDir, "xai_run_metadata.json"), xaipipeline.RunMetadata{
		Planned:        true,
		GeneratedShots: []int{1},
		ShotDecisions: []xaipipeline.ShotDecision{
			{
				Index:        1,
				Decision:     "reused",
				VideoPath:    "shots/shot_001.mp4",
				PromptHash:   "prompt-hash",
				XAIRequestID: "req_123",
				XAIStatus:    "done",
			},
		},
	})
	validator := &validateStubOutputValidator{
		metadata: xaipipeline.RenderMetadata{
			Path:        filepath.Join(outputDir, "output_xai.mp4"),
			Width:       720,
			Height:      1280,
			FPS:         24,
			DurationSec: 8,
			CodecName:   "h264",
			PixelFormat: "yuv420p",
			SizeBytes:   validationMP4SizeBytes(),
			HasAudio:    false,
		},
	}

	summary, err := xaipipeline.ValidateOutputDir(context.Background(), outputDir, validator)
	if err != nil {
		t.Fatalf("ValidateOutputDir() error = %v", err)
	}

	if summary.Status != xaipipeline.ValidationStatusInvalid {
		t.Fatalf("Status = %q, want %q", summary.Status, xaipipeline.ValidationStatusInvalid)
	}
	if !validationIssuesContain(summary.Issues, "generated_shots") {
		t.Fatalf("Issues = %#v, want inconsistent generated_shots issue", summary.Issues)
	}
}

func TestValidateOutputDir_RejectsInconsistentRunMetadataOrigin(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name           string
		planned        bool
		manifestReused bool
		forceReplan    bool
		wantIssue      string
	}{
		{
			name:           "planned and reused",
			planned:        true,
			manifestReused: true,
			wantIssue:      "planned and manifest_reused cannot both be true",
		},
		{
			name:           "missing origin",
			planned:        false,
			manifestReused: false,
			wantIssue:      "must set exactly one of planned or manifest_reused",
		},
		{
			name:           "force replan with reused manifest",
			planned:        false,
			manifestReused: true,
			forceReplan:    true,
			wantIssue:      "force_replan cannot reuse manifest",
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			outputDir := t.TempDir()
			writeProductionValidationArtifacts(t, outputDir, true)
			promptHash := validationPromptHash("shot", 8, "9:16", "720p")
			writeInspectJSON(t, filepath.Join(outputDir, "xai_run_metadata.json"), xaipipeline.RunMetadata{
				Planned:        tt.planned,
				ManifestReused: tt.manifestReused,
				ForceReplan:    tt.forceReplan,
				GeneratedShots: []int{1},
				ShotDecisions: []xaipipeline.ShotDecision{
					{
						Index:        1,
						Decision:     "generated",
						VideoPath:    "shots/shot_001.mp4",
						PromptHash:   promptHash,
						XAIRequestID: "req_123",
						XAIStatus:    "done",
					},
				},
			})
			validator := &validateStubOutputValidator{
				metadata: xaipipeline.RenderMetadata{
					Path:        filepath.Join(outputDir, "output_xai.mp4"),
					Width:       720,
					Height:      1280,
					FPS:         24,
					DurationSec: 8,
					CodecName:   "h264",
					SizeBytes:   validationMP4SizeBytes(),
					HasAudio:    false,
				},
			}

			summary, err := xaipipeline.ValidateOutputDir(context.Background(), outputDir, validator)
			if err != nil {
				t.Fatalf("ValidateOutputDir() error = %v", err)
			}

			if summary.Status != xaipipeline.ValidationStatusInvalid {
				t.Fatalf("Status = %q, want %q", summary.Status, xaipipeline.ValidationStatusInvalid)
			}
			if !validationIssuesContain(summary.Issues, tt.wantIssue) {
				t.Fatalf("Issues = %#v, want %q issue", summary.Issues, tt.wantIssue)
			}
		})
	}
}

func TestValidateOutputDir_RejectsForceRegenerateReusedShots(t *testing.T) {
	t.Parallel()

	outputDir := t.TempDir()
	writeProductionValidationArtifacts(t, outputDir, true)
	promptHash := validationPromptHash("shot", 8, "9:16", "720p")
	writeInspectJSON(t, filepath.Join(outputDir, "xai_run_metadata.json"), xaipipeline.RunMetadata{
		ManifestReused:  true,
		ForceRegenerate: true,
		ReusedShots:     []int{1},
		ShotDecisions: []xaipipeline.ShotDecision{
			{
				Index:        1,
				Decision:     "reused",
				VideoPath:    "shots/shot_001.mp4",
				PromptHash:   promptHash,
				XAIRequestID: "req_123",
				XAIStatus:    "done",
			},
		},
	})
	validator := &validateStubOutputValidator{
		metadata: xaipipeline.RenderMetadata{
			Path:        filepath.Join(outputDir, "output_xai.mp4"),
			Width:       720,
			Height:      1280,
			FPS:         24,
			DurationSec: 8,
			CodecName:   "h264",
			SizeBytes:   validationMP4SizeBytes(),
			HasAudio:    false,
		},
	}

	summary, err := xaipipeline.ValidateOutputDir(context.Background(), outputDir, validator)
	if err != nil {
		t.Fatalf("ValidateOutputDir() error = %v", err)
	}

	if summary.Status != xaipipeline.ValidationStatusInvalid {
		t.Fatalf("Status = %q, want %q", summary.Status, xaipipeline.ValidationStatusInvalid)
	}
	if !validationIssuesContain(summary.Issues, "force_regenerate cannot reuse shot") {
		t.Fatalf("Issues = %#v, want force_regenerate reuse issue", summary.Issues)
	}
}

func TestValidateOutputDir_RejectsMismatchedPersistedRenderMetadata(t *testing.T) {
	t.Parallel()

	outputDir := t.TempDir()
	manifest := writeProductionValidationArtifacts(t, outputDir, true)
	persistedMetadata := validValidationRenderMetadata(outputDir, manifest)
	persistedMetadata.Width = 1920
	persistedMetadata.Height = 1080
	writeInspectJSON(t, filepath.Join(outputDir, "render_metadata.json"), persistedMetadata)
	validator := &validateStubOutputValidator{
		metadata: validValidationRenderMetadata(outputDir, manifest),
	}

	summary, err := xaipipeline.ValidateOutputDir(context.Background(), outputDir, validator)
	if err != nil {
		t.Fatalf("ValidateOutputDir() error = %v", err)
	}

	if summary.Status != xaipipeline.ValidationStatusInvalid {
		t.Fatalf("Status = %q, want %q", summary.Status, xaipipeline.ValidationStatusInvalid)
	}
	if !validator.called {
		t.Fatal("ffprobe validator should still run")
	}
	if !validationIssuesContain(summary.Issues, "render metadata") {
		t.Fatalf("Issues = %#v, want persisted render metadata issue", summary.Issues)
	}
}

func TestValidateOutputDir_RejectsNonH264PersistedRenderMetadata(t *testing.T) {
	t.Parallel()

	outputDir := t.TempDir()
	manifest := writeProductionValidationArtifacts(t, outputDir, true)
	persistedMetadata := validValidationRenderMetadata(outputDir, manifest)
	persistedMetadata.CodecName = "hevc"
	writeInspectJSON(t, filepath.Join(outputDir, "render_metadata.json"), persistedMetadata)
	returnedMetadata := validValidationRenderMetadata(outputDir, manifest)
	returnedMetadata.CodecName = "hevc"
	validator := &validateStubOutputValidator{
		metadata: returnedMetadata,
	}

	summary, err := xaipipeline.ValidateOutputDir(context.Background(), outputDir, validator)
	if err != nil {
		t.Fatalf("ValidateOutputDir() error = %v", err)
	}

	if summary.Status != xaipipeline.ValidationStatusInvalid {
		t.Fatalf("Status = %q, want %q", summary.Status, xaipipeline.ValidationStatusInvalid)
	}
	if !validationIssuesContain(summary.Issues, "codec") {
		t.Fatalf("Issues = %#v, want codec issue", summary.Issues)
	}
}

func TestValidateOutputDir_RejectsNonYUV420PPersistedRenderMetadata(t *testing.T) {
	t.Parallel()

	outputDir := t.TempDir()
	manifest := writeProductionValidationArtifacts(t, outputDir, true)
	persistedMetadata := validValidationRenderMetadata(outputDir, manifest)
	persistedMetadata.PixelFormat = "yuv444p"
	writeInspectJSON(t, filepath.Join(outputDir, "render_metadata.json"), persistedMetadata)
	returnedMetadata := validValidationRenderMetadata(outputDir, manifest)
	returnedMetadata.PixelFormat = "yuv444p"
	validator := &validateStubOutputValidator{
		metadata: returnedMetadata,
	}

	summary, err := xaipipeline.ValidateOutputDir(context.Background(), outputDir, validator)
	if err != nil {
		t.Fatalf("ValidateOutputDir() error = %v", err)
	}

	if summary.Status != xaipipeline.ValidationStatusInvalid {
		t.Fatalf("Status = %q, want %q", summary.Status, xaipipeline.ValidationStatusInvalid)
	}
	if !validationIssuesContain(summary.Issues, "pixel format") {
		t.Fatalf("Issues = %#v, want persisted pixel format issue", summary.Issues)
	}
}

func TestValidateOutputDir_RejectsStalePersistedRenderMetadataPath(t *testing.T) {
	t.Parallel()

	outputDir := t.TempDir()
	manifest := writeProductionValidationArtifacts(t, outputDir, true)
	persistedMetadata := validValidationRenderMetadata(outputDir, manifest)
	persistedMetadata.Path = filepath.Join(outputDir, "stale.mp4")
	writeInspectJSON(t, filepath.Join(outputDir, "render_metadata.json"), persistedMetadata)
	validator := &validateStubOutputValidator{
		metadata: validValidationRenderMetadata(outputDir, manifest),
	}

	summary, err := xaipipeline.ValidateOutputDir(context.Background(), outputDir, validator)
	if err != nil {
		t.Fatalf("ValidateOutputDir() error = %v", err)
	}

	if summary.Status != xaipipeline.ValidationStatusInvalid {
		t.Fatalf("Status = %q, want %q", summary.Status, xaipipeline.ValidationStatusInvalid)
	}
	if !validationIssuesContain(summary.Issues, "render metadata path") {
		t.Fatalf("Issues = %#v, want render metadata path issue", summary.Issues)
	}
}

func TestValidateOutputDir_RejectsStalePersistedRenderMetadataSize(t *testing.T) {
	t.Parallel()

	outputDir := t.TempDir()
	manifest := writeProductionValidationArtifacts(t, outputDir, true)
	persistedMetadata := validValidationRenderMetadata(outputDir, manifest)
	persistedMetadata.SizeBytes = 1
	writeInspectJSON(t, filepath.Join(outputDir, "render_metadata.json"), persistedMetadata)
	validator := &validateStubOutputValidator{
		metadata: validValidationRenderMetadata(outputDir, manifest),
	}

	summary, err := xaipipeline.ValidateOutputDir(context.Background(), outputDir, validator)
	if err != nil {
		t.Fatalf("ValidateOutputDir() error = %v", err)
	}

	if summary.Status != xaipipeline.ValidationStatusInvalid {
		t.Fatalf("Status = %q, want %q", summary.Status, xaipipeline.ValidationStatusInvalid)
	}
	if !validationIssuesContain(summary.Issues, "render metadata size_bytes") {
		t.Fatalf("Issues = %#v, want render metadata size issue", summary.Issues)
	}
}

func TestValidateOutputDir_RejectsPersistedRenderMetadataForDifferentManifest(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		metadata func(outputDir string, manifest xaipipeline.Manifest) xaipipeline.RenderMetadata
		want     string
	}{
		{
			name: "project id",
			metadata: func(outputDir string, manifest xaipipeline.Manifest) xaipipeline.RenderMetadata {
				metadata := validValidationRenderMetadata(outputDir, manifest)
				metadata.ProjectID = "other-project"
				return metadata
			},
			want: "render metadata project_id",
		},
		{
			name: "manifest hash",
			metadata: func(outputDir string, manifest xaipipeline.Manifest) xaipipeline.RenderMetadata {
				metadata := validValidationRenderMetadata(outputDir, manifest)
				metadata.ManifestHash = strings.Repeat("0", 64)
				return metadata
			},
			want: "render metadata manifest_hash",
		},
		{
			name: "missing identity",
			metadata: func(outputDir string, manifest xaipipeline.Manifest) xaipipeline.RenderMetadata {
				metadata := validValidationRenderMetadata(outputDir, manifest)
				metadata.ProjectID = ""
				metadata.ManifestHash = ""
				return metadata
			},
			want: "render metadata project_id",
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			outputDir := t.TempDir()
			manifest := writeProductionValidationArtifacts(t, outputDir, true)
			writeInspectJSON(t, filepath.Join(outputDir, "render_metadata.json"), tt.metadata(outputDir, manifest))
			validator := &validateStubOutputValidator{
				metadata: validValidationRenderMetadata(outputDir, manifest),
			}

			summary, err := xaipipeline.ValidateOutputDir(context.Background(), outputDir, validator)
			if err != nil {
				t.Fatalf("ValidateOutputDir() error = %v", err)
			}

			if summary.Status != xaipipeline.ValidationStatusInvalid {
				t.Fatalf("Status = %q, want invalid", summary.Status)
			}
			if !validationIssuesContain(summary.Issues, tt.want) {
				t.Fatalf("Issues = %#v, want %q issue", summary.Issues, tt.want)
			}
		})
	}
}

func TestValidateOutputDir_ReportsAllPersistedRenderMetadataIssues(t *testing.T) {
	t.Parallel()

	outputDir := t.TempDir()
	manifest := writeProductionValidationArtifacts(t, outputDir, true)
	persistedMetadata := validValidationRenderMetadata(outputDir, manifest)
	persistedMetadata.ProjectID = ""
	persistedMetadata.ManifestHash = "not-a-sha"
	persistedMetadata.Path = filepath.Join(outputDir, "stale.mp4")
	persistedMetadata.Width = 1920
	persistedMetadata.Height = 1080
	persistedMetadata.SizeBytes = 1
	writeInspectJSON(t, filepath.Join(outputDir, "render_metadata.json"), persistedMetadata)
	validator := &validateStubOutputValidator{
		metadata: validValidationRenderMetadata(outputDir, manifest),
	}

	summary, err := xaipipeline.ValidateOutputDir(context.Background(), outputDir, validator)
	if err != nil {
		t.Fatalf("ValidateOutputDir() error = %v", err)
	}

	if summary.Status != xaipipeline.ValidationStatusInvalid {
		t.Fatalf("Status = %q, want invalid", summary.Status)
	}
	for _, want := range []string{
		"render metadata project_id is empty",
		"render metadata manifest_hash must be 64 lowercase hex characters",
		"render metadata path:",
		"render metadata: final output dimensions 1920x1080, want 720x1280",
		"render metadata size_bytes:",
	} {
		if !validationIssuesContain(summary.Issues, want) {
			t.Fatalf("Issues = %#v, want %q issue", summary.Issues, want)
		}
	}
}

func TestValidateOutputDir_RejectsManifestShotGenerationSpecMismatch(t *testing.T) {
	t.Parallel()

	outputDir := t.TempDir()
	writeProductionValidationArtifacts(t, outputDir, true)
	writeInspectJSON(t, filepath.Join(outputDir, "xai_manifest.json"), xaipipeline.Manifest{
		ProjectID: "production",
		Format:    "portrait",
		FPS:       24,
		Width:     720,
		Height:    1280,
		Shots: []xaipipeline.Shot{
			{
				Index:        1,
				Prompt:       "shot",
				PromptHash:   "prompt-hash",
				XAIRequestID: "req_123",
				XAIStatus:    "done",
				DurationSec:  8,
				AspectRatio:  "16:9",
				Resolution:   "720p",
				VideoPath:    "shots/shot_001.mp4",
			},
		},
	})
	validator := &validateStubOutputValidator{
		metadata: xaipipeline.RenderMetadata{
			Path:        filepath.Join(outputDir, "output_xai.mp4"),
			Width:       720,
			Height:      1280,
			FPS:         24,
			DurationSec: 8,
			CodecName:   "h264",
			SizeBytes:   validationMP4SizeBytes(),
			HasAudio:    false,
		},
	}

	summary, err := xaipipeline.ValidateOutputDir(context.Background(), outputDir, validator)
	if err != nil {
		t.Fatalf("ValidateOutputDir() error = %v", err)
	}

	if summary.Status != xaipipeline.ValidationStatusInvalid {
		t.Fatalf("Status = %q, want %q", summary.Status, xaipipeline.ValidationStatusInvalid)
	}
	if !validationIssuesContain(summary.Issues, "aspect_ratio") {
		t.Fatalf("Issues = %#v, want manifest shot generation spec issue", summary.Issues)
	}
}

func TestValidateOutputDir_RejectsNonCanonicalManifestShotGenerationSpec(t *testing.T) {
	tests := []struct {
		name        string
		aspectRatio string
		resolution  string
		wantIssue   string
	}{
		{
			name:        "aspect ratio",
			aspectRatio: " 9:16 ",
			resolution:  "720p",
			wantIssue:   `manifest shot 1 aspect_ratio " 9:16 " is not canonical, want "9:16"`,
		},
		{
			name:        "resolution",
			aspectRatio: "9:16",
			resolution:  " 720p ",
			wantIssue:   `manifest shot 1 resolution " 720p " is not canonical, want "720p"`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			outputDir := t.TempDir()
			writeProductionValidationArtifacts(t, outputDir, true)
			promptHash := validationPromptHash("shot", 8, tt.aspectRatio, tt.resolution)
			writeInspectJSON(t, filepath.Join(outputDir, "xai_manifest.json"), xaipipeline.Manifest{
				ProjectID: "production",
				StoryHash: validValidationStoryHash,
				Format:    "portrait",
				FPS:       24,
				Width:     720,
				Height:    1280,
				Shots: []xaipipeline.Shot{
					{
						Index:        1,
						Prompt:       "shot",
						PromptHash:   promptHash,
						XAIRequestID: "req_123",
						XAIStatus:    "done",
						DurationSec:  8,
						AspectRatio:  tt.aspectRatio,
						Resolution:   tt.resolution,
						VideoPath:    "shots/shot_001.mp4",
					},
				},
			})
			validator := &validateStubOutputValidator{
				metadata: xaipipeline.RenderMetadata{
					Path:        filepath.Join(outputDir, "output_xai.mp4"),
					Width:       720,
					Height:      1280,
					FPS:         24,
					DurationSec: 8,
					CodecName:   "h264",
					PixelFormat: "yuv420p",
					SizeBytes:   validationMP4SizeBytes(),
					HasAudio:    false,
				},
			}

			summary, err := xaipipeline.ValidateOutputDir(context.Background(), outputDir, validator)
			if err != nil {
				t.Fatalf("ValidateOutputDir() error = %v", err)
			}

			if summary.Status != xaipipeline.ValidationStatusInvalid {
				t.Fatalf("Status = %q, want %q", summary.Status, xaipipeline.ValidationStatusInvalid)
			}
			if !validationIssuesContain(summary.Issues, tt.wantIssue) {
				t.Fatalf("Issues = %#v, want manifest shot generation spec canonical issue", summary.Issues)
			}
		})
	}
}

func TestValidateOutputDir_RejectsManifestShotUnsupportedTransition(t *testing.T) {
	t.Parallel()

	outputDir := t.TempDir()
	writeProductionValidationArtifactsWithTransition(t, outputDir, "spin")
	validator := &validateStubOutputValidator{
		metadata: xaipipeline.RenderMetadata{
			Path:        filepath.Join(outputDir, "output_xai.mp4"),
			Width:       720,
			Height:      1280,
			FPS:         24,
			DurationSec: 8,
			CodecName:   "h264",
			PixelFormat: "yuv420p",
			SizeBytes:   validationMP4SizeBytes(),
			HasAudio:    false,
		},
	}

	summary, err := xaipipeline.ValidateOutputDir(context.Background(), outputDir, validator)
	if err != nil {
		t.Fatalf("ValidateOutputDir() error = %v", err)
	}

	if summary.Status != xaipipeline.ValidationStatusInvalid {
		t.Fatalf("Status = %q, want %q", summary.Status, xaipipeline.ValidationStatusInvalid)
	}
	if !validationIssuesContain(summary.Issues, `transition_out "spin"`) {
		t.Fatalf("Issues = %#v, want unsupported transition_out issue", summary.Issues)
	}
}

func TestValidateOutputDir_RejectsManifestShotVideoPathOutsideOutput(t *testing.T) {
	t.Parallel()

	outputDir := t.TempDir()
	writeProductionValidationArtifacts(t, outputDir, true)
	externalShot := filepath.Join(t.TempDir(), "external-shot.mp4")
	writeValidationMP4(t, externalShot)
	promptHash := validationPromptHash("shot", 8, "9:16", "720p")
	writeInspectJSON(t, filepath.Join(outputDir, "xai_manifest.json"), xaipipeline.Manifest{
		ProjectID: "production",
		StoryHash: validValidationStoryHash,
		Format:    "portrait",
		FPS:       24,
		Width:     720,
		Height:    1280,
		Shots: []xaipipeline.Shot{
			{
				Index:        1,
				Prompt:       "shot",
				PromptHash:   promptHash,
				XAIRequestID: "req_123",
				XAIStatus:    "done",
				DurationSec:  8,
				AspectRatio:  "9:16",
				Resolution:   "720p",
				VideoPath:    externalShot,
			},
		},
	})
	writeInspectJSON(t, filepath.Join(outputDir, "xai_run_metadata.json"), xaipipeline.RunMetadata{
		Planned:        true,
		GeneratedShots: []int{1},
		ShotDecisions: []xaipipeline.ShotDecision{
			{
				Index:        1,
				Decision:     "generated",
				VideoPath:    externalShot,
				PromptHash:   promptHash,
				XAIRequestID: "req_123",
				XAIStatus:    "done",
			},
		},
	})
	validator := &validateStubOutputValidator{
		metadata: xaipipeline.RenderMetadata{
			Path:        filepath.Join(outputDir, "output_xai.mp4"),
			Width:       720,
			Height:      1280,
			FPS:         24,
			DurationSec: 8,
			CodecName:   "h264",
			PixelFormat: "yuv420p",
			SizeBytes:   validationMP4SizeBytes(),
			HasAudio:    false,
		},
	}

	summary, err := xaipipeline.ValidateOutputDir(context.Background(), outputDir, validator)
	if err != nil {
		t.Fatalf("ValidateOutputDir() error = %v", err)
	}

	if summary.Status != xaipipeline.ValidationStatusInvalid {
		t.Fatalf("Status = %q, want %q", summary.Status, xaipipeline.ValidationStatusInvalid)
	}
	if !validationIssuesContain(summary.Issues, "manifest shot 1 video_path") {
		t.Fatalf("Issues = %#v, want manifest shot video_path issue", summary.Issues)
	}
	if !validationIssuesContain(summary.Issues, "shots/shot_001.mp4") {
		t.Fatalf("Issues = %#v, want expected relative shot path", summary.Issues)
	}
	if validationPathsContain(validator.paths, externalShot) {
		t.Fatalf("validator paths = %#v, must not ffprobe external manifest video_path %q", validator.paths, externalShot)
	}
}

func TestValidateOutputDir_RejectsNonCanonicalManifestVideoPath(t *testing.T) {
	t.Parallel()

	outputDir := t.TempDir()
	writeProductionValidationArtifacts(t, outputDir, true)
	promptHash := validationPromptHash("shot", 8, "9:16", "720p")
	const videoPath = " shots/shot_001.mp4 "
	writeInspectJSON(t, filepath.Join(outputDir, "xai_manifest.json"), xaipipeline.Manifest{
		ProjectID: "production",
		StoryHash: validValidationStoryHash,
		Format:    "portrait",
		FPS:       24,
		Width:     720,
		Height:    1280,
		Shots: []xaipipeline.Shot{
			{
				Index:        1,
				Prompt:       "shot",
				PromptHash:   promptHash,
				XAIRequestID: "req_123",
				XAIStatus:    "done",
				DurationSec:  8,
				AspectRatio:  "9:16",
				Resolution:   "720p",
				VideoPath:    videoPath,
			},
		},
	})
	writeInspectJSON(t, filepath.Join(outputDir, "xai_run_metadata.json"), xaipipeline.RunMetadata{
		Planned:        true,
		GeneratedShots: []int{1},
		ShotDecisions: []xaipipeline.ShotDecision{
			{
				Index:        1,
				Decision:     "generated",
				VideoPath:    videoPath,
				PromptHash:   promptHash,
				XAIRequestID: "req_123",
				XAIStatus:    "done",
			},
		},
	})
	validator := &validateStubOutputValidator{
		metadata: xaipipeline.RenderMetadata{
			Path:        filepath.Join(outputDir, "output_xai.mp4"),
			Width:       720,
			Height:      1280,
			FPS:         24,
			DurationSec: 8,
			CodecName:   "h264",
			PixelFormat: "yuv420p",
			SizeBytes:   validationMP4SizeBytes(),
			HasAudio:    false,
		},
	}

	summary, err := xaipipeline.ValidateOutputDir(context.Background(), outputDir, validator)
	if err != nil {
		t.Fatalf("ValidateOutputDir() error = %v", err)
	}

	if summary.Status != xaipipeline.ValidationStatusInvalid {
		t.Fatalf("Status = %q, want %q", summary.Status, xaipipeline.ValidationStatusInvalid)
	}
	if !validationIssuesContain(summary.Issues, `manifest shot 1 video_path " shots/shot_001.mp4 " is not canonical, want "shots/shot_001.mp4"`) {
		t.Fatalf("Issues = %#v, want manifest video_path canonical issue", summary.Issues)
	}
}

func TestValidateOutputDir_RejectsManifestShotOrderMismatch(t *testing.T) {
	t.Parallel()

	outputDir := t.TempDir()
	writeInspectJSON(t, filepath.Join(outputDir, "xai_manifest.json"), xaipipeline.Manifest{
		ProjectID: "production",
		Format:    "portrait",
		FPS:       24,
		Width:     720,
		Height:    1280,
		Shots: []xaipipeline.Shot{
			{
				Index:        2,
				Prompt:       "second shot",
				PromptHash:   "prompt-hash-2",
				XAIRequestID: "req_002",
				XAIStatus:    "done",
				DurationSec:  8,
				AspectRatio:  "9:16",
				Resolution:   "720p",
				VideoPath:    "shots/shot_002.mp4",
			},
			{
				Index:        1,
				Prompt:       "first shot",
				PromptHash:   "prompt-hash-1",
				XAIRequestID: "req_001",
				XAIStatus:    "done",
				DurationSec:  8,
				AspectRatio:  "9:16",
				Resolution:   "720p",
				VideoPath:    "shots/shot_001.mp4",
			},
		},
	})
	writeInspectJSON(t, filepath.Join(outputDir, "xai_run_metadata.json"), xaipipeline.RunMetadata{
		Planned:        true,
		GeneratedShots: []int{1, 2},
		ShotDecisions: []xaipipeline.ShotDecision{
			{
				Index:        1,
				Decision:     "generated",
				VideoPath:    "shots/shot_001.mp4",
				PromptHash:   "prompt-hash-1",
				XAIRequestID: "req_001",
				XAIStatus:    "done",
			},
			{
				Index:        2,
				Decision:     "generated",
				VideoPath:    "shots/shot_002.mp4",
				PromptHash:   "prompt-hash-2",
				XAIRequestID: "req_002",
				XAIStatus:    "done",
			},
		},
	})
	writeInspectJSON(t, filepath.Join(outputDir, "render_metadata.json"), xaipipeline.RenderMetadata{
		Path:        filepath.Join(outputDir, "output_xai.mp4"),
		Width:       720,
		Height:      1280,
		FPS:         24,
		DurationSec: 16,
		CodecName:   "h264",
		SizeBytes:   validationMP4SizeBytes(),
		HasAudio:    false,
	})
	for _, path := range []string{
		"shots/shot_001.mp4",
		"shots/shot_002.mp4",
		"normalized/shot_001.mp4",
		"normalized/shot_002.mp4",
		"hyperframes/index.html",
		"timeline_hyperframes.mp4",
		"output_xai.mp4",
		"preview_frame.jpg",
	} {
		writeInspectFile(t, filepath.Join(outputDir, path))
	}
	writeValidationHyperFramesPackage(t, filepath.Join(outputDir, "hyperframes", "package.json"))
	validator := &validateStubOutputValidator{
		metadata: xaipipeline.RenderMetadata{
			Path:        filepath.Join(outputDir, "output_xai.mp4"),
			Width:       720,
			Height:      1280,
			FPS:         24,
			DurationSec: 16,
			CodecName:   "h264",
			SizeBytes:   validationMP4SizeBytes(),
			HasAudio:    false,
		},
	}

	summary, err := xaipipeline.ValidateOutputDir(context.Background(), outputDir, validator)
	if err != nil {
		t.Fatalf("ValidateOutputDir() error = %v", err)
	}

	if summary.Status != xaipipeline.ValidationStatusInvalid {
		t.Fatalf("Status = %q, want %q", summary.Status, xaipipeline.ValidationStatusInvalid)
	}
	if !validationIssuesContain(summary.Issues, "shot indexes must match shot order") {
		t.Fatalf("Issues = %#v, want manifest shot order issue", summary.Issues)
	}
}

func TestValidateOutputDir_RejectsMissingShotArtifacts(t *testing.T) {
	t.Parallel()

	outputDir := t.TempDir()
	writeProductionValidationArtifacts(t, outputDir, true)
	if err := os.Remove(filepath.Join(outputDir, "shots", "shot_001.mp4")); err != nil {
		t.Fatalf("remove raw shot fixture: %v", err)
	}
	validator := &validateStubOutputValidator{
		metadata: xaipipeline.RenderMetadata{
			Path:        filepath.Join(outputDir, "output_xai.mp4"),
			Width:       720,
			Height:      1280,
			FPS:         24,
			DurationSec: 8,
			CodecName:   "h264",
			SizeBytes:   validationMP4SizeBytes(),
			HasAudio:    false,
		},
	}

	summary, err := xaipipeline.ValidateOutputDir(context.Background(), outputDir, validator)
	if err != nil {
		t.Fatalf("ValidateOutputDir() error = %v", err)
	}

	if summary.Status != xaipipeline.ValidationStatusInvalid {
		t.Fatalf("Status = %q, want %q", summary.Status, xaipipeline.ValidationStatusInvalid)
	}
	if !validationIssuesContain(summary.Issues, "missing artifact shots/shot_001.mp4") {
		t.Fatalf("Issues = %#v, want missing raw shot artifact issue", summary.Issues)
	}
}

func TestValidateOutputDir_RejectsMissingHyperFramesArtifacts(t *testing.T) {
	t.Parallel()

	outputDir := t.TempDir()
	writeProductionValidationArtifacts(t, outputDir, true)
	_ = os.Remove(filepath.Join(outputDir, "timeline_hyperframes.mp4"))
	validator := &validateStubOutputValidator{
		metadata: xaipipeline.RenderMetadata{
			Path:        filepath.Join(outputDir, "output_xai.mp4"),
			Width:       720,
			Height:      1280,
			FPS:         24,
			DurationSec: 8,
			CodecName:   "h264",
			SizeBytes:   validationMP4SizeBytes(),
			HasAudio:    false,
		},
	}

	summary, err := xaipipeline.ValidateOutputDir(context.Background(), outputDir, validator)
	if err != nil {
		t.Fatalf("ValidateOutputDir() error = %v", err)
	}

	if summary.Status != xaipipeline.ValidationStatusInvalid {
		t.Fatalf("Status = %q, want %q", summary.Status, xaipipeline.ValidationStatusInvalid)
	}
	if !validationIssuesContain(summary.Issues, "missing artifact timeline_hyperframes.mp4") {
		t.Fatalf("Issues = %#v, want missing HyperFrames timeline issue", summary.Issues)
	}
}

func TestValidateOutputDir_RejectsHyperFramesProjectMissingNormalizedShotReferences(t *testing.T) {
	t.Parallel()

	outputDir := t.TempDir()
	writeProductionValidationArtifacts(t, outputDir, true)
	writeInspectFile(t, filepath.Join(outputDir, "hyperframes", "index.html"))
	validator := &validateStubOutputValidator{
		metadata: xaipipeline.RenderMetadata{
			Path:        filepath.Join(outputDir, "output_xai.mp4"),
			Width:       720,
			Height:      1280,
			FPS:         24,
			DurationSec: 8,
			CodecName:   "h264",
			SizeBytes:   validationMP4SizeBytes(),
			HasAudio:    false,
		},
	}

	summary, err := xaipipeline.ValidateOutputDir(context.Background(), outputDir, validator)
	if err != nil {
		t.Fatalf("ValidateOutputDir() error = %v", err)
	}

	if summary.Status != xaipipeline.ValidationStatusInvalid {
		t.Fatalf("Status = %q, want %q", summary.Status, xaipipeline.ValidationStatusInvalid)
	}
	if !validationIssuesContain(summary.Issues, "hyperframes project missing normalized shot reference") {
		t.Fatalf("Issues = %#v, want missing HyperFrames normalized shot reference issue", summary.Issues)
	}
}

func TestValidateOutputDir_RejectsHyperFramesProjectExtraNormalizedShotReferences(t *testing.T) {
	t.Parallel()

	outputDir := t.TempDir()
	writeProductionValidationArtifacts(t, outputDir, true)
	indexPath := filepath.Join(outputDir, "hyperframes", "index.html")
	appendValidationHyperFramesNormalizedShotReference(t, indexPath, 999)
	validator := &validateStubOutputValidator{
		metadata: xaipipeline.RenderMetadata{
			Path:        filepath.Join(outputDir, "output_xai.mp4"),
			Width:       720,
			Height:      1280,
			FPS:         24,
			DurationSec: 8,
			CodecName:   "h264",
			PixelFormat: "yuv420p",
			SizeBytes:   validationMP4SizeBytes(),
			HasAudio:    false,
		},
	}

	summary, err := xaipipeline.ValidateOutputDir(context.Background(), outputDir, validator)
	if err != nil {
		t.Fatalf("ValidateOutputDir() error = %v", err)
	}

	if summary.Status != xaipipeline.ValidationStatusInvalid {
		t.Fatalf("Status = %q, want %q", summary.Status, xaipipeline.ValidationStatusInvalid)
	}
	if !validationIssuesContain(summary.Issues, "hyperframes project references normalized shot outside manifest") {
		t.Fatalf("Issues = %#v, want extra normalized HyperFrames reference issue", summary.Issues)
	}
}

func TestValidateOutputDir_RejectsHyperFramesProjectDuplicateNormalizedShotReferences(t *testing.T) {
	t.Parallel()

	outputDir := t.TempDir()
	writeProductionValidationArtifacts(t, outputDir, true)
	indexPath := filepath.Join(outputDir, "hyperframes", "index.html")
	appendValidationHyperFramesNormalizedShotReference(t, indexPath, 1)
	validator := &validateStubOutputValidator{
		metadata: xaipipeline.RenderMetadata{
			Path:        filepath.Join(outputDir, "output_xai.mp4"),
			Width:       720,
			Height:      1280,
			FPS:         24,
			DurationSec: 8,
			CodecName:   "h264",
			PixelFormat: "yuv420p",
			SizeBytes:   validationMP4SizeBytes(),
			HasAudio:    false,
		},
	}

	summary, err := xaipipeline.ValidateOutputDir(context.Background(), outputDir, validator)
	if err != nil {
		t.Fatalf("ValidateOutputDir() error = %v", err)
	}

	if summary.Status != xaipipeline.ValidationStatusInvalid {
		t.Fatalf("Status = %q, want %q", summary.Status, xaipipeline.ValidationStatusInvalid)
	}
	if !validationIssuesContain(summary.Issues, "hyperframes project references normalized shot more than once") {
		t.Fatalf("Issues = %#v, want duplicate normalized HyperFrames reference issue", summary.Issues)
	}
}

func TestValidateOutputDir_AcceptsHyperFramesProjectVideoSourceAttributeWhitespace(t *testing.T) {
	t.Parallel()

	outputDir := t.TempDir()
	writeProductionValidationArtifacts(t, outputDir, true)
	indexPath := filepath.Join(outputDir, "hyperframes", "index.html")
	replaceValidationHyperFramesIndex(t, indexPath, `src="../normalized/shot_001.mp4"`, `src = "../normalized/shot_001.mp4"`)
	validator := &validateStubOutputValidator{
		metadata: xaipipeline.RenderMetadata{
			Path:        filepath.Join(outputDir, "output_xai.mp4"),
			Width:       720,
			Height:      1280,
			FPS:         24,
			DurationSec: 8,
			CodecName:   "h264",
			PixelFormat: "yuv420p",
			SizeBytes:   validationMP4SizeBytes(),
			HasAudio:    false,
		},
	}

	summary, err := xaipipeline.ValidateOutputDir(context.Background(), outputDir, validator)
	if err != nil {
		t.Fatalf("ValidateOutputDir() error = %v", err)
	}

	if summary.Status != xaipipeline.ValidationStatusValid {
		t.Fatalf("Status = %q, want %q; issues=%#v", summary.Status, xaipipeline.ValidationStatusValid, summary.Issues)
	}
}

func TestValidateOutputDir_AcceptsHyperFramesProjectVideoTimingAttributeWhitespace(t *testing.T) {
	t.Parallel()

	outputDir := t.TempDir()
	writeProductionValidationArtifacts(t, outputDir, true)
	indexPath := filepath.Join(outputDir, "hyperframes", "index.html")
	replaceValidationHyperFramesIndex(
		t,
		indexPath,
		`<video id="video-1" class="shot-video clip" src="../normalized/shot_001.mp4" data-start="0.000" data-duration="8.000" data-track-index="0"></video>`,
		`<video id="video-1" class="shot-video clip" src="../normalized/shot_001.mp4" data-start = "0.000" data-duration = "8.000" data-track-index="0"></video>`,
	)
	validator := &validateStubOutputValidator{
		metadata: xaipipeline.RenderMetadata{
			Path:        filepath.Join(outputDir, "output_xai.mp4"),
			Width:       720,
			Height:      1280,
			FPS:         24,
			DurationSec: 8,
			CodecName:   "h264",
			PixelFormat: "yuv420p",
			SizeBytes:   validationMP4SizeBytes(),
			HasAudio:    false,
		},
	}

	summary, err := xaipipeline.ValidateOutputDir(context.Background(), outputDir, validator)
	if err != nil {
		t.Fatalf("ValidateOutputDir() error = %v", err)
	}

	if summary.Status != xaipipeline.ValidationStatusValid {
		t.Fatalf("Status = %q, want %q; issues=%#v", summary.Status, xaipipeline.ValidationStatusValid, summary.Issues)
	}
}

func TestValidateOutputDir_RejectsHyperFramesProjectExternalVideoSource(t *testing.T) {
	t.Parallel()

	outputDir := t.TempDir()
	writeProductionValidationArtifacts(t, outputDir, true)
	indexPath := filepath.Join(outputDir, "hyperframes", "index.html")
	appendValidationHyperFramesExternalVideoReference(t, indexPath, "https://example.com/not-xai.mp4")
	validator := &validateStubOutputValidator{
		metadata: xaipipeline.RenderMetadata{
			Path:        filepath.Join(outputDir, "output_xai.mp4"),
			Width:       720,
			Height:      1280,
			FPS:         24,
			DurationSec: 8,
			CodecName:   "h264",
			PixelFormat: "yuv420p",
			SizeBytes:   validationMP4SizeBytes(),
			HasAudio:    false,
		},
	}

	summary, err := xaipipeline.ValidateOutputDir(context.Background(), outputDir, validator)
	if err != nil {
		t.Fatalf("ValidateOutputDir() error = %v", err)
	}

	if summary.Status != xaipipeline.ValidationStatusInvalid {
		t.Fatalf("Status = %q, want %q", summary.Status, xaipipeline.ValidationStatusInvalid)
	}
	if !validationIssuesContain(summary.Issues, "hyperframes project video source is not manifest-owned normalized shot") {
		t.Fatalf("Issues = %#v, want external HyperFrames video source issue", summary.Issues)
	}
}

func TestValidateOutputDir_RejectsHyperFramesProjectUppercaseExternalVideoSource(t *testing.T) {
	t.Parallel()

	outputDir := t.TempDir()
	writeProductionValidationArtifacts(t, outputDir, true)
	indexPath := filepath.Join(outputDir, "hyperframes", "index.html")
	appendValidationHyperFramesUppercaseExternalVideoReference(t, indexPath, "https://example.com/not-xai.mp4")
	validator := &validateStubOutputValidator{
		metadata: xaipipeline.RenderMetadata{
			Path:        filepath.Join(outputDir, "output_xai.mp4"),
			Width:       720,
			Height:      1280,
			FPS:         24,
			DurationSec: 8,
			CodecName:   "h264",
			PixelFormat: "yuv420p",
			SizeBytes:   validationMP4SizeBytes(),
			HasAudio:    false,
		},
	}

	summary, err := xaipipeline.ValidateOutputDir(context.Background(), outputDir, validator)
	if err != nil {
		t.Fatalf("ValidateOutputDir() error = %v", err)
	}

	if summary.Status != xaipipeline.ValidationStatusInvalid {
		t.Fatalf("Status = %q, want %q", summary.Status, xaipipeline.ValidationStatusInvalid)
	}
	if !validationIssuesContain(summary.Issues, "hyperframes project video source is not manifest-owned normalized shot") {
		t.Fatalf("Issues = %#v, want uppercase external HyperFrames video source issue", summary.Issues)
	}
}

func TestValidateOutputDir_RejectsHyperFramesProjectRawShotReferences(t *testing.T) {
	t.Parallel()

	outputDir := t.TempDir()
	writeProductionValidationArtifacts(t, outputDir, true)
	indexPath := filepath.Join(outputDir, "hyperframes", "index.html")
	appendValidationHyperFramesRawShotReference(t, indexPath, 1)
	validator := &validateStubOutputValidator{
		metadata: xaipipeline.RenderMetadata{
			Path:        filepath.Join(outputDir, "output_xai.mp4"),
			Width:       720,
			Height:      1280,
			FPS:         24,
			DurationSec: 8,
			CodecName:   "h264",
			PixelFormat: "yuv420p",
			SizeBytes:   validationMP4SizeBytes(),
			HasAudio:    false,
		},
	}

	summary, err := xaipipeline.ValidateOutputDir(context.Background(), outputDir, validator)
	if err != nil {
		t.Fatalf("ValidateOutputDir() error = %v", err)
	}

	if summary.Status != xaipipeline.ValidationStatusInvalid {
		t.Fatalf("Status = %q, want %q", summary.Status, xaipipeline.ValidationStatusInvalid)
	}
	if !validationIssuesContain(summary.Issues, "hyperframes project must not reference raw xAI shot") {
		t.Fatalf("Issues = %#v, want raw HyperFrames shot reference issue", summary.Issues)
	}
}

func TestValidateOutputDir_RejectsHyperFramesProjectExtraRawShotReferences(t *testing.T) {
	t.Parallel()

	outputDir := t.TempDir()
	writeProductionValidationArtifacts(t, outputDir, true)
	indexPath := filepath.Join(outputDir, "hyperframes", "index.html")
	appendValidationHyperFramesRawShotReference(t, indexPath, 999)
	validator := &validateStubOutputValidator{
		metadata: xaipipeline.RenderMetadata{
			Path:        filepath.Join(outputDir, "output_xai.mp4"),
			Width:       720,
			Height:      1280,
			FPS:         24,
			DurationSec: 8,
			CodecName:   "h264",
			PixelFormat: "yuv420p",
			SizeBytes:   validationMP4SizeBytes(),
			HasAudio:    false,
		},
	}

	summary, err := xaipipeline.ValidateOutputDir(context.Background(), outputDir, validator)
	if err != nil {
		t.Fatalf("ValidateOutputDir() error = %v", err)
	}

	if summary.Status != xaipipeline.ValidationStatusInvalid {
		t.Fatalf("Status = %q, want %q", summary.Status, xaipipeline.ValidationStatusInvalid)
	}
	if !validationIssuesContain(summary.Issues, "hyperframes project must not reference raw xAI shots") {
		t.Fatalf("Issues = %#v, want raw HyperFrames shot reference issue", summary.Issues)
	}
}

func TestValidateOutputDir_RejectsHyperFramesProjectAbsoluteRawShotReferences(t *testing.T) {
	t.Parallel()

	outputDir := t.TempDir()
	writeProductionValidationArtifacts(t, outputDir, true)
	indexPath := filepath.Join(outputDir, "hyperframes", "index.html")
	appendValidationHyperFramesAbsoluteRawShotReference(t, indexPath, outputDir, 1)
	validator := &validateStubOutputValidator{
		metadata: xaipipeline.RenderMetadata{
			Path:        filepath.Join(outputDir, "output_xai.mp4"),
			Width:       720,
			Height:      1280,
			FPS:         24,
			DurationSec: 8,
			CodecName:   "h264",
			PixelFormat: "yuv420p",
			SizeBytes:   validationMP4SizeBytes(),
			HasAudio:    false,
		},
	}

	summary, err := xaipipeline.ValidateOutputDir(context.Background(), outputDir, validator)
	if err != nil {
		t.Fatalf("ValidateOutputDir() error = %v", err)
	}

	if summary.Status != xaipipeline.ValidationStatusInvalid {
		t.Fatalf("Status = %q, want %q", summary.Status, xaipipeline.ValidationStatusInvalid)
	}
	if !validationIssuesContain(summary.Issues, "hyperframes project must not reference raw xAI shots") {
		t.Fatalf("Issues = %#v, want absolute raw HyperFrames shot reference issue", summary.Issues)
	}
}

func TestValidateOutputDir_RejectsHyperFramesProjectFileURLRawShotReferences(t *testing.T) {
	t.Parallel()

	outputDir := filepath.Join(t.TempDir(), "output with spaces")
	if err := os.MkdirAll(outputDir, 0755); err != nil {
		t.Fatalf("mkdir output dir: %v", err)
	}
	writeProductionValidationArtifacts(t, outputDir, true)
	indexPath := filepath.Join(outputDir, "hyperframes", "index.html")
	appendValidationHyperFramesFileURLRawShotReference(t, indexPath, outputDir, 1)
	validator := &validateStubOutputValidator{
		metadata: xaipipeline.RenderMetadata{
			Path:        filepath.Join(outputDir, "output_xai.mp4"),
			Width:       720,
			Height:      1280,
			FPS:         24,
			DurationSec: 8,
			CodecName:   "h264",
			PixelFormat: "yuv420p",
			SizeBytes:   validationMP4SizeBytes(),
			HasAudio:    false,
		},
	}

	summary, err := xaipipeline.ValidateOutputDir(context.Background(), outputDir, validator)
	if err != nil {
		t.Fatalf("ValidateOutputDir() error = %v", err)
	}

	if summary.Status != xaipipeline.ValidationStatusInvalid {
		t.Fatalf("Status = %q, want %q", summary.Status, xaipipeline.ValidationStatusInvalid)
	}
	if !validationIssuesContain(summary.Issues, "hyperframes project must not reference raw xAI shots") {
		t.Fatalf("Issues = %#v, want file URL raw HyperFrames shot reference issue", summary.Issues)
	}
}

func TestValidateOutputDir_RejectsHyperFramesProjectOutOfManifestOrder(t *testing.T) {
	t.Parallel()

	outputDir := t.TempDir()
	writeTwoShotProductionValidationArtifacts(t, outputDir)
	writeValidationHyperFramesIndex(t, filepath.Join(outputDir, "hyperframes", "index.html"), 2, 1)
	validator := &validateStubOutputValidator{
		metadata: xaipipeline.RenderMetadata{
			Path:        filepath.Join(outputDir, "output_xai.mp4"),
			Width:       720,
			Height:      1280,
			FPS:         24,
			DurationSec: 16,
			CodecName:   "h264",
			SizeBytes:   validationMP4SizeBytes(),
			HasAudio:    false,
		},
	}

	summary, err := xaipipeline.ValidateOutputDir(context.Background(), outputDir, validator)
	if err != nil {
		t.Fatalf("ValidateOutputDir() error = %v", err)
	}

	if summary.Status != xaipipeline.ValidationStatusInvalid {
		t.Fatalf("Status = %q, want %q", summary.Status, xaipipeline.ValidationStatusInvalid)
	}
	if !validationIssuesContain(summary.Issues, "hyperframes project normalized shot references are out of manifest order") {
		t.Fatalf("Issues = %#v, want HyperFrames shot order issue", summary.Issues)
	}
}

func TestValidateOutputDir_RejectsHyperFramesProjectRenderSpecMismatch(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		width     int
		height    int
		duration  float64
		wantIssue string
	}{
		{
			name:      "width",
			width:     1080,
			height:    1280,
			duration:  16,
			wantIssue: "hyperframes project data-width",
		},
		{
			name:      "height",
			width:     720,
			height:    1920,
			duration:  16,
			wantIssue: "hyperframes project data-height",
		},
		{
			name:      "duration",
			width:     720,
			height:    1280,
			duration:  8,
			wantIssue: "hyperframes project data-duration",
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			outputDir := t.TempDir()
			writeTwoShotProductionValidationArtifacts(t, outputDir)
			writeValidationHyperFramesIndexWithSpec(t, filepath.Join(outputDir, "hyperframes", "index.html"), tt.width, tt.height, tt.duration, 1, 2)
			validator := &validateStubOutputValidator{
				metadata: xaipipeline.RenderMetadata{
					Path:        filepath.Join(outputDir, "output_xai.mp4"),
					Width:       720,
					Height:      1280,
					FPS:         24,
					DurationSec: 16,
					CodecName:   "h264",
					SizeBytes:   validationMP4SizeBytes(),
					HasAudio:    false,
				},
			}

			summary, err := xaipipeline.ValidateOutputDir(context.Background(), outputDir, validator)
			if err != nil {
				t.Fatalf("ValidateOutputDir() error = %v", err)
			}

			if summary.Status != xaipipeline.ValidationStatusInvalid {
				t.Fatalf("Status = %q, want %q", summary.Status, xaipipeline.ValidationStatusInvalid)
			}
			if !validationIssuesContain(summary.Issues, tt.wantIssue) {
				t.Fatalf("Issues = %#v, want %q issue", summary.Issues, tt.wantIssue)
			}
		})
	}
}

func TestValidateOutputDir_RejectsHyperFramesProjectRenderSpecInCommentOnly(t *testing.T) {
	t.Parallel()

	outputDir := t.TempDir()
	writeProductionValidationArtifacts(t, outputDir, true)
	indexPath := filepath.Join(outputDir, "hyperframes", "index.html")
	replaceValidationHyperFramesIndex(t, indexPath, `data-width="720"`, `data-width="999"`)
	appendValidationHyperFramesComment(t, indexPath, `data-width="720"`)
	validator := &validateStubOutputValidator{
		metadata: xaipipeline.RenderMetadata{
			Path:        filepath.Join(outputDir, "output_xai.mp4"),
			Width:       720,
			Height:      1280,
			FPS:         24,
			DurationSec: 8,
			CodecName:   "h264",
			PixelFormat: "yuv420p",
			SizeBytes:   validationMP4SizeBytes(),
			HasAudio:    false,
		},
	}

	summary, err := xaipipeline.ValidateOutputDir(context.Background(), outputDir, validator)
	if err != nil {
		t.Fatalf("ValidateOutputDir() error = %v", err)
	}

	if summary.Status != xaipipeline.ValidationStatusInvalid {
		t.Fatalf("Status = %q, want %q", summary.Status, xaipipeline.ValidationStatusInvalid)
	}
	if !validationIssuesContain(summary.Issues, "hyperframes project data-width mismatch") {
		t.Fatalf("Issues = %#v, want HyperFrames width issue", summary.Issues)
	}
}

func TestValidateOutputDir_RejectsHyperFramesProjectShotTimingMismatch(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		clips     []validationHyperFramesClip
		wantIssue string
	}{
		{
			name: "start",
			clips: []validationHyperFramesClip{
				{Index: 1, StartSec: 0, DurationSec: 8},
				{Index: 2, StartSec: 4, DurationSec: 8},
			},
			wantIssue: "hyperframes project shot 2 data-start",
		},
		{
			name: "duration",
			clips: []validationHyperFramesClip{
				{Index: 1, StartSec: 0, DurationSec: 4},
				{Index: 2, StartSec: 8, DurationSec: 8},
			},
			wantIssue: "hyperframes project shot 1 data-duration",
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			outputDir := t.TempDir()
			writeTwoShotProductionValidationArtifacts(t, outputDir)
			writeValidationHyperFramesIndexWithClips(t, filepath.Join(outputDir, "hyperframes", "index.html"), 720, 1280, 16, tt.clips...)
			validator := &validateStubOutputValidator{
				metadata: xaipipeline.RenderMetadata{
					Path:        filepath.Join(outputDir, "output_xai.mp4"),
					Width:       720,
					Height:      1280,
					FPS:         24,
					DurationSec: 16,
					CodecName:   "h264",
					SizeBytes:   validationMP4SizeBytes(),
					HasAudio:    false,
				},
			}

			summary, err := xaipipeline.ValidateOutputDir(context.Background(), outputDir, validator)
			if err != nil {
				t.Fatalf("ValidateOutputDir() error = %v", err)
			}

			if summary.Status != xaipipeline.ValidationStatusInvalid {
				t.Fatalf("Status = %q, want %q", summary.Status, xaipipeline.ValidationStatusInvalid)
			}
			if !validationIssuesContain(summary.Issues, tt.wantIssue) {
				t.Fatalf("Issues = %#v, want %q issue", summary.Issues, tt.wantIssue)
			}
		})
	}
}

func TestValidateOutputDir_RejectsHyperFramesProjectMissingRuntimeHooks(t *testing.T) {
	t.Parallel()

	outputDir := t.TempDir()
	writeProductionValidationArtifacts(t, outputDir, true)
	writeValidationHyperFramesIndexWithoutRuntimeHooks(t, filepath.Join(outputDir, "hyperframes", "index.html"), 1)
	validator := &validateStubOutputValidator{
		metadata: xaipipeline.RenderMetadata{
			Path:        filepath.Join(outputDir, "output_xai.mp4"),
			Width:       720,
			Height:      1280,
			FPS:         24,
			DurationSec: 8,
			CodecName:   "h264",
			SizeBytes:   validationMP4SizeBytes(),
			HasAudio:    false,
		},
	}

	summary, err := xaipipeline.ValidateOutputDir(context.Background(), outputDir, validator)
	if err != nil {
		t.Fatalf("ValidateOutputDir() error = %v", err)
	}

	if summary.Status != xaipipeline.ValidationStatusInvalid {
		t.Fatalf("Status = %q, want %q", summary.Status, xaipipeline.ValidationStatusInvalid)
	}
	if !validationIssuesContain(summary.Issues, "hyperframes project missing runtime hook") {
		t.Fatalf("Issues = %#v, want HyperFrames runtime hook issue", summary.Issues)
	}
}

func TestValidateOutputDir_RejectsHyperFramesProjectRuntimeHooksInCommentOnly(t *testing.T) {
	t.Parallel()

	outputDir := t.TempDir()
	writeProductionValidationArtifacts(t, outputDir, true)
	indexPath := filepath.Join(outputDir, "hyperframes", "index.html")
	replaceValidationHyperFramesScriptWithComment(t, indexPath, `applyShotVisibility fadeSeconds style.opacity window.__timelines["xai-video"] window.__hf`)
	validator := &validateStubOutputValidator{
		metadata: xaipipeline.RenderMetadata{
			Path:        filepath.Join(outputDir, "output_xai.mp4"),
			Width:       720,
			Height:      1280,
			FPS:         24,
			DurationSec: 8,
			CodecName:   "h264",
			PixelFormat: "yuv420p",
			SizeBytes:   validationMP4SizeBytes(),
			HasAudio:    false,
		},
	}

	summary, err := xaipipeline.ValidateOutputDir(context.Background(), outputDir, validator)
	if err != nil {
		t.Fatalf("ValidateOutputDir() error = %v", err)
	}

	if summary.Status != xaipipeline.ValidationStatusInvalid {
		t.Fatalf("Status = %q, want %q", summary.Status, xaipipeline.ValidationStatusInvalid)
	}
	if !validationIssuesContain(summary.Issues, "hyperframes project missing runtime hook") {
		t.Fatalf("Issues = %#v, want HyperFrames runtime hook issue", summary.Issues)
	}
}

func TestValidateOutputDir_RejectsHyperFramesProjectRuntimeEvidenceInJavaScriptCommentOnly(t *testing.T) {
	t.Parallel()

	outputDir := t.TempDir()
	writeProductionValidationArtifacts(t, outputDir, true)
	indexPath := filepath.Join(outputDir, "hyperframes", "index.html")
	replaceValidationHyperFramesScriptText(t, indexPath, `// applyShotVisibility fadeSeconds style.opacity window.__timelines["xai-video"] window.__hf document.getElementById("video-1")`)
	validator := &validateStubOutputValidator{
		metadata: xaipipeline.RenderMetadata{
			Path:        filepath.Join(outputDir, "output_xai.mp4"),
			Width:       720,
			Height:      1280,
			FPS:         24,
			DurationSec: 8,
			CodecName:   "h264",
			PixelFormat: "yuv420p",
			SizeBytes:   validationMP4SizeBytes(),
			HasAudio:    false,
		},
	}

	summary, err := xaipipeline.ValidateOutputDir(context.Background(), outputDir, validator)
	if err != nil {
		t.Fatalf("ValidateOutputDir() error = %v", err)
	}

	if summary.Status != xaipipeline.ValidationStatusInvalid {
		t.Fatalf("Status = %q, want %q", summary.Status, xaipipeline.ValidationStatusInvalid)
	}
	if !validationIssuesContain(summary.Issues, "hyperframes project missing runtime hook: applyShotVisibility") {
		t.Fatalf("Issues = %#v, want HyperFrames runtime hook issue", summary.Issues)
	}
	if !validationIssuesContain(summary.Issues, `hyperframes project runtime missing video clip reference: document.getElementById("video-1")`) {
		t.Fatalf("Issues = %#v, want HyperFrames video runtime reference issue", summary.Issues)
	}
}

func TestValidateOutputDir_RejectsHyperFramesProjectRuntimeEvidenceInStringLiteralOnly(t *testing.T) {
	t.Parallel()

	outputDir := t.TempDir()
	writeProductionValidationArtifacts(t, outputDir, true)
	indexPath := filepath.Join(outputDir, "hyperframes", "index.html")
	replaceValidationHyperFramesScriptText(t, indexPath, `var fakeRuntimeEvidence = 'applyShotVisibility fadeSeconds style.opacity window.__timelines["xai-video"] window.__timelines["xai-video"] = timeline window.__hf window.__hf = { seek: timeline.seek shot.video.currentTime var local = t - start var target = shot.video.currentTime = target document.getElementById("video-1")';`)
	validator := &validateStubOutputValidator{
		metadata: xaipipeline.RenderMetadata{
			Path:        filepath.Join(outputDir, "output_xai.mp4"),
			Width:       720,
			Height:      1280,
			FPS:         24,
			DurationSec: 8,
			CodecName:   "h264",
			PixelFormat: "yuv420p",
			SizeBytes:   validationMP4SizeBytes(),
			HasAudio:    false,
		},
	}

	summary, err := xaipipeline.ValidateOutputDir(context.Background(), outputDir, validator)
	if err != nil {
		t.Fatalf("ValidateOutputDir() error = %v", err)
	}

	if summary.Status != xaipipeline.ValidationStatusInvalid {
		t.Fatalf("Status = %q, want %q", summary.Status, xaipipeline.ValidationStatusInvalid)
	}
	if !validationIssuesContain(summary.Issues, "hyperframes project missing runtime hook: applyShotVisibility") {
		t.Fatalf("Issues = %#v, want HyperFrames runtime hook issue", summary.Issues)
	}
	if !validationIssuesContain(summary.Issues, `hyperframes project runtime missing video clip reference: document.getElementById("video-1")`) {
		t.Fatalf("Issues = %#v, want HyperFrames video runtime reference issue", summary.Issues)
	}
}

func TestValidateOutputDir_RejectsHyperFramesProjectRuntimeEvidenceInTemplateLiteralOnly(t *testing.T) {
	t.Parallel()

	outputDir := t.TempDir()
	writeProductionValidationArtifacts(t, outputDir, true)
	indexPath := filepath.Join(outputDir, "hyperframes", "index.html")
	replaceValidationHyperFramesScriptText(t, indexPath, "var fakeRuntimeEvidence = `applyShotVisibility fadeSeconds style.opacity window.__timelines[\"xai-video\"] window.__timelines[\"xai-video\"] = timeline window.__hf window.__hf = { seek: timeline.seek shot.video.currentTime var local = t - start var target = shot.video.currentTime = target document.getElementById(\"video-1\")`;")
	validator := &validateStubOutputValidator{
		metadata: xaipipeline.RenderMetadata{
			Path:        filepath.Join(outputDir, "output_xai.mp4"),
			Width:       720,
			Height:      1280,
			FPS:         24,
			DurationSec: 8,
			CodecName:   "h264",
			PixelFormat: "yuv420p",
			SizeBytes:   validationMP4SizeBytes(),
			HasAudio:    false,
		},
	}

	summary, err := xaipipeline.ValidateOutputDir(context.Background(), outputDir, validator)
	if err != nil {
		t.Fatalf("ValidateOutputDir() error = %v", err)
	}

	if summary.Status != xaipipeline.ValidationStatusInvalid {
		t.Fatalf("Status = %q, want %q", summary.Status, xaipipeline.ValidationStatusInvalid)
	}
	if !validationIssuesContain(summary.Issues, "hyperframes project missing runtime hook: applyShotVisibility") {
		t.Fatalf("Issues = %#v, want HyperFrames runtime hook issue", summary.Issues)
	}
	if !validationIssuesContain(summary.Issues, `hyperframes project runtime missing video clip reference: document.getElementById("video-1")`) {
		t.Fatalf("Issues = %#v, want HyperFrames video runtime reference issue", summary.Issues)
	}
}

func TestValidateOutputDir_RejectsHyperFramesProjectRuntimeEvidenceInRegexLiteralOnly(t *testing.T) {
	t.Parallel()

	outputDir := t.TempDir()
	writeProductionValidationArtifacts(t, outputDir, true)
	indexPath := filepath.Join(outputDir, "hyperframes", "index.html")
	replaceValidationHyperFramesScriptText(t, indexPath, `var fakeRuntimeEvidence = /applyShotVisibility fadeSeconds style.opacity window.__timelines["xai-video"] window.__timelines["xai-video"] = timeline window.__hf window.__hf = { seek: timeline.seek shot.video.currentTime var local = t - start var target = shot.video.currentTime = target document.getElementById("video-1")/;`)
	validator := &validateStubOutputValidator{
		metadata: xaipipeline.RenderMetadata{
			Path:        filepath.Join(outputDir, "output_xai.mp4"),
			Width:       720,
			Height:      1280,
			FPS:         24,
			DurationSec: 8,
			CodecName:   "h264",
			PixelFormat: "yuv420p",
			SizeBytes:   validationMP4SizeBytes(),
			HasAudio:    false,
		},
	}

	summary, err := xaipipeline.ValidateOutputDir(context.Background(), outputDir, validator)
	if err != nil {
		t.Fatalf("ValidateOutputDir() error = %v", err)
	}

	if summary.Status != xaipipeline.ValidationStatusInvalid {
		t.Fatalf("Status = %q, want %q", summary.Status, xaipipeline.ValidationStatusInvalid)
	}
	if !validationIssuesContain(summary.Issues, "hyperframes project missing runtime hook: applyShotVisibility") {
		t.Fatalf("Issues = %#v, want HyperFrames runtime hook issue", summary.Issues)
	}
	if !validationIssuesContain(summary.Issues, `hyperframes project runtime missing video clip reference: document.getElementById("video-1")`) {
		t.Fatalf("Issues = %#v, want HyperFrames video runtime reference issue", summary.Issues)
	}
}

func TestValidateOutputDir_AcceptsHyperFramesRuntimeEvidenceAfterDivisionExpression(t *testing.T) {
	t.Parallel()

	outputDir := t.TempDir()
	writeProductionValidationArtifacts(t, outputDir, true)
	indexPath := filepath.Join(outputDir, "hyperframes", "index.html")
	runtimeScript := validationHyperFramesRuntimeScript(validationHyperFramesClips(1), true)
	runtimeScript = strings.TrimPrefix(runtimeScript, "<script>")
	runtimeScript = strings.TrimSuffix(runtimeScript, "</script>")
	replaceValidationHyperFramesScriptText(t, indexPath, `var aspect = 720 / 1280; `+runtimeScript)
	validator := &validateStubOutputValidator{
		metadata: xaipipeline.RenderMetadata{
			Path:        filepath.Join(outputDir, "output_xai.mp4"),
			Width:       720,
			Height:      1280,
			FPS:         24,
			DurationSec: 8,
			CodecName:   "h264",
			PixelFormat: "yuv420p",
			SizeBytes:   validationMP4SizeBytes(),
			HasAudio:    false,
		},
	}

	summary, err := xaipipeline.ValidateOutputDir(context.Background(), outputDir, validator)
	if err != nil {
		t.Fatalf("ValidateOutputDir() error = %v", err)
	}

	if summary.Status != xaipipeline.ValidationStatusValid {
		t.Fatalf("Status = %q, want %q; issues = %#v", summary.Status, xaipipeline.ValidationStatusValid, summary.Issues)
	}
}

func TestValidateOutputDir_RejectsHyperFramesProjectMissingSeekRuntimeBinding(t *testing.T) {
	t.Parallel()

	outputDir := t.TempDir()
	writeProductionValidationArtifacts(t, outputDir, true)
	indexPath := filepath.Join(outputDir, "hyperframes", "index.html")
	replaceValidationHyperFramesScriptText(t, indexPath, validationHyperFramesRuntimeScriptWithoutSeek(validationHyperFramesClips(1)))
	validator := &validateStubOutputValidator{
		metadata: xaipipeline.RenderMetadata{
			Path:        filepath.Join(outputDir, "output_xai.mp4"),
			Width:       720,
			Height:      1280,
			FPS:         24,
			DurationSec: 8,
			CodecName:   "h264",
			PixelFormat: "yuv420p",
			SizeBytes:   validationMP4SizeBytes(),
			HasAudio:    false,
		},
	}

	summary, err := xaipipeline.ValidateOutputDir(context.Background(), outputDir, validator)
	if err != nil {
		t.Fatalf("ValidateOutputDir() error = %v", err)
	}

	if summary.Status != xaipipeline.ValidationStatusInvalid {
		t.Fatalf("Status = %q, want %q", summary.Status, xaipipeline.ValidationStatusInvalid)
	}
	if !validationIssuesContain(summary.Issues, "hyperframes project missing runtime hook: window.__hf = { seek:") {
		t.Fatalf("Issues = %#v, want HyperFrames __hf seek issue", summary.Issues)
	}
}

func TestValidateOutputDir_RejectsHyperFramesProjectSeekWithoutVideoCurrentTime(t *testing.T) {
	t.Parallel()

	outputDir := t.TempDir()
	writeProductionValidationArtifacts(t, outputDir, true)
	indexPath := filepath.Join(outputDir, "hyperframes", "index.html")
	replaceValidationHyperFramesScriptText(t, indexPath, validationHyperFramesRuntimeScriptWithoutCurrentTime(validationHyperFramesClips(1)))
	validator := &validateStubOutputValidator{
		metadata: xaipipeline.RenderMetadata{
			Path:        filepath.Join(outputDir, "output_xai.mp4"),
			Width:       720,
			Height:      1280,
			FPS:         24,
			DurationSec: 8,
			CodecName:   "h264",
			PixelFormat: "yuv420p",
			SizeBytes:   validationMP4SizeBytes(),
			HasAudio:    false,
		},
	}

	summary, err := xaipipeline.ValidateOutputDir(context.Background(), outputDir, validator)
	if err != nil {
		t.Fatalf("ValidateOutputDir() error = %v", err)
	}

	if summary.Status != xaipipeline.ValidationStatusInvalid {
		t.Fatalf("Status = %q, want %q", summary.Status, xaipipeline.ValidationStatusInvalid)
	}
	if !validationIssuesContain(summary.Issues, "hyperframes project missing runtime hook: shot.video.currentTime") {
		t.Fatalf("Issues = %#v, want HyperFrames video currentTime issue", summary.Issues)
	}
}

func TestValidateOutputDir_RejectsHyperFramesProjectSeekWithoutShotLocalTime(t *testing.T) {
	t.Parallel()

	outputDir := t.TempDir()
	writeProductionValidationArtifacts(t, outputDir, true)
	indexPath := filepath.Join(outputDir, "hyperframes", "index.html")
	replaceValidationHyperFramesScriptText(t, indexPath, validationHyperFramesRuntimeScriptWithoutLocalTime(validationHyperFramesClips(1)))
	validator := &validateStubOutputValidator{
		metadata: xaipipeline.RenderMetadata{
			Path:        filepath.Join(outputDir, "output_xai.mp4"),
			Width:       720,
			Height:      1280,
			FPS:         24,
			DurationSec: 8,
			CodecName:   "h264",
			PixelFormat: "yuv420p",
			SizeBytes:   validationMP4SizeBytes(),
			HasAudio:    false,
		},
	}

	summary, err := xaipipeline.ValidateOutputDir(context.Background(), outputDir, validator)
	if err != nil {
		t.Fatalf("ValidateOutputDir() error = %v", err)
	}

	if summary.Status != xaipipeline.ValidationStatusInvalid {
		t.Fatalf("Status = %q, want %q", summary.Status, xaipipeline.ValidationStatusInvalid)
	}
	if !validationIssuesContain(summary.Issues, "hyperframes project missing runtime hook: var local = t - start") {
		t.Fatalf("Issues = %#v, want HyperFrames local seek issue", summary.Issues)
	}
}

func TestValidateOutputDir_RejectsHyperFramesProjectVideoRuntimeReferenceMismatch(t *testing.T) {
	t.Parallel()

	outputDir := t.TempDir()
	writeProductionValidationArtifacts(t, outputDir, true)
	indexPath := filepath.Join(outputDir, "hyperframes", "index.html")
	replaceValidationHyperFramesIndex(t, indexPath, `document.getElementById("video-1")`, `document.getElementById("video-stale")`)
	validator := &validateStubOutputValidator{
		metadata: xaipipeline.RenderMetadata{
			Path:        filepath.Join(outputDir, "output_xai.mp4"),
			Width:       720,
			Height:      1280,
			FPS:         24,
			DurationSec: 8,
			CodecName:   "h264",
			PixelFormat: "yuv420p",
			SizeBytes:   validationMP4SizeBytes(),
			HasAudio:    false,
		},
	}

	summary, err := xaipipeline.ValidateOutputDir(context.Background(), outputDir, validator)
	if err != nil {
		t.Fatalf("ValidateOutputDir() error = %v", err)
	}

	if summary.Status != xaipipeline.ValidationStatusInvalid {
		t.Fatalf("Status = %q, want %q", summary.Status, xaipipeline.ValidationStatusInvalid)
	}
	if !validationIssuesContain(summary.Issues, `hyperframes project runtime missing video clip reference: document.getElementById("video-1")`) {
		t.Fatalf("Issues = %#v, want HyperFrames video runtime reference issue", summary.Issues)
	}
}

func TestValidateOutputDir_RejectsHyperFramesProjectSubtitleRuntimeReferenceMismatch(t *testing.T) {
	t.Parallel()

	outputDir := t.TempDir()
	writeProductionValidationArtifactsWithSubtitle(t, outputDir, "第一個字幕")
	indexPath := filepath.Join(outputDir, "hyperframes", "index.html")
	writeValidationHyperFramesIndexWithSubtitle(t, indexPath, "第一個字幕")
	replaceValidationHyperFramesIndex(t, indexPath, `document.getElementById("subtitle-1")`, `document.getElementById("subtitle-stale")`)
	validator := &validateStubOutputValidator{
		metadata: xaipipeline.RenderMetadata{
			Path:        filepath.Join(outputDir, "output_xai.mp4"),
			Width:       720,
			Height:      1280,
			FPS:         24,
			DurationSec: 8,
			CodecName:   "h264",
			PixelFormat: "yuv420p",
			SizeBytes:   validationMP4SizeBytes(),
			HasAudio:    false,
		},
	}

	summary, err := xaipipeline.ValidateOutputDir(context.Background(), outputDir, validator)
	if err != nil {
		t.Fatalf("ValidateOutputDir() error = %v", err)
	}

	if summary.Status != xaipipeline.ValidationStatusInvalid {
		t.Fatalf("Status = %q, want %q", summary.Status, xaipipeline.ValidationStatusInvalid)
	}
	if !validationIssuesContain(summary.Issues, `hyperframes project runtime missing subtitle clip reference: document.getElementById("subtitle-1")`) {
		t.Fatalf("Issues = %#v, want HyperFrames subtitle runtime reference issue", summary.Issues)
	}
}

func TestValidateOutputDir_RejectsHyperFramesProjectCompositionIDInCommentOnly(t *testing.T) {
	t.Parallel()

	outputDir := t.TempDir()
	writeProductionValidationArtifacts(t, outputDir, true)
	indexPath := filepath.Join(outputDir, "hyperframes", "index.html")
	replaceValidationHyperFramesIndex(t, indexPath, `data-composition-id="xai-video"`, `data-composition-id="not-xai-video"`)
	appendValidationHyperFramesComment(t, indexPath, `data-composition-id="xai-video"`)
	validator := &validateStubOutputValidator{
		metadata: xaipipeline.RenderMetadata{
			Path:        filepath.Join(outputDir, "output_xai.mp4"),
			Width:       720,
			Height:      1280,
			FPS:         24,
			DurationSec: 8,
			CodecName:   "h264",
			PixelFormat: "yuv420p",
			SizeBytes:   validationMP4SizeBytes(),
			HasAudio:    false,
		},
	}

	summary, err := xaipipeline.ValidateOutputDir(context.Background(), outputDir, validator)
	if err != nil {
		t.Fatalf("ValidateOutputDir() error = %v", err)
	}

	if summary.Status != xaipipeline.ValidationStatusInvalid {
		t.Fatalf("Status = %q, want %q", summary.Status, xaipipeline.ValidationStatusInvalid)
	}
	if !validationIssuesContain(summary.Issues, `hyperframes project missing runtime hook: data-composition-id="xai-video"`) {
		t.Fatalf("Issues = %#v, want HyperFrames composition id issue", summary.Issues)
	}
}

func TestValidateOutputDir_RejectsHyperFramesProjectMissingVisibilityRuntime(t *testing.T) {
	t.Parallel()

	outputDir := t.TempDir()
	writeProductionValidationArtifacts(t, outputDir, true)
	writeValidationHyperFramesIndexWithoutVisibilityRuntime(t, filepath.Join(outputDir, "hyperframes", "index.html"), 1)
	validator := &validateStubOutputValidator{
		metadata: xaipipeline.RenderMetadata{
			Path:        filepath.Join(outputDir, "output_xai.mp4"),
			Width:       720,
			Height:      1280,
			FPS:         24,
			DurationSec: 8,
			CodecName:   "h264",
			PixelFormat: "yuv420p",
			SizeBytes:   validationMP4SizeBytes(),
			HasAudio:    false,
		},
	}

	summary, err := xaipipeline.ValidateOutputDir(context.Background(), outputDir, validator)
	if err != nil {
		t.Fatalf("ValidateOutputDir() error = %v", err)
	}

	if summary.Status != xaipipeline.ValidationStatusInvalid {
		t.Fatalf("Status = %q, want %q", summary.Status, xaipipeline.ValidationStatusInvalid)
	}
	if !validationIssuesContain(summary.Issues, "hyperframes project missing runtime hook: applyShotVisibility") {
		t.Fatalf("Issues = %#v, want HyperFrames visibility runtime issue", summary.Issues)
	}
}

func TestValidateOutputDir_RejectsHyperFramesProjectMissingManifestSubtitle(t *testing.T) {
	t.Parallel()

	outputDir := t.TempDir()
	writeProductionValidationArtifactsWithSubtitle(t, outputDir, "第一個字幕")
	writeValidationHyperFramesIndex(t, filepath.Join(outputDir, "hyperframes", "index.html"), 1)
	validator := &validateStubOutputValidator{
		metadata: xaipipeline.RenderMetadata{
			Path:        filepath.Join(outputDir, "output_xai.mp4"),
			Width:       720,
			Height:      1280,
			FPS:         24,
			DurationSec: 8,
			CodecName:   "h264",
			SizeBytes:   validationMP4SizeBytes(),
			HasAudio:    false,
		},
	}

	summary, err := xaipipeline.ValidateOutputDir(context.Background(), outputDir, validator)
	if err != nil {
		t.Fatalf("ValidateOutputDir() error = %v", err)
	}

	if summary.Status != xaipipeline.ValidationStatusInvalid {
		t.Fatalf("Status = %q, want %q", summary.Status, xaipipeline.ValidationStatusInvalid)
	}
	if !validationIssuesContain(summary.Issues, "hyperframes project missing subtitle clip") {
		t.Fatalf("Issues = %#v, want HyperFrames subtitle issue", summary.Issues)
	}
}

func TestValidateOutputDir_RejectsNonCanonicalManifestSubtitle(t *testing.T) {
	t.Parallel()

	outputDir := t.TempDir()
	writeProductionValidationArtifactsWithSubtitle(t, outputDir, " 第一個字幕 ")
	writeValidationHyperFramesIndexWithSubtitle(t, filepath.Join(outputDir, "hyperframes", "index.html"), "第一個字幕")
	validator := &validateStubOutputValidator{
		metadata: xaipipeline.RenderMetadata{
			Path:        filepath.Join(outputDir, "output_xai.mp4"),
			Width:       720,
			Height:      1280,
			FPS:         24,
			DurationSec: 8,
			CodecName:   "h264",
			PixelFormat: "yuv420p",
			SizeBytes:   validationMP4SizeBytes(),
			HasAudio:    false,
		},
	}

	summary, err := xaipipeline.ValidateOutputDir(context.Background(), outputDir, validator)
	if err != nil {
		t.Fatalf("ValidateOutputDir() error = %v", err)
	}

	if summary.Status != xaipipeline.ValidationStatusInvalid {
		t.Fatalf("Status = %q, want %q", summary.Status, xaipipeline.ValidationStatusInvalid)
	}
	if !validationIssuesContain(summary.Issues, `manifest shot 1 subtitle " 第一個字幕 " is not canonical, want "第一個字幕"`) {
		t.Fatalf("Issues = %#v, want manifest subtitle canonical issue", summary.Issues)
	}
}

func TestValidateOutputDir_RejectsHyperFramesProjectSubtitleEvidenceInCommentOnly(t *testing.T) {
	t.Parallel()

	outputDir := t.TempDir()
	writeProductionValidationArtifactsWithSubtitle(t, outputDir, "第一個字幕")
	indexPath := filepath.Join(outputDir, "hyperframes", "index.html")
	writeValidationHyperFramesIndex(t, indexPath, 1)
	insertValidationHyperFramesCommentBeforeScript(t, indexPath, `id="subtitle-1" class="subtitle clip" data-start="0.000" data-duration="8.000" 第一個字幕`)
	validator := &validateStubOutputValidator{
		metadata: xaipipeline.RenderMetadata{
			Path:        filepath.Join(outputDir, "output_xai.mp4"),
			Width:       720,
			Height:      1280,
			FPS:         24,
			DurationSec: 8,
			CodecName:   "h264",
			PixelFormat: "yuv420p",
			SizeBytes:   validationMP4SizeBytes(),
			HasAudio:    false,
		},
	}

	summary, err := xaipipeline.ValidateOutputDir(context.Background(), outputDir, validator)
	if err != nil {
		t.Fatalf("ValidateOutputDir() error = %v", err)
	}

	if summary.Status != xaipipeline.ValidationStatusInvalid {
		t.Fatalf("Status = %q, want %q", summary.Status, xaipipeline.ValidationStatusInvalid)
	}
	if !validationIssuesContain(summary.Issues, "hyperframes project missing subtitle clip") {
		t.Fatalf("Issues = %#v, want HyperFrames subtitle issue", summary.Issues)
	}
}

func TestValidateOutputDir_RejectsHyperFramesProjectMissingManifestTransition(t *testing.T) {
	t.Parallel()

	outputDir := t.TempDir()
	writeProductionValidationArtifactsWithTransition(t, outputDir, "fade")
	writeValidationHyperFramesIndex(t, filepath.Join(outputDir, "hyperframes", "index.html"), 1)
	validator := &validateStubOutputValidator{
		metadata: xaipipeline.RenderMetadata{
			Path:        filepath.Join(outputDir, "output_xai.mp4"),
			Width:       720,
			Height:      1280,
			FPS:         24,
			DurationSec: 8,
			CodecName:   "h264",
			PixelFormat: "yuv420p",
			SizeBytes:   validationMP4SizeBytes(),
			HasAudio:    false,
		},
	}

	summary, err := xaipipeline.ValidateOutputDir(context.Background(), outputDir, validator)
	if err != nil {
		t.Fatalf("ValidateOutputDir() error = %v", err)
	}

	if summary.Status != xaipipeline.ValidationStatusInvalid {
		t.Fatalf("Status = %q, want %q", summary.Status, xaipipeline.ValidationStatusInvalid)
	}
	if !validationIssuesContain(summary.Issues, "hyperframes project shot 1 missing transition metadata") {
		t.Fatalf("Issues = %#v, want HyperFrames transition issue", summary.Issues)
	}
}

func TestValidateOutputDir_RejectsNonCanonicalManifestTransition(t *testing.T) {
	t.Parallel()

	outputDir := t.TempDir()
	writeProductionValidationArtifactsWithTransition(t, outputDir, " Fade ")
	indexPath := filepath.Join(outputDir, "hyperframes", "index.html")
	replaceValidationHyperFramesIndex(
		t,
		indexPath,
		`<video id="video-1" class="shot-video clip" src="../normalized/shot_001.mp4" data-start="0.000" data-duration="8.000" data-track-index="0"></video>`,
		`<video id="video-1" class="shot-video clip" src="../normalized/shot_001.mp4" data-start="0.000" data-duration="8.000" data-track-index="0" data-transition-out="fade"></video>`,
	)
	validator := &validateStubOutputValidator{
		metadata: xaipipeline.RenderMetadata{
			Path:        filepath.Join(outputDir, "output_xai.mp4"),
			Width:       720,
			Height:      1280,
			FPS:         24,
			DurationSec: 8,
			CodecName:   "h264",
			PixelFormat: "yuv420p",
			SizeBytes:   validationMP4SizeBytes(),
			HasAudio:    false,
		},
	}

	summary, err := xaipipeline.ValidateOutputDir(context.Background(), outputDir, validator)
	if err != nil {
		t.Fatalf("ValidateOutputDir() error = %v", err)
	}

	if summary.Status != xaipipeline.ValidationStatusInvalid {
		t.Fatalf("Status = %q, want %q", summary.Status, xaipipeline.ValidationStatusInvalid)
	}
	if !validationIssuesContain(summary.Issues, `manifest shot 1 transition_out " Fade " is not canonical, want "fade"`) {
		t.Fatalf("Issues = %#v, want manifest transition canonical issue", summary.Issues)
	}
}

func TestValidateOutputDir_AcceptsHyperFramesProjectTransitionAttributeWhitespace(t *testing.T) {
	t.Parallel()

	outputDir := t.TempDir()
	writeProductionValidationArtifactsWithTransition(t, outputDir, "fade")
	indexPath := filepath.Join(outputDir, "hyperframes", "index.html")
	replaceValidationHyperFramesIndex(
		t,
		indexPath,
		`<video id="video-1" class="shot-video clip" src="../normalized/shot_001.mp4" data-start="0.000" data-duration="8.000" data-track-index="0"></video>`,
		`<video id="video-1" class="shot-video clip" src="../normalized/shot_001.mp4" data-start="0.000" data-duration="8.000" data-track-index="0" data-transition-out = "fade"></video>`,
	)
	validator := &validateStubOutputValidator{
		metadata: xaipipeline.RenderMetadata{
			Path:        filepath.Join(outputDir, "output_xai.mp4"),
			Width:       720,
			Height:      1280,
			FPS:         24,
			DurationSec: 8,
			CodecName:   "h264",
			PixelFormat: "yuv420p",
			SizeBytes:   validationMP4SizeBytes(),
			HasAudio:    false,
		},
	}

	summary, err := xaipipeline.ValidateOutputDir(context.Background(), outputDir, validator)
	if err != nil {
		t.Fatalf("ValidateOutputDir() error = %v", err)
	}

	if summary.Status != xaipipeline.ValidationStatusValid {
		t.Fatalf("Status = %q, want %q; issues=%#v", summary.Status, xaipipeline.ValidationStatusValid, summary.Issues)
	}
}

func TestValidateOutputDir_RejectsHyperFramesProjectBareVideoClip(t *testing.T) {
	t.Parallel()

	outputDir := t.TempDir()
	writeProductionValidationArtifacts(t, outputDir, true)
	writeValidationHyperFramesIndexWithBareVideo(t, filepath.Join(outputDir, "hyperframes", "index.html"), 1)
	validator := &validateStubOutputValidator{
		metadata: xaipipeline.RenderMetadata{
			Path:        filepath.Join(outputDir, "output_xai.mp4"),
			Width:       720,
			Height:      1280,
			FPS:         24,
			DurationSec: 8,
			CodecName:   "h264",
			PixelFormat: "yuv420p",
			SizeBytes:   validationMP4SizeBytes(),
			HasAudio:    false,
		},
	}

	summary, err := xaipipeline.ValidateOutputDir(context.Background(), outputDir, validator)
	if err != nil {
		t.Fatalf("ValidateOutputDir() error = %v", err)
	}

	if summary.Status != xaipipeline.ValidationStatusInvalid {
		t.Fatalf("Status = %q, want %q", summary.Status, xaipipeline.ValidationStatusInvalid)
	}
	if !validationIssuesContain(summary.Issues, `hyperframes project shot 1 video clip missing id="video-1"`) {
		t.Fatalf("Issues = %#v, want HyperFrames video id issue", summary.Issues)
	}
	if !validationIssuesContain(summary.Issues, `hyperframes project shot 1 video clip missing class="shot-video clip"`) {
		t.Fatalf("Issues = %#v, want HyperFrames video class issue", summary.Issues)
	}
}

func TestValidateOutputDir_RejectsHyperFramesProjectVideoTrackIndexMismatch(t *testing.T) {
	t.Parallel()

	outputDir := t.TempDir()
	writeProductionValidationArtifacts(t, outputDir, true)
	indexPath := filepath.Join(outputDir, "hyperframes", "index.html")
	replaceValidationHyperFramesIndex(t, indexPath, `data-track-index="0"`, `data-track-index="3"`)
	validator := &validateStubOutputValidator{
		metadata: xaipipeline.RenderMetadata{
			Path:        filepath.Join(outputDir, "output_xai.mp4"),
			Width:       720,
			Height:      1280,
			FPS:         24,
			DurationSec: 8,
			CodecName:   "h264",
			PixelFormat: "yuv420p",
			SizeBytes:   validationMP4SizeBytes(),
			HasAudio:    false,
		},
	}

	summary, err := xaipipeline.ValidateOutputDir(context.Background(), outputDir, validator)
	if err != nil {
		t.Fatalf("ValidateOutputDir() error = %v", err)
	}

	if summary.Status != xaipipeline.ValidationStatusInvalid {
		t.Fatalf("Status = %q, want %q", summary.Status, xaipipeline.ValidationStatusInvalid)
	}
	if !validationIssuesContain(summary.Issues, `hyperframes project shot 1 video clip data-track-index mismatch`) {
		t.Fatalf("Issues = %#v, want HyperFrames video track issue", summary.Issues)
	}
}

func TestValidateOutputDir_RejectsMissingHyperFramesPackageManifest(t *testing.T) {
	t.Parallel()

	outputDir := t.TempDir()
	writeProductionValidationArtifacts(t, outputDir, true)
	if err := os.Remove(filepath.Join(outputDir, "hyperframes", "package.json")); err != nil {
		t.Fatalf("remove hyperframes package manifest: %v", err)
	}
	validator := &validateStubOutputValidator{
		metadata: xaipipeline.RenderMetadata{
			Path:        filepath.Join(outputDir, "output_xai.mp4"),
			Width:       720,
			Height:      1280,
			FPS:         24,
			DurationSec: 8,
			CodecName:   "h264",
			SizeBytes:   validationMP4SizeBytes(),
			HasAudio:    false,
		},
	}

	summary, err := xaipipeline.ValidateOutputDir(context.Background(), outputDir, validator)
	if err != nil {
		t.Fatalf("ValidateOutputDir() error = %v", err)
	}

	if summary.Status != xaipipeline.ValidationStatusInvalid {
		t.Fatalf("Status = %q, want %q", summary.Status, xaipipeline.ValidationStatusInvalid)
	}
	if !validationIssuesContain(summary.Issues, "missing artifact hyperframes/package.json") {
		t.Fatalf("Issues = %#v, want missing HyperFrames package manifest issue", summary.Issues)
	}
	if validator.callCount != 0 {
		t.Fatalf("validator calls = %d, want no ffprobe calls for incomplete inspect", validator.callCount)
	}
}

func TestValidateOutputDir_RejectsInvalidHyperFramesPackageManifest(t *testing.T) {
	t.Parallel()

	outputDir := t.TempDir()
	writeProductionValidationArtifacts(t, outputDir, true)
	writeInspectFile(t, filepath.Join(outputDir, "hyperframes", "package.json"))
	validator := &validateStubOutputValidator{
		metadata: xaipipeline.RenderMetadata{
			Path:        filepath.Join(outputDir, "output_xai.mp4"),
			Width:       720,
			Height:      1280,
			FPS:         24,
			DurationSec: 8,
			CodecName:   "h264",
			SizeBytes:   validationMP4SizeBytes(),
			HasAudio:    false,
		},
	}

	summary, err := xaipipeline.ValidateOutputDir(context.Background(), outputDir, validator)
	if err != nil {
		t.Fatalf("ValidateOutputDir() error = %v", err)
	}

	if summary.Status != xaipipeline.ValidationStatusInvalid {
		t.Fatalf("Status = %q, want %q", summary.Status, xaipipeline.ValidationStatusInvalid)
	}
	if !validationIssuesContain(summary.Issues, "hyperframes package.json invalid JSON") {
		t.Fatalf("Issues = %#v, want HyperFrames package JSON issue", summary.Issues)
	}
}

func TestValidateOutputDir_RejectsNonMP4VideoArtifacts(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		path      string
		wantIssue string
	}{
		{
			name:      "raw shot",
			path:      filepath.Join("shots", "shot_001.mp4"),
			wantIssue: "shot artifact for shot 1 is not an MP4",
		},
		{
			name:      "normalized shot",
			path:      filepath.Join("normalized", "shot_001.mp4"),
			wantIssue: "normalized shot artifact for shot 1 is not an MP4",
		},
		{
			name:      "hyperframes timeline",
			path:      "timeline_hyperframes.mp4",
			wantIssue: "hyperframes timeline artifact is not an MP4",
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			outputDir := t.TempDir()
			writeProductionValidationArtifacts(t, outputDir, true)
			writeInspectFile(t, filepath.Join(outputDir, tt.path))
			validator := &validateStubOutputValidator{
				metadata: xaipipeline.RenderMetadata{
					Path:        filepath.Join(outputDir, "output_xai.mp4"),
					Width:       720,
					Height:      1280,
					FPS:         24,
					DurationSec: 8,
					CodecName:   "h264",
					SizeBytes:   validationMP4SizeBytes(),
					HasAudio:    false,
				},
			}

			summary, err := xaipipeline.ValidateOutputDir(context.Background(), outputDir, validator)
			if err != nil {
				t.Fatalf("ValidateOutputDir() error = %v", err)
			}

			if summary.Status != xaipipeline.ValidationStatusInvalid {
				t.Fatalf("Status = %q, want %q", summary.Status, xaipipeline.ValidationStatusInvalid)
			}
			if !validationIssuesContain(summary.Issues, tt.wantIssue) {
				t.Fatalf("Issues = %#v, want %q issue", summary.Issues, tt.wantIssue)
			}
		})
	}
}

func TestValidateOutputDir_RejectsStagedShotArtifacts(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		filename  string
		wantIssue string
	}{
		{
			name:      "temporary shot",
			filename:  ".shot_001_123.tmp.mp4",
			wantIssue: "staged xAI shot artifact is not allowed",
		},
		{
			name:      "backup shot",
			filename:  ".shot_001.mp4_123.bak",
			wantIssue: "staged xAI shot artifact is not allowed",
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			outputDir := t.TempDir()
			writeProductionValidationArtifacts(t, outputDir, true)
			if err := os.WriteFile(filepath.Join(outputDir, "shots", tt.filename), validationMP4Bytes(), 0644); err != nil {
				t.Fatalf("write staged artifact: %v", err)
			}
			validator := &validateStubOutputValidator{
				metadata: xaipipeline.RenderMetadata{
					Path:        filepath.Join(outputDir, "output_xai.mp4"),
					Width:       720,
					Height:      1280,
					FPS:         24,
					DurationSec: 8,
					CodecName:   "h264",
					PixelFormat: "yuv420p",
					SizeBytes:   validationMP4SizeBytes(),
					HasAudio:    false,
				},
			}

			summary, err := xaipipeline.ValidateOutputDir(context.Background(), outputDir, validator)
			if err != nil {
				t.Fatalf("ValidateOutputDir() error = %v", err)
			}

			if summary.Status != xaipipeline.ValidationStatusInvalid {
				t.Fatalf("Status = %q, want %q", summary.Status, xaipipeline.ValidationStatusInvalid)
			}
			if !validationIssuesContain(summary.Issues, tt.wantIssue) {
				t.Fatalf("Issues = %#v, want %q issue", summary.Issues, tt.wantIssue)
			}
		})
	}
}

func TestValidateOutputDir_RejectsStagedNormalizedArtifacts(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		filename  string
		wantIssue string
	}{
		{
			name:      "temporary normalized shot",
			filename:  ".shot_001.mp4_123.tmp.mp4",
			wantIssue: "staged xAI normalized artifact is not allowed",
		},
		{
			name:      "legacy temporary normalized shot",
			filename:  ".shot_001.mp4_123.tmp",
			wantIssue: "staged xAI normalized artifact is not allowed",
		},
		{
			name:      "backup normalized shot",
			filename:  ".shot_001.mp4_123.bak",
			wantIssue: "staged xAI normalized artifact is not allowed",
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			outputDir := t.TempDir()
			writeProductionValidationArtifacts(t, outputDir, true)
			if err := os.WriteFile(filepath.Join(outputDir, "normalized", tt.filename), validationMP4Bytes(), 0644); err != nil {
				t.Fatalf("write staged normalized artifact: %v", err)
			}
			validator := &validateStubOutputValidator{
				metadata: xaipipeline.RenderMetadata{
					Path:        filepath.Join(outputDir, "output_xai.mp4"),
					Width:       720,
					Height:      1280,
					FPS:         24,
					DurationSec: 8,
					CodecName:   "h264",
					PixelFormat: "yuv420p",
					SizeBytes:   validationMP4SizeBytes(),
					HasAudio:    false,
				},
			}

			summary, err := xaipipeline.ValidateOutputDir(context.Background(), outputDir, validator)
			if err != nil {
				t.Fatalf("ValidateOutputDir() error = %v", err)
			}

			if summary.Status != xaipipeline.ValidationStatusInvalid {
				t.Fatalf("Status = %q, want %q", summary.Status, xaipipeline.ValidationStatusInvalid)
			}
			if !validationIssuesContain(summary.Issues, tt.wantIssue) {
				t.Fatalf("Issues = %#v, want %q issue", summary.Issues, tt.wantIssue)
			}
		})
	}
}

func TestValidateOutputDir_RejectsStagedHyperFramesProjectArtifacts(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		filename  string
		wantIssue string
	}{
		{
			name:      "index temp",
			filename:  ".index.html_123.tmp",
			wantIssue: "staged xAI HyperFrames project artifact is not allowed",
		},
		{
			name:      "index backup",
			filename:  ".index.html_123.bak",
			wantIssue: "staged xAI HyperFrames project artifact is not allowed",
		},
		{
			name:      "package temp",
			filename:  ".package.json_123.tmp",
			wantIssue: "staged xAI HyperFrames project artifact is not allowed",
		},
		{
			name:      "package backup",
			filename:  ".package.json_123.bak",
			wantIssue: "staged xAI HyperFrames project artifact is not allowed",
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			outputDir := t.TempDir()
			writeProductionValidationArtifacts(t, outputDir, true)
			if err := os.WriteFile(filepath.Join(outputDir, "hyperframes", tt.filename), []byte("artifact"), 0644); err != nil {
				t.Fatalf("write staged HyperFrames project artifact: %v", err)
			}
			validator := &validateStubOutputValidator{
				metadata: xaipipeline.RenderMetadata{
					Path:        filepath.Join(outputDir, "output_xai.mp4"),
					Width:       720,
					Height:      1280,
					FPS:         24,
					DurationSec: 8,
					CodecName:   "h264",
					PixelFormat: "yuv420p",
					SizeBytes:   validationMP4SizeBytes(),
					HasAudio:    false,
				},
			}

			summary, err := xaipipeline.ValidateOutputDir(context.Background(), outputDir, validator)
			if err != nil {
				t.Fatalf("ValidateOutputDir() error = %v", err)
			}

			if summary.Status != xaipipeline.ValidationStatusInvalid {
				t.Fatalf("Status = %q, want %q", summary.Status, xaipipeline.ValidationStatusInvalid)
			}
			if !validationIssuesContain(summary.Issues, tt.wantIssue) {
				t.Fatalf("Issues = %#v, want %q issue", summary.Issues, tt.wantIssue)
			}
		})
	}
}

func TestValidateOutputDir_RejectsStagedMetadataArtifacts(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		filename  string
		wantIssue string
	}{
		{
			name:      "manifest temp",
			filename:  ".xai_manifest.json_123.tmp",
			wantIssue: "staged xAI metadata artifact is not allowed",
		},
		{
			name:      "manifest backup",
			filename:  ".xai_manifest.json_123.bak",
			wantIssue: "staged xAI metadata artifact is not allowed",
		},
		{
			name:      "run metadata temp",
			filename:  ".xai_run_metadata.json_123.tmp",
			wantIssue: "staged xAI metadata artifact is not allowed",
		},
		{
			name:      "run metadata backup",
			filename:  ".xai_run_metadata.json_123.bak",
			wantIssue: "staged xAI metadata artifact is not allowed",
		},
		{
			name:      "render metadata temp",
			filename:  ".render_metadata.json_123.tmp",
			wantIssue: "staged xAI metadata artifact is not allowed",
		},
		{
			name:      "render metadata backup",
			filename:  ".render_metadata.json_123.bak",
			wantIssue: "staged xAI metadata artifact is not allowed",
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			outputDir := t.TempDir()
			writeProductionValidationArtifacts(t, outputDir, true)
			if err := os.WriteFile(filepath.Join(outputDir, tt.filename), []byte("{}"), 0644); err != nil {
				t.Fatalf("write staged metadata artifact: %v", err)
			}
			validator := &validateStubOutputValidator{
				metadata: xaipipeline.RenderMetadata{
					Path:        filepath.Join(outputDir, "output_xai.mp4"),
					Width:       720,
					Height:      1280,
					FPS:         24,
					DurationSec: 8,
					CodecName:   "h264",
					PixelFormat: "yuv420p",
					SizeBytes:   validationMP4SizeBytes(),
					HasAudio:    false,
				},
			}

			summary, err := xaipipeline.ValidateOutputDir(context.Background(), outputDir, validator)
			if err != nil {
				t.Fatalf("ValidateOutputDir() error = %v", err)
			}

			if summary.Status != xaipipeline.ValidationStatusInvalid {
				t.Fatalf("Status = %q, want %q", summary.Status, xaipipeline.ValidationStatusInvalid)
			}
			if !validationIssuesContain(summary.Issues, tt.wantIssue) {
				t.Fatalf("Issues = %#v, want %q issue", summary.Issues, tt.wantIssue)
			}
		})
	}
}

func TestValidateOutputDir_RejectsSymlinkedArtifacts(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		relPath string
	}{
		{name: "manifest", relPath: "xai_manifest.json"},
		{name: "run metadata", relPath: "xai_run_metadata.json"},
		{name: "render metadata", relPath: "render_metadata.json"},
		{name: "raw shot", relPath: filepath.Join("shots", "shot_001.mp4")},
		{name: "normalized shot", relPath: filepath.Join("normalized", "shot_001.mp4")},
		{name: "hyperframes index", relPath: filepath.Join("hyperframes", "index.html")},
		{name: "hyperframes package", relPath: filepath.Join("hyperframes", "package.json")},
		{name: "timeline", relPath: "timeline_hyperframes.mp4"},
		{name: "final output", relPath: "output_xai.mp4"},
		{name: "preview", relPath: "preview_frame.jpg"},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			outputDir := t.TempDir()
			writeProductionValidationArtifacts(t, outputDir, true)

			artifactPath := filepath.Join(outputDir, tt.relPath)
			externalArtifact := filepath.Join(t.TempDir(), filepath.Base(tt.relPath))
			data, err := os.ReadFile(artifactPath)
			if err != nil {
				t.Fatalf("read artifact fixture: %v", err)
			}
			if err := os.WriteFile(externalArtifact, data, 0644); err != nil {
				t.Fatalf("write external artifact: %v", err)
			}
			if err := os.Remove(artifactPath); err != nil {
				t.Fatalf("remove artifact fixture: %v", err)
			}
			if err := os.Symlink(externalArtifact, artifactPath); err != nil {
				t.Skipf("symlink not available: %v", err)
			}

			validator := &validateStubOutputValidator{
				metadata: xaipipeline.RenderMetadata{
					Path:        filepath.Join(outputDir, "output_xai.mp4"),
					Width:       720,
					Height:      1280,
					FPS:         24,
					DurationSec: 8,
					CodecName:   "h264",
					PixelFormat: "yuv420p",
					SizeBytes:   validationMP4SizeBytes(),
					HasAudio:    false,
				},
			}

			summary, err := xaipipeline.ValidateOutputDir(context.Background(), outputDir, validator)
			if err != nil {
				t.Fatalf("ValidateOutputDir() error = %v", err)
			}

			if summary.Status != xaipipeline.ValidationStatusInvalid {
				t.Fatalf("Status = %q, want %q", summary.Status, xaipipeline.ValidationStatusInvalid)
			}
			if !validationIssuesContain(summary.Issues, "symlink") {
				t.Fatalf("Issues = %#v, want symlink issue", summary.Issues)
			}
			if !validationIssuesContain(summary.Issues, filepath.ToSlash(tt.relPath)) {
				t.Fatalf("Issues = %#v, want artifact path %q", summary.Issues, tt.relPath)
			}
		})
	}
}

func TestValidateOutputDir_RejectsStagedPreviewArtifacts(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		filename  string
		wantIssue string
	}{
		{
			name:      "preview temp",
			filename:  ".preview_frame.jpg_123.tmp.jpg",
			wantIssue: "staged xAI preview artifact is not allowed",
		},
		{
			name:      "legacy preview temp",
			filename:  ".preview_frame.jpg_123.tmp",
			wantIssue: "staged xAI preview artifact is not allowed",
		},
		{
			name:      "preview backup",
			filename:  ".preview_frame.jpg_123.bak",
			wantIssue: "staged xAI preview artifact is not allowed",
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			outputDir := t.TempDir()
			writeProductionValidationArtifacts(t, outputDir, true)
			if err := os.WriteFile(filepath.Join(outputDir, tt.filename), []byte("preview"), 0644); err != nil {
				t.Fatalf("write staged preview artifact: %v", err)
			}
			validator := &validateStubOutputValidator{
				metadata: xaipipeline.RenderMetadata{
					Path:        filepath.Join(outputDir, "output_xai.mp4"),
					Width:       720,
					Height:      1280,
					FPS:         24,
					DurationSec: 8,
					CodecName:   "h264",
					PixelFormat: "yuv420p",
					SizeBytes:   validationMP4SizeBytes(),
					HasAudio:    false,
				},
			}

			summary, err := xaipipeline.ValidateOutputDir(context.Background(), outputDir, validator)
			if err != nil {
				t.Fatalf("ValidateOutputDir() error = %v", err)
			}

			if summary.Status != xaipipeline.ValidationStatusInvalid {
				t.Fatalf("Status = %q, want %q", summary.Status, xaipipeline.ValidationStatusInvalid)
			}
			if !validationIssuesContain(summary.Issues, tt.wantIssue) {
				t.Fatalf("Issues = %#v, want %q issue", summary.Issues, tt.wantIssue)
			}
		})
	}
}

func TestValidateOutputDir_RejectsStagedFinalOutputArtifacts(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		filename  string
		wantIssue string
	}{
		{
			name:      "final output temp",
			filename:  ".output_xai.mp4_123.tmp.mp4",
			wantIssue: "staged xAI final output artifact is not allowed",
		},
		{
			name:      "legacy final output temp",
			filename:  ".output_xai.mp4_123.tmp",
			wantIssue: "staged xAI final output artifact is not allowed",
		},
		{
			name:      "final output backup",
			filename:  ".output_xai.mp4_123.bak",
			wantIssue: "staged xAI final output artifact is not allowed",
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			outputDir := t.TempDir()
			writeProductionValidationArtifacts(t, outputDir, true)
			if err := os.WriteFile(filepath.Join(outputDir, tt.filename), validationMP4Bytes(), 0644); err != nil {
				t.Fatalf("write staged final output artifact: %v", err)
			}
			validator := &validateStubOutputValidator{
				metadata: xaipipeline.RenderMetadata{
					Path:        filepath.Join(outputDir, "output_xai.mp4"),
					Width:       720,
					Height:      1280,
					FPS:         24,
					DurationSec: 8,
					CodecName:   "h264",
					PixelFormat: "yuv420p",
					SizeBytes:   validationMP4SizeBytes(),
					HasAudio:    false,
				},
			}

			summary, err := xaipipeline.ValidateOutputDir(context.Background(), outputDir, validator)
			if err != nil {
				t.Fatalf("ValidateOutputDir() error = %v", err)
			}

			if summary.Status != xaipipeline.ValidationStatusInvalid {
				t.Fatalf("Status = %q, want %q", summary.Status, xaipipeline.ValidationStatusInvalid)
			}
			if !validationIssuesContain(summary.Issues, tt.wantIssue) {
				t.Fatalf("Issues = %#v, want %q issue", summary.Issues, tt.wantIssue)
			}
		})
	}
}

func TestValidateOutputDir_RejectsStagedTimelineArtifacts(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		filename  string
		wantIssue string
	}{
		{
			name:      "timeline temp",
			filename:  ".timeline_hyperframes.mp4_123.tmp.mp4",
			wantIssue: "staged xAI timeline artifact is not allowed",
		},
		{
			name:      "legacy timeline temp",
			filename:  ".timeline_hyperframes.mp4_123.tmp",
			wantIssue: "staged xAI timeline artifact is not allowed",
		},
		{
			name:      "timeline backup",
			filename:  ".timeline_hyperframes.mp4_123.bak",
			wantIssue: "staged xAI timeline artifact is not allowed",
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			outputDir := t.TempDir()
			writeProductionValidationArtifacts(t, outputDir, true)
			if err := os.WriteFile(filepath.Join(outputDir, tt.filename), validationMP4Bytes(), 0644); err != nil {
				t.Fatalf("write staged timeline artifact: %v", err)
			}
			validator := &validateStubOutputValidator{
				metadata: xaipipeline.RenderMetadata{
					Path:        filepath.Join(outputDir, "output_xai.mp4"),
					Width:       720,
					Height:      1280,
					FPS:         24,
					DurationSec: 8,
					CodecName:   "h264",
					PixelFormat: "yuv420p",
					SizeBytes:   validationMP4SizeBytes(),
					HasAudio:    false,
				},
			}

			summary, err := xaipipeline.ValidateOutputDir(context.Background(), outputDir, validator)
			if err != nil {
				t.Fatalf("ValidateOutputDir() error = %v", err)
			}

			if summary.Status != xaipipeline.ValidationStatusInvalid {
				t.Fatalf("Status = %q, want %q", summary.Status, xaipipeline.ValidationStatusInvalid)
			}
			if !validationIssuesContain(summary.Issues, tt.wantIssue) {
				t.Fatalf("Issues = %#v, want %q issue", summary.Issues, tt.wantIssue)
			}
		})
	}
}

func TestValidateOutputDir_RejectsNormalizedShotFFprobeFailure(t *testing.T) {
	t.Parallel()

	outputDir := t.TempDir()
	writeProductionValidationArtifacts(t, outputDir, true)
	validator := &validateStubOutputValidator{
		metadata: xaipipeline.RenderMetadata{
			Path:        filepath.Join(outputDir, "output_xai.mp4"),
			Width:       720,
			Height:      1280,
			FPS:         24,
			DurationSec: 8,
			CodecName:   "h264",
			SizeBytes:   validationMP4SizeBytes(),
			HasAudio:    false,
		},
		errByPath: map[string]error{
			filepath.Join(outputDir, "normalized", "shot_001.mp4"): errors.New("normalized shot has audio"),
		},
	}

	summary, err := xaipipeline.ValidateOutputDir(context.Background(), outputDir, validator)
	if err != nil {
		t.Fatalf("ValidateOutputDir() error = %v", err)
	}

	if summary.Status != xaipipeline.ValidationStatusInvalid {
		t.Fatalf("Status = %q, want %q", summary.Status, xaipipeline.ValidationStatusInvalid)
	}
	if !validationIssuesContain(summary.Issues, "normalized shot ffprobe validation for shot 1") {
		t.Fatalf("Issues = %#v, want normalized shot ffprobe issue", summary.Issues)
	}
	if validator.callCount != 4 {
		t.Fatalf("validator calls = %d, want raw shot, normalized shot, timeline, and final output", validator.callCount)
	}
	normalizedPath := filepath.Join(outputDir, "normalized", "shot_001.mp4")
	normalizedSpec, ok := validator.specByPath[normalizedPath]
	if !ok {
		t.Fatalf("normalized shot was not validated; paths=%#v", validator.paths)
	}
	if normalizedSpec.Width != 720 || normalizedSpec.Height != 1280 || normalizedSpec.FPS != 24 {
		t.Fatalf("normalized spec = %#v, want 720x1280@24", normalizedSpec)
	}
	if normalizedSpec.CodecName != "h264" || normalizedSpec.PixelFormat != "yuv420p" || normalizedSpec.ExpectedDurationSec != 8 || !normalizedSpec.RequireNoAudio {
		t.Fatalf("normalized spec = %#v, want h264/yuv420p 8s silent", normalizedSpec)
	}
}

func TestValidateOutputDir_RejectsRawShotFFprobeFailure(t *testing.T) {
	t.Parallel()

	outputDir := t.TempDir()
	writeProductionValidationArtifacts(t, outputDir, true)
	validator := &validateStubOutputValidator{
		metadata: xaipipeline.RenderMetadata{
			Path:        filepath.Join(outputDir, "output_xai.mp4"),
			Width:       720,
			Height:      1280,
			FPS:         24,
			DurationSec: 8,
			CodecName:   "h264",
			SizeBytes:   validationMP4SizeBytes(),
			HasAudio:    false,
		},
		errByPath: map[string]error{
			filepath.Join(outputDir, "shots", "shot_001.mp4"): errors.New("raw shot is not decodable"),
		},
	}

	summary, err := xaipipeline.ValidateOutputDir(context.Background(), outputDir, validator)
	if err != nil {
		t.Fatalf("ValidateOutputDir() error = %v", err)
	}

	if summary.Status != xaipipeline.ValidationStatusInvalid {
		t.Fatalf("Status = %q, want %q", summary.Status, xaipipeline.ValidationStatusInvalid)
	}
	if !validationIssuesContain(summary.Issues, "raw shot ffprobe validation for shot 1") {
		t.Fatalf("Issues = %#v, want raw shot ffprobe issue", summary.Issues)
	}
	if validator.callCount != 4 {
		t.Fatalf("validator calls = %d, want raw shot, normalized shot, timeline, and final output", validator.callCount)
	}
	rawPath := filepath.Join(outputDir, "shots", "shot_001.mp4")
	rawSpec, ok := validator.specByPath[rawPath]
	if !ok {
		t.Fatalf("raw shot was not validated; paths=%#v", validator.paths)
	}
	if rawSpec.ExpectedDurationSec != 8 {
		t.Fatalf("raw spec = %#v, want expected duration 8s", rawSpec)
	}
	if rawSpec.Width != 0 || rawSpec.Height != 0 || rawSpec.FPS != 0 || rawSpec.CodecName != "" || rawSpec.PixelFormat != "" || rawSpec.RequireNoAudio {
		t.Fatalf("raw spec = %#v, want decode plus duration only", rawSpec)
	}
}

func TestValidateOutputDir_RejectsHyperFramesTimelineFFprobeFailure(t *testing.T) {
	t.Parallel()

	outputDir := t.TempDir()
	writeProductionValidationArtifacts(t, outputDir, true)
	validator := &validateStubOutputValidator{
		metadata: xaipipeline.RenderMetadata{
			Path:        filepath.Join(outputDir, "output_xai.mp4"),
			Width:       720,
			Height:      1280,
			FPS:         24,
			DurationSec: 8,
			CodecName:   "h264",
			SizeBytes:   validationMP4SizeBytes(),
			HasAudio:    false,
		},
		errByPath: map[string]error{
			filepath.Join(outputDir, "timeline_hyperframes.mp4"): errors.New("timeline is not decodable"),
		},
	}

	summary, err := xaipipeline.ValidateOutputDir(context.Background(), outputDir, validator)
	if err != nil {
		t.Fatalf("ValidateOutputDir() error = %v", err)
	}

	if summary.Status != xaipipeline.ValidationStatusInvalid {
		t.Fatalf("Status = %q, want %q", summary.Status, xaipipeline.ValidationStatusInvalid)
	}
	if !validationIssuesContain(summary.Issues, "hyperframes timeline ffprobe validation") {
		t.Fatalf("Issues = %#v, want HyperFrames timeline ffprobe issue", summary.Issues)
	}
	if validator.callCount != 4 {
		t.Fatalf("validator calls = %d, want raw shot plus normalized shot plus timeline plus final output", validator.callCount)
	}
	timelinePath := filepath.Join(outputDir, "timeline_hyperframes.mp4")
	timelineSpec, ok := validator.specByPath[timelinePath]
	if !ok {
		t.Fatalf("timeline was not validated; paths=%#v", validator.paths)
	}
	if timelineSpec.Width != 720 || timelineSpec.Height != 1280 || timelineSpec.FPS != 24 {
		t.Fatalf("timeline spec = %#v, want 720x1280@24", timelineSpec)
	}
	if timelineSpec.CodecName != "" || timelineSpec.PixelFormat != "" || timelineSpec.ExpectedDurationSec != 8 || !timelineSpec.RequireNoAudio {
		t.Fatalf("timeline spec = %#v, want 8s silent timeline without codec/pixel-format constraint", timelineSpec)
	}
}

func TestValidateOutputDir_RejectsNonJPEGPreviewFrame(t *testing.T) {
	t.Parallel()

	outputDir := t.TempDir()
	writeProductionValidationArtifacts(t, outputDir, true)
	writeInspectFile(t, filepath.Join(outputDir, "preview_frame.jpg"))
	validator := &validateStubOutputValidator{
		metadata: xaipipeline.RenderMetadata{
			Path:        filepath.Join(outputDir, "output_xai.mp4"),
			Width:       720,
			Height:      1280,
			FPS:         24,
			DurationSec: 8,
			CodecName:   "h264",
			SizeBytes:   validationMP4SizeBytes(),
			HasAudio:    false,
		},
	}

	summary, err := xaipipeline.ValidateOutputDir(context.Background(), outputDir, validator)
	if err != nil {
		t.Fatalf("ValidateOutputDir() error = %v", err)
	}

	if summary.Status != xaipipeline.ValidationStatusInvalid {
		t.Fatalf("Status = %q, want %q", summary.Status, xaipipeline.ValidationStatusInvalid)
	}
	if !validationIssuesContain(summary.Issues, "preview frame") {
		t.Fatalf("Issues = %#v, want preview frame issue", summary.Issues)
	}
}

func TestValidateOutputDir_RejectsWrongSizePreviewFrame(t *testing.T) {
	t.Parallel()

	outputDir := t.TempDir()
	writeProductionValidationArtifacts(t, outputDir, true)
	writeValidationJPEGWithSize(t, filepath.Join(outputDir, "preview_frame.jpg"), 640, 640)
	validator := &validateStubOutputValidator{
		metadata: xaipipeline.RenderMetadata{
			Path:        filepath.Join(outputDir, "output_xai.mp4"),
			Width:       720,
			Height:      1280,
			FPS:         24,
			DurationSec: 8,
			CodecName:   "h264",
			SizeBytes:   validationMP4SizeBytes(),
			HasAudio:    false,
		},
	}

	summary, err := xaipipeline.ValidateOutputDir(context.Background(), outputDir, validator)
	if err != nil {
		t.Fatalf("ValidateOutputDir() error = %v", err)
	}

	if summary.Status != xaipipeline.ValidationStatusInvalid {
		t.Fatalf("Status = %q, want %q", summary.Status, xaipipeline.ValidationStatusInvalid)
	}
	if !validationIssuesContain(summary.Issues, "preview frame dimensions") {
		t.Fatalf("Issues = %#v, want preview frame dimensions issue", summary.Issues)
	}
}

func TestValidateOutputDir_RejectsDryRunProviderMetadata(t *testing.T) {
	t.Parallel()

	outputDir := t.TempDir()
	writeValidationArtifactsWithProviderMetadata(t, outputDir, "dry-run-shot-001", "dry_run")
	validator := &validateStubOutputValidator{
		metadata: xaipipeline.RenderMetadata{
			Path:        filepath.Join(outputDir, "output_xai.mp4"),
			Width:       720,
			Height:      1280,
			FPS:         24,
			DurationSec: 8,
			CodecName:   "h264",
			SizeBytes:   validationMP4SizeBytes(),
			HasAudio:    false,
		},
	}

	summary, err := xaipipeline.ValidateOutputDir(context.Background(), outputDir, validator)
	if err != nil {
		t.Fatalf("ValidateOutputDir() error = %v", err)
	}

	if summary.Status != xaipipeline.ValidationStatusInvalid {
		t.Fatalf("Status = %q, want %q", summary.Status, xaipipeline.ValidationStatusInvalid)
	}
	if len(summary.Issues) == 0 {
		t.Fatal("Issues is empty, want dry-run metadata issue")
	}
	if !validator.called {
		t.Fatal("ffprobe validator should still run for complete inspect output")
	}
}

func TestValidateOutputDir_RejectsFFprobeFailure(t *testing.T) {
	t.Parallel()

	outputDir := t.TempDir()
	writeProductionValidationArtifacts(t, outputDir, true)
	validator := &validateStubOutputValidator{err: errors.New("ffprobe failed")}

	summary, err := xaipipeline.ValidateOutputDir(context.Background(), outputDir, validator)
	if err != nil {
		t.Fatalf("ValidateOutputDir() error = %v", err)
	}

	if summary.Status != xaipipeline.ValidationStatusInvalid {
		t.Fatalf("Status = %q, want %q", summary.Status, xaipipeline.ValidationStatusInvalid)
	}
	if len(summary.Issues) == 0 {
		t.Fatal("Issues is empty, want ffprobe issue")
	}
}

func TestValidateBatchOutputDir_AcceptsCompleteProductionEpisodes(t *testing.T) {
	t.Parallel()

	outputDir := t.TempDir()
	writeProductionValidationEpisode(t, filepath.Join(outputDir, "episode_001"), true)
	writeProductionValidationEpisode(t, filepath.Join(outputDir, "episode_002"), true)
	validator := &validateStubOutputValidator{
		metadata: xaipipeline.RenderMetadata{
			Width:       720,
			Height:      1280,
			FPS:         24,
			DurationSec: 8,
			CodecName:   "h264",
			PixelFormat: "yuv420p",
			SizeBytes:   validationMP4SizeBytes(),
			HasAudio:    false,
		},
	}

	summary, err := xaipipeline.ValidateBatchOutputDir(context.Background(), outputDir, validator)
	if err != nil {
		t.Fatalf("ValidateBatchOutputDir() error = %v", err)
	}

	if summary.Status != xaipipeline.ValidationStatusValid {
		t.Fatalf("Status = %q, want %q; issues=%#v", summary.Status, xaipipeline.ValidationStatusValid, summary.Issues)
	}
	if summary.TotalEpisodes != 2 || summary.ValidEpisodes != 2 || summary.InvalidEpisodes != 0 {
		t.Fatalf("unexpected batch counts: %#v", summary)
	}
	if summary.StoryHash != validValidationStoryHash {
		t.Fatalf("StoryHash = %q, want %q", summary.StoryHash, validValidationStoryHash)
	}
	if summary.VideoModel != "grok-imagine-video" {
		t.Fatalf("VideoModel = %q, want grok-imagine-video", summary.VideoModel)
	}
	if len(summary.Episodes) != 2 {
		t.Fatalf("Episodes = %d, want 2", len(summary.Episodes))
	}
	if summary.Episodes[0].Episode != 1 || summary.Episodes[0].Validation.Status != xaipipeline.ValidationStatusValid {
		t.Fatalf("episode 1 summary = %#v", summary.Episodes[0])
	}
	if summary.Episodes[1].Episode != 2 || summary.Episodes[1].Validation.Status != xaipipeline.ValidationStatusValid {
		t.Fatalf("episode 2 summary = %#v", summary.Episodes[1])
	}
	if validator.callCount != 8 {
		t.Fatalf("validator calls = %d, want raw shot, normalized shot, timeline, and final output per episode", validator.callCount)
	}
}

func TestValidateBatchOutputDir_NilContextUsesBackground(t *testing.T) {
	t.Parallel()

	outputDir := t.TempDir()
	writeProductionValidationEpisode(t, filepath.Join(outputDir, "episode_001"), true)
	validator := &validateStubOutputValidator{
		metadata: xaipipeline.RenderMetadata{
			Width:       720,
			Height:      1280,
			FPS:         24,
			DurationSec: 8,
			CodecName:   "h264",
			PixelFormat: "yuv420p",
			SizeBytes:   validationMP4SizeBytes(),
			HasAudio:    false,
		},
	}

	summary, err := xaipipeline.ValidateBatchOutputDir(nil, outputDir, validator)
	if err != nil {
		t.Fatalf("ValidateBatchOutputDir() error = %v", err)
	}
	if summary.Status != xaipipeline.ValidationStatusValid || summary.ValidEpisodes != 1 {
		t.Fatalf("summary = %#v, want one valid episode", summary)
	}
	if validator.callCount == 0 {
		t.Fatal("validator was not called")
	}
}

func TestValidateBatchOutputDir_RejectsCanceledContextBeforeInspect(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	validator := &validateStubOutputValidator{}
	summary, err := xaipipeline.ValidateBatchOutputDir(ctx, filepath.Join(t.TempDir(), "missing-batch"), validator)

	if !errors.Is(err, context.Canceled) {
		t.Fatalf("ValidateBatchOutputDir() error = %v, want context.Canceled", err)
	}
	if summary.OutputDir != "" || summary.Status != "" {
		t.Fatalf("ValidateBatchOutputDir() summary = %#v, want zero summary", summary)
	}
	if validator.called {
		t.Fatal("validator should not be called after canceled context")
	}
}

func TestValidateBatchOutputDir_RejectsMixedEpisodeStoryHashes(t *testing.T) {
	t.Parallel()

	outputDir := t.TempDir()
	writeProductionValidationEpisodeWithStoryHash(t, filepath.Join(outputDir, "episode_001"), validValidationStoryHash)
	writeProductionValidationEpisodeWithStoryHash(t, filepath.Join(outputDir, "episode_002"), "abcdef0123456789abcdef0123456789abcdef0123456789abcdef0123456789")
	validator := &validateStubOutputValidator{
		metadata: xaipipeline.RenderMetadata{
			Width:       720,
			Height:      1280,
			FPS:         24,
			DurationSec: 8,
			CodecName:   "h264",
			PixelFormat: "yuv420p",
			SizeBytes:   validationMP4SizeBytes(),
			HasAudio:    false,
		},
	}

	summary, err := xaipipeline.ValidateBatchOutputDir(context.Background(), outputDir, validator)
	if err != nil {
		t.Fatalf("ValidateBatchOutputDir() error = %v", err)
	}

	if summary.Status != xaipipeline.ValidationStatusInvalid {
		t.Fatalf("Status = %q, want %q", summary.Status, xaipipeline.ValidationStatusInvalid)
	}
	if summary.ValidEpisodes != 2 || summary.InvalidEpisodes != 0 {
		t.Fatalf("unexpected batch counts: %#v", summary)
	}
	if !validationIssuesContain(summary.Issues, `episode_002 story_hash="abcdef0123456789abcdef0123456789abcdef0123456789abcdef0123456789", want batch story_hash "`+validValidationStoryHash+`"`) {
		t.Fatalf("Issues = %#v, want mixed story_hash issue", summary.Issues)
	}
}

func TestValidateBatchOutputDir_RejectsMixedEpisodeVideoModels(t *testing.T) {
	t.Parallel()

	outputDir := t.TempDir()
	writeProductionValidationEpisodeWithVideoModel(t, filepath.Join(outputDir, "episode_001"), "grok-imagine-video")
	writeProductionValidationEpisodeWithVideoModel(t, filepath.Join(outputDir, "episode_002"), "grok-imagine-video-v2")
	validator := &validateStubOutputValidator{
		metadata: xaipipeline.RenderMetadata{
			Width:       720,
			Height:      1280,
			FPS:         24,
			DurationSec: 8,
			CodecName:   "h264",
			PixelFormat: "yuv420p",
			SizeBytes:   validationMP4SizeBytes(),
			HasAudio:    false,
		},
	}

	summary, err := xaipipeline.ValidateBatchOutputDir(context.Background(), outputDir, validator)
	if err != nil {
		t.Fatalf("ValidateBatchOutputDir() error = %v", err)
	}

	if summary.Status != xaipipeline.ValidationStatusInvalid {
		t.Fatalf("Status = %q, want %q", summary.Status, xaipipeline.ValidationStatusInvalid)
	}
	if summary.ValidEpisodes != 2 || summary.InvalidEpisodes != 0 {
		t.Fatalf("unexpected batch counts: %#v", summary)
	}
	if !validationIssuesContain(summary.Issues, `episode_002 video_model="grok-imagine-video-v2", want batch video_model "grok-imagine-video"`) {
		t.Fatalf("Issues = %#v, want mixed video_model issue", summary.Issues)
	}
}

func TestValidateBatchOutputDir_RejectsInvalidEpisode(t *testing.T) {
	t.Parallel()

	outputDir := t.TempDir()
	writeProductionValidationEpisode(t, filepath.Join(outputDir, "episode_001"), true)
	writeProductionValidationEpisode(t, filepath.Join(outputDir, "episode_002"), false)
	validator := &validateStubOutputValidator{
		metadata: xaipipeline.RenderMetadata{
			Width:       720,
			Height:      1280,
			FPS:         24,
			DurationSec: 8,
			CodecName:   "h264",
			PixelFormat: "yuv420p",
			SizeBytes:   validationMP4SizeBytes(),
			HasAudio:    false,
		},
	}

	summary, err := xaipipeline.ValidateBatchOutputDir(context.Background(), outputDir, validator)
	if err != nil {
		t.Fatalf("ValidateBatchOutputDir() error = %v", err)
	}

	if summary.Status != xaipipeline.ValidationStatusInvalid {
		t.Fatalf("Status = %q, want %q", summary.Status, xaipipeline.ValidationStatusInvalid)
	}
	if summary.ValidEpisodes != 1 || summary.InvalidEpisodes != 1 {
		t.Fatalf("unexpected batch counts: %#v", summary)
	}
	if len(summary.Issues) == 0 {
		t.Fatal("Issues is empty, want invalid episode issue")
	}
}

func TestValidateBatchOutputDir_RejectsLegacyRemotionPropsAtBatchRoot(t *testing.T) {
	t.Parallel()

	outputDir := t.TempDir()
	writeProductionValidationEpisode(t, filepath.Join(outputDir, "episode_001"), true)
	writeProductionValidationEpisode(t, filepath.Join(outputDir, "episode_002"), true)
	writeInspectFile(t, filepath.Join(outputDir, "remotion_props.json"))
	validator := &validateStubOutputValidator{
		metadata: xaipipeline.RenderMetadata{
			Width:       720,
			Height:      1280,
			FPS:         24,
			DurationSec: 8,
			CodecName:   "h264",
			PixelFormat: "yuv420p",
			SizeBytes:   validationMP4SizeBytes(),
			HasAudio:    false,
		},
	}

	summary, err := xaipipeline.ValidateBatchOutputDir(context.Background(), outputDir, validator)
	if err != nil {
		t.Fatalf("ValidateBatchOutputDir() error = %v", err)
	}

	if summary.Status != xaipipeline.ValidationStatusInvalid {
		t.Fatalf("Status = %q, want %q", summary.Status, xaipipeline.ValidationStatusInvalid)
	}
	if summary.ValidEpisodes != 2 || summary.InvalidEpisodes != 0 {
		t.Fatalf("unexpected batch counts: %#v", summary)
	}
	if !validationIssuesContain(summary.Issues, "legacy remotion_props.json artifact is not allowed in xAI-native batch output") {
		t.Fatalf("Issues = %#v, want batch root legacy remotion_props issue", summary.Issues)
	}
}

func TestValidateBatchOutputDir_RejectsRootManifestAtBatchRoot(t *testing.T) {
	t.Parallel()

	outputDir := t.TempDir()
	writeProductionValidationEpisode(t, filepath.Join(outputDir, "episode_001"), true)
	writeProductionValidationEpisode(t, filepath.Join(outputDir, "episode_002"), true)
	writeInspectJSON(t, filepath.Join(outputDir, "xai_manifest.json"), xaipipeline.Manifest{
		ProjectID: "ambiguous-root",
		StoryHash: validValidationStoryHash,
		Format:    "portrait",
		FPS:       24,
		Width:     720,
		Height:    1280,
		Shots:     []xaipipeline.Shot{{Index: 1, Prompt: "shot", DurationSec: 8}},
	})
	validator := &validateStubOutputValidator{
		metadata: xaipipeline.RenderMetadata{
			Width:       720,
			Height:      1280,
			FPS:         24,
			DurationSec: 8,
			CodecName:   "h264",
			PixelFormat: "yuv420p",
			SizeBytes:   validationMP4SizeBytes(),
			HasAudio:    false,
		},
	}

	summary, err := xaipipeline.ValidateBatchOutputDir(context.Background(), outputDir, validator)
	if err != nil {
		t.Fatalf("ValidateBatchOutputDir() error = %v", err)
	}

	if summary.Status != xaipipeline.ValidationStatusInvalid {
		t.Fatalf("Status = %q, want %q", summary.Status, xaipipeline.ValidationStatusInvalid)
	}
	if summary.ValidEpisodes != 2 || summary.InvalidEpisodes != 0 {
		t.Fatalf("unexpected batch counts: %#v", summary)
	}
	if !validationIssuesContain(summary.Issues, "root xai_manifest.json is not allowed in xAI-native batch output") {
		t.Fatalf("Issues = %#v, want batch root manifest issue", summary.Issues)
	}
}

func TestValidateBatchOutputDir_RejectsRootSingleOutputArtifacts(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		writeRoot func(t *testing.T, outputDir string)
		wantIssue string
	}{
		{
			name: "output video",
			writeRoot: func(t *testing.T, outputDir string) {
				t.Helper()
				writeValidationMP4(t, filepath.Join(outputDir, "output_xai.mp4"))
			},
			wantIssue: "root output_xai.mp4 is not allowed in xAI-native batch output",
		},
		{
			name: "normalized directory",
			writeRoot: func(t *testing.T, outputDir string) {
				t.Helper()
				writeValidationMP4(t, filepath.Join(outputDir, "normalized", "shot_001.mp4"))
			},
			wantIssue: "root normalized is not allowed in xAI-native batch output",
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			outputDir := t.TempDir()
			writeProductionValidationEpisode(t, filepath.Join(outputDir, "episode_001"), true)
			writeProductionValidationEpisode(t, filepath.Join(outputDir, "episode_002"), true)
			tt.writeRoot(t, outputDir)
			validator := &validateStubOutputValidator{
				metadata: xaipipeline.RenderMetadata{
					Width:       720,
					Height:      1280,
					FPS:         24,
					DurationSec: 8,
					CodecName:   "h264",
					PixelFormat: "yuv420p",
					SizeBytes:   validationMP4SizeBytes(),
					HasAudio:    false,
				},
			}

			summary, err := xaipipeline.ValidateBatchOutputDir(context.Background(), outputDir, validator)
			if err != nil {
				t.Fatalf("ValidateBatchOutputDir() error = %v", err)
			}

			if summary.Status != xaipipeline.ValidationStatusInvalid {
				t.Fatalf("Status = %q, want %q", summary.Status, xaipipeline.ValidationStatusInvalid)
			}
			if summary.ValidEpisodes != 2 || summary.InvalidEpisodes != 0 {
				t.Fatalf("unexpected batch counts: %#v", summary)
			}
			if !validationIssuesContain(summary.Issues, tt.wantIssue) {
				t.Fatalf("Issues = %#v, want %q", summary.Issues, tt.wantIssue)
			}
		})
	}
}

func TestValidateBatchOutputDir_RejectsMalformedEpisodeDirs(t *testing.T) {
	t.Parallel()

	outputDir := t.TempDir()
	writeProductionValidationEpisode(t, filepath.Join(outputDir, "episode_001"), true)
	writeProductionValidationEpisode(t, filepath.Join(outputDir, "episode_1"), true)
	if err := os.MkdirAll(filepath.Join(outputDir, "episode_bad"), 0755); err != nil {
		t.Fatalf("mkdir malformed episode: %v", err)
	}
	validator := &validateStubOutputValidator{
		metadata: xaipipeline.RenderMetadata{
			Width:       720,
			Height:      1280,
			FPS:         24,
			DurationSec: 8,
			CodecName:   "h264",
			PixelFormat: "yuv420p",
			SizeBytes:   validationMP4SizeBytes(),
			HasAudio:    false,
		},
	}

	summary, err := xaipipeline.ValidateBatchOutputDir(context.Background(), outputDir, validator)
	if err != nil {
		t.Fatalf("ValidateBatchOutputDir() error = %v", err)
	}

	if summary.Status != xaipipeline.ValidationStatusInvalid {
		t.Fatalf("Status = %q, want %q", summary.Status, xaipipeline.ValidationStatusInvalid)
	}
	if summary.ValidEpisodes != 1 || summary.InvalidEpisodes != 0 {
		t.Fatalf("unexpected batch counts: %#v", summary)
	}
	for _, want := range []string{
		`malformed episode directory "episode_1": want episode_###`,
		`malformed episode directory "episode_bad": want episode_###`,
	} {
		if !validationIssuesContain(summary.Issues, want) {
			t.Fatalf("Issues = %#v, want %q", summary.Issues, want)
		}
	}
}

func TestValidateBatchOutputDir_RejectsSymlinkedEpisodeDir(t *testing.T) {
	t.Parallel()

	outputDir := t.TempDir()
	externalEpisode := filepath.Join(t.TempDir(), "external-episode")
	writeProductionValidationEpisode(t, externalEpisode, true)
	if err := os.Symlink(externalEpisode, filepath.Join(outputDir, "episode_001")); err != nil {
		t.Skipf("symlink not available: %v", err)
	}
	validator := &validateStubOutputValidator{
		metadata: xaipipeline.RenderMetadata{
			Width:       720,
			Height:      1280,
			FPS:         24,
			DurationSec: 8,
			CodecName:   "h264",
			PixelFormat: "yuv420p",
			SizeBytes:   validationMP4SizeBytes(),
			HasAudio:    false,
		},
	}

	summary, err := xaipipeline.ValidateBatchOutputDir(context.Background(), outputDir, validator)
	if err != nil {
		t.Fatalf("ValidateBatchOutputDir() error = %v", err)
	}

	if summary.Status != xaipipeline.ValidationStatusInvalid {
		t.Fatalf("Status = %q, want %q", summary.Status, xaipipeline.ValidationStatusInvalid)
	}
	if summary.TotalEpisodes != 0 {
		t.Fatalf("TotalEpisodes = %d, want 0", summary.TotalEpisodes)
	}
	if !validationIssuesContain(summary.Issues, `episode directory "episode_001" is a symlink`) {
		t.Fatalf("Issues = %#v, want symlink episode issue", summary.Issues)
	}
}

func TestValidateBatchOutputDir_RejectsMissingEpisodeNumber(t *testing.T) {
	t.Parallel()

	outputDir := t.TempDir()
	writeProductionValidationEpisode(t, filepath.Join(outputDir, "episode_001"), true)
	writeProductionValidationEpisode(t, filepath.Join(outputDir, "episode_003"), true)
	validator := &validateStubOutputValidator{
		metadata: xaipipeline.RenderMetadata{
			Width:       720,
			Height:      1280,
			FPS:         24,
			DurationSec: 8,
			CodecName:   "h264",
			PixelFormat: "yuv420p",
			SizeBytes:   validationMP4SizeBytes(),
			HasAudio:    false,
		},
	}

	summary, err := xaipipeline.ValidateBatchOutputDir(context.Background(), outputDir, validator)
	if err != nil {
		t.Fatalf("ValidateBatchOutputDir() error = %v", err)
	}

	if summary.Status != xaipipeline.ValidationStatusInvalid {
		t.Fatalf("Status = %q, want %q", summary.Status, xaipipeline.ValidationStatusInvalid)
	}
	if summary.ValidEpisodes != 2 || summary.InvalidEpisodes != 0 {
		t.Fatalf("unexpected batch counts: %#v", summary)
	}
	if !validationIssuesContain(summary.Issues, "missing episode_002 directory") {
		t.Fatalf("Issues = %#v, want missing episode issue", summary.Issues)
	}
}

func TestValidateBatchOutputDir_NoEpisodesReturnsInvalidSummary(t *testing.T) {
	t.Parallel()

	summary, err := xaipipeline.ValidateBatchOutputDir(context.Background(), t.TempDir(), &validateStubOutputValidator{})
	if err != nil {
		t.Fatalf("ValidateBatchOutputDir() error = %v", err)
	}

	if summary.Status != xaipipeline.ValidationStatusInvalid {
		t.Fatalf("Status = %q, want %q", summary.Status, xaipipeline.ValidationStatusInvalid)
	}
	if len(summary.Issues) != 1 {
		t.Fatalf("Issues = %#v, want one issue", summary.Issues)
	}
}

type validateStubOutputValidator struct {
	called         bool
	callCount      int
	path           string
	paths          []string
	spec           xaipipeline.RenderValidationSpec
	specByPath     map[string]xaipipeline.RenderValidationSpec
	metadata       xaipipeline.RenderMetadata
	metadataByPath map[string]xaipipeline.RenderMetadata
	err            error
	errByPath      map[string]error
	afterValidate  func(path string)
}

func (s *validateStubOutputValidator) Validate(_ context.Context, path string, spec xaipipeline.RenderValidationSpec) (xaipipeline.RenderMetadata, error) {
	s.called = true
	s.callCount++
	s.path = path
	s.paths = append(s.paths, path)
	s.spec = spec
	if s.specByPath == nil {
		s.specByPath = make(map[string]xaipipeline.RenderValidationSpec)
	}
	s.specByPath[path] = spec
	if err := s.errByPath[path]; err != nil {
		return xaipipeline.RenderMetadata{}, err
	}
	if metadata, ok := s.metadataByPath[path]; ok {
		if s.afterValidate != nil {
			s.afterValidate(path)
		}
		return metadata, nil
	}
	metadata := s.metadata
	metadata.Path = path
	if s.afterValidate != nil {
		s.afterValidate(path)
	}
	return metadata, s.err
}

func validationOutputValidator(t *testing.T, outputDir string) *validateStubOutputValidator {
	t.Helper()
	return &validateStubOutputValidator{
		metadata: xaipipeline.RenderMetadata{
			Path:        filepath.Join(outputDir, "output_xai.mp4"),
			Width:       720,
			Height:      1280,
			FPS:         24,
			DurationSec: 8,
			CodecName:   "h264",
			PixelFormat: "yuv420p",
			SizeBytes:   validationMP4SizeBytes(),
			HasAudio:    false,
		},
	}
}

func validationIssuesContain(issues []string, needle string) bool {
	for _, issue := range issues {
		if strings.Contains(issue, needle) {
			return true
		}
	}
	return false
}

func validationPathsContain(paths []string, want string) bool {
	for _, path := range paths {
		if path == want {
			return true
		}
	}
	return false
}

func writeProductionValidationArtifacts(t *testing.T, outputDir string, withProviderMetadata bool) xaipipeline.Manifest {
	t.Helper()

	requestID := "req_123"
	status := "done"
	if !withProviderMetadata {
		requestID = ""
		status = ""
	}
	return writeValidationArtifactsWithProviderMetadata(t, outputDir, requestID, status)
}

func writeProductionValidationArtifactsWithSubtitle(t *testing.T, outputDir string, subtitle string) {
	t.Helper()

	writeProductionValidationArtifacts(t, outputDir, true)
	promptHash := validationPromptHash("shot", 8, "9:16", "720p")
	manifest := xaipipeline.Manifest{
		ProjectID:  "production",
		StoryHash:  validValidationStoryHash,
		VideoModel: "grok-imagine-video",
		Format:     "portrait",
		FPS:        24,
		Width:      720,
		Height:     1280,
		Shots: []xaipipeline.Shot{
			{
				Index:        1,
				Prompt:       "shot",
				PromptHash:   promptHash,
				XAIRequestID: "req_123",
				XAIStatus:    "done",
				DurationSec:  8,
				VideoPath:    "shots/shot_001.mp4",
				Subtitle:     subtitle,
			},
		},
	}
	writeInspectJSON(t, filepath.Join(outputDir, "xai_manifest.json"), manifest)
	writeValidationRenderMetadata(t, outputDir, manifest, 8)
}

func writeProductionValidationArtifactsWithTransition(t *testing.T, outputDir string, transition string) {
	t.Helper()

	writeProductionValidationArtifacts(t, outputDir, true)
	promptHash := validationPromptHash("shot", 8, "9:16", "720p")
	manifest := xaipipeline.Manifest{
		ProjectID:  "production",
		StoryHash:  validValidationStoryHash,
		VideoModel: "grok-imagine-video",
		Format:     "portrait",
		FPS:        24,
		Width:      720,
		Height:     1280,
		Shots: []xaipipeline.Shot{
			{
				Index:         1,
				Prompt:        "shot",
				PromptHash:    promptHash,
				XAIRequestID:  "req_123",
				XAIStatus:     "done",
				DurationSec:   8,
				VideoPath:     "shots/shot_001.mp4",
				TransitionOut: transition,
			},
		},
	}
	writeInspectJSON(t, filepath.Join(outputDir, "xai_manifest.json"), manifest)
	writeValidationRenderMetadata(t, outputDir, manifest, 8)
}

func writeProductionValidationEpisode(t *testing.T, outputDir string, withProviderMetadata bool) {
	t.Helper()
	if err := os.MkdirAll(outputDir, 0755); err != nil {
		t.Fatalf("mkdir %s: %v", outputDir, err)
	}
	writeProductionValidationArtifacts(t, outputDir, withProviderMetadata)
}

func writeProductionValidationEpisodeWithVideoModel(t *testing.T, outputDir string, videoModel string) {
	t.Helper()
	if err := os.MkdirAll(outputDir, 0755); err != nil {
		t.Fatalf("mkdir %s: %v", outputDir, err)
	}
	writeValidationArtifactsWithIdentity(t, outputDir, validValidationStoryHash, videoModel, "req_123", "done")
}

func writeProductionValidationEpisodeWithStoryHash(t *testing.T, outputDir string, storyHash string) {
	t.Helper()
	if err := os.MkdirAll(outputDir, 0755); err != nil {
		t.Fatalf("mkdir %s: %v", outputDir, err)
	}
	writeValidationArtifactsWithIdentity(t, outputDir, storyHash, "grok-imagine-video", "req_123", "done")
}

func writeValidationArtifactsWithProviderMetadata(t *testing.T, outputDir string, requestID string, status string) xaipipeline.Manifest {
	t.Helper()

	return writeValidationArtifactsWithIdentity(t, outputDir, validValidationStoryHash, "grok-imagine-video", requestID, status)
}

func writeValidationArtifactsWithVideoModel(t *testing.T, outputDir string, videoModel string, requestID string, status string) xaipipeline.Manifest {
	t.Helper()

	return writeValidationArtifactsWithIdentity(t, outputDir, validValidationStoryHash, videoModel, requestID, status)
}

func writeValidationArtifactsWithIdentity(t *testing.T, outputDir string, storyHash string, videoModel string, requestID string, status string) xaipipeline.Manifest {
	t.Helper()

	promptHash := validationPromptHashWithVideoModel(videoModel, "shot", 8, "9:16", "720p")
	manifest := xaipipeline.Manifest{
		ProjectID:  "production",
		StoryHash:  storyHash,
		VideoModel: videoModel,
		Format:     "portrait",
		FPS:        24,
		Width:      720,
		Height:     1280,
		Shots: []xaipipeline.Shot{
			{
				Index:        1,
				Prompt:       "shot",
				PromptHash:   promptHash,
				XAIRequestID: requestID,
				XAIStatus:    status,
				DurationSec:  8,
				VideoPath:    "shots/shot_001.mp4",
			},
		},
	}
	writeInspectJSON(t, filepath.Join(outputDir, "xai_manifest.json"), manifest)
	writeInspectJSON(t, filepath.Join(outputDir, "xai_run_metadata.json"), xaipipeline.RunMetadata{
		Planned:        true,
		VideoModel:     videoModel,
		GeneratedShots: []int{1},
		ShotDecisions: []xaipipeline.ShotDecision{
			{
				Index:        1,
				Decision:     "generated",
				VideoPath:    "shots/shot_001.mp4",
				PromptHash:   promptHash,
				XAIRequestID: requestID,
				XAIStatus:    status,
			},
		},
	})
	writeValidationRenderMetadata(t, outputDir, manifest, 8)
	writeValidationMP4(t, filepath.Join(outputDir, "shots", "shot_001.mp4"))
	writeValidationMP4(t, filepath.Join(outputDir, "normalized", "shot_001.mp4"))
	writeValidationHyperFramesIndex(t, filepath.Join(outputDir, "hyperframes", "index.html"), 1)
	writeValidationHyperFramesPackage(t, filepath.Join(outputDir, "hyperframes", "package.json"))
	writeValidationMP4(t, filepath.Join(outputDir, "timeline_hyperframes.mp4"))
	writeValidationMP4(t, filepath.Join(outputDir, "output_xai.mp4"))
	writeValidationJPEG(t, filepath.Join(outputDir, "preview_frame.jpg"))
	return manifest
}

func writeTwoShotProductionValidationArtifacts(t *testing.T, outputDir string) {
	t.Helper()

	firstPromptHash := validationPromptHash("shot 1", 8, "9:16", "720p")
	secondPromptHash := validationPromptHash("shot 2", 8, "9:16", "720p")
	manifest := xaipipeline.Manifest{
		ProjectID:  "production",
		StoryHash:  validValidationStoryHash,
		VideoModel: "grok-imagine-video",
		Format:     "portrait",
		FPS:        24,
		Width:      720,
		Height:     1280,
		Shots: []xaipipeline.Shot{
			{
				Index:        1,
				Prompt:       "shot 1",
				PromptHash:   firstPromptHash,
				XAIRequestID: "req_001",
				XAIStatus:    "done",
				DurationSec:  8,
				VideoPath:    "shots/shot_001.mp4",
			},
			{
				Index:        2,
				Prompt:       "shot 2",
				PromptHash:   secondPromptHash,
				XAIRequestID: "req_002",
				XAIStatus:    "done",
				DurationSec:  8,
				VideoPath:    "shots/shot_002.mp4",
			},
		},
	}
	writeInspectJSON(t, filepath.Join(outputDir, "xai_manifest.json"), manifest)
	writeInspectJSON(t, filepath.Join(outputDir, "xai_run_metadata.json"), xaipipeline.RunMetadata{
		Planned:        true,
		VideoModel:     "grok-imagine-video",
		GeneratedShots: []int{1, 2},
		ShotDecisions: []xaipipeline.ShotDecision{
			{
				Index:        1,
				Decision:     "generated",
				VideoPath:    "shots/shot_001.mp4",
				PromptHash:   firstPromptHash,
				XAIRequestID: "req_001",
				XAIStatus:    "done",
			},
			{
				Index:        2,
				Decision:     "generated",
				VideoPath:    "shots/shot_002.mp4",
				PromptHash:   secondPromptHash,
				XAIRequestID: "req_002",
				XAIStatus:    "done",
			},
		},
	})
	writeValidationRenderMetadata(t, outputDir, manifest, 16)
	for _, index := range []int{1, 2} {
		writeValidationMP4(t, filepath.Join(outputDir, "shots", fmt.Sprintf("shot_%03d.mp4", index)))
		writeValidationMP4(t, filepath.Join(outputDir, "normalized", fmt.Sprintf("shot_%03d.mp4", index)))
	}
	writeValidationHyperFramesIndex(t, filepath.Join(outputDir, "hyperframes", "index.html"), 1, 2)
	writeValidationHyperFramesPackage(t, filepath.Join(outputDir, "hyperframes", "package.json"))
	writeValidationMP4(t, filepath.Join(outputDir, "timeline_hyperframes.mp4"))
	writeValidationMP4(t, filepath.Join(outputDir, "output_xai.mp4"))
	writeValidationJPEG(t, filepath.Join(outputDir, "preview_frame.jpg"))
}

func writeValidationRenderMetadata(t *testing.T, outputDir string, manifest xaipipeline.Manifest, durationSec float64) {
	t.Helper()
	writeInspectJSON(t, filepath.Join(outputDir, "render_metadata.json"), validValidationRenderMetadataWithDuration(outputDir, manifest, durationSec))
}

func validValidationRenderMetadata(outputDir string, manifest xaipipeline.Manifest) xaipipeline.RenderMetadata {
	return validValidationRenderMetadataWithDuration(outputDir, manifest, 8)
}

func validValidationRenderMetadataWithDuration(outputDir string, manifest xaipipeline.Manifest, durationSec float64) xaipipeline.RenderMetadata {
	return xaipipeline.RenderMetadata{
		Path:         filepath.Join(outputDir, "output_xai.mp4"),
		ProjectID:    manifest.ProjectID,
		ManifestHash: validationManifestHash(manifest),
		Width:        720,
		Height:       1280,
		FPS:          24,
		DurationSec:  durationSec,
		CodecName:    "h264",
		PixelFormat:  "yuv420p",
		SizeBytes:    validationMP4SizeBytes(),
		HasAudio:     false,
	}
}

func validationManifestHash(manifest xaipipeline.Manifest) string {
	data, err := json.Marshal(manifest)
	if err != nil {
		panic(err)
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func validationPromptHash(prompt string, duration float64, aspectRatio string, resolution string) string {
	return validationPromptHashWithVideoModel("grok-imagine-video", prompt, duration, aspectRatio, resolution)
}

func validationPromptHashWithVideoModel(videoModel string, prompt string, duration float64, aspectRatio string, resolution string) string {
	sum := sha256.Sum256([]byte(fmt.Sprintf(
		"%s\n%s\n%.3f\n%s\n%s",
		strings.TrimSpace(videoModel),
		strings.TrimSpace(prompt),
		duration,
		strings.TrimSpace(aspectRatio),
		strings.TrimSpace(resolution),
	)))
	return hex.EncodeToString(sum[:])
}

func writeValidationJPEG(t *testing.T, path string) {
	t.Helper()
	writeValidationJPEGWithSize(t, path, 720, 1280)
}

func writeValidationJPEGWithSize(t *testing.T, path string, width int, height int) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatalf("mkdir %s: %v", filepath.Dir(path), err)
	}
	file, err := os.Create(path)
	if err != nil {
		t.Fatalf("create jpeg %s: %v", path, err)
	}
	defer func() {
		if err := file.Close(); err != nil {
			t.Fatalf("close jpeg %s: %v", path, err)
		}
	}()
	if err := jpeg.Encode(file, image.NewRGBA(image.Rect(0, 0, width, height)), nil); err != nil {
		t.Fatalf("write jpeg %s: %v", path, err)
	}
}

func writeValidationMP4(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatalf("mkdir %s: %v", filepath.Dir(path), err)
	}
	data := validationMP4Bytes()
	if err := os.WriteFile(path, data, 0644); err != nil {
		t.Fatalf("write mp4 %s: %v", path, err)
	}
}

func validationMP4SizeBytes() int64 {
	return int64(len(validationMP4Bytes()))
}

func validationMP4Bytes() []byte {
	return []byte{
		0x00, 0x00, 0x00, 0x18,
		'f', 't', 'y', 'p',
		'i', 's', 'o', 'm',
		0x00, 0x00, 0x00, 0x00,
		'i', 's', 'o', 'm',
	}
}

func writeValidationHyperFramesPackage(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatalf("mkdir %s: %v", filepath.Dir(path), err)
	}
	const packageJSON = `{"name":"xai-video-timeline","version":"1.0.0","private":true}`
	if err := os.WriteFile(path, []byte(packageJSON), 0644); err != nil {
		t.Fatalf("write hyperframes package %s: %v", path, err)
	}
}

type validationHyperFramesClip struct {
	Index       int
	StartSec    float64
	DurationSec float64
}

func writeValidationHyperFramesIndex(t *testing.T, path string, shotIndexes ...int) {
	t.Helper()
	writeValidationHyperFramesIndexWithSpec(t, path, 720, 1280, float64(len(shotIndexes))*8, shotIndexes...)
}

func writeValidationHyperFramesIndexWithSpec(t *testing.T, path string, width int, height int, duration float64, shotIndexes ...int) {
	t.Helper()
	writeValidationHyperFramesIndexWithClips(t, path, width, height, duration, validationHyperFramesClips(shotIndexes...)...)
}

func writeValidationHyperFramesIndexWithClips(t *testing.T, path string, width int, height int, duration float64, clips ...validationHyperFramesClip) {
	t.Helper()
	writeValidationHyperFramesIndexHTML(t, path, true, width, height, duration, clips...)
}

func writeValidationHyperFramesIndexWithBareVideo(t *testing.T, path string, shotIndexes ...int) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatalf("mkdir %s: %v", filepath.Dir(path), err)
	}

	var html strings.Builder
	fmt.Fprintf(&html, `<!DOCTYPE html><html><body><div data-composition-id="xai-video" data-width="720" data-height="1280" data-duration="%.3f">`, float64(len(shotIndexes))*8)
	for _, clip := range validationHyperFramesClips(shotIndexes...) {
		fmt.Fprintf(&html, `<video src="../normalized/shot_%03d.mp4" data-start="%.3f" data-duration="%.3f"></video>`, clip.Index, clip.StartSec, clip.DurationSec)
	}
	html.WriteString("</div>")
	html.WriteString(validationHyperFramesRuntimeScript(validationHyperFramesClips(shotIndexes...), true))
	html.WriteString("</body></html>")
	if err := os.WriteFile(path, []byte(html.String()), 0644); err != nil {
		t.Fatalf("write hyperframes index %s: %v", path, err)
	}
}

func writeValidationHyperFramesIndexWithoutRuntimeHooks(t *testing.T, path string, shotIndexes ...int) {
	t.Helper()
	writeValidationHyperFramesIndexHTML(t, path, false, 720, 1280, float64(len(shotIndexes))*8, validationHyperFramesClips(shotIndexes...)...)
}

func writeValidationHyperFramesIndexWithoutVisibilityRuntime(t *testing.T, path string, shotIndexes ...int) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatalf("mkdir %s: %v", filepath.Dir(path), err)
	}

	var html strings.Builder
	fmt.Fprintf(&html, `<!DOCTYPE html><html><body><div data-composition-id="xai-video" data-width="720" data-height="1280" data-duration="%.3f">`, float64(len(shotIndexes))*8)
	for trackIndex, clip := range validationHyperFramesClips(shotIndexes...) {
		fmt.Fprintf(&html, `<video id="video-%d" class="shot-video clip" src="../normalized/shot_%03d.mp4" data-start="%.3f" data-duration="%.3f" data-track-index="%d" data-transition-out="cut"></video>`, clip.Index, clip.Index, clip.StartSec, clip.DurationSec, trackIndex*2)
	}
	html.WriteString("</div>")
	html.WriteString(validationHyperFramesRuntimeScript(validationHyperFramesClips(shotIndexes...), false))
	html.WriteString("</body></html>")
	if err := os.WriteFile(path, []byte(html.String()), 0644); err != nil {
		t.Fatalf("write hyperframes index %s: %v", path, err)
	}
}

func appendValidationHyperFramesRawShotReference(t *testing.T, path string, shotIndex int) {
	t.Helper()
	file, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		t.Fatalf("open hyperframes index %s: %v", path, err)
	}
	defer func() {
		if err := file.Close(); err != nil {
			t.Fatalf("close hyperframes index %s: %v", path, err)
		}
	}()
	if _, err := fmt.Fprintf(file, `<video src="../shots/shot_%03d.mp4"></video>`, shotIndex); err != nil {
		t.Fatalf("append raw shot reference to %s: %v", path, err)
	}
}

func appendValidationHyperFramesAbsoluteRawShotReference(t *testing.T, path string, outputDir string, shotIndex int) {
	t.Helper()
	file, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		t.Fatalf("open hyperframes index %s: %v", path, err)
	}
	defer func() {
		if err := file.Close(); err != nil {
			t.Fatalf("close hyperframes index %s: %v", path, err)
		}
	}()
	rawPath := filepath.Join(outputDir, "shots", fmt.Sprintf("shot_%03d.mp4", shotIndex))
	if _, err := fmt.Fprintf(file, `<video src="%s"></video>`, filepath.ToSlash(rawPath)); err != nil {
		t.Fatalf("append absolute raw shot reference to %s: %v", path, err)
	}
}

func appendValidationHyperFramesFileURLRawShotReference(t *testing.T, path string, outputDir string, shotIndex int) {
	t.Helper()
	file, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		t.Fatalf("open hyperframes index %s: %v", path, err)
	}
	defer func() {
		if err := file.Close(); err != nil {
			t.Fatalf("close hyperframes index %s: %v", path, err)
		}
	}()
	rawPath := filepath.ToSlash(filepath.Join(outputDir, "shots", fmt.Sprintf("shot_%03d.mp4", shotIndex)))
	rawURL := "file://" + strings.ReplaceAll(rawPath, " ", "%20")
	if _, err := fmt.Fprintf(file, `<video src="%s"></video>`, rawURL); err != nil {
		t.Fatalf("append file URL raw shot reference to %s: %v", path, err)
	}
}

func appendValidationHyperFramesNormalizedShotReference(t *testing.T, path string, shotIndex int) {
	t.Helper()
	file, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		t.Fatalf("open hyperframes index %s: %v", path, err)
	}
	defer func() {
		if err := file.Close(); err != nil {
			t.Fatalf("close hyperframes index %s: %v", path, err)
		}
	}()
	if _, err := fmt.Fprintf(file, `<video src="../normalized/shot_%03d.mp4"></video>`, shotIndex); err != nil {
		t.Fatalf("append normalized shot reference to %s: %v", path, err)
	}
}

func appendValidationHyperFramesExternalVideoReference(t *testing.T, path string, src string) {
	t.Helper()
	file, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		t.Fatalf("open hyperframes index %s: %v", path, err)
	}
	defer func() {
		if err := file.Close(); err != nil {
			t.Fatalf("close hyperframes index %s: %v", path, err)
		}
	}()
	if _, err := fmt.Fprintf(file, `<video class="shot-video clip" src="%s" data-start="0.000" data-duration="8.000"></video>`, src); err != nil {
		t.Fatalf("append external video reference to %s: %v", path, err)
	}
}

func appendValidationHyperFramesUppercaseExternalVideoReference(t *testing.T, path string, src string) {
	t.Helper()
	file, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		t.Fatalf("open hyperframes index %s: %v", path, err)
	}
	defer func() {
		if err := file.Close(); err != nil {
			t.Fatalf("close hyperframes index %s: %v", path, err)
		}
	}()
	if _, err := fmt.Fprintf(file, `<VIDEO CLASS="shot-video clip" SRC="%s" data-start="0.000" data-duration="8.000"></VIDEO>`, src); err != nil {
		t.Fatalf("append uppercase external video reference to %s: %v", path, err)
	}
}

func replaceValidationHyperFramesIndex(t *testing.T, path string, old string, new string) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read hyperframes index %s: %v", path, err)
	}
	updated := strings.Replace(string(data), old, new, 1)
	if updated == string(data) {
		t.Fatalf("hyperframes index %s did not contain %q", path, old)
	}
	if err := os.WriteFile(path, []byte(updated), 0644); err != nil {
		t.Fatalf("write hyperframes index %s: %v", path, err)
	}
}

func replaceValidationHyperFramesScriptWithComment(t *testing.T, path string, comment string) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read hyperframes index %s: %v", path, err)
	}
	document := string(data)
	start := strings.Index(document, "<script>")
	end := strings.Index(document, "</script>")
	if start < 0 || end < start {
		t.Fatalf("hyperframes index %s did not contain a script block", path)
	}
	updated := document[:start] + "<!-- " + comment + " -->" + document[end+len("</script>"):]
	if err := os.WriteFile(path, []byte(updated), 0644); err != nil {
		t.Fatalf("write hyperframes index %s: %v", path, err)
	}
}

func replaceValidationHyperFramesScriptText(t *testing.T, path string, scriptText string) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read hyperframes index %s: %v", path, err)
	}
	document := string(data)
	start := strings.Index(document, "<script>")
	end := strings.Index(document, "</script>")
	if start < 0 || end < start {
		t.Fatalf("hyperframes index %s did not contain a script block", path)
	}
	updated := document[:start+len("<script>")] + scriptText + document[end:]
	if err := os.WriteFile(path, []byte(updated), 0644); err != nil {
		t.Fatalf("write hyperframes index %s: %v", path, err)
	}
}

func appendValidationHyperFramesComment(t *testing.T, path string, comment string) {
	t.Helper()
	file, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		t.Fatalf("open hyperframes index %s: %v", path, err)
	}
	defer func() {
		if err := file.Close(); err != nil {
			t.Fatalf("close hyperframes index %s: %v", path, err)
		}
	}()
	if _, err := fmt.Fprintf(file, `<!-- %s -->`, comment); err != nil {
		t.Fatalf("append hyperframes comment to %s: %v", path, err)
	}
}

func insertValidationHyperFramesCommentBeforeScript(t *testing.T, path string, comment string) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read hyperframes index %s: %v", path, err)
	}
	document := string(data)
	marker := "</div><script>"
	if !strings.Contains(document, marker) {
		t.Fatalf("hyperframes index %s did not contain %q", path, marker)
	}
	updated := strings.Replace(document, marker, fmt.Sprintf("<!-- %s -->%s", comment, marker), 1)
	if err := os.WriteFile(path, []byte(updated), 0644); err != nil {
		t.Fatalf("write hyperframes index %s: %v", path, err)
	}
}

func writeValidationHyperFramesIndexWithSubtitle(t *testing.T, path string, subtitle string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatalf("mkdir %s: %v", filepath.Dir(path), err)
	}

	var document strings.Builder
	document.WriteString(`<!DOCTYPE html><html><body><div data-composition-id="xai-video" data-width="720" data-height="1280" data-duration="8.000">`)
	document.WriteString(`<video id="video-1" class="shot-video clip" src="../normalized/shot_001.mp4" data-start="0.000" data-duration="8.000" data-track-index="0"></video>`)
	fmt.Fprintf(&document, `<div id="subtitle-1" class="subtitle clip" data-start="0.000" data-duration="8.000" data-track-index="1">%s</div>`, html.EscapeString(subtitle))
	document.WriteString("</div>")
	document.WriteString(validationHyperFramesRuntimeScript(validationHyperFramesClips(1), true))
	document.WriteString("</body></html>")
	if err := os.WriteFile(path, []byte(document.String()), 0644); err != nil {
		t.Fatalf("write hyperframes index %s: %v", path, err)
	}
}

func writeValidationHyperFramesIndexHTML(t *testing.T, path string, includeRuntimeHooks bool, width int, height int, duration float64, clips ...validationHyperFramesClip) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatalf("mkdir %s: %v", filepath.Dir(path), err)
	}

	var html strings.Builder
	fmt.Fprintf(&html, `<!DOCTYPE html><html><body><div data-composition-id="xai-video" data-width="%d" data-height="%d" data-duration="%.3f">`, width, height, duration)
	for trackIndex, clip := range clips {
		fmt.Fprintf(&html, `<video id="video-%d" class="shot-video clip" src="../normalized/shot_%03d.mp4" data-start="%.3f" data-duration="%.3f" data-track-index="%d"></video>`, clip.Index, clip.Index, clip.StartSec, clip.DurationSec, trackIndex*2)
	}
	html.WriteString("</div>")
	if includeRuntimeHooks {
		html.WriteString(validationHyperFramesRuntimeScript(clips, true))
	}
	html.WriteString("</body></html>")
	if err := os.WriteFile(path, []byte(html.String()), 0644); err != nil {
		t.Fatalf("write hyperframes index %s: %v", path, err)
	}
}

func validationHyperFramesRuntimeScript(clips []validationHyperFramesClip, includeVisibility bool) string {
	var script strings.Builder
	script.WriteString(`<script>var shots = [`)
	for _, clip := range clips {
		fmt.Fprintf(&script, `{ video: document.getElementById("video-%d"), subtitle: document.getElementById("subtitle-%d"), start: %.3f, duration: %.3f },`, clip.Index, clip.Index, clip.StartSec, clip.DurationSec)
	}
	script.WriteString(`]; `)
	if includeVisibility {
		script.WriteString(`function applyShotVisibility(){ var fadeSeconds = 0.4; var node = { style: {} }; node.style.opacity = "1"; } `)
	}
	script.WriteString(`function seek(t){ t = Number(t) || 0; for (var i = 0; i < shots.length; i++) { var shot = shots[i]; var start = Number(shot.start); var duration = Number(shot.duration); var local = t - start; var target = Math.max(0, Math.min(local, Math.max(0, duration - 0.001))); shot.video.currentTime = target; } } var timeline = { seek: function (t) { seek(t); return timeline; } }; window.__timelines = window.__timelines || {}; window.__timelines["xai-video"] = timeline; window.__hf = { seek: timeline.seek };</script>`)
	return script.String()
}

func validationHyperFramesRuntimeScriptWithoutSeek(clips []validationHyperFramesClip) string {
	var script strings.Builder
	script.WriteString(`var shots = [`)
	for _, clip := range clips {
		fmt.Fprintf(&script, `{ video: document.getElementById("video-%d"), subtitle: document.getElementById("subtitle-%d") },`, clip.Index, clip.Index)
	}
	script.WriteString(`]; function applyShotVisibility(){ var fadeSeconds = 0.4; var node = { style: {} }; node.style.opacity = "1"; } window.__timelines = window.__timelines || {}; window.__timelines["xai-video"] = {}; window.__hf = {};`)
	return script.String()
}

func validationHyperFramesRuntimeScriptWithoutCurrentTime(clips []validationHyperFramesClip) string {
	var script strings.Builder
	script.WriteString(`var shots = [`)
	for _, clip := range clips {
		fmt.Fprintf(&script, `{ video: document.getElementById("video-%d"), subtitle: document.getElementById("subtitle-%d") },`, clip.Index, clip.Index)
	}
	script.WriteString(`]; function applyShotVisibility(){ var fadeSeconds = 0.4; var node = { style: {} }; node.style.opacity = "1"; } var timeline = { seek: function () { return timeline; } }; window.__timelines = window.__timelines || {}; window.__timelines["xai-video"] = timeline; window.__hf = { seek: timeline.seek };`)
	return script.String()
}

func validationHyperFramesRuntimeScriptWithoutLocalTime(clips []validationHyperFramesClip) string {
	var script strings.Builder
	script.WriteString(`var shots = [`)
	for _, clip := range clips {
		fmt.Fprintf(&script, `{ video: document.getElementById("video-%d"), subtitle: document.getElementById("subtitle-%d") },`, clip.Index, clip.Index)
	}
	script.WriteString(`]; function applyShotVisibility(){ var fadeSeconds = 0.4; var node = { style: {} }; node.style.opacity = "1"; } function seek(t){ for (var i = 0; i < shots.length; i++) { var shot = shots[i]; shot.video.currentTime = Number(t) || 0; } } var timeline = { seek: function (t) { seek(t); return timeline; } }; window.__timelines = window.__timelines || {}; window.__timelines["xai-video"] = timeline; window.__hf = { seek: timeline.seek };`)
	return script.String()
}

func validationHyperFramesClips(shotIndexes ...int) []validationHyperFramesClip {
	clips := make([]validationHyperFramesClip, 0, len(shotIndexes))
	cursor := 0.0
	for _, index := range shotIndexes {
		const duration = 8.0
		clips = append(clips, validationHyperFramesClip{
			Index:       index,
			StartSec:    cursor,
			DurationSec: duration,
		})
		cursor += duration
	}
	return clips
}
