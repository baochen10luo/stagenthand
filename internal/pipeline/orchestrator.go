package pipeline

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	"github.com/baochen10luo/stagenthand/internal/domain"
)

// ImageBatcher generates images for a batch of panels.
// Extracted as interface to honour ISP — orchestrator only needs batch generation.
type ImageBatcher interface {
	BatchGenerateImages(ctx context.Context, panels []domain.Panel, targetDir string) ([]domain.Panel, error)
}

// AudioBatcher generates audio for a batch of panels.
type AudioBatcher interface {
	BatchGenerateAudio(ctx context.Context, panels []domain.Panel, targetDir string) ([]domain.Panel, error)
}

// MusicBatcher generates a single background music track for a project.
type MusicBatcher interface {
	GenerateProjectBGM(ctx context.Context, projectID string, baseTag string, targetDir string) (string, error)
}

// CheckpointGate represents a HITL pause point that must be approved to continue.
type CheckpointGate interface {
	CreateAndWait(ctx context.Context, jobID string, stage domain.CheckpointStage) error
}

// VideoCriticEvaluator evaluates a rendered video against its RemotionProps.
// Defined here (in the consumer package) per DIP.
type VideoCriticEvaluator interface {
	Evaluate(ctx context.Context, videoPath string, propsJSON []byte) (*CriticResult, error)
}

// CriticResult mirrors video.Evaluation but lives in pipeline package (DIP).
type CriticResult struct {
	VisualScore    int    `json:"visual_score"`
	AudioSyncScore int    `json:"audio_sync_score"`
	AdherenceScore int    `json:"adherence_score"`
	ToneScore      int    `json:"tone_score"`
	Feedback       string `json:"feedback"`
	Action         string `json:"action"` // "APPROVE" or "REJECT"
}

// IsApproved returns true if the critic result passes the acceptance threshold.
func (r *CriticResult) IsApproved() bool {
	if r.VisualScore < 8 || r.AudioSyncScore < 8 {
		return false
	}
	return r.VisualScore+r.AudioSyncScore+r.AdherenceScore+r.ToneScore >= 32
}

// PropsCriticEvaluator evaluates generated panels JSON for quality issues.
// Defined here (in the consumer package) per DIP.
type PropsCriticEvaluator interface {
	Evaluate(ctx context.Context, propsJSON []byte) (*PropsCriticResult, error)
}

// PropsCriticResult holds the outcome of a PropsCritic evaluation.
type PropsCriticResult struct {
	Issues []string `json:"issues"`
	OK     bool     `json:"ok"`
}

