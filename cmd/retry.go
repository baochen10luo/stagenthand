package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/spf13/cobra"
)

var (
	retryPanels    string // comma-separated list like "1,3,5"
	retryOutputDir string
)

func init() {
	retryCmd.Flags().StringVar(&retryPanels, "panels", "", "Panels to retry (comma-separated, e.g. 1,3,5)")
	retryCmd.Flags().StringVarP(&retryOutputDir, "output-dir", "o", "", "Project output dir (required)")
	retryCmd.MarkFlagRequired("output-dir")
	rootCmd.AddCommand(retryCmd)
}

var retryCmd = &cobra.Command{
	Use:   "retry",
	Short: "Retry failed I2V shots",
	Long: `Retry specific failed panels from a previous pipeline run.

Examples:
  shand retry --output-dir ~/.shand/projects/bloodline_of_gangland --panels 1,5,9
  shand retry -o ~/.shand/projects/xxx --panels 3`,
	RunE: runRetry,
}

func runRetry(cmd *cobra.Command, args []string) error {
	// Parse panels list
	var panelNums []int
	for _, s := range strings.Split(retryPanels, ",") {
		s = strings.TrimSpace(s)
		if s == "" {
			continue
		}
		n, err := strconv.Atoi(s)
		if err != nil {
			return fmt.Errorf("invalid panel number: %s", s)
		}
		panelNums = append(panelNums, n)
	}

	if len(panelNums) == 0 {
		return fmt.Errorf("no panels specified (use --panels 1,3,5)")
	}

	shotsDir := filepath.Join(retryOutputDir, "shots")
	propsPath := filepath.Join(retryOutputDir, "remotion_props.json")

	// Load remotion_props.json to get panel prompts
	propsData, err := os.ReadFile(propsPath)
	if err != nil {
		return fmt.Errorf("read remotion_props.json: %w", err)
	}

	var props struct {
		Panels []struct {
			PanelNumber int    `json:"panel_number"`
			Description string `json:"description"`
			Dialogue    string `json:"dialogue"`
		} `json:"panels"`
	}
	if err := json.Unmarshal(propsData, &props); err != nil {
		return fmt.Errorf("parse remotion_props.json: %w", err)
	}

	// Build panel map for quick lookup
	panelMap := make(map[int]struct {
		Description string
		Dialogue    string
	})
	for _, p := range props.Panels {
		panelMap[p.PanelNumber] = struct {
			Description string
			Dialogue    string
		}{p.Description, p.Dialogue}
	}

	// Retry each specified panel
	fmt.Printf("Retrying %d panels: %v\n", len(panelNums), panelNums)
	successCount := 0

	for _, panelNum := range panelNums {
		shotPath := filepath.Join(shotsDir, fmt.Sprintf("shot_%03d.mp4", panelNum))
		errorLogPath := filepath.Join(shotsDir, fmt.Sprintf("shot_%03d_error.json", panelNum))

		// Get panel info
		panelInfo, ok := panelMap[panelNum]
		if !ok {
			fmt.Printf("[Warning] Panel %d not found in remotion_props.json, skipping\n", panelNum)
			continue
		}

		// Remove old files
		os.Remove(shotPath)
		os.Remove(errorLogPath)

		// Run I2V (simplified - just regenerate this shot)
		fmt.Printf("[Info] Retrying panel %d: %s\n", panelNum, panelInfo.Description[:min(60, len(panelInfo.Description))])

		// Note: Full retry would call runGrokBrowserStage or opencli directly
		// For now, just delete error log to mark for retry
		fmt.Printf("[Info] Panel %d marked for retry (run pipeline again to regenerate)\n", panelNum)
		_ = panelInfo // suppress unused warning
		_ = shotPath
		_ = errorLogPath
		successCount++
	}

	fmt.Printf("\nRetry marked: %d panels ready for regeneration\n", successCount)
	fmt.Printf("Run: shand pipeline --skip-llm -o %s --image-dir <images> --panels <n> --i2v\n", retryOutputDir)
	return nil
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
