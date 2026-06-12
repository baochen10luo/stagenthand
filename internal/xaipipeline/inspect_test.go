package xaipipeline_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/baochen10luo/stagenthand/internal/xaipipeline"
)

const inspectStoryHash = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"

func TestInspectOutputDir_SummarizesXAIArtifacts(t *testing.T) {
	t.Parallel()

	outputDir := t.TempDir()
	writeInspectJSON(t, filepath.Join(outputDir, "xai_manifest.json"), xaipipeline.Manifest{
		ProjectID: "inspect-story",
		StoryHash: "story-hash",
		Format:    "portrait",
		FPS:       24,
		Width:     720,
		Height:    1280,
		Shots: []xaipipeline.Shot{
			{
				Index:        1,
				Prompt:       "wide cinematic xai shot",
				PromptHash:   "prompt-hash",
				XAIRequestID: "req_123",
				XAIStatus:    "done",
				DurationSec:  8,
				VideoPath:    "shots/shot_001.mp4",
			},
		},
	})
	writeInspectJSON(t, filepath.Join(outputDir, "xai_run_metadata.json"), xaipipeline.RunMetadata{
		Planned:        true,
		ManifestReused: false,
		GeneratedShots: []int{1},
		ShotDecisions: []xaipipeline.ShotDecision{
			{
				Index:        1,
				Decision:     "generated",
				VideoPath:    "shots/shot_001.mp4",
				PromptHash:   "prompt-hash",
				XAIRequestID: "req_123",
				XAIStatus:    "done",
			},
		},
	})
	writeInspectJSON(t, filepath.Join(outputDir, "render_metadata.json"), xaipipeline.RenderMetadata{
		Path:        filepath.Join(outputDir, "output_xai.mp4"),
		Width:       720,
		Height:      1280,
		FPS:         24,
		DurationSec: 8,
		CodecName:   "h264",
		SizeBytes:   12,
		HasAudio:    false,
	})
	writeInspectFile(t, filepath.Join(outputDir, "shots", "shot_001.mp4"))
	writeInspectFile(t, filepath.Join(outputDir, "normalized", "shot_001.mp4"))
	writeInspectFile(t, filepath.Join(outputDir, "hyperframes", "index.html"))
	writeInspectFile(t, filepath.Join(outputDir, "hyperframes", "package.json"))
	writeInspectFile(t, filepath.Join(outputDir, "timeline_hyperframes.mp4"))
	writeInspectFile(t, filepath.Join(outputDir, "output_xai.mp4"))
	writeInspectFile(t, filepath.Join(outputDir, "preview_frame.jpg"))

	summary, err := xaipipeline.InspectOutputDir(outputDir)
	if err != nil {
		t.Fatalf("InspectOutputDir() error = %v", err)
	}

	if summary.OutputDir != outputDir {
		t.Fatalf("OutputDir = %q, want %q", summary.OutputDir, outputDir)
	}
	if summary.Status != xaipipeline.InspectStatusComplete {
		t.Fatalf("Status = %q, want %q", summary.Status, xaipipeline.InspectStatusComplete)
	}
	if summary.ProjectID != "inspect-story" {
		t.Fatalf("ProjectID = %q", summary.ProjectID)
	}
	if summary.StoryHash != "story-hash" {
		t.Fatalf("StoryHash = %q", summary.StoryHash)
	}
	if summary.Format != "portrait" || summary.FPS != 24 || summary.Width != 720 || summary.Height != 1280 {
		t.Fatalf("unexpected video summary: %#v", summary)
	}
	if summary.Shots != 1 {
		t.Fatalf("Shots = %d, want 1", summary.Shots)
	}
	if summary.Artifacts.XAIManifest != filepath.Join(outputDir, "xai_manifest.json") {
		t.Fatalf("XAIManifest path = %q", summary.Artifacts.XAIManifest)
	}
	if summary.Artifacts.RunMetadata != filepath.Join(outputDir, "xai_run_metadata.json") {
		t.Fatalf("RunMetadata path = %q", summary.Artifacts.RunMetadata)
	}
	if summary.Artifacts.RenderMetadata != filepath.Join(outputDir, "render_metadata.json") {
		t.Fatalf("RenderMetadata path = %q", summary.Artifacts.RenderMetadata)
	}
	if summary.Artifacts.OutputVideo != filepath.Join(outputDir, "output_xai.mp4") {
		t.Fatalf("OutputVideo path = %q", summary.Artifacts.OutputVideo)
	}
	if summary.Artifacts.PreviewFrame != filepath.Join(outputDir, "preview_frame.jpg") {
		t.Fatalf("PreviewFrame path = %q", summary.Artifacts.PreviewFrame)
	}
	if !summary.Artifacts.OutputVideoExists {
		t.Fatal("OutputVideoExists = false, want true")
	}
	if !summary.Artifacts.PreviewFrameExists {
		t.Fatal("PreviewFrameExists = false, want true")
	}
	if summary.RunMetadata == nil || !summary.RunMetadata.Planned {
		t.Fatalf("RunMetadata not loaded: %#v", summary.RunMetadata)
	}
	if summary.RenderMetadata == nil || summary.RenderMetadata.Width != 720 {
		t.Fatalf("RenderMetadata not loaded: %#v", summary.RenderMetadata)
	}
	if len(summary.MissingArtifacts) != 0 {
		t.Fatalf("MissingArtifacts = %#v, want none", summary.MissingArtifacts)
	}
	if len(summary.Issues) != 0 {
		t.Fatalf("Issues = %#v, want none", summary.Issues)
	}
}

