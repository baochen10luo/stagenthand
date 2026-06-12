package cmd

import (
	"strings"
	"testing"

	"github.com/baochen10luo/stagenthand/config"
)

func TestResolveVideoBackend_DefaultsToXAIOAuth(t *testing.T) {
	oldFlag := pipelineVideoBackend
	oldCfg := cfg
	t.Cleanup(func() {
		pipelineVideoBackend = oldFlag
		cfg = oldCfg
	})

	pipelineVideoBackend = ""
	cfg = nil
	if got := resolveVideoBackend(); got != "xai_oauth" {
		t.Fatalf("resolveVideoBackend() = %q, want xai_oauth", got)
	}

	pipelineVideoBackend = " \t "
	cfg = &config.Config{Video: config.VideoConfig{Provider: " remotion "}}
	if got := resolveVideoBackend(); got != "remotion" {
		t.Fatalf("resolveVideoBackend() blank flag should use config = %q, want remotion", got)
	}

	pipelineVideoBackend = ""
	cfg = &config.Config{Video: config.VideoConfig{Provider: " \n "}}
	if got := resolveVideoBackend(); got != "xai_oauth" {
		t.Fatalf("resolveVideoBackend() blank config provider = %q, want xai_oauth", got)
	}
}

func TestResolveVideoBackend_UsesConfigAndFlag(t *testing.T) {
	oldFlag := pipelineVideoBackend
	oldCfg := cfg
	t.Cleanup(func() {
		pipelineVideoBackend = oldFlag
		cfg = oldCfg
	})

	pipelineVideoBackend = ""
	cfg = &config.Config{Video: config.VideoConfig{Provider: "remotion"}}
	if got := resolveVideoBackend(); got != "remotion" {
		t.Fatalf("resolveVideoBackend() = %q, want remotion", got)
	}

	pipelineVideoBackend = "xai_oauth"
	if got := resolveVideoBackend(); got != "xai_oauth" {
		t.Fatalf("resolveVideoBackend() with flag = %q, want xai_oauth", got)
	}
}

func TestResolveVideoBackend_NormalizesXAIOAuthAlias(t *testing.T) {
	oldFlag := pipelineVideoBackend
	oldCfg := cfg
	t.Cleanup(func() {
		pipelineVideoBackend = oldFlag
		cfg = oldCfg
	})

	pipelineVideoBackend = ""
	cfg = &config.Config{Video: config.VideoConfig{Provider: " xai-oauth "}}
	if got := resolveVideoBackend(); got != "xai_oauth" {
		t.Fatalf("resolveVideoBackend() config alias = %q, want xai_oauth", got)
	}

	pipelineVideoBackend = "xai-oauth"
	if got := resolveVideoBackend(); got != "xai_oauth" {
		t.Fatalf("resolveVideoBackend() flag alias = %q, want xai_oauth", got)
	}

	pipelineVideoBackend = " XAI-OAUTH "
	if got := resolveVideoBackend(); got != "xai_oauth" {
		t.Fatalf("resolveVideoBackend() uppercase flag alias = %q, want xai_oauth", got)
	}

	pipelineVideoBackend = ""
	cfg = &config.Config{Video: config.VideoConfig{Provider: " XAI_OAUTH "}}
	if got := resolveVideoBackend(); got != "xai_oauth" {
		t.Fatalf("resolveVideoBackend() uppercase config provider = %q, want xai_oauth", got)
	}
}

func TestShouldRenderStatic_SkipsXAIOAuth(t *testing.T) {
	if shouldRenderStatic("xai_oauth") {
		t.Fatal("xai_oauth should skip static Remotion render")
	}
	if shouldRenderStatic("xai-oauth") {
		t.Fatal("xai-oauth alias should skip static Remotion render")
	}
	if !shouldRenderStatic("remotion") {
		t.Fatal("remotion should render static video")
	}
}

