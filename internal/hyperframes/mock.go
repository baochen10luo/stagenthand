package hyperframes

import "context"

// MockExecutor implements Executor for use in tests.
type MockExecutor struct {
	RenderFunc func(ctx context.Context, projectDir string, outputPath string) error
}

func (m *MockExecutor) Render(ctx context.Context, projectDir string, outputPath string) error {
	if m.RenderFunc != nil {
		return m.RenderFunc(ctx, projectDir, outputPath)
	}
	return nil
}
