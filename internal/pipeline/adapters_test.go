package pipeline_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/baochen10luo/stagenthand/internal/domain"
	"github.com/baochen10luo/stagenthand/internal/pipeline"
)

// --- Mock image.Client for ImageClientBatcher ---

type mockImageClient struct {
	data []byte
	err  error
}

func (m *mockImageClient) GenerateImage(_ context.Context, _ string, _ []string) ([]byte, error) {
	return m.data, m.err
}

func TestImageClientBatcher_Success(t *testing.T) {
	tmpDir := t.TempDir()
	batcher := pipeline.NewImageClientBatcher(&mockImageClient{data: []byte("fakepng")}, tmpDir)
	panels := []domain.Panel{
		{SceneNumber: 1, PanelNumber: 1, Description: "hero", CharacterRefs: []string{}},
		{SceneNumber: 1, PanelNumber: 2, Description: "cafe", CharacterRefs: []string{}},
	}

	result, err := batcher.BatchGenerateImages(context.Background(), panels, "test-proj/images")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result) != 2 {
		t.Errorf("want 2 panels, got %d", len(result))
	}
	for _, p := range result {
		if p.ImageURL == "" {
			t.Errorf("panel %d-%d: expected ImageURL to be set, got empty", p.SceneNumber, p.PanelNumber)
		}
	}
}

func TestImageClientBatcher_PropagatesError(t *testing.T) {
	batcher := pipeline.NewImageClientBatcher(&mockImageClient{err: errors.New("quota exceeded")}, t.TempDir())
	panels := []domain.Panel{
		{SceneNumber: 1, PanelNumber: 1, Description: "hero"},
	}

	_, err := batcher.BatchGenerateImages(context.Background(), panels, "error-proj")
	if err == nil {
		t.Error("expected error to propagate, got nil")
	}
}

// --- Mock store.CheckpointRepository for CheckpointGateAdapter ---

type mockCkptRepo struct {
	getStatus domain.CheckpointStatus
	createErr error
	getErr    error
}

func (m *mockCkptRepo) Create(cp *domain.Checkpoint) error { return m.createErr }
func (m *mockCkptRepo) GetByID(_ string) (*domain.Checkpoint, error) {
	if m.getErr != nil {
		return nil, m.getErr
	}
	return &domain.Checkpoint{Status: m.getStatus}, nil
}
func (m *mockCkptRepo) ListByJobID(_ string) ([]*domain.Checkpoint, error) { return nil, nil }
func (m *mockCkptRepo) UpdateStatus(_ string, _ domain.CheckpointStatus, _ string) error {
	return nil
}

func TestCheckpointGate_ApprovedImmediately(t *testing.T) {
	repo := &mockCkptRepo{getStatus: domain.CheckpointStatusApproved}
	gate := pipeline.NewCheckpointGate(repo)

	// Use a context with short deadline; approved immediately so should not timeout
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	err := gate.CreateAndWait(ctx, "job-1", domain.StageStoryboard)
	if err != nil {
		t.Errorf("expected nil for approved checkpoint, got: %v", err)
	}
}

func TestCheckpointGate_Rejected(t *testing.T) {
	repo := &mockCkptRepo{getStatus: domain.CheckpointStatusRejected}
	gate := pipeline.NewCheckpointGate(repo)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	err := gate.CreateAndWait(ctx, "job-2", domain.StageImages)
	if err == nil {
		t.Error("expected error for rejected checkpoint, got nil")
	}
}

func TestCheckpointGate_CreateFailure(t *testing.T) {
	repo := &mockCkptRepo{createErr: errors.New("db down")}
	gate := pipeline.NewCheckpointGate(repo)

	err := gate.CreateAndWait(context.Background(), "job-3", domain.StageFinal)
	if err == nil {
		t.Error("expected error when Create fails, got nil")
	}
}

func TestCheckpointGate_ContextCancellation(t *testing.T) {
	// Keep returning pending so it never resolves on its own
	repo := &mockCkptRepo{getStatus: domain.CheckpointStatusPending}
	gate := pipeline.NewCheckpointGate(repo)

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately

	err := gate.CreateAndWait(ctx, "job-4", domain.StageOutline)
	if err == nil {
		t.Error("expected error from cancelled context, got nil")
	}
}
