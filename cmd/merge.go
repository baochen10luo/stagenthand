package cmd

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"
)

var (
	mergeOutputPath string
	mergeOutputDir  string
)

func init() {
	mergeCmd.Flags().StringVarP(&mergeOutputPath, "output", "o", "", "Output file path (default: output_grok.mp4 in project dir)")
	mergeCmd.Flags().StringVar(&mergeOutputDir, "output-dir", "", "Project output dir (alternative to --output)")
	rootCmd.AddCommand(mergeCmd)
}

var mergeCmd = &cobra.Command{
	Use:   "merge",
	Short: "Merge I2V shots into final video",
	Long: `Merge all shot_*.mp4 files in a project into a single video.

Examples:
  shand merge --output-dir ~/.shand/projects/bloodline_of_gangland
  shand merge -o ~/.shand/projects/xxx/output.mp4`,
	RunE: runMerge,
}

func runMerge(cmd *cobra.Command, args []string) error {
	if mergeOutputDir == "" && mergeOutputPath == "" {
		return fmt.Errorf("either --output-dir or --output is required")
	}

	var shotsDir string
	var outputFile string

	if mergeOutputPath != "" {
		outputFile = mergeOutputPath
		shotsDir = filepath.Dir(outputFile) + "/shots"
	} else {
		shotsDir = filepath.Join(mergeOutputDir, "shots")
		outputFile = filepath.Join(mergeOutputDir, "output_grok.mp4")
	}

	// Find all shot files
	entries, err := os.ReadDir(shotsDir)
	if err != nil {
		return fmt.Errorf("read shots dir: %w", err)
	}

	var shotFiles []string
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		if strings.HasPrefix(e.Name(), "shot_") && strings.HasSuffix(e.Name(), ".mp4") {
			shotFiles = append(shotFiles, filepath.Join(shotsDir, e.Name()))
		}
	}

	if len(shotFiles) == 0 {
		return fmt.Errorf("no shot files found in %s", shotsDir)
	}

	// Sort by filename (shot_001.mp4, shot_002.mp4, ...)
	// Already in order from ReadDir but ensure correct order
	var sortedShotFiles []string
	for i := 1; i <= len(shotFiles)+10; i++ {
		shotPath := filepath.Join(shotsDir, fmt.Sprintf("shot_%03d.mp4", i))
		if _, err := os.Stat(shotPath); err == nil {
			sortedShotFiles = append(sortedShotFiles, shotPath)
		}
	}

	if len(sortedShotFiles) == 0 {
		return fmt.Errorf("no numbered shot files found")
	}

	// Create concat list
	concatFile := filepath.Join(shotsDir, "concat_list.txt")
	concatContent := strings.Join(sortedShotFiles, "\n") + "\n"
	if err := os.WriteFile(concatFile, []byte(concatContent), 0644); err != nil {
		return fmt.Errorf("write concat list: %w", err)
	}

	// Run ffmpeg
	fmt.Printf("Merging %d shots into %s\n", len(sortedShotFiles), outputFile)
	ffmpegCmd := exec.Command("ffmpeg", "-y",
		"-f", "concat",
		"-safe", "0",
		"-i", concatFile,
		"-c", "copy",
		outputFile)
	ffmpegCmd.Stdout = os.Stdout
	ffmpegCmd.Stderr = os.Stderr

	if err := ffmpegCmd.Run(); err != nil {
		return fmt.Errorf("ffmpeg merge: %w", err)
	}

	fmt.Printf("[Done] Output: %s\n", outputFile)
	return nil
}
