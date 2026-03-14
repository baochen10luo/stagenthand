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

// Run executes the pipeline from an initial input (story prompt, outline JSON, or storyboard JSON)
// and returns the final PipelineResult.
// It automatically detects the input type and skips already-completed stages.
func (o *Orchestrator) Run(ctx context.Context, inputData []byte) (*PipelineResult, error) {
	if len(inputData) == 0 {
		return nil, fmt.Errorf("input data is empty")
	}

	// 1. Resolve to a Storyboard
	storyboard, err := o.resolveToStoryboard(ctx, inputData)
	if err != nil {
		return nil, err
	}

	// HITL: storyboard checkpoint
	if err := o.checkpoint(ctx, "pipeline", domain.StageStoryboard); err != nil {
		return nil, err
	}

	// 2. Extract panels and generate images
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

// resolveToStoryboard determines if the input is a Story, Outline, or Storyboard
// and performs necessary LLM transformations to reach the Storyboard stage.
func (o *Orchestrator) resolveToStoryboard(ctx context.Context, input []byte) (domain.Storyboard, error) {
	// Is it already a Storyboard?
	var sb domain.Storyboard
	if jsonUnmarshal(input, &sb) == nil && len(sb.Scenes) > 0 {
		return sb, nil
	}

	// Is it an Outline? (Try to convert to Storyboard)
	var outline struct {
		Episodes []any `json:"episodes"`
	}
	if jsonUnmarshal(input, &outline) == nil && len(outline.Episodes) > 0 {
		return o.transformOutline(ctx, input)
	}

	// Assume it's a raw Story prompt
	return o.transformStory(ctx, input)
}

func (o *Orchestrator) transformStory(ctx context.Context, story []byte) (domain.Storyboard, error) {
	// Story -> Outline
	outlineJSON, err := o.deps.LLM.GenerateTransformation(ctx, PromptStoryToOutline, story)
	if err != nil {
		return domain.Storyboard{}, fmt.Errorf("story-to-outline failed: %w", err)
	}

	// Outline -> Storyboard
	return o.transformOutline(ctx, outlineJSON)
}

func (o *Orchestrator) transformOutline(ctx context.Context, outline []byte) (domain.Storyboard, error) {
	storyboardJSON, err := o.deps.LLM.GenerateTransformation(ctx, PromptOutlineToStoryboard, outline)
	if err != nil {
		return domain.Storyboard{}, fmt.Errorf("outline-to-storyboard failed: %w", err)
	}

	var sb domain.Storyboard
	if err := jsonUnmarshal(storyboardJSON, &sb); err != nil {
		return domain.Storyboard{}, fmt.Errorf("invalid storyboard JSON produced: %w", err)
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
