package pipeline_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/baochen10luo/stagenthand/internal/character"
	"github.com/baochen10luo/stagenthand/internal/domain"
	"github.com/baochen10luo/stagenthand/internal/pipeline"
)

// fakePNG is a minimal PNG header used in tests so isValidImageBytes accepts it.
var fakePNG = []byte{0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A}

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
	batcher := pipeline.NewImageClientBatcher(&mockImageClient{data: fakePNG}, tmpDir)
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

func TestImageClientBatcher_ResumeSkipsGeneration(t *testing.T) {
	tmpDir := t.TempDir()
	
	panels := []domain.Panel{
		{SceneNumber: 1, PanelNumber: 1, Description: "already done"},
	}

	// Create the file beforehand so it triggers the resume logic
	fullDir := filepath.Join(tmpDir, "resume-proj")
	os.MkdirAll(fullDir, 0755)
	absPath := filepath.Join(fullDir, "scene_1_panel_1.png")
	os.WriteFile(absPath, fakePNG, 0644)

	// Inject a mock that will ERROR if called. If resume works, it won't be called.
	batcher := pipeline.NewImageClientBatcher(&mockImageClient{err: errors.New("SHOULD NOT BE CALLED")}, tmpDir)

	result, err := batcher.BatchGenerateImages(context.Background(), panels, "resume-proj")
	if err != nil {
		t.Fatalf("unexpected error (API called instead of resumed?): %v", err)
	}

	if result[0].ImageURL != absPath {
		t.Errorf("expected ImageURL to point to existing file %s, got %s", absPath, result[0].ImageURL)
	}
}

// --- ImageClientBatcher with Registry tests ---

// captureImageClient records the characterRefs passed to GenerateImage for assertion.
type captureImageClient struct {
	capturedRefs []string
	data         []byte
}

func (c *captureImageClient) GenerateImage(_ context.Context, _ string, refs []string) ([]byte, error) {
	c.capturedRefs = append([]string(nil), refs...)
	if c.data != nil {
		return c.data, nil
	}
	return fakePNG, nil
}

func TestImageClientBatcherWithRegistry_LooksUpCharacterRefs(t *testing.T) {
	tmpDir := t.TempDir()

	reg := character.NewMockRegistry()
	_, _ = reg.Register(context.Background(), "Alice", []byte("alice-img"))

	cap := &captureImageClient{}
	batcher := pipeline.NewImageClientBatcherWithRegistry(cap, tmpDir, reg)

	panels := []domain.Panel{
		{SceneNumber: 1, PanelNumber: 1, Description: "scene", Characters: []string{"Alice"}},
	}

	_, err := batcher.BatchGenerateImages(context.Background(), panels, "proj/images")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(cap.capturedRefs) == 0 {
		t.Fatal("expected character refs to be appended, got none")
	}
	want := "/mock/characters/Alice/ref.png"
	if cap.capturedRefs[0] != want {
		t.Errorf("expected ref %q, got %q", want, cap.capturedRefs[0])
	}
}

func TestImageClientBatcherWithRegistry_NilRegistryNoLookup(t *testing.T) {
	tmpDir := t.TempDir()

	cap := &captureImageClient{}
	// nil registry — no lookup should happen
	batcher := pipeline.NewImageClientBatcherWithRegistry(cap, tmpDir, nil)

	panels := []domain.Panel{
		{SceneNumber: 1, PanelNumber: 1, Description: "scene", Characters: []string{"Bob"}},
	}

	_, err := batcher.BatchGenerateImages(context.Background(), panels, "proj/images")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// CharacterRefs starts empty and no registry lookup occurs, so refs should remain empty
	if len(cap.capturedRefs) != 0 {
		t.Errorf("expected no character refs when registry is nil, got %v", cap.capturedRefs)
	}
}

func TestImageClientBatcherWithRegistry_MissingCharacterSkipped(t *testing.T) {
	tmpDir := t.TempDir()

	// Empty registry — Lookup returns "" for unknown names
	reg := character.NewMockRegistry()

	cap := &captureImageClient{}
	batcher := pipeline.NewImageClientBatcherWithRegistry(cap, tmpDir, reg)

	panels := []domain.Panel{
		{SceneNumber: 1, PanelNumber: 1, Description: "scene", Characters: []string{"Unknown"}},
	}

	_, err := batcher.BatchGenerateImages(context.Background(), panels, "proj/images")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Unknown character returns "" so it must not be appended
	if len(cap.capturedRefs) != 0 {
		t.Errorf("expected empty refs for missing character, got %v", cap.capturedRefs)
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