func TestInspectOutputDir_MissingManifestReturnsInvalidSummary(t *testing.T) {
	t.Parallel()

	summary, err := xaipipeline.InspectOutputDir(t.TempDir())
	if err != nil {
		t.Fatalf("InspectOutputDir() error = %v", err)
	}

	if summary.Status != xaipipeline.InspectStatusInvalid {
		t.Fatalf("Status = %q, want %q", summary.Status, xaipipeline.InspectStatusInvalid)
	}
	if !sameStrings(summary.MissingArtifacts, []string{"xai_manifest.json"}) {
		t.Fatalf("MissingArtifacts = %#v", summary.MissingArtifacts)
	}
	if len(summary.Issues) != 1 {
		t.Fatalf("Issues = %#v, want one issue", summary.Issues)
	}
}

func TestInspectOutputDir_InvalidManifestReturnsInvalidSummary(t *testing.T) {
	t.Parallel()

	outputDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(outputDir, "xai_manifest.json"), []byte("{"), 0644); err != nil {
		t.Fatalf("write invalid manifest: %v", err)
	}

	summary, err := xaipipeline.InspectOutputDir(outputDir)
	if err != nil {
		t.Fatalf("InspectOutputDir() error = %v", err)
	}

	if summary.Status != xaipipeline.InspectStatusInvalid {
		t.Fatalf("Status = %q, want %q", summary.Status, xaipipeline.InspectStatusInvalid)
	}
	if len(summary.Issues) != 1 {
		t.Fatalf("Issues = %#v, want one issue", summary.Issues)
	}
}

