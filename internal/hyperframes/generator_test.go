package hyperframes

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/baochen10luo/stagenthand/internal/domain"
)

func TestGenerateProject(t *testing.T) {
	props := domain.RemotionProps{
		ProjectID: "test-proj",
		Title:     "Test Drama",
		FPS:       24,
		Width:     576,
		Height:    1024,
		BGMURL:    "/shand/test-proj/audio/bgm.mp3",
		Panels: []domain.Panel{
			{
				SceneNumber: 1,
				PanelNumber: 1,
				ImageURL:    "/shand/test-proj/images/scene_1_panel_1.png",
				AudioURL:    "/shand/test-proj/audio/tts_1.mp3",
				DurationSec: 3.0,
				Dialogue:    "這是第一句話",
			},
			{
				SceneNumber: 1,
				PanelNumber: 2,
				ImageURL:    "/shand/test-proj/images/scene_1_panel_2.png",
				AudioURL:    "/shand/test-proj/audio/tts_2.mp3",
				DurationSec: 4.0,
				DialogueLines: []domain.DialogueLine{
					{Text: "Line A", StartSec: 0.0, EndSec: 2.0},
					{Text: "Line B", StartSec: 2.0, EndSec: 4.0},
				},
			},
		},
	}

	tmpDir := t.TempDir()
	cfg := Config{ShandHome: "/home/user/.shand", DryRun: false}

	if err := GenerateProject(props, tmpDir, cfg); err != nil {
		t.Fatalf("GenerateProject: %v", err)
	}

	// index.html must exist
	htmlPath := filepath.Join(tmpDir, "index.html")
	data, err := os.ReadFile(htmlPath)
	if err != nil {
		t.Fatalf("index.html not written: %v", err)
	}
	html := string(data)

	// data-composition-id and data-duration
	if !strings.Contains(html, `data-composition-id="short-drama"`) {
		t.Error("missing data-composition-id")
	}
	if !strings.Contains(html, `data-duration="7.000"`) {
		t.Errorf("expected data-duration=7.000 (3+4), got:\n%s", html)
	}
	if !strings.Contains(html, `data-width="576"`) {
		t.Error("missing data-width")
	}

	// Two scene divs
	if !strings.Contains(html, `id="scene-0"`) || !strings.Contains(html, `id="scene-1"`) {
		t.Error("missing scene divs")
	}

	// Resolved image paths
	if !strings.Contains(html, `/home/user/.shand/projects/test-proj/images/scene_1_panel_1.png`) {
		t.Error("image path not resolved for panel 0")
	}

	// HyperFrames seek registration
	if !strings.Contains(html, `window.__hf`) {
		t.Error("missing window.__hf registration")
	}

	// GSAP timeline
	if !strings.Contains(html, `gsap.timeline`) {
		t.Error("missing gsap.timeline")
	}

	// Subtitle text from panel 0 (Dialogue fallback)
	if !strings.Contains(html, "這是第一句話") {
		t.Error("missing subtitle text from panel 0")
	}

	// Subtitle lines from panel 1 (DialogueLines)
	if !strings.Contains(html, "Line A") || !strings.Contains(html, "Line B") {
		t.Error("missing subtitle lines from panel 1")
	}

	// package.json must exist
	if _, err := os.Stat(filepath.Join(tmpDir, "package.json")); err != nil {
		t.Error("package.json not written")
	}
}

func TestPrepareResolvedPanels_defaults(t *testing.T) {
	props := domain.RemotionProps{
		Panels: []domain.Panel{
			{ImageURL: "/shand/p/images/a.png", DurationSec: 0}, // zero → default
			{ImageURL: "/abs/b.png", DurationSec: 5.0},
		},
	}
	panels, total := prepareResolvedPanels(props, "/shand-home")

	if len(panels) != 2 {
		t.Fatalf("expected 2 panels, got %d", len(panels))
	}
	if panels[0].DurationSec != 3.0 {
		t.Errorf("panel 0 DurationSec: got %.1f, want 3.0", panels[0].DurationSec)
	}
	if panels[1].StartSec != 3.0 {
		t.Errorf("panel 1 StartSec: got %.1f, want 3.0", panels[1].StartSec)
	}
	if total != 8.0 {
		t.Errorf("total: got %.1f, want 8.0", total)
	}
	if panels[0].MotionEffect != "ken_burns_in" {
		t.Errorf("default motion effect: got %q", panels[0].MotionEffect)
	}
}
