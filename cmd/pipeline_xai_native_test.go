package cmd

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/baochen10luo/stagenthand/config"
	"github.com/baochen10luo/stagenthand/internal/video"
	"github.com/baochen10luo/stagenthand/internal/xaipipeline"
	"github.com/spf13/cobra"
)

type stubXAINativeRunner struct {
	inputs        [][]byte
	opts          []xaipipeline.RunOptions
	contexts      []context.Context
	failOnEpisode map[int]error
}

func (s *stubXAINativeRunner) Run(ctx context.Context, input []byte, opts xaipipeline.RunOptions) (*xaipipeline.Result, error) {
	s.inputs = append(s.inputs, append([]byte(nil), input...))
	s.opts = append(s.opts, opts)
	s.contexts = append(s.contexts, ctx)
	if err := s.failOnEpisode[xaiNativeTestEpisodeFromOutputDir(opts.OutputDir)]; err != nil {
		return nil, err
	}
	return &xaipipeline.Result{
		Manifest: xaipipeline.Manifest{
			ProjectID: "stub-xai",
			Shots: []xaipipeline.Shot{
				{Index: 1, VideoPath: "shots/shot_001.mp4"},
			},
		},
		OutputDir:          opts.OutputDir,
		OutputVideo:        filepath.Join(opts.OutputDir, "output_xai.mp4"),
		ManifestPath:       filepath.Join(opts.OutputDir, "xai_manifest.json"),
		RenderMetadataPath: filepath.Join(opts.OutputDir, "render_metadata.json"),
		PreviewFramePath:   filepath.Join(opts.OutputDir, "preview_frame.jpg"),
	}, nil
}

type nilResultXAINativeRunner struct{}

func (nilResultXAINativeRunner) Run(context.Context, []byte, xaipipeline.RunOptions) (*xaipipeline.Result, error) {
	return nil, nil
}

func xaiNativeTestEpisodeFromOutputDir(outputDir string) int {
	base := filepath.Base(outputDir)
	episode, _ := strconv.Atoi(strings.TrimPrefix(base, "episode_"))
	return episode
}

func runPipelineWithStdio(t *testing.T, input string) (string, error) {
	t.Helper()

	oldStdin := os.Stdin
	oldStdout := os.Stdout
	t.Cleanup(func() {
		os.Stdin = oldStdin
		os.Stdout = oldStdout
	})

	stdinR, stdinW, err := os.Pipe()
	if err != nil {
		t.Fatalf("create stdin pipe: %v", err)
	}
	stdoutR, stdoutW, err := os.Pipe()
	if err != nil {
		t.Fatalf("create stdout pipe: %v", err)
	}

	if _, err := stdinW.WriteString(input); err != nil {
		t.Fatalf("write stdin: %v", err)
	}
	if err := stdinW.Close(); err != nil {
		t.Fatalf("close stdin writer: %v", err)
	}

	os.Stdin = stdinR
	os.Stdout = stdoutW
	runErr := runPipeline(&cobra.Command{}, nil)
	if err := stdoutW.Close(); err != nil {
		t.Fatalf("close stdout writer: %v", err)
	}

	var out bytes.Buffer
	if _, err := io.Copy(&out, stdoutR); err != nil {
		t.Fatalf("read stdout: %v", err)
	}
	return out.String(), runErr
}

func TestShouldUseXAINativePipeline(t *testing.T) {
	oldSkipLLM := pipelineSkipLLM
	oldEpisodes := pipelineEpisodes
	oldImageDir := pipelineImageDir
	oldI2V := pipelineI2V
	t.Cleanup(func() {
		pipelineSkipLLM = oldSkipLLM
		pipelineEpisodes = oldEpisodes
		pipelineImageDir = oldImageDir
		pipelineI2V = oldI2V
	})

	pipelineSkipLLM = false
	pipelineEpisodes = 1
	pipelineImageDir = ""
	pipelineI2V = false
	if !shouldUseXAINativePipeline("xai_oauth") {
		t.Fatal("default xai_oauth should use xAI-native pipeline")
	}
	if !shouldUseXAINativePipeline("xai-oauth") {
		t.Fatal("xai-oauth alias should use xAI-native pipeline")
	}
	pipelineImageDir = " \t "
	if !shouldUseXAINativePipeline("xai_oauth") {
		t.Fatal("blank image-dir should behave as unset for single-episode xAI-native pipeline")
	}
	pipelineImageDir = ""
	if shouldUseXAINativeBatchPipeline("xai_oauth") {
		t.Fatal("single episode should not use xAI-native batch pipeline")
	}
	if shouldUseXAINativeBatchPipeline("xai-oauth") {
		t.Fatal("single episode alias should not use xAI-native batch pipeline")
	}

	if shouldUseXAINativePipeline("remotion") {
		t.Fatal("remotion should not use xAI-native pipeline")
	}

	pipelineSkipLLM = true
	if shouldUseXAINativePipeline("xai_oauth") {
		t.Fatal("skip-llm should stay on legacy reuse path")
	}
	pipelineSkipLLM = false

	pipelineEpisodes = 2
	if shouldUseXAINativePipeline("xai_oauth") {
		t.Fatal("batch mode should not use single-episode xAI-native pipeline")
	}
	if !shouldUseXAINativeBatchPipeline("xai_oauth") {
		t.Fatal("batch mode should route to xAI-native batch pipeline")
	}
	if !shouldUseXAINativeBatchPipeline("xai-oauth") {
		t.Fatal("batch mode alias should route to xAI-native batch pipeline")
	}
	pipelineImageDir = " \n "
	if !shouldUseXAINativeBatchPipeline("xai_oauth") {
		t.Fatal("blank image-dir should behave as unset for xAI-native batch pipeline")
	}
	pipelineImageDir = ""
	pipelineEpisodes = 1

	pipelineImageDir = "/tmp/images"
	if shouldUseXAINativePipeline("xai_oauth") {
		t.Fatal("image-dir mode should stay on legacy I2V path")
	}
	pipelineImageDir = ""

	pipelineI2V = true
	if shouldUseXAINativePipeline("xai_oauth") {
		t.Fatal("i2v mode should stay on legacy I2V path")
	}
}

