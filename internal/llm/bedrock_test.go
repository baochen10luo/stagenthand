package llm_test

import (
	"context"
	"errors"
	"testing"

	"github.com/aws/aws-sdk-go-v2/service/bedrockruntime"
	brtypes "github.com/aws/aws-sdk-go-v2/service/bedrockruntime/types"
	"github.com/baochen10luo/stagenthand/internal/llm"
	"github.com/stretchr/testify/assert"
)

// mockBedrockAPI is a test double that satisfies llm.BedrockAPI.
type mockBedrockAPI struct {
	ConverseFunc func(ctx context.Context, params *bedrockruntime.ConverseInput, optFns ...func(*bedrockruntime.Options)) (*bedrockruntime.ConverseOutput, error)
}

func (m *mockBedrockAPI) Converse(ctx context.Context, params *bedrockruntime.ConverseInput, optFns ...func(*bedrockruntime.Options)) (*bedrockruntime.ConverseOutput, error) {
	return m.ConverseFunc(ctx, params, optFns...)
}

func TestBedrockClient_GenerateTransformation(t *testing.T) {
	t.Parallel()

	t.Run("success", func(t *testing.T) {
		mock := &mockBedrockAPI{
			ConverseFunc: func(ctx context.Context, params *bedrockruntime.ConverseInput, optFns ...func(*bedrockruntime.Options)) (*bedrockruntime.ConverseOutput, error) {
				// Verify model ID is passed through
				assert.Equal(t, "amazon.nova-pro-v1:0", *params.ModelId)
				// Verify system prompt is set
				assert.Len(t, params.System, 1)
				// Verify user message is set
				assert.Len(t, params.Messages, 1)

				responseText := `{"outline": {"title": "test"}}`
				return &bedrockruntime.ConverseOutput{
					Output: &brtypes.ConverseOutputMemberMessage{
						Value: brtypes.Message{
							Role: brtypes.ConversationRoleAssistant,
							Content: []brtypes.ContentBlock{
								&brtypes.ContentBlockMemberText{
									Value: responseText,
								},
							},
						},
					},
				}, nil
			},
		}

		client := llm.NewBedrockClientWithAPI(mock, "amazon.nova-pro-v1:0")
		res, err := client.GenerateTransformation(context.Background(), "You are a director.", []byte("test input"))

		assert.NoError(t, err)
		assert.JSONEq(t, `{"outline": {"title": "test"}}`, string(res))
	})

	t.Run("api error", func(t *testing.T) {
		mock := &mockBedrockAPI{
			ConverseFunc: func(ctx context.Context, params *bedrockruntime.ConverseInput, optFns ...func(*bedrockruntime.Options)) (*bedrockruntime.ConverseOutput, error) {
				return nil, errors.New("ThrottlingException: rate exceeded")
			},
		}

		client := llm.NewBedrockClientWithAPI(mock, "amazon.nova-pro-v1:0")
		_, err := client.GenerateTransformation(context.Background(), "sys", []byte("in"))

		assert.ErrorContains(t, err, "bedrock converse failed")
		assert.ErrorContains(t, err, "ThrottlingException")
	})

	t.Run("empty response", func(t *testing.T) {
		mock := &mockBedrockAPI{
			ConverseFunc: func(ctx context.Context, params *bedrockruntime.ConverseInput, optFns ...func(*bedrockruntime.Options)) (*bedrockruntime.ConverseOutput, error) {
				return &bedrockruntime.ConverseOutput{
					Output: &brtypes.ConverseOutputMemberMessage{
						Value: brtypes.Message{
							Role:    brtypes.ConversationRoleAssistant,
							Content: []brtypes.ContentBlock{},
						},
					},
				}, nil
			},
		}

		client := llm.NewBedrockClientWithAPI(mock, "amazon.nova-pro-v1:0")
		_, err := client.GenerateTransformation(context.Background(), "sys", []byte("in"))

		assert.ErrorContains(t, err, "empty response")
	})

	t.Run("markdown strip", func(t *testing.T) {
		mock := &mockBedrockAPI{
			ConverseFunc: func(ctx context.Context, params *bedrockruntime.ConverseInput, optFns ...func(*bedrockruntime.Options)) (*bedrockruntime.ConverseOutput, error) {
				wrapped := "```json\n{\"clean\": true}\n```"
				return &bedrockruntime.ConverseOutput{
					Output: &brtypes.ConverseOutputMemberMessage{
						Value: brtypes.Message{
							Role: brtypes.ConversationRoleAssistant,
							Content: []brtypes.ContentBlock{
								&brtypes.ContentBlockMemberText{Value: wrapped},
							},
						},
					},
				}, nil
			},
		}

		client := llm.NewBedrockClientWithAPI(mock, "amazon.nova-pro-v1:0")
		res, err := client.GenerateTransformation(context.Background(), "sys", []byte("in"))

		assert.NoError(t, err)
		assert.JSONEq(t, `{"clean": true}`, string(res))
	})

	t.Run("nil output", func(t *testing.T) {
		mock := &mockBedrockAPI{
			ConverseFunc: func(ctx context.Context, params *bedrockruntime.ConverseInput, optFns ...func(*bedrockruntime.Options)) (*bedrockruntime.ConverseOutput, error) {
				return &bedrockruntime.ConverseOutput{Output: nil}, nil
			},
		}

		client := llm.NewBedrockClientWithAPI(mock, "amazon.nova-pro-v1:0")
		_, err := client.GenerateTransformation(context.Background(), "sys", []byte("in"))

		assert.ErrorContains(t, err, "unexpected output type")
	})
}

