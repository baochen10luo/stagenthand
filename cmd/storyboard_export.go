package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/baochen10luo/stagenthand/internal/domain"
	"github.com/spf13/cobra"
)

var (
	storyboardExportOutputDir string
	storyboardExportForce     bool
)

var storyboardExportCmd = &cobra.Command{
	Use:   "storyboard-export",
	Short: "Export a storyboard manifest from an existing project for Notion upload",
	Long: `Reads remotion_props.json from --output-dir and writes storyboard_manifest.json.
The manifest captures panel order, image paths, and dialogue for use with notion-push and rough-cut.
AudioURL fields are cleared so rough-cut regenerates TTS from Notion-edited dialogue.

NOTE: This command is for retroactive export only. When running the full pipeline,
storyboard_manifest.json is automatically created after image generation. If the file
already exists (created by the pipeline), this command will exit with a message unless
--force is passed to overwrite it.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		if storyboardExportOutputDir == "" {
			return stageError("storyboard-export", "missing_flag", "--output-dir is required")
		}

		outPath := filepath.Join(storyboardExportOutputDir, "storyboard_manifest.json")

		// Check if manifest already exists (created by the pipeline).
		if _, err := os.Stat(outPath); err == nil && !storyboardExportForce {
			fmt.Fprintf(os.Stderr, "[Info] manifest already exists at %s; use --force to overwrite\n", outPath)
			// Read and emit existing manifest to stdout so callers get valid JSON.
			existingData, readErr := os.ReadFile(outPath)
			if readErr == nil {
				_, _ = os.Stdout.Write(existingData)
			}
			return nil
		}

		propsPath := filepath.Join(storyboardExportOutputDir, "remotion_props.json")
		data, err := os.ReadFile(propsPath)
		if err != nil {
			return stageError("storyboard-export", "read_error",
				fmt.Sprintf("could not read %s: %v", propsPath, err))
		}

		var props domain.RemotionProps
		if err := json.Unmarshal(data, &props); err != nil {
			return stageError("storyboard-export", "parse_error",
				fmt.Sprintf("could not parse remotion_props.json: %v", err))
		}

		// Strip AudioURL so rough-cut regenerates TTS from Notion-edited dialogue.
		panels := make([]domain.Panel, len(props.Panels))
		for i, p := range props.Panels {
			p.AudioURL = ""
			panels[i] = p
		}

		manifest := domain.StoryboardManifest{
			ProjectID:  props.ProjectID,
			StoryTitle: props.Title,
			Panels:     panels,
		}

		out, err := json.MarshalIndent(manifest, "", "  ")
		if err != nil {
			return stageError("storyboard-export", "marshal_error", err.Error())
		}
		if err := os.WriteFile(outPath, out, 0644); err != nil {
			return stageError("storyboard-export", "write_error", err.Error())
		}

		fmt.Fprintf(os.Stderr, "[Info] storyboard_manifest.json written → %s (%d panels)\n",
			outPath, len(panels))
		return json.NewEncoder(os.Stdout).Encode(manifest)
	},
}

func init() {
	storyboardExportCmd.Flags().StringVar(&storyboardExportOutputDir, "output-dir", "",
		"project output directory containing remotion_props.json")
	storyboardExportCmd.Flags().BoolVar(&storyboardExportForce, "force", false,
		"overwrite existing storyboard_manifest.json even if already created by the pipeline")
	rootCmd.AddCommand(storyboardExportCmd)
}
