package xaipipeline

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
)

type FFmpegShotNormalizer struct{}

func NewFFmpegShotNormalizer() *FFmpegShotNormalizer {
	return &FFmpegShotNormalizer{}
}

func (n *FFmpegShotNormalizer) Normalize(ctx context.Context, inputPath string, outputPath string, spec RenderSpec) error {
	ctx = contextOrBackground(ctx)
	if err := ctx.Err(); err != nil {
		return err
	}
	if spec.Width <= 0 {
		spec.Width = defaultWidth
	}
	if spec.Height <= 0 {
		spec.Height = defaultHeight
	}
	if spec.FPS <= 0 {
		spec.FPS = defaultFPS
	}
	if err := os.MkdirAll(filepath.Dir(outputPath), 0755); err != nil {
		return fmt.Errorf("create normalized output dir: %w", err)
	}

	filter := fmt.Sprintf(
		"scale=%d:%d:force_original_aspect_ratio=increase,crop=%d:%d,fps=%d,format=yuv420p",
		spec.Width, spec.Height, spec.Width, spec.Height, spec.FPS,
	)
	cmd, err := mediaToolCommand(ctx, "ffmpeg",
		"-i", inputPath,
		"-map", "0:v:0",
		"-vf", filter,
		"-c:v", "libx264",
		"-preset", "medium",
		"-crf", "20",
		"-an",
		"-movflags", "+faststart",
		"-y",
		outputPath,
	)
	if err != nil {
		return err
	}
	cmd.Stdout = os.Stderr
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("ffmpeg normalize failed: %w", err)
	}
	return nil
}
