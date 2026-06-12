package xaipipeline_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/baochen10luo/stagenthand/internal/xaipipeline"
)

type stubVideoClient struct {
	imageURL            string
	prompt              string
	options             xaipipeline.VideoOptions
	data                []byte
	requestID           string
	status              string
	err                 error
	calls               int
	afterGenerateResult func()
}

func (s *stubVideoClient) GenerateVideo(_ context.Context, imageURL string, prompt string, options xaipipeline.VideoOptions) ([]byte, error) {
	s.calls++
	s.imageURL = imageURL
	s.prompt = prompt
	s.options = options
	return s.data, s.err
}

func (s *stubVideoClient) GenerateVideoResult(_ context.Context, imageURL string, prompt string, options xaipipeline.VideoOptions) (xaipipeline.VideoGenerationResult, error) {
	s.calls++
	s.imageURL = imageURL
	s.prompt = prompt
	s.options = options
	if s.afterGenerateResult != nil {
		s.afterGenerateResult()
	}
	return xaipipeline.VideoGenerationResult{
		Data:      s.data,
		RequestID: s.requestID,
		Status:    s.status,
	}, s.err
}

func TestVideoShotGenerator_UsesShotPrompt(t *testing.T) {
	t.Parallel()

	client := &stubVideoClient{data: []byte("mp4"), requestID: "req_123", status: "done"}
	generator := xaipipeline.NewVideoShotGenerator(client)

	got, err := generator.GenerateShot(context.Background(), xaipipeline.Shot{
		Index:       1,
		Prompt:      "wide cinematic shot",
		DurationSec: 6.5,
		AspectRatio: "16:9",
		Resolution:  "1080p",
	})
	if err != nil {
		t.Fatalf("GenerateShot() error = %v", err)
	}
	if string(got) != "mp4" {
		t.Fatalf("GenerateShot() = %q, want mp4", string(got))
	}
	if client.imageURL != "" {
		t.Fatalf("imageURL = %q, want empty", client.imageURL)
	}
	if client.prompt != "wide cinematic shot" {
		t.Fatalf("prompt = %q, want wide cinematic shot", client.prompt)
	}
	if client.options.DurationSec != 6.5 {
		t.Fatalf("DurationSec = %.1f, want 6.5", client.options.DurationSec)
	}
	if client.options.AspectRatio != "16:9" {
		t.Fatalf("AspectRatio = %q, want 16:9", client.options.AspectRatio)
	}
	if client.options.Resolution != "1080p" {
		t.Fatalf("Resolution = %q, want 1080p", client.options.Resolution)
	}
}

func TestVideoShotGenerator_TrimsShotPromptBeforeClient(t *testing.T) {
	t.Parallel()

	client := &stubVideoClient{data: []byte("mp4"), requestID: "req_123", status: "done"}
	generator := xaipipeline.NewVideoShotGenerator(client)

	_, err := generator.GenerateShotResult(context.Background(), xaipipeline.Shot{
		Index:  1,
		Prompt: "  wide cinematic shot  ",
	})
	if err != nil {
		t.Fatalf("GenerateShotResult() error = %v", err)
	}
	if client.prompt != "wide cinematic shot" {
		t.Fatalf("prompt = %q, want trimmed prompt", client.prompt)
	}
}

func TestVideoShotGenerator_RejectsEmptyPromptBeforeClient(t *testing.T) {
	t.Parallel()

	client := &stubVideoClient{data: []byte("mp4")}
	generator := xaipipeline.NewVideoShotGenerator(client)

	_, err := generator.GenerateShotResult(context.Background(), xaipipeline.Shot{
		Index:  7,
		Prompt: " \t\n",
	})
	if err == nil {
		t.Fatal("GenerateShotResult() error = nil, want empty prompt error")
	}
	if !strings.Contains(err.Error(), "shot 7 prompt is empty") {
		t.Fatalf("GenerateShotResult() error = %v, want empty prompt error", err)
	}
	if client.calls != 0 {
		t.Fatalf("client calls = %d, want 0", client.calls)
	}
}

