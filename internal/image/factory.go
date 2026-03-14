package image

import (
	"fmt"
	"github.com/baochen10luo/stagenthand/config"
)

// NewClient acts as a factory for Image clients like NanoBanana.
func NewClient(provider string, dryRun bool, cfg *config.Config) (Client, error) {
	if dryRun || provider == "mock" {
		return &MockClient{}, nil
	}

	switch provider {
	case "nanobanana":
		// Defaults to Zeabur proxy per memory rules. 
		// If Image API Config has a specific BaseURL we could pass it,
		// but for now NewNanoBananaClient uses a valid proxy default.
		return NewNanoBananaClient("", cfg.Image.APIKey, "nano-banana-2"), nil
	case "bedrock":
		return NewNovaCanvasClient(
			cfg.LLM.AWSAccessKeyID,
			cfg.LLM.AWSSecretAccessKey,
			cfg.LLM.AWSRegion,
			"amazon.nova-canvas-v1:0",
			cfg.Image.Width,
			cfg.Image.Height,
			"",
		)
	default:	
		return nil, fmt.Errorf("provider %s not implemented yet. Use --dry-run for testing", provider)
	}
}
