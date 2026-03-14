package remotion

import (
	"context"
	"fmt"
	"os"
	"os/exec"
)

// CLIExecutor implements Executor by actually spawning `npx remotion` commands.
type CLIExecutor struct {
	dryRun bool
}

func NewCLIExecutor(dryRun bool) *CLIExecutor {
	return &CLIExecutor{dryRun: dryRun}
}

// Render triggers `npx remotion render src/index.ts <composition> <output> --props=<propsPath>` inside `templatePath`.
func (c *CLIExecutor) Render(ctx context.Context, templatePath string, composition string, propsPath string, outputPath string) error {
	if c.dryRun {
		fmt.Fprintf(os.Stderr, "[DRY-RUN] Would run: npx remotion render src/index.ts %s %s --props=%s in %s\n", composition, outputPath, propsPath, templatePath)
		return nil
	}

	cmd := exec.CommandContext(ctx, "npx", "--no-install", "remotion", "render", "src/index.ts", composition, outputPath, "--props", propsPath)
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

	// For studio we don't pass composition to enforce one, studio handles interactive selection. But we pass props.
	cmd := exec.CommandContext(ctx, "npx", "--no-install", "remotion", "studio", "src/index.ts", "--props", propsPath)
	cmd.Dir = templatePath
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Env = os.Environ()

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("failed to run remotion studio: %w", err)
	}

	return nil
}