// OrchestratorDeps groups external dependencies injected at construction time.
// Dependency Inversion: orchestrator only knows interfaces, never concrete types.
type OrchestratorDeps struct {
	LLM         Transformer
	Images      ImageBatcher
	Audio       AudioBatcher
	Music       MusicBatcher
	Checkpoints CheckpointGate
	PropsCritic PropsCriticEvaluator // optional; nil = disabled
	Language    string               // BCP-47 language tag for TTS/dialogue
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

func (o *Orchestrator) Run(ctx context.Context, inputData []byte) (*PipelineResult, error) {
	if len(inputData) == 0 {
		return nil, fmt.Errorf("input data is empty")
	}

	// 1. Detection: Is this already a flat list of panels (RemotionProps)?
	var props domain.RemotionProps
	if jsonUnmarshal(inputData, &props) == nil && len(props.Panels) > 0 {
		return o.executeFromPanels(ctx, props.ProjectID, props.Panels, props.BGMURL, props.Directives)
	}

	// 2. Normal flow: Resolve to a Storyboard
	storyboard, err := o.resolveToStoryboard(ctx, inputData)
	if err != nil {
		return nil, err
	}

	// HITL: review storyboard before generating panels/images
	if err := o.checkpoint(ctx, "pipeline", domain.StageStoryboard); err != nil {
		return nil, err
	}

	// 3. Storyboard -> Panels
	panels, err := o.transformStoryboardToPanels(ctx, storyboard)
	if err != nil {
		return nil, fmt.Errorf("panels stage failed: %w", err)
	}

	// 3.5 PropsCritic: validate panels, retry once if issues found
	if o.deps.PropsCritic != nil {
		panelsJSON, _ := jsonMarshal(struct {
			Panels []domain.Panel `json:"panels"`
		}{Panels: panels})

		eval, evalErr := o.deps.PropsCritic.Evaluate(ctx, panelsJSON)
		if evalErr != nil {
			slog.Warn("PropsCritic evaluation failed, continuing with current panels", "error", evalErr)
		} else if !eval.OK {
			for _, issue := range eval.Issues {
				slog.Warn("PropsCritic issue", "issue", issue)
			}
			// Retry once: re-generate panels from the same storyboard
			retryPanels, retryErr := o.transformStoryboardToPanels(ctx, storyboard)
			if retryErr != nil {
				return nil, fmt.Errorf("panels retry after PropsCritic failed: %w", retryErr)
			}
			panels = retryPanels
		}
	}

	return o.executeFromPanels(ctx, storyboard.ProjectID, panels, storyboard.BGMURL, storyboard.Directives)
}

// executeFromPanels runs the asset generation stages (Images, Audio, Music) from a flat panel list.
func (o *Orchestrator) executeFromPanels(ctx context.Context, projectID string, panels []domain.Panel, bgmURL string, directives *domain.Directives) (*PipelineResult, error) {
	var err error

	// Prepend StylePrompt to enforce visual consistency across all panels
	if directives != nil && directives.StylePrompt != "" {
		for i := range panels {
			panels[i].Description = directives.StylePrompt + ", " + panels[i].Description
		}
	}

	// Apply dynamic duration: ensure each panel is long enough for its dialogue
	// plus an inversely-proportional breathing buffer for the viewer.
	panels = applyDynamicDuration(panels)

	// Apply subtitle timings: compute start_sec/end_sec for each DialogueLine
	// using character-proportional distribution (Strategy C) or full-panel
	// display for single-line panels (Strategy D).
	panels = applySubtitleTimings(panels)


	// 3. Generate images for panels
	if !o.deps.DryRun {
		// Target directory for images: projects/<project_id>/images/
		targetDir := fmt.Sprintf("projects/%s/images", projectID)
		panels, err = o.deps.Images.BatchGenerateImages(ctx, panels, targetDir)
		if err != nil {
			return nil, fmt.Errorf("image stage failed: %w", err)
		}
	}

	// HITL: images checkpoint
	if err := o.checkpoint(ctx, "pipeline", domain.StageImages); err != nil {
		return nil, err
	}

	// 4. Generate audio (TTS) for panels
	if !o.deps.DryRun && o.deps.Audio != nil {
		audioDir := fmt.Sprintf("projects/%s/audio", projectID)
		panels, err = o.deps.Audio.BatchGenerateAudio(ctx, panels, audioDir)
		if err != nil {
			return nil, fmt.Errorf("audio stage failed: %w", err)
		}
	}

	// 5. Generate BGM
	if !o.deps.DryRun && o.deps.Music != nil {
		musicDir := fmt.Sprintf("projects/%s/audio", projectID)
		
		bgmTags := "cinematic"
		if directives != nil && directives.BGMTags != "" {
			bgmTags = directives.BGMTags
		}

		bgm, err := o.deps.Music.GenerateProjectBGM(ctx, projectID, bgmTags, musicDir)
		if err != nil {
			fmt.Printf("⚠️  [Warning] BGM generation skipped: %v\n", err)
		} else {
			bgmURL = bgm
		}
	}

	return &PipelineResult{
		Storyboard: domain.Storyboard{ProjectID: projectID, BGMURL: bgmURL, Directives: directives}, // Minimal backfill
		Panels:     panels,
	}, nil
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

	// HITL: review outline before generating storyboard
	if err := o.checkpoint(ctx, "pipeline", domain.StageOutline); err != nil {
		return domain.Storyboard{}, err
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

func (o *Orchestrator) transformStoryboardToPanels(ctx context.Context, sb domain.Storyboard) ([]domain.Panel, error) {
	input, _ := jsonMarshal(sb)

	// Build language-aware prompt
	prompt := buildStoryboardToPanelsPrompt(o.deps.Language, sb)
	panelsJSON, err := o.deps.LLM.GenerateTransformation(ctx, prompt, input)
	if err != nil {
		return nil, err
	}

	var result struct {
		Panels []domain.Panel `json:"panels"`
	}
	if err := jsonUnmarshal(panelsJSON, &result); err != nil {
		return nil, fmt.Errorf("LLM produced invalid panels JSON: %w", err)
	}
	// Post-process: strip any speaker/panel prefix artefacts the LLM may have added.
	return cleanPanels(result.Panels), nil
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

// applyDynamicDuration ensures every panel has enough display time for its dialogue.
// For panels with dialogue, we estimate speech duration (120ms/char at 90% Polly rate),
// then add an inversely-proportional breathing buffer:
//
//	buffer = clamp(3.0 - speechSec × 0.25, 1.0, 3.5)
//
// This means short punchy lines ("Not tonight.") get a longer dramatic pause,
// while long monologues flow straight into the next panel.
// Panels without dialogue are left untouched.
func applyDynamicDuration(panels []domain.Panel) []domain.Panel {
	const msPerChar = 0.12  // seconds per rune at 90% Polly neural rate
	const baseBuffer = 3.0  // maximum buffer for very short lines
	const minBuffer = 1.0   // minimum breathing room
	const bufferSlope = 0.25 // how quickly buffer shrinks as speech grows

	for i, p := range panels {
		if p.Dialogue == "" {
			continue // silent panel — leave LLM-assigned duration intact
		}
		speechSec := float64(len([]rune(p.Dialogue))) * msPerChar
		buffer := baseBuffer - speechSec*bufferSlope
		if buffer < minBuffer {
			buffer = minBuffer
		}
		if buffer > 3.5 {
			buffer = 3.5
		}
		required := speechSec + buffer
		if panels[i].DurationSec < required {
			panels[i].DurationSec = required
		}
	}
	return panels
}

// applySubtitleTimings sets StartSec/EndSec on every DialogueLine in every panel.
// It delegates per-panel computation to calcSubtitleTimings.
func applySubtitleTimings(panels []domain.Panel) []domain.Panel {
	for i := range panels {
		if len(panels[i].DialogueLines) == 0 {
			continue
		}
		panels[i].DialogueLines = calcSubtitleTimings(panels[i].DialogueLines, panels[i].DurationSec)
	}
	return panels
}

// calcSubtitleTimings computes per-line subtitle timing for a single panel.
//
// Strategy D — single line (or empty): the entire panel duration is assigned
// to the one line; no time splitting is performed.
//
// Strategy C — multiple lines: each line's share of the total duration is
// proportional to its non-whitespace rune count, so longer lines stay on
// screen longer than short ones.
func calcSubtitleTimings(lines []domain.DialogueLine, totalDuration float64) []domain.DialogueLine {
	if len(lines) == 0 {
		return lines
	}
	// Strategy D: single line — occupy the full panel.
	if len(lines) == 1 {
		lines[0].StartSec = 0
		lines[0].EndSec = totalDuration
		return lines
	}
	// Strategy C: proportional distribution by non-whitespace rune count.
	counts := make([]int, len(lines))
	total := 0
	for i, l := range lines {
		n := 0
		for _, c := range l.Text {
			if !isWhitespaceRune(c) {
				n++
			}
		}
		if n == 0 {
			n = 1 // avoid zero-width line
		}
		counts[i] = n
		total += n
	}
	cursor := 0.0
	for i := range lines {
		dur := (float64(counts[i]) / float64(total)) * totalDuration
		lines[i].StartSec = cursor
		cursor += dur
		if i == len(lines)-1 {
			lines[i].EndSec = totalDuration
		} else {
			lines[i].EndSec = cursor
		}
	}
	return lines
}

// isWhitespaceRune reports whether r is a Unicode whitespace character.
func isWhitespaceRune(r rune) bool {
	return strings.TrimSpace(string(r)) == ""
}
