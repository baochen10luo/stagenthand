package pipeline

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"

	"github.com/baochen10luo/stagenthand/internal/domain"
)

// Transformer defines the behavior needed to run a transformation stage.
// This is exactly the llm.Client footprint, kept clean.
type Transformer interface {
	GenerateTransformation(ctx context.Context, systemPrompt string, inputData []byte) ([]byte, error)
}

// RunTransformationStage executes a single LLM transformation pipeline step.
func RunTransformationStage(ctx context.Context, transformer Transformer, systemPrompt string, inputData []byte) ([]byte, error) {
	if len(inputData) == 0 {
		return nil, errors.New("input data cannot be empty")
	}

	if systemPrompt == "" {
		return nil, errors.New("system prompt cannot be empty")
	}

	output, err := transformer.GenerateTransformation(ctx, systemPrompt, inputData)
	if err != nil {
		return nil, fmt.Errorf("transformer failed: %w", err)
	}

	if len(output) == 0 {
		return nil, errors.New("transformer returned empty output")
	}

	return output, nil
}

// rePanelPrefix matches "Panel N:" prefixes (case-insensitive).
var rePanelPrefix = regexp.MustCompile(`(?i)^panel\s+\d+:\s*`)

// reSpeakerPrefix matches "SomeName:" or "VO (Name):" style prefixes followed by quoted content,
// e.g. `VO (Narrator): '...'` or `奶奶: '...'`.
// Group 1 captures the inner text without surrounding quotes.
var reSpeakerPrefix = regexp.MustCompile(`^[^'"：:]+[：:]\s*['"](.+?)['"]$`)

// reSpeakerPrefixUnquoted matches speaker prefixes where the content is NOT quoted,
// e.g. `奶奶: 啊，你來了！`.
var reSpeakerPrefixUnquoted = regexp.MustCompile(`^[A-Za-z\p{Han}()\s]+[：:]\s+(.+)$`)

// CleanDialogue strips common speaker/panel prefix patterns from a dialogue or text field,
// returning only the spoken words themselves.
// Exported for testing; internal callers use cleanDialogue.
func CleanDialogue(s string) string {
	return cleanDialogue(s)
}

// cleanDialogue is the internal implementation.
func cleanDialogue(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return s
	}

	// 1. Remove "Panel N:" prefix.
	s = rePanelPrefix.ReplaceAllString(s, "")
	s = strings.TrimSpace(s)

	// 1b. After Panel prefix removal, strip a single outer wrapping quote layer so that
	//     `"VO (Alex): '在路上'"` becomes `VO (Alex): '在路上'` and can be further processed.
	if len(s) >= 2 {
		first, last := s[0], s[len(s)-1]
		if (first == '"' && last == '"') || (first == '\'' && last == '\'') {
			s = strings.TrimSpace(s[1 : len(s)-1])
		}
	}

	// 2. Remove "Speaker: 'content'" format → extract quoted content only.
	if m := reSpeakerPrefix.FindStringSubmatch(s); len(m) > 1 {
		s = m[1]
		return strings.TrimSpace(s)
	}

	// 3. Remove "Speaker: content" format (unquoted) — only when the prefix looks like
	//    a name/VO tag, not a sentence containing a colon (e.g. Chinese time expressions).
	//    We require the prefix to be ≤ 20 chars to avoid stripping legitimate content.
	if m := reSpeakerPrefixUnquoted.FindStringSubmatch(s); len(m) > 1 {
		prefix := s[:strings.Index(s, m[1])]
		if len([]rune(prefix)) <= 22 {
			s = m[1]
			return strings.TrimSpace(s)
		}
	}

	// 4. Strip wrapping quotes left over after prefix removal.
	s = strings.Trim(s, `'"`)
	return strings.TrimSpace(s)
}

// cleanPanels applies cleanDialogue to the Dialogue field and every DialogueLine.Text
// within a slice of panels.
func cleanPanels(panels []domain.Panel) []domain.Panel {
	for i := range panels {
		panels[i].Dialogue = cleanDialogue(panels[i].Dialogue)
		for j := range panels[i].DialogueLines {
			panels[i].DialogueLines[j].Text = cleanDialogue(panels[i].DialogueLines[j].Text)
		}
	}
	return panels
}

