package image

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/bedrockruntime"
)

type mockTitanInvoker struct {
	t          *testing.T
	wantModel  string
	wantPrompt string
	wantWidth  int
	wantHeight int
	output     *bedrockruntime.InvokeModelOutput
	err        error
}

func (m *mockTitanInvoker) InvokeModel(ctx context.Context, params *bedrockruntime.InvokeModelInput, optFns ...func(*bedrockruntime.Options)) (*bedrockruntime.InvokeModelOutput, error) {
	if m.wantModel != "" && aws.ToString(params.ModelId) != m.wantModel {
		m.t.Fatalf("model id = %q, want %q", aws.ToString(params.ModelId), m.wantModel)
	}

	if m.wantPrompt != "" {
		var body struct {
			TaskType          string `json:"taskType"`
			TextToImageParams struct {
				Text string `json:"text"`
			} `json:"textToImageParams"`
			ImageGenerationConfig struct {
				Quality        string  `json:"quality"`
				NumberOfImages int     `json:"numberOfImages"`
				Height         int     `json:"height"`
				Width          int     `json:"width"`
				CfgScale       float64 `json:"cfgScale"`
			} `json:"imageGenerationConfig"`
		}
		if err := json.Unmarshal(params.Body, &body); err != nil {
			m.t.Fatalf("unmarshal request body: %v", err)
		}
		if body.TaskType != "TEXT_IMAGE" {
			m.t.Fatalf("taskType = %q, want TEXT_IMAGE", body.TaskType)
		}
		if body.TextToImageParams.Text != m.wantPrompt {
			m.t.Fatalf("text = %q, want %q", body.TextToImageParams.Text, m.wantPrompt)
		}
		if body.ImageGenerationConfig.Quality != "standard" {
			m.t.Fatalf("quality = %q, want standard", body.ImageGenerationConfig.Quality)
		}
		if body.ImageGenerationConfig.NumberOfImages != 1 {
			m.t.Fatalf("numberOfImages = %d, want 1", body.ImageGenerationConfig.NumberOfImages)
		}
		if body.ImageGenerationConfig.Width != m.wantWidth {
			m.t.Fatalf("width = %d, want %d", body.ImageGenerationConfig.Width, m.wantWidth)
		}
		if body.ImageGenerationConfig.Height != m.wantHeight {
			m.t.Fatalf("height = %d, want %d", body.ImageGenerationConfig.Height, m.wantHeight)
		}
	}

	return m.output, m.err
}

func TestTitanImageClientGenerateImage(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		client  *TitanImageClient
		prompt  string
		want    []byte
		wantErr string
	}{
		{
			name: "success returns decoded image bytes",
			client: &TitanImageClient{
				client: &mockTitanInvoker{
					t:          t,
					wantModel:  "amazon.titan-image-generator-v2:0",
					wantPrompt: "cinematic temple courtyard at sunrise",
					wantWidth:  1024,
					wantHeight: 576,
					output: &bedrockruntime.InvokeModelOutput{
						Body: []byte(`{"images":["YmFzZTY0aW1hZ2VkYXRh"]}`),
					},
				},
				model:  "amazon.titan-image-generator-v2:0",
				region: "us-west-2",
				width:  1024,
				height: 576,
			},
			prompt: "cinematic temple courtyard at sunrise",
			want:   []byte("base64imagedata"),
		},
		{
			name: "invoke failure surfaces error",
			client: &TitanImageClient{
				client: &mockTitanInvoker{
					t:   t,
					err: errors.New("access denied"),
				},
				model:  "amazon.titan-image-generator-v2:0",
				region: "us-west-2",
				width:  1024,
				height: 576,
			},
			prompt:  "cinematic temple courtyard at sunrise",
			wantErr: "bedrock invoke failed",
		},
		{
			name: "service error is surfaced",
			client: &TitanImageClient{
				client: &mockTitanInvoker{
					t: t,
					output: &bedrockruntime.InvokeModelOutput{
						Body: []byte(`{"error":"quota exceeded"}`),
					},
				},
				model:  "amazon.titan-image-generator-v2:0",
				region: "us-west-2",
				width:  1024,
				height: 576,
			},
			prompt:  "cinematic temple courtyard at sunrise",
			wantErr: "titan image generation failed",
		},
		{
			name: "empty images returns error",
			client: &TitanImageClient{
				client: &mockTitanInvoker{
					t: t,
					output: &bedrockruntime.InvokeModelOutput{
						Body: []byte(`{"images":[]}`),
					},
				},
				model:  "amazon.titan-image-generator-v2:0",
				region: "us-west-2",
				width:  1024,
				height: 576,
			},
			prompt:  "cinematic temple courtyard at sunrise",
			wantErr: "no images returned from Titan Image Generator",
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, err := tt.client.GenerateImage(context.Background(), tt.prompt, nil)
			if tt.wantErr != "" {
				if err == nil {
					t.Fatalf("expected error containing %q, got nil", tt.wantErr)
				}
				if !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("error = %q, want substring %q", err.Error(), tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("GenerateImage() error = %v", err)
			}
			if string(got) != string(tt.want) {
				t.Fatalf("GenerateImage() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestNewTitanImageClientDefaults(t *testing.T) {
	t.Parallel()

	client, err := NewTitanImageClient("test_ak", "test_sk", "", "", 0, 0)
	if err != nil {
		t.Fatalf("NewTitanImageClient() error = %v", err)
	}
	if client.Model() != "amazon.titan-image-generator-v2:0" {
		t.Fatalf("Model() = %q, want %q", client.Model(), "amazon.titan-image-generator-v2:0")
	}
	if client.Region() != "us-west-2" {
		t.Fatalf("Region() = %q, want %q", client.Region(), "us-west-2")
	}
}