func TestRunXAINativePipeline_UsesGlobalCLIOptions(t *testing.T) {
	oldFactory := newXAINativePipelineRunner
	oldOutputDir := pipelineOutputDir
	oldTargetPanels := pipelineTargetPanels
	oldFormat := pipelineFormat
	oldForceReplan := pipelineForceReplan
	oldForceRegenerate := pipelineForceRegenerate
	oldDryRun := dryRun
	t.Cleanup(func() {
		newXAINativePipelineRunner = oldFactory
		pipelineOutputDir = oldOutputDir
		pipelineTargetPanels = oldTargetPanels
		pipelineFormat = oldFormat
		pipelineForceReplan = oldForceReplan
		pipelineForceRegenerate = oldForceRegenerate
		dryRun = oldDryRun
	})

	runner := &stubXAINativeRunner{}
	var gotDryRun bool
	newXAINativePipelineRunner = func(_ *config.Config, isDryRun bool) (xaiNativeRunner, error) {
		gotDryRun = isDryRun
		return runner, nil
	}
	dryRun = true
	pipelineOutputDir = t.TempDir()
	pipelineTargetPanels = 2
	pipelineFormat = " PORTRAIT "
	pipelineForceReplan = true
	pipelineForceRegenerate = true

	result, err := runXAINativePipeline(context.Background(), []byte("story"), &config.Config{
		Video: config.VideoConfig{
			Provider: "xai-oauth",
			Model:    "  grok-imagine-video-test  ",
		},
	})
	if err != nil {
		t.Fatalf("runXAINativePipeline: %v", err)
	}
	if result.Manifest.ProjectID != "stub-xai" {
		t.Fatalf("project_id = %q", result.Manifest.ProjectID)
	}
	if len(runner.inputs) != 1 {
		t.Fatalf("runner calls = %d, want 1", len(runner.inputs))
	}
	if string(runner.inputs[0]) != "story" {
		t.Fatalf("input = %q", string(runner.inputs[0]))
	}
	if runner.opts[0].OutputDir != pipelineOutputDir {
		t.Fatalf("OutputDir = %q, want %q", runner.opts[0].OutputDir, pipelineOutputDir)
	}
	if runner.opts[0].ShandHome == "" {
		t.Fatal("ShandHome should be set")
	}
	if runner.opts[0].TargetShots != 2 {
		t.Fatalf("TargetShots = %d, want 2", runner.opts[0].TargetShots)
	}
	if runner.opts[0].Format != "portrait" {
		t.Fatalf("Format = %q, want portrait", runner.opts[0].Format)
	}
	if !runner.opts[0].ForceReplan {
		t.Fatal("ForceReplan = false, want true")
	}
	if !runner.opts[0].ForceRegenerate {
		t.Fatal("ForceRegenerate = false, want true")
	}
	if runner.opts[0].VideoModel != "grok-imagine-video-test" {
		t.Fatalf("VideoModel = %q, want grok-imagine-video-test", runner.opts[0].VideoModel)
	}
	if !gotDryRun {
		t.Fatal("factory did not receive dryRun=true")
	}
}

func TestRunXAINativePipeline_RejectsNilRunnerFactoryResult(t *testing.T) {
	oldFactory := newXAINativePipelineRunner
	t.Cleanup(func() {
		newXAINativePipelineRunner = oldFactory
	})

	newXAINativePipelineRunner = func(_ *config.Config, _ bool) (xaiNativeRunner, error) {
		return nil, nil
	}

	defer func() {
		if recovered := recover(); recovered != nil {
			t.Fatalf("runXAINativePipeline() panicked with nil runner: %v", recovered)
		}
	}()

	_, err := runXAINativePipeline(context.Background(), []byte("story"), &config.Config{})
	if err == nil {
		t.Fatal("runXAINativePipeline() error = nil, want nil runner error")
	}
	if !strings.Contains(err.Error(), "xai-native runner is nil") {
		t.Fatalf("runXAINativePipeline() error = %v, want nil runner error", err)
	}
}

func TestRunXAINativePipeline_RejectsNilRunnerResult(t *testing.T) {
	oldFactory := newXAINativePipelineRunner
	t.Cleanup(func() {
		newXAINativePipelineRunner = oldFactory
	})

	newXAINativePipelineRunner = func(_ *config.Config, _ bool) (xaiNativeRunner, error) {
		return nilResultXAINativeRunner{}, nil
	}

	_, err := runXAINativePipeline(context.Background(), []byte("story"), &config.Config{})
	if err == nil {
		t.Fatal("runXAINativePipeline() error = nil, want nil result error")
	}
	if !strings.Contains(err.Error(), "xai-native result is nil") {
		t.Fatalf("runXAINativePipeline() error = %v, want nil result error", err)
	}
}

func TestRunXAINativePipeline_NilContextUsesBackground(t *testing.T) {
	oldFactory := newXAINativePipelineRunner
	oldOutputDir := pipelineOutputDir
	t.Cleanup(func() {
		newXAINativePipelineRunner = oldFactory
		pipelineOutputDir = oldOutputDir
	})

	runner := &stubXAINativeRunner{}
	newXAINativePipelineRunner = func(_ *config.Config, _ bool) (xaiNativeRunner, error) {
		return runner, nil
	}
	pipelineOutputDir = t.TempDir()

	_, err := runXAINativePipeline(nil, []byte("story"), &config.Config{})
	if err != nil {
		t.Fatalf("runXAINativePipeline() error = %v", err)
	}
	if len(runner.contexts) != 1 {
		t.Fatalf("runner contexts = %d, want 1", len(runner.contexts))
	}
	if runner.contexts[0] == nil {
		t.Fatal("runner context is nil, want background context")
	}
}

