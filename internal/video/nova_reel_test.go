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

func TestParseS3URI(t *testing.T) {
	tests := []struct {
		name       string
		uri        string
		wantBucket string
		wantKey    string
		wantErr    bool
	}{
		{"valid s3 URI", "s3://mybucket/some/key.mp4", "mybucket", "some/key.mp4", false},
		{"valid s3 URI with trailing slash", "s3://mybucket/output/", "mybucket", "output/", false},
		{"invalid scheme http", "http://mybucket/key", "", "", true},
		{"invalid scheme https", "https://mybucket/key", "", "", true},
		{"empty scheme", "://bad", "", "", true},
		{"no scheme", "mybucket/key", "", "", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			bucket, key, err := parseS3URI(tt.uri)
			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.wantBucket, bucket)
			assert.Equal(t, tt.wantKey, key)
		})
	}
}

func TestNewNovaReelClient(t *testing.T) {
	// NewNovaReelClient should succeed with valid region and credentials
	// (AWS SDK does not validate credentials at config load time)
	client, err := NewNovaReelClient("AKIAIOSFODNN7EXAMPLE", "wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY", "us-east-1", "test-bucket")
	require.NoError(t, err)
	assert.NotNil(t, client)
	assert.Equal(t, "test-bucket", client.s3Bucket)
	assert.Equal(t, defaultPollInterval, client.pollInterval)
	assert.Equal(t, defaultMaxWait, client.maxWait)
}

func TestNewNovaReelClient_DefaultRegion(t *testing.T) {
	// When region is empty, it should default to us-east-1 and still succeed
	client, err := NewNovaReelClient("AKID", "SECRET", "", "my-bucket")
	require.NoError(t, err)
	assert.NotNil(t, client)
}

func TestGenerateShot_S3URIDirectoryAppendOutputMp4(t *testing.T) {
	// When the S3 URI key does not end in .mp4, GenerateShot appends /output.mp4
	expectedMP4 := []byte("mp4-data")
	mock := &mockBedrockAsync{
		startResult: "arn:aws:bedrock:us-east-1:123:async-invoke/dir-test",
		getStatuses: []asyncInvokeResult{
			{status: "Completed", s3URI: "s3://bucket/reel-output/dir-test"},
		},
	}
	mockS3 := &mockS3Downloader{data: expectedMP4}

	client := &NovaReelClient{
		bedrock:      mock,
		s3Downloader: mockS3,
		s3Bucket:     "bucket",
		pollInterval: 10 * time.Millisecond,
		maxWait:      5 * time.Second,
	}

	result, err := client.GenerateShot(context.Background(), "testdata/panel.jpg", "prompt")
	require.NoError(t, err)
	assert.Equal(t, expectedMP4, result)
	// Key should have /output.mp4 appended
	assert.Equal(t, "reel-output/dir-test/output.mp4", mockS3.capturedKey)
}

func TestGenerateShot_GetAsyncInvokeError(t *testing.T) {
	mock := &mockBedrockAsync{
		startResult: "arn:aws:bedrock:us-east-1:123:async-invoke/poll-err",
		getStatuses: []asyncInvokeResult{},
	}
	// Override GetAsyncInvoke to return error by using a custom mock
	errMock := &mockBedrockAsyncWithGetError{
		mockBedrockAsync: mock,
		getErr:           errors.New("network timeout"),
	}

	client := &NovaReelClient{
		bedrock:      errMock,
		s3Downloader: &mockS3Downloader{},
		s3Bucket:     "bucket",
		pollInterval: 10 * time.Millisecond,
		maxWait:      5 * time.Second,
	}

	_, err := client.GenerateShot(context.Background(), "testdata/panel.jpg", "prompt")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "poll async invoke")
}

// mockBedrockAsyncWithGetError wraps mockBedrockAsync but returns an error on GetAsyncInvoke.
type mockBedrockAsyncWithGetError struct {
	*mockBedrockAsync
	getErr error
}

func (m *mockBedrockAsyncWithGetError) GetAsyncInvoke(ctx context.Context, invocationArn string) (status, s3URI, failureMsg string, err error) {
	return "", "", "", m.getErr
}

func TestGenerateShot_InvalidS3URIFromCompleted(t *testing.T) {
	// When Completed returns an invalid S3 URI, GenerateShot should return parseS3URI error
	mock := &mockBedrockAsync{
		startResult: "arn:aws:bedrock:us-east-1:123:async-invoke/bad-uri",
		getStatuses: []asyncInvokeResult{
			{status: "Completed", s3URI: "http://not-s3/path"},
		},
	}

	client := &NovaReelClient{
		bedrock:      mock,
		s3Downloader: &mockS3Downloader{},
		s3Bucket:     "bucket",
		pollInterval: 10 * time.Millisecond,
		maxWait:      5 * time.Second,
	}

	_, err := client.GenerateShot(context.Background(), "testdata/panel.jpg", "prompt")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "parse S3 output URI")
}

func TestGenerateShot_ContextCancelled(t *testing.T) {
	mock := &mockBedrockAsync{
		startResult:      "arn:aws:bedrock:us-east-1:123:async-invoke/ctx-cancel",
		alwaysInProgress: true,
	}

	client := &NovaReelClient{
		bedrock:      mock,
		s3Downloader: &mockS3Downloader{},
		s3Bucket:     "bucket",
		pollInterval: 10 * time.Millisecond,
		maxWait:      10 * time.Second, // long enough that context cancels first
	}

	ctx, cancel := context.WithCancel(context.Background())
	// Cancel immediately so the select picks up ctx.Done()
	cancel()

	_, err := client.GenerateShot(ctx, "testdata/panel.jpg", "prompt")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "context cancelled")
}