func TestInspectOutputDir_ReportsOptionalMissingArtifacts(t *testing.T) {
	t.Parallel()

	outputDir := t.TempDir()
	writeInspectJSON(t, filepath.Join(outputDir, "xai_manifest.json"), xaipipeline.Manifest{
		ProjectID: "partial-story",
		Format:    "portrait",
		FPS:       24,
		Width:     720,
		Height:    1280,
		Shots: []xaipipeline.Shot{
			{Index: 1, Prompt: "shot", DurationSec: 8},
		},
	})

	summary, err := xaipipeline.InspectOutputDir(outputDir)
	if err != nil {
		t.Fatalf("InspectOutputDir() error = %v", err)
	}

	if summary.Status != xaipipeline.InspectStatusPartial {
		t.Fatalf("Status = %q, want %q", summary.Status, xaipipeline.InspectStatusPartial)
	}
	wantMissing := []string{
		"shots/shot_001.mp4",
		"normalized/shot_001.mp4",
		"hyperframes/index.html",
		"hyperframes/package.json",
		"timeline_hyperframes.mp4",
		"xai_run_metadata.json",
		"render_metadata.json",
		"output_xai.mp4",
		"preview_frame.jpg",
	}
	if !sameStrings(summary.MissingArtifacts, wantMissing) {
		t.Fatalf("MissingArtifacts = %#v, want %#v", summary.MissingArtifacts, wantMissing)
	}
	if summary.RunMetadata != nil {
		t.Fatalf("RunMetadata = %#v, want nil", summary.RunMetadata)
	}
	if summary.RenderMetadata != nil {
		t.Fatalf("RenderMetadata = %#v, want nil", summary.RenderMetadata)
	}
	if summary.Artifacts.OutputVideoExists {
		t.Fatal("OutputVideoExists = true, want false")
	}
	if summary.Artifacts.PreviewFrameExists {
		t.Fatal("PreviewFrameExists = true, want false")
	}
	if len(summary.Issues) != 0 {
		t.Fatalf("Issues = %#v, want none", summary.Issues)
	}
}

func TestInspectOutputDir_ReportsMissingProductionArtifacts(t *testing.T) {
	t.Parallel()

	outputDir := t.TempDir()
	writeCompleteInspectFixture(t, outputDir, "missing-production-artifacts")
	if err := os.Remove(filepath.Join(outputDir, "normalized", "shot_001.mp4")); err != nil {
		t.Fatalf("remove normalized shot: %v", err)
	}
	if err := os.Remove(filepath.Join(outputDir, "timeline_hyperframes.mp4")); err != nil {
		t.Fatalf("remove timeline: %v", err)
	}

	summary, err := xaipipeline.InspectOutputDir(outputDir)
	if err != nil {
		t.Fatalf("InspectOutputDir() error = %v", err)
	}

	if summary.Status != xaipipeline.InspectStatusPartial {
		t.Fatalf("Status = %q, want %q", summary.Status, xaipipeline.InspectStatusPartial)
	}
	wantMissing := []string{
		"normalized/shot_001.mp4",
		"timeline_hyperframes.mp4",
	}
	if !sameStrings(summary.MissingArtifacts, wantMissing) {
		t.Fatalf("MissingArtifacts = %#v, want %#v", summary.MissingArtifacts, wantMissing)
	}
}

func TestInspectOutputDir_ReportsMissingCanonicalShotWhenManifestVideoPathIsExternal(t *testing.T) {
	t.Parallel()

	outputDir := t.TempDir()
	writeCompleteInspectFixture(t, outputDir, "external-video-path")
	externalShot := filepath.Join(t.TempDir(), "external-shot.mp4")
	writeInspectFile(t, externalShot)
	if err := os.Remove(filepath.Join(outputDir, "shots", "shot_001.mp4")); err != nil {
		t.Fatalf("remove canonical shot: %v", err)
	}
	writeInspectJSON(t, filepath.Join(outputDir, "xai_manifest.json"), xaipipeline.Manifest{
		ProjectID: "external-video-path",
		Format:    "portrait",
		FPS:       24,
		Width:     720,
		Height:    1280,
		Shots: []xaipipeline.Shot{
			{
				Index:        1,
				Prompt:       "shot",
				PromptHash:   "prompt-hash",
				XAIRequestID: "req_123",
				XAIStatus:    "done",
				DurationSec:  8,
				VideoPath:    externalShot,
			},
		},
	})

	summary, err := xaipipeline.InspectOutputDir(outputDir)
	if err != nil {
		t.Fatalf("InspectOutputDir() error = %v", err)
	}

	if summary.Status != xaipipeline.InspectStatusPartial {
		t.Fatalf("Status = %q, want %q", summary.Status, xaipipeline.InspectStatusPartial)
	}
	if !sameStrings(summary.MissingArtifacts, []string{"shots/shot_001.mp4"}) {
		t.Fatalf("MissingArtifacts = %#v, want canonical local shot missing", summary.MissingArtifacts)
	}
}

