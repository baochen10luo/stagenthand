package pipeline

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/baochen10luo/stagenthand/internal/audio"
	"github.com/baochen10luo/stagenthand/internal/character"
	"github.com/baochen10luo/stagenthand/internal/domain"
	"github.com/baochen10luo/stagenthand/internal/image"
	"github.com/baochen10luo/stagenthand/internal/store"
	"github.com/google/uuid"
)

var pngMagic = []byte{0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A}

func isValidImageFile(path string) bool {
	f, err := os.Open(path)
	if err != nil {
		return false
	}
	defer f.Close()
	buf := make([]byte, 8)
	n, _ := f.Read(buf)
	return n == 8 && bytes.Equal(buf, pngMagic)
}

// ImageClientBatcher adapts an image.Client into the ImageBatcher interface.
// It generates images concurrently for each panel using the underlying client.
type ImageClientBatcher struct {
	client   image.Client
	rootDir  string            // e.g. /Users/paul/.shand/
	registry character.Registry // optional, nil = disabled
}

// NewImageClientBatcher wraps an image.Client as an ImageBatcher.
func NewImageClientBatcher(c image.Client, rootDir string) ImageBatcher {
	return NewImageClientBatcherWithRegistry(c, rootDir, nil)
}

// NewImageClientBatcherWithRegistry wraps an image.Client as an ImageBatcher with an optional character registry.
// When registry is non-nil, character names in each panel are looked up and their reference image paths
// are appended to CharacterRefs before image generation.
func NewImageClientBatcherWithRegistry(c image.Client, rootDir string, reg character.Registry) ImageBatcher {
	return &ImageClientBatcher{client: c, rootDir: rootDir, registry: reg}
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
		filename := fmt.Sprintf("scene_%d_panel_%d.png", p.SceneNumber, p.PanelNumber)
		absPath := filepath.Join(fullDir, filename)

		// Resume: skip generation only if the file is a valid PNG (not a gateway error stub).
		if info, err := os.Stat(absPath); err == nil && info.Size() > 0 && isValidImageFile(absPath) {
			logStage("image", fmt.Sprintf("[%d/%d] scene%d-panel%d  SKIP (cached)", i+1, len(panels), p.SceneNumber, p.PanelNumber))
			p.ImageURL = absPath
			result[i] = p
			continue
		}

		logStage("image", fmt.Sprintf("[%d/%d] scene%d-panel%d  generating...", i+1, len(panels), p.SceneNumber, p.PanelNumber))

		if b.registry != nil {
			for _, name := range p.Characters {
				if path, err := b.registry.Lookup(ctx, name); err == nil && path != "" {
					p.CharacterRefs = append(p.CharacterRefs, path)
				}
			}
		}

		imgBytes, err := b.client.GenerateImage(ctx, p.Description, p.CharacterRefs)
		if err != nil {
			return nil, fmt.Errorf("panel %d-%d image gen failed: %w", p.SceneNumber, p.PanelNumber, err)
		}
		if len(imgBytes) < 8 || !bytes.Equal(imgBytes[:8], pngMagic) {
			return nil, fmt.Errorf("panel %d-%d: image API returned non-PNG data (len=%d): %q", p.SceneNumber, p.PanelNumber, len(imgBytes), imgBytes)
		}

		if err := os.WriteFile(absPath, imgBytes, 0644); err != nil {
			return nil, fmt.Errorf("failed to save image %s: %w", absPath, err)
		}

		logStage("image", fmt.Sprintf("[%d/%d] scene%d-panel%d  done  size=%dB", i+1, len(panels), p.SceneNumber, p.PanelNumber, len(imgBytes)))
		p.ImageURL = absPath
		result[i] = p
	}
	return result, nil
}

// ─────────────────────────────────────────────────────────────────────────────
// PrebuiltImageBatcher maps pre-existing image files to panels in order.
// When --image-dir is set, shand skips image generation entirely.
// ─────────────────────────────────────────────────────────────────────────────

// PrebuiltImageBatcher implements ImageBatcher using images already on disk.
// Files in imageDir are sorted and assigned to panels in order (panel 0 → file[0], etc.).
// Images are copied into rootDir/targetDir so normalizePath() can convert them to /shand/ virtual paths.
type PrebuiltImageBatcher struct {
	imageDir string
	rootDir  string // shandHome (~/.shand)
	offset   int    // number of leading files to skip (e.g. 1 to skip cover image)
}

// NewPrebuiltImageBatcher returns an ImageBatcher that assigns sorted files from imageDir to panels.
func NewPrebuiltImageBatcher(imageDir, rootDir string) ImageBatcher {
	return &PrebuiltImageBatcher{imageDir: imageDir, rootDir: rootDir}
}

