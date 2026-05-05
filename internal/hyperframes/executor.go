package hyperframes

import "context"

// Executor renders an HTML composition to a silent video using the HyperFrames CLI.
type Executor interface {
	Render(ctx context.Context, projectDir string, outputPath string) error
}
