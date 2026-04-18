package image

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/bedrockruntime"
)

// TitanImageClient implements the image.Client interface using Amazon Titan Image Generator.
type TitanImageClient struct {
	client bedrockInvoker
	model  string
	region string
	width  int
	height int
}

// NewTitanImageClient initializes an AWS Bedrock Runtime client for Titan Image Generator.
func NewTitanImageClient(accessKey, secretKey, region, model string, width, height int) (*TitanImageClient, error) {
	if region == "" {
		region = "us-west-2"
	}
	if model == "" {
		model = "amazon.titan-image-generator-v2:0"
	}
	if width == 0 {
		width = 1024
	}
	if height == 0 {
		height = 576
	}

	cfg, err := awsconfig.LoadDefaultConfig(context.Background(),
		awsconfig.WithRegion(region),
		awsconfig.WithCredentialsProvider(credentials.NewStaticCredentialsProvider(accessKey, secretKey, "")),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to load AWS config: %w", err)
	}

	return &TitanImageClient{
		client: bedrockruntime.NewFromConfig(cfg),
		model:  model,
		region: region,
		width:  width,
		height: height,
	}, nil
}

// GenerateImage sends a prompt to Titan and returns the generated image bytes.
func (c *TitanImageClient) GenerateImage(ctx context.Context, prompt string, characterRefs []string) ([]byte, error) {
	type textToImageParams struct {
		Text string `json:"text"`
	}

	type imageGenerationConfig struct {
		Quality        string  `json:"quality"`
		NumberOfImages int     `json:"numberOfImages"`
		Height         int     `json:"height"`
		Width          int     `json:"width"`
		CfgScale       float64 `json:"cfgScale,omitempty"`
	}

	type titanRequest struct {
		TaskType              string                `json:"taskType"`
		TextToImageParams     textToImageParams     `json:"textToImageParams"`
		ImageGenerationConfig imageGenerationConfig `json:"imageGenerationConfig"`
	}

	body, err := json.Marshal(titanRequest{
		TaskType: "TEXT_IMAGE",
		TextToImageParams: textToImageParams{
			Text: prompt,
		},
		ImageGenerationConfig: imageGenerationConfig{
			Quality:        "standard",
			NumberOfImages: 1,
			Height:         c.height,
			Width:          c.width,
			CfgScale:       8.0,
		},
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

	var res struct {
		Images []string `json:"images"`
		Error  string   `json:"error"`
	}
	if err := json.Unmarshal(resp.Body, &res); err != nil {
		return nil, fmt.Errorf("failed to unmarshal response: %w", err)
	}
	if res.Error != "" {
		return nil, fmt.Errorf("titan image generation failed: %s", res.Error)
	}
	if len(res.Images) == 0 {
		return nil, fmt.Errorf("no images returned from Titan Image Generator")
	}

	imgBytes, err := base64.StdEncoding.DecodeString(res.Images[0])
	if err != nil {
		return nil, fmt.Errorf("failed to decode image: %w", err)
	}

	return imgBytes, nil
}

// Model returns the resolved Bedrock model ID for tests and diagnostics.
func (c *TitanImageClient) Model() string {
	return c.model
}

// Region returns the resolved AWS region for tests and diagnostics.
func (c *TitanImageClient) Region() string {
	return c.region
}
