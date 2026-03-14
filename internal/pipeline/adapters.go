package pipeline

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/baochen10luo/stagenthand/internal/domain"
	"github.com/baochen10luo/stagenthand/internal/image"
	"github.com/baochen10luo/stagenthand/internal/store"
	"github.com/google/uuid"
)

// ImageClientBatcher adapts an image.Client into the ImageBatcher interface.
// It generates images concurrently for each panel using the underlying client.
type ImageClientBatcher struct {
	client  image.Client
	rootDir string // e.g. /Users/paul/.shand/
}

// NewImageClientBatcher wraps an image.Client as an ImageBatcher.
func NewImageClientBatcher(c image.Client, rootDir string) ImageBatcher {
	return &ImageClientBatcher{client: c, rootDir: rootDir}
}

// BatchGenerateImages generates images for all panels sequentially.
// Each panel's ImageURL is set to the local path where the bytes were saved.
func (b *ImageClientBatcher) BatchGenerateImages(ctx context.Context, panels []domain.Panel, targetDir string) ([]domain.Panel, error) {
	fullDir := filepath.Join(b.rootDir, targetDir)
	if err := os.MkdirAll(fullDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create image dir %s: %w", fullDir, err)
	}

	result := make([]domain.Panel, len(panels))
	for i, p := range panels {
		imgBytes, err := b.client.GenerateImage(ctx, p.Description, p.CharacterRefs)
		if err != nil {
			return nil, fmt.Errorf("panel %d-%d image gen failed: %w", p.SceneNumber, p.PanelNumber, err)
		}

		filename := fmt.Sprintf("scene_%d_panel_%d.png", p.SceneNumber, p.PanelNumber)
		absPath := filepath.Join(fullDir, filename)
		if err := os.WriteFile(absPath, imgBytes, 0644); err != nil {
			return nil, fmt.Errorf("failed to save image %s: %w", absPath, err)
		}

		p.ImageURL = absPath
		result[i] = p
	}
	return result, nil
}

// ─────────────────────────────────────────────────────────────────────────────
// CheckpointGateAdapter adapts store.CheckpointRepository into CheckpointGate.
// It creates a Checkpoint record and polls until it is approved or rejected.
// ─────────────────────────────────────────────────────────────────────────────

// CheckpointGateAdapter wraps a store.CheckpointRepository to implement CheckpointGate.
type CheckpointGateAdapter struct {
	repo store.CheckpointRepository
}

// NewCheckpointGate constructs a CheckpointGate backed by the given repository.
func NewCheckpointGate(repo store.CheckpointRepository) CheckpointGate {
	return &CheckpointGateAdapter{repo: repo}
}

// CreateAndWait creates a pending checkpoint and polls every 5s for approval.
// Returns nil when approved, or an error if rejected or context is cancelled.
func (g *CheckpointGateAdapter) CreateAndWait(ctx context.Context, jobID string, stage domain.CheckpointStage) error {
	cp := &domain.Checkpoint{
		ID:        uuid.New().String(),
		JobID:     jobID,
		Stage:     stage,
		Status:    domain.CheckpointStatusPending,
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}
	if err := g.repo.Create(cp); err != nil {
		return fmt.Errorf("creating checkpoint: %w", err)
	}

	// Poll until resolved or context cancelled
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return fmt.Errorf("checkpoint %s cancelled: %w", cp.ID, ctx.Err())
		case <-ticker.C:
			current, err := g.repo.GetByID(cp.ID)
			if err != nil {
				return fmt.Errorf("polling checkpoint %s: %w", cp.ID, err)
			}
			switch current.Status {
			case domain.CheckpointStatusApproved:
				return nil
			case domain.CheckpointStatusRejected:
				return fmt.Errorf("checkpoint %s rejected at stage %s: %s", cp.ID, stage, current.Notes)
			}
			// still pending, continue polling
		}
	}
}
