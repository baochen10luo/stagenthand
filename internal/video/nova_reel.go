package video

import (
	"context"
	"encoding/base64"
	"fmt"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/bedrockruntime"
	"github.com/aws/aws-sdk-go-v2/service/bedrockruntime/document"
	"github.com/aws/aws-sdk-go-v2/service/bedrockruntime/types"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"io"
)

const (
	novaReelModelID     = "amazon.nova-reel-v1:1"
	defaultPollInterval = 10 * time.Second
	defaultMaxWait      = 10 * time.Minute
	reelDurationSeconds = 6
	reelFPS             = 24
	reelDimension       = "1280x720"
)

// bedrockAsyncInvoker abstracts the Bedrock async invoke operations for testability.
type bedrockAsyncInvoker interface {
	StartAsyncInvoke(ctx context.Context, modelID, prompt, imageBase64, s3Bucket string) (invocationArn string, err error)
	GetAsyncInvoke(ctx context.Context, invocationArn string) (status, s3URI, failureMsg string, err error)
}

// s3Downloader abstracts S3 object download for testability.
type s3Downloader interface {
	Download(ctx context.Context, bucket, key string) ([]byte, error)
}

// NovaReelClient sends panel images to Nova Reel I2V (Image-to-Video) and returns shot mp4 bytes.
// It does NOT implement video.Client because Reel's input is a local file path, not an imageURL string.
type NovaReelClient struct {
	bedrock      bedrockAsyncInvoker
	s3Downloader s3Downloader
	s3Bucket     string
	pollInterval time.Duration
	maxWait      time.Duration
}

// NewNovaReelClient creates a NovaReelClient backed by real AWS Bedrock and S3.
func NewNovaReelClient(accessKeyID, secretAccessKey, region, s3Bucket string) (*NovaReelClient, error) {
	if region == "" {
		region = "us-east-1"
	}

	cfg, err := awsconfig.LoadDefaultConfig(context.Background(),
		awsconfig.WithRegion(region),
		awsconfig.WithCredentialsProvider(credentials.NewStaticCredentialsProvider(accessKeyID, secretAccessKey, "")),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to load AWS config for Nova Reel: %w", err)
	}

	brClient := bedrockruntime.NewFromConfig(cfg)
	s3Client := s3.NewFromConfig(cfg)

	return &NovaReelClient{
		bedrock:      &bedrockAsyncAdapter{client: brClient},
		s3Downloader: &realS3Downloader{client: s3Client},
		s3Bucket:     s3Bucket,
		pollInterval: defaultPollInterval,
		maxWait:      defaultMaxWait,
	}, nil
}

// GenerateShot sends a single panel image to Nova Reel I2V and returns the shot mp4 bytes.
// imagePath is a local file path. prompt is the panel description.
// Nova Reel is async: StartAsyncInvoke -> poll GetAsyncInvoke -> download from S3.
func (c *NovaReelClient) GenerateShot(ctx context.Context, imagePath string, prompt string) ([]byte, error) {
	// 1. Read and base64-encode the image
	imgBytes, err := os.ReadFile(imagePath)
	if err != nil {
		return nil, fmt.Errorf("read image %s: %w", imagePath, err)
	}
	imageB64 := base64.StdEncoding.EncodeToString(imgBytes)

	// 2. Start async invoke
	invocationArn, err := c.bedrock.StartAsyncInvoke(ctx, novaReelModelID, prompt, imageB64, c.s3Bucket)
	if err != nil {
		return nil, fmt.Errorf("start async invoke: %w", err)
	}

	// 3. Poll until complete or timeout
	deadline := time.Now().Add(c.maxWait)
	for {
		if time.Now().After(deadline) {
			return nil, fmt.Errorf("nova reel timed out after %v waiting for invocation %s", c.maxWait, invocationArn)
		}

		select {
		case <-ctx.Done():
			return nil, fmt.Errorf("nova reel timed out: context cancelled for invocation %s", invocationArn)
		case <-time.After(c.pollInterval):
		}

		status, s3URI, failureMsg, err := c.bedrock.GetAsyncInvoke(ctx, invocationArn)
		if err != nil {
			return nil, fmt.Errorf("poll async invoke: %w", err)
		}

		switch status {
		case "Completed":
			// 4. Parse S3 URI and download
			bucket, key, parseErr := parseS3URI(s3URI)
			if parseErr != nil {
				return nil, fmt.Errorf("parse S3 output URI: %w", parseErr)
			}
			// Append /output.mp4 if the key looks like a directory
			if !strings.HasSuffix(key, ".mp4") {
				key = strings.TrimSuffix(key, "/") + "/output.mp4"
			}
			data, dlErr := c.s3Downloader.Download(ctx, bucket, key)
			if dlErr != nil {
				return nil, fmt.Errorf("download reel output from s3://%s/%s: %w", bucket, key, dlErr)
			}
			return data, nil

		case "Failed":
			return nil, fmt.Errorf("nova reel invocation failed: %s", failureMsg)

		default:
			// InProgress — continue polling
		}
	}
}

