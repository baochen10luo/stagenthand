package pipeline

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"regexp"
	"strings"

	"github.com/baochen10luo/stagenthand/internal/domain"
)

// textLLMClient is the minimal interface the validator needs from an LLM client.
// Satisfied by llm.Client, llm.MockClient, etc.
type textLLMClient interface {
	GenerateTransformation(ctx context.Context, systemPrompt string, inputData []byte) ([]byte, error)
}

// LLMPanelTextValidator uses an LLM to validate Description ↔ Dialogue alignment.
type LLMPanelTextValidator struct {
	client textLLMClient
}

const panelTextValidatorPrompt = `You are a storyboard consistency checker. Check each panel's visual description against its spoken dialogue.

For each panel:
- Does the description reflect the setting, objects, and characters mentioned in the dialogue?
- Is the mood or atmosphere consistent between description and dialogue?

Output ONLY valid JSON with this exact structure:
{"validations": [{"scene_number": int, "panel_number": int, "score": int, "issue": string}]}

Score guide:
5 = perfect match — description fully reflects dialogue
4 = good match — minor elements missing but overall consistent
3 = partial match — some key elements from dialogue missing in description
2 = poor match — description doesn't adequately reflect dialogue
1 = complete mismatch — description contradicts dialogue

Only include "issue" text when score ≤ 3. Omit or use empty string when score ≥ 4.`

// NewLLMPanelTextValidator creates a validator that uses the given LLM client.
func NewLLMPanelTextValidator(client textLLMClient) *LLMPanelTextValidator {
	return &LLMPanelTextValidator{client: client}
}

// Validate sends panels to the LLM and returns validation results.
func (v *LLMPanelTextValidator) Validate(ctx context.Context, panels []domain.Panel) (*PanelTextValidationResult, error) {
	if len(panels) == 0 {
		return &PanelTextValidationResult{OK: true}, nil
	}

	input := newValidatorInput(panels)
	inputJSON, err := json.Marshal(input)
	if err != nil {
		return nil, fmt.Errorf("validator marshal input: %w", err)
	}

	response, err := v.client.GenerateTransformation(ctx, panelTextValidatorPrompt, inputJSON)
	if err != nil {
		slog.Warn("Panel text validator LLM call failed, skipping validation", "error", err)
		return &PanelTextValidationResult{OK: true}, nil
	}

	result, err := parseValidatorResponse(response)
	if err != nil {
		return nil, fmt.Errorf("validator parse response: %w", err)
	}
	return result, nil
}

// validatorInput is the JSON sent to the LLM.
type validatorInput struct {
	Panels []validatorInputPanel `json:"panels"`
}

type validatorInputPanel struct {
	SceneNumber int    `json:"scene_number"`
	PanelNumber int    `json:"panel_number"`
	Description string `json:"description"`
	Dialogue    string `json:"dialogue"`
}

func newValidatorInput(panels []domain.Panel) validatorInput {
	in := validatorInput{Panels: make([]validatorInputPanel, len(panels))}
	for i, p := range panels {
		dialogue := p.Dialogue
		if dialogue == "" && len(p.DialogueLines) > 0 {
			var parts []string
			for _, dl := range p.DialogueLines {
				if t := strings.TrimSpace(dl.Text); t != "" {
					parts = append(parts, t)
				}
			}
			dialogue = strings.Join(parts, " ")
		}
		in.Panels[i] = validatorInputPanel{
			SceneNumber: p.SceneNumber,
			PanelNumber: p.PanelNumber,
			Description: p.Description,
			Dialogue:    dialogue,
		}
	}
	return in
}

// validatorResponse mirrors the JSON structure the LLM returns.
type validatorResponse struct {
	Validations []validatorResponseItem `json:"validations"`
}

type validatorResponseItem struct {
	SceneNumber int    `json:"scene_number"`
	PanelNumber int    `json:"panel_number"`
	Score       int    `json:"score"`
	Issue       string `json:"issue"`
}

var reFence = regexp.MustCompile("(?s)^```[a-zA-Z]*\\n?(.*?)\\n?```$")
var reThink = regexp.MustCompile(`(?s)<think>.*?</think>`)

func parseValidatorResponse(raw []byte) (*PanelTextValidationResult, error) {
	text := strings.TrimSpace(string(raw))
	// Strip <think> tags (reasoning models)
	text = reThink.ReplaceAllString(text, "")
	// Strip markdown fences
	if m := reFence.FindStringSubmatch(text); m != nil {
		text = strings.TrimSpace(m[1])
	}

	var resp validatorResponse
	if err := json.Unmarshal([]byte(text), &resp); err != nil {
		return nil, fmt.Errorf("json unmarshal: %w (raw: %.200s)", err, string(raw))
	}

	var issues []PanelTextIssue
	for _, v := range resp.Validations {
		if v.Score <= 3 {
			issues = append(issues, PanelTextIssue{
				SceneNumber: v.SceneNumber,
				PanelNumber: v.PanelNumber,
				Score:       v.Score,
				Issue:       v.Issue,
			})
		}
	}

	return &PanelTextValidationResult{
		OK:     len(issues) == 0,
		Issues: issues,
	}, nil
}