func TestNewBedrockClient_InvalidCreds(t *testing.T) {
	t.Parallel()

	_, err := llm.NewBedrockClient("", "secret", "us-east-1", "model")
	assert.ErrorContains(t, err, "aws_access_key_id is required")

	_, err = llm.NewBedrockClient("key", "", "us-east-1", "model")
	assert.ErrorContains(t, err, "aws_secret_access_key is required")
}

func TestNewBedrockClient_DefaultRegionAndModel(t *testing.T) {
	t.Parallel()

	// Should succeed and fill in default region + model without error.
	client, err := llm.NewBedrockClient("AKIATEST", "secretkey", "", "")
	assert.NoError(t, err)
	assert.NotNil(t, client)
}

func TestBedrockClient_ReviewVideo(t *testing.T) {
	t.Parallel()

	makeSuccessAPI := func(responseText string) *mockBedrockAPI {
		return &mockBedrockAPI{
			ConverseFunc: func(ctx context.Context, params *bedrockruntime.ConverseInput, optFns ...func(*bedrockruntime.Options)) (*bedrockruntime.ConverseOutput, error) {
				return &bedrockruntime.ConverseOutput{
					Output: &brtypes.ConverseOutputMemberMessage{
						Value: brtypes.Message{
							Role: brtypes.ConversationRoleAssistant,
							Content: []brtypes.ContentBlock{
								&brtypes.ContentBlockMemberText{Value: responseText},
							},
						},
					},
				}, nil
			},
		}
	}

	tests := []struct {
		name         string
		api          *mockBedrockAPI
		inputData    []byte
		videoFormat  string
		videoBytes   []byte
		wantErr      string
		wantContains string
	}{
		{
			name:         "success with input data",
			api:          makeSuccessAPI(`{"score": 9}`),
			inputData:    []byte("review this"),
			videoFormat:  "mp4",
			videoBytes:   []byte("fakevideobytes"),
			wantContains: `{"score": 9}`,
		},
		{
			name:         "success without input data",
			api:          makeSuccessAPI(`{"ok": true}`),
			inputData:    nil,
			videoFormat:  "mp4",
			videoBytes:   []byte("fakevideobytes"),
			wantContains: `{"ok": true}`,
		},
		{
			name: "api error",
			api: &mockBedrockAPI{
				ConverseFunc: func(ctx context.Context, params *bedrockruntime.ConverseInput, optFns ...func(*bedrockruntime.Options)) (*bedrockruntime.ConverseOutput, error) {
					return nil, errors.New("service unavailable")
				},
			},
			inputData:   []byte("review"),
			videoFormat: "mp4",
			videoBytes:  []byte("bytes"),
			wantErr:     "bedrock video converse failed",
		},
		{
			name: "nil output type",
			api: &mockBedrockAPI{
				ConverseFunc: func(ctx context.Context, params *bedrockruntime.ConverseInput, optFns ...func(*bedrockruntime.Options)) (*bedrockruntime.ConverseOutput, error) {
					return &bedrockruntime.ConverseOutput{Output: nil}, nil
				},
			},
			inputData:   []byte("review"),
			videoFormat: "mp4",
			videoBytes:  []byte("bytes"),
			wantErr:     "unexpected output type",
		},
		{
			name: "empty content blocks",
			api: &mockBedrockAPI{
				ConverseFunc: func(ctx context.Context, params *bedrockruntime.ConverseInput, optFns ...func(*bedrockruntime.Options)) (*bedrockruntime.ConverseOutput, error) {
					return &bedrockruntime.ConverseOutput{
						Output: &brtypes.ConverseOutputMemberMessage{
							Value: brtypes.Message{
								Role:    brtypes.ConversationRoleAssistant,
								Content: []brtypes.ContentBlock{},
							},
						},
					}, nil
				},
			},
			inputData:   []byte("review"),
			videoFormat: "mp4",
			videoBytes:  []byte("bytes"),
			wantErr:     "empty response content",
		},
		{
			name: "non-text content block",
			api: &mockBedrockAPI{
				ConverseFunc: func(ctx context.Context, params *bedrockruntime.ConverseInput, optFns ...func(*bedrockruntime.Options)) (*bedrockruntime.ConverseOutput, error) {
					return &bedrockruntime.ConverseOutput{
						Output: &brtypes.ConverseOutputMemberMessage{
							Value: brtypes.Message{
								Role: brtypes.ConversationRoleAssistant,
								// Return a video block instead of text — should trigger "not text" error.
								Content: []brtypes.ContentBlock{
									&brtypes.ContentBlockMemberVideo{
										Value: brtypes.VideoBlock{Format: brtypes.VideoFormatMp4},
									},
								},
							},
						},
					}, nil
				},
			},
			inputData:   []byte("review"),
			videoFormat: "mp4",
			videoBytes:  []byte("bytes"),
			wantErr:     "bedrock response content is not text",
		},
		{
			name:         "markdown json fence stripped",
			api:          makeSuccessAPI("```json\n{\"stripped\": true}\n```"),
			inputData:    []byte("review"),
			videoFormat:  "mp4",
			videoBytes:   []byte("bytes"),
			wantContains: `{"stripped": true}`,
		},
		{
			name:         "plain markdown fence stripped",
			api:          makeSuccessAPI("```\n{\"stripped\": true}\n```"),
			inputData:    []byte("review"),
			videoFormat:  "mp4",
			videoBytes:   []byte("bytes"),
			wantContains: `{"stripped": true}`,
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			client := llm.NewBedrockClientWithAPI(tc.api, "amazon.nova-pro-v1:0")
			res, err := client.ReviewVideo(context.Background(), "sys", tc.inputData, tc.videoFormat, tc.videoBytes)

			if tc.wantErr != "" {
				assert.ErrorContains(t, err, tc.wantErr)
				return
			}

			assert.NoError(t, err)
			assert.Contains(t, string(res), tc.wantContains)
		})
	}
}

