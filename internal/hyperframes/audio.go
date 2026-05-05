package hyperframes

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/baochen10luo/stagenthand/internal/domain"
)

type audioTrack struct {
	path     string
	startSec float64
	endSec   float64
}

// MixAudio mixes per-panel TTS tracks and optional BGM into <outputDir>/audio_mix.aac.
// Returns ("", nil) when there is no audio to mix.
func MixAudio(ctx context.Context, props domain.RemotionProps, cfg Config, outputDir string) (string, error) {
	tracks, totalDur := collectTracks(props, cfg.ShandHome)
	bgmPath := ResolveVirtualPath(cfg.ShandHome, props.BGMURL)

	if len(tracks) == 0 && bgmPath == "" {
		return "", nil
	}

	if cfg.DryRun {
		fmt.Fprintf(os.Stderr, "[DRY-RUN] Would mix %d TTS track(s) + BGM=%q\n", len(tracks), bgmPath)
		return "", nil
	}

	ffmpegBin, err := exec.LookPath("ffmpeg")
	if err != nil {
		return "", fmt.Errorf("ffmpeg not found in PATH: %w", err)
	}

	dir := effectiveDirectives(props.Directives)
	mixPath := filepath.Join(outputDir, "audio_mix.aac")

	inputArgs, filterArg, outputLabel := buildAudioFilterComplex(tracks, bgmPath, dir, totalDur)
	args := append(inputArgs,
		"-filter_complex", filterArg,
		"-map", outputLabel,
		"-c:a", "aac",
		"-ar", "44100",
		"-ac", "2",
		"-y",
		mixPath,
	)

	cmd := exec.CommandContext(ctx, ffmpegBin, args...)
	cmd.Stdout = os.Stderr
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("ffmpeg audio mix failed: %w", err)
	}
	return mixPath, nil
}

// MuxVideoAudio combines a silent video with an audio mix into outputPath.
func MuxVideoAudio(ctx context.Context, silentVideoPath, audioMixPath, outputPath string) error {
	ffmpegBin, err := exec.LookPath("ffmpeg")
	if err != nil {
		return fmt.Errorf("ffmpeg not found in PATH: %w", err)
	}
	cmd := exec.CommandContext(ctx, ffmpegBin,
		"-i", silentVideoPath,
		"-i", audioMixPath,
		"-c:v", "copy",
		"-c:a", "aac",
		"-shortest",
		"-y",
		outputPath,
	)
	cmd.Stdout = os.Stderr
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("ffmpeg mux failed: %w", err)
	}
	return nil
}

// audioDirectives holds effective audio mix parameters.
type audioDirectives struct {
	BGMFadeInSec  float64
	BGMFadeOutSec float64
	BGMVolume     float64
	DuckingDepth  float64
}

func effectiveDirectives(d *domain.Directives) audioDirectives {
	out := audioDirectives{
		BGMFadeInSec:  2.0,
		BGMFadeOutSec: 3.0,
		BGMVolume:     0.6,
		DuckingDepth:  0.15,
	}
	if d == nil {
		return out
	}
	if d.BGMFadeInSec > 0 {
		out.BGMFadeInSec = d.BGMFadeInSec
	}
	if d.BGMFadeOutSec > 0 {
		out.BGMFadeOutSec = d.BGMFadeOutSec
	}
	if d.BGMVolume > 0 {
		out.BGMVolume = d.BGMVolume
	}
	if d.DuckingDepth > 0 {
		out.DuckingDepth = d.DuckingDepth
	}
	return out
}

func collectTracks(props domain.RemotionProps, shandHome string) ([]audioTrack, float64) {
	const defaultDur = 3.0
	var tracks []audioTrack
	cursor := 0.0
	for _, p := range props.Panels {
		dur := p.DurationSec
		if dur <= 0 {
			dur = defaultDur
		}
		if p.AudioURL != "" {
			tracks = append(tracks, audioTrack{
				path:     ResolveVirtualPath(shandHome, p.AudioURL),
				startSec: cursor,
				endSec:   cursor + dur,
			})
		}
		cursor += dur
	}
	return tracks, cursor
}

// buildAudioFilterComplex produces the -filter_complex argument and the list of
// -i inputs for an ffmpeg invocation that mixes TTS tracks + optional BGM.
func buildAudioFilterComplex(
	tracks []audioTrack,
	bgmPath string,
	dir audioDirectives,
	totalDur float64,
) (inputArgs []string, filterComplex string, outputLabel string) {

	var filters []string
	inputIdx := 0

	// ── TTS inputs ──────────────────────────────────────────────────────────
	var ttsLabels []string
	for i, t := range tracks {
		inputArgs = append(inputArgs, "-i", t.path)
		delayMs := int(t.startSec * 1000)
		label := fmt.Sprintf("[tts%d]", i)
		filters = append(filters, fmt.Sprintf(
			"[%d:a]adelay=%d|%d,aresample=44100%s",
			inputIdx, delayMs, delayMs, label,
		))
		ttsLabels = append(ttsLabels, label)
		inputIdx++
	}

	var mixedTTS string
	switch len(ttsLabels) {
	case 0:
		// no TTS
	case 1:
		mixedTTS = ttsLabels[0]
	default:
		mixedTTS = "[tts_mix]"
		filters = append(filters, fmt.Sprintf(
			"%samix=inputs=%d:duration=longest[tts_mix]",
			strings.Join(ttsLabels, ""), len(ttsLabels),
		))
	}

	// ── BGM input ────────────────────────────────────────────────────────────
	var bgmLabel string
	if bgmPath != "" {
		inputArgs = append(inputArgs, "-i", bgmPath)

		fadeOutStart := totalDur - dir.BGMFadeOutSec
		if fadeOutStart < 0 {
			fadeOutStart = 0
		}

		// Volume expression: duck to DuckingDepth during each TTS segment.
		volExpr := fmt.Sprintf("%.3f", dir.BGMVolume)
		for j := len(tracks) - 1; j >= 0; j-- {
			t := tracks[j]
			volExpr = fmt.Sprintf(
				"if(between(t\\,%.3f\\,%.3f)\\,%.3f\\,%s)",
				t.startSec, t.endSec, dir.DuckingDepth, volExpr,
			)
		}

		bgmLabel = "[bgm_out]"
		filters = append(filters, fmt.Sprintf(
			"[%d:a]aloop=loop=-1:size=2000000000,"+
				"afade=t=in:st=0:d=%.3f,"+
				"afade=t=out:st=%.3f:d=%.3f,"+
				"volume='%s'%s",
			inputIdx,
			dir.BGMFadeInSec,
			fadeOutStart, dir.BGMFadeOutSec,
			volExpr,
			bgmLabel,
		))
		inputIdx++
	}

	// ── Final mix ────────────────────────────────────────────────────────────
	switch {
	case mixedTTS != "" && bgmLabel != "":
		filters = append(filters, fmt.Sprintf(
			"%s%samix=inputs=2:duration=longest[final_mix]",
			bgmLabel, mixedTTS,
		))
		outputLabel = "[final_mix]"
	case mixedTTS != "":
		outputLabel = mixedTTS
	default:
		outputLabel = bgmLabel
	}

	return inputArgs, strings.Join(filters, ";"), outputLabel
}
