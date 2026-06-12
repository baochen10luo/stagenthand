package cmd

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/baochen10luo/stagenthand/config"
	xvideo "github.com/baochen10luo/stagenthand/internal/video"
)

type stubXAIVideoGenerator struct {
	imageURL string
	prompt   string
	options  xvideo.GenerateVideoOptions
	result   xvideo.GenerateVideoResult
	err      error
}

func (s *stubXAIVideoGenerator) GenerateVideoWithOptionsResult(ctx context.Context, imageURL string, prompt string, options xvideo.GenerateVideoOptions) (xvideo.GenerateVideoResult, error) {
	if err := ctx.Err(); err != nil {
		return xvideo.GenerateVideoResult{}, err
	}
	s.imageURL = imageURL
	s.prompt = prompt
	s.options = options
	if s.err != nil {
		return xvideo.GenerateVideoResult{}, s.err
	}
	if len(s.result.Data) == 0 {
		s.result.Data = []byte("mp4-bytes")
	}
	s.result.Status = "done"
	s.result.RequestID = "req_test"
	return s.result, nil
}

func TestRunXAIVideoI2VWritesClip(t *testing.T) {
	oldFactory := newXAIVideoGenerator
	t.Cleanup(func() { newXAIVideoGenerator = oldFactory })

	generator := &stubXAIVideoGenerator{}
	newXAIVideoGenerator = func(*config.Config, string) (xaiVideoGenerator, error) {
		return generator, nil
	}

	dir := t.TempDir()
	imagePath := filepath.Join(dir, "first-frame.jpg")
	if err := os.WriteFile(imagePath, []byte{0xff, 0xd8, 0xff, 0xdb}, 0644); err != nil {
		t.Fatalf("write image: %v", err)
	}
	output := filepath.Join(dir, "shot.mp4")
	var out strings.Builder

	err := runXAIVideoI2V(context.Background(), nil, xaiVideoI2VOptions{
		Image:      imagePath,
		Prompt:     "slow camera drift",
		Output:     output,
		Model:      "grok-imagine-video-test",
		Duration:   5,
		Aspect:     "9:16",
		Resolution: "720p",
	}, &out)
	if err != nil {
		t.Fatalf("runXAIVideoI2V() error = %v", err)
	}

	got, err := os.ReadFile(output)
	if err != nil {
		t.Fatalf("read output: %v", err)
	}
	if string(got) != "mp4-bytes" {
		t.Fatalf("output = %q", got)
	}
	if !strings.HasPrefix(generator.imageURL, "data:image/jpeg;base64,") {
		t.Fatalf("imageURL = %.32q, want jpeg data URI", generator.imageURL)
	}
	if generator.prompt != "slow camera drift" {
		t.Fatalf("prompt = %q", generator.prompt)
	}
	if generator.options.DurationSec != 5 || generator.options.AspectRatio != "9:16" || generator.options.Resolution != "720p" {
		t.Fatalf("options = %#v", generator.options)
	}
	if !strings.Contains(out.String(), `"mode": "i2v"`) {
		t.Fatalf("summary missing i2v mode: %s", out.String())
	}
}

func TestRunXAIVideoI2VRejectsNonImage(t *testing.T) {
	dir := t.TempDir()
	imagePath := filepath.Join(dir, "not-image.txt")
	if err := os.WriteFile(imagePath, []byte("not an image"), 0644); err != nil {
		t.Fatalf("write file: %v", err)
	}

	err := runXAIVideoI2V(context.Background(), nil, xaiVideoI2VOptions{
		Image:  imagePath,
		Prompt: "move",
		Output: filepath.Join(dir, "shot.mp4"),
	}, &strings.Builder{})
	if err == nil || !strings.Contains(err.Error(), "not an image") {
		t.Fatalf("runXAIVideoI2V() error = %v, want not an image", err)
	}
}
