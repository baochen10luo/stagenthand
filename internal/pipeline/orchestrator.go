package pipeline

import (
	"context"
	"fmt"

	"github.com/baochen10luo/stagenthand/internal/domain"
)

// ImageBatcher generates images for a batch of panels.
// Extracted as interface to honour ISP — orchestrator only needs batch generation.
type ImageBatcher interface {
	BatchGenerateImages(ctx context.Context, panels []domain.Panel) ([]domain.Panel, error)
}

// CheckpointGate represents a HITL pause point that must be approved to continue.
type CheckpointGate interface {
	CreateAndWait(ctx context.Context, jobID string, stage domain.CheckpointStage) error
}

// OrchestratorDeps groups external dependencies injected at construction time.
// Dependency Inversion: orchestrator only knows interfaces, never concrete types.
type OrchestratorDeps struct {
	LLM         Transformer
	Images      ImageBatcher
	Checkpoints CheckpointGate
	DryRun      bool
	SkipHITL    bool
}

// Orchestrator coordinates the full shand pipeline:
//
//	story → outline → storyboard → panels → images → remotion-props → mp4
type Orchestrator struct {
	deps OrchestratorDeps
}

// NewOrchestrator constructs an Orchestrator with explicit deps injection.
func NewOrchestrator(deps OrchestratorDeps) *Orchestrator {
	return &Orchestrator{deps: deps}
}

// PipelineResult holds the final artefacts from a complete pipeline run.
type PipelineResult struct {
	Storyboard domain.Storyboard
	Panels     []domain.Panel
	Props      domain.RemotionProps
}

// Run executes the pipeline from an initial input (story or storyboard JSON)
// and returns the final RemotionProps.
// Errors propagate immediately — no silent failures.
func (o *Orchestrator) Run(ctx context.Context, inputJSON []byte) (*PipelineResult, error) {
	// Stage 1: parse storyboard from input
	// (in a real pipeline this would also call story→outline→storyboard LLM steps;
	// here we accept already-transformed storyboard JSON to keep the orchestrator thin)
	storyboard, err := o.parseStoryboard(ctx, inputJSON)
	if err != nil {
		return nil, fmt.Errorf("storyboard stage failed: %w", err)
	}

	// HITL: storyboard checkpoint
	if err := o.checkpoint(ctx, "pipeline", domain.StageStoryboard); err != nil {
		return nil, err
	}

	// Stage 2: image generation (skipped in dry-run)
	panels := flattenScenePanels(storyboard.Scenes)
	if !o.deps.DryRun {
		panels, err = o.deps.Images.BatchGenerateImages(ctx, panels)
		if err != nil {
			return nil, fmt.Errorf("image stage failed: %w", err)
		}
	}

	// HITL: images checkpoint
	if err := o.checkpoint(ctx, "pipeline", domain.StageImages); err != nil {
		return nil, err
	}

	result := &PipelineResult{
		Storyboard: storyboard,
		Panels:     panels,
	}
	return result, nil
}

// parseStoryboard runs the LLM transformation to produce a Storyboard.
// If inputJSON is already a valid Storyboard, it returns it directly.
func (o *Orchestrator) parseStoryboard(ctx context.Context, inputJSON []byte) (domain.Storyboard, error) {
	// Try parsing as-is first (storyboard already provided)
	var sb domain.Storyboard
	if err := jsonUnmarshal(inputJSON, &sb); err == nil && len(sb.Scenes) > 0 {
		return sb, nil
	}

	// Otherwise ask LLM to transform it
	out, err := o.deps.LLM.GenerateTransformation(ctx, PromptOutlineToStoryboard, inputJSON)
	if err != nil {
		return domain.Storyboard{}, err
	}

	if err := jsonUnmarshal(out, &sb); err != nil {
		return domain.Storyboard{}, fmt.Errorf("LLM produced invalid storyboard JSON: %w", err)
	}
	return sb, nil
}

// checkpoint pauses for HITL approval unless SkipHITL is set.
func (o *Orchestrator) checkpoint(ctx context.Context, jobID string, stage domain.CheckpointStage) error {
	if o.deps.SkipHITL {
		return nil
	}
	return o.deps.Checkpoints.CreateAndWait(ctx, jobID, stage)
}

// flattenScenePanels extracts all panels from scenes in order.
func flattenScenePanels(scenes []domain.Scene) []domain.Panel {
	var out []domain.Panel
	for _, s := range scenes {
		out = append(out, s.Panels...)
	}
	return out
}
