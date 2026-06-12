package xaipipeline

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
)

type FFmpegPreviewExtractor struct{}

func NewFFmpegPreviewExtractor() *FFmpegPreviewExtractor {
	return &FFmpegPreviewExtractor{}
}

func (e *FFmpegPreviewExtractor) Extract(ctx context.Context, inputPath string, outputPath string) error {
	ctx = contextOrBackground(ctx)
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(outputPath), 0755); err != nil {
		return fmt.Errorf("create preview output dir: %w", err)
	}
	cmd, err := mediaToolCommand(ctx, "ffmpeg",
		"-i", inputPath,
		"-ss", "0.25",
		"-frames:v", "1",
		"-q:v", "2",
		"-y",
		outputPath,
	)
	if err != nil {
		return err
	}
	cmd.Stdout = os.Stderr
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("ffmpeg preview extraction failed: %w", err)
	}
	return nil
}
