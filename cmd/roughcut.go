package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

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

		panels := manifest.Panels

		// ── 2. Pull Notion edits (if token available) ────────────────────────
		if token != "" {
			notionPanels, notionErr := notion.ReadPanels(cmd.Context(), pageID, manifest.StoryTitle, token)
			if notionErr != nil {
				fmt.Fprintf(os.Stderr, "[Warning] Notion read skipped: %v\n", notionErr)
			} else {
				panels = mergeNotionEdits(panels, notionPanels)
			}
		} else {
			fmt.Fprintln(os.Stderr, "[Warning] NOTION_API_KEY not set; using manifest dialogue as-is")
		}

		if !dryRun {
			// ── 3. Generate TTS audio ────────────────────────────────────────
			shandHome, _ := os.UserHomeDir()
			shandHome = filepath.Join(shandHome, ".shand")

			audioClient := audio.NewPollyCLIClientWithLanguage(
				cfg.LLM.AWSRegion, cfg.LLM.AWSAccessKeyID, cfg.LLM.AWSSecretAccessKey,
				roughCutLanguage,
			)
			audioBatcher := pipeline.NewAudioClientBatcher(audioClient, shandHome)
			audioDir := filepath.Join("projects", manifest.ProjectID, "audio")

			panels, err = audioBatcher.BatchGenerateAudio(cmd.Context(), panels, audioDir)
			if err != nil {
				return stageError("rough-cut", "tts_error", err.Error())
			}

			// ── 4. Align duration to real audio length ───────────────────────
			panels = pipeline.ApplyRealAudioDuration(panels)
		}

		panels = pipeline.ApplySubtitleTimings(panels)

		// ── 5. Build and write RemotionProps ─────────────────────────────────
		videoFormat := render.VideoFormatLandscape
		if roughCutFormat == "portrait" {
			videoFormat = render.VideoFormatPortrait
		}

		props := remotion.PanelsToPropsWithFormat(
			manifest.ProjectID, panels,
			0, 0, 24, "", nil, videoFormat,
		)
		props.Title = manifest.StoryTitle

		propsPath := filepath.Join(roughCutOutputDir, "remotion_props.json")
		propsData, _ := json.MarshalIndent(props, "", "  ")
		if err := os.WriteFile(propsPath, propsData, 0644); err != nil {
			return stageError("rough-cut", "write_error", err.Error())
		}
		fmt.Fprintf(os.Stderr, "[Info] remotion_props.json written → %s\n", propsPath)

		// ── 6. Render ────────────────────────────────────────────────────────
		if !roughCutSkipRender && !dryRun {
			templatePath := "./remotion-template"
			if cfg != nil && cfg.Remotion.TemplatePath != "" {
				templatePath = cfg.Remotion.TemplatePath
			}
			outputPath := filepath.Join(roughCutOutputDir, "rough_cut.mp4")
			executor := remotion.NewCLIExecutor(false)
			if renderErr := executor.Render(cmd.Context(), templatePath, "ShortDrama", propsPath, outputPath); renderErr != nil {
				return stageError("rough-cut", "render_error", renderErr.Error())
			}
			fmt.Fprintf(os.Stderr, "[Info] 粗剪完成 → %s\n", outputPath)
		}

		result := map[string]any{
			"project_id":  manifest.ProjectID,
			"story_title": manifest.StoryTitle,
			"panel_count": len(panels),
			"props_path":  propsPath,
		}
		return json.NewEncoder(os.Stdout).Encode(result)
	},
}

// mergeNotionEdits overlays Notion-edited dialogue/description onto manifest panels.
// Matching is by panel order (幕 01 → panels[0], 幕 02 → panels[1], …).
// Image paths are always taken from the manifest to ensure local files are used.
func mergeNotionEdits(manifest []domain.Panel, notion []domain.Panel) []domain.Panel {
	result := make([]domain.Panel, len(manifest))
	copy(result, manifest)
	for i := range result {
		if i >= len(notion) {
			break
		}
		if notion[i].Dialogue != "" {
			result[i].Dialogue = notion[i].Dialogue
		}
		if notion[i].Description != "" {
			result[i].Description = notion[i].Description
		}
		// Always keep manifest's ImageURL (local path).
	}
	return result
}

func init() {
	roughCutCmd.Flags().StringVar(&roughCutOutputDir, "output-dir", "",
		"project output directory containing storyboard_manifest.json")
	roughCutCmd.Flags().StringVar(&roughCutPageID, "notion-page-id", "",
		"Notion page ID to read edits from (overrides NOTION_GROK_PAGE_ID env var)")
	roughCutCmd.Flags().StringVar(&roughCutLanguage, "language", "zh-TW",
		"TTS language (zh-TW, en-US, ja-JP, ko-KR, …)")
	roughCutCmd.Flags().StringVar(&roughCutFormat, "format", "landscape",
		"output video format: landscape or portrait")
	roughCutCmd.Flags().BoolVar(&roughCutSkipRender, "skip-render", false,
		"write remotion_props.json but skip Remotion render")
	rootCmd.AddCommand(roughCutCmd)
}
