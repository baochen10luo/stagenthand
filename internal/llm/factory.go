package llm

import (
	"context"
	"fmt"
	"os"

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
					return []byte(`{"project_id": "robot-flower", "episode": 1, "scenes": [
						{"number": 1, "description": "廢墟中的機器人"},
						{"number": 2, "description": "發光的小花"},
						{"number": 3, "description": "夕陽下的依偎"}
					]}`), nil
				}
				// Stage 3: Storyboard -> Panels
				if systemPrompt == pipeline.PromptStoryboardToPanels {
					return []byte(`{"project_id": "robot-flower", "episode": 1, "panels": [
						{"scene_number": 1, "panel_number": 1, "description": "生鏽的機器人在鋼鐵廢墟中漫步", "dialogue": "今天也是安靜的一天...", "character_refs": [], "duration_sec": 3.0},
						{"scene_number": 2, "panel_number": 1, "description": "瓦礫堆中一朵發光的小花", "dialogue": "那是...什麼？", "character_refs": [], "duration_sec": 4.0},
						{"scene_number": 3, "panel_number": 1, "description": "機器人捧著小花，背景是巨大的夕陽", "dialogue": "真美。", "character_refs": [], "duration_sec": 3.5}
					]}`), nil
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
	case "anthropic":
		model := "claude-sonnet-4-6"
		apiKey := ""
		if cfg != nil {
			if cfg.LLM.Model != "" {
				model = cfg.LLM.Model
			}
			apiKey = cfg.LLM.APIKey
		}
		// Fall back to ANTHROPIC_API_KEY env var if not in config
		if apiKey == "" {
			apiKey = os.Getenv("ANTHROPIC_API_KEY")
		}
		return NewOpenAICompatibleClientWithHeaders(
			"https://api.anthropic.com/v1",
			apiKey,
			model,
			map[string]string{"anthropic-version": "2023-06-01"},
		), nil
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