func TestRunXAINativePipeline_RejectsCanceledContextBeforeFactory(t *testing.T) {
	oldFactory := newXAINativePipelineRunner
	t.Cleanup(func() {
		newXAINativePipelineRunner = oldFactory
	})

	factoryCalled := false
	newXAINativePipelineRunner = func(_ *config.Config, _ bool) (xaiNativeRunner, error) {
		factoryCalled = true
		return &stubXAINativeRunner{}, nil
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := runXAINativePipeline(ctx, []byte("story"), &config.Config{})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("runXAINativePipeline() error = %v, want context.Canceled", err)
	}
	if factoryCalled {
		t.Fatal("xAI-native runner factory was called after context cancellation")
	}
}

func TestRunPipeline_DefaultXAIOAuthSkipsLegacyProviders(t *testing.T) {
	oldFactory := newXAINativePipelineRunner
	oldCfg := cfg
	oldDryRun := dryRun
	oldVideoBackend := pipelineVideoBackend
	oldOutputDir := pipelineOutputDir
	oldTargetPanels := pipelineTargetPanels
	oldFormat := pipelineFormat
	oldEpisodes := pipelineEpisodes
	oldSkipLLM := pipelineSkipLLM
	oldImageDir := pipelineImageDir
	oldI2V := pipelineI2V
	oldMaxRetries := pipelineMaxRetries
	t.Cleanup(func() {
		newXAINativePipelineRunner = oldFactory
		cfg = oldCfg
		dryRun = oldDryRun
		pipelineVideoBackend = oldVideoBackend
		pipelineOutputDir = oldOutputDir
		pipelineTargetPanels = oldTargetPanels
		pipelineFormat = oldFormat
		pipelineEpisodes = oldEpisodes
		pipelineSkipLLM = oldSkipLLM
		pipelineImageDir = oldImageDir
		pipelineI2V = oldI2V
		pipelineMaxRetries = oldMaxRetries
	})

	runner := &stubXAINativeRunner{}
	factoryCalled := false
	newXAINativePipelineRunner = func(appCfg *config.Config, isDryRun bool) (xaiNativeRunner, error) {
		factoryCalled = true
		if isDryRun {
			t.Fatal("default production route should pass dryRun=false")
		}
		if appCfg == nil || appCfg.LLM.Provider != "unknown-local-provider" || appCfg.Image.Provider != "unsupported-image" {
			t.Fatalf("factory config = %#v", appCfg)
		}
		return runner, nil
	}

	dryRun = false
	pipelineVideoBackend = ""
	pipelineOutputDir = t.TempDir()
	pipelineTargetPanels = 1
	pipelineFormat = "portrait"
	pipelineEpisodes = 1
	pipelineSkipLLM = false
	pipelineImageDir = ""
	pipelineI2V = false
	pipelineMaxRetries = 0
	cfg = &config.Config{
		Video: config.VideoConfig{Provider: "xai_oauth"},
		LLM: config.LLMConfig{
			Provider: "unknown-local-provider",
			Model:    "local/qwen",
			BaseURL:  "http://127.0.0.1:9999/v1",
		},
		Image: config.ImageConfig{Provider: "unsupported-image"},
		Audio: config.AudioConfig{
			VoiceProvider: "unsupported-tts",
			MusicProvider: "unsupported-music",
		},
	}

	out, err := runPipelineWithStdio(t, "story")
	if err != nil {
		t.Fatalf("runPipeline() error = %v", err)
	}
	if !factoryCalled {
		t.Fatal("xAI-native factory was not called")
	}
	if len(runner.inputs) != 1 {
		t.Fatalf("runner calls = %d, want 1", len(runner.inputs))
	}

	var summary map[string]any
	if err := json.Unmarshal([]byte(out), &summary); err != nil {
		t.Fatalf("decode stdout JSON: %v\n%s", err, out)
	}
	if summary["pipeline"] != "xai_native" {
		t.Fatalf("pipeline summary = %#v", summary)
	}
	if summary["renderer"] != "hyperframes_ffmpeg" {
		t.Fatalf("renderer summary = %#v", summary)
	}
	if summary["video_backend"] != "xai_oauth" {
		t.Fatalf("video_backend summary = %#v", summary)
	}
}

func TestRunPipeline_DefaultXAIOAuthBatchSkipsLegacyProviders(t *testing.T) {
	oldFactory := newXAINativePipelineRunner
	oldCfg := cfg
	oldDryRun := dryRun
	oldVideoBackend := pipelineVideoBackend
	oldOutputDir := pipelineOutputDir
	oldTargetPanels := pipelineTargetPanels
	oldFormat := pipelineFormat
	oldEpisodes := pipelineEpisodes
	oldBatchConc := pipelineBatchConc
	oldSkipLLM := pipelineSkipLLM
	oldImageDir := pipelineImageDir
	oldI2V := pipelineI2V
	oldMaxRetries := pipelineMaxRetries
	t.Cleanup(func() {
		newXAINativePipelineRunner = oldFactory
		cfg = oldCfg
		dryRun = oldDryRun
		pipelineVideoBackend = oldVideoBackend
		pipelineOutputDir = oldOutputDir
		pipelineTargetPanels = oldTargetPanels
		pipelineFormat = oldFormat
		pipelineEpisodes = oldEpisodes
		pipelineBatchConc = oldBatchConc
		pipelineSkipLLM = oldSkipLLM
		pipelineImageDir = oldImageDir
		pipelineI2V = oldI2V
		pipelineMaxRetries = oldMaxRetries
	})

	runner := &stubXAINativeRunner{}
	factoryCalled := false
	newXAINativePipelineRunner = func(appCfg *config.Config, isDryRun bool) (xaiNativeRunner, error) {
		factoryCalled = true
		if isDryRun {
			t.Fatal("default production batch route should pass dryRun=false")
		}
		if appCfg == nil || appCfg.LLM.Provider != "unknown-local-provider" || appCfg.Audio.MusicProvider != "unsupported-music" {
			t.Fatalf("factory config = %#v", appCfg)
		}
		return runner, nil
	}

	dryRun = false
	pipelineVideoBackend = ""
	pipelineOutputDir = t.TempDir()
	pipelineTargetPanels = 1
	pipelineFormat = "portrait"
	pipelineEpisodes = 2
	pipelineBatchConc = 1
	pipelineSkipLLM = false
	pipelineImageDir = ""
	pipelineI2V = false
	pipelineMaxRetries = 0
	cfg = &config.Config{
		Video: config.VideoConfig{Provider: "xai_oauth"},
		LLM: config.LLMConfig{
			Provider: "unknown-local-provider",
			Model:    "local/qwen",
			BaseURL:  "http://127.0.0.1:9999/v1",
		},
		Image: config.ImageConfig{Provider: "unsupported-image"},
		Audio: config.AudioConfig{
			VoiceProvider: "unsupported-tts",
			MusicProvider: "unsupported-music",
		},
	}

	out, err := runPipelineWithStdio(t, "story")
	if err != nil {
		t.Fatalf("runPipeline() error = %v", err)
	}
	if !factoryCalled {
		t.Fatal("xAI-native factory was not called")
	}
	if len(runner.inputs) != 2 {
		t.Fatalf("runner calls = %d, want 2", len(runner.inputs))
	}

	var summary map[string]any
	if err := json.Unmarshal([]byte(out), &summary); err != nil {
		t.Fatalf("decode stdout JSON: %v\n%s", err, out)
	}
	if summary["pipeline"] != "xai_native_batch" {
		t.Fatalf("pipeline summary = %#v", summary)
	}
	if summary["renderer"] != "hyperframes_ffmpeg" {
		t.Fatalf("renderer summary = %#v", summary)
	}
	if summary["video_backend"] != "xai_oauth" {
		t.Fatalf("video_backend summary = %#v", summary)
	}
	if summary["total_episodes"] != float64(2) || summary["failed"] != float64(0) {
		t.Fatalf("batch summary = %#v", summary)
	}
}

