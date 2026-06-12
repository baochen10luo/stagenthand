package xaipipeline_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"image"
	"image/jpeg"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/baochen10luo/stagenthand/internal/xaipipeline"
)

type stubHyperFramesExecutor struct {
	projectDir string
	outputPath string
	fps        int
	calls      int
}

func (s *stubHyperFramesExecutor) RenderWithFPS(_ context.Context, projectDir string, outputPath string, fps int) error {
	s.calls++
	s.projectDir = projectDir
	s.outputPath = outputPath
	s.fps = fps
	return os.WriteFile(outputPath, []byte("timeline"), 0644)
}

type stubVideoFinalizer struct {
	inputPath  string
	outputPath string
	calls      int
}

func (s *stubVideoFinalizer) Finalize(_ context.Context, inputPath string, outputPath string) error {
	s.calls++
	s.inputPath = inputPath
	s.outputPath = outputPath
	return os.WriteFile(outputPath, []byte("final"), 0644)
}

type stubShotNormalizer struct {
	inputPath  string
	outputPath string
	calls      int
}

func (s *stubShotNormalizer) Normalize(_ context.Context, inputPath string, outputPath string, _ xaipipeline.RenderSpec) error {
	s.calls++
	s.inputPath = inputPath
	s.outputPath = outputPath
	return os.WriteFile(outputPath, []byte("normalized"), 0644)
}

type stubOutputValidator struct {
	path       string
	paths      []string
	spec       xaipipeline.RenderValidationSpec
	specByPath map[string]xaipipeline.RenderValidationSpec
	metadata   xaipipeline.RenderMetadata
	err        error
	errByPath  map[string]error
	errForPath func(path string) error
	calls      int
}

func (s *stubOutputValidator) Validate(_ context.Context, path string, spec xaipipeline.RenderValidationSpec) (xaipipeline.RenderMetadata, error) {
	s.calls++
	s.path = path
	s.paths = append(s.paths, path)
	s.spec = spec
	if s.specByPath == nil {
		s.specByPath = make(map[string]xaipipeline.RenderValidationSpec)
	}
	s.specByPath[path] = spec
	if err := s.errByPath[path]; err != nil {
		return xaipipeline.RenderMetadata{}, err
	}
	if s.errForPath != nil {
		if err := s.errForPath(path); err != nil {
			return xaipipeline.RenderMetadata{}, err
		}
	}
	if s.err != nil {
		return xaipipeline.RenderMetadata{}, s.err
	}
	metadata := s.metadata
	metadata.Path = path
	if metadata.Width == 0 {
		metadata.Width = spec.Width
	}
	if metadata.Height == 0 {
		metadata.Height = spec.Height
	}
	if metadata.FPS == 0 {
		metadata.FPS = float64(spec.FPS)
	}
	if metadata.DurationSec == 0 {
		metadata.DurationSec = spec.ExpectedDurationSec
	}
	if metadata.CodecName == "" {
		metadata.CodecName = spec.CodecName
	}
	if metadata.PixelFormat == "" {
		metadata.PixelFormat = spec.PixelFormat
	}
	if metadata.SizeBytes == 0 {
		if info, err := os.Stat(path); err == nil {
			metadata.SizeBytes = info.Size()
		}
	}
	return metadata, nil
}

type stubPreviewExtractor struct {
	inputPath      string
	outputPath     string
	err            error
	invalidPreview bool
	calls          int
}

func (s *stubPreviewExtractor) Extract(_ context.Context, inputPath string, outputPath string) error {
	s.calls++
	s.inputPath = inputPath
	s.outputPath = outputPath
	if s.err != nil {
		return s.err
	}
	if s.invalidPreview {
		return os.WriteFile(outputPath, []byte("not a jpeg"), 0644)
	}
	file, err := os.Create(outputPath)
	if err != nil {
		return err
	}
	defer file.Close()
	return jpeg.Encode(file, image.NewRGBA(image.Rect(0, 0, 720, 1280)), nil)
}

func newTestHyperFramesRenderer(executor *stubHyperFramesExecutor, finalizer *stubVideoFinalizer) *xaipipeline.HyperFramesFFmpegRenderer {
	return xaipipeline.NewHyperFramesFFmpegRenderer(executor, finalizer).
		WithShotNormalizer(&stubShotNormalizer{}).
		WithOutputValidator(&stubOutputValidator{}).
		WithPreviewExtractor(&stubPreviewExtractor{})
}

func TestHyperFramesFFmpegRenderer_RendersTimelineThenFinalizes(t *testing.T) {
	outputDir := t.TempDir()
	executor := &stubHyperFramesExecutor{}
	finalizer := &stubVideoFinalizer{}
	renderer := newTestHyperFramesRenderer(executor, finalizer)

	got, err := renderer.Render(context.Background(), xaipipeline.Manifest{
		ProjectID: "xai-test",
		Format:    "portrait",
		FPS:       24,
		Width:     720,
		Height:    1280,
		Shots: []xaipipeline.Shot{
			{
				Index:         1,
				Prompt:        "shot 1",
				VideoPath:     "shots/shot_001.mp4",
				DurationSec:   8,
				Subtitle:      "第一個鏡頭",
				TransitionOut: "cut",
			},
			{
				Index:         2,
				Prompt:        "shot 2",
				VideoPath:     "shots/shot_002.mp4",
				DurationSec:   7.5,
				Subtitle:      "第二個鏡頭",
				TransitionOut: "fade",
			},
		},
	}, outputDir)
	if err != nil {
		t.Fatalf("Render: %v", err)
	}

	wantOutput := filepath.Join(outputDir, "output_xai.mp4")
	if got != wantOutput {
		t.Fatalf("output path = %q, want %q", got, wantOutput)
	}
	if executor.calls != 1 {
		t.Fatalf("executor calls = %d, want 1", executor.calls)
	}
	if finalizer.calls != 1 {
		t.Fatalf("finalizer calls = %d, want 1", finalizer.calls)
	}
	if executor.projectDir != filepath.Join(outputDir, "hyperframes") {
		t.Fatalf("projectDir = %q", executor.projectDir)
	}
	wantTimeline := filepath.Join(outputDir, "timeline_hyperframes.mp4")
	if executor.outputPath == wantTimeline {
		t.Fatalf("executor should render staged timeline before commit, got canonical path %q", executor.outputPath)
	}
	if filepath.Dir(executor.outputPath) != outputDir {
		t.Fatalf("executor staged timeline dir = %q, want %q", filepath.Dir(executor.outputPath), outputDir)
	}
	stagedTimelineName := filepath.Base(executor.outputPath)
	if !strings.HasPrefix(stagedTimelineName, ".timeline_hyperframes.mp4_") || !strings.HasSuffix(stagedTimelineName, ".tmp.mp4") {
		t.Fatalf("executor staged timeline name = %q, want timeline temp artifact", stagedTimelineName)
	}
	if executor.fps != 24 {
		t.Fatalf("executor fps = %d, want 24", executor.fps)
	}
	if finalizer.inputPath != wantTimeline {
		t.Fatalf("finalizer input = %q, want committed HyperFrames timeline %q", finalizer.inputPath, wantTimeline)
	}

	htmlBytes, err := os.ReadFile(filepath.Join(outputDir, "hyperframes", "index.html"))
	if err != nil {
		t.Fatalf("read index.html: %v", err)
	}
	html := string(htmlBytes)
	for _, want := range []string{
		`data-composition-id="xai-video"`,
		`data-start="0"`,
		`data-width="720"`,
		`data-height="1280"`,
		`data-duration="15.500"`,
		`class="shot-video clip"`,
		`display: none`,
		`opacity: 0`,
		`data-track-index="0"`,
		`class="subtitle clip"`,
		`data-track-index="1"`,
		`<video`,
		`data-start="0.000"`,
		`data-duration="8.000"`,
		`data-transition-out="cut"`,
		`../normalized/shot_001.mp4`,
		`data-transition-out="fade"`,
		`../normalized/shot_002.mp4`,
		`第一個鏡頭`,
		`第二個鏡頭`,
		`transitionOut: "cut"`,
		`transitionOut: "fade"`,
		`previousTransitionOut`,
		`fadeSeconds`,
		`applyShotVisibility`,
		`shot.video.style.opacity`,
		`shot.subtitle.style.opacity`,
		`window.__timelines["xai-video"]`,
		`window.__hf`,
	} {
		if !strings.Contains(html, want) {
			t.Fatalf("index.html missing %q:\n%s", want, html)
		}
	}
	if _, err := os.Stat(filepath.Join(outputDir, "hyperframes", "package.json")); err != nil {
		t.Fatalf("package.json not written: %v", err)
	}
}

func TestHyperFramesFFmpegRenderer_NilContextUsesBackground(t *testing.T) {
	outputDir := t.TempDir()
	executor := &stubHyperFramesExecutor{}
	finalizer := &stubVideoFinalizer{}
	renderer := newTestHyperFramesRenderer(executor, finalizer)

	got, err := renderer.Render(nil, xaipipeline.Manifest{
		ProjectID: "nil-context-render",
		FPS:       24,
		Width:     720,
		Height:    1280,
		Shots: []xaipipeline.Shot{
			{Index: 1, Prompt: "shot 1", VideoPath: "shots/shot_001.mp4", DurationSec: 8},
		},
	}, outputDir)
	if err != nil {
		t.Fatalf("Render(nil): %v", err)
	}
	if got != filepath.Join(outputDir, "output_xai.mp4") {
		t.Fatalf("output path = %q", got)
	}
	if executor.calls != 1 || finalizer.calls != 1 {
		t.Fatalf("executor/finalizer calls = %d/%d, want 1/1", executor.calls, finalizer.calls)
	}
}

