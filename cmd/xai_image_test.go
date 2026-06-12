package cmd

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/baochen10luo/stagenthand/config"
	ximage "github.com/baochen10luo/stagenthand/internal/image"
)

type stubXAIImageCreator struct {
	result ximage.XAIImageResult
	err    error
	calls  []ximage.XAIImageOptions
}

func (s *stubXAIImageCreator) Create(ctx context.Context, prompt string, opts ximage.XAIImageOptions) (ximage.XAIImageResult, error) {
	if err := ctx.Err(); err != nil {
		return ximage.XAIImageResult{}, err
	}
	s.calls = append(s.calls, opts)
	if s.err != nil {
		return ximage.XAIImageResult{}, s.err
	}
	if len(s.result.Data) == 0 {
		s.result.Data = []byte("image-bytes")
	}
	if s.result.Model == "" {
		s.result.Model = "grok-imagine-test"
	}
	if s.result.AspectRatio == "" {
		s.result.AspectRatio = "9:16"
	}
	if s.result.Resolution == "" {
		s.result.Resolution = "1k"
	}
	s.result.ReferenceCount = len(opts.References)
	return s.result, nil
}

func TestRunXAIImageCreateWritesGeneratedImage(t *testing.T) {
	oldFactory := newXAIImageCreator
	t.Cleanup(func() { newXAIImageCreator = oldFactory })

	creator := &stubXAIImageCreator{}
	newXAIImageCreator = func(*config.Config, string, string, string) (xaiImageCreator, error) {
		return creator, nil
	}

	output := filepath.Join(t.TempDir(), "character.png")
	var out strings.Builder
	err := runXAIImageCreate(context.Background(), nil, xaiImageCreateOptions{
		Prompt:     "moose character sheet",
		Output:     output,
		Model:      "grok-imagine-test",
		Aspect:     "9:16",
		Resolution: "1k",
	}, &out)
	if err != nil {
		t.Fatalf("runXAIImageCreate() error = %v", err)
	}

	got, err := os.ReadFile(output)
	if err != nil {
		t.Fatalf("read output: %v", err)
	}
	if string(got) != "image-bytes" {
		t.Fatalf("output = %q", got)
	}
	if !strings.Contains(out.String(), `"mode": "generate"`) {
		t.Fatalf("summary missing generate mode: %s", out.String())
	}
	if len(creator.calls) != 1 || len(creator.calls[0].References) != 0 {
		t.Fatalf("calls = %#v", creator.calls)
	}
}

func TestRunXAIImageCreateWritesEditedImageWithReferences(t *testing.T) {
	oldFactory := newXAIImageCreator
	t.Cleanup(func() { newXAIImageCreator = oldFactory })

	creator := &stubXAIImageCreator{}
	newXAIImageCreator = func(*config.Config, string, string, string) (xaiImageCreator, error) {
		return creator, nil
	}

	output := filepath.Join(t.TempDir(), "scene.png")
	var out strings.Builder
	err := runXAIImageCreate(context.Background(), nil, xaiImageCreateOptions{
		Prompt:     "scene still",
		Output:     output,
		References: []string{"moose.png", "giraffe.png"},
	}, &out)
	if err != nil {
		t.Fatalf("runXAIImageCreate() error = %v", err)
	}

	if !strings.Contains(out.String(), `"mode": "edit"`) {
		t.Fatalf("summary missing edit mode: %s", out.String())
	}
	if !strings.Contains(out.String(), `"reference_count": 2`) {
		t.Fatalf("summary missing reference count: %s", out.String())
	}
	if len(creator.calls) != 1 || len(creator.calls[0].References) != 2 {
		t.Fatalf("calls = %#v", creator.calls)
	}
}
