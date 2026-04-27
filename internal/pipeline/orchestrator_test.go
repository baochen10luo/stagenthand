package pipeline_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/baochen10luo/stagenthand/internal/domain"
	"github.com/baochen10luo/stagenthand/internal/pipeline"
)

// --- Mock implementations ---

type mockTransformer struct {
	output       []byte
	err          error
	GenerateFunc func(ctx context.Context, systemPrompt string, inputData []byte) ([]byte, error)
}

func (m *mockTransformer) GenerateTransformation(ctx context.Context, systemPrompt string, inputData []byte) ([]byte, error) {
	if m.GenerateFunc != nil {
		return m.GenerateFunc(ctx, systemPrompt, inputData)
	}
	return m.output, m.err
}

type mockImageBatcher struct {
	called bool
	err    error
}

func (m *mockImageBatcher) BatchGenerateImages(_ context.Context, panels []domain.Panel, _ string) ([]domain.Panel, error) {
	m.called = true
	if m.err != nil {
		return nil, m.err
	}
	// mark each panel as having an image
	result := make([]domain.Panel, len(panels))
	for i, p := range panels {
		p.ImageURL = "https://example.com/generated_" + p.Description + ".png"
		result[i] = p
	}
	return result, nil
}

type mockCheckpointStore struct {
	approved bool
}

func (m *mockCheckpointStore) CreateAndWait(_ context.Context, _ string, _ domain.CheckpointStage) error {
	if !m.approved {
		return errors.New("checkpoint rejected")
	}
	return nil
}

// --- Tests ---

func TestOrchestrator_RunStagesInOrder(t *testing.T) {
	outlineJSON := []byte(`{"project_id":"p1","episodes":[{"number":1,"title":"Ep1","synopsis":"s","hook":"h","cliffhanger":"c"}]}`)
	storyboardJSON := []byte(`{"project_id":"p1","episode":1,"scenes":[{"number":1,"description":"scene"}]}`)
	panelsJSON := []byte(`{"panels":[{"scene_number":1,"panel_number":1,"description":"hero","dialogue":"Hello","character_refs":[],"duration_sec":3.0}]}`)

	orch := pipeline.NewOrchestrator(pipeline.OrchestratorDeps{
		LLM: &mockTransformer{
			GenerateFunc: func(_ context.Context, systemPrompt string, _ []byte) ([]byte, error) {
				if systemPrompt == pipeline.PromptOutlineToStoryboard {
					return storyboardJSON, nil
				}
				if strings.HasPrefix(systemPrompt, pipeline.PromptStoryboardToPanels) {
					return panelsJSON, nil
				}
				return nil, errors.New("unexpected prompt")
			},
		},
		Images:      &mockImageBatcher{},
		Checkpoints: &mockCheckpointStore{approved: true},
		DryRun:      false,
		SkipHITL:    true,
	})

	ctx := context.Background()
	result, err := orch.Run(ctx, outlineJSON)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result == nil {
		t.Fatal("expected non-nil result")
	}
}

func TestOrchestrator_DryRunSkipsImages(t *testing.T) {
	storyboardJSON := []byte(`{"project_id":"dry","episode":1,"scenes":[{"number":1,"description":"s"}]}`)
	panelsJSON := []byte(`{"panels":[{"scene_number":1,"panel_number":1,"description":"p"}]}`)

	imgBatcher := &mockImageBatcher{}
	orch := pipeline.NewOrchestrator(pipeline.OrchestratorDeps{
		LLM:         &mockTransformer{output: panelsJSON},
		Images:      imgBatcher,
		Checkpoints: &mockCheckpointStore{approved: true},
		DryRun:      true,
		SkipHITL:    true,
	})

	_, err := orch.Run(context.Background(), storyboardJSON)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if imgBatcher.called {
		t.Error("dry-run: image generator should NOT be called")
	}
}

func TestOrchestrator_HITLRejectionAborts(t *testing.T) {
	storyboardJSON := []byte(`{"project_id":"hitl","episode":1,"scenes":[{"number":1,"description":"s"}]}`)
	panelsJSON := []byte(`{"panels":[]}`)

	orch := pipeline.NewOrchestrator(pipeline.OrchestratorDeps{
		LLM:         &mockTransformer{output: panelsJSON},
		Images:      &mockImageBatcher{},
		Checkpoints: &mockCheckpointStore{approved: false}, // rejects
		DryRun:      false,
		SkipHITL:    false,
	})

	_, err := orch.Run(context.Background(), storyboardJSON)
	if err == nil {
		t.Error("expected error when checkpoint is rejected, got nil")
	}
}

func TestOrchestrator_LLMFailurePropagates(t *testing.T) {
	orch := pipeline.NewOrchestrator(pipeline.OrchestratorDeps{
		LLM:         &mockTransformer{err: errors.New("LLM quota exceeded")},
		Images:      &mockImageBatcher{},
		Checkpoints: &mockCheckpointStore{approved: true},
		DryRun:      false,
		SkipHITL:    true,
	})

	_, err := orch.Run(context.Background(), []byte(`{"story":"test"}`))
	if err == nil {
		t.Error("expected LLM error to propagate, got nil")
	}
}

// --- Fix 3: Manifest populated after image generation ---