// languageInstructions maps BCP-47 language tags to dialogue instructions appended to PromptStoryboardToPanels.
var languageInstructions = map[string]string{
	"zh-TW":  "IMPORTANT: All 'dialogue' and 'text' fields MUST be written in Traditional Chinese (繁體中文). Use natural conversational Taiwanese Mandarin.",
	"cmn-CN": "IMPORTANT: All 'dialogue' and 'text' fields MUST be written in Simplified Chinese (简体中文). Use natural conversational Mandarin.",
	"en-US":  "IMPORTANT: All 'dialogue' and 'text' fields MUST be written in English. Use natural American English.",
	"en-GB":  "IMPORTANT: All 'dialogue' and 'text' fields MUST be written in English. Use natural British English.",
	"ja-JP":  "IMPORTANT: All 'dialogue' and 'text' fields MUST be written in Japanese (日本語). Use natural conversational Japanese.",
	"ko-KR":  "IMPORTANT: All 'dialogue' and 'text' fields MUST be written in Korean (한국어). Use natural conversational Korean.",
}

// LangInstruction is the exported version of langInstruction for use in tests.
func LangInstruction(lang string) string {
	return langInstruction(lang)
}

// langInstruction returns a language-enforcement prefix for LLM system prompts.
// For Chinese languages it returns an empty string (LLM defaults to Chinese already).
// For all other BCP-47 tags a strong directive is prepended so the LLM cannot
// silently fall back to Chinese output.
func langInstruction(lang string) string {
	switch {
	case strings.HasPrefix(lang, "en"):
		return "IMPORTANT: You MUST write ALL dialogue, narration, and text content in English only. Do not use any other language.\n\n"
	case strings.HasPrefix(lang, "ja"):
		return "IMPORTANT: You MUST write ALL dialogue, narration, and text content in Japanese only. Do not use any other language.\n\n"
	case strings.HasPrefix(lang, "ko"):
		return "IMPORTANT: You MUST write ALL dialogue, narration, and text content in Korean only. Do not use any other language.\n\n"
	default:
		return ""
	}
}

// BuildStoryboardToPanelsPrompt returns the PromptStoryboardToPanels with optional language instruction appended.
// Exported for testing. Internal callers use buildStoryboardToPanelsPrompt.
func BuildStoryboardToPanelsPrompt(language string, sb domain.Storyboard) string {
	return buildStoryboardToPanelsPrompt(language, sb, 0)
}

// buildStoryboardToPanelsPrompt returns the PromptStoryboardToPanels with optional language instruction appended.
func buildStoryboardToPanelsPrompt(language string, sb domain.Storyboard, targetPanels int) string {
	base := PromptStoryboardToPanels

	// Check language from storyboard directives first, then from orchestrator deps language
	lang := language
	if sb.Directives != nil && sb.Directives.Language != "" {
		lang = sb.Directives.Language
	}

	if lang == "" {
		lang = "zh-TW" // default to Traditional Chinese
	}

	if instruction, ok := languageInstructions[lang]; ok {
		base = base + "\n" + instruction
	}

	if targetPanels > 0 {
		base = base + fmt.Sprintf("\nIMPORTANT: Generate exactly %d panels total across all scenes.", targetPanels)
	}

	return base
}

