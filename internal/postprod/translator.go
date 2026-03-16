package postprod

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/baochen10luo/stagenthand/internal/domain"
)

// PromptTranslateSubtitles is the LLM system prompt for subtitle translation.
const PromptTranslateSubtitles = `You are a subtitle translator for a short drama.
Translate ALL dialogue lines to {LANGUAGE}.
Keep speaker names unchanged. Keep emotional nuance.
Input: JSON array of panels with dialogue fields.
Output: same JSON array with translated dialogue and dialogue_lines[].text fields.
Respond with JSON only.`

// Transformer defines the LLM interface needed by SubtitleTranslator.
// It is defined here (in postprod) per Dependency Inversion Principle.
type Transformer interface {
	GenerateTransformation(ctx context.Context, systemPrompt string, input []byte) ([]byte, error)
}

// SubtitleTranslator uses an LLM to translate all panel dialogue in a RemotionProps.
type SubtitleTranslator struct {
	llm Transformer
}

// NewSubtitleTranslator creates a new SubtitleTranslator backed by the given LLM.
func NewSubtitleTranslator(llm Transformer) *SubtitleTranslator {
	return &SubtitleTranslator{llm: llm}
}

// panelPayload is the minimal panel representation sent to the LLM.
type panelPayload struct {
	Scene         int                   `json:"scene"`
	Panel         int                   `json:"panel"`
	Dialogue      string                `json:"dialogue"`
	DialogueLines []domain.DialogueLine `json:"dialogue_lines"`
}

// Translate calls the LLM to translate all dialogue fields to the target language.
// Updates both Panel.Dialogue and Panel.DialogueLines[].Text in place.
// Returns the modified RemotionProps.
func (t *SubtitleTranslator) Translate(ctx context.Context, props domain.RemotionProps, targetLang string) (domain.RemotionProps, error) {
	// Build minimal payload to avoid token waste
	payloads := make([]panelPayload, len(props.Panels))
	for i, p := range props.Panels {
		payloads[i] = panelPayload{
			Scene:         p.SceneNumber,
			Panel:         p.PanelNumber,
			Dialogue:      p.Dialogue,
			DialogueLines: p.DialogueLines,
		}
	}

	inputBytes, err := json.Marshal(payloads)
	if err != nil {
		return props, fmt.Errorf("marshal panel payloads: %w", err)
	}

	// Build prompt with language substituted
	prompt := strings.ReplaceAll(PromptTranslateSubtitles, "{LANGUAGE}", targetLang)

	responseBytes, err := t.llm.GenerateTransformation(ctx, prompt, inputBytes)
	if err != nil {
		return props, fmt.Errorf("LLM translation failed: %w", err)
	}

	// Parse translated response
	var translated []panelPayload
	if err := json.Unmarshal(responseBytes, &translated); err != nil {
		return props, fmt.Errorf("parse LLM response: %w", err)
	}

	// Build a lookup map from scene+panel to translated payload
	type key struct{ scene, panel int }
	lookupMap := make(map[key]panelPayload, len(translated))
	for _, p := range translated {
		lookupMap[key{p.Scene, p.Panel}] = p
	}

	// Apply translations to a copy of props (do not mutate original)
	result := copyProps(props)
	for i := range result.Panels {
		p := &result.Panels[i]
		k := key{p.SceneNumber, p.PanelNumber}
		if tp, ok := lookupMap[k]; ok {
			p.Dialogue = tp.Dialogue
			if tp.DialogueLines != nil {
				p.DialogueLines = tp.DialogueLines
			}
		}
	}

	return result, nil
}