func TestValidatePipelineVideoModeRejectsXAIOAuthLegacyInputs(t *testing.T) {
	tests := []struct {
		name     string
		backend  string
		imageDir string
		i2v      bool
		want     string
	}{
		{
			name:    "i2v",
			backend: "xai_oauth",
			i2v:     true,
			want:    "not supported by xAI-native",
		},
		{
			name:    "i2v alias",
			backend: "xai-oauth",
			i2v:     true,
			want:    "not supported by xAI-native",
		},
		{
			name:     "image-dir",
			backend:  "xai_oauth",
			imageDir: "/tmp/images",
			want:     "legacy asset input",
		},
		{
			name:     "image-dir alias",
			backend:  "xai-oauth",
			imageDir: "/tmp/images",
			want:     "legacy asset input",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validatePipelineVideoMode(pipelineVideoModeOptions{
				Backend:  tt.backend,
				ImageDir: tt.imageDir,
				I2V:      tt.i2v,
				Episodes: 1,
			})
			if err == nil {
				t.Fatal("expected error")
			}
			if !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("error = %q, want substring %q", err.Error(), tt.want)
			}
		})
	}
}

func TestValidatePipelineVideoModeAllowsNativeDefaultAndLegacyGrokI2V(t *testing.T) {
	if err := validatePipelineVideoMode(pipelineVideoModeOptions{
		Backend:  "xai_oauth",
		Episodes: 1,
	}); err != nil {
		t.Fatalf("xai native default should be valid: %v", err)
	}
	if err := validatePipelineVideoMode(pipelineVideoModeOptions{
		Backend:  "xai-oauth",
		Episodes: 1,
	}); err != nil {
		t.Fatalf("xai native alias default should be valid: %v", err)
	}
	if err := validatePipelineVideoMode(pipelineVideoModeOptions{
		Backend:          "xai_oauth",
		Episodes:         2,
		BatchConcurrency: 2,
	}); err != nil {
		t.Fatalf("xai native batch should be valid: %v", err)
	}
	if err := validatePipelineVideoMode(pipelineVideoModeOptions{
		Backend:          "xai-oauth",
		Episodes:         2,
		BatchConcurrency: 2,
	}); err != nil {
		t.Fatalf("xai native alias batch should be valid: %v", err)
	}
	if err := validatePipelineVideoMode(pipelineVideoModeOptions{
		Backend:  "grok_browser",
		ImageDir: "/tmp/images",
		I2V:      true,
		Episodes: 1,
	}); err != nil {
		t.Fatalf("legacy grok i2v should remain valid: %v", err)
	}
	for _, backend := range []string{"remotion", "nova_reel", "hyperframes", " REMOTION "} {
		if err := validatePipelineVideoMode(pipelineVideoModeOptions{
			Backend:  backend,
			Episodes: 1,
		}); err != nil {
			t.Fatalf("explicit legacy backend %q should remain valid: %v", backend, err)
		}
	}
}

func TestValidatePipelineVideoModeRejectsUnknownBackend(t *testing.T) {
	err := validatePipelineVideoMode(pipelineVideoModeOptions{
		Backend:  "xai-oauthh",
		Episodes: 1,
	})
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "unsupported --video-backend") {
		t.Fatalf("error = %q, want unsupported backend issue", err.Error())
	}
}

func TestValidatePipelineVideoModeRejectsInvalidBatchConcurrency(t *testing.T) {
	tests := []struct {
		name string
		opts pipelineVideoModeOptions
	}{
		{
			name: "xai zero",
			opts: pipelineVideoModeOptions{Backend: "xai_oauth", Episodes: 2, BatchConcurrency: 0},
		},
		{
			name: "xai negative",
			opts: pipelineVideoModeOptions{Backend: "xai_oauth", Episodes: 2, BatchConcurrency: -1},
		},
		{
			name: "legacy zero",
			opts: pipelineVideoModeOptions{Backend: "remotion", Episodes: 2, BatchConcurrency: 0},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validatePipelineVideoMode(tt.opts)
			if err == nil {
				t.Fatal("expected error")
			}
			if !strings.Contains(err.Error(), "--batch-concurrency must be greater than zero") {
				t.Fatalf("error = %q, want batch concurrency issue", err.Error())
			}
		})
	}

	if err := validatePipelineVideoMode(pipelineVideoModeOptions{
		Backend:          "xai_oauth",
		Episodes:         1,
		BatchConcurrency: 0,
	}); err != nil {
		t.Fatalf("single episode should ignore batch concurrency: %v", err)
	}
}

