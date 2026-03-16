package postprod

import (
	"testing"

	"github.com/baochen10luo/stagenthand/internal/domain"
)

func makeTestProps() domain.RemotionProps {
	return domain.RemotionProps{
		ProjectID: "test-proj",
		Title:     "Test Drama",
		Panels: []domain.Panel{
			{
				SceneNumber: 1,
				PanelNumber: 1,
				Dialogue:    "Hello world",
				DialogueLines: []domain.DialogueLine{
					{Speaker: "Alice", Text: "Hello world", Emotion: "happy"},
				},
				ImageURL: "images/scene_1_panel_1.png",
				AudioURL: "audio/scene_1_panel_1.mp3",
			},
			{
				SceneNumber: 1,
				PanelNumber: 2,
				Dialogue:    "Goodbye world",
				DialogueLines: []domain.DialogueLine{
					{Speaker: "Bob", Text: "Goodbye world", Emotion: "sad"},
				},
				ImageURL: "images/scene_1_panel_2.png",
				AudioURL: "audio/scene_1_panel_2.mp3",
			},
			{
				SceneNumber: 2,
				PanelNumber: 1,
				Dialogue:    "Scene two begins",
				DialogueLines: []domain.DialogueLine{
					{Speaker: "Alice", Text: "Scene two begins", Emotion: "neutral"},
				},
				ImageURL: "images/scene_2_panel_1.png",
				AudioURL: "audio/scene_2_panel_1.mp3",
			},
		},
	}
}

// TestSubtitlePatcher_PatchPanel_Found verifies that an existing panel's dialogue is updated.
func TestSubtitlePatcher_PatchPanel_Found(t *testing.T) {
	props := makeTestProps()
	patcher := &SubtitlePatcher{}

	newLines := []domain.DialogueLine{
		{Speaker: "Alice", Text: "Updated text", Emotion: "neutral"},
	}

	err := patcher.PatchPanel(&props, 1, 1, "Updated text", newLines)
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}

	panel := props.Panels[0]
	if panel.Dialogue != "Updated text" {
		t.Errorf("expected Dialogue=%q, got %q", "Updated text", panel.Dialogue)
	}
	if len(panel.DialogueLines) != 1 {
		t.Fatalf("expected 1 dialogue line, got %d", len(panel.DialogueLines))
	}
	if panel.DialogueLines[0].Text != "Updated text" {
		t.Errorf("expected DialogueLines[0].Text=%q, got %q", "Updated text", panel.DialogueLines[0].Text)
	}
	if panel.DialogueLines[0].Speaker != "Alice" {
		t.Errorf("expected Speaker=Alice, got %q", panel.DialogueLines[0].Speaker)
	}
}

// TestSubtitlePatcher_PatchPanel_NotFound verifies that an error is returned for a non-existent panel.
func TestSubtitlePatcher_PatchPanel_NotFound(t *testing.T) {
	props := makeTestProps()
	patcher := &SubtitlePatcher{}

	err := patcher.PatchPanel(&props, 99, 99, "text", nil)
	if err == nil {
		t.Fatal("expected error for non-existent panel, got nil")
	}
}

// TestSubtitlePatcher_PatchAllFromMap verifies that multiple panels are updated correctly.
func TestSubtitlePatcher_PatchAllFromMap(t *testing.T) {
	props := makeTestProps()
	patcher := &SubtitlePatcher{}

	patches := map[string]string{
		"scene_1_panel_1": "Patched line one",
		"scene_2_panel_1": "Patched scene two",
	}

	err := patcher.PatchAllFromMap(&props, patches)
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}

	// Verify scene 1 panel 1 was updated
	if props.Panels[0].Dialogue != "Patched line one" {
		t.Errorf("scene_1_panel_1: expected %q, got %q", "Patched line one", props.Panels[0].Dialogue)
	}
	// Verify scene 1 panel 2 was NOT updated
	if props.Panels[1].Dialogue != "Goodbye world" {
		t.Errorf("scene_1_panel_2 should be unchanged, got %q", props.Panels[1].Dialogue)
	}
	// Verify scene 2 panel 1 was updated
	if props.Panels[2].Dialogue != "Patched scene two" {
		t.Errorf("scene_2_panel_1: expected %q, got %q", "Patched scene two", props.Panels[2].Dialogue)
	}
}

// TestSubtitlePatcher_PatchAllFromMap_InvalidKey verifies that an invalid key returns an error.
func TestSubtitlePatcher_PatchAllFromMap_InvalidKey(t *testing.T) {
	props := makeTestProps()
	patcher := &SubtitlePatcher{}

	patches := map[string]string{
		"bad_key_format": "some text",
	}

	err := patcher.PatchAllFromMap(&props, patches)
	if err == nil {
		t.Fatal("expected error for invalid key format, got nil")
	}
}
