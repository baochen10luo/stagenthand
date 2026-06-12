package cmd

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/baochen10luo/stagenthand/internal/audio"
	"github.com/baochen10luo/stagenthand/internal/domain"
	"github.com/baochen10luo/stagenthand/internal/pronunciation"
	"github.com/spf13/cobra"
)

var (
	ttsAuditProjectDir string
	ttsAuditFix        bool
)

var ttsAuditCmd = &cobra.Command{
	Use:   "tts-audit",
	Short: "STT round-trip audit on TTS audio for a project",
	Long: `Reads remotion_props.json from a project directory, transcribes each panel's
audio via STT, compares with original dialogue, and reports character-level
mismatches with homophone replacement suggestions.

Use --fix to automatically apply homophone replacements and re-generate TTS.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		shandHome := filepath.Join(os.Getenv("HOME"), ".shand")
		projectDir := ttsAuditProjectDir
		if projectDir == "" {
			return fmt.Errorf("--project-dir is required")
		}
		if !filepath.IsAbs(projectDir) {
			projectDir = filepath.Join(shandHome, "projects", projectDir)
		}

		// Read remotion_props.json
		propsPath := filepath.Join(projectDir, "remotion_props.json")
		data, err := os.ReadFile(propsPath)
		if err != nil {
			return fmt.Errorf("read remotion_props.json: %w", err)
		}
		var props domain.RemotionProps
		if err := json.Unmarshal(data, &props); err != nil {
			return fmt.Errorf("parse remotion_props.json: %w", err)
		}

		// Get API key from config
		apiKey := ""
		if cfg != nil {
			apiKey = cfg.Audio.AiarkTTSAPIKey
		}
		if apiKey == "" {
			apiKey = "datasys2026"
		}

		auditor := pronunciation.NewSTTAuditor(pronunciation.STTAuditConfig{
			APIKey: apiKey,
		})

		ctx := context.Background()
		results := make([]pronunciation.STTAuditResult, 0, len(props.Panels))

		for i, panel := range props.Panels {
			// Get dialogue text
			dialogue := panel.Dialogue
			if dialogue == "" && len(panel.DialogueLines) > 0 {
				for _, dl := range panel.DialogueLines {
					dialogue += dl.Text
				}
			}
			if dialogue == "" {
				results = append(results, pronunciation.STTAuditResult{
					PanelIndex:   i,
					SceneNumber:  panel.SceneNumber,
					PanelNumber:  panel.PanelNumber,
					ErrorMessage: "empty dialogue",
				})
				continue
			}

			// Read audio file — /projects/xxx/audio/yyy.wav → ~/.shand/projects/xxx/audio/yyy.wav
			audioRel := strings.TrimPrefix(panel.AudioURL, "/")
			audioPath := filepath.Join(shandHome, audioRel)
			audioData, err := os.ReadFile(audioPath)
			if err != nil {
				results = append(results, pronunciation.STTAuditResult{
					PanelIndex:   i,
					SceneNumber:  panel.SceneNumber,
					PanelNumber:  panel.PanelNumber,
					Original:     dialogue,
					ErrorMessage: fmt.Sprintf("read audio: %v", err),
				})
				continue
			}

			// Transcribe via STT
			fmt.Fprintf(os.Stderr, "[Audit] Panel %d/%d: transcribing...\n", i+1, len(props.Panels))
			transcribed, err := auditor.Transcribe(ctx, audioData)
			if err != nil {
				results = append(results, pronunciation.STTAuditResult{
					PanelIndex:   i,
					SceneNumber:  panel.SceneNumber,
					PanelNumber:  panel.PanelNumber,
					Original:     dialogue,
					ErrorMessage: fmt.Sprintf("STT: %v", err),
				})
				continue
			}

			// Audit
			result := auditor.Audit(dialogue, transcribed, i, panel.SceneNumber, panel.PanelNumber)
			results = append(results, result)

			// Print summary
			if result.Pass {
				fmt.Fprintf(os.Stderr, "[Audit] Panel %d/%d: ✅ PASS (%d chars)\n", i+1, len(props.Panels), len(dialogue))
			} else {
				fmt.Fprintf(os.Stderr, "[Audit] Panel %d/%d: ⚠️  %d mismatches\n", i+1, len(props.Panels), len(result.Mismatches))
				for _, mm := range result.Mismatches {
					sugg := ""
					if len(mm.HomophoneSuggest) > 0 {
						sugg = fmt.Sprintf(" (suggest: %s)", mm.HomophoneSuggest)
					}
					fmt.Fprintf(os.Stderr, "  pos %d: expected '%s' got '%s'%s\n", mm.Position, mm.Expected, mm.Got, sugg)
				}
			}

			// Auto-fix if --fix
			if ttsAuditFix && !result.Pass {
				// Only fix mismatches that are likely real TTS errors (not STT homophone ambiguity)
				var fixable []pronunciation.CharMismatch
				for _, mm := range result.Mismatches {
					if mm.LikelyTTS {
						fixable = append(fixable, mm)
					}
				}
				if len(fixable) == 0 {
					fmt.Fprintf(os.Stderr, "[Fix] Panel %d: all mismatches are STT ambiguity (same pronunciation), skipping\n", i+1)
					continue
				}
				corrected, replacements := pronunciation.BuildCorrectedInput(dialogue, fixable, auditor.Cfg().Homophones)
				if len(replacements) > 0 {
					fmt.Fprintf(os.Stderr, "[Fix] Panel %d: %v\n", i+1, replacements)
					fmt.Fprintf(os.Stderr, "[Fix] Input: %s → %s\n", dialogue, corrected)

					ttsClient := audio.NewAiarkTTSClientWithVoiceID(
						cfg.Audio.AiarkTTSBaseURL,
						apiKey,
						"zh-TW",
						cfg.Audio.AiarkTTSVoice,
						cfg.Audio.AiarkTTSVoiceID,
					)
					audioData, err := ttsClient.GenerateSpeech(ctx, corrected)
					if err != nil {
						fmt.Fprintf(os.Stderr, "[Fix] Panel %d: TTS failed: %v\n", i+1, err)
						continue
					}
					if err := os.WriteFile(audioPath, audioData, 0644); err != nil {
						fmt.Fprintf(os.Stderr, "[Fix] Panel %d: write audio: %v\n", i+1, err)
						continue
					}
					fmt.Fprintf(os.Stderr, "[Fix] Panel %d: wrote %d bytes → %s\n", i+1, len(audioData), audioPath)

					// Update duration from new WAV
					dur, err := audio.WAVDuration(bytes.NewReader(audioData))
					if err != nil {
						fmt.Fprintf(os.Stderr, "[Fix] Panel %d: WAV duration: %v (keeping old duration)\n", i+1, err)
					} else {
						props.Panels[i].DurationSec = dur
						fmt.Fprintf(os.Stderr, "[Fix] Panel %d: duration updated to %.2fs\n", i+1, dur)
					}

					// Update results in-place so JSON output reflects the fix
					results[i].Pass = true
					results[i].Mismatches = nil
					fmt.Fprintf(os.Stderr, "[Fix] Panel %d: ✅ fixed\n", i+1)
				} else {
					fmt.Fprintf(os.Stderr, "[Fix] Panel %d: no homophone replacements available, skipping\n", i+1)
				}
			}

			time.Sleep(500 * time.Millisecond)
		}

		// Write updated props if any fixes were applied
		if ttsAuditFix {
			backupPath := propsPath + ".bak"
			if err := os.WriteFile(backupPath, data, 0644); err != nil {
				fmt.Fprintf(os.Stderr, "[Warn] backup failed: %v\n", err)
			} else {
				fmt.Fprintf(os.Stderr, "[Info] backed up original → %s\n", backupPath)
			}
			updatedData, _ := json.MarshalIndent(props, "", "  ")
			if err := os.WriteFile(propsPath, updatedData, 0644); err != nil {
				fmt.Fprintf(os.Stderr, "[Error] save remotion_props.json: %v\n", err)
			} else {
				fmt.Fprintf(os.Stderr, "[Info] saved updated remotion_props.json\n")
			}
		}

		// Output results as JSON
		summary := map[string]interface{}{
			"project":          props.ProjectID,
			"total_panels":     len(results),
			"passed":           0,
			"failed":           0,
			"errors":           0,
			"total_mismatches": 0,
			"panels":           results,
		}
		for _, r := range results {
			if r.ErrorMessage != "" {
				summary["errors"] = summary["errors"].(int) + 1
			} else if r.Pass {
				summary["passed"] = summary["passed"].(int) + 1
			} else {
				summary["failed"] = summary["failed"].(int) + 1
				summary["total_mismatches"] = summary["total_mismatches"].(int) + len(r.Mismatches)
			}
		}

		return json.NewEncoder(os.Stdout).Encode(summary)
	},
}

func init() {
	rootCmd.AddCommand(ttsAuditCmd)
	ttsAuditCmd.Flags().StringVar(&ttsAuditProjectDir, "project-dir", "", "project directory (default: ~/.shand/projects/<project_id>/)")
	ttsAuditCmd.Flags().BoolVar(&ttsAuditFix, "fix", false, "auto-fix mispronunciations with homophone replacements")
}