func TestValidatePipelineVideoModeRejectsXAIOnlyForceFlagsForLegacyBackends(t *testing.T) {
	tests := []struct {
		name string
		opts pipelineVideoModeOptions
		want string
	}{
		{
			name: "force replan remotion",
			opts: pipelineVideoModeOptions{Backend: "remotion", Episodes: 1, ForceReplan: true},
			want: "--force-replan is supported only by xAI-native xai_oauth",
		},
		{
			name: "force regenerate hyperframes",
			opts: pipelineVideoModeOptions{Backend: "hyperframes", Episodes: 1, ForceRegenerate: true},
			want: "--force-regenerate is supported only by xAI-native xai_oauth",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validatePipelineVideoMode(tt.opts)
			if err == nil {
				t.Fatal("expected error")
			}
			if !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("error = %q, want substring %q", err.Error(), tt.want)
			}
		})
	}

	if err := validatePipelineVideoMode(pipelineVideoModeOptions{
		Backend:         "xai_oauth",
		Episodes:        1,
		ForceReplan:     true,
		ForceRegenerate: true,
	}); err != nil {
		t.Fatalf("xAI-native force flags should remain valid: %v", err)
	}
}

func TestValidatePipelineVideoModeRejectsXAIOAuthUnsupportedFormat(t *testing.T) {
	err := validatePipelineVideoMode(pipelineVideoModeOptions{
		Backend:  "xai_oauth",
		Format:   " LANDSCAPE ",
		Episodes: 1,
	})
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "supports portrait only") {
		t.Fatalf("error = %q, want portrait-only issue", err.Error())
	}

	if err := validatePipelineVideoMode(pipelineVideoModeOptions{
		Backend:  "remotion",
		Format:   "landscape",
		Episodes: 1,
	}); err != nil {
		t.Fatalf("legacy remotion landscape should remain valid: %v", err)
	}
}

func TestValidatePipelineVideoModeRejectsI2VForNonI2VBackends(t *testing.T) {
	err := validatePipelineVideoMode(pipelineVideoModeOptions{
		Backend:  "remotion",
		ImageDir: "/tmp/images",
		I2V:      true,
		Episodes: 1,
	})
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "requires legacy deprecated --video-backend grok_browser") {
		t.Fatalf("error = %q", err.Error())
	}
}

