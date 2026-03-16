package video

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGenerateShot_HappyPath(t *testing.T) {
	expectedMP4 := []byte("fake-mp4-data")
	invocationArn := "arn:aws:bedrock:us-east-1:123456789012:async-invoke/test-id-123"

	mock := &mockBedrockAsync{
		startResult: invocationArn,
		getStatuses: []asyncInvokeResult{
			{status: "InProgress"},
			{status: "Completed", s3URI: "s3://test-bucket/reel-output/test-id-123/output.mp4"},
		},
	}
	mockS3 := &mockS3Downloader{
		data: expectedMP4,
	}

	client := &NovaReelClient{
		bedrock:      mock,
		s3Downloader: mockS3,
		s3Bucket:     "test-bucket",
		pollInterval: 10 * time.Millisecond, // fast for tests
		maxWait:      5 * time.Second,
	}

	result, err := client.GenerateShot(context.Background(), "testdata/panel.jpg", "a beautiful sunset scene")
	require.NoError(t, err)
	assert.Equal(t, expectedMP4, result)

	// Verify the start was called with correct model
	assert.Equal(t, "amazon.nova-reel-v1:1", mock.capturedModelID)
	assert.Contains(t, mock.capturedPrompt, "a beautiful sunset scene")
	assert.NotEmpty(t, mock.capturedImageB64)
	assert.Equal(t, "test-bucket", mock.capturedS3Bucket)

	// Verify S3 download was called with the right bucket/key
	assert.Equal(t, "test-bucket", mockS3.capturedBucket)
	assert.Equal(t, "reel-output/test-id-123/output.mp4", mockS3.capturedKey)
}

func TestGenerateShot_PollTimeout(t *testing.T) {
	mock := &mockBedrockAsync{
		startResult: "arn:aws:bedrock:us-east-1:123456789012:async-invoke/timeout-id",
		getStatuses: []asyncInvokeResult{
			{status: "InProgress"},
			{status: "InProgress"},
			{status: "InProgress"},
		},
		alwaysInProgress: true,
	}

	client := &NovaReelClient{
		bedrock:      mock,
		s3Downloader: &mockS3Downloader{},
		s3Bucket:     "test-bucket",
		pollInterval: 10 * time.Millisecond,
		maxWait:      50 * time.Millisecond,
	}

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	_, err := client.GenerateShot(ctx, "testdata/panel.jpg", "test prompt")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "timed out")
}

func TestGenerateShot_StartAsyncError(t *testing.T) {
	mock := &mockBedrockAsync{
		startErr: errors.New("bedrock API error: status 500"),
	}

	client := &NovaReelClient{
		bedrock:      mock,
		s3Downloader: &mockS3Downloader{},
		s3Bucket:     "test-bucket",
		pollInterval: 10 * time.Millisecond,
		maxWait:      5 * time.Second,
	}

	_, err := client.GenerateShot(context.Background(), "testdata/panel.jpg", "test prompt")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "start async invoke")
}

func TestGenerateShot_AsyncInvokeFailed(t *testing.T) {
	mock := &mockBedrockAsync{
		startResult: "arn:aws:bedrock:us-east-1:123456789012:async-invoke/fail-id",
		getStatuses: []asyncInvokeResult{
			{status: "InProgress"},
			{status: "Failed", failureMsg: "content policy violation"},
		},
	}

	client := &NovaReelClient{
		bedrock:      mock,
		s3Downloader: &mockS3Downloader{},
		s3Bucket:     "test-bucket",
		pollInterval: 10 * time.Millisecond,
		maxWait:      5 * time.Second,
	}

	_, err := client.GenerateShot(context.Background(), "testdata/panel.jpg", "test prompt")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "content policy violation")
}

func TestGenerateShot_S3DownloadError(t *testing.T) {
	mock := &mockBedrockAsync{
		startResult: "arn:aws:bedrock:us-east-1:123456789012:async-invoke/s3err-id",
		getStatuses: []asyncInvokeResult{
			{status: "Completed", s3URI: "s3://test-bucket/reel-output/s3err-id/output.mp4"},
		},
	}
	mockS3 := &mockS3Downloader{
		err: errors.New("access denied"),
	}

	client := &NovaReelClient{
		bedrock:      mock,
		s3Downloader: mockS3,
		s3Bucket:     "test-bucket",
		pollInterval: 10 * time.Millisecond,
		maxWait:      5 * time.Second,
	}

	_, err := client.GenerateShot(context.Background(), "testdata/panel.jpg", "test prompt")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "access denied")
}

func TestGenerateShot_InvalidImagePath(t *testing.T) {
	client := &NovaReelClient{
		bedrock:      &mockBedrockAsync{},
		s3Downloader: &mockS3Downloader{},
		s3Bucket:     "test-bucket",
		pollInterval: 10 * time.Millisecond,
		maxWait:      5 * time.Second,
	}

	_, err := client.GenerateShot(context.Background(), "/nonexistent/image.jpg", "test prompt")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "read image")
}
