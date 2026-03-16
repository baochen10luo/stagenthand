package postprod

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/baochen10luo/stagenthand/internal/domain"
)

// mockAudioBatcher is a test double for AudioBatcher.
type mockAudioBatcher struct {
	callCount int
	panels    []domain.Panel
	err       error
}

func (m *mockAudioBatcher) BatchGenerateAudio(_ context.Context, panels []domain.Panel, _ string) ([]domain.Panel, error) {
	m.callCount++
	if m.err != nil {
		return nil, m.err
	}
	// Return panels with fake audio URLs set
	result := make([]domain.Panel, len(panels))
	for i, p := range panels {
		p.AudioURL = "/fake/audio/" + filepath.Base(p.AudioURL)
		result[i] = p
	}
	return result, nil
}

// TestAudioRegenerator_RegenerateForPanels_DeletesAndRegenerates verifies files are deleted and batcher is called.
func TestAudioRegenerator_RegenerateForPanels_DeletesAndRegenerates(t *testing.T) {
	// Create temp dir to simulate audio files
	tmpDir := t.TempDir()

	// Create fake mp3 files
	panel1File := filepath.Join(tmpDir, "scene_1_panel_1.mp3")
	panel2File := filepath.Join(tmpDir, "scene_1_panel_2.mp3")
	if err := os.WriteFile(panel1File, []byte("fake audio"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(panel2File, []byte("fake audio"), 0644); err != nil {
		t.Fatal(err)
	}

	batcher := &mockAudioBatcher{}
	regen := &AudioRegenerator{
		batcher: batcher,
		rootDir: tmpDir,
	}

	panels := []domain.Panel{
		{SceneNumber: 1, PanelNumber: 1, Dialogue: "Hello world", AudioURL: panel1File},
		{SceneNumber: 1, PanelNumber: 2, Dialogue: "Goodbye world", AudioURL: panel2File},
	}

	_, err := regen.RegenerateForPanels(context.Background(), panels, "test-project")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Files should be deleted before regeneration
	if _, err := os.Stat(panel1File); !os.IsNotExist(err) {
		t.Error("expected panel1 audio file to be deleted before regeneration")
	}
	if _, err := os.Stat(panel2File); !os.IsNotExist(err) {
		t.Error("expected panel2 audio file to be deleted before regeneration")
	}

	// Batcher should have been called
	if batcher.callCount != 1 {
		t.Errorf("expected batcher to be called once, got %d", batcher.callCount)
	}
}

// TestAudioRegenerator_RegenerateForPanels_SkipsEmptyDialogue verifies panels with no dialogue are skipped.
func TestAudioRegenerator_RegenerateForPanels_SkipsEmptyDialogue(t *testing.T) {
	tmpDir := t.TempDir()

	batcher := &mockAudioBatcher{}
	regen := &AudioRegenerator{
		batcher: batcher,
		rootDir: tmpDir,
	}

	panels := []domain.Panel{
		{SceneNumber: 1, PanelNumber: 1, Dialogue: ""},  // empty dialogue — should be skipped
		{SceneNumber: 1, PanelNumber: 2, Dialogue: "Hello"},
	}

	result, err := regen.RegenerateForPanels(context.Background(), panels, "test-project")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Result should contain both panels
	if len(result) != 2 {
		t.Errorf("expected 2 panels, got %d", len(result))
	}

	// Batcher should still be called (with only the non-empty panel)
	if batcher.callCount != 1 {
		t.Errorf("expected batcher to be called once, got %d", batcher.callCount)
	}
}
