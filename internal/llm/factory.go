package llm

import (
	"context"
	"fmt"

	"github.com/baochen10luo/stagenthand/config"
	"github.com/baochen10luo/stagenthand/internal/pipeline"
)

// NewClient returns a new LLM client.
// If dryRun is true, it returns a MockClient that responds with a dummy JSON payload.
func NewClient(provider string, dryRun bool, cfg *config.Config) (Client, error) {
	if dryRun || provider == "mock" {
		return &MockClient{
			GenerateFunc: func(ctx context.Context, systemPrompt string, inputData []byte) ([]byte, error) {
				// Stage 1: Story -> Outline
				if systemPrompt == pipeline.PromptStoryToOutline {
					return []byte(`{"project_id": "test-proj", "episodes": [{"number": 1, "title": "Mock Title", "synopsis": "Mock S", "hook": "H", "cliffhanger": "C"}]}`), nil
				}
				// Stage 2: Outline -> Storyboard
				if systemPrompt == pipeline.PromptOutlineToStoryboard {
					return []byte(`{"project_id": "test-proj", "episode": 1, "scenes": [{"number": 1, "description": "Mock Scene"}]}`), nil
				}
				// Stage 3: Storyboard -> Panels
				if systemPrompt == pipeline.PromptStoryboardToPanels {
					return []byte(`{"project_id": "test-proj", "episode": 1, "panels": [{"scene_number": 1, "panel_number": 1, "description": "Mock Panel", "dialogue": "Hello Mock", "character_refs": [], "duration_sec": 3.0}]}`), nil
				}
				// Default catch-all
				return []byte(`{"status": "dry-run-ok"}`), nil
			},
		}, nil
	}

	switch provider {
	case "openai", "gemini":
		model := ""
		baseURL := ""
		apiKey := ""
		if cfg != nil {
			model = cfg.LLM.Model
			baseURL = cfg.LLM.BaseURL
			apiKey = cfg.LLM.APIKey
		}
		if model == "" {
			if provider == "gemini" {
				model = "gemini-2.5-pro"
			} else {
				model = "gpt-4o"
			}
		}
		return NewOpenAICompatibleClient(baseURL, apiKey, model), nil
	case "bedrock":
		return NewBedrockClient(
			cfg.LLM.AWSAccessKeyID,
			cfg.LLM.AWSSecretAccessKey,
			cfg.LLM.AWSRegion,
			cfg.LLM.Model,
		)
	default:	
		return nil, fmt.Errorf("provider %s not implemented yet. Use --dry-run for testing", provider)
	}
}
