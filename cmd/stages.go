package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"

	"github.com/baochen10luo/stagenthand/internal/llm"
	"github.com/baochen10luo/stagenthand/internal/pipeline"
	"github.com/spf13/cobra"
)

// runStage returns a cobra RunE handler for a single transformation stage.
// On error it writes a structured JSON payload to stderr before returning,
// guaranteeing non-zero exit and machine-parseable failure information.
func runStage(systemPrompt string) func(cmd *cobra.Command, args []string) error {
	return func(cmd *cobra.Command, args []string) error {
		inputData, err := io.ReadAll(os.Stdin)
		if err != nil {
			return stageError(cmd.Use, "stdin_read_error", fmt.Sprintf("reading stdin: %v", err))
		}

		provider := "mock"
		if cfg != nil && cfg.LLM.Provider != "" {
			provider = cfg.LLM.Provider
		}

		client, err := llm.NewClient(provider, dryRun, cfg)
		if err != nil {
			return stageError(cmd.Use, "llm_init_error", fmt.Sprintf("llm client factory: %v", err))
		}

		out, err := pipeline.RunTransformationStage(context.Background(), client, systemPrompt, inputData)
		if err != nil {
			return stageError(cmd.Use, "pipeline_error", err.Error())
		}

		fmt.Fprintln(os.Stdout, string(out))
		return nil
	}
}

// stageError writes a structured JSON error to stderr and returns it as an error
// so cobra propagates the non-zero exit correctly.
func stageError(command, code, msg string) error {
	p := errorPayload{Error: msg, Code: code, Command: command}
	data, _ := json.Marshal(p)
	fmt.Fprintln(os.Stderr, string(data))
	// Return the plain message so cobra doesn't double-print a confusing wrapper.
	return fmt.Errorf("%s", msg)
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
