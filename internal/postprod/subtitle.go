package postprod

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/baochen10luo/stagenthand/internal/domain"
)

// SubtitlePatcher patches dialogue on a RemotionProps in memory.
// It updates Panel.Dialogue and Panel.DialogueLines together.
type SubtitlePatcher struct{}

// PatchPanel replaces the dialogue of the panel at (sceneNumber, panelNumber).
// Returns an error if the panel is not found.
func (s *SubtitlePatcher) PatchPanel(props *domain.RemotionProps, sceneNum, panelNum int, dialogue string, lines []domain.DialogueLine) error {
	for i := range props.Panels {
		p := &props.Panels[i]
		if p.SceneNumber == sceneNum && p.PanelNumber == panelNum {
			p.Dialogue = dialogue
			if lines != nil {
				p.DialogueLines = lines
			}
			return nil
		}
	}
	return fmt.Errorf("panel not found: scene=%d panel=%d", sceneNum, panelNum)
}

// PatchAllFromMap replaces dialogue for multiple panels at once.
// key = "scene_N_panel_M", value = new dialogue string.
func (s *SubtitlePatcher) PatchAllFromMap(props *domain.RemotionProps, patches map[string]string) error {
	for key, dialogue := range patches {
		sceneNum, panelNum, err := parseScenePanelKey(key)
		if err != nil {
			return fmt.Errorf("invalid patch key %q: %w", key, err)
		}
		if err := s.PatchPanel(props, sceneNum, panelNum, dialogue, nil); err != nil {
			return fmt.Errorf("patch %q: %w", key, err)
		}
	}
	return nil
}

// parseScenePanelKey parses a key of the form "scene_N_panel_M" and returns (sceneNum, panelNum, error).
func parseScenePanelKey(key string) (int, int, error) {
	// Expected format: "scene_N_panel_M"
	parts := strings.Split(key, "_")
	if len(parts) != 4 || parts[0] != "scene" || parts[2] != "panel" {
		return 0, 0, fmt.Errorf("expected format scene_N_panel_M")
	}
	sceneNum, err := strconv.Atoi(parts[1])
	if err != nil {
		return 0, 0, fmt.Errorf("invalid scene number %q", parts[1])
	}
	panelNum, err := strconv.Atoi(parts[3])
	if err != nil {
		return 0, 0, fmt.Errorf("invalid panel number %q", parts[3])
	}
	return sceneNum, panelNum, nil
}
