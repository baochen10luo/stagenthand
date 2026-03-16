package video

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
)

// ConcatenateShots joins multiple mp4 files into one using ffmpeg concat demuxer.
// shotPaths: ordered list of local mp4 file paths.
// outputPath: destination mp4.
func ConcatenateShots(ctx context.Context, shotPaths []string, outputPath string) error {
	if len(shotPaths) == 0 {
		return fmt.Errorf("no shot paths provided for concatenation")
	}

	// Check ffmpeg availability
	ffmpegPath, err := exec.LookPath("ffmpeg")
	if err != nil {
		return fmt.Errorf("ffmpeg not found in PATH: %w", err)
	}

	// Create temporary concat list file
	tmpFile, err := os.CreateTemp("", "ffmpeg-concat-*.txt")
	if err != nil {
		return fmt.Errorf("create temp concat list: %w", err)
	}
	tmpPath := tmpFile.Name()
	defer os.Remove(tmpPath)

	for _, p := range shotPaths {
		absPath, absErr := filepath.Abs(p)
		if absErr != nil {
			tmpFile.Close()
			return fmt.Errorf("resolve absolute path for %s: %w", p, absErr)
		}
		if _, writeErr := fmt.Fprintf(tmpFile, "file '%s'\n", absPath); writeErr != nil {
			tmpFile.Close()
			return fmt.Errorf("write to concat list: %w", writeErr)
		}
	}
	if err := tmpFile.Close(); err != nil {
		return fmt.Errorf("close concat list: %w", err)
	}

	// Run ffmpeg concat demuxer
	cmd := exec.CommandContext(ctx, ffmpegPath,
		"-f", "concat",
		"-safe", "0",
		"-i", tmpPath,
		"-c", "copy",
		"-y",
		outputPath,
	)

	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("ffmpeg concat failed: %s: %w", string(output), err)
	}

	return nil
}