// NewPrebuiltImageBatcherWithOffset is like NewPrebuiltImageBatcher but skips the first `offset` files.
// Use offset=1 to skip a cover image (e.g. _1.png) in --i2v mode.
func NewPrebuiltImageBatcherWithOffset(imageDir, rootDir string, offset int) ImageBatcher {
	return &PrebuiltImageBatcher{imageDir: imageDir, rootDir: rootDir, offset: offset}
}

func (b *PrebuiltImageBatcher) BatchGenerateImages(_ context.Context, panels []domain.Panel, targetDir string) ([]domain.Panel, error) {
	entries, err := os.ReadDir(b.imageDir)
	if err != nil {
		return nil, fmt.Errorf("prebuilt image dir %q: %w", b.imageDir, err)
	}

	// Collect image files (png, jpg, jpeg) sorted by name
	var files []string
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		ext := filepath.Ext(e.Name())
		if ext == ".png" || ext == ".jpg" || ext == ".jpeg" {
			files = append(files, filepath.Join(b.imageDir, e.Name()))
		}
	}
	if len(files) == 0 {
		return nil, fmt.Errorf("prebuilt image dir %q: no image files found", b.imageDir)
	}
	// Skip leading files (e.g. cover image in --i2v mode)
	if b.offset > 0 && b.offset < len(files) {
		files = files[b.offset:]
	}

	// Copy images to targetDir so remotion normalizePath works
	destDir := filepath.Join(b.rootDir, targetDir)
	if err := os.MkdirAll(destDir, 0755); err != nil {
		return nil, fmt.Errorf("create image dest dir: %w", err)
	}

	result := make([]domain.Panel, len(panels))
	for i, p := range panels {
		srcIdx := i
		if srcIdx >= len(files) {
			srcIdx = srcIdx % len(files)
		}
		src := files[srcIdx]
		ext := filepath.Ext(src)
		destName := fmt.Sprintf("scene_%d_panel_%d%s", p.SceneNumber, p.PanelNumber, ext)
		dest := filepath.Join(destDir, destName)

		// Copy only if not already present
		if info, statErr := os.Stat(dest); statErr != nil || info.Size() == 0 {
			srcBytes, readErr := os.ReadFile(src)
			if readErr != nil {
				return nil, fmt.Errorf("read prebuilt image %s: %w", src, readErr)
			}
			if writeErr := os.WriteFile(dest, srcBytes, 0644); writeErr != nil {
				return nil, fmt.Errorf("write image %s: %w", dest, writeErr)
			}
		}

		p.ImageURL = dest
		result[i] = p
	}
	return result, nil
}

// ─────────────────────────────────────────────────────────────────────────────
// AudioClientBatcher adapts an audio.Client into an AudioBatcher interface.
// ─────────────────────────────────────────────────────────────────────────────

// AudioClientBatcher uses a text-to-speech client to generate audio for dialogs.
type AudioClientBatcher struct {
	client  audio.Client
	rootDir string
}

func NewAudioClientBatcher(c audio.Client, rootDir string) *AudioClientBatcher {
	return &AudioClientBatcher{client: c, rootDir: rootDir}
}

