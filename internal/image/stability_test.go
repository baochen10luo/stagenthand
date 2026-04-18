package image

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/bedrockruntime"
)

type mockStabilityInvoker struct {
	t          *testing.T
	wantModel  string
	wantAccept string
	wantPrompt string
	wantAspect string
	output     *bedrockruntime.InvokeModelOutput
	err        error
}

func (m *mockStabilityInvoker) InvokeModel(ctx context.Context, params *bedrockruntime.InvokeModelInput, optFns ...func(*bedrockruntime.Options)) (*bedrockruntime.InvokeModelOutput, error) {
	if m.wantModel != "" && aws.ToString(params.ModelId) != m.wantModel {
		m.t.Fatalf("model id = %q, want %q", aws.ToString(params.ModelId), m.wantModel)
	}
	if m.wantAccept != "" && aws.ToString(params.Accept) != m.wantAccept {
		m.t.Fatalf("accept = %q, want %q", aws.ToString(params.Accept), m.wantAccept)
	}
	if m.wantPrompt != "" || m.wantAspect != "" {
		var body struct {
			Prompt       string `json:"prompt"`
			AspectRatio  string `json:"aspect_ratio"`
			Mode         string `json:"mode"`
			OutputFormat string `json:"output_format"`
		}
		if err := json.Unmarshal(params.Body, &body); err != nil {
			m.t.Fatalf("unmarshal request body: %v", err)
		}
		if body.Prompt != m.wantPrompt {
			m.t.Fatalf("prompt = %q, want %q", body.Prompt, m.wantPrompt)
		}
		if body.AspectRatio != m.wantAspect {
			m.t.Fatalf("aspect_ratio = %q, want %q", body.AspectRatio, m.wantAspect)
		}
		if body.Mode != "text-to-image" {
			m.t.Fatalf("mode = %q, want text-to-image", body.Mode)
		}
		if body.OutputFormat != "png" {
			m.t.Fatalf("output_format = %q, want png", body.OutputFormat)
		}
	}

	return m.output, m.err
}

func TestStabilityClientGenerateImage(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		client  *StabilityClient
		prompt  string
		want    []byte
		wantErr string
	}{
		{
			name: "success returns raw png bytes",
			client: &StabilityClient{
				client: &mockStabilityInvoker{
					t:          t,
					wantModel:  "stability.stable-image-ultra-v1:1",
					wantAccept: "application/json",
					wantPrompt: "dramatic devotional portrait",
					wantAspect: "9:16",
					output: &bedrockruntime.InvokeModelOutput{
						Body: func() []byte {
							b, _ := json.Marshal(map[string][]string{
								"images": {base64.StdEncoding.EncodeToString([]byte{0x89, 0x50, 0x4e, 0x47})},
							})
							return b
						}(),
					},
				},
				model:       "stability.stable-image-ultra-v1:1",
				aspectRatio: "9:16",
			},
			prompt: "dramatic devotional portrait",
			want:   []byte{0x89, 0x50, 0x4e, 0x47},
		},
		{
			name: "invoke failure surfaces error",
			client: &StabilityClient{
				client: &mockStabilityInvoker{
					t:   t,
					err: errors.New("bedrock denied"),
				},
				model:       "stability.stable-image-ultra-v1:1",
				aspectRatio: "9:16",
			},
			prompt:  "dramatic devotional portrait",
			wantErr: "bedrock invoke failed",
		},
		{
			name: "empty body returns error",
			client: &StabilityClient{
				client: &mockStabilityInvoker{
					t:      t,
					output: &bedrockruntime.InvokeModelOutput{Body: []byte{}},
				},
				model:       "stability.stable-image-ultra-v1:1",
				aspectRatio: "9:16",
			},
			prompt:  "dramatic devotional portrait",
			wantErr: "no image bytes returned from Stability",
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

func TestAspectRatioForDimensions(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		width  int
		height int
		want   string
	}{
		{name: "portrait", width: 720, height: 1280, want: "9:16"},
		{name: "square", width: 1024, height: 1024, want: "1:1"},
		{name: "landscape default", width: 1280, height: 720, want: "16:9"},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := aspectRatioForDimensions(tt.width, tt.height); got != tt.want {
				t.Fatalf("aspectRatioForDimensions(%d, %d) = %q, want %q", tt.width, tt.height, got, tt.want)
			}
		})
	}
}
