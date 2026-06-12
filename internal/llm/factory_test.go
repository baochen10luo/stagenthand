package llm_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/baochen10luo/stagenthand/config"
	xauth "github.com/baochen10luo/stagenthand/internal/auth/xai"
	"github.com/baochen10luo/stagenthand/internal/llm"
	"github.com/baochen10luo/stagenthand/internal/pipeline"
	"github.com/baochen10luo/stagenthand/internal/xaipipeline"
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
			AWS: config.AWSConfig{
				AccessKeyID:     "AKIATEST",
				SecretAccessKey: "secretkey",
				Region:          "us-east-1",
			},
			LLM: config.LLMConfig{
				Model: "amazon.nova-pro-v1:0",
			},
		}
		client, err := llm.NewClient("bedrock", false, bedrockCfg)
		assert.NoError(t, err)
		assert.NotNil(t, client)
	})

	t.Run("bedrock provider missing access key returns error", func(t *testing.T) {
		bedrockCfg := &config.Config{
			AWS: config.AWSConfig{
				SecretAccessKey: "secretkey",
				Region:          "us-east-1",
			},
		}
		client, err := llm.NewClient("bedrock", false, bedrockCfg)
		assert.ErrorContains(t, err, "aws_access_key_id is required")
		assert.Nil(t, client)
	})

	t.Run("bedrock provider missing secret key returns error", func(t *testing.T) {
		bedrockCfg := &config.Config{
			AWS: config.AWSConfig{
				AccessKeyID: "AKIATEST",
				Region:      "us-east-1",
			},
		}
		client, err := llm.NewClient("bedrock", false, bedrockCfg)
		assert.ErrorContains(t, err, "aws_secret_access_key is required")
		assert.Nil(t, client)
	})

	t.Run("xai oauth provider", func(t *testing.T) {
		xaiCfg := &config.Config{
			LLM: config.LLMConfig{
				Provider: "xai-oauth",
			},
			XAI: config.XAIConfig{
				Model:     "grok-4.3",
				BaseURL:   "https://api.x.ai/v1",
				TokenPath: "/tmp/xai-auth.json",
			},
		}
		client, err := llm.NewClient("xai-oauth", false, xaiCfg)
		assert.NoError(t, err)
		_, ok := client.(*llm.XAIOAuthClient)
		assert.True(t, ok)
	})
}

func TestNewClient_XAIOAuthUsesProviderSpecificModel(t *testing.T) {
	t.Parallel()

	tokenPath := filepath.Join(t.TempDir(), "xai.json")
	store := xauth.NewFileTokenStore(tokenPath)
	assert.NoError(t, store.Save(xauth.Token{
		AccessToken: "oauth-access",
		TokenType:   "Bearer",
	}))

	var capturedModel string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "Bearer oauth-access", r.Header.Get("Authorization"))
		var body map[string]any
		assert.NoError(t, json.NewDecoder(r.Body).Decode(&body))
		capturedModel, _ = body["model"].(string)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"output_text": `{"ok":true}`,
		})
	}))
	defer server.Close()

	cfg := &config.Config{
		LLM: config.LLMConfig{
			Model: "aiark/qwen36-35b-a3b",
		},
		XAI: config.XAIConfig{
			Model:     "grok-4.3",
			BaseURL:   server.URL + "/v1",
			TokenPath: tokenPath,
		},
	}
	client, err := llm.NewClient("xai-oauth", false, cfg)
	assert.NoError(t, err)

	got, err := client.GenerateTransformation(t.Context(), "Return JSON only.", []byte(`{}`))
	assert.NoError(t, err)
	assert.JSONEq(t, `{"ok":true}`, string(got))
	assert.Equal(t, "grok-4.3", capturedModel)
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
			name:          "PromptStoryToXAIManifest returns shots field",
			systemPrompt:  xaipipeline.PromptStoryToXAIManifest,
			wantSubstring: "shots",
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

func TestNewClient_MockDryRunXAIManifestHonorsTargetShots(t *testing.T) {
	t.Parallel()

	client, err := llm.NewClient("xai-oauth", true, nil)
	assert.NoError(t, err)

	res, err := client.GenerateTransformation(context.Background(), xaipipeline.PromptStoryToXAIManifest, []byte(`{
		"story": "機器人找到花",
		"target_shots": 3,
		"format": "portrait"
	}`))
	assert.NoError(t, err)

	var manifest xaipipeline.Manifest
	assert.NoError(t, json.Unmarshal(res, &manifest))
	if assert.Len(t, manifest.Shots, 3) {
		for i, shot := range manifest.Shots {
			assert.Equal(t, i+1, shot.Index)
			assert.NotEmpty(t, shot.Prompt)
			assert.Equal(t, "9:16", shot.AspectRatio)
			assert.Equal(t, "720p", shot.Resolution)
		}
	}
}

// TestNewClient_DryRunFlag verifies that dryRun=true always returns MockClient
// regardless of the provider name.
func TestNewClient_DryRunFlag(t *testing.T) {
	t.Parallel()

	providers := []string{"openai", "gemini", "bedrock", "unknown", "nova", "xai-oauth"}
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