func TestRunPipeline_XAIOAuthRejectsLegacyModesBeforeRouting(t *testing.T) {
	tests := []struct {
		name       string
		configure  func()
		wantError  string
		wantOutput string
	}{
		{
			name: "skip llm",
			configure: func() {
				pipelineSkipLLM = true
			},
			wantError: "--skip-llm is a legacy remotion_props.json reuse mode",
		},
		{
			name: "image dir",
			configure: func() {
				pipelineImageDir = "/tmp/legacy-images"
			},
			wantError: "--image-dir is a legacy asset input",
		},
		{
			name: "i2v",
			configure: func() {
				pipelineI2V = true
			},
			wantError: "--i2v is not supported by xAI-native xai_oauth",
		},
		{
			name: "max retries",
			configure: func() {
				pipelineMaxRetries = 1
			},
			wantError: "--max-retries is a legacy AI Critic rerender mode",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			oldFactory := newXAINativePipelineRunner
			oldCfg := cfg
			oldDryRun := dryRun
			oldVideoBackend := pipelineVideoBackend
			oldOutputDir := pipelineOutputDir
			oldTargetPanels := pipelineTargetPanels
			oldFormat := pipelineFormat
			oldEpisodes := pipelineEpisodes
			oldBatchConc := pipelineBatchConc
			oldSkipLLM := pipelineSkipLLM
			oldImageDir := pipelineImageDir
			oldI2V := pipelineI2V
			oldMaxRetries := pipelineMaxRetries
			oldSeriesMemory := pipelineSeriesMemory
			oldMultiSpeaker := pipelineMultiSpeaker
			oldFaithful := pipelineFaithful
			oldVerbatim := pipelineVerbatim
			oldNarration := pipelineNarration
			t.Cleanup(func() {
				newXAINativePipelineRunner = oldFactory
				cfg = oldCfg
				dryRun = oldDryRun
				pipelineVideoBackend = oldVideoBackend
				pipelineOutputDir = oldOutputDir
				pipelineTargetPanels = oldTargetPanels
				pipelineFormat = oldFormat
				pipelineEpisodes = oldEpisodes
				pipelineBatchConc = oldBatchConc
				pipelineSkipLLM = oldSkipLLM
				pipelineImageDir = oldImageDir
				pipelineI2V = oldI2V
				pipelineMaxRetries = oldMaxRetries
				pipelineSeriesMemory = oldSeriesMemory
				pipelineMultiSpeaker = oldMultiSpeaker
				pipelineFaithful = oldFaithful
				pipelineVerbatim = oldVerbatim
				pipelineNarration = oldNarration
			})

			factoryCalled := false
			newXAINativePipelineRunner = func(_ *config.Config, _ bool) (xaiNativeRunner, error) {
				factoryCalled = true
				return &stubXAINativeRunner{}, nil
			}

			dryRun = false
			pipelineVideoBackend = ""
			pipelineOutputDir = t.TempDir()
			pipelineTargetPanels = 1
			pipelineFormat = "portrait"
			pipelineEpisodes = 1
			pipelineBatchConc = 1
			pipelineSkipLLM = false
			pipelineImageDir = ""
			pipelineI2V = false
			pipelineMaxRetries = 0
			pipelineSeriesMemory = false
			pipelineMultiSpeaker = false
			pipelineFaithful = false
			pipelineVerbatim = false
			pipelineNarration = false
			cfg = &config.Config{
				Video: config.VideoConfig{Provider: "xai_oauth"},
				LLM: config.LLMConfig{
					Provider: "unknown-local-provider",
					Model:    "local/qwen",
					BaseURL:  "http://127.0.0.1:9999/v1",
				},
				Image: config.ImageConfig{Provider: "unsupported-image"},
				Audio: config.AudioConfig{
					VoiceProvider: "unsupported-tts",
					MusicProvider: "unsupported-music",
				},
			}
			tt.configure()

			out, err := runPipelineWithStdio(t, "story")
			if err == nil {
				t.Fatal("runPipeline() error = nil, want xAI-native legacy-mode rejection")
			}
			if !strings.Contains(err.Error(), tt.wantError) {
				t.Fatalf("error = %q, want substring %q", err.Error(), tt.wantError)
			}
			if factoryCalled {
				t.Fatal("xAI-native runner factory was called after invalid xAI-native legacy mode")
			}
			if out != tt.wantOutput {
				t.Fatalf("stdout = %q, want %q", out, tt.wantOutput)
			}
		})
	}
}

