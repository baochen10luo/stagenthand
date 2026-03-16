package video

import (
	"context"
	"sync"
)

// asyncInvokeResult holds a single poll result for testing.
type asyncInvokeResult struct {
	status     string
	s3URI      string
	failureMsg string
}

// mockBedrockAsync implements bedrockAsyncInvoker for testing.
type mockBedrockAsync struct {
	startResult string
	startErr    error

	getStatuses      []asyncInvokeResult
	alwaysInProgress bool

	mu               sync.Mutex
	pollIndex        int
	capturedModelID  string
	capturedPrompt   string
	capturedImageB64 string
	capturedS3Bucket string
}

func (m *mockBedrockAsync) StartAsyncInvoke(ctx context.Context, modelID, prompt, imageBase64, s3Bucket string) (string, error) {
	m.capturedModelID = modelID
	m.capturedPrompt = prompt
	m.capturedImageB64 = imageBase64
	m.capturedS3Bucket = s3Bucket
	return m.startResult, m.startErr
}

func (m *mockBedrockAsync) GetAsyncInvoke(ctx context.Context, invocationArn string) (status, s3URI, failureMsg string, err error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.alwaysInProgress {
		return "InProgress", "", "", nil
	}

	if m.pollIndex >= len(m.getStatuses) {
		return "InProgress", "", "", nil
	}

	result := m.getStatuses[m.pollIndex]
	m.pollIndex++
	return result.status, result.s3URI, result.failureMsg, nil
}

// mockS3Downloader implements s3Downloader for testing.
type mockS3Downloader struct {
	data []byte
	err  error

	capturedBucket string
	capturedKey    string
}

func (m *mockS3Downloader) Download(ctx context.Context, bucket, key string) ([]byte, error) {
	m.capturedBucket = bucket
	m.capturedKey = key
	return m.data, m.err
}
