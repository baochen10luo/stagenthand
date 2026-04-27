package cmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/baochen10luo/stagenthand/internal/domain"
)

// --- mergeNotionEdits tests ---

func TestMergeNotionEdits_EqualLength_AllDialoguesUpdated(t *testing.T) {
	manifest := []domain.Panel{
		{SceneNumber: 1, PanelNumber: 1, Dialogue: "original 1", Description: "desc 1"},
		{SceneNumber: 1, PanelNumber: 2, Dialogue: "original 2", Description: "desc 2"},
	}
	notion := []domain.Panel{
		{Dialogue: "updated 1", Description: "new desc 1"},
		{Dialogue: "updated 2", Description: "new desc 2"},
	}

	result := mergeNotionEdits(manifest, notion)

	if len(result) != 2 {
		t.Fatalf("expected 2 panels, got %d", len(result))
	}
	if result[0].Dialogue != "updated 1" {
		t.Errorf("panel 0: expected dialogue 'updated 1', got %q", result[0].Dialogue)
	}
	if result[1].Dialogue != "updated 2" {
		t.Errorf("panel 1: expected dialogue 'updated 2', got %q", result[1].Dialogue)
	}
	if result[0].Description != "new desc 1" {
		t.Errorf("panel 0: expected description 'new desc 1', got %q", result[0].Description)
	}
}

func TestMergeNotionEdits_NotionShorter_ExtraManifestPanelsKept(t *testing.T) {
	manifest := []domain.Panel{
		{SceneNumber: 1, PanelNumber: 1, Dialogue: "original 1", Description: "desc 1"},
		{SceneNumber: 1, PanelNumber: 2, Dialogue: "original 2", Description: "desc 2"},
		{SceneNumber: 2, PanelNumber: 1, Dialogue: "original 3", Description: "desc 3"},
	}
	notion := []domain.Panel{
		{Dialogue: "updated 1", Description: "new desc 1"},
	}

	result := mergeNotionEdits(manifest, notion)

	if len(result) != 3 {
		t.Fatalf("expected 3 panels (manifest length), got %d", len(result))
	}
	// First panel updated from notion
	if result[0].Dialogue != "updated 1" {
		t.Errorf("panel 0: expected 'updated 1', got %q", result[0].Dialogue)
	}
	// Extra manifest panels preserved
	if result[1].Dialogue != "original 2" {
		t.Errorf("panel 1: expected 'original 2', got %q", result[1].Dialogue)
	}
	if result[2].Dialogue != "original 3" {
		t.Errorf("panel 2: expected 'original 3', got %q", result[2].Dialogue)
	}
}

func TestMergeNotionEdits_EmptyNotionDialogue_KeepsManifest(t *testing.T) {
	manifest := []domain.Panel{
		{SceneNumber: 1, PanelNumber: 1, Dialogue: "original 1"},
	}
	notion := []domain.Panel{
		{Dialogue: "", Description: "new desc"},
	}

	result := mergeNotionEdits(manifest, notion)

	if result[0].Dialogue != "original 1" {
		t.Errorf("expected manifest dialogue 'original 1' kept when notion is empty, got %q", result[0].Dialogue)
	}
	if result[0].Description != "new desc" {
		t.Errorf("expected description updated to 'new desc', got %q", result[0].Description)
	}
}

func TestMergeNotionEdits_EmptyNotionDescription_KeepsManifest(t *testing.T) {
	manifest := []domain.Panel{
		{SceneNumber: 1, PanelNumber: 1, Dialogue: "hello", Description: "original desc"},
	}
	notion := []domain.Panel{
		{Dialogue: "updated", Description: ""},
	}

	result := mergeNotionEdits(manifest, notion)

	if result[0].Description != "original desc" {
		t.Errorf("expected manifest description 'original desc' kept when notion is empty, got %q", result[0].Description)
	}
	if result[0].Dialogue != "updated" {
		t.Errorf("expected dialogue updated to 'updated', got %q", result[0].Dialogue)
	}
}

func TestMergeNotionEdits_ImageURLAlwaysFromManifest(t *testing.T) {
	manifest := []domain.Panel{
		{SceneNumber: 1, PanelNumber: 1, ImageURL: "local/image.png", Dialogue: "hello"},
	}
	notion := []domain.Panel{
		{Dialogue: "updated", ImageURL: "https://remote/image.png"},
	}

	result := mergeNotionEdits(manifest, notion)

	if result[0].ImageURL != "local/image.png" {
		t.Errorf("expected local ImageURL 'local/image.png', got %q", result[0].ImageURL)
	}
}

// --- validateImagePaths tests ---

func TestValidateImagePaths_AllExist_ReturnsNil(t *testing.T) {
	dir := t.TempDir()
	img1 := filepath.Join(dir, "image1.png")
	img2 := filepath.Join(dir, "image2.png")
	_ = os.WriteFile(img1, []byte("data"), 0644)
	_ = os.WriteFile(img2, []byte("data"), 0644)

	panels := []domain.Panel{
		{SceneNumber: 1, PanelNumber: 1, ImageURL: img1},
		{SceneNumber: 1, PanelNumber: 2, ImageURL: img2},
	}

	err := validateImagePaths(panels)
	if err != nil {
		t.Errorf("expected nil error, got: %v", err)
	}
}

func TestValidateImagePaths_OneMissing_ReturnsError(t *testing.T) {
	dir := t.TempDir()
	img1 := filepath.Join(dir, "exists.png")
	_ = os.WriteFile(img1, []byte("data"), 0644)
	missing := filepath.Join(dir, "missing.png")

	panels := []domain.Panel{
		{SceneNumber: 1, PanelNumber: 1, ImageURL: img1},
		{SceneNumber: 1, PanelNumber: 2, ImageURL: missing},
	}

	err := validateImagePaths(panels)
	if err == nil {
		t.Fatal("expected error for missing image, got nil")
	}
	if !strings.Contains(err.Error(), "missing.png") {
		t.Errorf("expected error to mention 'missing.png', got: %v", err)
	}
}

func TestValidateImagePaths_EmptyImageURL_Skipped(t *testing.T) {
	panels := []domain.Panel{
		{SceneNumber: 1, PanelNumber: 1, ImageURL: ""},
		{SceneNumber: 1, PanelNumber: 2, ImageURL: ""},
	}

	err := validateImagePaths(panels)
	if err != nil {
		t.Errorf("expected nil error for empty ImageURL panels, got: %v", err)
	}
}

func TestValidateImagePaths_MultipleMissing_ListsAll(t *testing.T) {
	dir := t.TempDir()
	panels := []domain.Panel{
		{SceneNumber: 1, PanelNumber: 1, ImageURL: filepath.Join(dir, "a.png")},
		{SceneNumber: 1, PanelNumber: 2, ImageURL: filepath.Join(dir, "b.png")},
	}

	err := validateImagePaths(panels)
	if err == nil {
		t.Fatal("expected error for multiple missing images, got nil")
	}
	if !strings.Contains(err.Error(), "a.png") || !strings.Contains(err.Error(), "b.png") {
		t.Errorf("expected error to mention both missing files, got: %v", err)
	}
}
