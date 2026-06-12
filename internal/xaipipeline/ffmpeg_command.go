package xaipipeline

import (
	"context"
	"fmt"
	"os/exec"
)

func mediaToolCommand(ctx context.Context, tool string, args ...string) (*exec.Cmd, error) {
	ctx = contextOrBackground(ctx)
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if path, err := exec.LookPath(tool); err == nil {
		return exec.CommandContext(ctx, path, args...), nil
	}

	bunxPath, err := exec.LookPath("bunx")
	if err != nil {
		return nil, fmt.Errorf("%s not found in PATH and bunx remotion %s is unavailable: %w", tool, tool, err)
	}

	bunxArgs := make([]string, 0, len(args)+2)
	bunxArgs = append(bunxArgs, "remotion", tool)
	bunxArgs = append(bunxArgs, args...)
	return exec.CommandContext(ctx, bunxPath, bunxArgs...), nil
}
