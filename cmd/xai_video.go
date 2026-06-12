package cmd

import (
	"context"
	"encoding/base64"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/baochen10luo/stagenthand/config"
	xauth "github.com/baochen10luo/stagenthand/internal/auth/xai"
	xvideo "github.com/baochen10luo/stagenthand/internal/video"
	"github.com/spf13/cobra"
)

type xaiVideoGenerator interface {
	GenerateVideoWithOptionsResult(ctx context.Context, imageURL string, prompt string, options xvideo.GenerateVideoOptions) (xvideo.GenerateVideoResult, error)
}

var newXAIVideoGenerator = func(appCfg *config.Config, model string) (xaiVideoGenerator, error) {
	if appCfg == nil {
		appCfg = &config.Config{}
	}
	store := xauth.NewFileTokenStore(appCfg.XAI.TokenPath)
	return xvideo.NewXAIOAuthClient(appCfg.XAI.BaseURL, model, xauth.NewFileTokenSource(store), nil), nil
}

var (
	xaiVideoPrompt     string
	xaiVideoImage      string
	xaiVideoOutput     string
	xaiVideoModel      string
	xaiVideoDuration   float64
	xaiVideoAspect     string
	xaiVideoResolution string
)

var xaiVideoCmd = &cobra.Command{
	Use:   "video",
	Short: "Generate Grok videos through xAI OAuth",
}

var xaiVideoI2VCmd = &cobra.Command{
	Use:   "i2v",
	Short: "Generate an image-to-video clip from a local first frame",
	RunE: func(cmd *cobra.Command, args []string) error {
		return runXAIVideoI2V(cmd.Context(), cfg, xaiVideoI2VOptions{
			Image:      xaiVideoImage,
			Prompt:     xaiVideoPrompt,
			Output:     xaiVideoOutput,
			Model:      xaiVideoModel,
			Duration:   xaiVideoDuration,
			Aspect:     xaiVideoAspect,
			Resolution: xaiVideoResolution,
		}, cmd.OutOrStdout())
	},
}

type xaiVideoI2VOptions struct {
	Image      string
	Prompt     string
	Output     string
	Model      string
	Duration   float64
	Aspect     string
	Resolution string
}

type xaiVideoI2VSummary struct {
	Provider    string  `json:"provider"`
	Mode        string  `json:"mode"`
	Model       string  `json:"model,omitempty"`
	Image       string  `json:"image"`
	Output      string  `json:"output"`
	Bytes       int     `json:"bytes"`
	RequestID   string  `json:"request_id,omitempty"`
	Status      string  `json:"status,omitempty"`
	VideoURL    string  `json:"video_url,omitempty"`
	DurationSec float64 `json:"duration_sec,omitempty"`
	AspectRatio string  `json:"aspect_ratio,omitempty"`
	Resolution  string  `json:"resolution,omitempty"`
}

func runXAIVideoI2V(ctx context.Context, appCfg *config.Config, opts xaiVideoI2VOptions, out io.Writer) error {
	imagePath := strings.TrimSpace(opts.Image)
	if imagePath == "" {
		return fmt.Errorf("xai video i2v image is empty")
	}
	prompt := strings.TrimSpace(opts.Prompt)
	if prompt == "" {
		return fmt.Errorf("xai video i2v prompt is empty")
	}
	output := strings.TrimSpace(opts.Output)
	if output == "" {
		return fmt.Errorf("xai video i2v output is empty")
	}
	imageURL, err := localImageDataURI(imagePath)
	if err != nil {
		return err
	}
	generator, err := newXAIVideoGenerator(appCfg, opts.Model)
	if err != nil {
		return err
	}
	if generator == nil {
		return fmt.Errorf("xai video generator is nil")
	}
	result, err := generator.GenerateVideoWithOptionsResult(ctx, imageURL, prompt, xvideo.GenerateVideoOptions{
		DurationSec: opts.Duration,
		AspectRatio: opts.Aspect,
		Resolution:  opts.Resolution,
	})
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(output), 0755); err != nil {
		return err
	}
	if err := os.WriteFile(output, result.Data, 0644); err != nil {
		return err
	}
	return writeJSON(out, xaiVideoI2VSummary{
		Provider:    "xai-oauth",
		Mode:        "i2v",
		Model:       strings.TrimSpace(opts.Model),
		Image:       imagePath,
		Output:      output,
		Bytes:       len(result.Data),
		RequestID:   result.RequestID,
		Status:      result.Status,
		VideoURL:    result.VideoURL,
		DurationSec: opts.Duration,
		AspectRatio: opts.Aspect,
		Resolution:  opts.Resolution,
	})
}

func localImageDataURI(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("read i2v image %s: %w", path, err)
	}
	mimeType := http.DetectContentType(data)
	if !strings.HasPrefix(mimeType, "image/") {
		return "", fmt.Errorf("i2v image %s is not an image: %s", path, mimeType)
	}
	return "data:" + mimeType + ";base64," + base64.StdEncoding.EncodeToString(data), nil
}

func init() {
	xaiVideoI2VCmd.Flags().StringVar(&xaiVideoImage, "image", "", "local first-frame image path")
	xaiVideoI2VCmd.Flags().StringVar(&xaiVideoPrompt, "prompt", "", "Grok video prompt")
	xaiVideoI2VCmd.Flags().StringVar(&xaiVideoOutput, "output", "", "output video path")
	xaiVideoI2VCmd.Flags().StringVar(&xaiVideoModel, "model", "", "xAI video model override")
	xaiVideoI2VCmd.Flags().Float64Var(&xaiVideoDuration, "duration", 4, "video duration in seconds")
	xaiVideoI2VCmd.Flags().StringVar(&xaiVideoAspect, "aspect-ratio", "9:16", "video aspect ratio")
	xaiVideoI2VCmd.Flags().StringVar(&xaiVideoResolution, "resolution", "720p", "video resolution")

	xaiVideoCmd.AddCommand(xaiVideoI2VCmd)
	xaiCmd.AddCommand(xaiVideoCmd)
}