func TestRunXAINativeBatchPipeline_UsesGlobalCLIOptions(t *testing.T) {
	oldFactory := newXAINativePipelineRunner
	oldOutputDir := pipelineOutputDir
	oldTargetPanels := pipelineTargetPanels
	oldFormat := pipelineFormat
	oldForceReplan := pipelineForceReplan
	oldForceRegenerate := pipelineForceRegenerate
	oldDryRun := dryRun
	oldEpisodes := pipelineEpisodes
	oldBatchConc := pipelineBatchConc
	t.Cleanup(func() {
		newXAINativePipelineRunner = oldFactory
		pipelineOutputDir = oldOutputDir
		pipelineTargetPanels = oldTargetPanels
		pipelineFormat = oldFormat
		pipelineForceReplan = oldForceReplan
		pipelineForceRegenerate = oldForceRegenerate
		dryRun = oldDryRun
		pipelineEpisodes = oldEpisodes
		pipelineBatchConc = oldBatchConc
	})

	runner := &stubXAINativeRunner{}
	newXAINativePipelineRunner = func(_ *config.Config, isDryRun bool) (xaiNativeRunner, error) {
		if !isDryRun {
			t.Fatal("factory did not receive dryRun=true")
		}
		return runner, nil
	}
	dryRun = true
	pipelineOutputDir = t.TempDir()
	pipelineTargetPanels = 2
	pipelineFormat = " \t "
	pipelineForceReplan = true
	pipelineForceRegenerate = true
	pipelineEpisodes = 2
	pipelineBatchConc = 1

	result, err := runXAINativeBatchPipeline(context.Background(), []byte("story"), &config.Config{
		Video: config.VideoConfig{
			Provider: "xai-oauth",
			Model:    "  grok-imagine-video-batch  ",
		},
	})
	if err != nil {
		t.Fatalf("runXAINativeBatchPipeline: %v", err)
	}
	if result.TotalEpisodes != 2 || result.Succeeded != 2 || result.Failed != 0 {
		t.Fatalf("batch result = %+v", result)
	}
	if len(runner.inputs) != 2 {
		t.Fatalf("runner calls = %d, want 2", len(runner.inputs))
	}
	seenDirs := map[string]bool{}
	for i := range runner.inputs {
		if string(runner.inputs[i]) != "story" {
			t.Fatalf("input %d = %q", i+1, string(runner.inputs[i]))
		}
		seenDirs[runner.opts[i].OutputDir] = true
		if runner.opts[i].TargetShots != 2 || runner.opts[i].Format != "portrait" {
			t.Fatalf("opts %d = %+v", i+1, runner.opts[i])
		}
		if runner.opts[i].VideoModel != "grok-imagine-video-batch" {
			t.Fatalf("VideoModel %d = %q, want grok-imagine-video-batch", i+1, runner.opts[i].VideoModel)
		}
	}
	for i := 1; i <= 2; i++ {
		wantDir := filepath.Join(pipelineOutputDir, "episode_00"+string(rune('0'+i)))
		if !seenDirs[wantDir] {
			t.Fatalf("runner did not receive output dir %q; seen=%v", wantDir, seenDirs)
		}
		if result.Episodes[i-1].Episode != i {
			t.Fatalf("episode index %d = %d", i, result.Episodes[i-1].Episode)
		}
	}
}

func TestRunXAINativeBatchPipeline_NilContextUsesBackground(t *testing.T) {
	oldFactory := newXAINativePipelineRunner
	oldOutputDir := pipelineOutputDir
	oldDryRun := dryRun
	oldEpisodes := pipelineEpisodes
	oldBatchConc := pipelineBatchConc
	t.Cleanup(func() {
		newXAINativePipelineRunner = oldFactory
		pipelineOutputDir = oldOutputDir
		dryRun = oldDryRun
		pipelineEpisodes = oldEpisodes
		pipelineBatchConc = oldBatchConc
	})

	runner := &stubXAINativeRunner{}
	newXAINativePipelineRunner = func(_ *config.Config, _ bool) (xaiNativeRunner, error) {
		return runner, nil
	}
	dryRun = true
	pipelineOutputDir = t.TempDir()
	pipelineEpisodes = 2
	pipelineBatchConc = 1

	_, err := runXAINativeBatchPipeline(nil, []byte("story"), &config.Config{})
	if err != nil {
		t.Fatalf("runXAINativeBatchPipeline() error = %v", err)
	}
	if len(runner.contexts) != 2 {
		t.Fatalf("runner contexts = %d, want 2", len(runner.contexts))
	}
	for i, ctx := range runner.contexts {
		if ctx == nil {
			t.Fatalf("runner context %d is nil, want background context", i+1)
		}
	}
}

func TestRunXAINativeBatchPipeline_RejectsCanceledContextBeforeFactory(t *testing.T) {
	oldFactory := newXAINativePipelineRunner
	t.Cleanup(func() {
		newXAINativePipelineRunner = oldFactory
	})

	factoryCalled := false
	newXAINativePipelineRunner = func(_ *config.Config, _ bool) (xaiNativeRunner, error) {
		factoryCalled = true
		return &stubXAINativeRunner{}, nil
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := runXAINativeBatchPipeline(ctx, []byte("story"), &config.Config{})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("runXAINativeBatchPipeline() error = %v, want context.Canceled", err)
	}
	if factoryCalled {
		t.Fatal("xAI-native runner factory was called after context cancellation")
	}
}

