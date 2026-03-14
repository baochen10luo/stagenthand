package pipeline_test

import (
	"context"
	"errors"
	"testing"

	"github.com/baochen10luo/stagenthand/internal/domain"
	"github.com/baochen10luo/stagenthand/internal/pipeline"
)

// --- Mock implementations ---

type mockTransformer struct {
	output []byte
	err    error
}

func (m *mockTransformer) GenerateTransformation(_ context.Context, _ string, _ []byte) ([]byte, error) {
	return m.output, m.err
}

type mockImageBatcher struct {
	called bool
	err    error
}

func (m *mockImageBatcher) BatchGenerateImages(_ context.Context, panels []domain.Panel) ([]domain.Panel, error) {
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
	storyboardJSON := []byte(`{"project_id":"p1","episode":1,"scenes":[{"number":1,"description":"scene","panels":[{"scene_number":1,"panel_number":1,"description":"hero","dialogue":"Hello","character_refs":[],"duration_sec":3.0}]}]}`)

	orch := pipeline.NewOrchestrator(pipeline.OrchestratorDeps{
		LLM: &mockTransformer{
			// Will be called multiple times; always returns next-stage fixture
			output: storyboardJSON,
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
	storyboardJSON := []byte(`{"project_id":"dry","episode":1,"scenes":[{"number":1,"description":"s","panels":[{"scene_number":1,"panel_number":1,"description":"p","dialogue":"d","character_refs":[],"duration_sec":3.0}]}]}`)

	imgBatcher := &mockImageBatcher{}
	orch := pipeline.NewOrchestrator(pipeline.OrchestratorDeps{
		LLM:         &mockTransformer{output: storyboardJSON},
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
	storyboardJSON := []byte(`{"project_id":"hitl","episode":1,"scenes":[{"number":1,"description":"s","panels":[]}]}`)

	orch := pipeline.NewOrchestrator(pipeline.OrchestratorDeps{
		LLM:         &mockTransformer{output: storyboardJSON},
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
