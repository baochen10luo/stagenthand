package cmd

import (
	"io"
	"os"
	"path/filepath"

	"github.com/baochen10luo/stagenthand/internal/remotion"
	"github.com/spf13/cobra"
)

var renderOutput string
var renderPropsPath string

var remotionRenderCmd = &cobra.Command{
	Use:   "remotion-render",
	Short: "Render the final MP4 video using remotion",
	RunE: func(cmd *cobra.Command, args []string) error {
		executor := remotion.NewCLIExecutor(dryRun)

		// Read props from stdin or path
		propsFile := renderPropsPath
		if propsFile == "" {
			// Save stdin to temporary props file
			propsData, err := io.ReadAll(os.Stdin)
			if err != nil {
				return err
			}
			f, err := os.CreateTemp("", "shand-props-*.json")
			if err != nil {
				return err
			}
			defer os.Remove(f.Name())
			f.Write(propsData)
			f.Close()
			propsFile = f.Name()
		}

		if renderOutput == "" {
			renderOutput = "out.mp4"
		}

		// Ensure absolute paths
		absProps, _ := filepath.Abs(propsFile)
		absOutput, _ := filepath.Abs(renderOutput)
		
		rawTemplatePath := cfg.Remotion.TemplatePath
		if rawTemplatePath == "" {
			rawTemplatePath = "./remotion-template"
		}
		templatePath, _ := filepath.Abs(rawTemplatePath)

		composition := cfg.Remotion.Composition
		if composition == "" {
			composition = "ShortDrama"
		}

		if err := executor.Render(cmd.Context(), templatePath, composition, absProps, absOutput); err != nil {
			return err
		}

		return nil
	},
}

func init() {
	remotionRenderCmd.Flags().StringVarP(&renderOutput, "output", "o", "out.mp4", "Output video path")
	remotionRenderCmd.Flags().StringVarP(&renderPropsPath, "props", "p", "", "Path to props json file (leave empty to use stdin)")
	rootCmd.AddCommand(remotionRenderCmd)
}