func TestRunXAINativeBatchPipeline_ReturnsErrorWhenAnyEpisodeFails(t *testing.T) {
	oldFactory := newXAINativePipelineRunner
	oldOutputDir := pipelineOutputDir
	oldDryRun := dryRun
	oldEpisodes := pipelineEpisodes
	oldBatchConc := pipelineBatchConc
	t.Cleanup(func() {
		newXAINativePipelineRunner = oldFactory
		pipelineOutputDir = oldOutputDir
		dryRun = oldDryRun
		pipelineEpisodes = oldEpisodes
		pipelineBatchConc = oldBatchConc
	})

	runner := &stubXAINativeRunner{failOnEpisode: map[int]error{2: errors.New("episode failed")}}
	newXAINativePipelineRunner = func(_ *config.Config, _ bool) (xaiNativeRunner, error) {
		return runner, nil
	}
	dryRun = true
	pipelineOutputDir = t.TempDir()
	pipelineEpisodes = 3
	pipelineBatchConc = 2

	result, err := runXAINativeBatchPipeline(context.Background(), []byte("story"), nil)
	if err == nil {
		t.Fatal("runXAINativeBatchPipeline() error = nil, want failed batch error")
	}
	if !strings.Contains(err.Error(), "xAI-native batch failed") || !strings.Contains(err.Error(), "1/3") {
		t.Fatalf("error = %q", err.Error())
	}
	if result == nil {
		t.Fatal("result = nil, want partial batch result")
	}
	if result.Succeeded != 2 || result.Failed != 1 {
		t.Fatalf("batch tally = %+v", result)
	}
	if result.Episodes[1].Error != "episode failed" {
		t.Fatalf("episode 2 error = %q", result.Episodes[1].Error)
	}
}

func TestXAINativeBatchFailureErrorRejectsNilResult(t *testing.T) {
	err := xaiNativeBatchFailureError(nil)
	if err == nil {
		t.Fatal("xaiNativeBatchFailureError(nil) = nil, want nil result error")
	}
	if !strings.Contains(err.Error(), "xai-native batch result is nil") {
		t.Fatalf("xaiNativeBatchFailureError(nil) = %v, want nil result error", err)
	}
}

