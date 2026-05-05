package hyperframes

import (
	"context"
	"fmt"
	"os"
	"os/exec"
)

// CLIExecutor implements Executor by shelling out to `npx @hyperframes/cli render`.
type CLIExecutor struct {
	dryRun bool
}

func NewCLIExecutor(dryRun bool) *CLIExecutor {
	return &CLIExecutor{dryRun: dryRun}
}

// Render runs `npx @hyperframes/cli render index.html --output <outputPath>` inside projectDir.
func (c *CLIExecutor) Render(ctx context.Context, projectDir string, outputPath string) error {
	if c.dryRun {
		fmt.Fprintf(os.Stderr, "[DRY-RUN] Would run: npx @hyperframes/cli render index.html --output %s in %s\n",
			outputPath, projectDir)
		return nil
	}
	cmd := exec.CommandContext(ctx,
		"npx", "@hyperframes/cli", "render", "index.html",
		"--output", outputPath,
	)
	cmd.Dir = projectDir
	cmd.Stdout = os.Stderr
	cmd.Stderr = os.Stderr
	cmd.Env = os.Environ()
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("hyperframes render failed: %w", err)
	}
	return nil
}
