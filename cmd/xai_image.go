package cmd

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/baochen10luo/stagenthand/config"
	xauth "github.com/baochen10luo/stagenthand/internal/auth/xai"
	ximage "github.com/baochen10luo/stagenthand/internal/image"
	"github.com/baochen10luo/stagenthand/internal/llm"
	"github.com/spf13/cobra"
)

type xaiImageCreator interface {
	Create(ctx context.Context, prompt string, options ximage.XAIImageOptions) (ximage.XAIImageResult, error)
}

type xaiImageAuditor interface {
	Audit(ctx context.Context, story string, scenePaths []string) ([]byte, error)
}

var newXAIImageCreator = func(appCfg *config.Config, model, aspectRatio, resolution string) (xaiImageCreator, error) {
	if appCfg == nil {
		appCfg = &config.Config{}
	}
	store := xauth.NewFileTokenStore(appCfg.XAI.TokenPath)
	return ximage.NewXAIOAuthImageClient(appCfg.XAI.BaseURL, model, aspectRatio, resolution, xauth.NewFileTokenSource(store), nil), nil
}

var newXAIImageAuditor = func(appCfg *config.Config, model string) (xaiImageAuditor, error) {
	if appCfg == nil {
		appCfg = &config.Config{}
	}
	store := xauth.NewFileTokenStore(appCfg.XAI.TokenPath)
	return &xaiVisionAuditor{
		client: llm.NewXAIOAuthClient(appCfg.XAI.BaseURL, model, xauth.NewFileTokenSource(store), nil),
	}, nil
}

var (
	xaiImagePrompt     string
	xaiImageOutput     string
	xaiImageModel      string
	xaiImageAspect     string
	xaiImageResolution string
	xaiImageRefs       []string
	xaiImageAuditStory string
	xaiImageAuditScene []string
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

var xaiImageAuditStoryCmd = &cobra.Command{
	Use:   "audit-story",
	Short: "Audit generated story stills through xAI OAuth vision",
	RunE: func(cmd *cobra.Command, args []string) error {
		return runXAIImageAuditStory(cmd.Context(), cfg, xaiImageAuditOptions{
			Story:  xaiImageAuditStory,
			Output: xaiImageOutput,
			Model:  xaiImageModel,
			Scenes: xaiImageAuditScene,
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

type xaiImageAuditOptions struct {
	Story  string
	Output string
	Model  string
	Scenes []string
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

type xaiImageAuditSummary struct {
	Provider string   `json:"provider"`
	Model    string   `json:"model,omitempty"`
	Output   string   `json:"output,omitempty"`
	Scenes   []string `json:"scenes"`
	Bytes    int      `json:"bytes"`
}

type xaiVisionAuditor struct {
	client interface {
		GenerateVisionTransformation(ctx context.Context, systemPrompt string, text string, images []llm.XAIImageInput) ([]byte, error)
	}
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

func runXAIImageAuditStory(ctx context.Context, appCfg *config.Config, opts xaiImageAuditOptions, out io.Writer) error {
	story := strings.TrimSpace(opts.Story)
	if story == "" {
		return fmt.Errorf("xai image audit story is empty")
	}
	scenes := trimCLIStrings(opts.Scenes)
	if len(scenes) == 0 {
		return fmt.Errorf("xai image audit requires at least one --scene")
	}
	auditor, err := newXAIImageAuditor(appCfg, opts.Model)
	if err != nil {
		return err
	}
	if auditor == nil {
		return fmt.Errorf("xai image auditor is nil")
	}
	result, err := auditor.Audit(ctx, story, scenes)
	if err != nil {
		return err
	}
	output := strings.TrimSpace(opts.Output)
	if output == "" {
		_, err = fmt.Fprintln(out, string(result))
		return err
	}
	if err := os.MkdirAll(filepath.Dir(output), 0755); err != nil {
		return err
	}
	if err := os.WriteFile(output, result, 0644); err != nil {
		return err
	}
	return writeJSON(out, xaiImageAuditSummary{
		Provider: "xai-oauth",
		Model:    opts.Model,
		Output:   output,
		Scenes:   scenes,
		Bytes:    len(result),
	})
}

func (a *xaiVisionAuditor) Audit(ctx context.Context, story string, scenePaths []string) ([]byte, error) {
	images := make([]llm.XAIImageInput, 0, len(scenePaths))
	for _, path := range scenePaths {
		data, err := os.ReadFile(path)
		if err != nil {
			return nil, fmt.Errorf("read audit scene %s: %w", path, err)
		}
		images = append(images, llm.XAIImageInput{
			Data:     data,
			MimeType: http.DetectContentType(data),
			Name:     filepath.Base(path),
		})
	}
	return a.client.GenerateVisionTransformation(ctx, xaiImageStoryAuditPrompt, buildXAIImageStoryAuditInput(story, scenePaths), images)
}

func buildXAIImageStoryAuditInput(story string, scenePaths []string) string {
	var b strings.Builder
	b.WriteString("Story:\n")
	b.WriteString(strings.TrimSpace(story))
	b.WriteString("\n\nScenes are attached in this order:\n")
	for i, path := range scenePaths {
		fmt.Fprintf(&b, "%d. %s\n", i+1, filepath.Base(path))
	}
	b.WriteString(`
Expected visual continuity:
1. Moose walking alone in the forest.
2. Moose visibly lost and nervous.
3. Moose alone using a phone to call for help.
4. Giraffe in a separate location receiving or listening to the call.
5. Giraffe phone-call punchline angle, not physically standing with the moose in the same forest space.

Audit for story fit, character consistency, phone-call spatial relationship, unwanted text/speech bubbles, and whether any image explains the joke instead of just playing the timing.
`)
	return b.String()
}

const xaiImageStoryAuditPrompt = `You are a strict visual continuity auditor for a short static story video.
Return JSON only with this shape:
{
  "status": "pass" | "revise",
  "issues": [
    {"scene": 1, "severity": "minor" | "major", "problem": "...", "fix": "..."}
  ],
  "scene_notes": [
    {"scene": 1, "fits_story": true, "notes": "..."}
  ]
}
Mark "revise" for any major story relationship error, especially if a phone-call scene shows characters physically together when they should be cross-cut between locations.`

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
	xaiImageAuditStoryCmd.Flags().StringVar(&xaiImageAuditStory, "story", "", "story text or visual brief to audit against")
	xaiImageAuditStoryCmd.Flags().StringArrayVar(&xaiImageAuditScene, "scene", nil, "scene image path to audit; repeat in story order")
	xaiImageAuditStoryCmd.Flags().StringVar(&xaiImageOutput, "output", "", "output audit JSON path")
	xaiImageAuditStoryCmd.Flags().StringVar(&xaiImageModel, "model", "", "xAI vision model override")

	xaiImageCmd.AddCommand(xaiImageGenerateCmd)
	xaiImageCmd.AddCommand(xaiImageEditCmd)
	xaiImageCmd.AddCommand(xaiImageAuditStoryCmd)
	xaiCmd.AddCommand(xaiImageCmd)
}