// BatchGenerateAudio generates audio for all panels that have dialogue.
func (b *AudioClientBatcher) BatchGenerateAudio(ctx context.Context, panels []domain.Panel, targetDir string) ([]domain.Panel, error) {
	fullDir := filepath.Join(b.rootDir, targetDir)
	if err := os.MkdirAll(fullDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create audio dir %s: %w", fullDir, err)
	}

	result := make([]domain.Panel, len(panels))
	for i, p := range panels {
		result[i] = p

		// Derive spoken text: prefer p.Dialogue; fall back to dialogue_lines text
		// (LLM sometimes fills dialogue_lines but leaves dialogue empty).
		spokenText := p.Dialogue
		if spokenText == "" && len(p.DialogueLines) > 0 {
			var parts []string
			for _, dl := range p.DialogueLines {
				t := strings.TrimSpace(dl.Text)
				// Skip stage directions wrapped in full-width or ASCII parentheses.
				if strings.HasPrefix(t, "（") || strings.HasPrefix(t, "(") {
					continue
				}
				if t != "" {
					parts = append(parts, t)
				}
			}
			spokenText = strings.Join(parts, " ")
		}

		if spokenText == "" {
			logStage("audio", fmt.Sprintf("[%d/%d] scene%d-panel%d  SKIP (no dialogue)", i+1, len(panels), p.SceneNumber, p.PanelNumber))
			continue
		}

		ext := "mp3"
		if ce, ok := b.client.(audio.ClientWithExt); ok {
			ext = ce.FileExt()
		}
		filename := fmt.Sprintf("scene_%d_panel_%d.%s", p.SceneNumber, p.PanelNumber, ext)
		absPath := filepath.Join(fullDir, filename)

		// Resume Logic: also check the other common extension to handle provider switches.
		if info, err := os.Stat(absPath); err == nil && info.Size() > 0 {
			logStage("audio", fmt.Sprintf("[%d/%d] scene%d-panel%d  SKIP (cached)", i+1, len(panels), p.SceneNumber, p.PanelNumber))
			result[i].AudioURL = absPath
			continue
		}
		// Fallback: check the alternative extension so a provider switch doesn't break resume.
		altExt := map[string]string{"mp3": "wav", "wav": "mp3"}[ext]
		if altExt != "" {
			altPath := filepath.Join(fullDir, fmt.Sprintf("scene_%d_panel_%d.%s", p.SceneNumber, p.PanelNumber, altExt))
			if info, err := os.Stat(altPath); err == nil && info.Size() > 0 {
				logStage("audio", fmt.Sprintf("[%d/%d] scene%d-panel%d  SKIP (cached alt ext)", i+1, len(panels), p.SceneNumber, p.PanelNumber))
				result[i].AudioURL = altPath
				continue
			}
		}

		logStage("audio", fmt.Sprintf("[%d/%d] scene%d-panel%d  generating...", i+1, len(panels), p.SceneNumber, p.PanelNumber))
		audioBytes, err := b.client.GenerateSpeech(ctx, spokenText)
		if err != nil {
			return nil, fmt.Errorf("panel %d-%d audio gen failed: %w", p.SceneNumber, p.PanelNumber, err)
		}

		if err := os.WriteFile(absPath, audioBytes, 0644); err != nil {
			return nil, fmt.Errorf("failed to save audio %s: %w", absPath, err)
		}

		logStage("audio", fmt.Sprintf("[%d/%d] scene%d-panel%d  done  size=%dB", i+1, len(panels), p.SceneNumber, p.PanelNumber, len(audioBytes)))
		result[i].AudioURL = absPath
	}
	return result, nil
}

// ─────────────────────────────────────────────────────────────────────────────
// MusicClientBatcher adapts an audio.MusicClient into a MusicBatcher interface.
// ─────────────────────────────────────────────────────────────────────────────

type MusicClientBatcher struct {
	client  audio.MusicClient
	rootDir string
}

func NewMusicClientBatcher(c audio.MusicClient, rootDir string) *MusicClientBatcher {
	return &MusicClientBatcher{client: c, rootDir: rootDir}
}

func (b *MusicClientBatcher) GenerateProjectBGM(ctx context.Context, projectID string, baseTag string, targetDir string) (string, error) {
	fullDir := filepath.Join(b.rootDir, targetDir)
	if err := os.MkdirAll(fullDir, 0755); err != nil {
		return "", fmt.Errorf("failed to create music dir %s: %w", fullDir, err)
	}

	// Check for existing bgm in both mp3 and wav (provider may have changed).
	for _, ext := range []string{"mp3", "wav"} {
		candidate := filepath.Join(fullDir, "bgm."+ext)
		if info, err := os.Stat(candidate); err == nil && info.Size() > 0 {
			return candidate, nil
		}
	}

	if baseTag == "" {
		baseTag = "cinematic"
	}

	audioBytes, err := b.client.SearchAndDownload(ctx, baseTag)
	if err != nil {
		return "", fmt.Errorf("bgm gen failed: %w", err)
	}

	// Detect actual format from magic bytes to choose the right extension.
	ext := "mp3"
	if len(audioBytes) >= 4 && string(audioBytes[:4]) == "RIFF" {
		ext = "wav"
	}
	absPath := filepath.Join(fullDir, "bgm."+ext)

	if err := os.WriteFile(absPath, audioBytes, 0644); err != nil {
		return "", fmt.Errorf("failed to save bgm %s: %w", absPath, err)
	}

	return absPath, nil
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

	// Notify user on stderr
	fmt.Fprintf(os.Stderr, "\n⏸  HITL checkpoint [stage=%s  id=%s]\n", stage, cp.ID)
	fmt.Fprintf(os.Stderr, "   Approve : shand checkpoint approve %s\n", cp.ID)
	fmt.Fprintf(os.Stderr, "   Reject  : shand checkpoint reject  %s\n\n", cp.ID)

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
