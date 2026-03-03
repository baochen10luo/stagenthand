package llm_test

import (
	"testing"

	"github.com/baochen10luo/stagenthand/config"
	"github.com/baochen10luo/stagenthand/internal/llm"
	"github.com/stretchr/testify/assert"
)

func TestNewClient(t *testing.T) {
	t.Parallel()

	cfg := &config.Config{
		LLM: config.LLMConfig{
			APIKey: "test",
		},
	}

	t.Run("dry run", func(t *testing.T) {
		client, err := llm.NewClient("gemini", true, cfg)
		assert.NoError(t, err)
		_, ok := client.(*llm.MockClient)
		assert.True(t, ok)
	})

	t.Run("mock provider", func(t *testing.T) {
		client, err := llm.NewClient("mock", false, cfg)
		assert.NoError(t, err)
		_, ok := client.(*llm.MockClient)
		assert.True(t, ok)
	})

	t.Run("gemini provider", func(t *testing.T) {
		client, err := llm.NewClient("gemini", false, cfg)
		assert.NoError(t, err)
		_, ok := client.(*llm.GeminiClient)
		assert.True(t, ok)
	})

	t.Run("openai provider", func(t *testing.T) {
		client, err := llm.NewClient("openai", false, cfg)
		assert.NoError(t, err)
		_, ok := client.(*llm.GeminiClient) // maps to GeminiClient internally
		assert.True(t, ok)
	})

	t.Run("unknown provider", func(t *testing.T) {
		client, err := llm.NewClient("unknown", false, nil)
		assert.ErrorContains(t, err, "not implemented")
		assert.Nil(t, client)
	})
}