func TestHyperFramesFFmpegRenderer_RejectsCanceledContextBeforeArtifacts(t *testing.T) {
	outputDir := t.TempDir()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	normalizer := &stubShotNormalizer{}
	executor := &stubHyperFramesExecutor{}
	finalizer := &stubVideoFinalizer{}
	renderer := xaipipeline.NewHyperFramesFFmpegRenderer(executor, finalizer).
		WithShotNormalizer(normalizer).
		WithOutputValidator(&stubOutputValidator{}).
		WithPreviewExtractor(&stubPreviewExtractor{})

	got, err := renderer.Render(ctx, xaipipeline.Manifest{
		ProjectID: "canceled-render",
		FPS:       24,
		Width:     720,
		Height:    1280,
		Shots: []xaipipeline.Shot{
			{Index: 1, Prompt: "shot 1", VideoPath: "shots/shot_001.mp4", DurationSec: 8},
		},
	}, outputDir)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Render() error = %v, want context.Canceled", err)
	}
	if got != "" {
		t.Fatalf("Render() output = %q, want empty path on canceled context", got)
	}
	if normalizer.calls != 0 {
		t.Fatalf("normalizer calls = %d, want 0", normalizer.calls)
	}
	if executor.calls != 0 {
		t.Fatalf("executor calls = %d, want 0", executor.calls)
	}
	if finalizer.calls != 0 {
		t.Fatalf("finalizer calls = %d, want 0", finalizer.calls)
	}
	for _, path := range []string{
		filepath.Join(outputDir, "normalized"),
		filepath.Join(outputDir, "hyperframes"),
		filepath.Join(outputDir, "timeline_hyperframes.mp4"),
		filepath.Join(outputDir, "output_xai.mp4"),
		filepath.Join(outputDir, "render_metadata.json"),
		filepath.Join(outputDir, "preview_frame.jpg"),
	} {
		if _, statErr := os.Stat(path); !os.IsNotExist(statErr) {
			t.Fatalf("%s should not exist after canceled render, stat err=%v", path, statErr)
		}
	}
}

func TestFFmpegFinalizer_UsesSilentH264YUV420PContract(t *testing.T) {
	binDir := t.TempDir()
	argsLog := filepath.Join(t.TempDir(), "ffmpeg.args")
	ffmpegPath := filepath.Join(binDir, "ffmpeg")
	script := "#!/bin/sh\n" +
		"printf '%s\\n' \"$@\" > \"$FFMPEG_ARGS_LOG\"\n" +
		"last=''\n" +
		"for arg do last=\"$arg\"; done\n" +
		"printf final > \"$last\"\n"
	if err := os.WriteFile(ffmpegPath, []byte(script), 0755); err != nil {
		t.Fatalf("write stub ffmpeg: %v", err)
	}
	t.Setenv("PATH", binDir)
	t.Setenv("FFMPEG_ARGS_LOG", argsLog)

	workDir := t.TempDir()
	inputPath := filepath.Join(workDir, "timeline_hyperframes.mp4")
	outputPath := filepath.Join(workDir, "output_xai.mp4")
	if err := os.WriteFile(inputPath, []byte("timeline"), 0644); err != nil {
		t.Fatalf("write input timeline: %v", err)
	}

	if err := (xaipipeline.FFmpegFinalizer{}).Finalize(context.Background(), inputPath, outputPath); err != nil {
		t.Fatalf("Finalize() error = %v", err)
	}

	data, err := os.ReadFile(argsLog)
	if err != nil {
		t.Fatalf("read ffmpeg args log: %v", err)
	}
	gotArgs := strings.Split(strings.TrimSpace(string(data)), "\n")
	wantArgs := []string{
		"-i", inputPath,
		"-c:v", "libx264",
		"-pix_fmt", "yuv420p",
		"-movflags", "+faststart",
		"-an",
		"-y",
		outputPath,
	}
	if !sameStrings(gotArgs, wantArgs) {
		t.Fatalf("ffmpeg args = %#v, want %#v", gotArgs, wantArgs)
	}
	if _, err := os.Stat(outputPath); err != nil {
		t.Fatalf("final output missing: %v", err)
	}
}

func TestFFmpegFinalizer_NilContextUsesBackground(t *testing.T) {
	binDir := t.TempDir()
	ffmpegPath := filepath.Join(binDir, "ffmpeg")
	script := "#!/bin/sh\n" +
		"last=''\n" +
		"for arg do last=\"$arg\"; done\n" +
		"printf final > \"$last\"\n"
	if err := os.WriteFile(ffmpegPath, []byte(script), 0755); err != nil {
		t.Fatalf("write stub ffmpeg: %v", err)
	}
	t.Setenv("PATH", binDir)

	workDir := t.TempDir()
	inputPath := filepath.Join(workDir, "timeline_hyperframes.mp4")
	outputPath := filepath.Join(workDir, "output_xai.mp4")
	if err := os.WriteFile(inputPath, []byte("timeline"), 0644); err != nil {
		t.Fatalf("write input timeline: %v", err)
	}

	if err := (xaipipeline.FFmpegFinalizer{}).Finalize(nil, inputPath, outputPath); err != nil {
		t.Fatalf("Finalize(nil): %v", err)
	}
	if _, err := os.Stat(outputPath); err != nil {
		t.Fatalf("final output missing: %v", err)
	}
}

func TestHyperFramesFFmpegRenderer_AlwaysUsesHyperFramesTimelineWithoutSubtitles(t *testing.T) {
	outputDir := t.TempDir()
	executor := &stubHyperFramesExecutor{}
	finalizer := &stubVideoFinalizer{}
	renderer := newTestHyperFramesRenderer(executor, finalizer)

	_, err := renderer.Render(context.Background(), xaipipeline.Manifest{
		ProjectID: "no-subtitles",
		FPS:       24,
		Width:     720,
		Height:    1280,
		Shots: []xaipipeline.Shot{
			{Index: 1, Prompt: "shot 1", VideoPath: "shots/shot_001.mp4", DurationSec: 8},
		},
	}, outputDir)
	if err != nil {
		t.Fatalf("Render: %v", err)
	}

	if executor.calls != 1 {
		t.Fatalf("executor calls = %d, want 1", executor.calls)
	}
	wantTimeline := filepath.Join(outputDir, "timeline_hyperframes.mp4")
	if executor.outputPath == wantTimeline {
		t.Fatalf("executor should render staged timeline before commit, got canonical path %q", executor.outputPath)
	}
	if finalizer.inputPath != wantTimeline {
		t.Fatalf("finalizer input = %q, want committed HyperFrames timeline %q", finalizer.inputPath, wantTimeline)
	}

	htmlBytes, err := os.ReadFile(filepath.Join(outputDir, "hyperframes", "index.html"))
	if err != nil {
		t.Fatalf("read index.html: %v", err)
	}
	html := string(htmlBytes)
	if !strings.Contains(html, `class="shot-video clip"`) {
		t.Fatalf("index.html should contain xAI video clips:\n%s", html)
	}
	if strings.Contains(html, `class="subtitle clip"`) {
		t.Fatalf("index.html should not emit subtitle clips for empty subtitles:\n%s", html)
	}
}

func TestHyperFramesFFmpegRenderer_PreservesExistingHyperFramesIndexWhenPackageCommitFails(t *testing.T) {
	outputDir := t.TempDir()
	projectDir := filepath.Join(outputDir, "hyperframes")
	if err := os.MkdirAll(projectDir, 0755); err != nil {
		t.Fatalf("mkdir hyperframes dir: %v", err)
	}
	indexPath := filepath.Join(projectDir, "index.html")
	const oldIndex = "old hyperframes index"
	if err := os.WriteFile(indexPath, []byte(oldIndex), 0644); err != nil {
		t.Fatalf("write existing index.html: %v", err)
	}
	packagePath := filepath.Join(projectDir, "package.json")
	if err := os.Mkdir(packagePath, 0755); err != nil {
		t.Fatalf("mkdir package.json blocker: %v", err)
	}
	executor := &stubHyperFramesExecutor{}
	finalizer := &stubVideoFinalizer{}
	renderer := newTestHyperFramesRenderer(executor, finalizer)

	_, err := renderer.Render(context.Background(), xaipipeline.Manifest{
		ProjectID: "project-commit-rollback",
		FPS:       24,
		Width:     720,
		Height:    1280,
		Shots: []xaipipeline.Shot{
			{Index: 1, Prompt: "shot 1", VideoPath: "shots/shot_001.mp4", DurationSec: 8},
		},
	}, outputDir)
	if err == nil {
		t.Fatal("Render() error = nil, want package commit error")
	}
	if !strings.Contains(err.Error(), "write xai hyperframes package.json") {
		t.Fatalf("Render() error = %v, want package write error", err)
	}
	indexData, err := os.ReadFile(indexPath)
	if err != nil {
		t.Fatalf("read index.html after failed render: %v", err)
	}
	if string(indexData) != oldIndex {
		t.Fatalf("existing hyperframes index should be preserved after package commit failure: %q", string(indexData))
	}
	if executor.calls != 0 {
		t.Fatalf("executor calls = %d, want 0 before project files commit", executor.calls)
	}
	if finalizer.calls != 0 {
		t.Fatalf("finalizer calls = %d, want 0 before project files commit", finalizer.calls)
	}
}

