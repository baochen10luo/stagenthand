package llm

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/bedrockruntime"
	brtypes "github.com/aws/aws-sdk-go-v2/service/bedrockruntime/types"
)

// BedrockAPI is a narrow interface for the Converse method.
// Follows Interface Segregation — we only expose what we need.
type BedrockAPI interface {
	Converse(ctx context.Context, params *bedrockruntime.ConverseInput,
		optFns ...func(*bedrockruntime.Options)) (*bedrockruntime.ConverseOutput, error)
}

// BedrockClient implements Client using the AWS Bedrock Converse API.
type BedrockClient struct {
	api     BedrockAPI
	modelID string
}

// NewBedrockClient creates a real Bedrock client with static AWS credentials.
func NewBedrockClient(accessKeyID, secretKey, region, modelID string) (*BedrockClient, error) {
	if accessKeyID == "" {
		return nil, errors.New("aws_access_key_id is required for bedrock provider")
	}
	if secretKey == "" {
		return nil, errors.New("aws_secret_access_key is required for bedrock provider")
	}
	if region == "" {
		region = "us-east-1"
	}
	if modelID == "" {
		modelID = "amazon.nova-pro-v1:0"
	}

	client := bedrockruntime.New(bedrockruntime.Options{
		Region: region,
		Credentials: credentials.NewStaticCredentialsProvider(
			accessKeyID, secretKey, "",
		),
	})

	return &BedrockClient{
		api:     client,
		modelID: modelID,
	}, nil
}

// NewBedrockClientWithAPI creates a BedrockClient with an injected API implementation.
// Used for testing (Dependency Inversion + Liskov Substitution).
func NewBedrockClientWithAPI(api BedrockAPI, modelID string) *BedrockClient {
	return &BedrockClient{
		api:     api,
		modelID: modelID,
	}
}

// GenerateTransformation implements Client.
func (b *BedrockClient) GenerateTransformation(ctx context.Context, systemPrompt string, inputData []byte) ([]byte, error) {
	input := &bedrockruntime.ConverseInput{
		ModelId: aws.String(b.modelID),
		InferenceConfig: &brtypes.InferenceConfiguration{
			MaxTokens: aws.Int32(4096),
		},
		System: []brtypes.SystemContentBlock{
			&brtypes.SystemContentBlockMemberText{Value: systemPrompt},
		},
		Messages: []brtypes.Message{
			{
				Role: brtypes.ConversationRoleUser,
				Content: []brtypes.ContentBlock{
					&brtypes.ContentBlockMemberText{Value: string(inputData)},
				},
			},
		},
	}

	output, err := b.api.Converse(ctx, input)
	if err != nil {
		return nil, fmt.Errorf("bedrock converse failed: %w", err)
	}

	msgOutput, ok := output.Output.(*brtypes.ConverseOutputMemberMessage)
	if !ok {
		return nil, errors.New("unexpected output type from bedrock converse")
	}

	if len(msgOutput.Value.Content) == 0 {
		return nil, errors.New("bedrock returned empty response content")
	}

	textBlock, ok := msgOutput.Value.Content[0].(*brtypes.ContentBlockMemberText)
	if !ok {
		return nil, errors.New("bedrock response content is not text")
	}

	content := strings.TrimSpace(textBlock.Value)

	// Strip markdown code fences — same strategy as openai_compat.go
	if strings.HasPrefix(content, "```json") {
		content = strings.TrimPrefix(content, "```json")
	} else if strings.HasPrefix(content, "```") {
		content = strings.TrimPrefix(content, "```")
	}
	if strings.HasSuffix(content, "```") {
		content = strings.TrimSuffix(content, "```")
	}
	content = strings.TrimSpace(content)

	return []byte(content), nil
}

// ReviewVideo implements VideoCriticClient.
func (b *BedrockClient) ReviewVideo(ctx context.Context, systemPrompt string, inputData []byte, videoFormat string, videoBytes []byte) ([]byte, error) {
	format := brtypes.VideoFormat(videoFormat)
	videoBlock := brtypes.VideoBlock{
		Format: format,
		Source: &brtypes.VideoSourceMemberBytes{
			Value: videoBytes,
		},
	}

	contentBlocks := []brtypes.ContentBlock{
		&brtypes.ContentBlockMemberVideo{Value: videoBlock},
	}
	if len(inputData) > 0 {
		contentBlocks = append(contentBlocks, &brtypes.ContentBlockMemberText{Value: string(inputData)})
	}

	input := &bedrockruntime.ConverseInput{
		ModelId: aws.String(b.modelID),
		InferenceConfig: &brtypes.InferenceConfiguration{
			MaxTokens: aws.Int32(4096),
		},
		System: []brtypes.SystemContentBlock{
			&brtypes.SystemContentBlockMemberText{Value: systemPrompt},
		},
		Messages: []brtypes.Message{
			{
				Role:    brtypes.ConversationRoleUser,
				Content: contentBlocks,
			},
		},
	}

	output, err := b.api.Converse(ctx, input)
	if err != nil {
		return nil, fmt.Errorf("bedrock video converse failed: %w", err)
	}

	msgOutput, ok := output.Output.(*brtypes.ConverseOutputMemberMessage)
	if !ok {
		return nil, errors.New("unexpected output type from bedrock converse")
	}

	if len(msgOutput.Value.Content) == 0 {
		return nil, errors.New("bedrock returned empty response content")
	}

	textBlock, ok := msgOutput.Value.Content[0].(*brtypes.ContentBlockMemberText)
	if !ok {
		return nil, errors.New("bedrock response content is not text")
	}

	content := strings.TrimSpace(textBlock.Value)

	if strings.HasPrefix(content, "```json") {
		content = strings.TrimPrefix(content, "```json")
	} else if strings.HasPrefix(content, "```") {
		content = strings.TrimPrefix(content, "```")
	}
	if strings.HasSuffix(content, "```") {
		content = strings.TrimSuffix(content, "```")
	}

	return []byte(strings.TrimSpace(content)), nil
}
