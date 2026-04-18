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
	if cfg.Image.Provider != "nanobanana" {
		t.Errorf("Image.Provider = %q, want %q", cfg.Image.Provider, "nanobanana")
	}
	if cfg.Image.Width != 1024 {
		t.Errorf("Image.Width = %d, want 1024", cfg.Image.Width)
	}
	if cfg.Image.Region != "us-west-2" {
		t.Errorf("Image.Region = %q, want %q", cfg.Image.Region, "us-west-2")
	}
	if cfg.Image.Model != "amazon.titan-image-generator-v2:0" {
		t.Errorf("Image.Model = %q, want %q", cfg.Image.Model, "amazon.titan-image-generator-v2:0")
	}
	if cfg.Server.Port != 28080 {
		t.Errorf("Server.Port = %d, want 28080", cfg.Server.Port)
	}
	if cfg.Store.DBPath == "" {
		t.Error("Store.DBPath must not be empty")
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
	if cfg.Image.Provider != "nanobanana" {
		t.Errorf("Image.Provider = %q, want %q", cfg.Image.Provider, "nanobanana")
	}
}