func TestInspectOutputDir_RejectsLegacyRemotionPropsArtifact(t *testing.T) {
	t.Parallel()

	outputDir := t.TempDir()
	writeCompleteInspectFixture(t, outputDir, "legacy-remotion-props")
	writeInspectFile(t, filepath.Join(outputDir, "remotion_props.json"))

	summary, err := xaipipeline.InspectOutputDir(outputDir)
	if err != nil {
		t.Fatalf("InspectOutputDir() error = %v", err)
	}

	if summary.Status != xaipipeline.InspectStatusInvalid {
		t.Fatalf("Status = %q, want %q", summary.Status, xaipipeline.InspectStatusInvalid)
	}
	if !stringsContain(summary.Issues, "legacy remotion_props.json artifact is not allowed in xAI-native output") {
		t.Fatalf("Issues = %#v, want legacy remotion_props issue", summary.Issues)
	}
}

func TestInspectBatchOutputDir_SummarizesEpisodeDirs(t *testing.T) {
	t.Parallel()

	outputDir := t.TempDir()
	writeCompleteInspectFixture(t, filepath.Join(outputDir, "episode_001"), "batch-ep-1")
	writeCompleteInspectFixture(t, filepath.Join(outputDir, "episode_002"), "batch-ep-2")

	summary, err := xaipipeline.InspectBatchOutputDir(outputDir)
	if err != nil {
		t.Fatalf("InspectBatchOutputDir() error = %v", err)
	}

	if summary.OutputDir != outputDir {
		t.Fatalf("OutputDir = %q, want %q", summary.OutputDir, outputDir)
	}
	if summary.Status != xaipipeline.InspectStatusComplete {
		t.Fatalf("Status = %q, want %q", summary.Status, xaipipeline.InspectStatusComplete)
	}
	if summary.TotalEpisodes != 2 || summary.CompleteEpisodes != 2 || summary.PartialEpisodes != 0 || summary.InvalidEpisodes != 0 {
		t.Fatalf("unexpected batch counts: %#v", summary)
	}
	if summary.StoryHash != inspectStoryHash {
		t.Fatalf("StoryHash = %q, want %q", summary.StoryHash, inspectStoryHash)
	}
	if summary.VideoModel != "grok-imagine-video" {
		t.Fatalf("VideoModel = %q, want grok-imagine-video", summary.VideoModel)
	}
	if len(summary.Episodes) != 2 {
		t.Fatalf("Episodes = %d, want 2", len(summary.Episodes))
	}
	if summary.Episodes[0].Episode != 1 || summary.Episodes[0].Inspect.ProjectID != "batch-ep-1" {
		t.Fatalf("episode 1 summary = %#v", summary.Episodes[0])
	}
	if summary.Episodes[1].Episode != 2 || summary.Episodes[1].Inspect.ProjectID != "batch-ep-2" {
		t.Fatalf("episode 2 summary = %#v", summary.Episodes[1])
	}
}

func TestInspectBatchOutputDir_RejectsMixedEpisodeVideoModels(t *testing.T) {
	t.Parallel()

	outputDir := t.TempDir()
	writeCompleteInspectFixtureWithVideoModel(t, filepath.Join(outputDir, "episode_001"), "batch-ep-1", "grok-imagine-video")
	writeCompleteInspectFixtureWithVideoModel(t, filepath.Join(outputDir, "episode_002"), "batch-ep-2", "grok-imagine-video-v2")

	summary, err := xaipipeline.InspectBatchOutputDir(outputDir)
	if err != nil {
		t.Fatalf("InspectBatchOutputDir() error = %v", err)
	}

	if summary.Status != xaipipeline.InspectStatusInvalid {
		t.Fatalf("Status = %q, want %q", summary.Status, xaipipeline.InspectStatusInvalid)
	}
	if summary.TotalEpisodes != 2 || summary.CompleteEpisodes != 2 || summary.InvalidEpisodes != 0 {
		t.Fatalf("unexpected batch counts: %#v", summary)
	}
	if !stringsContain(summary.Issues, `episode_002 video_model="grok-imagine-video-v2", want batch video_model "grok-imagine-video"`) {
		t.Fatalf("Issues = %#v, want mixed video_model issue", summary.Issues)
	}
}

