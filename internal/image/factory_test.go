package image_test

import (
	"testing"

	"github.com/baochen10luo/stagenthand/config"
	"github.com/baochen10luo/stagenthand/internal/image"
	"github.com/baochen10luo/stagenthand/internal/render"
	"github.com/stretchr/testify/assert"
)

func TestNewClient(t *testing.T) {
	t.Parallel()

	cfg := &config.Config{
		Image: config.ImageConfig{
			APIKey: "test",
		},
	}

	t.Run("dry run", func(t *testing.T) {
		client, err := image.NewClient("nanobanana", true, cfg)
		assert.NoError(t, err)
		_, ok := client.(*image.MockClient)
		assert.True(t, ok)
	})

	t.Run("mock provider", func(t *testing.T) {
		client, err := image.NewClient("mock", false, cfg)
		assert.NoError(t, err)
		_, ok := client.(*image.MockClient)
		assert.True(t, ok)
	})

	t.Run("nanobanana provider", func(t *testing.T) {
		client, err := image.NewClient("nanobanana", false, cfg)
		assert.NoError(t, err)
		_, ok := client.(*image.NanoBananaClient)
		assert.True(t, ok)
	})

	t.Run("unknown provider", func(t *testing.T) {
		client, err := image.NewClient("unknown", false, cfg)
		assert.ErrorContains(t, err, "not implemented")
		assert.Nil(t, client)
	})
}

func TestNewClientWithFormat(t *testing.T) {
	t.Parallel()

	cfg := &config.Config{
		Image: config.ImageConfig{
			APIKey: "test",
		},
	}

	t.Run("portrait format returns nanobanana client", func(t *testing.T) {
		client, err := image.NewClientWithFormat("nanobanana", false, cfg, render.VideoFormatPortrait)
		assert.NoError(t, err)
		_, ok := client.(*image.NanoBananaClient)
		assert.True(t, ok)
	})

	t.Run("landscape format dry run returns mock", func(t *testing.T) {
		client, err := image.NewClientWithFormat("nanobanana", true, cfg, render.VideoFormatLandscape)
		assert.NoError(t, err)
		_, ok := client.(*image.MockClient)
		assert.True(t, ok)
	})

	t.Run("bedrock defaults to titan in us-west-2", func(t *testing.T) {
		cfg := &config.Config{
			LLM: config.LLMConfig{
				AWSAccessKeyID:     "ak",
				AWSSecretAccessKey: "sk",
				AWSRegion:          "us-east-1",
			},
		}

		client, err := image.NewClientWithFormat("bedrock", false, cfg, render.VideoFormatLandscape)
		assert.NoError(t, err)

		titan, ok := client.(*image.TitanImageClient)
		assert.True(t, ok)
		assert.Equal(t, "amazon.titan-image-generator-v2:0", titan.Model())
		assert.Equal(t, "us-west-2", titan.Region())
	})

	t.Run("bedrock honors image model and image region overrides", func(t *testing.T) {
		cfg := &config.Config{
			LLM: config.LLMConfig{
				AWSAccessKeyID:     "ak",
				AWSSecretAccessKey: "sk",
				AWSRegion:          "us-east-1",
			},
			Image: config.ImageConfig{
				Model:  "amazon.nova-canvas-v1:0",
				Region: "us-east-2",
			},
		}

		client, err := image.NewClientWithFormat("bedrock", false, cfg, render.VideoFormatPortrait)
		assert.NoError(t, err)

		nova, ok := client.(*image.NovaCanvasClient)
		assert.True(t, ok)
		assert.Equal(t, "amazon.nova-canvas-v1:0", nova.Model())
		assert.Equal(t, "us-east-2", nova.Region())
	})

	t.Run("stability honors image model and image region overrides", func(t *testing.T) {
		cfg := &config.Config{
			LLM: config.LLMConfig{
				AWSAccessKeyID:     "ak",
				AWSSecretAccessKey: "sk",
				AWSRegion:          "us-east-1",
			},
			Image: config.ImageConfig{
				Model:  "stability.stable-image-ultra-v1:1",
				Region: "us-west-2",
			},
		}

		client, err := image.NewClientWithFormat("stability", false, cfg, render.VideoFormatPortrait)
		assert.NoError(t, err)

		stability, ok := client.(*image.StabilityClient)
		assert.True(t, ok)
		assert.Equal(t, "stability.stable-image-ultra-v1:1", stability.Model())
		assert.Equal(t, "us-west-2", stability.Region())
	})
}