func TestHyperFramesFFmpegRenderer_ExtractsPreviewFrame(t *testing.T) {
	outputDir := t.TempDir()
	extractor := &stubPreviewExtractor{}
	renderer := newTestHyperFramesRenderer(&stubHyperFramesExecutor{}, &stubVideoFinalizer{}).
		WithPreviewExtractor(extractor)

	got, err := renderer.Render(context.Background(), xaipipeline.Manifest{
		ProjectID: "preview-test",
		FPS:       24,
		Width:     720,
		Height:    1280,
		Shots: []xaipipeline.Shot{
			{Index: 1, Prompt: "shot 1", VideoPath: "shots/shot_001.mp4", DurationSec: 8},
		},
	}, outputDir)
	if err != nil {
		t.Fatalf("Render: %v", err)
	}

	if extractor.calls != 1 {
		t.Fatalf("extractor calls = %d, want 1", extractor.calls)
	}
	if extractor.inputPath == got {
		t.Fatalf("extractor should read staged final output before commit, got canonical path %q", extractor.inputPath)
	}
	if filepath.Dir(extractor.inputPath) != outputDir {
		t.Fatalf("extractor staged input dir = %q, want %q", filepath.Dir(extractor.inputPath), outputDir)
	}
	stagedInputName := filepath.Base(extractor.inputPath)
	if !strings.HasPrefix(stagedInputName, ".output_xai.mp4_") || !strings.HasSuffix(stagedInputName, ".tmp.mp4") {
		t.Fatalf("extractor staged input name = %q, want final output temp artifact", stagedInputName)
	}
	wantPreview := filepath.Join(outputDir, "preview_frame.jpg")
	if extractor.outputPath == wantPreview {
		t.Fatalf("extractor output should be staged before commit, got canonical path %q", extractor.outputPath)
	}
	if filepath.Dir(extractor.outputPath) != outputDir {
		t.Fatalf("extractor staged output dir = %q, want %q", filepath.Dir(extractor.outputPath), outputDir)
	}
	stagedName := filepath.Base(extractor.outputPath)
	if !strings.HasPrefix(stagedName, ".preview_frame.jpg_") || !strings.HasSuffix(stagedName, ".tmp.jpg") {
		t.Fatalf("extractor staged output name = %q, want preview temp artifact", stagedName)
	}
	if _, err := os.Stat(wantPreview); err != nil {
		t.Fatalf("preview frame not written: %v", err)
	}
}

func TestHyperFramesFFmpegRenderer_ReturnsPreviewError(t *testing.T) {
	renderer := newTestHyperFramesRenderer(&stubHyperFramesExecutor{}, &stubVideoFinalizer{}).
		WithPreviewExtractor(&stubPreviewExtractor{err: errors.New("no frame")})

	_, err := renderer.Render(context.Background(), xaipipeline.Manifest{
		ProjectID: "preview-test",
		FPS:       24,
		Width:     720,
		Height:    1280,
		Shots: []xaipipeline.Shot{
			{Index: 1, Prompt: "shot 1", VideoPath: "shots/shot_001.mp4", DurationSec: 8},
		},
	}, t.TempDir())
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "extract preview frame") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestHyperFramesFFmpegRenderer_RejectsInvalidPreviewFrameBeforeSuccess(t *testing.T) {
	outputDir := t.TempDir()
	extractor := &stubPreviewExtractor{invalidPreview: true}
	renderer := newTestHyperFramesRenderer(&stubHyperFramesExecutor{}, &stubVideoFinalizer{}).
		WithPreviewExtractor(extractor)

	_, err := renderer.Render(context.Background(), xaipipeline.Manifest{
		ProjectID: "preview-validation-test",
		FPS:       24,
		Width:     720,
		Height:    1280,
		Shots: []xaipipeline.Shot{
			{Index: 1, Prompt: "shot 1", VideoPath: "shots/shot_001.mp4", DurationSec: 8},
		},
	}, outputDir)
	if err == nil {
		t.Fatal("Render() error = nil, want preview validation error")
	}
	if !strings.Contains(err.Error(), "validate preview frame") {
		t.Fatalf("Render() error = %v, want preview validation error", err)
	}
	if extractor.calls != 1 {
		t.Fatalf("extractor calls = %d, want 1", extractor.calls)
	}
}

func TestHyperFramesFFmpegRenderer_PreservesExistingPreviewWhenNewPreviewFailsValidation(t *testing.T) {
	outputDir := t.TempDir()
	previewPath := filepath.Join(outputDir, "preview_frame.jpg")
	if err := os.MkdirAll(outputDir, 0755); err != nil {
		t.Fatalf("mkdir output dir: %v", err)
	}
	file, err := os.Create(previewPath)
	if err != nil {
		t.Fatalf("create existing preview: %v", err)
	}
	if err := jpeg.Encode(file, image.NewRGBA(image.Rect(0, 0, 720, 1280)), nil); err != nil {
		_ = file.Close()
		t.Fatalf("write existing preview: %v", err)
	}
	if err := file.Close(); err != nil {
		t.Fatalf("close existing preview: %v", err)
	}
	before, err := os.ReadFile(previewPath)
	if err != nil {
		t.Fatalf("read existing preview: %v", err)
	}

	renderer := newTestHyperFramesRenderer(&stubHyperFramesExecutor{}, &stubVideoFinalizer{}).
		WithPreviewExtractor(&stubPreviewExtractor{invalidPreview: true})

	_, err = renderer.Render(context.Background(), xaipipeline.Manifest{
		ProjectID: "preview-preserve-test",
		FPS:       24,
		Width:     720,
		Height:    1280,
		Shots: []xaipipeline.Shot{
			{Index: 1, Prompt: "shot 1", VideoPath: "shots/shot_001.mp4", DurationSec: 8},
		},
	}, outputDir)
	if err == nil {
		t.Fatal("Render() error = nil, want preview validation error")
	}
	if !strings.Contains(err.Error(), "validate preview frame") {
		t.Fatalf("Render() error = %v, want preview validation error", err)
	}
	after, err := os.ReadFile(previewPath)
	if err != nil {
		t.Fatalf("read preview after failed render: %v", err)
	}
	if string(after) != string(before) {
		t.Fatalf("existing preview should be preserved after failed preview validation")
	}
}

func TestHyperFramesFFmpegRenderer_ValidatesFinalOutputAndWritesMetadata(t *testing.T) {
	outputDir := t.TempDir()
	executor := &stubHyperFramesExecutor{}
	finalizer := &stubVideoFinalizer{}
	validator := &stubOutputValidator{
		metadata: xaipipeline.RenderMetadata{
			Path:        filepath.Join(outputDir, "output_xai.mp4"),
			Width:       720,
			Height:      1280,
			FPS:         24,
			DurationSec: 8,
			CodecName:   "h264",
		},
	}
	renderer := newTestHyperFramesRenderer(executor, finalizer).
		WithOutputValidator(validator)

	manifest := xaipipeline.Manifest{
		ProjectID: "metadata-test",
		FPS:       24,
		Width:     720,
		Height:    1280,
		Shots: []xaipipeline.Shot{
			{Index: 1, Prompt: "shot 1", VideoPath: "shots/shot_001.mp4", DurationSec: 8},
		},
	}
	sourceManifestHash := testManifestIdentityHash(t, manifest)
	got, err := renderer.Render(context.Background(), manifest, outputDir)
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	if manifest.Shots[0].VideoPath != "shots/shot_001.mp4" {
		t.Fatalf("renderer mutated source manifest video_path = %q, want raw shot path", manifest.Shots[0].VideoPath)
	}

	if validator.calls != 4 {
		t.Fatalf("validator calls = %d, want staged normalized shot, committed normalized shot, timeline, and final output", validator.calls)
	}
	if validator.path == got {
		t.Fatalf("validator should inspect staged final output before commit, got canonical path %q", validator.path)
	}
	if filepath.Dir(validator.path) != outputDir {
		t.Fatalf("validator staged final output dir = %q, want %q", filepath.Dir(validator.path), outputDir)
	}
	stagedFinalName := filepath.Base(validator.path)
	if !strings.HasPrefix(stagedFinalName, ".output_xai.mp4_") || !strings.HasSuffix(stagedFinalName, ".tmp.mp4") {
		t.Fatalf("validator staged final output name = %q, want output temp artifact", stagedFinalName)
	}
	if validator.spec.Width != 720 || validator.spec.Height != 1280 || validator.spec.FPS != 24 {
		t.Fatalf("validator spec = %+v, want 720x1280@24", validator.spec)
	}
	if validator.spec.ExpectedDurationSec != 8 {
		t.Fatalf("expected duration = %v, want 8", validator.spec.ExpectedDurationSec)
	}
	if !validator.spec.RequireNoAudio {
		t.Fatal("validator spec should require no audio in xAI-native output")
	}

	metadataBytes, err := os.ReadFile(filepath.Join(outputDir, "render_metadata.json"))
	if err != nil {
		t.Fatalf("read render_metadata.json: %v", err)
	}
	var renderMetadata xaipipeline.RenderMetadata
	if err := json.Unmarshal(metadataBytes, &renderMetadata); err != nil {
		t.Fatalf("parse render_metadata.json: %v", err)
	}
	if renderMetadata.ProjectID != "metadata-test" {
		t.Fatalf("render metadata project_id = %q, want metadata-test", renderMetadata.ProjectID)
	}
	if !isLowercaseHex64(renderMetadata.ManifestHash) {
		t.Fatalf("render metadata manifest_hash = %q, want 64 lowercase hex characters", renderMetadata.ManifestHash)
	}
	if renderMetadata.ManifestHash != sourceManifestHash {
		t.Fatalf("render metadata manifest_hash = %q, want hash of source manifest", renderMetadata.ManifestHash)
	}
	metadata := string(metadataBytes)
	for _, want := range []string{
		`"path":`,
		`"project_id": "metadata-test"`,
		`"manifest_hash":`,
		`"width": 720`,
		`"height": 1280`,
		`"fps": 24`,
		`"duration_sec": 8`,
		`"codec_name": "h264"`,
		`"size_bytes": 5`,
	} {
		if !strings.Contains(metadata, want) {
			t.Fatalf("metadata missing %q:\n%s", want, metadata)
		}
	}
}