func TestValidatePipelineVideoModeRejectsXAIOAuthLegacyExecutionModes(t *testing.T) {
	tests := []struct {
		name string
		opts pipelineVideoModeOptions
		want string
	}{
		{
			name: "skip-llm",
			opts: pipelineVideoModeOptions{Backend: "xai_oauth", SkipLLM: true, Episodes: 1},
			want: "--skip-llm is a legacy remotion_props.json reuse mode",
		},
		{
			name: "skip-llm alias",
			opts: pipelineVideoModeOptions{Backend: "xai-oauth", SkipLLM: true, Episodes: 1},
			want: "--skip-llm is a legacy remotion_props.json reuse mode",
		},
		{
			name: "series-memory",
			opts: pipelineVideoModeOptions{Backend: "xai_oauth", SeriesMemory: true, Episodes: 2, BatchConcurrency: 2},
			want: "--series-memory is a legacy series-continuity mode",
		},
		{
			name: "series-memory alias",
			opts: pipelineVideoModeOptions{Backend: "xai-oauth", SeriesMemory: true, Episodes: 2, BatchConcurrency: 2},
			want: "--series-memory is a legacy series-continuity mode",
		},
		{
			name: "multi-speaker",
			opts: pipelineVideoModeOptions{Backend: "xai_oauth", MultiSpeaker: true, Episodes: 1},
			want: "--multi-speaker is a legacy TTS mode",
		},
		{
			name: "faithful",
			opts: pipelineVideoModeOptions{Backend: "xai_oauth", Faithful: true, Episodes: 1},
			want: "--faithful is a legacy story transformation mode",
		},
		{
			name: "verbatim",
			opts: pipelineVideoModeOptions{Backend: "xai_oauth", Verbatim: true, Episodes: 1},
			want: "--verbatim is a legacy story transformation mode",
		},
		{
			name: "narration",
			opts: pipelineVideoModeOptions{Backend: "xai_oauth", Narration: true, Episodes: 1},
			want: "--narration is a legacy story transformation mode",
		},
		{
			name: "max-retries",
			opts: pipelineVideoModeOptions{Backend: "xai_oauth", MaxRetries: 1, Episodes: 1},
			want: "--max-retries is a legacy AI Critic rerender mode",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validatePipelineVideoMode(tt.opts)
			if err == nil {
				t.Fatal("expected error")
			}
			if !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("error = %q, want substring %q", err.Error(), tt.want)
			}
		})
	}

	if err := validatePipelineVideoMode(pipelineVideoModeOptions{
		Backend:          "remotion",
		SeriesMemory:     true,
		Episodes:         2,
		BatchConcurrency: 2,
	}); err != nil {
		t.Fatalf("legacy series memory should remain valid for explicit legacy backends: %v", err)
	}

	legacyModeCases := []pipelineVideoModeOptions{
		{Backend: "remotion", MultiSpeaker: true, Episodes: 1},
		{Backend: "remotion", Faithful: true, Episodes: 1},
		{Backend: "remotion", Verbatim: true, Episodes: 1},
		{Backend: "remotion", Narration: true, Episodes: 1},
		{Backend: "remotion", MaxRetries: 1, Episodes: 1},
	}
	for _, opts := range legacyModeCases {
		if err := validatePipelineVideoMode(opts); err != nil {
			t.Fatalf("legacy mode %+v should remain valid for explicit legacy backends: %v", opts, err)
		}
	}
}

func TestValidatePipelineVideoModeRejectsInvalidCounts(t *testing.T) {
	tests := []struct {
		name string
		opts pipelineVideoModeOptions
		want string
	}{
		{
			name: "zero episodes",
			opts: pipelineVideoModeOptions{Backend: "xai_oauth", Episodes: 0},
			want: "--episodes must be greater than zero",
		},
		{
			name: "negative episodes",
			opts: pipelineVideoModeOptions{Backend: "xai_oauth", Episodes: -1},
			want: "--episodes must be greater than zero",
		},
		{
			name: "negative panels",
			opts: pipelineVideoModeOptions{Backend: "xai_oauth", Episodes: 1, TargetPanels: -1},
			want: "--panels must be zero or greater",
		},
		{
			name: "negative panels alias",
			opts: pipelineVideoModeOptions{Backend: "xai-oauth", Episodes: 1, TargetPanels: -1},
			want: "--panels must be zero or greater",
		},
		{
			name: "negative max retries xai",
			opts: pipelineVideoModeOptions{Backend: "xai_oauth", Episodes: 1, MaxRetries: -1},
			want: "--max-retries must be zero or greater",
		},
		{
			name: "negative max retries legacy",
			opts: pipelineVideoModeOptions{Backend: "remotion", Episodes: 1, MaxRetries: -1},
			want: "--max-retries must be zero or greater",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validatePipelineVideoMode(tt.opts)
			if err == nil {
				t.Fatal("expected error")
			}
			if !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("error = %q, want substring %q", err.Error(), tt.want)
			}
		})
	}

	if err := validatePipelineVideoMode(pipelineVideoModeOptions{
		Backend:    "xai_oauth",
		Episodes:   1,
		MaxRetries: 0,
	}); err != nil {
		t.Fatalf("zero max retries should remain valid for xAI-native default: %v", err)
	}
}

