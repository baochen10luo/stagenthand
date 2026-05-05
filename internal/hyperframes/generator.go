package hyperframes

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"text/template"
	"unicode"

	"github.com/baochen10luo/stagenthand/internal/domain"
)

// Config holds settings for the HyperFrames render pass.
type Config struct {
	ShandHome string
	DryRun    bool
}

type resolvedPanel struct {
	Index           int
	ImagePath       string
	DurationSec     float64
	StartSec        float64
	MotionEffect    string
	MotionIntensity float64
	TransitionInMS  int
	SubtitleLines   []subtitleLine
}

type subtitleLine struct {
	Text     string
	StartSec float64
	EndSec   float64
}

type templateData struct {
	Title         string
	Width         int
	Height        int
	TotalDuration float64
	Panels        []resolvedPanel
}

var templateFuncs = template.FuncMap{
	"divBy":    func(ms int, d float64) float64 { return float64(ms) / d },
	"addFloat": func(a, b float64) float64 { return a + b },
	"mulFloat": func(a, b float64) float64 { return a * b },
}

// GenerateProject writes index.html and a minimal package.json into projectDir,
// ready for `npx @hyperframes/cli render index.html`.
func GenerateProject(props domain.RemotionProps, projectDir string, cfg Config) error {
	panels, totalDur := prepareResolvedPanels(props, cfg.ShandHome)

	data := templateData{
		Title:         props.Title,
		Width:         props.Width,
		Height:        props.Height,
		TotalDuration: totalDur,
		Panels:        panels,
	}

	tmpl, err := template.New("hf").Funcs(templateFuncs).Parse(htmlTemplate)
	if err != nil {
		return fmt.Errorf("parse html template: %w", err)
	}

	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, data); err != nil {
		return fmt.Errorf("execute html template: %w", err)
	}

	if err := os.WriteFile(filepath.Join(projectDir, "index.html"), buf.Bytes(), 0644); err != nil {
		return fmt.Errorf("write index.html: %w", err)
	}

	const pkgJSON = `{"name":"hf-composition","version":"1.0.0","private":true}`
	if err := os.WriteFile(filepath.Join(projectDir, "package.json"), []byte(pkgJSON), 0644); err != nil {
		return fmt.Errorf("write package.json: %w", err)
	}

	return nil
}

func prepareResolvedPanels(props domain.RemotionProps, shandHome string) ([]resolvedPanel, float64) {
	const (
		defaultDur             = 3.0
		defaultTransitionMS    = 400
		defaultMotionIntensity = 0.05
	)

	panels := make([]resolvedPanel, len(props.Panels))
	cursor := 0.0

	for i, p := range props.Panels {
		dur := p.DurationSec
		if dur <= 0 {
			dur = defaultDur
		}

		motionEffect := "ken_burns_in"
		motionIntensity := defaultMotionIntensity
		transitionMS := defaultTransitionMS

		if p.Directive != nil {
			if p.Directive.MotionEffect != "" {
				motionEffect = p.Directive.MotionEffect
			}
			if p.Directive.MotionIntensity > 0 {
				motionIntensity = p.Directive.MotionIntensity
			}
			if p.Directive.TransitionDurationMs > 0 {
				transitionMS = p.Directive.TransitionDurationMs
			}
		}

		panels[i] = resolvedPanel{
			Index:           i,
			ImagePath:       ResolveVirtualPath(shandHome, p.ImageURL),
			DurationSec:     dur,
			StartSec:        cursor,
			MotionEffect:    motionEffect,
			MotionIntensity: motionIntensity,
			TransitionInMS:  transitionMS,
			SubtitleLines:   buildSubtitleLines(p, dur),
		}
		cursor += dur
	}
	return panels, cursor
}

func buildSubtitleLines(p domain.Panel, panelDur float64) []subtitleLine {
	// Prefer pre-timed dialogue lines
	if len(p.DialogueLines) > 0 && p.DialogueLines[0].StartSec > 0 {
		lines := make([]subtitleLine, len(p.DialogueLines))
		for i, l := range p.DialogueLines {
			lines[i] = subtitleLine{Text: l.Text, StartSec: l.StartSec, EndSec: l.EndSec}
		}
		return lines
	}
	// Untimed dialogue lines: distribute proportionally
	if len(p.DialogueLines) > 0 {
		return distributeLines(p.DialogueLines, panelDur)
	}
	// Fallback: single legacy dialogue string
	if p.Dialogue != "" {
		return []subtitleLine{{Text: p.Dialogue, StartSec: 0, EndSec: panelDur}}
	}
	return nil
}

func distributeLines(lines []domain.DialogueLine, totalDur float64) []subtitleLine {
	if len(lines) == 1 {
		return []subtitleLine{{Text: lines[0].Text, StartSec: 0, EndSec: totalDur}}
	}
	counts := make([]int, len(lines))
	total := 0
	for i, l := range lines {
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
	result := make([]subtitleLine, len(lines))
	cursor := 0.0
	for i := range lines {
		dur := (float64(counts[i]) / float64(total)) * totalDur
		end := cursor + dur
		if i == len(lines)-1 {
			end = totalDur
		}
		result[i] = subtitleLine{Text: lines[i].Text, StartSec: cursor, EndSec: end}
		cursor += dur
	}
	return result
}
