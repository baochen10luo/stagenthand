package pipeline

import (
	"testing"

	"github.com/baochen10luo/stagenthand/internal/domain"
	"github.com/stretchr/testify/assert"
)

func TestCalcSubtitleTimings(t *testing.T) {
	t.Parallel()

	t.Run("empty lines returns empty", func(t *testing.T) {
		result := calcSubtitleTimings([]domain.DialogueLine{}, 10.0)
		assert.Empty(t, result)
	})

	t.Run("strategy D: single line occupies full panel", func(t *testing.T) {
		lines := []domain.DialogueLine{
			{Speaker: "Alice", Text: "Hello world."},
		}
		result := calcSubtitleTimings(lines, 5.0)
		assert.Len(t, result, 1)
		assert.Equal(t, 0.0, result[0].StartSec)
		assert.Equal(t, 5.0, result[0].EndSec)
	})

	t.Run("strategy D: single line preserves speaker and text", func(t *testing.T) {
		lines := []domain.DialogueLine{
			{Speaker: "Bob", Text: "Short.", Emotion: "angry"},
		}
		result := calcSubtitleTimings(lines, 3.0)
		assert.Equal(t, "Bob", result[0].Speaker)
		assert.Equal(t, "Short.", result[0].Text)
		assert.Equal(t, "angry", result[0].Emotion)
	})

	t.Run("strategy C: two equal-length lines split evenly", func(t *testing.T) {
		lines := []domain.DialogueLine{
			{Text: "abcd"}, // 4 non-ws chars
			{Text: "efgh"}, // 4 non-ws chars
		}
		result := calcSubtitleTimings(lines, 8.0)
		assert.Len(t, result, 2)
		assert.InDelta(t, 0.0, result[0].StartSec, 0.001)
		assert.InDelta(t, 4.0, result[0].EndSec, 0.001)
		assert.InDelta(t, 4.0, result[1].StartSec, 0.001)
		assert.InDelta(t, 8.0, result[1].EndSec, 0.001)
	})

	t.Run("strategy C: proportional to char count", func(t *testing.T) {
		// Line 0: 3 chars, Line 1: 9 chars → ratio 1:3
		lines := []domain.DialogueLine{
			{Text: "abc"},      // 3 chars
			{Text: "defghijkl"}, // 9 chars
		}
		result := calcSubtitleTimings(lines, 12.0)
		// Line 0 should get 3/12 * 12 = 3s, Line 1 gets 9s
		assert.InDelta(t, 0.0, result[0].StartSec, 0.001)
		assert.InDelta(t, 3.0, result[0].EndSec, 0.001)
		assert.InDelta(t, 3.0, result[1].StartSec, 0.001)
		assert.InDelta(t, 12.0, result[1].EndSec, 0.001)
	})

	t.Run("strategy C: whitespace not counted", func(t *testing.T) {
		// "a b" → 2 non-ws, "c d e" → 3 non-ws, total 5
		lines := []domain.DialogueLine{
			{Text: "a b"},   // 2 non-ws
			{Text: "c d e"}, // 3 non-ws
		}
		result := calcSubtitleTimings(lines, 10.0)
		assert.InDelta(t, 0.0, result[0].StartSec, 0.001)
		assert.InDelta(t, 4.0, result[0].EndSec, 0.001)   // 2/5 * 10 = 4
		assert.InDelta(t, 4.0, result[1].StartSec, 0.001)
		assert.InDelta(t, 10.0, result[1].EndSec, 0.001)
	})

	t.Run("strategy C: last line always ends at totalDuration", func(t *testing.T) {
		lines := []domain.DialogueLine{
			{Text: "一"},
			{Text: "二"},
			{Text: "三"},
		}
		result := calcSubtitleTimings(lines, 9.0)
		assert.Equal(t, 9.0, result[len(result)-1].EndSec)
	})

	t.Run("strategy C: empty text line gets minimum count of 1", func(t *testing.T) {
		lines := []domain.DialogueLine{
			{Text: ""},    // 0 non-ws → clamped to 1
			{Text: "abc"}, // 3 non-ws
		}
		// total = 4, line0 = 1/4*8 = 2, line1 = 3/4*8 = 6
		result := calcSubtitleTimings(lines, 8.0)
		assert.InDelta(t, 0.0, result[0].StartSec, 0.001)
		assert.InDelta(t, 2.0, result[0].EndSec, 0.001)
		assert.InDelta(t, 2.0, result[1].StartSec, 0.001)
		assert.InDelta(t, 8.0, result[1].EndSec, 0.001)
	})

	t.Run("strategy C: CJK chars counted correctly", func(t *testing.T) {
		// "你好" → 2 runes, "再見吧朋友" → 5 runes, total 7
		lines := []domain.DialogueLine{
			{Text: "你好"},
			{Text: "再見吧朋友"},
		}
		result := calcSubtitleTimings(lines, 7.0)
		assert.InDelta(t, 0.0, result[0].StartSec, 0.001)
		assert.InDelta(t, 2.0, result[0].EndSec, 0.001)
		assert.InDelta(t, 2.0, result[1].StartSec, 0.001)
		assert.InDelta(t, 7.0, result[1].EndSec, 0.001)
	})
}

func TestApplySubtitleTimings(t *testing.T) {
	t.Parallel()

	t.Run("panels without dialogue_lines are untouched", func(t *testing.T) {
		panels := []domain.Panel{
			{Dialogue: "no lines", DurationSec: 5.0},
		}
		result := applySubtitleTimings(panels)
		assert.Empty(t, result[0].DialogueLines)
	})

	t.Run("panels with dialogue_lines get timings set", func(t *testing.T) {
		panels := []domain.Panel{
			{
				DurationSec: 6.0,
				DialogueLines: []domain.DialogueLine{
					{Text: "abc"},
					{Text: "def"},
				},
			},
		}
		result := applySubtitleTimings(panels)
		assert.Equal(t, 0.0, result[0].DialogueLines[0].StartSec)
		assert.InDelta(t, 3.0, result[0].DialogueLines[0].EndSec, 0.001)
		assert.InDelta(t, 3.0, result[0].DialogueLines[1].StartSec, 0.001)
		assert.Equal(t, 6.0, result[0].DialogueLines[1].EndSec)
	})

	t.Run("multiple panels each get independent timings", func(t *testing.T) {
		panels := []domain.Panel{
			{
				DurationSec: 4.0,
				DialogueLines: []domain.DialogueLine{{Text: "one line"}},
			},
			{
				DurationSec: 10.0,
				DialogueLines: []domain.DialogueLine{
					{Text: "aa"},
					{Text: "bbbb"},
				},
			},
		}
		result := applySubtitleTimings(panels)
		// Panel 0: single line → full duration
		assert.Equal(t, 0.0, result[0].DialogueLines[0].StartSec)
		assert.Equal(t, 4.0, result[0].DialogueLines[0].EndSec)
		// Panel 1: 2 chars vs 4 chars → ratio 1:2
		assert.InDelta(t, 0.0, result[1].DialogueLines[0].StartSec, 0.001)
		// 2/6 * 10 ≈ 3.333
		assert.InDelta(t, 3.333, result[1].DialogueLines[0].EndSec, 0.01)
		assert.InDelta(t, 3.333, result[1].DialogueLines[1].StartSec, 0.01)
		assert.Equal(t, 10.0, result[1].DialogueLines[1].EndSec)
	})
}
