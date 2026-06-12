package xaipipeline

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"strconv"
	"strings"
)

type OutputValidator interface {
	Validate(ctx context.Context, path string, spec RenderValidationSpec) (RenderMetadata, error)
}

type RenderValidationSpec struct {
	Width               int
	Height              int
	FPS                 int
	CodecName           string
	PixelFormat         string
	ExpectedDurationSec float64
	RequireNoAudio      bool
}

type RenderMetadata struct {
	Path         string  `json:"path"`
	ProjectID    string  `json:"project_id,omitempty"`
	ManifestHash string  `json:"manifest_hash,omitempty"`
	Width        int     `json:"width"`
	Height       int     `json:"height"`
	FPS          float64 `json:"fps"`
	DurationSec  float64 `json:"duration_sec"`
	CodecName    string  `json:"codec_name"`
	PixelFormat  string  `json:"pixel_format"`
	SizeBytes    int64   `json:"size_bytes"`
	HasAudio     bool    `json:"has_audio"`
}

type FFprobeOutputValidator struct{}

func NewFFprobeOutputValidator() *FFprobeOutputValidator {
	return &FFprobeOutputValidator{}
}

func (v *FFprobeOutputValidator) Validate(ctx context.Context, path string, spec RenderValidationSpec) (RenderMetadata, error) {
	ctx = contextOrBackground(ctx)
	if err := ctx.Err(); err != nil {
		return RenderMetadata{}, err
	}
	info, err := os.Stat(path)
	if err != nil {
		return RenderMetadata{}, fmt.Errorf("stat final output: %w", err)
	}
	if info.Size() == 0 {
		return RenderMetadata{}, fmt.Errorf("final output is empty: %s", path)
	}

	cmd, err := mediaToolCommand(ctx, "ffprobe",
		"-v", "error",
		"-show_entries", "stream=codec_type,codec_name,width,height,r_frame_rate,pix_fmt",
		"-show_entries", "format=duration",
		"-of", "json",
		path,
	)
	if err != nil {
		return RenderMetadata{}, err
	}
	out, err := cmd.CombinedOutput()
	if err != nil {
		return RenderMetadata{}, fmt.Errorf("ffprobe final output failed: %w: %s", err, strings.TrimSpace(string(out)))
	}

	var probed ffprobeRenderMetadata
	if err := json.Unmarshal(out, &probed); err != nil {
		return RenderMetadata{}, fmt.Errorf("parse ffprobe render metadata: %w", err)
	}
	video, ok := firstVideoStream(probed.Streams)
	if !ok {
		return RenderMetadata{}, fmt.Errorf("final output has no video stream: %s", path)
	}

	duration, err := strconv.ParseFloat(probed.Format.Duration, 64)
	if err != nil || duration <= 0 {
		return RenderMetadata{}, fmt.Errorf("invalid final output duration %q", probed.Format.Duration)
	}
	fps, err := parseRenderFrameRate(video.FrameRate)
	if err != nil {
		return RenderMetadata{}, fmt.Errorf("invalid final output frame rate %q: %w", video.FrameRate, err)
	}

	metadata := RenderMetadata{
		Path:        path,
		Width:       video.Width,
		Height:      video.Height,
		FPS:         fps,
		DurationSec: duration,
		CodecName:   video.CodecName,
		PixelFormat: video.PixelFormat,
		SizeBytes:   info.Size(),
		HasAudio:    hasAudioStream(probed.Streams),
	}
	if err := validateRenderMetadata(metadata, spec); err != nil {
		return RenderMetadata{}, err
	}
	return metadata, nil
}

type ffprobeRenderMetadata struct {
	Streams []ffprobeRenderStream `json:"streams"`
	Format  struct {
		Duration string `json:"duration"`
	} `json:"format"`
}

type ffprobeRenderStream struct {
	CodecName   string `json:"codec_name"`
	CodecType   string `json:"codec_type"`
	Width       int    `json:"width"`
	Height      int    `json:"height"`
	FrameRate   string `json:"r_frame_rate"`
	PixelFormat string `json:"pix_fmt"`
}

func firstVideoStream(streams []ffprobeRenderStream) (ffprobeRenderStream, bool) {
	for _, stream := range streams {
		if stream.CodecType == "video" {
			return stream, true
		}
	}
	return ffprobeRenderStream{}, false
}

func hasAudioStream(streams []ffprobeRenderStream) bool {
	for _, stream := range streams {
		if stream.CodecType == "audio" {
			return true
		}
	}
	return false
}

func parseRenderFrameRate(rate string) (float64, error) {
	if rate == "" {
		return 0, fmt.Errorf("empty frame rate")
	}
	parts := strings.Split(rate, "/")
	if len(parts) == 1 {
		return strconv.ParseFloat(rate, 64)
	}
	if len(parts) != 2 {
		return 0, fmt.Errorf("unexpected frame rate format")
	}
	numerator, err := strconv.ParseFloat(parts[0], 64)
	if err != nil {
		return 0, err
	}
	denominator, err := strconv.ParseFloat(parts[1], 64)
	if err != nil {
		return 0, err
	}
	if denominator == 0 {
		return 0, fmt.Errorf("zero frame rate denominator")
	}
	return numerator / denominator, nil
}

func validateRenderMetadata(metadata RenderMetadata, spec RenderValidationSpec) error {
	if spec.Width > 0 && metadata.Width != spec.Width {
		return fmt.Errorf("final output dimensions %dx%d, want %dx%d", metadata.Width, metadata.Height, spec.Width, spec.Height)
	}
	if spec.Height > 0 && metadata.Height != spec.Height {
		return fmt.Errorf("final output dimensions %dx%d, want %dx%d", metadata.Width, metadata.Height, spec.Width, spec.Height)
	}
	if spec.FPS > 0 && math.Abs(metadata.FPS-float64(spec.FPS)) > 0.1 {
		return fmt.Errorf("final output fps %.3f, want %d", metadata.FPS, spec.FPS)
	}
	if spec.CodecName != "" && metadata.CodecName != spec.CodecName {
		return fmt.Errorf("final output codec %q, want %q", metadata.CodecName, spec.CodecName)
	}
	if spec.PixelFormat != "" && metadata.PixelFormat != spec.PixelFormat {
		return fmt.Errorf("final output pixel format %q, want %q", metadata.PixelFormat, spec.PixelFormat)
	}
	if spec.ExpectedDurationSec > 0 {
		tolerance := math.Max(0.35, spec.ExpectedDurationSec*0.05)
		if math.Abs(metadata.DurationSec-spec.ExpectedDurationSec) > tolerance {
			return fmt.Errorf("final output duration %.3fs, want %.3fs +/- %.3fs", metadata.DurationSec, spec.ExpectedDurationSec, tolerance)
		}
	}
	if spec.RequireNoAudio && metadata.HasAudio {
		return fmt.Errorf("final output has audio stream, want silent output")
	}
	return nil
}
