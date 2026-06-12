package xaipipeline

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

const PromptStoryToXAIManifest = `You are the planning stage for an xAI-native video pipeline.

Turn the user's story into a compact JSON manifest for per-shot video generation.
Return JSON only. Do not include markdown.

Schema:
{
  "project_id": "short-kebab-id",
  "format": "portrait",
  "fps": 24,
  "width": 720,
  "height": 1280,
  "shots": [
    {
      "index": 1,
      "prompt": "cinematic visual prompt for grok-imagine-video",
      "duration_sec": 8,
      "aspect_ratio": "9:16",
      "resolution": "720p",
      "subtitle": "optional subtitle text",
      "transition_out": "cut"
    }
  ]
}

If the input includes target_shots greater than zero, return exactly target_shots shots.
Use transition_out "cut" by default; "fade" is also supported. Do not invent other transition_out values.
The shots must be directly usable as xAI video prompts. Do not plan still-image generation, TTS, BGM, local model work, browser automation, or Remotion rendering.`

type Transformer interface {
	GenerateTransformation(ctx context.Context, systemPrompt string, inputData []byte) ([]byte, error)
}

type LLMPlanner struct {
	transformer Transformer
}

func NewLLMPlanner(transformer Transformer) *LLMPlanner {
	return &LLMPlanner{transformer: transformer}
}

func (p *LLMPlanner) Plan(ctx context.Context, input PlanInput) (Manifest, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if p.transformer == nil {
		return Manifest{}, errors.New("xai planner transformer is nil")
	}
	story := strings.TrimSpace(input.Story)
	if story == "" {
		return Manifest{}, errors.New("xai planner story is empty")
	}
	if input.TargetShots < 0 {
		return Manifest{}, errors.New("xai planner target shots must be zero or greater")
	}
	format := normalizeRequestedFormat(input.Format)
	if err := validateSupportedFormat(format); err != nil {
		return Manifest{}, err
	}
	if err := ctx.Err(); err != nil {
		return Manifest{}, err
	}

	req := struct {
		Story       string `json:"story"`
		TargetShots int    `json:"target_shots,omitempty"`
		Format      string `json:"format,omitempty"`
	}{
		Story:       story,
		TargetShots: input.TargetShots,
		Format:      format,
	}
	data, err := json.Marshal(req)
	if err != nil {
		return Manifest{}, fmt.Errorf("marshal xai planner request: %w", err)
	}

	raw, err := p.transformer.GenerateTransformation(ctx, PromptStoryToXAIManifest, data)
	if err != nil {
		return Manifest{}, fmt.Errorf("xai planner llm: %w", err)
	}
	if err := ctx.Err(); err != nil {
		return Manifest{}, err
	}
	raw = []byte(strings.TrimSpace(string(raw)))
	if len(raw) == 0 {
		return Manifest{}, errors.New("xai planner llm output is empty")
	}

	var manifest *Manifest
	if err := json.Unmarshal(raw, &manifest); err != nil {
		return Manifest{}, fmt.Errorf("parse xai manifest: %w", err)
	}
	if manifest == nil {
		return Manifest{}, errors.New("xai planner llm output is not a manifest object")
	}
	return *manifest, nil
}
