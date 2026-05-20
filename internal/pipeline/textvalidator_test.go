package pipeline

import (
	"context"
	"testing"

	"github.com/baochen10luo/stagenthand/internal/domain"
)

func TestLLMPanelTextValidator_Validate(t *testing.T) {
	t.Run("all panels pass", func(t *testing.T) {
		mock := &mockLLMClient{
			response: `{"validations": [
				{"scene_number": 1, "panel_number": 1, "score": 5, "issue": ""},
				{"scene_number": 1, "panel_number": 2, "score": 5, "issue": ""}
			]}`,
		}
		v := NewLLMPanelTextValidator(mock)

		panels := []domain.Panel{
			{SceneNumber: 1, PanelNumber: 1, Description: "a robot in a forest", Dialogue: "I love this forest"},
			{SceneNumber: 1, PanelNumber: 2, Description: "sunset over mountains", Dialogue: "What a beautiful view"},
		}

		result, err := v.Validate(context.Background(), panels)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !result.OK {
			t.Errorf("expected OK=true, got false with issues: %v", result.Issues)
		}
	})

	t.Run("some panels have issues", func(t *testing.T) {
		mock := &mockLLMClient{
			response: `{"validations": [
				{"scene_number": 1, "panel_number": 1, "score": 5, "issue": ""},
				{"scene_number": 1, "panel_number": 2, "score": 2, "issue": "Description shows city but dialogue mentions ocean"}
			]}`,
		}
		v := NewLLMPanelTextValidator(mock)

		panels := []domain.Panel{
			{SceneNumber: 1, PanelNumber: 1, Description: "city street", Dialogue: "I love this city"},
			{SceneNumber: 1, PanelNumber: 2, Description: "city street", Dialogue: "The ocean is so blue"},
		}

		result, err := v.Validate(context.Background(), panels)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if result.OK {
			t.Error("expected OK=false due to panel 2 issue")
		}
		if len(result.Issues) != 1 {
			t.Fatalf("expected 1 issue, got %d", len(result.Issues))
		}
		if result.Issues[0].PanelNumber != 2 {
			t.Errorf("expected issue on panel 2, got panel %d", result.Issues[0].PanelNumber)
		}
	})

	t.Run("empty panels returns OK", func(t *testing.T) {
		mock := &mockLLMClient{
			response: `{"validations": []}`,
		}
		v := NewLLMPanelTextValidator(mock)

		result, err := v.Validate(context.Background(), nil)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !result.OK {
			t.Error("expected OK=true for empty panels")
		}
	})

	t.Run("LLM returns invalid JSON", func(t *testing.T) {
		mock := &mockLLMClient{
			response: `not json`,
		}
		v := NewLLMPanelTextValidator(mock)

		panels := []domain.Panel{
			{SceneNumber: 1, PanelNumber: 1, Description: "test", Dialogue: "test"},
		}

		_, err := v.Validate(context.Background(), panels)
		if err == nil {
			t.Error("expected error for invalid JSON response")
		}
	})

	t.Run("LLM returns markdown-fenced JSON", func(t *testing.T) {
		mock := &mockLLMClient{
			response: "```json\n{\"validations\": [{\"scene_number\": 1, \"panel_number\": 1, \"score\": 4, \"issue\": \"\"}]}\n```",
		}
		v := NewLLMPanelTextValidator(mock)

		panels := []domain.Panel{
			{SceneNumber: 1, PanelNumber: 1, Description: "robot", Dialogue: "beep boop"},
		}

		result, err := v.Validate(context.Background(), panels)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !result.OK {
			t.Errorf("expected OK=true for score 4, got issues: %v", result.Issues)
		}
	})

	t.Run("LLM returns error", func(t *testing.T) {
		mock := &mockLLMClient{err: assertError("LLM unavailable")}
		v := NewLLMPanelTextValidator(mock)

		panels := []domain.Panel{
			{SceneNumber: 1, PanelNumber: 1, Description: "test", Dialogue: "test"},
		}

		result, err := v.Validate(context.Background(), panels)
		if err != nil {
			t.Fatalf("expected no error when LLM fails (validation is advisory): %v", err)
		}
		if !result.OK {
			t.Error("expected OK=true (graceful degradation when LLM unavailable)")
		}
		if len(result.Issues) != 0 {
			t.Errorf("expected no issues when LLM fails, got %d", len(result.Issues))
		}
	})
}

type mockLLMClient struct {
	response string
	err      error
}

func (m *mockLLMClient) GenerateTransformation(_ context.Context, _ string, _ []byte) ([]byte, error) {
	if m.err != nil {
		return nil, m.err
	}
	return []byte(m.response), nil
}

type assertError string

func (e assertError) Error() string { return string(e) }
