package pipeline_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/baochen10luo/stagenthand/internal/domain"
	"github.com/baochen10luo/stagenthand/internal/pipeline"
)

type mockAudioClient struct {
	err error
}

func (m *mockAudioClient) GenerateSpeech(ctx context.Context, text string) ([]byte, error) {
	if m.err != nil {
		return nil, m.err
	}
	return []byte("fake-audio"), nil
}

type mockMusicClient struct {
	err error
}

func (m *mockMusicClient) SearchAndDownload(ctx context.Context, tag string) ([]byte, error) {
	if m.err != nil {
		return nil, m.err
	}
	return []byte("fake-bgm"), nil
}

func TestAudioClientBatcher(t *testing.T) {
	tmpDir := t.TempDir()
	client := &mockAudioClient{}
	batcher := pipeline.NewAudioClientBatcher(client, tmpDir)

	panels := []domain.Panel{
		{SceneNumber: 1, PanelNumber: 1, Dialogue: "Hello"},
		{SceneNumber: 1, PanelNumber: 2, Dialogue: ""}, // no dialogue
	}

	res, err := batcher.BatchGenerateAudio(context.Background(), panels, "audio")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(res) != 2 {
		t.Errorf("expected 2 panels, got %d", len(res))
	}
	if res[0].AudioURL == "" {
		t.Error("expected AudioURL to be populated for panel with dialogue")
	}
	if res[1].AudioURL != "" {
		t.Error("expected AudioURL to be empty for panel without dialogue")
	}
	
	// Test Resume Logic
	res2, err := batcher.BatchGenerateAudio(context.Background(), panels, "audio")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res2[0].AudioURL == "" {
		t.Error("expected AudioURL to be populated from cache")
	}
}

func TestAudioClientBatcher_Error(t *testing.T) {
	tmpDir := t.TempDir()
	client := &mockAudioClient{err: errors.New("TTS failed")}
	batcher := pipeline.NewAudioClientBatcher(client, tmpDir)

	panels := []domain.Panel{{SceneNumber: 1, PanelNumber: 1, Dialogue: "Hello"}}
	_, err := batcher.BatchGenerateAudio(context.Background(), panels, "audio")
	if err == nil {
		t.Error("expected error, got nil")
	}
}

func TestMusicClientBatcher(t *testing.T) {
	tmpDir := t.TempDir()
	client := &mockMusicClient{}
	batcher := pipeline.NewMusicClientBatcher(client, tmpDir)

	// Generate BGM
	url, err := batcher.GenerateProjectBGM(context.Background(), "job-1", "cinematic", "music")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if url == "" {
		t.Error("expected BGM URL to be populated")
	}

	// Test Resume Logic
	url2, err := batcher.GenerateProjectBGM(context.Background(), "job-1", "cinematic", "music")
	if err != nil {
		t.Fatalf("unexpected error on resume: %v", err)
	}
	if url != url2 {
		t.Errorf("expected %q, got %q", url, url2)
	}
}

func TestMusicClientBatcher_Error(t *testing.T) {
	tmpDir := t.TempDir()
	client := &mockMusicClient{err: errors.New("Music failed")}
	batcher := pipeline.NewMusicClientBatcher(client, tmpDir)

	_, err := batcher.GenerateProjectBGM(context.Background(), "job-2", "", "music")
	if err == nil {
		t.Error("expected error, got nil")
	}
}

func TestImageClientBatcher_Resume(t *testing.T) {
	tmpDir := t.TempDir()
	cacheDir := filepath.Join(tmpDir, "images")
	err := os.MkdirAll(cacheDir, 0755)
	if err != nil {
		t.Fatal(err)
	}

	fakeFile := filepath.Join(cacheDir, "scene_1_panel_1.png")
	// pre-populate file with valid PNG magic bytes so isValidImageFile returns true.
	pngMagic := []byte{0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A}
	os.WriteFile(fakeFile, pngMagic, 0644)

	// Since we mock it, if GenerateImage is called it will fail because client is nil here, but we shouldn't reach it.
	batcher := pipeline.NewImageClientBatcher(nil, tmpDir)
	panels := []domain.Panel{{SceneNumber: 1, PanelNumber: 1, Description: "desc"}}
	
	res, err := batcher.BatchGenerateImages(context.Background(), panels, "images")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res[0].ImageURL != fakeFile {
		t.Errorf("expected ImageURL %s from cache, got %s", fakeFile, res[0].ImageURL)
	}
}
