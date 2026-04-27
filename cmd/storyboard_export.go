package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/baochen10luo/stagenthand/internal/domain"
	"github.com/spf13/cobra"
)

var storyboardExportOutputDir string

var storyboardExportCmd = &cobra.Command{
	Use:   "storyboard-export",
	Short: "Export a storyboard manifest from an existing project for Notion upload",
	Long: `Reads remotion_props.json from --output-dir and writes storyboard_manifest.json.
The manifest captures panel order, image paths, and dialogue for use with notion-push and rough-cut.
AudioURL fields are cleared so rough-cut regenerates TTS from Notion-edited dialogue.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		if storyboardExportOutputDir == "" {
			return stageError("storyboard-export", "missing_flag", "--output-dir is required")
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

		outPath := filepath.Join(storyboardExportOutputDir, "storyboard_manifest.json")
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
	rootCmd.AddCommand(storyboardExportCmd)
}
