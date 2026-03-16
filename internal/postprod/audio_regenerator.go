package postprod

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/baochen10luo/stagenthand/internal/domain"
)

// AudioBatcher is the interface needed by AudioRegenerator.
// Defined here (in postprod) per DIP — mirrors pipeline.AudioBatcher.
type AudioBatcher interface {
	BatchGenerateAudio(ctx context.Context, panels []domain.Panel, targetDir string) ([]domain.Panel, error)
}

// AudioRegenerator deletes existing audio files for specified panels
// and re-generates them using the audio batcher.
type AudioRegenerator struct {
	batcher AudioBatcher
	rootDir string
}

// NewAudioRegenerator creates a new AudioRegenerator.
func NewAudioRegenerator(batcher AudioBatcher, rootDir string) *AudioRegenerator {
	return &AudioRegenerator{batcher: batcher, rootDir: rootDir}
}

// RegenerateForPanels deletes the mp3 files for the given panels and re-generates.
// Panels with empty dialogue are skipped from deletion and generation.
func (a *AudioRegenerator) RegenerateForPanels(ctx context.Context, panels []domain.Panel, projectID string) ([]domain.Panel, error) {
	// Separate panels with dialogue from those without
	var toRegen []domain.Panel
	var skipped []domain.Panel

	for _, p := range panels {
		if p.Dialogue == "" {
			skipped = append(skipped, p)
			continue
		}
		// Delete existing audio file if it exists
		if p.AudioURL != "" {
			audioPath := p.AudioURL
			if !filepath.IsAbs(audioPath) {
				audioPath = filepath.Join(a.rootDir, audioPath)
			}
			if err := os.Remove(audioPath); err != nil && !os.IsNotExist(err) {
				return nil, fmt.Errorf("delete audio %s: %w", audioPath, err)
			}
		}
		toRegen = append(toRegen, p)
	}

	if len(toRegen) == 0 {
		return panels, nil
	}

	// targetDir for the batcher — use projectID-based path
	targetDir := filepath.Join("projects", projectID, "audio")

	regenResult, err := a.batcher.BatchGenerateAudio(ctx, toRegen, targetDir)
	if err != nil {
		return nil, fmt.Errorf("regenerate audio: %w", err)
	}

	// Merge results: regenerated panels + skipped panels (in original order)
	// Build map from scene+panel to regenerated panel
	type key struct{ scene, panel int }
	regenMap := make(map[key]domain.Panel, len(regenResult))
	for _, p := range regenResult {
		regenMap[key{p.SceneNumber, p.PanelNumber}] = p
	}

	result := make([]domain.Panel, len(panels))
	for i, p := range panels {
		k := key{p.SceneNumber, p.PanelNumber}
		if rp, ok := regenMap[k]; ok {
			result[i] = rp
		} else {
			result[i] = p
		}
	}

	_ = skipped // included in result via original panels fallthrough
	return result, nil
}