func TestInspectBatchOutputDir_RejectsMixedEpisodeStoryHashes(t *testing.T) {
	t.Parallel()

	outputDir := t.TempDir()
	writeCompleteInspectFixtureWithIdentity(t, filepath.Join(outputDir, "episode_001"), "batch-ep-1", inspectStoryHash, "grok-imagine-video")
	writeCompleteInspectFixtureWithIdentity(t, filepath.Join(outputDir, "episode_002"), "batch-ep-2", "abcdef0123456789abcdef0123456789abcdef0123456789abcdef0123456789", "grok-imagine-video")

	summary, err := xaipipeline.InspectBatchOutputDir(outputDir)
	if err != nil {
		t.Fatalf("InspectBatchOutputDir() error = %v", err)
	}

	if summary.Status != xaipipeline.InspectStatusInvalid {
		t.Fatalf("Status = %q, want %q", summary.Status, xaipipeline.InspectStatusInvalid)
	}
	if summary.TotalEpisodes != 2 || summary.CompleteEpisodes != 2 || summary.InvalidEpisodes != 0 {
		t.Fatalf("unexpected batch counts: %#v", summary)
	}
	if !stringsContain(summary.Issues, `episode_002 story_hash="abcdef0123456789abcdef0123456789abcdef0123456789abcdef0123456789", want batch story_hash "`+inspectStoryHash+`"`) {
		t.Fatalf("Issues = %#v, want mixed story_hash issue", summary.Issues)
	}
}

func TestInspectBatchOutputDir_MixesEpisodeStatuses(t *testing.T) {
	t.Parallel()

	outputDir := t.TempDir()
	writeCompleteInspectFixture(t, filepath.Join(outputDir, "episode_001"), "complete")
	partialDir := filepath.Join(outputDir, "episode_002")
	if err := os.MkdirAll(partialDir, 0755); err != nil {
		t.Fatalf("mkdir partial: %v", err)
	}
	writeInspectJSON(t, filepath.Join(partialDir, "xai_manifest.json"), xaipipeline.Manifest{
		ProjectID: "partial",
		Format:    "portrait",
		FPS:       24,
		Width:     720,
		Height:    1280,
		Shots:     []xaipipeline.Shot{{Index: 1, Prompt: "shot", DurationSec: 8}},
	})
	invalidDir := filepath.Join(outputDir, "episode_003")
	if err := os.MkdirAll(invalidDir, 0755); err != nil {
		t.Fatalf("mkdir invalid: %v", err)
	}

	summary, err := xaipipeline.InspectBatchOutputDir(outputDir)
	if err != nil {
		t.Fatalf("InspectBatchOutputDir() error = %v", err)
	}

	if summary.Status != xaipipeline.InspectStatusInvalid {
		t.Fatalf("Status = %q, want %q", summary.Status, xaipipeline.InspectStatusInvalid)
	}
	if summary.TotalEpisodes != 3 || summary.CompleteEpisodes != 1 || summary.PartialEpisodes != 1 || summary.InvalidEpisodes != 1 {
		t.Fatalf("unexpected batch counts: %#v", summary)
	}
	if len(summary.Issues) == 0 {
		t.Fatal("Issues is empty, want invalid episode issue")
	}
}