// TestBedrockClient_GenerateTransformation_NonTextBlock tests the branch where
// the first content block returned is not a text block.
func TestBedrockClient_GenerateTransformation_NonTextBlock(t *testing.T) {
	t.Parallel()

	mock := &mockBedrockAPI{
		ConverseFunc: func(ctx context.Context, params *bedrockruntime.ConverseInput, optFns ...func(*bedrockruntime.Options)) (*bedrockruntime.ConverseOutput, error) {
			return &bedrockruntime.ConverseOutput{
				Output: &brtypes.ConverseOutputMemberMessage{
					Value: brtypes.Message{
						Role: brtypes.ConversationRoleAssistant,
						Content: []brtypes.ContentBlock{
							// Return a non-text block to exercise "not text" error path.
							&brtypes.ContentBlockMemberVideo{
								Value: brtypes.VideoBlock{Format: brtypes.VideoFormatMp4},
							},
						},
					},
				},
			}, nil
		},
	}

	client := llm.NewBedrockClientWithAPI(mock, "amazon.nova-pro-v1:0")
	_, err := client.GenerateTransformation(context.Background(), "sys", []byte("in"))
	assert.ErrorContains(t, err, "bedrock response content is not text")
}

// TestBedrockClient_GenerateTransformation_MarkdownStrip_Plain tests stripping "```" prefix.
func TestBedrockClient_GenerateTransformation_MarkdownStrip_Plain(t *testing.T) {
	t.Parallel()

	mock := &mockBedrockAPI{
		ConverseFunc: func(ctx context.Context, params *bedrockruntime.ConverseInput, optFns ...func(*bedrockruntime.Options)) (*bedrockruntime.ConverseOutput, error) {
			return &bedrockruntime.ConverseOutput{
				Output: &brtypes.ConverseOutputMemberMessage{
					Value: brtypes.Message{
						Role: brtypes.ConversationRoleAssistant,
						Content: []brtypes.ContentBlock{
							&brtypes.ContentBlockMemberText{Value: "```\n{\"plain\": true}\n```"},
						},
					},
				},
			}, nil
		},
	}

	client := llm.NewBedrockClientWithAPI(mock, "amazon.nova-pro-v1:0")
	res, err := client.GenerateTransformation(context.Background(), "sys", []byte("in"))
	assert.NoError(t, err)
	assert.JSONEq(t, `{"plain": true}`, string(res))
}
