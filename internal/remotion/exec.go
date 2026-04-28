package remotion

import (
	"context"
	"fmt"
	"os"
	"os/exec"
)

// CLIExecutor implements Executor by actually spawning `npx remotion` commands.
type CLIExecutor struct {
	dryRun    bool
	publicDir string // passed as --public-dir; empty = use remotion default (./public)
}

func NewCLIExecutor(dryRun bool) *CLIExecutor {
	return &CLIExecutor{dryRun: dryRun}
}

// NewCLIExecutorWithPublicDir creates a CLIExecutor that passes --public-dir to remotion,
// allowing static assets outside the template's own public/ directory to be served.
func NewCLIExecutorWithPublicDir(dryRun bool, publicDir string) *CLIExecutor {
	return &CLIExecutor{dryRun: dryRun, publicDir: publicDir}
}

// Render triggers `npx remotion render src/index.ts <composition> <output> --props=<propsPath>` inside `templatePath`.
func (c *CLIExecutor) Render(ctx context.Context, templatePath string, composition string, propsPath string, outputPath string) error {
	if c.dryRun {
		fmt.Fprintf(os.Stderr, "[DRY-RUN] Would run: npx remotion render src/index.ts %s %s --props=%s in %s\n", composition, outputPath, propsPath, templatePath)
		return nil
	}

	args := []string{"--no-install", "remotion", "render", "src/index.ts", composition, outputPath, "--props", propsPath}
	if c.publicDir != "" {
		args = append(args, "--public-dir", c.publicDir)
	}
	cmd := exec.CommandContext(ctx, "npx", args...)
	cmd.Dir = templatePath
	cmd.Stdout = os.Stderr // pipe remotion stdout to shand stderr
	cmd.Stderr = os.Stderr
	cmd.Env = os.Environ()

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("failed to run remotion render: %w", err)
	}

	return nil
}

// Preview triggers `npx remotion studio src/index.ts --props=<propsPath>` inside `templatePath`.
func (c *CLIExecutor) Preview(ctx context.Context, templatePath string, composition string, propsPath string) error {
	if c.dryRun {
		fmt.Fprintf(os.Stderr, "[DRY-RUN] Would run: npx remotion studio src/index.ts --props=%s in %s\n", propsPath, templatePath)
		return nil
	}

	args := []string{"--no-install", "remotion", "studio", "src/index.ts", "--props", propsPath}
	if c.publicDir != "" {
		args = append(args, "--public-dir", c.publicDir)
	}
	cmd := exec.CommandContext(ctx, "npx", args...)
	cmd.Dir = templatePath
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Env = os.Environ()

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("failed to run remotion studio: %w", err)
	}

	return nil
}