func TestHyperFramesFFmpegRenderer_WritesRenderMetadataAsOutputLocalFile(t *testing.T) {
	outputDir := t.TempDir()
	externalMetadata := filepath.Join(t.TempDir(), "render_metadata.json")
	const externalSentinel = "external metadata should not change"
	if err := os.WriteFile(externalMetadata, []byte(externalSentinel), 0644); err != nil {
		t.Fatalf("write external metadata: %v", err)
	}
	renderMetadataPath := filepath.Join(outputDir, "render_metadata.json")
	if err := os.Symlink(externalMetadata, renderMetadataPath); err != nil {
		t.Skipf("symlink not available: %v", err)
	}

	renderer := newTestHyperFramesRenderer(&stubHyperFramesExecutor{}, &stubVideoFinalizer{})

	_, err := renderer.Render(context.Background(), xaipipeline.Manifest{
		ProjectID: "metadata-local-file",
		FPS:       24,
		Width:     720,
		Height:    1280,
		Shots: []xaipipeline.Shot{
			{Index: 1, Prompt: "shot 1", VideoPath: "shots/shot_001.mp4", DurationSec: 8},
		},
	}, outputDir)
	if err != nil {
		t.Fatalf("Render: %v", err)
	}

	info, err := os.Lstat(renderMetadataPath)
	if err != nil {
		t.Fatalf("stat render_metadata.json: %v", err)
	}
	if info.Mode()&os.ModeSymlink != 0 {
		t.Fatalf("render_metadata.json should be an output-local regular file, got symlink")
	}
	externalData, err := os.ReadFile(externalMetadata)
	if err != nil {
		t.Fatalf("read external metadata: %v", err)
	}
	if string(externalData) != externalSentinel {
		t.Fatalf("external metadata changed: %q", string(externalData))
	}
	localData, err := os.ReadFile(renderMetadataPath)
	if err != nil {
		t.Fatalf("read local render metadata: %v", err)
	}
	if !strings.Contains(string(localData), "output_xai.mp4") {
		t.Fatalf("local render metadata does not describe output_xai.mp4:\n%s", string(localData))
	}
}

func TestHyperFramesFFmpegRenderer_RejectsInvalidNormalizedShotBeforeTimeline(t *testing.T) {
	outputDir := t.TempDir()
	executor := &stubHyperFramesExecutor{}
	finalizer := &stubVideoFinalizer{}
	normalizer := &stubShotNormalizer{}
	validator := &stubOutputValidator{
		errForPath: func(path string) error {
			if strings.Contains(filepath.Base(path), "shot_001.mp4") {
				return errors.New("normalized shot has audio")
			}
			return nil
		},
	}
	renderer := xaipipeline.NewHyperFramesFFmpegRenderer(executor, finalizer).
		WithShotNormalizer(normalizer).
		WithOutputValidator(validator).
		WithPreviewExtractor(&stubPreviewExtractor{})

	_, err := renderer.Render(context.Background(), xaipipeline.Manifest{
		ProjectID: "normalized-validation-test",
		FPS:       24,
		Width:     720,
		Height:    1280,
		Shots: []xaipipeline.Shot{
			{Index: 1, Prompt: "shot 1", VideoPath: "shots/shot_001.mp4", DurationSec: 8},
		},
	}, outputDir)
	if err == nil {
		t.Fatal("Render() error = nil, want normalized validation error")
	}
	if !strings.Contains(err.Error(), "validate normalized xai shot 1") {
		t.Fatalf("Render() error = %v, want normalized validation error", err)
	}
	if normalizer.calls != 1 {
		t.Fatalf("normalizer calls = %d, want 1", normalizer.calls)
	}
	if executor.calls != 0 {
		t.Fatalf("executor calls = %d, want 0 before valid normalized shots", executor.calls)
	}
	if finalizer.calls != 0 {
		t.Fatalf("finalizer calls = %d, want 0", finalizer.calls)
	}
	if validator.calls != 1 {
		t.Fatalf("validator calls = %d, want normalized shot only", validator.calls)
	}
	wantNormalized := filepath.Join(outputDir, "normalized", "shot_001.mp4")
	if validator.path == wantNormalized {
		t.Fatalf("validator should inspect staged normalized shot before commit, got canonical path %q", validator.path)
	}
	if filepath.Dir(validator.path) != filepath.Dir(wantNormalized) {
		t.Fatalf("validator staged normalized dir = %q, want %q", filepath.Dir(validator.path), filepath.Dir(wantNormalized))
	}
	stagedName := filepath.Base(validator.path)
	if !strings.HasPrefix(stagedName, ".shot_001.mp4_") || !strings.HasSuffix(stagedName, ".tmp.mp4") {
		t.Fatalf("validator staged normalized name = %q, want normalized temp artifact", stagedName)
	}
	spec := validator.specByPath[validator.path]
	if spec.Width != 720 || spec.Height != 1280 || spec.FPS != 24 {
		t.Fatalf("normalized spec = %+v, want 720x1280@24", spec)
	}
	if spec.CodecName != "h264" || spec.PixelFormat != "yuv420p" || !spec.RequireNoAudio {
		t.Fatalf("normalized spec = %+v, want h264/yuv420p silent", spec)
	}
	if spec.ExpectedDurationSec != 8 {
		t.Fatalf("normalized duration = %v, want 8", spec.ExpectedDurationSec)
	}
}

func TestHyperFramesFFmpegRenderer_PreservesExistingNormalizedShotWhenValidationFails(t *testing.T) {
	outputDir := t.TempDir()
	normalizedPath := filepath.Join(outputDir, "normalized", "shot_001.mp4")
	const oldNormalized = "old-normalized"
	if err := os.MkdirAll(filepath.Dir(normalizedPath), 0755); err != nil {
		t.Fatalf("mkdir normalized dir: %v", err)
	}
	if err := os.WriteFile(normalizedPath, []byte(oldNormalized), 0644); err != nil {
		t.Fatalf("write existing normalized shot: %v", err)
	}
	validator := &stubOutputValidator{
		errForPath: func(path string) error {
			if strings.Contains(filepath.Base(path), "shot_001.mp4") {
				return errors.New("bad normalized shot")
			}
			return nil
		},
	}
	renderer := xaipipeline.NewHyperFramesFFmpegRenderer(&stubHyperFramesExecutor{}, &stubVideoFinalizer{}).
		WithShotNormalizer(&stubShotNormalizer{}).
		WithOutputValidator(validator).
		WithPreviewExtractor(&stubPreviewExtractor{})

	_, err := renderer.Render(context.Background(), xaipipeline.Manifest{
		ProjectID: "normalized-preserve-test",
		FPS:       24,
		Width:     720,
		Height:    1280,
		Shots: []xaipipeline.Shot{
			{Index: 1, Prompt: "shot 1", VideoPath: "shots/shot_001.mp4", DurationSec: 8},
		},
	}, outputDir)
	if err == nil {
		t.Fatal("Render() error = nil, want normalized validation error")
	}
	if !strings.Contains(err.Error(), "validate normalized xai shot 1") {
		t.Fatalf("Render() error = %v, want normalized validation error", err)
	}
	after, err := os.ReadFile(normalizedPath)
	if err != nil {
		t.Fatalf("read normalized shot after failed render: %v", err)
	}
	if string(after) != oldNormalized {
		t.Fatalf("existing normalized shot should be preserved after failed validation: %q", string(after))
	}
}

func TestHyperFramesFFmpegRenderer_RejectsCanceledContextAfterNormalizedShotValidationBeforeCommit(t *testing.T) {
	outputDir := t.TempDir()
	ctx, cancel := context.WithCancel(context.Background())
	normalizer := &stubShotNormalizer{}
	executor := &stubHyperFramesExecutor{}
	finalizer := &stubVideoFinalizer{}
	validator := &stubOutputValidator{
		errForPath: func(path string) error {
			if strings.Contains(filepath.Base(path), "shot_001.mp4") {
				info, err := os.Stat(path)
				if err == nil && info.Size() > 0 {
					cancel()
				}
			}
			return nil
		},
	}
	renderer := xaipipeline.NewHyperFramesFFmpegRenderer(executor, finalizer).
		WithShotNormalizer(normalizer).
		WithOutputValidator(validator).
		WithPreviewExtractor(&stubPreviewExtractor{})

	_, err := renderer.Render(ctx, xaipipeline.Manifest{
		ProjectID: "normalized-cancel-test",
		FPS:       24,
		Width:     720,
		Height:    1280,
		Shots: []xaipipeline.Shot{
			{Index: 1, Prompt: "shot 1", VideoPath: "shots/shot_001.mp4", DurationSec: 8},
		},
	}, outputDir)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Render() error = %v, want context.Canceled", err)
	}
	if normalizer.calls != 1 {
		t.Fatalf("normalizer calls = %d, want 1", normalizer.calls)
	}
	if executor.calls != 0 {
		t.Fatalf("executor calls = %d, want 0 after context cancellation", executor.calls)
	}
	if finalizer.calls != 0 {
		t.Fatalf("finalizer calls = %d, want 0 after context cancellation", finalizer.calls)
	}
	if _, statErr := os.Stat(filepath.Join(outputDir, "normalized", "shot_001.mp4")); !os.IsNotExist(statErr) {
		t.Fatalf("normalized shot should not be committed after context cancellation, stat err=%v", statErr)
	}
	if _, statErr := os.Stat(filepath.Join(outputDir, "hyperframes")); !os.IsNotExist(statErr) {
		t.Fatalf("hyperframes project should not be created after context cancellation, stat err=%v", statErr)
	}
}