// parseS3URI parses "s3://bucket/key" into bucket and key components.
func parseS3URI(s3URI string) (bucket, key string, err error) {
	u, err := url.Parse(s3URI)
	if err != nil {
		return "", "", fmt.Errorf("invalid S3 URI %q: %w", s3URI, err)
	}
	if u.Scheme != "s3" {
		return "", "", fmt.Errorf("expected s3:// scheme, got %q", u.Scheme)
	}
	bucket = u.Host
	key = strings.TrimPrefix(u.Path, "/")
	return bucket, key, nil
}

// ---- Real AWS adapters (used in production) ----

// bedrockAsyncAdapter wraps the real bedrockruntime.Client to implement bedrockAsyncInvoker.
type bedrockAsyncAdapter struct {
	client *bedrockruntime.Client
}

func (a *bedrockAsyncAdapter) StartAsyncInvoke(ctx context.Context, modelID, prompt, imageBase64, s3Bucket string) (string, error) {
	modelInput := map[string]interface{}{
		"taskType": "TEXT_VIDEO",
		"textToVideoParams": map[string]interface{}{
			"text": prompt,
			"images": []map[string]interface{}{
				{
					"format": "jpeg",
					"source": map[string]interface{}{
						"bytes": imageBase64,
					},
				},
			},
		},
		"videoGenerationConfig": map[string]interface{}{
			"durationSeconds": reelDurationSeconds,
			"fps":             reelFPS,
			"dimension":       reelDimension,
			"seed":            0,
		},
	}

	s3URI := fmt.Sprintf("s3://%s/reel-output/", s3Bucket)

	resp, err := a.client.StartAsyncInvoke(ctx, &bedrockruntime.StartAsyncInvokeInput{
		ModelId:    aws.String(modelID),
		ModelInput: document.NewLazyDocument(modelInput),
		OutputDataConfig: &types.AsyncInvokeOutputDataConfigMemberS3OutputDataConfig{
			Value: types.AsyncInvokeS3OutputDataConfig{
				S3Uri: aws.String(s3URI),
			},
		},
	})
	if err != nil {
		return "", err
	}

	return aws.ToString(resp.InvocationArn), nil
}

func (a *bedrockAsyncAdapter) GetAsyncInvoke(ctx context.Context, invocationArn string) (status, s3URI, failureMsg string, err error) {
	resp, err := a.client.GetAsyncInvoke(ctx, &bedrockruntime.GetAsyncInvokeInput{
		InvocationArn: aws.String(invocationArn),
	})
	if err != nil {
		return "", "", "", err
	}

	status = string(resp.Status)
	failureMsg = aws.ToString(resp.FailureMessage)

	// Extract S3 URI from output config if available
	if s3Out, ok := resp.OutputDataConfig.(*types.AsyncInvokeOutputDataConfigMemberS3OutputDataConfig); ok {
		s3URI = aws.ToString(s3Out.Value.S3Uri)
	}

	return status, s3URI, failureMsg, nil
}

// realS3Downloader downloads S3 objects using the AWS S3 SDK client.
type realS3Downloader struct {
	client *s3.Client
}

func (d *realS3Downloader) Download(ctx context.Context, bucket, key string) ([]byte, error) {
	resp, err := d.client.GetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(bucket),
		Key:    aws.String(key),
	})
	if err != nil {
		return nil, fmt.Errorf("s3 GetObject failed: %w", err)
	}
	defer resp.Body.Close()

	return io.ReadAll(resp.Body)
}
