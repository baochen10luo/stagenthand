package postprod

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/baochen10luo/stagenthand/internal/domain"
)

// TestSubtitleTranslator_Translate_CallsLLM verifies that LLM receives panel dialogue data.
func TestSubtitleTranslator_Translate_CallsLLM(t *testing.T) {
	props := makeTestProps()

	// Build a response that mimics the translated JSON the LLM would return
	type panelPayload struct {
		Scene         int                    `json:"scene"`
		Panel         int                    `json:"panel"`
		Dialogue      string                 `json:"dialogue"`
		DialogueLines []domain.DialogueLine  `json:"dialogue_lines"`
	}

	translated := []panelPayload{
		{Scene: 1, Panel: 1, Dialogue: "你好世界", DialogueLines: []domain.DialogueLine{{Speaker: "Alice", Text: "你好世界", Emotion: "happy"}}},
		{Scene: 1, Panel: 2, Dialogue: "再見世界", DialogueLines: []domain.DialogueLine{{Speaker: "Bob", Text: "再見世界", Emotion: "sad"}}},
		{Scene: 2, Panel: 1, Dialogue: "第二場景開始", DialogueLines: []domain.DialogueLine{{Speaker: "Alice", Text: "第二場景開始", Emotion: "neutral"}}},
	}
	respBytes, _ := json.Marshal(translated)

	var capturedPrompt string
	var capturedInput []byte
	mockLLM := &capturingTransformer{
		response: respBytes,
		onCall: func(prompt string, input []byte) {
			capturedPrompt = prompt
			capturedInput = input
		},
	}

	translator := &SubtitleTranslator{llm: mockLLM}
	_, err := translator.Translate(context.Background(), props, "zh-TW")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if capturedPrompt == "" {
		t.Error("expected LLM to be called with a prompt")
	}
	if !strings.Contains(capturedPrompt, "zh-TW") {
		t.Errorf("expected prompt to contain target language 'zh-TW', got: %s", capturedPrompt)
	}
	if len(capturedInput) == 0 {
		t.Error("expected LLM to receive input data")
	}
	// Verify input contains dialogue
	if !strings.Contains(string(capturedInput), "Hello world") {
		t.Errorf("expected input to contain dialogue 'Hello world', got: %s", string(capturedInput))
	}
}

// TestSubtitleTranslator_Translate_ParsesResult verifies that LLM output is applied to props.
func TestSubtitleTranslator_Translate_ParsesResult(t *testing.T) {
	props := makeTestProps()

	type panelPayload struct {
		Scene         int                    `json:"scene"`
		Panel         int                    `json:"panel"`
		Dialogue      string                 `json:"dialogue"`
		DialogueLines []domain.DialogueLine  `json:"dialogue_lines"`
	}

	translated := []panelPayload{
		{Scene: 1, Panel: 1, Dialogue: "你好世界", DialogueLines: []domain.DialogueLine{{Speaker: "Alice", Text: "你好世界", Emotion: "happy"}}},
		{Scene: 1, Panel: 2, Dialogue: "再見世界", DialogueLines: []domain.DialogueLine{{Speaker: "Bob", Text: "再見世界", Emotion: "sad"}}},
		{Scene: 2, Panel: 1, Dialogue: "第二場景開始", DialogueLines: []domain.DialogueLine{{Speaker: "Alice", Text: "第二場景開始", Emotion: "neutral"}}},
	}
	respBytes, _ := json.Marshal(translated)

	mockLLM := &MockLLMClient{Response: respBytes}
	translator := &SubtitleTranslator{llm: mockLLM}

	result, err := translator.Translate(context.Background(), props, "zh-TW")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result.Panels[0].Dialogue != "你好世界" {
		t.Errorf("expected translated dialogue '你好世界', got %q", result.Panels[0].Dialogue)
	}
	if result.Panels[1].Dialogue != "再見世界" {
		t.Errorf("expected translated dialogue '再見世界', got %q", result.Panels[1].Dialogue)
	}
	if result.Panels[2].Dialogue != "第二場景開始" {
		t.Errorf("expected translated dialogue '第二場景開始', got %q", result.Panels[2].Dialogue)
	}
	if result.Panels[0].DialogueLines[0].Text != "你好世界" {
		t.Errorf("expected translated dialogue_lines text '你好世界', got %q", result.Panels[0].DialogueLines[0].Text)
	}
}

// TestSubtitleTranslator_Translate_PreservesImages verifies that image_url and audio_url are unchanged.
func TestSubtitleTranslator_Translate_PreservesImages(t *testing.T) {
	props := makeTestProps()

	type panelPayload struct {
		Scene         int                    `json:"scene"`
		Panel         int                    `json:"panel"`
		Dialogue      string                 `json:"dialogue"`
		DialogueLines []domain.DialogueLine  `json:"dialogue_lines"`
	}

	translated := []panelPayload{
		{Scene: 1, Panel: 1, Dialogue: "你好世界", DialogueLines: []domain.DialogueLine{{Speaker: "Alice", Text: "你好世界"}}},
		{Scene: 1, Panel: 2, Dialogue: "再見世界", DialogueLines: []domain.DialogueLine{{Speaker: "Bob", Text: "再見世界"}}},
		{Scene: 2, Panel: 1, Dialogue: "第二場景開始", DialogueLines: []domain.DialogueLine{{Speaker: "Alice", Text: "第二場景開始"}}},
	}
	respBytes, _ := json.Marshal(translated)

	mockLLM := &MockLLMClient{Response: respBytes}
	translator := &SubtitleTranslator{llm: mockLLM}

	result, err := translator.Translate(context.Background(), props, "zh-TW")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// image_url and audio_url should be unchanged
	if result.Panels[0].ImageURL != "images/scene_1_panel_1.png" {
		t.Errorf("ImageURL should be preserved, got %q", result.Panels[0].ImageURL)
	}
	if result.Panels[0].AudioURL != "audio/scene_1_panel_1.mp3" {
		t.Errorf("AudioURL should be preserved, got %q", result.Panels[0].AudioURL)
	}
}

// capturingTransformer is a test helper that captures the last call.
type capturingTransformer struct {
	response []byte
	err      error
	onCall   func(prompt string, input []byte)
}

func (c *capturingTransformer) GenerateTransformation(_ context.Context, prompt string, input []byte) ([]byte, error) {
	if c.onCall != nil {
		c.onCall(prompt, input)
	}
	return c.response, c.err
}
