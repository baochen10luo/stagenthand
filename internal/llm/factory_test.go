package llm_test

import (
	"context"
	"testing"

	"github.com/baochen10luo/stagenthand/config"
	"github.com/baochen10luo/stagenthand/internal/llm"
	"github.com/baochen10luo/stagenthand/internal/pipeline"
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
		_, ok := client.(*llm.OpenAICompatibleClient)
		assert.True(t, ok)
	})

	t.Run("openai provider", func(t *testing.T) {
		client, err := llm.NewClient("openai", false, cfg)
		assert.NoError(t, err)
		_, ok := client.(*llm.OpenAICompatibleClient) // maps to OpenAICompatible internally
		assert.True(t, ok)
	})

	t.Run("unknown provider", func(t *testing.T) {
		client, err := llm.NewClient("unknown", false, nil)
		assert.ErrorContains(t, err, "not implemented")
		assert.Nil(t, client)
	})

	t.Run("openai with nil config uses default model", func(t *testing.T) {
		// When cfg is nil, the factory should still fall through to default model "gpt-4o".
		client, err := llm.NewClient("openai", false, nil)
		assert.NoError(t, err)
		_, ok := client.(*llm.OpenAICompatibleClient)
		assert.True(t, ok)
	})

	t.Run("gemini with nil config uses default model", func(t *testing.T) {
		client, err := llm.NewClient("gemini", false, nil)
		assert.NoError(t, err)
		_, ok := client.(*llm.OpenAICompatibleClient)
		assert.True(t, ok)
	})

	t.Run("bedrock provider with valid creds", func(t *testing.T) {
		bedrockCfg := &config.Config{
			LLM: config.LLMConfig{
				AWSAccessKeyID:     "AKIATEST",
				AWSSecretAccessKey: "secretkey",
				AWSRegion:          "us-east-1",
				Model:              "amazon.nova-pro-v1:0",
			},
		}
		client, err := llm.NewClient("bedrock", false, bedrockCfg)
		assert.NoError(t, err)
		assert.NotNil(t, client)
	})

	t.Run("bedrock provider missing access key returns error", func(t *testing.T) {
		bedrockCfg := &config.Config{
			LLM: config.LLMConfig{
				AWSSecretAccessKey: "secretkey",
				AWSRegion:          "us-east-1",
			},
		}
		client, err := llm.NewClient("bedrock", false, bedrockCfg)
		assert.ErrorContains(t, err, "aws_access_key_id is required")
		assert.Nil(t, client)
	})

	t.Run("bedrock provider missing secret key returns error", func(t *testing.T) {
		bedrockCfg := &config.Config{
			LLM: config.LLMConfig{
				AWSAccessKeyID: "AKIATEST",
				AWSRegion:      "us-east-1",
			},
		}
		client, err := llm.NewClient("bedrock", false, bedrockCfg)
		assert.ErrorContains(t, err, "aws_secret_access_key is required")
		assert.Nil(t, client)
	})
}

// TestNewClient_MockDryRunBehavior checks the mock client returned by dry-run
// responds to all known pipeline prompts correctly (table-driven).
func TestNewClient_MockDryRunBehavior(t *testing.T) {
	t.Parallel()

	ctx := context.Background()

	client, err := llm.NewClient("mock", false, nil)
	assert.NoError(t, err)

	tests := []struct {
		name          string
		systemPrompt  string
		wantSubstring string
	}{
		{
			name:          "PromptStoryToOutline returns project_id field",
			systemPrompt:  pipeline.PromptStoryToOutline,
			wantSubstring: "project_id",
		},
		{
			name:          "PromptOutlineToStoryboard returns scenes field",
			systemPrompt:  pipeline.PromptOutlineToStoryboard,
			wantSubstring: "scenes",
		},
		{
			name:          "PromptStoryboardToPanels returns panels field",
			systemPrompt:  pipeline.PromptStoryboardToPanels,
			wantSubstring: "panels",
		},
		{
			name:          "unknown prompt returns dry-run-ok default",
			systemPrompt:  "some random prompt",
			wantSubstring: "dry-run-ok",
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			res, err := client.GenerateTransformation(ctx, tc.systemPrompt, []byte("test input"))
			assert.NoError(t, err)
			assert.Contains(t, string(res), tc.wantSubstring)
		})
	}
}

// TestNewClient_DryRunFlag verifies that dryRun=true always returns MockClient
// regardless of the provider name.
func TestNewClient_DryRunFlag(t *testing.T) {
	t.Parallel()

	providers := []string{"openai", "gemini", "bedrock", "unknown", "nova"}
	for _, p := range providers {
		p := p
		t.Run("dryRun with provider "+p, func(t *testing.T) {
			t.Parallel()
			client, err := llm.NewClient(p, true, nil)
			assert.NoError(t, err)
			_, ok := client.(*llm.MockClient)
			assert.True(t, ok, "expected MockClient for provider %q with dryRun=true", p)
		})
	}
}
