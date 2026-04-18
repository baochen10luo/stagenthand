package image

import (
	"fmt"
	"strings"

	"github.com/baochen10luo/stagenthand/config"
	"github.com/baochen10luo/stagenthand/internal/render"
)

// NewClient acts as a factory for Image clients like NanoBanana.
func NewClient(provider string, dryRun bool, cfg *config.Config) (Client, error) {
	return NewClientWithFormat(provider, dryRun, cfg, render.VideoFormatLandscape)
}

// NewClientWithFormat acts as a factory for Image clients, accepting a VideoFormat
// to configure portrait or landscape image dimensions.
func NewClientWithFormat(provider string, dryRun bool, cfg *config.Config, format render.VideoFormat) (Client, error) {
	if dryRun || provider == "mock" {
		return &MockClient{}, nil
	}

	width, height := format.Dimensions()

	switch provider {
	case "nanobanana":
		// Defaults to Zeabur proxy per memory rules.
		// If Image API Config has a specific BaseURL we could pass it,
		// but for now NewNanoBananaClient uses a valid proxy default.
		return NewNanoBananaClient("", cfg.Image.APIKey, "nano-banana-2", width, height), nil
	case "bedrock":
		model := cfg.Image.Model
		if model == "" {
			model = "amazon.titan-image-generator-v2:0"
		}
		region := imageRegionForModel(cfg, model)
		switch {
		case strings.HasPrefix(model, "amazon.nova-canvas"):
			return NewNovaCanvasClient(
				cfg.LLM.AWSAccessKeyID,
				cfg.LLM.AWSSecretAccessKey,
				region,
				model,
				width,
				height,
				"",
			)
		case strings.HasPrefix(model, "amazon.titan-image-generator"):
			return NewTitanImageClient(
				cfg.LLM.AWSAccessKeyID,
				cfg.LLM.AWSSecretAccessKey,
				region,
				model,
				width,
				height,
			)
		default:
			return nil, fmt.Errorf("bedrock image model %s is not supported", model)
		}
	case "stability":
		model := cfg.Image.Model
		if model == "" {
			model = "stability.stable-image-ultra-v1:1"
		}
		return NewStabilityClient(
			cfg.LLM.AWSAccessKeyID,
			cfg.LLM.AWSSecretAccessKey,
			imageRegionForModel(cfg, model),
			model,
			width,
			height,
		)
	default:
		return nil, fmt.Errorf("provider %s not implemented yet. Use --dry-run for testing", provider)
	}
}

func imageRegionForModel(cfg *config.Config, model string) string {
	if cfg != nil && cfg.Image.Region != "" {
		return cfg.Image.Region
	}
	if cfg != nil && cfg.LLM.AWSRegion != "" {
		if strings.HasPrefix(model, "amazon.titan-image-generator") {
			return "us-west-2"
		}
		return cfg.LLM.AWSRegion
	}
	if strings.HasPrefix(model, "amazon.titan-image-generator") {
		return "us-west-2"
	}
	return "us-east-1"
}