func TestRunXAINativePipeline_PropagatesFactoryError(t *testing.T) {
	oldFactory := newXAINativePipelineRunner
	t.Cleanup(func() {
		newXAINativePipelineRunner = oldFactory
	})

	newXAINativePipelineRunner = func(_ *config.Config, _ bool) (xaiNativeRunner, error) {
		return nil, errors.New("factory failed")
	}

	_, err := runXAINativePipeline(context.Background(), []byte("story"), nil)
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestXAINativeSummaryIncludesArtifactPaths(t *testing.T) {
	outputDir := t.TempDir()
	result := &xaipipeline.Result{
		Manifest: xaipipeline.Manifest{
			ProjectID:  "summary-test",
			StoryHash:  "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
			VideoModel: "grok-imagine-video-summary",
			Shots: []xaipipeline.Shot{
				{Index: 1},
				{Index: 2},
			},
		},
		OutputDir:          outputDir,
		OutputVideo:        filepath.Join(outputDir, "output_xai.mp4"),
		ManifestPath:       filepath.Join(outputDir, "xai_manifest.json"),
		RunMetadataPath:    filepath.Join(outputDir, "xai_run_metadata.json"),
		RenderMetadataPath: filepath.Join(outputDir, "render_metadata.json"),
		PreviewFramePath:   filepath.Join(outputDir, "preview_frame.jpg"),
	}

	summary := xaiNativeSummary(result, "xai_oauth", true)

	if summary["project_id"] != "summary-test" {
		t.Fatalf("project_id = %v", summary["project_id"])
	}
	if summary["shots"] != 2 {
		t.Fatalf("shots = %v", summary["shots"])
	}
	if summary["dry_run"] != true {
		t.Fatalf("dry_run = %v", summary["dry_run"])
	}
	for key, want := range map[string]string{
		"output_dir":      outputDir,
		"output_video":    filepath.Join(outputDir, "output_xai.mp4"),
		"xai_manifest":    filepath.Join(outputDir, "xai_manifest.json"),
		"run_metadata":    filepath.Join(outputDir, "xai_run_metadata.json"),
		"render_metadata": filepath.Join(outputDir, "render_metadata.json"),
		"preview_frame":   filepath.Join(outputDir, "preview_frame.jpg"),
		"story_hash":      "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
		"video_model":     "grok-imagine-video-summary",
	} {
		if summary[key] != want {
			t.Fatalf("%s = %v, want %q", key, summary[key], want)
		}
	}
	if _, ok := summary["remotion_props"]; ok {
		t.Fatal("xAI-native summary should not expose remotion_props")
	}
}

func TestXAINativeBatchSummaryIncludesIdentity(t *testing.T) {
	outputDir := t.TempDir()
	result := &xaipipeline.BatchResult{
		TotalEpisodes: 2,
		Succeeded:     2,
		OutputDir:     outputDir,
		Episodes: []xaipipeline.BatchEpisodeResult{
			{
				Episode: 1,
				Result: &xaipipeline.Result{
					Manifest: xaipipeline.Manifest{
						StoryHash:  "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
						VideoModel: "grok-imagine-video-batch",
					},
				},
			},
			{
				Episode: 2,
				Result: &xaipipeline.Result{
					Manifest: xaipipeline.Manifest{
						StoryHash:  "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
						VideoModel: "grok-imagine-video-batch",
					},
				},
			},
		},
	}

	summary := xaiNativeBatchSummary(result, "xai_oauth", true)

	if summary["video_model"] != "grok-imagine-video-batch" {
		t.Fatalf("video_model = %v, want grok-imagine-video-batch", summary["video_model"])
	}
	if summary["story_hash"] != "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef" {
		t.Fatalf("story_hash = %v, want sha256 story hash", summary["story_hash"])
	}
}

func TestNewDefaultXAINativePipelineRunner_IgnoresLegacyLLMProvider(t *testing.T) {
	runner, err := newDefaultXAINativePipelineRunner(&config.Config{
		LLM: config.LLMConfig{
			Provider: "unknown-local-provider",
			Model:    "local/qwen",
			BaseURL:  "http://127.0.0.1:9999/v1",
		},
		XAI: config.XAIConfig{
			Model:     "grok-4.3",
			BaseURL:   "https://api.x.ai/v1",
			TokenPath: filepath.Join(t.TempDir(), "missing-hermes-auth.json"),
		},
	}, false)
	if err != nil {
		t.Fatalf("newDefaultXAINativePipelineRunner should force xai-oauth planner: %v", err)
	}
	if runner == nil {
		t.Fatal("runner is nil")
	}
}

func TestXAINativePlannerConfig_DoesNotInheritLegacyLLMModelOrBaseURL(t *testing.T) {
	got := xaiNativePlannerConfig(&config.Config{
		LLM: config.LLMConfig{
			Provider: "openai",
			Model:    "local/qwen",
			BaseURL:  "http://127.0.0.1:9999/v1",
		},
		XAI: config.XAIConfig{
			TokenPath: "/tmp/hermes-auth.json",
		},
	})

	if got.LLM.Provider != "xai-oauth" {
		t.Fatalf("LLM.Provider = %q, want xai-oauth", got.LLM.Provider)
	}
	if got.LLM.Model != "" {
		t.Fatalf("LLM.Model should be cleared, got %q", got.LLM.Model)
	}
	if got.LLM.BaseURL != "" {
		t.Fatalf("LLM.BaseURL should be cleared, got %q", got.LLM.BaseURL)
	}
	if got.XAI.Model != "grok-4.3" {
		t.Fatalf("XAI.Model = %q, want grok-4.3", got.XAI.Model)
	}
	if got.XAI.BaseURL != "https://api.x.ai/v1" {
		t.Fatalf("XAI.BaseURL = %q, want https://api.x.ai/v1", got.XAI.BaseURL)
	}
	if got.XAI.TokenPath != "/tmp/hermes-auth.json" {
		t.Fatalf("XAI.TokenPath = %q", got.XAI.TokenPath)
	}
}

func TestXAINativePlannerConfig_NormalizesXAIModelAndBaseURL(t *testing.T) {
	t.Run("blank xai fields fall back without inheriting legacy llm", func(t *testing.T) {
		got := xaiNativePlannerConfig(&config.Config{
			LLM: config.LLMConfig{
				Provider: "openai",
				Model:    "local/qwen",
				BaseURL:  "http://127.0.0.1:9999/v1",
			},
			XAI: config.XAIConfig{
				Model:   " \t ",
				BaseURL: "\n ",
			},
		})

		if got.XAI.Model != "grok-4.3" {
			t.Fatalf("XAI.Model = %q, want grok-4.3", got.XAI.Model)
		}
		if got.XAI.BaseURL != "https://api.x.ai/v1" {
			t.Fatalf("XAI.BaseURL = %q, want https://api.x.ai/v1", got.XAI.BaseURL)
		}
		if got.LLM.Model != "" || got.LLM.BaseURL != "" {
			t.Fatalf("legacy LLM fields leaked into xAI planner config: %+v", got.LLM)
		}
	})

	t.Run("custom xai fields are trimmed", func(t *testing.T) {
		got := xaiNativePlannerConfig(&config.Config{
			XAI: config.XAIConfig{
				Model:   "  grok-4.3-fast  ",
				BaseURL: "  https://api.x.ai/custom  ",
			},
		})

		if got.XAI.Model != "grok-4.3-fast" {
			t.Fatalf("XAI.Model = %q, want trimmed model", got.XAI.Model)
		}
		if got.XAI.BaseURL != "https://api.x.ai/custom" {
			t.Fatalf("XAI.BaseURL = %q, want trimmed base URL", got.XAI.BaseURL)
		}
	})
}

func TestXAINativeVideoModel_OnlyUsesVideoModelForXAIOAuthProvider(t *testing.T) {
	if got := xaiNativeVideoModel(nil); got != "grok-imagine-video" {
		t.Fatalf("nil config model = %q", got)
	}

	if got := xaiNativeVideoModel(&config.Config{
		Video: config.VideoConfig{
			Provider: "nova_reel",
			Model:    "amazon.nova-reel-v1:1",
		},
	}); got != "grok-imagine-video" {
		t.Fatalf("legacy video model leaked into xAI pipeline: %q", got)
	}

	if got := xaiNativeVideoModel(&config.Config{
		Video: config.VideoConfig{
			Provider: "xai_oauth",
			Model:    "grok-imagine-video-custom",
		},
	}); got != "grok-imagine-video-custom" {
		t.Fatalf("xAI video model = %q", got)
	}

	if got := xaiNativeVideoModel(&config.Config{
		Video: config.VideoConfig{
			Provider: "xai-oauth",
			Model:    "grok-imagine-video-alias",
		},
	}); got != "grok-imagine-video-alias" {
		t.Fatalf("xAI video model with provider alias = %q", got)
	}

	if got := xaiNativeVideoModel(&config.Config{
		Video: config.VideoConfig{
			Provider: " xai-oauth ",
			Model:    "  grok-imagine-video-trimmed  ",
		},
	}); got != "grok-imagine-video-trimmed" {
		t.Fatalf("trimmed xAI video model = %q", got)
	}

	if got := xaiNativeVideoModel(&config.Config{
		Video: config.VideoConfig{
			Provider: "xai_oauth",
			Model:    "   ",
		},
	}); got != "grok-imagine-video" {
		t.Fatalf("blank xAI video model = %q", got)
	}
}

type stubXAIVideoOptionsClient struct {
	imageURL  string
	prompt    string
	options   video.GenerateVideoOptions
	videoData []byte
	calls     int
}

func (s *stubXAIVideoOptionsClient) GenerateVideoWithOptions(_ context.Context, imageURL string, prompt string, options video.GenerateVideoOptions) ([]byte, error) {
	s.calls++
	s.imageURL = imageURL
	s.prompt = prompt
	s.options = options
	data := s.videoData
	if data == nil {
		data = []byte("mp4")
	}
	return data, nil
}

type stubXAIVideoResultOptionsClient struct {
	stubXAIVideoOptionsClient
	data      []byte
	requestID string
	status    string
}

func (s *stubXAIVideoResultOptionsClient) GenerateVideoWithOptionsResult(_ context.Context, imageURL string, prompt string, options video.GenerateVideoOptions) (video.GenerateVideoResult, error) {
	s.calls++
	s.imageURL = imageURL
	s.prompt = prompt
	s.options = options
	data := s.data
	if data == nil {
		data = []byte("mp4")
	}
	return video.GenerateVideoResult{
		Data:      data,
		RequestID: s.requestID,
		Status:    s.status,
	}, nil
}

func TestXAIOAuthVideoAdapter_MapsPipelineVideoOptions(t *testing.T) {
	client := &stubXAIVideoResultOptionsClient{}
	adapter := xaiOAuthVideoAdapter{client: client}

	got, err := adapter.GenerateVideo(context.Background(), "", "xai shot", xaipipeline.VideoOptions{
		DurationSec: 6.5,
		AspectRatio: "16:9",
		Resolution:  "1080p",
	})
	if err != nil {
		t.Fatalf("GenerateVideo: %v", err)
	}
	if string(got) != "mp4" {
		t.Fatalf("GenerateVideo = %q", string(got))
	}
	if client.prompt != "xai shot" {
		t.Fatalf("prompt = %q", client.prompt)
	}
	if client.options.DurationSec != 6.5 {
		t.Fatalf("DurationSec = %.1f", client.options.DurationSec)
	}
	if client.options.AspectRatio != "16:9" {
		t.Fatalf("AspectRatio = %q", client.options.AspectRatio)
	}
	if client.options.Resolution != "1080p" {
		t.Fatalf("Resolution = %q", client.options.Resolution)
	}
}

func TestXAIOAuthVideoAdapter_MapsProviderMetadata(t *testing.T) {
	client := &stubXAIVideoResultOptionsClient{
		requestID: "req_123",
		status:    "done",
	}
	adapter := xaiOAuthVideoAdapter{client: client}

	got, err := adapter.GenerateVideoResult(context.Background(), "", "xai shot", xaipipeline.VideoOptions{
		DurationSec: 6.5,
		AspectRatio: "16:9",
		Resolution:  "1080p",
	})
	if err != nil {
		t.Fatalf("GenerateVideoResult: %v", err)
	}
	if string(got.Data) != "mp4" {
		t.Fatalf("Data = %q", string(got.Data))
	}
	if got.RequestID != "req_123" {
		t.Fatalf("RequestID = %q", got.RequestID)
	}
	if got.Status != "done" {
		t.Fatalf("Status = %q", got.Status)
	}
	if client.prompt != "xai shot" {
		t.Fatalf("prompt = %q", client.prompt)
	}
	if client.options.DurationSec != 6.5 {
		t.Fatalf("DurationSec = %.1f", client.options.DurationSec)
	}
}

func TestXAIOAuthVideoAdapter_RejectsEmptyProviderVideoData(t *testing.T) {
	client := &stubXAIVideoResultOptionsClient{
		data:      []byte{},
		requestID: "req_123",
		status:    "done",
	}
	adapter := xaiOAuthVideoAdapter{client: client}

	got, err := adapter.GenerateVideoResult(context.Background(), "", "xai shot", xaipipeline.VideoOptions{})
	if err == nil {
		t.Fatal("GenerateVideoResult() error = nil, want empty video data error")
	}
	if len(got.Data) != 0 || got.RequestID != "" || got.Status != "" {
		t.Fatalf("GenerateVideoResult() = %+v, want zero result on empty video data", got)
	}
	if !strings.Contains(err.Error(), "empty video") {
		t.Fatalf("GenerateVideoResult() error = %v, want empty video error", err)
	}
}

func TestXAIOAuthVideoAdapter_RejectsEmptyByteOnlyProviderVideoData(t *testing.T) {
	client := &stubXAIVideoResultOptionsClient{
		stubXAIVideoOptionsClient: stubXAIVideoOptionsClient{videoData: []byte{}},
		requestID:                 "req_123",
		status:                    "done",
	}
	adapter := xaiOAuthVideoAdapter{client: client}

	got, err := adapter.GenerateVideo(context.Background(), "", "xai shot", xaipipeline.VideoOptions{})
	if err == nil {
		t.Fatal("GenerateVideo() error = nil, want empty video data error")
	}
	if got != nil {
		t.Fatalf("GenerateVideo() = %q, want nil bytes", string(got))
	}
	if !strings.Contains(err.Error(), "empty video") {
		t.Fatalf("GenerateVideo() error = %v, want empty video error", err)
	}
}

func TestXAIOAuthVideoAdapter_RejectsNilClient(t *testing.T) {
	adapter := xaiOAuthVideoAdapter{}

	t.Run("GenerateVideo", func(t *testing.T) {
		defer func() {
			if recovered := recover(); recovered != nil {
				t.Fatalf("GenerateVideo() panicked with nil client: %v", recovered)
			}
		}()

		got, err := adapter.GenerateVideo(context.Background(), "", "xai shot", xaipipeline.VideoOptions{})
		if err == nil {
			t.Fatal("GenerateVideo() error = nil, want nil client error")
		}
		if got != nil {
			t.Fatalf("GenerateVideo() = %q, want nil bytes", string(got))
		}
		if !strings.Contains(err.Error(), "client is nil") {
			t.Fatalf("GenerateVideo() error = %v, want nil client error", err)
		}
	})

	t.Run("GenerateVideoResult", func(t *testing.T) {
		defer func() {
			if recovered := recover(); recovered != nil {
				t.Fatalf("GenerateVideoResult() panicked with nil client: %v", recovered)
			}
		}()

		got, err := adapter.GenerateVideoResult(context.Background(), "", "xai shot", xaipipeline.VideoOptions{})
		if err == nil {
			t.Fatal("GenerateVideoResult() error = nil, want nil client error")
		}
		if len(got.Data) != 0 || got.RequestID != "" || got.Status != "" {
			t.Fatalf("GenerateVideoResult() = %+v, want zero result", got)
		}
		if !strings.Contains(err.Error(), "client is nil") {
			t.Fatalf("GenerateVideoResult() error = %v, want nil client error", err)
		}
	})
}