func TestInspectBatchOutputDir_RejectsRootManifest(t *testing.T) {
	t.Parallel()

	outputDir := t.TempDir()
	writeCompleteInspectFixture(t, filepath.Join(outputDir, "episode_001"), "batch-ep-1")
	writeCompleteInspectFixture(t, filepath.Join(outputDir, "episode_002"), "batch-ep-2")
	writeInspectJSON(t, filepath.Join(outputDir, "xai_manifest.json"), xaipipeline.Manifest{
		ProjectID: "ambiguous-root",
		Format:    "portrait",
		FPS:       24,
		Width:     720,
		Height:    1280,
		Shots:     []xaipipeline.Shot{{Index: 1, Prompt: "shot", DurationSec: 8}},
	})

	summary, err := xaipipeline.InspectBatchOutputDir(outputDir)
	if err != nil {
		t.Fatalf("InspectBatchOutputDir() error = %v", err)
	}

	if summary.Status != xaipipeline.InspectStatusInvalid {
		t.Fatalf("Status = %q, want %q", summary.Status, xaipipeline.InspectStatusInvalid)
	}
	if summary.TotalEpisodes != 2 || summary.CompleteEpisodes != 2 {
		t.Fatalf("batch counts = %#v", summary)
	}
	if !sameStrings(summary.Issues, []string{"root xai_manifest.json is not allowed in xAI-native batch output"}) {
		t.Fatalf("Issues = %#v, want root manifest issue", summary.Issues)
	}
}

func TestInspectBatchOutputDir_RejectsRootSingleOutputArtifacts(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		writeRoot func(t *testing.T, outputDir string)
		wantIssue string
	}{
		{
			name: "output video",
			writeRoot: func(t *testing.T, outputDir string) {
				t.Helper()
				writeInspectFile(t, filepath.Join(outputDir, "output_xai.mp4"))
			},
			wantIssue: "root output_xai.mp4 is not allowed in xAI-native batch output",
		},
		{
			name: "hyperframes project",
			writeRoot: func(t *testing.T, outputDir string) {
				t.Helper()
				writeInspectFile(t, filepath.Join(outputDir, "hyperframes", "index.html"))
			},
			wantIssue: "root hyperframes is not allowed in xAI-native batch output",
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			outputDir := t.TempDir()
			writeCompleteInspectFixture(t, filepath.Join(outputDir, "episode_001"), "batch-ep-1")
			writeCompleteInspectFixture(t, filepath.Join(outputDir, "episode_002"), "batch-ep-2")
			tt.writeRoot(t, outputDir)

			summary, err := xaipipeline.InspectBatchOutputDir(outputDir)
			if err != nil {
				t.Fatalf("InspectBatchOutputDir() error = %v", err)
			}

			if summary.Status != xaipipeline.InspectStatusInvalid {
				t.Fatalf("Status = %q, want %q", summary.Status, xaipipeline.InspectStatusInvalid)
			}
			if summary.TotalEpisodes != 2 || summary.CompleteEpisodes != 2 {
				t.Fatalf("batch counts = %#v", summary)
			}
			if !stringsContain(summary.Issues, tt.wantIssue) {
				t.Fatalf("Issues = %#v, want %q", summary.Issues, tt.wantIssue)
			}
		})
	}
}

func TestInspectBatchOutputDir_RejectsRootLegacyRemotionProps(t *testing.T) {
	t.Parallel()

	outputDir := t.TempDir()
	writeCompleteInspectFixture(t, filepath.Join(outputDir, "episode_001"), "batch-ep-1")
	writeCompleteInspectFixture(t, filepath.Join(outputDir, "episode_002"), "batch-ep-2")
	writeInspectFile(t, filepath.Join(outputDir, "remotion_props.json"))

	summary, err := xaipipeline.InspectBatchOutputDir(outputDir)
	if err != nil {
		t.Fatalf("InspectBatchOutputDir() error = %v", err)
	}

	if summary.Status != xaipipeline.InspectStatusInvalid {
		t.Fatalf("Status = %q, want %q", summary.Status, xaipipeline.InspectStatusInvalid)
	}
	if summary.TotalEpisodes != 2 || summary.CompleteEpisodes != 2 {
		t.Fatalf("batch counts = %#v", summary)
	}
	if !stringsContain(summary.Issues, "legacy remotion_props.json artifact is not allowed in xAI-native batch output") {
		t.Fatalf("Issues = %#v, want legacy remotion_props issue", summary.Issues)
	}
}