func TestVideoShotGenerator_RejectsCanceledContextBeforeClient(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	client := &stubVideoClient{data: []byte("mp4"), requestID: "req_123", status: "done"}
	generator := xaipipeline.NewVideoShotGenerator(client)

	got, err := generator.GenerateShotResult(ctx, xaipipeline.Shot{
		Index:  9,
		Prompt: "wide cinematic shot",
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("GenerateShotResult() error = %v, want context.Canceled", err)
	}
	if got.Data != nil || got.RequestID != "" || got.Status != "" {
		t.Fatalf("GenerateShotResult() = %+v, want zero result on canceled context", got)
	}
	if client.calls != 0 {
		t.Fatalf("client calls = %d, want 0", client.calls)
	}
}

func TestVideoShotGenerator_RejectsCanceledContextAfterClientBeforeResult(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	client := &stubVideoClient{
		data:                []byte("mp4"),
		requestID:           "req_123",
		status:              "done",
		afterGenerateResult: cancel,
	}
	generator := xaipipeline.NewVideoShotGenerator(client)

	got, err := generator.GenerateShotResult(ctx, xaipipeline.Shot{
		Index:  10,
		Prompt: "wide cinematic shot",
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("GenerateShotResult() error = %v, want context.Canceled", err)
	}
	if got.Data != nil || got.RequestID != "" || got.Status != "" {
		t.Fatalf("GenerateShotResult() = %+v, want zero result on canceled context", got)
	}
	if client.calls != 1 {
		t.Fatalf("client calls = %d, want 1", client.calls)
	}
}

func TestVideoShotGenerator_NormalizesGenerationOptionsBeforeClient(t *testing.T) {
	t.Parallel()

	client := &stubVideoClient{data: []byte("mp4"), requestID: "req_123", status: "done"}
	generator := xaipipeline.NewVideoShotGenerator(client)

	_, err := generator.GenerateShotResult(context.Background(), xaipipeline.Shot{
		Index:       1,
		Prompt:      "wide cinematic shot",
		AspectRatio: " 9:16 ",
		Resolution:  " 720p ",
	})
	if err != nil {
		t.Fatalf("GenerateShotResult() error = %v", err)
	}
	if client.options.DurationSec != 8 {
		t.Fatalf("DurationSec = %.1f, want 8.0", client.options.DurationSec)
	}
	if client.options.AspectRatio != "9:16" {
		t.Fatalf("AspectRatio = %q, want 9:16", client.options.AspectRatio)
	}
	if client.options.Resolution != "720p" {
		t.Fatalf("Resolution = %q, want 720p", client.options.Resolution)
	}
}

func TestVideoShotGenerator_ReturnsProviderMetadata(t *testing.T) {
	t.Parallel()

	client := &stubVideoClient{
		data:      []byte("mp4"),
		requestID: "req_123",
		status:    "done",
	}
	generator := xaipipeline.NewVideoShotGenerator(client)

	got, err := generator.GenerateShotResult(context.Background(), xaipipeline.Shot{
		Index:  1,
		Prompt: "wide cinematic shot",
	})
	if err != nil {
		t.Fatalf("GenerateShotResult() error = %v", err)
	}
	if string(got.Data) != "mp4" {
		t.Fatalf("Data = %q, want mp4", string(got.Data))
	}
	if got.RequestID != "req_123" {
		t.Fatalf("RequestID = %q, want req_123", got.RequestID)
	}
	if got.Status != "done" {
		t.Fatalf("Status = %q, want done", got.Status)
	}
}

func TestVideoShotGenerator_NormalizesProviderMetadata(t *testing.T) {
	t.Parallel()

	client := &stubVideoClient{
		data:      []byte("mp4"),
		requestID: " req_123 ",
		status:    " DONE ",
	}
	generator := xaipipeline.NewVideoShotGenerator(client)

	got, err := generator.GenerateShotResult(context.Background(), xaipipeline.Shot{
		Index:  1,
		Prompt: "wide cinematic shot",
	})
	if err != nil {
		t.Fatalf("GenerateShotResult() error = %v", err)
	}
	if got.RequestID != "req_123" {
		t.Fatalf("RequestID = %q, want req_123", got.RequestID)
	}
	if got.Status != "done" {
		t.Fatalf("Status = %q, want done", got.Status)
	}
}

func TestVideoShotGenerator_RejectsEmptyProviderVideoData(t *testing.T) {
	t.Parallel()

	client := &stubVideoClient{
		requestID: "req_123",
		status:    "done",
	}
	generator := xaipipeline.NewVideoShotGenerator(client)

	got, err := generator.GenerateShotResult(context.Background(), xaipipeline.Shot{
		Index:  5,
		Prompt: "wide cinematic shot",
	})
	if err == nil {
		t.Fatal("GenerateShotResult() error = nil, want empty video data error")
	}
	if got.Data != nil || got.RequestID != "" || got.Status != "" {
		t.Fatalf("GenerateShotResult() = %+v, want zero result on empty video data", got)
	}
	if !strings.Contains(err.Error(), "shot 5 video data is empty") {
		t.Fatalf("GenerateShotResult() error = %v, want empty video data error", err)
	}
}

func TestVideoShotGenerator_RejectsMissingProviderRequestID(t *testing.T) {
	t.Parallel()

	client := &stubVideoClient{
		data:   []byte("mp4"),
		status: "done",
	}
	generator := xaipipeline.NewVideoShotGenerator(client)

	got, err := generator.GenerateShotResult(context.Background(), xaipipeline.Shot{
		Index:  3,
		Prompt: "wide cinematic shot",
	})
	if err == nil {
		t.Fatal("GenerateShotResult() error = nil, want missing request metadata error")
	}
	if got.Data != nil || got.RequestID != "" || got.Status != "" {
		t.Fatalf("GenerateShotResult() = %+v, want zero result on missing provider metadata", got)
	}
	if !strings.Contains(err.Error(), "shot 3 xai_request_id is empty") {
		t.Fatalf("GenerateShotResult() error = %v, want missing xai_request_id error", err)
	}
}

func TestVideoShotGenerator_RejectsNonDoneProviderStatus(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		status string
	}{
		{name: "pending", status: "pending"},
		{name: "failed", status: "failed"},
		{name: "dry run status does not leave live adapter", status: "dry_run"},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			client := &stubVideoClient{
				data:      []byte("mp4"),
				requestID: "req_123",
				status:    tt.status,
			}
			generator := xaipipeline.NewVideoShotGenerator(client)

			got, err := generator.GenerateShotResult(context.Background(), xaipipeline.Shot{
				Index:  4,
				Prompt: "wide cinematic shot",
			})
			if err == nil {
				t.Fatal("GenerateShotResult() error = nil, want non-done provider status error")
			}
			if got.Data != nil || got.RequestID != "" || got.Status != "" {
				t.Fatalf("GenerateShotResult() = %+v, want zero result on non-done provider status", got)
			}
			if !strings.Contains(err.Error(), "xai_status") || !strings.Contains(err.Error(), strings.TrimSpace(tt.status)) {
				t.Fatalf("GenerateShotResult() error = %v, want xai_status %q error", err, tt.status)
			}
		})
	}
}
