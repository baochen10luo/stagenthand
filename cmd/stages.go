package cmd

import (
	"context"
	"fmt"
	"io"
	"os"

	"github.com/baochen10luo/stagenthand/internal/llm"
	"github.com/baochen10luo/stagenthand/internal/pipeline"
	"github.com/spf13/cobra"
)

// runStage is the generic handler for all transformation stages.
func runStage(systemPrompt string) func(cmd *cobra.Command, args []string) error {
	return func(cmd *cobra.Command, args []string) error {
		inputData, err := io.ReadAll(os.Stdin)
		if err != nil {
			return fmt.Errorf("reading stdin: %w", err)
		}

		provider := "mock"
		if cfg != nil && cfg.LLM.Provider != "" {
			provider = cfg.LLM.Provider
		}

		client, err := llm.NewClient(provider, dryRun, cfg)
		if err != nil {
			return fmt.Errorf("llm client factory: %w", err)
		}

		out, err := pipeline.RunTransformationStage(context.Background(), client, systemPrompt, inputData)
		if err != nil {
			return err
		}

		// STDOUT outputs JSON purely.
		fmt.Fprintln(os.Stdout, string(out))
		return nil
	}
}

var storyToOutlineCmd = &cobra.Command{
	Use:   "story-to-outline",
	Short: "Convert story prompt into a structured outline",
	RunE:  runStage(pipeline.PromptStoryToOutline),
}

var outlineToStoryboardCmd = &cobra.Command{
	Use:   "outline-to-storyboard",
	Short: "Convert an outline into a storyboard",
	RunE:  runStage(pipeline.PromptOutlineToStoryboard),
}

var storyboardToPanelsCmd = &cobra.Command{
	Use:   "storyboard-to-panels",
	Short: "Convert a storyboard into image panels",
	RunE:  runStage(pipeline.PromptStoryboardToPanels),
}

func init() {
	rootCmd.AddCommand(storyToOutlineCmd)
	rootCmd.AddCommand(outlineToStoryboardCmd)
	rootCmd.AddCommand(storyboardToPanelsCmd)
}
