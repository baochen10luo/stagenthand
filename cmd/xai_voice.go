package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/baochen10luo/stagenthand/config"
	"github.com/baochen10luo/stagenthand/internal/audio"
	xauth "github.com/baochen10luo/stagenthand/internal/auth/xai"
	"github.com/spf13/cobra"
)

type xaiVoiceSynthesizer interface {
	Synthesize(ctx context.Context, text string, options audio.XAITTSOptions) (audio.XAITTSResult, error)
}

var newXAIVoiceSynthesizer = func(appCfg *config.Config) (xaiVoiceSynthesizer, error) {
	if appCfg == nil {
		appCfg = &config.Config{}
	}
	store := xauth.NewFileTokenStore(appCfg.XAI.TokenPath)
	return audio.NewXAIOAuthTTSClient(appCfg.XAI.BaseURL, xauth.NewFileTokenSource(store), nil), nil
}

var (
	xaiVoiceSeed      int64
	xaiVoiceID        string
	xaiVoiceIDs       string
	xaiVoiceText      string
	xaiVoiceOutput    string
	xaiVoiceOutputDir string
	xaiVoiceLanguage  string
	xaiVoiceCodec     string
	xaiVoiceRandom    bool
)

var xaiVoiceCmd = &cobra.Command{
	Use:   "voice",
	Short: "Probe xAI OAuth TTS voices",
}

var xaiVoiceVoicesCmd = &cobra.Command{
	Use:   "voices",
	Short: "List local xAI TTS voice candidates",
	RunE: func(cmd *cobra.Command, args []string) error {
		return writeJSON(cmd.OutOrStdout(), map[string]any{
			"provider": "xai-oauth",
			"voices":   audio.DefaultXAITTSVoices(),
			"note":     "local candidates; run `shand xai voice probe` to verify account support",
		})
	},
}

var xaiVoicePickCmd = &cobra.Command{
	Use:   "pick",
	Short: "Pick one xAI TTS voice from the candidate set",
	RunE: func(cmd *cobra.Command, args []string) error {
		voices := xaiVoiceCandidates("", xaiVoiceIDs)
		seed := effectiveXAISeed(xaiVoiceSeed)
		voice, err := audio.PickXAITTSVoice(voices, seed)
		if err != nil {
			return err
		}
		return writeJSON(cmd.OutOrStdout(), map[string]any{
			"provider":        "xai-oauth",
			"voice":           voice,
			"seed":            seed,
			"candidate_count": len(voices),
		})
	},
}

var xaiVoiceProbeCmd = &cobra.Command{
	Use:   "probe",
	Short: "Generate test audio through xAI OAuth TTS",
	RunE: func(cmd *cobra.Command, args []string) error {
		return runXAIVoiceProbe(cmd.Context(), cfg, xaiVoiceProbeOptions{
			Text:      xaiVoiceText,
			Voice:     xaiVoiceID,
			Voices:    xaiVoiceIDs,
			Random:    xaiVoiceRandom,
			Seed:      xaiVoiceSeed,
			Language:  xaiVoiceLanguage,
			Codec:     xaiVoiceCodec,
			Output:    xaiVoiceOutput,
			OutputDir: xaiVoiceOutputDir,
		}, cmd.OutOrStdout())
	},
}

type xaiVoiceProbeOptions struct {
	Text      string
	Voice     string
	Voices    string
	Random    bool
	Seed      int64
	Language  string
	Codec     string
	Output    string
	OutputDir string
}

type xaiVoiceProbeSummary struct {
	Provider string                `json:"provider"`
	TextLen  int                   `json:"text_len"`
	Random   bool                  `json:"random"`
	Seed     int64                 `json:"seed,omitempty"`
	Results  []xaiVoiceProbeResult `json:"results"`
}

type xaiVoiceProbeResult struct {
	VoiceID  string `json:"voice_id"`
	Language string `json:"language"`
	Codec    string `json:"codec"`
	Output   string `json:"output,omitempty"`
	Bytes    int    `json:"bytes,omitempty"`
	Status   string `json:"status"`
	Error    string `json:"error,omitempty"`
}

