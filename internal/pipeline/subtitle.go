package pipeline

import (
	"strings"

	"github.com/baochen10luo/stagenthand/internal/domain"
	"github.com/baochen10luo/stagenthand/internal/render"
)

const (
	DefaultMaxCharsPortrait = 10
)

// BreakLongDialogueLines splits long dialogue lines into shorter segments for
// portrait (9:16) video subtitles. Uses the video-translation approach:
//  1. Split by sentence-ending punctuation (。！？.!?)
//  2. If still too long, split by clause punctuation (，、；,;)
//  3. If still too long, hard split at maxChars
//
// Each segment becomes its own DialogueLine with copied speaker/emotion.
// Timing is left unset (0) — applySubtitleTimings handles proportional distribution.
// Landscape format is left untouched.
func BreakLongDialogueLines(panels []domain.Panel, format render.VideoFormat) []domain.Panel {
	maxChars := maxCharsForFormat(format)
	if maxChars <= 0 {
		return panels
	}
	for i := range panels {
		var newLines []domain.DialogueLine
		for _, dl := range panels[i].DialogueLines {
			segments := splitTextForSubtitle(dl.Text, maxChars)
			for _, seg := range segments {
				newLines = append(newLines, domain.DialogueLine{
					Speaker:  dl.Speaker,
					Text:     seg,
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

func maxCharsForFormat(format render.VideoFormat) int {
	if format == render.VideoFormatPortrait {
		return DefaultMaxCharsPortrait
	}
	return 0
}

// splitTextForSubtitle segments text into chunks of at most maxChars runes,
// using a 3-level approach:
//
// Level 1: Split by sentence-ending punctuation (。！？.!?)
// Level 2: For chunks still > maxChars, split by clause punctuation (，、；,;)
// Level 3: For chunks still > maxChars, hard split at maxChars boundary.
func splitTextForSubtitle(text string, maxChars int) []string {
	text = strings.TrimSpace(text)
	if text == "" {
		return []string{""}
	}
	runes := []rune(text)
	if len(runes) <= maxChars {
		return []string{text}
	}

	// Level 1: split by sentence-ending punctuation
	segments := splitByPunct(runes, sentenceEndPunct)
	if allWithinLimit(segments, maxChars) {
		return cleanEmpty(segments)
	}

	// Level 2: for overlong segments, split by clause punctuation
	var result []string
	for _, seg := range segments {
		r := []rune(seg)
		if len(r) <= maxChars {
			result = append(result, seg)
			continue
		}
		sub := splitByPunct(r, clausePunct)
		result = append(result, sub...)
	}
	if allWithinLimit(result, maxChars) {
		return cleanEmpty(result)
	}

	// Level 3: hard split at maxChars boundary
	var final []string
	for _, seg := range result {
		r := []rune(seg)
		if len(r) <= maxChars {
			final = append(final, seg)
			continue
		}
		for i := 0; i < len(r); i += maxChars {
			end := i + maxChars
			if end > len(r) {
				end = len(r)
			}
			final = append(final, string(r[i:end]))
		}
	}
	return cleanEmpty(final)
}

var sentenceEndPunct = func() *punctuationSet {
	return &punctuationSet{chars: map[rune]bool{
		'。': true, '！': true, '？': true, '.': true, '!': true, '?': true,
	}, keep: true}
}()

var clausePunct = func() *punctuationSet {
	return &punctuationSet{chars: map[rune]bool{
		'，': true, '、': true, '；': true, ',': true, ';': true,
	}, keep: false}
}()

type punctuationSet struct {
	chars map[rune]bool
	keep  bool // keep the punctuation char at end of segment
}

// splitByPunct splits runes at punctuation boundaries, keeping the segment
// containing the punctuation char if keep=true.
func splitByPunct(runes []rune, p *punctuationSet) []string {
	var segments []string
	start := 0
	for i, r := range runes {
		if p.chars[r] {
			if p.keep {
				segments = append(segments, string(runes[start:i+1]))
			} else {
				if i > start {
					segments = append(segments, string(runes[start:i]))
				}
			}
			start = i + 1
		}
	}
	if start < len(runes) {
		segments = append(segments, string(runes[start:]))
	}
	return segments
}

func allWithinLimit(segments []string, maxChars int) bool {
	for _, s := range segments {
		if len([]rune(s)) > maxChars {
			return false
		}
	}
	return true
}

func cleanEmpty(segments []string) []string {
	var out []string
	for _, s := range segments {
		s = strings.TrimSpace(s)
		if s == "" {
			continue
		}
		out = append(out, s)
	}
	if len(out) == 0 {
		return []string{""}
	}
	return out
}


