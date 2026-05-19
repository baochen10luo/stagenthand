package remotion

import (
	"testing"

	"github.com/baochen10luo/stagenthand/internal/domain"
	"github.com/baochen10luo/stagenthand/internal/render"
)

func TestBreakLongDialogueLines_Portrait(t *testing.T) {
	panels := []domain.Panel{
		{
			PanelNumber: 1,
			Dialogue:    "這是一個非常長的句子，需要被切成多段！因為每行不能超過十八個字元？",
			DialogueLines: []domain.DialogueLine{
				{Speaker: "", Text: "這是一個非常長的句子，需要被切成多段！因為每行不能超過十八個字元？"},
			},
			DurationSec: 10,
		},
		{
			PanelNumber: 2,
			Dialogue:    "短句",
			DialogueLines: []domain.DialogueLine{
				{Speaker: "", Text: "短句"},
			},
			DurationSec: 5,
		},
	}

	result := BreakLongDialogueLines(panels, render.VideoFormatPortrait)

	p1 := result[0]
	if len(p1.DialogueLines) != 3 {
		t.Errorf("Panel 1: expected 3 lines from 3 break-punct groups, got %d", len(p1.DialogueLines))
	}
	expected := []string{"這是一個非常長的句子", "需要被切成多段", "因為每行不能超過十八個字元"}
	for i, dl := range p1.DialogueLines {
		if dl.Text != expected[i] {
			t.Errorf("Panel 1 line %d: expected %q, got %q", i+1, expected[i], dl.Text)
		}
	}
	t.Logf("Panel 1: split into %d lines", len(p1.DialogueLines))
	for i, dl := range p1.DialogueLines {
		t.Logf("  [%d] %q", i+1, dl.Text)
	}

	p2 := result[1]
	if len(p2.DialogueLines) != 1 {
		t.Errorf("Panel 2: expected 1 line, got %d", len(p2.DialogueLines))
	}
}

func TestBreakLongDialogueLines_Landscape(t *testing.T) {
	panels := []domain.Panel{
		{
			PanelNumber: 1,
			Dialogue:    "這是一個非常長的句子但是在landscape模式不應該被切",
			DialogueLines: []domain.DialogueLine{
				{Speaker: "", Text: "這是一個非常長的句子但是在landscape模式不應該被切"},
			},
		},
	}

	result := BreakLongDialogueLines(panels, render.VideoFormatLandscape)
	if len(result[0].DialogueLines) != 1 {
		t.Errorf("Landscape: expected 1 line unchanged, got %d", len(result[0].DialogueLines))
	}
}

func TestBreakLongDialogueLines_DefaultFormat(t *testing.T) {
	panels := []domain.Panel{
		{
			PanelNumber: 1,
			DialogueLines: []domain.DialogueLine{
				{Text: "長文字串長文字串長文字串長文字串長文字串"},
			},
		},
	}

	result := BreakLongDialogueLines(panels, render.VideoFormat(""))
	if len(result[0].DialogueLines) != 1 {
		t.Errorf("Unknown format: expected 1 line unchanged, got %d", len(result[0].DialogueLines))
	}
}

func TestBreakLongDialogueLines_MixedText(t *testing.T) {
	panels := []domain.Panel{
		{
			PanelNumber: 1,
			Dialogue:    "這次的 deadline 真的來得太突然了",
			DialogueLines: []domain.DialogueLine{
				{Text: "這次的 deadline 真的來得太突然了"},
			},
			DurationSec: 6,
		},
	}

	result := BreakLongDialogueLines(panels, render.VideoFormatPortrait)

	if len(result[0].DialogueLines) != 1 {
		t.Errorf("Expected 1 line (no break-punct), got %d", len(result[0].DialogueLines))
	}
	if result[0].DialogueLines[0].Text != "這次的 deadline 真的來得太突然了" {
		t.Errorf("Expected full text unchanged, got %q", result[0].DialogueLines[0].Text)
	}
}

func TestBreakLongDialogueLines_StripPunctNoBreak(t *testing.T) {
	// 「砰」 should have quotes removed without breaking "砰" and "地一聲" apart.
	panels := []domain.Panel{
		{
			PanelNumber: 1,
			Dialogue:    "他夾起棋子砸在石盤上，「砰」地一聲，彷彿在命運的臉上落下一記重拳。",
			DialogueLines: []domain.DialogueLine{
				{Text: "他夾起棋子砸在石盤上，「砰」地一聲，彷彿在命運的臉上落下一記重拳。"},
			},
			DurationSec: 8,
		},
	}

	result := BreakLongDialogueLines(panels, render.VideoFormatPortrait)

	// Split at ， and 。 → 3 segments
	if len(result[0].DialogueLines) != 3 {
		t.Errorf("Expected 3 lines (split at ，， 。), got %d", len(result[0].DialogueLines))
	}
	for i, dl := range result[0].DialogueLines {
		t.Logf("  [%d] %q", i+1, dl.Text)
	}
	// 「砰」 removed without splitting "砰" from "地一聲"
	if result[0].DialogueLines[1].Text != "砰地一聲" {
		t.Errorf("Line 2: expected quotes removed keeping '砰地一聲' together, got %q", result[0].DialogueLines[1].Text)
	}
}

func TestBreakLongDialogueLines_QuotedTerm(t *testing.T) {
	// 「賭性」 as a quoted term — should be removed without splitting the phrase.
	panels := []domain.Panel{
		{
			PanelNumber: 1,
			Dialogue:    "這股「賭性」在血脈裡暗流湧動，連結了那個為愛孤注一擲的細伯。",
			DialogueLines: []domain.DialogueLine{
				{Text: "這股「賭性」在血脈裡暗流湧動，連結了那個為愛孤注一擲的細伯。"},
			},
			DurationSec: 8,
		},
	}

	result := BreakLongDialogueLines(panels, render.VideoFormatPortrait)

	if len(result[0].DialogueLines) != 2 {
		t.Errorf("Expected 2 lines (split at ，), got %d", len(result[0].DialogueLines))
	}
	t.Logf("Quoted term result: %d lines", len(result[0].DialogueLines))
	for i, dl := range result[0].DialogueLines {
		t.Logf("  [%d] %q", i+1, dl.Text)
	}
	// 「賭性」 removed, no split inside the phrase
	if result[0].DialogueLines[0].Text != "這股賭性在血脈裡暗流湧動" {
		t.Errorf("Line 1: expected quotes removed keeping phrase intact, got %q", result[0].DialogueLines[0].Text)
	}
}