func TestHyperFramesFFmpegRenderer_RollsBackCommittedNormalizedShotsWhenLaterCommitFails(t *testing.T) {
	outputDir := t.TempDir()
	normalizedDir := filepath.Join(outputDir, "normalized")
	if err := os.MkdirAll(normalizedDir, 0755); err != nil {
		t.Fatalf("mkdir normalized dir: %v", err)
	}
	shot1Path := filepath.Join(normalizedDir, "shot_001.mp4")
	const oldShot1 = "old-normalized-1"
	if err := os.WriteFile(shot1Path, []byte(oldShot1), 0644); err != nil {
		t.Fatalf("write existing normalized shot 1: %v", err)
	}
	shot2Path := filepath.Join(normalizedDir, "shot_002.mp4")
	if err := os.Mkdir(shot2Path, 0755); err != nil {
		t.Fatalf("mkdir normalized shot 2 blocker: %v", err)
	}
	executor := &stubHyperFramesExecutor{}
	finalizer := &stubVideoFinalizer{}
	renderer := xaipipeline.NewHyperFramesFFmpegRenderer(executor, finalizer).
		WithShotNormalizer(&stubShotNormalizer{}).
		WithOutputValidator(&stubOutputValidator{}).
		WithPreviewExtractor(&stubPreviewExtractor{})

	_, err := renderer.Render(context.Background(), xaipipeline.Manifest{
		ProjectID: "normalized-commit-rollback-test",
		FPS:       24,
		Width:     720,
		Height:    1280,
		Shots: []xaipipeline.Shot{
			{Index: 1, Prompt: "shot 1", VideoPath: "shots/shot_001.mp4", DurationSec: 8},
			{Index: 2, Prompt: "shot 2", VideoPath: "shots/shot_002.mp4", DurationSec: 8},
		},
	}, outputDir)
	if err == nil {
		t.Fatal("Render() error = nil, want normalized commit error")
	}
	if !strings.Contains(err.Error(), "write normalized xai shot 2") {
		t.Fatalf("Render() error = %v, want normalized shot 2 commit error", err)
	}
	after, err := os.ReadFile(shot1Path)
	if err != nil {
		t.Fatalf("read normalized shot 1 after failed render: %v", err)
	}
	if string(after) != oldShot1 {
		t.Fatalf("normalized shot 1 should roll back after shot 2 commit failure: %q", string(after))
	}
	if executor.calls != 0 {
		t.Fatalf("executor calls = %d, want 0 after normalized commit failure", executor.calls)
	}
	if finalizer.calls != 0 {
		t.Fatalf("finalizer calls = %d, want 0 after normalized commit failure", finalizer.calls)
	}
}

func TestHyperFramesFFmpegRenderer_RejectsInvalidTimelineBeforeFinalize(t *testing.T) {
	outputDir := t.TempDir()
	executor := &stubHyperFramesExecutor{}
	finalizer := &stubVideoFinalizer{}
	validator := &stubOutputValidator{
		errForPath: func(path string) error {
			if strings.Contains(filepath.Base(path), "timeline_hyperframes.mp4") {
				return errors.New("timeline has audio")
			}
			return nil
		},
	}
	renderer := xaipipeline.NewHyperFramesFFmpegRenderer(executor, finalizer).
		WithShotNormalizer(&stubShotNormalizer{}).
		WithOutputValidator(validator).
		WithPreviewExtractor(&stubPreviewExtractor{})

	_, err := renderer.Render(context.Background(), xaipipeline.Manifest{
		ProjectID: "timeline-validation-test",
		FPS:       24,
		Width:     720,
		Height:    1280,
		Shots: []xaipipeline.Shot{
			{Index: 1, Prompt: "shot 1", VideoPath: "shots/shot_001.mp4", DurationSec: 8},
		},
	}, outputDir)
	if err == nil {
		t.Fatal("Render() error = nil, want timeline validation error")
	}
	if !strings.Contains(err.Error(), "validate hyperframes timeline") {
		t.Fatalf("Render() error = %v, want timeline validation error", err)
	}
	if executor.calls != 1 {
		t.Fatalf("executor calls = %d, want 1", executor.calls)
	}
	if finalizer.calls != 0 {
		t.Fatalf("finalizer calls = %d, want 0 before valid timeline", finalizer.calls)
	}
	if executor.outputPath == "" || !validationPathsContain(validator.paths, executor.outputPath) {
		t.Fatalf("validator paths = %#v, want staged timeline validation %q", validator.paths, executor.outputPath)
	}
	spec := validator.specByPath[executor.outputPath]
	if spec.Width != 720 || spec.Height != 1280 || spec.FPS != 24 {
		t.Fatalf("timeline spec = %+v, want 720x1280@24", spec)
	}
	if spec.CodecName != "" || spec.PixelFormat != "" {
		t.Fatalf("timeline spec = %+v, want no codec/pixel-format constraint", spec)
	}
	if spec.ExpectedDurationSec != 8 || !spec.RequireNoAudio {
		t.Fatalf("timeline spec = %+v, want 8s silent", spec)
	}
}

func TestHyperFramesFFmpegRenderer_PreservesExistingTimelineWhenTimelineValidationFails(t *testing.T) {
	outputDir := t.TempDir()
	timelinePath := filepath.Join(outputDir, "timeline_hyperframes.mp4")
	const oldTimeline = "old-timeline"
	if err := os.WriteFile(timelinePath, []byte(oldTimeline), 0644); err != nil {
		t.Fatalf("write existing timeline: %v", err)
	}
	validator := &stubOutputValidator{
		errForPath: func(path string) error {
			if strings.Contains(filepath.Base(path), "timeline_hyperframes.mp4") {
				return errors.New("bad timeline")
			}
			return nil
		},
	}
	finalizer := &stubVideoFinalizer{}
	renderer := xaipipeline.NewHyperFramesFFmpegRenderer(&stubHyperFramesExecutor{}, finalizer).
		WithShotNormalizer(&stubShotNormalizer{}).
		WithOutputValidator(validator).
		WithPreviewExtractor(&stubPreviewExtractor{})

	_, err := renderer.Render(context.Background(), xaipipeline.Manifest{
		ProjectID: "timeline-preserve-test",
		FPS:       24,
		Width:     720,
		Height:    1280,
		Shots: []xaipipeline.Shot{
			{Index: 1, Prompt: "shot 1", VideoPath: "shots/shot_001.mp4", DurationSec: 8},
		},
	}, outputDir)
	if err == nil {
		t.Fatal("Render() error = nil, want timeline validation error")
	}
	if !strings.Contains(err.Error(), "validate hyperframes timeline") {
		t.Fatalf("Render() error = %v, want timeline validation error", err)
	}
	if finalizer.calls != 0 {
		t.Fatalf("finalizer calls = %d, want 0 before valid timeline", finalizer.calls)
	}
	after, err := os.ReadFile(timelinePath)
	if err != nil {
		t.Fatalf("read timeline after failed render: %v", err)
	}
	if string(after) != oldTimeline {
		t.Fatalf("existing timeline should be preserved after failed timeline validation: %q", string(after))
	}
}

func TestHyperFramesFFmpegRenderer_DoesNotFinalizeWhenTimelineCommitFails(t *testing.T) {
	outputDir := t.TempDir()
	timelinePath := filepath.Join(outputDir, "timeline_hyperframes.mp4")
	if err := os.Mkdir(timelinePath, 0755); err != nil {
		t.Fatalf("mkdir timeline blocker: %v", err)
	}
	executor := &stubHyperFramesExecutor{}
	finalizer := &stubVideoFinalizer{}
	renderer := xaipipeline.NewHyperFramesFFmpegRenderer(executor, finalizer).
		WithShotNormalizer(&stubShotNormalizer{}).
		WithOutputValidator(&stubOutputValidator{}).
		WithPreviewExtractor(&stubPreviewExtractor{})

	_, err := renderer.Render(context.Background(), xaipipeline.Manifest{
		ProjectID: "timeline-commit-failure-test",
		FPS:       24,
		Width:     720,
		Height:    1280,
		Shots: []xaipipeline.Shot{
			{Index: 1, Prompt: "shot 1", VideoPath: "shots/shot_001.mp4", DurationSec: 8},
		},
	}, outputDir)
	if err == nil {
		t.Fatal("Render() error = nil, want timeline commit error")
	}
	if !strings.Contains(err.Error(), "write hyperframes timeline") {
		t.Fatalf("Render() error = %v, want timeline commit error", err)
	}
	if executor.calls != 1 {
		t.Fatalf("executor calls = %d, want 1", executor.calls)
	}
	if finalizer.calls != 0 {
		t.Fatalf("finalizer calls = %d, want 0 after timeline commit failure", finalizer.calls)
	}
	info, statErr := os.Stat(timelinePath)
	if statErr != nil {
		t.Fatalf("stat timeline blocker after failed render: %v", statErr)
	}
	if !info.IsDir() {
		t.Fatalf("timeline blocker should remain a directory after failed render")
	}
	if _, statErr := os.Stat(filepath.Join(outputDir, "output_xai.mp4")); !os.IsNotExist(statErr) {
		t.Fatalf("final output should not exist after timeline commit failure, stat err=%v", statErr)
	}
}

