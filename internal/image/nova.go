package image

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/bedrockruntime"
)

type bedrockInvoker interface {
	InvokeModel(ctx context.Context, params *bedrockruntime.InvokeModelInput, optFns ...func(*bedrockruntime.Options)) (*bedrockruntime.InvokeModelOutput, error)
}

// NovaCanvasClient implements the image.Client interface using AWS Bedrock Nova Canvas.
type NovaCanvasClient struct {
	client bedrockInvoker
	model  string
	width  int
	height int
	refDir string
}

// NewNovaCanvasClient initializes an AWS Bedrock Runtime client.
func NewNovaCanvasClient(accessKey, secretKey, region, model string, width, height int, refDir string) (*NovaCanvasClient, error) {
	if region == "" {
		region = "us-east-1"
	}
	if model == "" {
		model = "amazon.nova-canvas-v1:0"
	}
	if width == 0 {
		width = 1024
	}
	if height == 0 {
		height = 576
	}

	cfg, err := config.LoadDefaultConfig(context.Background(),
		config.WithRegion(region),
		config.WithCredentialsProvider(credentials.NewStaticCredentialsProvider(accessKey, secretKey, "")),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to load AWS config: %w", err)
	}

	return &NovaCanvasClient{
		client: bedrockruntime.NewFromConfig(cfg),
		model:  model,
		width:  width,
		height: height,
		refDir: refDir,
	}, nil
}

// GenerateImage sends a prompt to Nova Canvas and returns the generated image bytes.
func (c *NovaCanvasClient) GenerateImage(ctx context.Context, prompt string, characterRefs []string) ([]byte, error) {
	// Nova Canvas Text-To-Image Body
	type TextToImageParams struct {
		Text string `json:"text"`
	}

	type ImageGenerationConfig struct {
		NumberOfImages int     `json:"numberOfImages"`
		Height         int     `json:"height"`
		Width          int     `json:"width"`
		CfgScale       float64 `json:"cfgScale,omitempty"`
	}

	type NovaCanvasRequest struct {
		TaskType              string                `json:"taskType"`
		TextToImageParams     TextToImageParams     `json:"textToImageParams"`
		ImageGenerationConfig ImageGenerationConfig `json:"imageGenerationConfig"`
	}

	// For now, shand simple implementation doesn't pass image conditioning
	// until we define a clearer schema for multi-image prompts in shand.
	// We use the provided width/height from config.
	input := NovaCanvasRequest{
		TaskType: "TEXT_IMAGE",
		TextToImageParams: TextToImageParams{
			Text: prompt,
		},
		ImageGenerationConfig: ImageGenerationConfig{
			NumberOfImages: 1,
			Height:         c.height,
			Width:          c.width,
			CfgScale:       7.0, // Default balanced scale
		},
	}

	body, err := json.Marshal(input)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	resp, err := c.client.InvokeModel(ctx, &bedrockruntime.InvokeModelInput{
		ModelId:     aws.String(c.model),
		Body:        body,
		ContentType: aws.String("application/json"),
		Accept:      aws.String("application/json"),
	})
	if err != nil {
		return nil, fmt.Errorf("bedrock invoke failed: %w", err)
	}

	type NovaCanvasResponse struct {
		Images []string `json:"images"`
	}

	var res NovaCanvasResponse
	if err := json.Unmarshal(resp.Body, &res); err != nil {
		return nil, fmt.Errorf("failed to unmarshal response: %w", err)
	}

	if len(res.Images) == 0 {
		return nil, fmt.Errorf("no images returned from Nova Canvas")
	}

	imgBytes, err := base64.StdEncoding.DecodeString(res.Images[0])
	if err != nil {
		return nil, fmt.Errorf("failed to decode image: %w", err)
	}

	return imgBytes, nil
}
