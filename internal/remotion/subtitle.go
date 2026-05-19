package remotion

import (
	"strings"

	"github.com/baochen10luo/stagenthand/internal/domain"
	"github.com/baochen10luo/stagenthand/internal/render"
)

// BreakLongDialogueLines splits dialogue lines at punctuation boundaries only.
// Each punctuation-delimited phrase becomes its own timed subtitle segment.
// Spaces within a phrase are preserved. Only runs for portrait (9:16) format.
func BreakLongDialogueLines(panels []domain.Panel, format render.VideoFormat) []domain.Panel {
	if format != render.VideoFormatPortrait {
		return panels
	}
	for i := range panels {
		var newLines []domain.DialogueLine
		for _, dl := range panels[i].DialogueLines {
			for _, phrase := range splitAtPunctuation(dl.Text) {
				newLines = append(newLines, domain.DialogueLine{
					Speaker:  dl.Speaker,
					Text:     phrase,
					Emotion:  dl.Emotion,
					StartSec: 0,
					EndSec:   0,
				})
			}
		}
		panels[i].DialogueLines = newLines
	}
	return panels
}

// splitAtPunctuation splits text at break-punctuation characters, removing both
// break-punctuation and strip-punctuation from the output.
//
// Two tiers:
//   - breakPunct — triggers a line break AND is removed (。！？，、；：.!?,;)
//   - stripPunct — removed silently without causing a break (quotes, brackets, etc.)
func splitAtPunctuation(text string) []string {
	text = strings.TrimSpace(text)
	if text == "" {
		return nil
	}
	var segments []string
	var current []rune
	for _, r := range text {
		if isBreakPunct(r) {
			if len(current) > 0 {
				s := strings.TrimSpace(string(current))
				if s != "" {
					segments = append(segments, s)
				}
				current = nil
			}
		} else if isStripPunct(r) {
			// Remove without breaking — continue accumulating
		} else {
			current = append(current, r)
		}
	}
	if len(current) > 0 {
		s := strings.TrimSpace(string(current))
		if s != "" {
			segments = append(segments, s)
		}
	}
	return segments
}

// isBreakPunct triggers a line break and is removed.
func isBreakPunct(r rune) bool {
	switch r {
	case '。', '！', '？', '.', '!', '?':
		return true
	case '，', '、', '；', '：', ',', ';', ':':
		return true
	}
	return false
}

// isStripPunct is removed from the output WITHOUT triggering a line break.
func isStripPunct(r rune) bool {
	switch r {
	case '「', '」', '『', '』', '（', '）', '【', '】', '〈', '〉':
		return true
	case '…', '—', '～':
		return true
	}
	return false
}