func TestHyperFramesFFmpegRenderer_ReturnsValidationError(t *testing.T) {
	outputDir := t.TempDir()
	validator := &stubOutputValidator{
		errForPath: func(path string) error {
			if strings.Contains(filepath.Base(path), "output_xai.mp4") {
				return errors.New("bad duration")
			}
			return nil
		},
	}
	renderer := newTestHyperFramesRenderer(&stubHyperFramesExecutor{}, &stubVideoFinalizer{}).
		WithOutputValidator(validator)

	_, err := renderer.Render(context.Background(), xaipipeline.Manifest{
		ProjectID: "metadata-test",
		FPS:       24,
		Width:     720,
		Height:    1280,
		Shots: []xaipipeline.Shot{
			{Index: 1, Prompt: "shot 1", VideoPath: "shots/shot_001.mp4", DurationSec: 8},
		},
	}, outputDir)
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "validate final output") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestHyperFramesFFmpegRenderer_PreservesExistingFinalOutputWhenValidationFails(t *testing.T) {
	outputDir := t.TempDir()
	outputPath := filepath.Join(outputDir, "output_xai.mp4")
	const oldFinal = "old-final"
	if err := os.WriteFile(outputPath, []byte(oldFinal), 0644); err != nil {
		t.Fatalf("write existing final output: %v", err)
	}
	validator := &stubOutputValidator{
		errForPath: func(path string) error {
			if strings.Contains(filepath.Base(path), "output_xai.mp4") {
				return errors.New("bad final output")
			}
			return nil
		},
	}
	renderer := newTestHyperFramesRenderer(&stubHyperFramesExecutor{}, &stubVideoFinalizer{}).
		WithOutputValidator(validator)

	_, err := renderer.Render(context.Background(), xaipipeline.Manifest{
		ProjectID: "final-output-preserve-test",
		FPS:       24,
		Width:     720,
		Height:    1280,
		Shots: []xaipipeline.Shot{
			{Index: 1, Prompt: "shot 1", VideoPath: "shots/shot_001.mp4", DurationSec: 8},
		},
	}, outputDir)
	if err == nil {
		t.Fatal("Render() error = nil, want final output validation error")
	}
	if !strings.Contains(err.Error(), "validate final output") {
		t.Fatalf("Render() error = %v, want final output validation error", err)
	}
	after, err := os.ReadFile(outputPath)
	if err != nil {
		t.Fatalf("read final output after failed render: %v", err)
	}
	if string(after) != oldFinal {
		t.Fatalf("existing final output should be preserved after failed final validation: %q", string(after))
	}
}

func TestHyperFramesFFmpegRenderer_PreservesExistingFinalOutputWhenRenderMetadataCommitFails(t *testing.T) {
	outputDir := t.TempDir()
	outputPath := filepath.Join(outputDir, "output_xai.mp4")
	const oldFinal = "old-final-before-metadata-failure"
	if err := os.WriteFile(outputPath, []byte(oldFinal), 0644); err != nil {
		t.Fatalf("write existing final output: %v", err)
	}
	metadataPath := filepath.Join(outputDir, "render_metadata.json")
	if err := os.Mkdir(metadataPath, 0755); err != nil {
		t.Fatalf("mkdir metadata blocker: %v", err)
	}
	renderer := newTestHyperFramesRenderer(&stubHyperFramesExecutor{}, &stubVideoFinalizer{})

	_, err := renderer.Render(context.Background(), xaipipeline.Manifest{
		ProjectID: "metadata-commit-preserve-test",
		FPS:       24,
		Width:     720,
		Height:    1280,
		Shots: []xaipipeline.Shot{
			{Index: 1, Prompt: "shot 1", VideoPath: "shots/shot_001.mp4", DurationSec: 8},
		},
	}, outputDir)
	if err == nil {
		t.Fatal("Render() error = nil, want render metadata commit error")
	}
	if !strings.Contains(err.Error(), "write render metadata") {
		t.Fatalf("Render() error = %v, want render metadata commit error", err)
	}
	after, err := os.ReadFile(outputPath)
	if err != nil {
		t.Fatalf("read final output after failed render: %v", err)
	}
	if string(after) != oldFinal {
		t.Fatalf("existing final output should be preserved after failed metadata commit: %q", string(after))
	}
}

func TestHyperFramesFFmpegRenderer_DoesNotCommitMetadataOrPreviewWhenFinalOutputCommitFails(t *testing.T) {
	outputDir := t.TempDir()
	outputPath := filepath.Join(outputDir, "output_xai.mp4")
	if err := os.Mkdir(outputPath, 0755); err != nil {
		t.Fatalf("mkdir final output blocker: %v", err)
	}
	metadataPath := filepath.Join(outputDir, "render_metadata.json")
	const oldMetadata = `{"path":"old-output_xai.mp4"}`
	if err := os.WriteFile(metadataPath, []byte(oldMetadata), 0644); err != nil {
		t.Fatalf("write existing render metadata: %v", err)
	}
	previewPath := filepath.Join(outputDir, "preview_frame.jpg")
	const oldPreview = "old-preview"
	if err := os.WriteFile(previewPath, []byte(oldPreview), 0644); err != nil {
		t.Fatalf("write existing preview: %v", err)
	}
	renderer := newTestHyperFramesRenderer(&stubHyperFramesExecutor{}, &stubVideoFinalizer{})

	_, err := renderer.Render(context.Background(), xaipipeline.Manifest{
		ProjectID: "final-commit-preserve-test",
		FPS:       24,
		Width:     720,
		Height:    1280,
		Shots: []xaipipeline.Shot{
			{Index: 1, Prompt: "shot 1", VideoPath: "shots/shot_001.mp4", DurationSec: 8},
		},
	}, outputDir)
	if err == nil {
		t.Fatal("Render() error = nil, want final output commit error")
	}
	if !strings.Contains(err.Error(), "write final output") {
		t.Fatalf("Render() error = %v, want final output commit error", err)
	}
	metadataAfter, err := os.ReadFile(metadataPath)
	if err != nil {
		t.Fatalf("read render metadata after failed render: %v", err)
	}
	if string(metadataAfter) != oldMetadata {
		t.Fatalf("existing render metadata should be preserved after failed final commit: %q", string(metadataAfter))
	}
	previewAfter, err := os.ReadFile(previewPath)
	if err != nil {
		t.Fatalf("read preview after failed render: %v", err)
	}
	if string(previewAfter) != oldPreview {
		t.Fatalf("existing preview should be preserved after failed final commit: %q", string(previewAfter))
	}
}

func TestHyperFramesFFmpegRenderer_PreservesExistingFinalBundleWhenPreviewCommitFails(t *testing.T) {
	outputDir := t.TempDir()
	outputPath := filepath.Join(outputDir, "output_xai.mp4")
	const oldFinal = "old-final-before-preview-failure"
	if err := os.WriteFile(outputPath, []byte(oldFinal), 0644); err != nil {
		t.Fatalf("write existing final output: %v", err)
	}
	metadataPath := filepath.Join(outputDir, "render_metadata.json")
	const oldMetadata = `{"path":"old-output_xai.mp4"}`
	if err := os.WriteFile(metadataPath, []byte(oldMetadata), 0644); err != nil {
		t.Fatalf("write existing render metadata: %v", err)
	}
	previewPath := filepath.Join(outputDir, "preview_frame.jpg")
	if err := os.Mkdir(previewPath, 0755); err != nil {
		t.Fatalf("mkdir preview blocker: %v", err)
	}
	renderer := newTestHyperFramesRenderer(&stubHyperFramesExecutor{}, &stubVideoFinalizer{})

	_, err := renderer.Render(context.Background(), xaipipeline.Manifest{
		ProjectID: "preview-commit-preserve-test",
		FPS:       24,
		Width:     720,
		Height:    1280,
		Shots: []xaipipeline.Shot{
			{Index: 1, Prompt: "shot 1", VideoPath: "shots/shot_001.mp4", DurationSec: 8},
		},
	}, outputDir)
	if err == nil {
		t.Fatal("Render() error = nil, want preview commit error")
	}
	if !strings.Contains(err.Error(), "write preview frame") {
		t.Fatalf("Render() error = %v, want preview commit error", err)
	}
	finalAfter, err := os.ReadFile(outputPath)
	if err != nil {
		t.Fatalf("read final output after failed render: %v", err)
	}
	if string(finalAfter) != oldFinal {
		t.Fatalf("existing final output should be preserved after failed preview commit: %q", string(finalAfter))
	}
	metadataAfter, err := os.ReadFile(metadataPath)
	if err != nil {
		t.Fatalf("read render metadata after failed render: %v", err)
	}
	if string(metadataAfter) != oldMetadata {
		t.Fatalf("existing render metadata should be preserved after failed preview commit: %q", string(metadataAfter))
	}
}

