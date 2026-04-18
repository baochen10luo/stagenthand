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

// StabilityClient implements the image.Client interface using Stability AI on AWS Bedrock.
type StabilityClient struct {
	client      bedrockInvoker
	model       string
	region      string
	aspectRatio string
}

// NewStabilityClient initializes an AWS Bedrock Runtime client for Stability image generation.
func NewStabilityClient(accessKey, secretKey, region, model string, width, height int) (*StabilityClient, error) {
	if region == "" {
		region = "us-east-1"
	}
	if model == "" {
		model = "stability.stable-image-ultra-v1:1"
	}

	cfg, err := config.LoadDefaultConfig(context.Background(),
		config.WithRegion(region),
		config.WithCredentialsProvider(credentials.NewStaticCredentialsProvider(accessKey, secretKey, "")),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to load AWS config: %w", err)
	}

	return &StabilityClient{
		client:      bedrockruntime.NewFromConfig(cfg),
		model:       model,
		region:      region,
		aspectRatio: aspectRatioForDimensions(width, height),
	}, nil
}

// GenerateImage sends a prompt to Stability and returns the generated raw image bytes.
func (c *StabilityClient) GenerateImage(ctx context.Context, prompt string, characterRefs []string) ([]byte, error) {
	type stabilityRequest struct {
		Prompt       string `json:"prompt"`
		AspectRatio  string `json:"aspect_ratio"`
		Mode         string `json:"mode"`
		OutputFormat string `json:"output_format"`
	}

	body, err := json.Marshal(stabilityRequest{
		Prompt:       prompt,
		AspectRatio:  c.aspectRatio,
		Mode:         "text-to-image",
		OutputFormat: "png",
	})
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

	if len(resp.Body) == 0 {
		return nil, fmt.Errorf("no image bytes returned from Stability")
	}

	var result struct {
		Images []string `json:"images"`
	}
	if err := json.Unmarshal(resp.Body, &result); err != nil {
		return nil, fmt.Errorf("failed to parse Stability response: %w", err)
	}
	if len(result.Images) == 0 {
		return nil, fmt.Errorf("no images in Stability response")
	}
	imgBytes, err := base64.StdEncoding.DecodeString(result.Images[0])
	if err != nil {
		return nil, fmt.Errorf("failed to decode Stability image: %w", err)
	}
	return imgBytes, nil
}

func aspectRatioForDimensions(width, height int) string {
	switch {
	case width > 0 && height > 0 && width < height:
		return "9:16"
	case width > 0 && height > 0 && width == height:
		return "1:1"
	default:
		return "16:9"
	}
}

// Model returns the resolved Bedrock model ID for tests and diagnostics.
func (c *StabilityClient) Model() string {
	return c.model
}

// Region returns the resolved AWS region for tests and diagnostics.
func (c *StabilityClient) Region() string {
	return c.region
}
