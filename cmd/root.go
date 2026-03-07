// Package cmd implements the shand CLI using cobra.
package cmd

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/baochen10luo/stagenthand/config"
	"github.com/spf13/cobra"
)

var (
	cfgFile string
	verbose bool
	dryRun  bool
	cfg     *config.Config
)

// rootCmd is the base command for shand.
var rootCmd = &cobra.Command{
	Use:   "shand",
	Short: "StagentHand — CLI-first AI short drama pipeline",
	Long: `StagentHand (shand) is a CLI tool for generating AI-powered short dramas.
Each subcommand reads JSON from stdin and writes JSON to stdout.
Errors are written to stderr as JSON: {"error": "...", "code": "..."}.
Use --dry-run to validate without calling external APIs.`,
	// Disable cobra's built-in error printing; we handle it in Execute().
	SilenceErrors: true,
	// Still show usage on unknown flags/commands, but not on RunE errors.
	SilenceUsage: true,
	PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
		var err error
		cfg, err = config.Load(cfgFile)
		if err != nil {
			return fmt.Errorf("loading config: %w", err)
		}
		return nil
	},
}

// errorPayload is the structured error envelope written to stderr.
type errorPayload struct {
	Error   string `json:"error"`
	Code    string `json:"code"`
	Command string `json:"command,omitempty"`
}

// writeStderrError writes a structured JSON error to stderr and exits non-zero.
// This ensures agents can always parse failure reasons.
func writeStderrError(code, msg, command string) {
	p := errorPayload{Error: msg, Code: code, Command: command}
	data, _ := json.Marshal(p)
	fmt.Fprintln(os.Stderr, string(data))
}

// Execute runs the root command. Called from main.
func Execute() {
	if err := rootCmd.Execute(); err != nil {
		writeStderrError("runtime_error", err.Error(), os.Args[0])
		os.Exit(1)
	}
}

func init() {
	rootCmd.PersistentFlags().StringVar(&cfgFile, "config", "", "config file (default ~/.shand/config.yaml)")
	rootCmd.PersistentFlags().BoolVar(&verbose, "verbose", false, "enable debug logging")
	rootCmd.PersistentFlags().BoolVar(&dryRun, "dry-run", false, "validate without calling external APIs")

	rootCmd.AddCommand(statusCmd)
	rootCmd.AddCommand(checkpointCmd)
}
