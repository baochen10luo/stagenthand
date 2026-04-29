package cmd

import (
	"encoding/json"
	"fmt"
	"image"
	_ "image/jpeg"
	_ "image/png"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"github.com/baochen10luo/stagenthand/internal/audio"
	"github.com/baochen10luo/stagenthand/internal/domain"
	"github.com/baochen10luo/stagenthand/internal/notion"
	"github.com/baochen10luo/stagenthand/internal/pipeline"
	"github.com/baochen10luo/stagenthand/internal/remotion"
	"github.com/baochen10luo/stagenthand/internal/render"
	"github.com/spf13/cobra"
)

var (
	roughCutOutputDir  string
	roughCutPageID     string
	roughCutLanguage   string
	roughCutFormat     string
	roughCutSkipRender bool
)

var roughCutCmd = &cobra.Command{
	Use:   "rough-cut",
	Short: "Generate a rough-cut video from Notion-edited storyboard + existing images",
	Long: `Reads storyboard_manifest.json (project structure + image paths) and pulls the
latest dialogue edits from the Notion storyboard database. Regenerates TTS audio
with the edited dialogue, aligns each panel's duration to real audio length, and
renders a first-pass MP4 for evaluation. No LLM calls; no image generation.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		if roughCutOutputDir == "" {
			return stageError("rough-cut", "missing_flag", "--output-dir is required")
		}

		pageID := roughCutPageID
		if pageID == "" {
			pageID = os.Getenv("NOTION_GROK_PAGE_ID")
		}
		if pageID == "" {
			pageID = "3485ee2ef54d8034bb8ceabf27c3f29c"
		}
		token := os.Getenv("NOTION_API_KEY")

		// ── 1. Load storyboard manifest ──────────────────────────────────────
		manifestPath := filepath.Join(roughCutOutputDir, "storyboard_manifest.json")
		data, err := os.ReadFile(manifestPath)
		if err != nil {
			return stageError("rough-cut", "read_error",
				fmt.Sprintf("could not read %s: %v", manifestPath, err))
		}
		var manifest domain.StoryboardManifest
		if err := json.Unmarshal(data, &manifest); err != nil {
			return stageError("rough-cut", "parse_error",
				fmt.Sprintf("could not parse storyboard_manifest.json: %v", err))
		}

		// Resolve TTS language: flag > manifest > default.
		lang := roughCutLanguage
		if lang == "" {
			lang = manifest.Language
		}
		if lang == "" {
			lang = "zh-TW"
		}

		panels := manifest.Panels

		// ── 2. Pull Notion edits (if token available) ────────────────────────
		if token != "" {
			notionPanels, notionErr := notion.ReadPanels(cmd.Context(), pageID, manifest.StoryTitle, token)
			if notionErr != nil {
				fmt.Fprintf(os.Stderr, "[Warning] Notion read skipped: %v\n", notionErr)
			} else {
				panels = mergeNotionEdits(manifest.Panels, notionPanels)
			}
		} else {
			fmt.Fprintln(os.Stderr, "[Warning] NOTION_API_KEY not set; using manifest dialogue as-is")
		}

		if !dryRun {
			if err := validateImagePaths(panels); err != nil {
				return stageError("rough-cut", "missing_images", err.Error())
			}
		}

		shandHome, _ := os.UserHomeDir()
		shandHome = filepath.Join(shandHome, ".shand")
		audioDir := filepath.Join("projects", manifest.ProjectID, "audio")
		fullAudioDir := filepath.Join(shandHome, audioDir)

		if !dryRun {
			// ── 3. Prune stale audio for panels whose Notion dialogue changed ─
			pruneStaleAudio(manifest.Panels, panels, fullAudioDir)

			// ── 4. Generate TTS audio ────────────────────────────────────────
			audioClient := audio.NewPollyCLIClientWithLanguage(
				cfg.LLM.AWSRegion, cfg.LLM.AWSAccessKeyID, cfg.LLM.AWSSecretAccessKey,
				lang,
			)
			audioBatcher := pipeline.NewAudioClientBatcher(audioClient, shandHome)

			panels, err = audioBatcher.BatchGenerateAudio(cmd.Context(), panels, audioDir)
			if err != nil {
				return stageError("rough-cut", "tts_error", err.Error())
			}

			// ── 5. Set duration = real audio length ──────────────────────────
			panels = pipeline.OverrideDurationFromAudio(panels)
		}

		panels = pipeline.ApplySubtitleTimings(panels)

		// ── 6. BGM: reuse existing or download via Jamendo ───────────────────
		bgmURL := ""
		if !dryRun && manifest.BGMTags != "" && cfg.Audio.JamendoClientID != "" {
			bgmPath := filepath.Join(fullAudioDir, "bgm.mp3")
			if info, statErr := os.Stat(bgmPath); statErr == nil && info.Size() > 0 {
				bgmURL = bgmPath
				fmt.Fprintf(os.Stderr, "[Info] BGM reused: %s\n", bgmPath)
			} else {
				musicClient := audio.NewJamendoClient(cfg.Audio.JamendoClientID)
				musicBatcher := pipeline.NewMusicClientBatcher(musicClient, shandHome)
				bgm, bgmErr := musicBatcher.GenerateProjectBGM(cmd.Context(), manifest.ProjectID, manifest.BGMTags, audioDir)
				if bgmErr != nil {
					fmt.Fprintf(os.Stderr, "[Warning] BGM download failed: %v\n", bgmErr)
				} else {
					bgmURL = bgm
					fmt.Fprintf(os.Stderr, "[Info] BGM downloaded: %s\n", bgmURL)
				}
			}
		}

		// ── 7. Build Directives from manifest ────────────────────────────────
		var directives *domain.Directives
		if manifest.BGMTags != "" || manifest.ColorFilter != "" || manifest.StylePrompt != "" || manifest.Language != "" {
			directives = &domain.Directives{
				BGMTags:     manifest.BGMTags,
				ColorFilter: manifest.ColorFilter,
				StylePrompt: manifest.StylePrompt,
				Language:    manifest.Language,
			}
		}

		// ── 8. Build and write RemotionProps ─────────────────────────────────
		width, height := 0, 0
		videoFormat := render.VideoFormatLandscape
		switch roughCutFormat {
		case "portrait":
			videoFormat = render.VideoFormatPortrait
		case "landscape":
			videoFormat = render.VideoFormatLandscape
		default: // "" or "auto": read from first available image
			for _, p := range panels {
				if p.ImageURL != "" {
					if w, h, err := readImageDimensions(p.ImageURL); err == nil {
						width, height = w, h
						fmt.Fprintf(os.Stderr, "[Info] canvas size auto-detected from image: %dx%d\n", w, h)
					} else {
						fmt.Fprintf(os.Stderr, "[Warning] could not read image dimensions from %s: %v\n", p.ImageURL, err)
					}
					break
				}
			}
		}

		props := remotion.PanelsToPropsWithFormat(
			manifest.ProjectID, panels,
			width, height, 24, bgmURL, directives, videoFormat,
		)
		props.Title = manifest.StoryTitle

		propsPath := filepath.Join(roughCutOutputDir, "remotion_props.json")
		propsData, _ := json.MarshalIndent(props, "", "  ")
		if err := os.WriteFile(propsPath, propsData, 0644); err != nil {
			return stageError("rough-cut", "write_error", err.Error())
		}
		fmt.Fprintf(os.Stderr, "[Info] remotion_props.json written → %s\n", propsPath)

		// ── 9. Render ────────────────────────────────────────────────────────
		if !roughCutSkipRender && !dryRun {
			templatePath := "./remotion-template"
			if cfg != nil && cfg.Remotion.TemplatePath != "" {
				templatePath = cfg.Remotion.TemplatePath
			}
			outputPath := filepath.Join(roughCutOutputDir, manifest.ProjectID+".mp4")
			executor := remotion.NewCLIExecutorWithPublicDir(false, shandHome)
			if renderErr := executor.Render(cmd.Context(), templatePath, "ShortDrama", propsPath, outputPath); renderErr != nil {
				return stageError("rough-cut", "render_error", renderErr.Error())
			}
			fmt.Fprintf(os.Stderr, "[Info] 粗剪完成 → %s\n", outputPath)
		}

		outputFile := filepath.Join(roughCutOutputDir, manifest.ProjectID+".mp4")
		result := map[string]any{
			"project_id":  manifest.ProjectID,
			"story_title": manifest.StoryTitle,
			"panel_count": len(panels),
			"output":      outputFile,
			"bgm":         bgmURL != "",
		}
		return json.NewEncoder(os.Stdout).Encode(result)
	},
}

// mergeNotionEdits overlays Notion-edited dialogue/description onto manifest panels.
// Matching is by panel order (幕 01 → panels[0], 幕 02 → panels[1], …).
// When Dialogue changes, DialogueLines is rebuilt as a single narration line so that
// subtitle timings reflect the new text. Image paths always come from the manifest.
func mergeNotionEdits(manifest []domain.Panel, notion []domain.Panel) []domain.Panel {
	result := make([]domain.Panel, len(manifest))
	copy(result, manifest)
	for i := range result {
		if i >= len(notion) {
			slog.Warn("mergeNotionEdits: Notion has fewer rows than manifest; leftover panel uses stale dialogue",
				"panel_index", i, "manifest_panels", len(manifest), "notion_panels", len(notion))
			continue
		}
		if notion[i].Dialogue != "" && notion[i].Dialogue != result[i].Dialogue {
			result[i].Dialogue = notion[i].Dialogue
			// Notion stores only the flat dialogue text; rebuild DialogueLines so
			// subtitle timings are computed from the edited text, not the old one.
			result[i].DialogueLines = []domain.DialogueLine{{Speaker: "", Text: notion[i].Dialogue}}
		}
		if notion[i].Description != "" {
			result[i].Description = notion[i].Description
		}
		// Always keep manifest's ImageURL (local path).
	}
	return result
}

// pruneStaleAudio deletes cached audio files for panels whose dialogue changed
// after Notion editing, so BatchGenerateAudio re-generates them instead of reusing stale files.
func pruneStaleAudio(original, merged []domain.Panel, audioDir string) {
	for i := range merged {
		if i >= len(original) {
			break
		}
		if original[i].Dialogue == merged[i].Dialogue {
			continue
		}
		filename := fmt.Sprintf("scene_%d_panel_%d.mp3", merged[i].SceneNumber, merged[i].PanelNumber)
		path := filepath.Join(audioDir, filename)
		if err := os.Remove(path); err == nil {
			fmt.Fprintf(os.Stderr, "[Info] cleared stale audio for panel %d (dialogue changed)\n", i+1)
		}
	}
}

// readImageDimensions returns the pixel dimensions of an image file without fully decoding it.
func readImageDimensions(path string) (int, int, error) {
	f, err := os.Open(path)
	if err != nil {
		return 0, 0, err
	}
	defer f.Close()
	cfg, _, err := image.DecodeConfig(f)
	if err != nil {
		return 0, 0, err
	}
	return cfg.Width, cfg.Height, nil
}

// validateImagePaths checks that every panel with a non-empty ImageURL points to an
// existing file. Returns nil if all paths exist. Returns an error listing missing files.
func validateImagePaths(panels []domain.Panel) error {
	var missing []string
	for _, p := range panels {
		if p.ImageURL == "" {
			continue
		}
		if _, err := os.Stat(p.ImageURL); os.IsNotExist(err) {
			missing = append(missing, p.ImageURL)
		}
	}
	if len(missing) > 0 {
		return fmt.Errorf("missing image files: %s", strings.Join(missing, ", "))
	}
	return nil
}

func init() {
	roughCutCmd.Flags().StringVar(&roughCutOutputDir, "output-dir", "",
		"project output directory containing storyboard_manifest.json")
	roughCutCmd.Flags().StringVar(&roughCutPageID, "notion-page-id", "",
		"Notion page ID to read edits from (overrides NOTION_GROK_PAGE_ID env var)")
	roughCutCmd.Flags().StringVar(&roughCutLanguage, "language", "",
		"TTS language (zh-TW, en-US, ja-JP, …); defaults to manifest language or zh-TW")
	roughCutCmd.Flags().StringVar(&roughCutFormat, "format", "",
		"output video format: landscape (1024×576), portrait (576×1024), or empty to auto-detect from image")
	roughCutCmd.Flags().BoolVar(&roughCutSkipRender, "skip-render", false,
		"write remotion_props.json but skip Remotion render")
	rootCmd.AddCommand(roughCutCmd)
}
