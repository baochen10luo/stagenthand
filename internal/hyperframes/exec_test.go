package hyperframes

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCLIExecutor_DryRunUsesPublishedHyperFramesPackage(t *testing.T) {
	oldStderr := os.Stderr
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	os.Stderr = w
	t.Cleanup(func() {
		os.Stderr = oldStderr
	})

	executor := NewCLIExecutor(true)
	outputPath := filepath.Join(t.TempDir(), "output.mp4")
	if err := executor.Render(context.Background(), "/tmp/project", outputPath); err != nil {
		t.Fatalf("Render: %v", err)
	}
	if err := w.Close(); err != nil {
		t.Fatalf("close pipe writer: %v", err)
	}
	out, err := io.ReadAll(r)
	if err != nil {
		t.Fatalf("read pipe: %v", err)
	}
	got := string(out)
	if !strings.Contains(got, "npx --yes hyperframes@0.6.55 render") {
		t.Fatalf("dry-run command = %q", got)
	}
	if strings.Contains(got, "render index.html") {
		t.Fatalf("dry-run command should not pass index.html as the project dir: %q", got)
	}
	if strings.Contains(got, "@hyperframes/cli") {
		t.Fatalf("dry-run command should not use unpublished @hyperframes/cli: %q", got)
	}
}

func TestCLIExecutor_DryRunWritesPlaceholderOutput(t *testing.T) {
	executor := NewCLIExecutor(true)
	outputPath := filepath.Join(t.TempDir(), "output.mp4")
	if err := executor.Render(context.Background(), "/tmp/project", outputPath); err != nil {
		t.Fatalf("Render: %v", err)
	}
	if data, err := os.ReadFile(outputPath); err != nil || len(data) == 0 {
		t.Fatalf("dry-run output missing: err=%v data=%q", err, string(data))
	} else if !hyperFramesTestHasMP4Magic(data) {
		t.Fatalf("dry-run output should have MP4 ftyp magic, got %q", string(data))
	}
}

func TestCLIExecutor_DryRunRenderWithFPSPassesFPSFlag(t *testing.T) {
	oldStderr := os.Stderr
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	os.Stderr = w
	t.Cleanup(func() {
		os.Stderr = oldStderr
	})

	executor := NewCLIExecutor(true)
	outputPath := filepath.Join(t.TempDir(), "output.mp4")
	if err := executor.RenderWithFPS(context.Background(), "/tmp/project", outputPath, 24); err != nil {
		t.Fatalf("RenderWithFPS: %v", err)
	}
	if err := w.Close(); err != nil {
		t.Fatalf("close pipe writer: %v", err)
	}
	out, err := io.ReadAll(r)
	if err != nil {
		t.Fatalf("read pipe: %v", err)
	}
	got := string(out)
	if !strings.Contains(got, "--fps 24") {
		t.Fatalf("dry-run command should include --fps 24: %q", got)
	}
}

func hyperFramesTestHasMP4Magic(data []byte) bool {
	return len(data) >= 8 && string(data[4:8]) == "ftyp"
}