// System prompts for the Phase 2 stages.
const (
	PromptStoryToOutline = `You are an expert story outliner. Read the input story prompt and generate a JSON outline.
Output JSON MUST follow this outline schema:
{
  "project_id": "...",
  "episodes": [
    {
      "number": 1,
      "title": "...",
      "synopsis": "...",
      "hook": "...",
      "cliffhanger": "..."
    }
  ]
}`

	PromptOutlineToStoryboard = `You are a storyboard director. Convert the input outline JSON into a localized scene-by-scene storyboard JSON. Ensure your scenes follow a cohesive 3-act narrative arc (setup, conflict, resolution).
CRITICAL: If the story lacks spoken dialogue, you MUST invent a compelling voiceover (VO) narrator or internal monologue to convey the deeper emotion, sacrifice, or meaning of the scene. Do not leave the story silent, otherwise the audience will not understand the plot.
Output JSON MUST follow this schema:
{
  "project_id": "...",
  "episode": 1,
  "directives": {
    "style_prompt": "YOUR_ACTUAL_STYLE_PROMPT_HERE (e.g., 'photorealistic cyberpunk, dark noir')",
    "color_filter": "cinematic",
    "bgm_tags": "suspense+dark+ambient"
  },
  "scenes": [
    {
      "number": 1,
      "description": "..."
    }
  ]
}`

	PromptStoryboardToPanels = `You are a visual panel designer and cinematographer. Convert the input storyboard JSON into a detailed panel-by-panel generation JSON.
Target total video length: approximately 30–50 seconds. Use 4–7 panels maximum.
Each panel's 'duration_sec' should reflect the time needed to naturally speak the dialogue aloud PLUS viewer breathing time. Estimate ~0.12 seconds per character. Keep individual dialogue short and punchy — no more than 30 words per panel.
CRITICAL: Every panel MUST have a 'dialogue' field. If the character is not speaking, use a VoiceOver (VO) to narrate the emotion, sacrifice, or plot context so the audience understands what is happening.
Each panel should have at most one primary speaker. Split multi-speaker exchanges into separate panels.
Output JSON MUST follow this schema:
{
  "project_id": "...",
  "episode": 1,
  "panels": [
    {
      "scene_number": 1,
      "panel_number": 1,
      "description": "...",
      "dialogue": "...",
      "dialogue_lines": [
        {"speaker": "角色名", "text": "對白內容", "emotion": "neutral"}
      ],
      "character_refs": [],
      "duration_sec": 4.0,
      "directive": {
        "motion_effect": "ken_burns_in",
        "motion_intensity": 0.05,
        "transition_in": "fade",
        "transition_out": "fade",
        "subtitle_effect": "fade",
        "subtitle_position": "bottom"
      }
    }
  ]
}

DIRECTOR RULES — You are the cinematographer. Vary motion and transitions to match the scene's emotional beat:

motion_effect choices (pick based on scene type):
- "ken_burns_in"  → slow zoom in: intimacy, revelation, tension building
- "ken_burns_out" → slow zoom out: establishing shot, loneliness, ending, wide context
- "pan_left"      → lateral pan left: movement, departure, searching
- "pan_right"     → lateral pan right: arrival, discovery, following action
- "static"        → no movement: shock, confrontation, held breath moment

motion_intensity: 0.03–0.08 (subtle = 0.03, dramatic = 0.08)

transition_in choices: "fade" | "cut" | "dissolve" | "wipe_left"
- "cut"      → abrupt, action-driven or shocking moment
- "fade"     → soft, time passing, emotional shift
- "dissolve" → memory, dream, gentle transition
- "wipe_left" → scene change, new location

subtitle_effect: "fade" | "typewriter" | "none"
- "typewriter" → for key reveals or dramatic spoken lines
- "fade"       → standard
- "none"       → silent panels

subtitle_position: "bottom" (default) | "top" | "center"

RULES:
1. Never use the same motion_effect for more than 2 consecutive panels
2. Opening panel: prefer "ken_burns_out" to establish the world
3. Climax/conflict panel: prefer "ken_burns_in" + transition_in "cut"
4. Final panel: prefer "ken_burns_out" + transition_out "fade"
5. motion_intensity should vary — don't use 0.05 for every panel

CRITICAL DIALOGUE FORMAT RULES:
- The "dialogue" field and every "text" field inside "dialogue_lines" MUST contain ONLY the spoken words themselves.
- Do NOT include speaker names, character prefixes, "VO:", "VO (Name):", or "Panel N:" prefixes anywhere in these fields.
- Do NOT wrap the text in quotes.
- CORRECT:   "dialogue": "在一個珍惜傳統的世界裡..."
- WRONG:     "dialogue": "VO (Narrator): '在一個珍惜傳統的世界裡...'"
- WRONG:     "dialogue": "奶奶: '啊，你來了！'"
- WRONG:     "dialogue": "Panel 1: \"VO (Alex): '...'\""
- The "speaker" field in dialogue_lines is the correct place to record who is speaking.`
)