func runXAIVoiceProbe(ctx context.Context, appCfg *config.Config, opts xaiVoiceProbeOptions, out io.Writer) error {
	text := strings.TrimSpace(opts.Text)
	if text == "" {
		return fmt.Errorf("xai voice probe text is empty")
	}
	codec := normalizedXAIVoiceCodec(opts.Codec)
	voices := xaiVoiceCandidates(opts.Voice, opts.Voices)
	seed := effectiveXAISeed(opts.Seed)
	if opts.Random {
		voice, err := audio.PickXAITTSVoice(voices, seed)
		if err != nil {
			return err
		}
		voices = []audio.XAITTSVoice{voice}
	}

	synth, err := newXAIVoiceSynthesizer(appCfg)
	if err != nil {
		return err
	}
	if synth == nil {
		return fmt.Errorf("xai voice synthesizer is nil")
	}

	summary := xaiVoiceProbeSummary{
		Provider: "xai-oauth",
		TextLen:  len([]rune(text)),
		Random:   opts.Random,
		Seed:     seed,
	}
	successes := 0
	for i, voice := range voices {
		language := strings.TrimSpace(opts.Language)
		if language == "" {
			language = voice.Language
		}
		if language == "" {
			language = "en"
		}
		result, err := synth.Synthesize(ctx, text, audio.XAITTSOptions{
			VoiceID:  voice.ID,
			Language: language,
			Codec:    codec,
		})
		probe := xaiVoiceProbeResult{
			VoiceID:  voice.ID,
			Language: language,
			Codec:    codec,
		}
		if err != nil {
			probe.Status = "error"
			probe.Error = err.Error()
			summary.Results = append(summary.Results, probe)
			continue
		}
		outputPath := xaiVoiceOutputPath(opts, voice.ID, codec, len(voices), i)
		if err := os.MkdirAll(filepath.Dir(outputPath), 0755); err != nil {
			probe.Status = "error"
			probe.Error = err.Error()
			summary.Results = append(summary.Results, probe)
			continue
		}
		if err := os.WriteFile(outputPath, result.Data, 0644); err != nil {
			probe.Status = "error"
			probe.Error = err.Error()
			summary.Results = append(summary.Results, probe)
			continue
		}
		probe.Status = "ok"
		probe.Output = outputPath
		probe.Bytes = len(result.Data)
		if result.Codec != "" {
			probe.Codec = result.Codec
		}
		if result.Language != "" {
			probe.Language = result.Language
		}
		successes++
		summary.Results = append(summary.Results, probe)
	}

	if err := writeJSON(out, summary); err != nil {
		return err
	}
	if successes == 0 {
		return fmt.Errorf("xai voice probe failed for all %d voice candidates", len(voices))
	}
	return nil
}

func xaiVoiceCandidates(single string, raw string) []audio.XAITTSVoice {
	if strings.TrimSpace(single) != "" {
		return []audio.XAITTSVoice{{ID: strings.TrimSpace(single), Language: "en"}}
	}
	if strings.TrimSpace(raw) == "" {
		return audio.DefaultXAITTSVoices()
	}
	parts := strings.Split(raw, ",")
	voices := make([]audio.XAITTSVoice, 0, len(parts))
	for _, part := range parts {
		id := strings.TrimSpace(part)
		if id == "" {
			continue
		}
		voices = append(voices, audio.XAITTSVoice{ID: id, Language: "en"})
	}
	return voices
}

func effectiveXAISeed(seed int64) int64 {
	if seed != 0 {
		return seed
	}
	return time.Now().UnixNano()
}

func normalizedXAIVoiceCodec(raw string) string {
	codec := strings.TrimPrefix(strings.ToLower(strings.TrimSpace(raw)), ".")
	if codec == "" {
		return "mp3"
	}
	return codec
}

func xaiVoiceOutputPath(opts xaiVoiceProbeOptions, voiceID, codec string, total, index int) string {
	if total == 1 && strings.TrimSpace(opts.Output) != "" {
		return opts.Output
	}
	outputDir := strings.TrimSpace(opts.OutputDir)
	if outputDir == "" {
		outputDir = filepath.Join("outputs", "xai-voice-probe")
	}
	name := sanitizeVoiceFilename(voiceID)
	if name == "" {
		name = "voice_" + strconv.Itoa(index+1)
	}
	return filepath.Join(outputDir, name+"."+codec)
}

func sanitizeVoiceFilename(raw string) string {
	raw = strings.ToLower(strings.TrimSpace(raw))
	var b strings.Builder
	for _, r := range raw {
		if r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '-' || r == '_' {
			b.WriteRune(r)
			continue
		}
		b.WriteByte('_')
	}
	return strings.Trim(b.String(), "_")
}

func writeJSON(out io.Writer, value any) error {
	enc := json.NewEncoder(out)
	enc.SetIndent("", "  ")
	return enc.Encode(value)
}

func init() {
	xaiVoicePickCmd.Flags().Int64Var(&xaiVoiceSeed, "seed", 0, "random seed (default: current time)")
	xaiVoicePickCmd.Flags().StringVar(&xaiVoiceIDs, "voices", "", "comma-separated xAI voice candidates")

	xaiVoiceProbeCmd.Flags().StringVar(&xaiVoiceText, "text", "Hello from StagentHand.", "text to synthesize")
	xaiVoiceProbeCmd.Flags().StringVar(&xaiVoiceID, "voice", "", "single xAI voice candidate")
	xaiVoiceProbeCmd.Flags().StringVar(&xaiVoiceIDs, "voices", "", "comma-separated xAI voice candidates")
	xaiVoiceProbeCmd.Flags().BoolVar(&xaiVoiceRandom, "random", false, "pick one random voice from candidates")
	xaiVoiceProbeCmd.Flags().Int64Var(&xaiVoiceSeed, "seed", 0, "random seed used with --random")
	xaiVoiceProbeCmd.Flags().StringVar(&xaiVoiceLanguage, "language", "", "xAI TTS language override")
	xaiVoiceProbeCmd.Flags().StringVar(&xaiVoiceCodec, "codec", "mp3", "audio codec hint (mp3 or wav)")
	xaiVoiceProbeCmd.Flags().StringVar(&xaiVoiceOutput, "output", "", "output file when probing one voice")
	xaiVoiceProbeCmd.Flags().StringVar(&xaiVoiceOutputDir, "output-dir", filepath.Join("outputs", "xai-voice-probe"), "output directory for multiple voices")

	xaiVoiceCmd.AddCommand(xaiVoiceVoicesCmd)
	xaiVoiceCmd.AddCommand(xaiVoicePickCmd)
	xaiVoiceCmd.AddCommand(xaiVoiceProbeCmd)
	xaiCmd.AddCommand(xaiVoiceCmd)
}
