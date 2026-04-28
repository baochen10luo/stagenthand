package remotion

import (
	"fmt"
	"strings"
	"unicode"

	"github.com/baochen10luo/stagenthand/internal/domain"
	"github.com/baochen10luo/stagenthand/internal/render"
)

const defaultPanelDurationSec = 3.0

// StoryboardToProps converts a Storyboard (with nested Scenes and Panels)
// into a flat RemotionProps ready for the Remotion template.
// All panels are extracted in scene order, preserving panel order within each scene.
// Panels with zero DurationSec are assigned the default duration.
func StoryboardToProps(sb domain.Storyboard, width, height, fps int) domain.RemotionProps {
	panels := flattenPanels(sb.Scenes)
	return domain.RemotionProps{
		ProjectID: sb.ProjectID,
		Title:     "Generated Drama",
		BGMURL:    normalizePath(sb.BGMURL, sb.ProjectID),
		Panels:    panels,
		FPS:       fps,
		Width:     width,
		Height:    height,
	}
}

// PanelsToProps converts a flat []Panel directly into RemotionProps.
// Useful when the pipeline has already extracted panels from the storyboard.
// width and height are explicit overrides; pass 0,0 to derive from format.
func PanelsToProps(projectID string, panels []domain.Panel, width, height, fps int, bgmURL string, directives *domain.Directives) domain.RemotionProps {
	return PanelsToPropsWithFormat(projectID, panels, width, height, fps, bgmURL, directives, render.VideoFormatLandscape)
}

// PanelsToPropsWithFormat converts a flat []Panel into RemotionProps using the given VideoFormat
// to set canvas dimensions. Explicit width/height > 0 override the format dimensions.
func PanelsToPropsWithFormat(projectID string, panels []domain.Panel, width, height, fps int, bgmURL string, directives *domain.Directives, format render.VideoFormat) domain.RemotionProps {
	fw, fh := format.Dimensions()
	if width == 0 {
		width = fw
	}
	if height == 0 {
		height = fh
	}
	normalized := make([]domain.Panel, len(panels))
	for i, p := range panels {
		p = withDefaultDuration(p)
		p.ImageURL = normalizePath(p.ImageURL, projectID)
		p.AudioURL = normalizePath(p.AudioURL, projectID)
		p = applySubtitleTimings(p)
		normalized[i] = p
	}
	return domain.RemotionProps{
		ProjectID:  projectID,
		Title:      "Generated Drama",
		BGMURL:     normalizePath(bgmURL, projectID),
		Directives: directives,
		Panels:     normalized,
		FPS:        fps,
		Width:      width,
		Height:     height,
	}
}

func normalizePath(path, projectID string) string {
	if path == "" || strings.HasPrefix(path, "/projects/") {
		return path
	}

	// Extract the "projects/<project_id>/subpath" segment from an absolute path
	// and return it as a root-relative virtual path served by --public-dir ~/.shand.
	marker := fmt.Sprintf("projects/%s/", projectID)
	idx := strings.Index(path, marker)
	if idx != -1 {
		return "/" + path[idx:] // /projects/<project_id>/images/... or /audio/...
	}

	return path
}


// flattenPanels extracts all panels from scenes in order, applying default durations and subtitle timings.
func flattenPanels(scenes []domain.Scene) []domain.Panel {
	var out []domain.Panel
	for _, scene := range scenes {
		for _, p := range scene.Panels {
			p = withDefaultDuration(p)
			p = applySubtitleTimings(p)
			out = append(out, p)
		}
	}
	return out
}

// withDefaultDuration ensures a Panel has a non-zero DurationSec.
func withDefaultDuration(p domain.Panel) domain.Panel {
	if p.DurationSec == 0 {
		p.DurationSec = defaultPanelDurationSec
	}
	return p
}

// calcSubtitleTimings assigns StartSec/EndSec to each DialogueLine using:
//   - Strategy D (single line or empty): the one line spans the full panel duration.
//   - Strategy C (multiple lines): each line's duration ∝ its non-whitespace character count.
func calcSubtitleTimings(lines []domain.DialogueLine, totalDuration float64) []domain.DialogueLine {
	if len(lines) == 0 {
		return lines
	}
	result := make([]domain.DialogueLine, len(lines))
	copy(result, lines)

	if len(result) == 1 {
		// Strategy D: single line covers the whole panel
		result[0].StartSec = 0
		result[0].EndSec = totalDuration
		return result
	}

	// Strategy C: proportional to non-whitespace character count
	counts := make([]int, len(result))
	total := 0
	for i, l := range result {
		n := 0
		for _, c := range l.Text {
			if !unicode.IsSpace(c) {
				n++
			}
		}
		if n == 0 {
			n = 1
		}
		counts[i] = n
		total += n
	}

	cursor := 0.0
	for i := range result {
		dur := (float64(counts[i]) / float64(total)) * totalDuration
		result[i].StartSec = cursor
		cursor += dur
		if i == len(result)-1 {
			result[i].EndSec = totalDuration
		} else {
			result[i].EndSec = cursor
		}
	}
	return result
}

// applySubtitleTimings calculates and assigns subtitle timings for a panel's DialogueLines.
func applySubtitleTimings(p domain.Panel) domain.Panel {
	if len(p.DialogueLines) == 0 {
		return p
	}
	p.DialogueLines = calcSubtitleTimings(p.DialogueLines, p.DurationSec)
	return p
}
