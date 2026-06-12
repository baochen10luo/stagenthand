package hyperframes

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
)

const hyperFramesNPMPackage = "hyperframes@0.6.55"

var dryRunMP4PlaceholderBytes = []byte{
	0x00, 0x00, 0x00, 0x18,
	'f', 't', 'y', 'p',
	'i', 's', 'o', 'm',
	0x00, 0x00, 0x00, 0x00,
	'i', 's', 'o', 'm',
}

// CLIExecutor implements Executor by shelling out to `npx hyperframes render`.
type CLIExecutor struct {
	dryRun bool
}

func NewCLIExecutor(dryRun bool) *CLIExecutor {
	return &CLIExecutor{dryRun: dryRun}
}

// Render runs `npx --yes hyperframes@0.6.55 render --output <outputPath>` inside projectDir.
func (c *CLIExecutor) Render(ctx context.Context, projectDir string, outputPath string) error {
	return c.RenderWithFPS(ctx, projectDir, outputPath, 0)
}

// RenderWithFPS runs HyperFrames with an explicit frame rate when fps > 0.
func (c *CLIExecutor) RenderWithFPS(ctx context.Context, projectDir string, outputPath string, fps int) error {
	args := []string{"--yes", hyperFramesNPMPackage, "render", "--output", outputPath}
	if fps > 0 {
		args = append(args, "--fps", strconv.Itoa(fps))
	}
	if c.dryRun {
		fmt.Fprintf(os.Stderr, "[DRY-RUN] Would run: npx %s in %s\n",
			strings.Join(args, " "), projectDir)
		if err := os.MkdirAll(filepath.Dir(outputPath), 0755); err != nil {
			return err
		}
		return os.WriteFile(outputPath, dryRunMP4PlaceholderBytes, 0644)
	}
	cmd := exec.CommandContext(ctx, "npx", args...)
	cmd.Dir = projectDir
	cmd.Stdout = os.Stderr
	cmd.Stderr = os.Stderr
	cmd.Env = os.Environ()
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("hyperframes render failed: %w", err)
	}
	return nil
}
