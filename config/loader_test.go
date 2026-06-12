package config_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/baochen10luo/stagenthand/config"
)

func TestLoad_Defaults(t *testing.T) {
	// Point at a non-existent config so only defaults apply.
	cfg, err := config.Load("testdata/nonexistent.yaml")
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.LLM.Provider != "xai-oauth" {
		t.Errorf("LLM.Provider = %q, want xai-oauth", cfg.LLM.Provider)
	}
	if cfg.LLM.Model != "grok-4.3" {
		t.Errorf("LLM.Model = %q, want grok-4.3", cfg.LLM.Model)
	}
	if cfg.Image.Provider != "mock" {
		t.Errorf("Image.Provider = %q, want %q", cfg.Image.Provider, "mock")
	}
	if cfg.Image.Width != 576 {
		t.Errorf("Image.Width = %d, want 576", cfg.Image.Width)
	}
	if cfg.Image.Height != 1024 {
		t.Errorf("Image.Height = %d, want 1024", cfg.Image.Height)
	}
	if cfg.Server.Port != 28080 {
		t.Errorf("Server.Port = %d, want 28080", cfg.Server.Port)
	}
	if cfg.Store.DBPath == "" {
		t.Error("Store.DBPath must not be empty")
	}
	if cfg.XAI.Model != "grok-4.3" {
		t.Errorf("XAI.Model = %q, want grok-4.3", cfg.XAI.Model)
	}
	if cfg.XAI.BaseURL != "https://api.x.ai/v1" {
		t.Errorf("XAI.BaseURL = %q, want https://api.x.ai/v1", cfg.XAI.BaseURL)
	}
	if cfg.XAI.TokenPath != "~/.hermes/auth.json" {
		t.Errorf("XAI.TokenPath = %q, want ~/.hermes/auth.json", cfg.XAI.TokenPath)
	}
	if cfg.Video.Model != "grok-imagine-video" {
		t.Errorf("Video.Model = %q, want grok-imagine-video", cfg.Video.Model)
	}
	if cfg.Video.Provider != "xai_oauth" {
		t.Errorf("Video.Provider = %q, want xai_oauth", cfg.Video.Provider)
	}
	if !cfg.Video.Enabled {
		t.Error("Video.Enabled = false, want true")
	}
	if cfg.Audio.VoiceProvider != "mock" {
		t.Errorf("Audio.VoiceProvider = %q, want mock", cfg.Audio.VoiceProvider)
	}
	if cfg.Audio.MusicProvider != "mock" {
		t.Errorf("Audio.MusicProvider = %q, want mock", cfg.Audio.MusicProvider)
	}
}

func TestLoad_OverrideFromFile(t *testing.T) {
	dir := t.TempDir()
	cfgFile := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(cfgFile, []byte("image:\n  width: 512\n"), 0644); err != nil {
		t.Fatal(err)
	}

	cfg, err := config.Load(cfgFile)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.Image.Width != 512 {
		t.Errorf("Image.Width = %d, want 512", cfg.Image.Width)
	}
	// Non-overridden fields keep defaults.
	if cfg.Image.Provider != "mock" {
		t.Errorf("Image.Provider = %q, want %q", cfg.Image.Provider, "mock")
	}
}