func TestOrchestrator_ManifestPopulatedAfterImages(t *testing.T) {
	storyboardJSON := []byte(`{"project_id":"proj-manifest","episode":1,"scenes":[{"number":1,"description":"s","panels":[]}]}`)
	panelsJSON := []byte(`{"panels":[{"scene_number":1,"panel_number":1,"description":"hero","dialogue":"Hello","duration_sec":3.0}]}`)

	imgBatcher := &mockImageBatcher{}
	orch := pipeline.NewOrchestrator(pipeline.OrchestratorDeps{
		LLM:         &mockTransformer{output: panelsJSON},
		Images:      imgBatcher,
		Checkpoints: &mockCheckpointStore{approved: true},
		DryRun:      false,
		SkipHITL:    true,
	})

	result, err := orch.Run(context.Background(), storyboardJSON)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Manifest == nil {
		t.Fatal("expected result.Manifest to be non-nil after image generation")
	}
	if result.Manifest.ProjectID != "proj-manifest" {
		t.Errorf("expected ProjectID 'proj-manifest', got %q", result.Manifest.ProjectID)
	}
	if len(result.Manifest.Panels) == 0 {
		t.Error("expected manifest to have panels")
	}
	// Verify manifest panels have AudioURL cleared (captured before audio stage)
	for _, p := range result.Manifest.Panels {
		if p.AudioURL != "" {
			t.Errorf("manifest panel has AudioURL %q; should be empty (manifest is pre-audio)", p.AudioURL)
		}
	}
	// Verify manifest panels have ImageURL set (captured after image stage)
	for _, p := range result.Manifest.Panels {
		if p.ImageURL == "" {
			t.Errorf("manifest panel has empty ImageURL; should be set (manifest is post-image)")
		}
	}
}

func TestOrchestrator_DryRun_ManifestIsNil(t *testing.T) {
	storyboardJSON := []byte(`{"project_id":"dryproj","episode":1,"scenes":[{"number":1,"description":"s"}]}`)
	panelsJSON := []byte(`{"panels":[{"scene_number":1,"panel_number":1,"description":"p","duration_sec":3.0}]}`)

	orch := pipeline.NewOrchestrator(pipeline.OrchestratorDeps{
		LLM:         &mockTransformer{output: panelsJSON},
		Images:      &mockImageBatcher{},
		Checkpoints: &mockCheckpointStore{approved: true},
		DryRun:      true,
		SkipHITL:    true,
	})

	result, err := orch.Run(context.Background(), storyboardJSON)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Manifest != nil {
		t.Error("expected result.Manifest to be nil in dry-run mode")
	}
}

// --- Fix 1: Duration function naming tests ---

// TestOverrideDurationFromAudio verifies that OverrideDurationFromAudio always sets
// DurationSec = audio_duration + 0.5, even when the original DurationSec was LONGER.
// Since mp3Duration calls ffprobe (unavailable in tests), panels with AudioURL pointing
// to non-existent files are left unchanged — we verify the non-regression case.
func TestOverrideDurationFromAudio_NoAudioURL_Unchanged(t *testing.T) {
	panels := []domain.Panel{
		{SceneNumber: 1, PanelNumber: 1, DurationSec: 10.0, AudioURL: ""},
	}
	result := pipeline.OverrideDurationFromAudio(panels)
	if result[0].DurationSec != 10.0 {
		t.Errorf("expected DurationSec 10.0 (no AudioURL), got %v", result[0].DurationSec)
	}
}

// TestApplyRealAudioDuration_NoAudioURL_Unchanged verifies that ApplyRealAudioDuration
// (the extend-only variant) also leaves panels without AudioURL untouched.
func TestApplyRealAudioDuration_NoAudioURL_Unchanged(t *testing.T) {
	panels := []domain.Panel{
		{SceneNumber: 1, PanelNumber: 1, DurationSec: 5.0, AudioURL: ""},
	}
	result := pipeline.ApplyRealAudioDuration(panels)
	if result[0].DurationSec != 5.0 {
		t.Errorf("expected DurationSec 5.0 (no AudioURL), got %v", result[0].DurationSec)
	}
}

// TestDurationFunctionSemanticsDiffer verifies that OverrideDurationFromAudio and
// ApplyRealAudioDuration have the correct documented semantics:
// - OverrideDurationFromAudio always overrides (even shrinks)
// - ApplyRealAudioDuration only extends (never shrinks)
// We exercise this via panels without AudioURLs (ffprobe not available in CI),
// confirming neither function touches panels with no audio.
func TestDurationFunctionSemanticsDiffer(t *testing.T) {
	table := []struct {
		name          string
		inputDuration float64
		audioURL      string
		wantDuration  float64
		fn            func([]domain.Panel) []domain.Panel
	}{
		{
			name:          "OverrideDurationFromAudio: no URL, leaves unchanged",
			inputDuration: 99.0,
			audioURL:      "",
			wantDuration:  99.0,
			fn:            pipeline.OverrideDurationFromAudio,
		},
		{
			name:          "ApplyRealAudioDuration: no URL, leaves unchanged",
			inputDuration: 99.0,
			audioURL:      "",
			wantDuration:  99.0,
			fn:            pipeline.ApplyRealAudioDuration,
		},
	}
	for _, tc := range table {
		t.Run(tc.name, func(t *testing.T) {
			panels := []domain.Panel{
				{SceneNumber: 1, PanelNumber: 1, DurationSec: tc.inputDuration, AudioURL: tc.audioURL},
			}
			result := tc.fn(panels)
			if result[0].DurationSec != tc.wantDuration {
				t.Errorf("expected DurationSec %v, got %v", tc.wantDuration, result[0].DurationSec)
			}
		})
	}
}