func TestPipelineVideoBackendHelpMarksGrokBrowserDeprecated(t *testing.T) {
	flag := pipelineCmd.Flags().Lookup("video-backend")
	if flag == nil {
		t.Fatal("video-backend flag not registered")
	}
	if !strings.Contains(flag.Usage, "grok_browser (deprecated)") {
		t.Fatalf("video-backend help should mark grok_browser deprecated, got %q", flag.Usage)
	}
	if !strings.Contains(flag.Usage, "xai-oauth alias") {
		t.Fatalf("video-backend help should document xai-oauth alias, got %q", flag.Usage)
	}
}

func TestPipelinePanelsHelpDocumentsXAINativeShotMapping(t *testing.T) {
	flag := pipelineCmd.Flags().Lookup("panels")
	if flag == nil {
		t.Fatal("panels flag not registered")
	}
	if !strings.Contains(flag.Usage, "xAI-native") || !strings.Contains(flag.Usage, "one xAI video shot per panel") {
		t.Fatalf("panels help should document xAI-native one-shot mapping, got %q", flag.Usage)
	}
}

func TestPipelineI2VHelpDocumentsLegacyGrokOnly(t *testing.T) {
	flag := pipelineCmd.Flags().Lookup("i2v")
	if flag == nil {
		t.Fatal("i2v flag not registered")
	}
	if !strings.Contains(flag.Usage, "legacy grok_browser") {
		t.Fatalf("i2v help should document legacy grok_browser mode, got %q", flag.Usage)
	}
}

func TestPipelineLegacyModeHelpDocumentsXAIRestrictions(t *testing.T) {
	flag := pipelineCmd.Flags().Lookup("skip-llm")
	if flag == nil {
		t.Fatal("skip-llm flag not registered")
	}
	if !strings.Contains(flag.Usage, "legacy") || !strings.Contains(flag.Usage, "not xAI-native") {
		t.Fatalf("skip-llm help should document legacy non-xAI-native mode, got %q", flag.Usage)
	}
}

func TestPipelineSeriesMemoryHelpDocumentsXAIRestrictions(t *testing.T) {
	flag := pipelineCmd.Flags().Lookup("series-memory")
	if flag == nil {
		t.Fatal("series-memory flag not registered")
	}
	if !strings.Contains(flag.Usage, "legacy") || !strings.Contains(flag.Usage, "not xAI-native") {
		t.Fatalf("series-memory help should document legacy non-xAI-native mode, got %q", flag.Usage)
	}
}

func TestPipelineLegacyTextAndAudioModeHelpDocumentsXAIRestrictions(t *testing.T) {
	for _, name := range []string{"multi-speaker", "faithful", "verbatim", "narration"} {
		flag := pipelineCmd.Flags().Lookup(name)
		if flag == nil {
			t.Fatalf("%s flag not registered", name)
		}
		if !strings.Contains(flag.Usage, "legacy") || !strings.Contains(flag.Usage, "not xAI-native") {
			t.Fatalf("%s help should document legacy non-xAI-native mode, got %q", name, flag.Usage)
		}
	}
}

func TestPipelineMaxRetriesHelpDocumentsXAIRestrictions(t *testing.T) {
	flag := pipelineCmd.Flags().Lookup("max-retries")
	if flag == nil {
		t.Fatal("max-retries flag not registered")
	}
	if !strings.Contains(flag.Usage, "legacy") || !strings.Contains(flag.Usage, "not xAI-native") {
		t.Fatalf("max-retries help should document legacy non-xAI-native mode, got %q", flag.Usage)
	}
}

func TestPipelineEpisodesHelpDocumentsXAINativeBatch(t *testing.T) {
	flag := pipelineCmd.Flags().Lookup("episodes")
	if flag == nil {
		t.Fatal("episodes flag not registered")
	}
	if !strings.Contains(flag.Usage, "xAI-native") || !strings.Contains(flag.Usage, "episode_###") {
		t.Fatalf("episodes help should document xAI-native batch output dirs, got %q", flag.Usage)
	}
}
