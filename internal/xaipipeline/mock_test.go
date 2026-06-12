package xaipipeline_test

import (
	"context"
	"testing"

	"github.com/baochen10luo/stagenthand/internal/xaipipeline"
)

func TestMockShotGenerator_GenerateShotResultDoesNotUseDataOnlyFallback(t *testing.T) {
	t.Parallel()

	dataOnlyCalled := false
	generator := &xaipipeline.MockShotGenerator{
		GenerateShotFunc: func(context.Context, xaipipeline.Shot) ([]byte, error) {
			dataOnlyCalled = true
			return []byte("mp4"), nil
		},
	}

	got, err := generator.GenerateShotResult(context.Background(), xaipipeline.Shot{Index: 1})
	if err != nil {
		t.Fatalf("GenerateShotResult() error = %v", err)
	}
	if dataOnlyCalled {
		t.Fatal("GenerateShotResult() should not call data-only GenerateShotFunc")
	}
	if len(got.Data) != 0 || got.RequestID != "" || got.Status != "" {
		t.Fatalf("GenerateShotResult() = %+v, want zero result without GenerateShotResultFunc", got)
	}
}

func TestMockVideoClient_GenerateVideoResultDoesNotUseDataOnlyFallback(t *testing.T) {
	t.Parallel()

	dataOnlyCalled := false
	client := &xaipipeline.MockVideoClient{
		GenerateVideoFunc: func(context.Context, string, string, xaipipeline.VideoOptions) ([]byte, error) {
			dataOnlyCalled = true
			return []byte("mp4"), nil
		},
	}

	got, err := client.GenerateVideoResult(context.Background(), "", "prompt", xaipipeline.VideoOptions{})
	if err != nil {
		t.Fatalf("GenerateVideoResult() error = %v", err)
	}
	if dataOnlyCalled {
		t.Fatal("GenerateVideoResult() should not call data-only GenerateVideoFunc")
	}
	if len(got.Data) != 0 || got.RequestID != "" || got.Status != "" {
		t.Fatalf("GenerateVideoResult() = %+v, want zero result without GenerateVideoResultFunc", got)
	}
}
