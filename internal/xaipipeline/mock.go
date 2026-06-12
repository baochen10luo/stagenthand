package xaipipeline

import "context"

type MockPlanner struct {
	PlanFunc  func(ctx context.Context, input PlanInput) (Manifest, error)
	CallCount int
}

func (m *MockPlanner) Plan(ctx context.Context, input PlanInput) (Manifest, error) {
	m.CallCount++
	if m.PlanFunc != nil {
		return m.PlanFunc(ctx, input)
	}
	return Manifest{}, nil
}

type MockShotGenerator struct {
	GenerateShotFunc       func(ctx context.Context, shot Shot) ([]byte, error)
	GenerateShotResultFunc func(ctx context.Context, shot Shot) (ShotGenerationResult, error)
	CallCount              int
}

func (m *MockShotGenerator) GenerateShot(ctx context.Context, shot Shot) ([]byte, error) {
	m.CallCount++
	if m.GenerateShotFunc != nil {
		return m.GenerateShotFunc(ctx, shot)
	}
	if m.GenerateShotResultFunc != nil {
		result, err := m.GenerateShotResultFunc(ctx, shot)
		return result.Data, err
	}
	return nil, nil
}

func (m *MockShotGenerator) GenerateShotResult(ctx context.Context, shot Shot) (ShotGenerationResult, error) {
	m.CallCount++
	if m.GenerateShotResultFunc != nil {
		return m.GenerateShotResultFunc(ctx, shot)
	}
	return ShotGenerationResult{}, nil
}

type MockRenderer struct {
	RenderFunc func(ctx context.Context, manifest Manifest, outputDir string) (string, error)
	CallCount  int
}

func (m *MockRenderer) Render(ctx context.Context, manifest Manifest, outputDir string) (string, error) {
	m.CallCount++
	if m.RenderFunc != nil {
		return m.RenderFunc(ctx, manifest, outputDir)
	}
	return "", nil
}

type MockShotValidator struct {
	ValidShotFunc func(ctx context.Context, path string, spec RenderValidationSpec) bool
	CallCount     int
}

func (m *MockShotValidator) ValidShot(ctx context.Context, path string, spec RenderValidationSpec) bool {
	m.CallCount++
	if m.ValidShotFunc != nil {
		return m.ValidShotFunc(ctx, path, spec)
	}
	return false
}

type MockTransformer struct {
	GenerateTransformationFunc func(ctx context.Context, systemPrompt string, inputData []byte) ([]byte, error)
	CallCount                  int
}

func (m *MockTransformer) GenerateTransformation(ctx context.Context, systemPrompt string, inputData []byte) ([]byte, error) {
	m.CallCount++
	if m.GenerateTransformationFunc != nil {
		return m.GenerateTransformationFunc(ctx, systemPrompt, inputData)
	}
	return nil, nil
}

type MockVideoClient struct {
	GenerateVideoFunc       func(ctx context.Context, imageURL string, prompt string, options VideoOptions) ([]byte, error)
	GenerateVideoResultFunc func(ctx context.Context, imageURL string, prompt string, options VideoOptions) (VideoGenerationResult, error)
	CallCount               int
}

func (m *MockVideoClient) GenerateVideo(ctx context.Context, imageURL string, prompt string, options VideoOptions) ([]byte, error) {
	m.CallCount++
	if m.GenerateVideoFunc != nil {
		return m.GenerateVideoFunc(ctx, imageURL, prompt, options)
	}
	if m.GenerateVideoResultFunc != nil {
		result, err := m.GenerateVideoResultFunc(ctx, imageURL, prompt, options)
		return result.Data, err
	}
	return nil, nil
}

func (m *MockVideoClient) GenerateVideoResult(ctx context.Context, imageURL string, prompt string, options VideoOptions) (VideoGenerationResult, error) {
	m.CallCount++
	if m.GenerateVideoResultFunc != nil {
		return m.GenerateVideoResultFunc(ctx, imageURL, prompt, options)
	}
	return VideoGenerationResult{}, nil
}

type MockHyperFramesExecutor struct {
	RenderWithFPSFunc func(ctx context.Context, projectDir string, outputPath string, fps int) error
	CallCount         int
}

func (m *MockHyperFramesExecutor) RenderWithFPS(ctx context.Context, projectDir string, outputPath string, fps int) error {
	m.CallCount++
	if m.RenderWithFPSFunc != nil {
		return m.RenderWithFPSFunc(ctx, projectDir, outputPath, fps)
	}
	return nil
}

type MockVideoFinalizer struct {
	FinalizeFunc func(ctx context.Context, inputPath string, outputPath string) error
	CallCount    int
}

func (m *MockVideoFinalizer) Finalize(ctx context.Context, inputPath string, outputPath string) error {
	m.CallCount++
	if m.FinalizeFunc != nil {
		return m.FinalizeFunc(ctx, inputPath, outputPath)
	}
	return nil
}

type MockShotNormalizer struct {
	NormalizeFunc func(ctx context.Context, inputPath string, outputPath string, spec RenderSpec) error
	CallCount     int
}

func (m *MockShotNormalizer) Normalize(ctx context.Context, inputPath string, outputPath string, spec RenderSpec) error {
	m.CallCount++
	if m.NormalizeFunc != nil {
		return m.NormalizeFunc(ctx, inputPath, outputPath, spec)
	}
	return nil
}

type MockOutputValidator struct {
	ValidateFunc func(ctx context.Context, path string, spec RenderValidationSpec) (RenderMetadata, error)
	CallCount    int
}

func (m *MockOutputValidator) Validate(ctx context.Context, path string, spec RenderValidationSpec) (RenderMetadata, error) {
	m.CallCount++
	if m.ValidateFunc != nil {
		return m.ValidateFunc(ctx, path, spec)
	}
	return RenderMetadata{}, nil
}

type MockPreviewExtractor struct {
	ExtractFunc func(ctx context.Context, inputPath string, outputPath string) error
	CallCount   int
}

func (m *MockPreviewExtractor) Extract(ctx context.Context, inputPath string, outputPath string) error {
	m.CallCount++
	if m.ExtractFunc != nil {
		return m.ExtractFunc(ctx, inputPath, outputPath)
	}
	return nil
}