func TestHyperFramesFFmpegRenderer_NormalizesShotsBeforeTimeline(t *testing.T) {
	outputDir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(outputDir, "shots"), 0755); err != nil {
		t.Fatal(err)
	}
	rawShot := filepath.Join(outputDir, "shots", "shot_001.mp4")
	if err := os.WriteFile(rawShot, []byte("raw"), 0644); err != nil {
		t.Fatal(err)
	}

	executor := &stubHyperFramesExecutor{}
	finalizer := &stubVideoFinalizer{}
	normalizer := &stubShotNormalizer{}
	renderer := xaipipeline.NewHyperFramesFFmpegRenderer(executor, finalizer).
		WithShotNormalizer(normalizer).
		WithOutputValidator(&stubOutputValidator{}).
		WithPreviewExtractor(&stubPreviewExtractor{})

	_, err := renderer.Render(context.Background(), xaipipeline.Manifest{
		ProjectID: "normalize-test",
		FPS:       24,
		Width:     720,
		Height:    1280,
		Shots: []xaipipeline.Shot{
			{Index: 1, Prompt: "shot 1", VideoPath: "shots/shot_001.mp4", DurationSec: 8},
		},
	}, outputDir)
	if err != nil {
		t.Fatalf("Render: %v", err)
	}

	if normalizer.calls != 1 {
		t.Fatalf("normalizer calls = %d, want 1", normalizer.calls)
	}
	if normalizer.inputPath != rawShot {
		t.Fatalf("normalizer input = %q, want %q", normalizer.inputPath, rawShot)
	}
	wantNormalized := filepath.Join(outputDir, "normalized", "shot_001.mp4")
	if normalizer.outputPath == wantNormalized {
		t.Fatalf("normalizer should write staged normalized shot before commit, got canonical path %q", normalizer.outputPath)
	}
	if filepath.Dir(normalizer.outputPath) != filepath.Dir(wantNormalized) {
		t.Fatalf("normalizer staged output dir = %q, want %q", filepath.Dir(normalizer.outputPath), filepath.Dir(wantNormalized))
	}
	stagedName := filepath.Base(normalizer.outputPath)
	if !strings.HasPrefix(stagedName, ".shot_001.mp4_") || !strings.HasSuffix(stagedName, ".tmp.mp4") {
		t.Fatalf("normalizer staged output name = %q, want normalized temp artifact", stagedName)
	}
	if _, err := os.Stat(wantNormalized); err != nil {
		t.Fatalf("committed normalized shot missing: %v", err)
	}

	htmlBytes, err := os.ReadFile(filepath.Join(outputDir, "hyperframes", "index.html"))
	if err != nil {
		t.Fatalf("read index.html: %v", err)
	}
	html := string(htmlBytes)
	if !strings.Contains(html, `../normalized/shot_001.mp4`) {
		t.Fatalf("index.html should use normalized shot path:\n%s", html)
	}
	if strings.Contains(html, `../shots/shot_001.mp4`) {
		t.Fatalf("index.html should not use raw shot path after normalization:\n%s", html)
	}
}

func TestHyperFramesFFmpegRenderer_RejectsNonCanonicalManifestVideoPathBeforeNormalization(t *testing.T) {
	outputDir := t.TempDir()
	externalShot := filepath.Join(t.TempDir(), "external-shot.mp4")
	if err := os.WriteFile(externalShot, []byte("external"), 0644); err != nil {
		t.Fatal(err)
	}

	normalizer := &stubShotNormalizer{}
	renderer := xaipipeline.NewHyperFramesFFmpegRenderer(&stubHyperFramesExecutor{}, &stubVideoFinalizer{}).
		WithShotNormalizer(normalizer).
		WithOutputValidator(&stubOutputValidator{}).
		WithPreviewExtractor(&stubPreviewExtractor{})

	_, err := renderer.Render(context.Background(), xaipipeline.Manifest{
		ProjectID: "canonical-normalize-test",
		FPS:       24,
		Width:     720,
		Height:    1280,
		Shots: []xaipipeline.Shot{
			{Index: 1, Prompt: "shot 1", VideoPath: externalShot, DurationSec: 8},
		},
	}, outputDir)
	if err == nil {
		t.Fatal("Render() error = nil, want non-canonical video_path error")
	}
	if !strings.Contains(err.Error(), "video_path") || !strings.Contains(err.Error(), "shots/shot_001.mp4") {
		t.Fatalf("Render() error = %v, want canonical video_path error", err)
	}
	if normalizer.calls != 0 {
		t.Fatalf("normalizer calls = %d, want 0 before canonical video_path validation passes", normalizer.calls)
	}
	if _, statErr := os.Stat(filepath.Join(outputDir, "normalized")); !os.IsNotExist(statErr) {
		t.Fatalf("normalized dir should not be created before canonical video_path validation passes, stat err=%v", statErr)
	}
	if _, statErr := os.Stat(filepath.Join(outputDir, "hyperframes")); !os.IsNotExist(statErr) {
		t.Fatalf("hyperframes dir should not be created before canonical video_path validation passes, stat err=%v", statErr)
	}
}

func TestHyperFramesFFmpegRenderer_RejectsUnsupportedRenderSpecBeforeNormalization(t *testing.T) {
	tests := []struct {
		name     string
		manifest xaipipeline.Manifest
		want     string
	}{
		{
			name: "project id",
			manifest: xaipipeline.Manifest{
				ProjectID: "../escaped",
				Format:    "portrait",
				FPS:       24,
				Width:     720,
				Height:    1280,
				Shots: []xaipipeline.Shot{
					{Index: 1, Prompt: "shot 1", VideoPath: "shots/shot_001.mp4", DurationSec: 8},
				},
			},
			want: "project_id",
		},
		{
			name: "dimensions",
			manifest: xaipipeline.Manifest{
				ProjectID: "bad-dimensions",
				FPS:       24,
				Width:     1080,
				Height:    1920,
				Shots: []xaipipeline.Shot{
					{Index: 1, Prompt: "shot 1", VideoPath: "shots/shot_001.mp4", DurationSec: 8},
				},
			},
			want: "render dimensions",
		},
		{
			name: "fps",
			manifest: xaipipeline.Manifest{
				ProjectID: "bad-fps",
				FPS:       30,
				Width:     720,
				Height:    1280,
				Shots: []xaipipeline.Shot{
					{Index: 1, Prompt: "shot 1", VideoPath: "shots/shot_001.mp4", DurationSec: 8},
				},
			},
			want: "fps",
		},
		{
			name: "format value",
			manifest: xaipipeline.Manifest{
				ProjectID: "bad-format",
				Format:    "landscape",
				FPS:       24,
				Width:     720,
				Height:    1280,
				Shots: []xaipipeline.Shot{
					{Index: 1, Prompt: "shot 1", VideoPath: "shots/shot_001.mp4", DurationSec: 8},
				},
			},
			want: "format",
		},
		{
			name: "format canonical form",
			manifest: xaipipeline.Manifest{
				ProjectID: "bad-format-canonical",
				Format:    " portrait ",
				FPS:       24,
				Width:     720,
				Height:    1280,
				Shots: []xaipipeline.Shot{
					{Index: 1, Prompt: "shot 1", VideoPath: "shots/shot_001.mp4", DurationSec: 8},
				},
			},
			want: "format",
		},
		{
			name: "transition",
			manifest: xaipipeline.Manifest{
				ProjectID: "bad-transition",
				Format:    "portrait",
				FPS:       24,
				Width:     720,
				Height:    1280,
				Shots: []xaipipeline.Shot{
					{Index: 1, Prompt: "shot 1", VideoPath: "shots/shot_001.mp4", DurationSec: 8, TransitionOut: "wipe"},
				},
			},
			want: "transition_out",
		},
		{
			name: "duration",
			manifest: xaipipeline.Manifest{
				ProjectID: "bad-duration",
				Format:    "portrait",
				FPS:       24,
				Width:     720,
				Height:    1280,
				Shots: []xaipipeline.Shot{
					{Index: 1, Prompt: "shot 1", VideoPath: "shots/shot_001.mp4", DurationSec: 0},
				},
			},
			want: "duration_sec",
		},
		{
			name: "prompt canonical form",
			manifest: xaipipeline.Manifest{
				ProjectID: "bad-prompt-canonical",
				Format:    "portrait",
				FPS:       24,
				Width:     720,
				Height:    1280,
				Shots: []xaipipeline.Shot{
					{Index: 1, Prompt: " shot ", VideoPath: "shots/shot_001.mp4", DurationSec: 8},
				},
			},
			want: "prompt",
		},
		{
			name: "prompt empty",
			manifest: xaipipeline.Manifest{
				ProjectID: "bad-prompt-empty",
				Format:    "portrait",
				FPS:       24,
				Width:     720,
				Height:    1280,
				Shots: []xaipipeline.Shot{
					{Index: 1, Prompt: " \t\n", VideoPath: "shots/shot_001.mp4", DurationSec: 8},
				},
			},
			want: "prompt is empty",
		},
		{
			name: "aspect ratio value",
			manifest: xaipipeline.Manifest{
				ProjectID: "bad-aspect-ratio",
				Format:    "portrait",
				FPS:       24,
				Width:     720,
				Height:    1280,
				Shots: []xaipipeline.Shot{
					{Index: 1, Prompt: "shot 1", VideoPath: "shots/shot_001.mp4", DurationSec: 8, AspectRatio: "1:1"},
				},
			},
			want: "aspect_ratio",
		},
		{
			name: "aspect ratio canonical form",
			manifest: xaipipeline.Manifest{
				ProjectID: "bad-aspect-ratio-canonical",
				Format:    "portrait",
				FPS:       24,
				Width:     720,
				Height:    1280,
				Shots: []xaipipeline.Shot{
					{Index: 1, Prompt: "shot 1", VideoPath: "shots/shot_001.mp4", DurationSec: 8, AspectRatio: " 9:16 "},
				},
			},
			want: "aspect_ratio",
		},
		{
			name: "resolution value",
			manifest: xaipipeline.Manifest{
				ProjectID: "bad-resolution",
				Format:    "portrait",
				FPS:       24,
				Width:     720,
				Height:    1280,
				Shots: []xaipipeline.Shot{
					{Index: 1, Prompt: "shot 1", VideoPath: "shots/shot_001.mp4", DurationSec: 8, Resolution: "1080p"},
				},
			},
			want: "resolution",
		},
		{
			name: "resolution canonical form",
			manifest: xaipipeline.Manifest{
				ProjectID: "bad-resolution-canonical",
				Format:    "portrait",
				FPS:       24,
				Width:     720,
				Height:    1280,
				Shots: []xaipipeline.Shot{
					{Index: 1, Prompt: "shot 1", VideoPath: "shots/shot_001.mp4", DurationSec: 8, Resolution: " 720p "},
				},
			},
			want: "resolution",
		},
		{
			name: "subtitle canonical form",
			manifest: xaipipeline.Manifest{
				ProjectID: "bad-subtitle-canonical",
				Format:    "portrait",
				FPS:       24,
				Width:     720,
				Height:    1280,
				Shots: []xaipipeline.Shot{
					{Index: 1, Prompt: "shot 1", VideoPath: "shots/shot_001.mp4", DurationSec: 8, Subtitle: " 第一個字幕 "},
				},
			},
			want: "subtitle",
		},
		{
			name: "duplicate shot index",
			manifest: xaipipeline.Manifest{
				ProjectID: "bad-duplicate-index",
				Format:    "portrait",
				FPS:       24,
				Width:     720,
				Height:    1280,
				Shots: []xaipipeline.Shot{
					{Index: 1, Prompt: "shot 1", VideoPath: "shots/shot_001.mp4", DurationSec: 8},
					{Index: 1, Prompt: "shot 1", VideoPath: "shots/shot_001.mp4", DurationSec: 8},
				},
			},
			want: "duplicate shot index",
		},
		{
			name: "non-contiguous shot index",
			manifest: xaipipeline.Manifest{
				ProjectID: "bad-non-contiguous-index",
				Format:    "portrait",
				FPS:       24,
				Width:     720,
				Height:    1280,
				Shots: []xaipipeline.Shot{
					{Index: 2, Prompt: "shot 2", VideoPath: "shots/shot_002.mp4", DurationSec: 8},
				},
			},
			want: "shot indexes must be contiguous from 1",
		},
		{
			name: "out-of-order shot index",
			manifest: xaipipeline.Manifest{
				ProjectID: "bad-out-of-order-index",
				Format:    "portrait",
				FPS:       24,
				Width:     720,
				Height:    1280,
				Shots: []xaipipeline.Shot{
					{Index: 2, Prompt: "shot 2", VideoPath: "shots/shot_002.mp4", DurationSec: 8},
					{Index: 1, Prompt: "shot 1", VideoPath: "shots/shot_001.mp4", DurationSec: 8},
				},
			},
			want: "shot indexes must match shot order from 1",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			normalizer := &stubShotNormalizer{}
			executor := &stubHyperFramesExecutor{}
			finalizer := &stubVideoFinalizer{}
			renderer := xaipipeline.NewHyperFramesFFmpegRenderer(executor, finalizer).
				WithShotNormalizer(normalizer).
				WithOutputValidator(&stubOutputValidator{}).
				WithPreviewExtractor(&stubPreviewExtractor{})

			_, err := renderer.Render(context.Background(), tt.manifest, t.TempDir())
			if err == nil {
				t.Fatal("Render() error = nil, want unsupported render spec error")
			}
			if !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("Render() error = %v, want %q", err, tt.want)
			}
			if normalizer.calls != 0 {
				t.Fatalf("normalizer calls = %d, want 0 before render spec validation passes", normalizer.calls)
			}
			if executor.calls != 0 {
				t.Fatalf("executor calls = %d, want 0 before render spec validation passes", executor.calls)
			}
			if finalizer.calls != 0 {
				t.Fatalf("finalizer calls = %d, want 0 before render spec validation passes", finalizer.calls)
			}
		})
	}
}

