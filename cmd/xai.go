package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"

	xpipe "github.com/baochen10luo/stagenthand/internal/xaipipeline"
	"github.com/spf13/cobra"
)

var xaiInspectStrict bool

var xaiCmd = &cobra.Command{
	Use:   "xai",
	Short: "Inspect xAI-native pipeline artifacts",
}

var xaiInspectCmd = &cobra.Command{
	Use:   "inspect <output-dir>",
	Short: "Summarize xAI-native pipeline artifacts",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		return runXAIInspect(args[0], xaiInspectStrict, os.Stdout)
	},
}

var xaiValidateCmd = &cobra.Command{
	Use:   "validate <output-dir>",
	Short: "Validate xAI-native production output",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		return runXAIValidate(cmd.Context(), args[0], xpipe.NewFFprobeOutputValidator(), os.Stdout)
	},
}

func buildXAIInspectSummary(outputDir string) (any, error) {
	batchSummary, batchErr := xpipe.InspectBatchOutputDir(outputDir)
	if batchErr != nil {
		return nil, batchErr
	}
	if inspectBatchSummaryHasEpisodeSignal(batchSummary) {
		return batchSummary, nil
	}
	summary, err := xpipe.InspectOutputDir(outputDir)
	if err != nil {
		return nil, err
	}
	if summary.Status != xpipe.InspectStatusInvalid || len(summary.MissingArtifacts) != 1 || summary.MissingArtifacts[0] != "xai_manifest.json" {
		return summary, nil
	}
	return summary, nil
}

func runXAIInspect(outputDir string, strict bool, out io.Writer) error {
	summary, err := buildXAIInspectSummary(outputDir)
	if err != nil {
		return err
	}
	if err := json.NewEncoder(out).Encode(summary); err != nil {
		return err
	}
	status := inspectSummaryStatus(summary)
	if strict && status != xpipe.InspectStatusComplete {
		return fmt.Errorf("xai inspect status %s: output dir is not complete", status)
	}
	return nil
}

func inspectSummaryStatus(summary any) xpipe.InspectStatus {
	switch typed := summary.(type) {
	case xpipe.InspectSummary:
		return typed.Status
	case xpipe.BatchInspectSummary:
		return typed.Status
	default:
		return xpipe.InspectStatusInvalid
	}
}

func runXAIValidate(ctx context.Context, outputDir string, validator xpipe.OutputValidator, out io.Writer) error {
	summary, err := buildXAIValidationSummary(ctx, outputDir, validator)
	if err != nil {
		return err
	}
	if err := json.NewEncoder(out).Encode(summary); err != nil {
		return err
	}
	status := validationSummaryStatus(summary)
	if status != xpipe.ValidationStatusValid {
		return fmt.Errorf("xai validate status %s: output dir is not valid", status)
	}
	return nil
}

func buildXAIValidationSummary(ctx context.Context, outputDir string, validator xpipe.OutputValidator) (any, error) {
	batchSummary, batchErr := xpipe.ValidateBatchOutputDir(ctx, outputDir, validator)
	if batchErr != nil {
		return nil, batchErr
	}
	if validationBatchSummaryHasEpisodeSignal(batchSummary) {
		return batchSummary, nil
	}
	summary, err := xpipe.ValidateOutputDir(ctx, outputDir, validator)
	if err != nil {
		return nil, err
	}
	if summary.Inspect.Status != xpipe.InspectStatusInvalid ||
		len(summary.Inspect.MissingArtifacts) != 1 ||
		summary.Inspect.MissingArtifacts[0] != "xai_manifest.json" {
		return summary, nil
	}
	return summary, nil
}

func inspectBatchSummaryHasEpisodeSignal(summary xpipe.BatchInspectSummary) bool {
	if summary.TotalEpisodes > 0 {
		return true
	}
	return batchIssuesHaveEpisodeSignal(summary.Issues)
}

func validationBatchSummaryHasEpisodeSignal(summary xpipe.BatchValidationSummary) bool {
	if summary.TotalEpisodes > 0 {
		return true
	}
	return batchIssuesHaveEpisodeSignal(summary.Issues)
}

func batchIssuesHaveEpisodeSignal(issues []string) bool {
	for _, issue := range issues {
		if strings.HasPrefix(issue, "malformed episode directory ") ||
			strings.HasPrefix(issue, "episode directory ") ||
			strings.HasPrefix(issue, "missing episode_") {
			return true
		}
	}
	return false
}

func validationSummaryStatus(summary any) xpipe.ValidationStatus {
	switch typed := summary.(type) {
	case xpipe.ValidationSummary:
		return typed.Status
	case xpipe.BatchValidationSummary:
		return typed.Status
	default:
		return xpipe.ValidationStatusInvalid
	}
}

func init() {
	xaiInspectCmd.Flags().BoolVar(&xaiInspectStrict, "strict", false, "exit non-zero unless inspected output is complete")

	xaiCmd.AddCommand(xaiInspectCmd)
	xaiCmd.AddCommand(xaiValidateCmd)
	rootCmd.AddCommand(xaiCmd)
}
