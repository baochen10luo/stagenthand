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
		return NewNanoBananaClient("", cfg.Image.APIKey, "nano-banana-2"), nil
	case "nova":
		return NewNovaCanvasClient(
			cfg.Image.APIKey,
			cfg.Image.SecretKey,
			cfg.Image.Region,
			cfg.Image.Model,
			cfg.Image.Width,
			cfg.Image.Height,
			cfg.Image.CharacterRefsDir,
		)
	default:	
		return nil, fmt.Errorf("provider %s not implemented yet. Use --dry-run for testing", provider)
	}
}