func TestInspectBatchOutputDir_RejectsMalformedEpisodeDirs(t *testing.T) {
	t.Parallel()

	outputDir := t.TempDir()
	writeCompleteInspectFixture(t, filepath.Join(outputDir, "episode_001"), "batch-ep-1")
	writeCompleteInspectFixture(t, filepath.Join(outputDir, "episode_1"), "bad-name")
	if err := os.MkdirAll(filepath.Join(outputDir, "episode_bad"), 0755); err != nil {
		t.Fatalf("mkdir malformed episode: %v", err)
	}

	summary, err := xaipipeline.InspectBatchOutputDir(outputDir)
	if err != nil {
		t.Fatalf("InspectBatchOutputDir() error = %v", err)
	}

	if summary.Status != xaipipeline.InspectStatusInvalid {
		t.Fatalf("Status = %q, want %q", summary.Status, xaipipeline.InspectStatusInvalid)
	}
	if summary.TotalEpisodes != 1 || summary.CompleteEpisodes != 1 {
		t.Fatalf("batch counts = %#v", summary)
	}
	for _, want := range []string{
		`malformed episode directory "episode_1": want episode_###`,
		`malformed episode directory "episode_bad": want episode_###`,
	} {
		if !stringsContain(summary.Issues, want) {
			t.Fatalf("Issues = %#v, want %q", summary.Issues, want)
		}
	}
}

func TestInspectBatchOutputDir_RejectsSymlinkedEpisodeDir(t *testing.T) {
	t.Parallel()

	outputDir := t.TempDir()
	externalEpisode := filepath.Join(t.TempDir(), "external-episode")
	writeCompleteInspectFixture(t, externalEpisode, "external-episode")
	if err := os.Symlink(externalEpisode, filepath.Join(outputDir, "episode_001")); err != nil {
		t.Skipf("symlink not available: %v", err)
	}

	summary, err := xaipipeline.InspectBatchOutputDir(outputDir)
	if err != nil {
		t.Fatalf("InspectBatchOutputDir() error = %v", err)
	}

	if summary.Status != xaipipeline.InspectStatusInvalid {
		t.Fatalf("Status = %q, want %q", summary.Status, xaipipeline.InspectStatusInvalid)
	}
	if summary.TotalEpisodes != 0 {
		t.Fatalf("TotalEpisodes = %d, want 0", summary.TotalEpisodes)
	}
	if !stringsContain(summary.Issues, `episode directory "episode_001" is a symlink; xAI-native batch episodes must be output-local directories`) {
		t.Fatalf("Issues = %#v, want symlink episode issue", summary.Issues)
	}
}

func TestInspectBatchOutputDir_RejectsMissingEpisodeNumber(t *testing.T) {
	t.Parallel()

	outputDir := t.TempDir()
	writeCompleteInspectFixture(t, filepath.Join(outputDir, "episode_001"), "batch-ep-1")
	writeCompleteInspectFixture(t, filepath.Join(outputDir, "episode_003"), "batch-ep-3")

	summary, err := xaipipeline.InspectBatchOutputDir(outputDir)
	if err != nil {
		t.Fatalf("InspectBatchOutputDir() error = %v", err)
	}

	if summary.Status != xaipipeline.InspectStatusInvalid {
		t.Fatalf("Status = %q, want %q", summary.Status, xaipipeline.InspectStatusInvalid)
	}
	if summary.TotalEpisodes != 2 || summary.CompleteEpisodes != 2 {
		t.Fatalf("batch counts = %#v", summary)
	}
	if !stringsContain(summary.Issues, "missing episode_002 directory") {
		t.Fatalf("Issues = %#v, want missing episode issue", summary.Issues)
	}
}

