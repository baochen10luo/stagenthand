package xaipipeline

import (
	"context"
	"fmt"
	"image"
	"image/jpeg"
	"os"
	"path/filepath"
)

var dryRunMP4PlaceholderBytes = []byte{
	0x00, 0x00, 0x00, 0x18,
	'f', 't', 'y', 'p',
	'i', 's', 'o', 'm',
	0x00, 0x00, 0x00, 0x00,
	'i', 's', 'o', 'm',
}

type DryRunShotGenerator struct{}

func NewDryRunShotGenerator() *DryRunShotGenerator {
	return &DryRunShotGenerator{}
}

func (g *DryRunShotGenerator) GenerateShot(context.Context, Shot) ([]byte, error) {
	return append([]byte(nil), dryRunMP4PlaceholderBytes...), nil
}

func (g *DryRunShotGenerator) GenerateShotResult(ctx context.Context, shot Shot) (ShotGenerationResult, error) {
	data, err := g.GenerateShot(ctx, shot)
	if err != nil {
		return ShotGenerationResult{}, err
	}
	return ShotGenerationResult{
		Data:      data,
		RequestID: fmt.Sprintf("dry-run-shot-%03d", shot.Index),
		Status:    "dry_run",
	}, nil
}

type DryRunShotValidator struct{}

func (v DryRunShotValidator) ValidShot(_ context.Context, path string, _ RenderValidationSpec) bool {
	info, err := os.Stat(path)
	if err != nil || info.IsDir() || info.Size() == 0 {
		return false
	}
	return fileHasMP4FtypMagic(path)
}

type DryRunFinalizer struct{}

func (f DryRunFinalizer) Finalize(_ context.Context, _ string, outputPath string) error {
	if err := os.MkdirAll(filepath.Dir(outputPath), 0755); err != nil {
		return err
	}
	return os.WriteFile(outputPath, dryRunMP4PlaceholderBytes, 0644)
}

type DryRunShotNormalizer struct{}

func (n DryRunShotNormalizer) Normalize(_ context.Context, _ string, outputPath string, _ RenderSpec) error {
	if err := os.MkdirAll(filepath.Dir(outputPath), 0755); err != nil {
		return err
	}
	return os.WriteFile(outputPath, dryRunMP4PlaceholderBytes, 0644)
}

type DryRunOutputValidator struct{}

func (v DryRunOutputValidator) Validate(_ context.Context, path string, spec RenderValidationSpec) (RenderMetadata, error) {
	sizeBytes := int64(1)
	if info, err := os.Stat(path); err == nil && !info.IsDir() && info.Size() > 0 {
		sizeBytes = info.Size()
	}
	return RenderMetadata{
		Path:        path,
		Width:       spec.Width,
		Height:      spec.Height,
		FPS:         float64(spec.FPS),
		DurationSec: spec.ExpectedDurationSec,
		CodecName:   defaultCodecName,
		PixelFormat: defaultPixelFormat,
		SizeBytes:   sizeBytes,
		HasAudio:    false,
	}, nil
}

type DryRunPreviewExtractor struct{}

func (e DryRunPreviewExtractor) Extract(_ context.Context, _ string, outputPath string) error {
	if err := os.MkdirAll(filepath.Dir(outputPath), 0755); err != nil {
		return err
	}
	file, err := os.Create(outputPath)
	if err != nil {
		return err
	}
	defer file.Close()
	return jpeg.Encode(file, image.NewRGBA(image.Rect(0, 0, defaultWidth, defaultHeight)), nil)
}
