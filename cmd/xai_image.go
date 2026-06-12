package cmd

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/baochen10luo/stagenthand/config"
	xauth "github.com/baochen10luo/stagenthand/internal/auth/xai"
	ximage "github.com/baochen10luo/stagenthand/internal/image"
	"github.com/spf13/cobra"
)

type xaiImageCreator interface {
	Create(ctx context.Context, prompt string, options ximage.XAIImageOptions) (ximage.XAIImageResult, error)
}

var newXAIImageCreator = func(appCfg *config.Config, model, aspectRatio, resolution string) (xaiImageCreator, error) {
	if appCfg == nil {
		appCfg = &config.Config{}
	}
	store := xauth.NewFileTokenStore(appCfg.XAI.TokenPath)
	return ximage.NewXAIOAuthImageClient(appCfg.XAI.BaseURL, model, aspectRatio, resolution, xauth.NewFileTokenSource(store), nil), nil
}

var (
	xaiImagePrompt     string
	xaiImageOutput     string
	xaiImageModel      string
	xaiImageAspect     string
	xaiImageResolution string
	xaiImageRefs       []string
)

var xaiImageCmd = &cobra.Command{
	Use:   "image",
	Short: "Generate Grok images through xAI OAuth",
}

var xaiImageGenerateCmd = &cobra.Command{
	Use:   "generate",
	Short: "Generate a Grok image from text",
	RunE: func(cmd *cobra.Command, args []string) error {
		return runXAIImageCreate(cmd.Context(), cfg, xaiImageCreateOptions{
			Prompt:     xaiImagePrompt,
			Output:     xaiImageOutput,
			Model:      xaiImageModel,
			Aspect:     xaiImageAspect,
			Resolution: xaiImageResolution,
		}, cmd.OutOrStdout())
	},
}

var xaiImageEditCmd = &cobra.Command{
	Use:   "edit",
	Short: "Generate a Grok image using reference images",
	RunE: func(cmd *cobra.Command, args []string) error {
		return runXAIImageCreate(cmd.Context(), cfg, xaiImageCreateOptions{
			Prompt:     xaiImagePrompt,
			Output:     xaiImageOutput,
			Model:      xaiImageModel,
			Aspect:     xaiImageAspect,
			Resolution: xaiImageResolution,
			References: xaiImageRefs,
		}, cmd.OutOrStdout())
	},
}

type xaiImageCreateOptions struct {
	Prompt     string
	Output     string
	Model      string
	Aspect     string
	Resolution string
	References []string
}

type xaiImageCreateSummary struct {
	Provider       string   `json:"provider"`
	Mode           string   `json:"mode"`
	Model          string   `json:"model"`
	AspectRatio    string   `json:"aspect_ratio"`
	Resolution     string   `json:"resolution"`
	ReferenceCount int      `json:"reference_count"`
	References     []string `json:"references,omitempty"`
	Output         string   `json:"output"`
	Bytes          int      `json:"bytes"`
	URL            string   `json:"url,omitempty"`
	MimeType       string   `json:"mime_type,omitempty"`
	RevisedPrompt  string   `json:"revised_prompt,omitempty"`
}

func runXAIImageCreate(ctx context.Context, appCfg *config.Config, opts xaiImageCreateOptions, out io.Writer) error {
	prompt := strings.TrimSpace(opts.Prompt)
	if prompt == "" {
		return fmt.Errorf("xai image prompt is empty")
	}
	output := strings.TrimSpace(opts.Output)
	if output == "" {
		return fmt.Errorf("xai image output is empty")
	}
	creator, err := newXAIImageCreator(appCfg, opts.Model, opts.Aspect, opts.Resolution)
	if err != nil {
		return err
	}
	if creator == nil {
		return fmt.Errorf("xai image creator is nil")
	}
	result, err := creator.Create(ctx, prompt, ximage.XAIImageOptions{
		Model:       opts.Model,
		AspectRatio: opts.Aspect,
		Resolution:  opts.Resolution,
		References:  opts.References,
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
	mode := "generate"
	if len(trimCLIStrings(opts.References)) > 0 {
		mode = "edit"
	}
	return writeJSON(out, xaiImageCreateSummary{
		Provider:       "xai-oauth",
		Mode:           mode,
		Model:          result.Model,
		AspectRatio:    result.AspectRatio,
		Resolution:     result.Resolution,
		ReferenceCount: result.ReferenceCount,
		References:     trimCLIStrings(opts.References),
		Output:         output,
		Bytes:          len(result.Data),
		URL:            result.URL,
		MimeType:       result.MimeType,
		RevisedPrompt:  result.RevisedPrompt,
	})
}

func trimCLIStrings(values []string) []string {
	out := make([]string, 0, len(values))
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			out = append(out, strings.TrimSpace(value))
		}
	}
	return out
}

func init() {
	for _, command := range []*cobra.Command{xaiImageGenerateCmd, xaiImageEditCmd} {
		command.Flags().StringVar(&xaiImagePrompt, "prompt", "", "Grok image prompt")
		command.Flags().StringVar(&xaiImageOutput, "output", "", "output image path")
		command.Flags().StringVar(&xaiImageModel, "model", "", "xAI image model override")
		command.Flags().StringVar(&xaiImageAspect, "aspect-ratio", "9:16", "image aspect ratio")
		command.Flags().StringVar(&xaiImageResolution, "resolution", "1k", "image resolution")
	}
	xaiImageEditCmd.Flags().StringArrayVar(&xaiImageRefs, "reference", nil, "reference image path, URL, or data URI; repeat up to 3 times")

	xaiImageCmd.AddCommand(xaiImageGenerateCmd)
	xaiImageCmd.AddCommand(xaiImageEditCmd)
	xaiCmd.AddCommand(xaiImageCmd)
}