func TestInspectBatchOutputDir_NoEpisodesReturnsInvalidSummary(t *testing.T) {
	t.Parallel()

	summary, err := xaipipeline.InspectBatchOutputDir(t.TempDir())
	if err != nil {
		t.Fatalf("InspectBatchOutputDir() error = %v", err)
	}
	if summary.Status != xaipipeline.InspectStatusInvalid {
		t.Fatalf("Status = %q, want %q", summary.Status, xaipipeline.InspectStatusInvalid)
	}
	if len(summary.Issues) != 1 {
		t.Fatalf("Issues = %#v, want one issue", summary.Issues)
	}
}

func writeInspectJSON(t *testing.T, path string, value any) {
	t.Helper()

	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		t.Fatalf("marshal %s: %v", path, err)
	}
	if err := os.WriteFile(path, data, 0644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

func writeCompleteInspectFixture(t *testing.T, outputDir string, projectID string) {
	t.Helper()

	writeCompleteInspectFixtureWithIdentity(t, outputDir, projectID, inspectStoryHash, "grok-imagine-video")
}

func writeCompleteInspectFixtureWithVideoModel(t *testing.T, outputDir string, projectID string, videoModel string) {
	t.Helper()

	writeCompleteInspectFixtureWithIdentity(t, outputDir, projectID, inspectStoryHash, videoModel)
}

func writeCompleteInspectFixtureWithIdentity(t *testing.T, outputDir string, projectID string, storyHash string, videoModel string) {
	t.Helper()

	if err := os.MkdirAll(outputDir, 0755); err != nil {
		t.Fatalf("mkdir %s: %v", outputDir, err)
	}
	writeInspectJSON(t, filepath.Join(outputDir, "xai_manifest.json"), xaipipeline.Manifest{
		ProjectID:  projectID,
		StoryHash:  storyHash,
		VideoModel: videoModel,
		Format:     "portrait",
		FPS:        24,
		Width:      720,
		Height:     1280,
		Shots: []xaipipeline.Shot{
			{
				Index:        1,
				Prompt:       "shot",
				PromptHash:   "prompt-hash",
				XAIRequestID: "req_123",
				XAIStatus:    "done",
				DurationSec:  8,
				VideoPath:    "shots/shot_001.mp4",
			},
		},
	})
	writeInspectJSON(t, filepath.Join(outputDir, "xai_run_metadata.json"), xaipipeline.RunMetadata{
		Planned:        true,
		VideoModel:     videoModel,
		GeneratedShots: []int{1},
		ShotDecisions: []xaipipeline.ShotDecision{
			{
				Index:        1,
				Decision:     "generated",
				VideoPath:    "shots/shot_001.mp4",
				PromptHash:   "prompt-hash",
				XAIRequestID: "req_123",
				XAIStatus:    "done",
			},
		},
	})
	writeInspectJSON(t, filepath.Join(outputDir, "render_metadata.json"), xaipipeline.RenderMetadata{
		Path:        filepath.Join(outputDir, "output_xai.mp4"),
		Width:       720,
		Height:      1280,
		FPS:         24,
		DurationSec: 8,
		CodecName:   "h264",
		SizeBytes:   12,
		HasAudio:    false,
	})
	writeInspectFile(t, filepath.Join(outputDir, "shots", "shot_001.mp4"))
	writeInspectFile(t, filepath.Join(outputDir, "normalized", "shot_001.mp4"))
	writeInspectFile(t, filepath.Join(outputDir, "hyperframes", "index.html"))
	writeInspectFile(t, filepath.Join(outputDir, "hyperframes", "package.json"))
	writeInspectFile(t, filepath.Join(outputDir, "timeline_hyperframes.mp4"))
	writeInspectFile(t, filepath.Join(outputDir, "output_xai.mp4"))
	writeInspectFile(t, filepath.Join(outputDir, "preview_frame.jpg"))
}

func writeInspectFile(t *testing.T, path string) {
	t.Helper()

	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatalf("mkdir %s: %v", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, []byte("artifact"), 0644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

func sameStrings(got, want []string) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range got {
		if got[i] != want[i] {
			return false
		}
	}
	return true
}

func stringsContain(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}