func TestHyperFramesFFmpegRenderer_ResolvesRelativeOutputDirBeforeExternalTools(t *testing.T) {
	t.Chdir(t.TempDir())

	executor := &stubHyperFramesExecutor{}
	finalizer := &stubVideoFinalizer{}
	renderer := newTestHyperFramesRenderer(executor, finalizer)

	got, err := renderer.Render(context.Background(), xaipipeline.Manifest{
		ProjectID: "relative-test",
		Shots: []xaipipeline.Shot{
			{Index: 1, Prompt: "shot 1", VideoPath: "shots/shot_001.mp4", DurationSec: 8},
		},
	}, "relative-output")
	if err != nil {
		t.Fatalf("Render: %v", err)
	}

	if !filepath.IsAbs(got) {
		t.Fatalf("output path should be absolute, got %q", got)
	}
	if !filepath.IsAbs(executor.projectDir) {
		t.Fatalf("project dir should be absolute, got %q", executor.projectDir)
	}
	if !filepath.IsAbs(executor.outputPath) {
		t.Fatalf("timeline path should be absolute, got %q", executor.outputPath)
	}
	if !filepath.IsAbs(finalizer.outputPath) {
		t.Fatalf("final output path should be absolute, got %q", finalizer.outputPath)
	}
}

func TestHyperFramesFFmpegRenderer_RequiresShotNormalizer(t *testing.T) {
	executor := &stubHyperFramesExecutor{}
	finalizer := &stubVideoFinalizer{}
	renderer := xaipipeline.NewHyperFramesFFmpegRenderer(executor, finalizer)

	_, err := renderer.Render(context.Background(), xaipipeline.Manifest{
		ProjectID: "missing-normalizer",
		Shots: []xaipipeline.Shot{
			{Index: 1, Prompt: "shot 1", VideoPath: "shots/shot_001.mp4", DurationSec: 8},
		},
	}, t.TempDir())
	if err == nil {
		t.Fatal("Render() error = nil, want missing normalizer error")
	}
	if !strings.Contains(err.Error(), "shot normalizer is nil") {
		t.Fatalf("Render() error = %v, want shot normalizer error", err)
	}
	if executor.calls != 0 {
		t.Fatalf("executor calls = %d, want 0", executor.calls)
	}
	if finalizer.calls != 0 {
		t.Fatalf("finalizer calls = %d, want 0", finalizer.calls)
	}
}

func TestHyperFramesFFmpegRenderer_RequiresOutputValidator(t *testing.T) {
	executor := &stubHyperFramesExecutor{}
	finalizer := &stubVideoFinalizer{}
	renderer := xaipipeline.NewHyperFramesFFmpegRenderer(executor, finalizer).
		WithShotNormalizer(&stubShotNormalizer{})

	_, err := renderer.Render(context.Background(), xaipipeline.Manifest{
		ProjectID: "missing-validator",
		Shots: []xaipipeline.Shot{
			{Index: 1, Prompt: "shot 1", VideoPath: "shots/shot_001.mp4", DurationSec: 8},
		},
	}, t.TempDir())
	if err == nil {
		t.Fatal("Render() error = nil, want missing output validator error")
	}
	if !strings.Contains(err.Error(), "output validator is nil") {
		t.Fatalf("Render() error = %v, want output validator error", err)
	}
	if executor.calls != 0 {
		t.Fatalf("executor calls = %d, want 0", executor.calls)
	}
	if finalizer.calls != 0 {
		t.Fatalf("finalizer calls = %d, want 0", finalizer.calls)
	}
}

func TestHyperFramesFFmpegRenderer_RequiresPreviewExtractor(t *testing.T) {
	executor := &stubHyperFramesExecutor{}
	finalizer := &stubVideoFinalizer{}
	renderer := xaipipeline.NewHyperFramesFFmpegRenderer(executor, finalizer).
		WithShotNormalizer(&stubShotNormalizer{}).
		WithOutputValidator(&stubOutputValidator{})

	_, err := renderer.Render(context.Background(), xaipipeline.Manifest{
		ProjectID: "missing-previewer",
		Shots: []xaipipeline.Shot{
			{Index: 1, Prompt: "shot 1", VideoPath: "shots/shot_001.mp4", DurationSec: 8},
		},
	}, t.TempDir())
	if err == nil {
		t.Fatal("Render() error = nil, want missing preview extractor error")
	}
	if !strings.Contains(err.Error(), "preview extractor is nil") {
		t.Fatalf("Render() error = %v, want preview extractor error", err)
	}
	if executor.calls != 0 {
		t.Fatalf("executor calls = %d, want 0", executor.calls)
	}
	if finalizer.calls != 0 {
		t.Fatalf("finalizer calls = %d, want 0", finalizer.calls)
	}
}

func TestHyperFramesFFmpegRenderer_RejectsNilExecutor(t *testing.T) {
	renderer := xaipipeline.NewHyperFramesFFmpegRenderer(nil, &stubVideoFinalizer{})
	_, err := renderer.Render(context.Background(), xaipipeline.Manifest{
		ProjectID: "xai-test",
		Shots: []xaipipeline.Shot{
			{Index: 1, Prompt: "shot 1", VideoPath: "shots/shot_001.mp4", DurationSec: 8},
		},
	}, t.TempDir())
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "hyperframes executor is nil") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func isLowercaseHex64(value string) bool {
	if len(value) != 64 {
		return false
	}
	for _, r := range value {
		if (r >= '0' && r <= '9') || (r >= 'a' && r <= 'f') {
			continue
		}
		return false
	}
	return true
}

func testManifestIdentityHash(t *testing.T, manifest xaipipeline.Manifest) string {
	t.Helper()
	data, err := json.Marshal(manifest)
	if err != nil {
		t.Fatalf("marshal manifest for test hash: %v", err)
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}
