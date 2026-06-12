package cmd

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/baochen10luo/stagenthand/internal/xaipipeline"
)

const cmdInspectStoryHash = "abcdef0123456789abcdef0123456789abcdef0123456789abcdef0123456789"
const cmdInspectVideoModel = "grok-imagine-video"

func TestBuildXAIInspectSummary_ReturnsPipelineInspectorSummary(t *testing.T) {
	t.Parallel()

	outputDir := t.TempDir()
	writeXAIInspectJSON(t, filepath.Join(outputDir, "xai_manifest.json"), xaipipeline.Manifest{
		ProjectID: "cmd-inspect",
		Format:    "portrait",
		FPS:       24,
		Width:     720,
		Height:    1280,
		Shots: []xaipipeline.Shot{
			{Index: 1, Prompt: "shot", DurationSec: 8},
			{Index: 2, Prompt: "shot", DurationSec: 8},
		},
	})

	summaryAny, err := buildXAIInspectSummary(outputDir)
	if err != nil {
		t.Fatalf("buildXAIInspectSummary() error = %v", err)
	}
	summary, ok := summaryAny.(xaipipeline.InspectSummary)
	if !ok {
		t.Fatalf("summary type = %T, want InspectSummary", summaryAny)
	}

	if summary.ProjectID != "cmd-inspect" {
		t.Fatalf("ProjectID = %q", summary.ProjectID)
	}
	if summary.Status != xaipipeline.InspectStatusPartial {
		t.Fatalf("Status = %q, want %q", summary.Status, xaipipeline.InspectStatusPartial)
	}
	if summary.Shots != 2 {
		t.Fatalf("Shots = %d, want 2", summary.Shots)
	}
	if summary.Artifacts.XAIManifest != filepath.Join(outputDir, "xai_manifest.json") {
		t.Fatalf("XAIManifest = %q", summary.Artifacts.XAIManifest)
	}
}

func TestBuildXAIInspectSummary_ReturnsInvalidSummaryForMissingManifest(t *testing.T) {
	t.Parallel()

	summaryAny, err := buildXAIInspectSummary(t.TempDir())
	if err != nil {
		t.Fatalf("buildXAIInspectSummary() error = %v", err)
	}
	summary, ok := summaryAny.(xaipipeline.InspectSummary)
	if !ok {
		t.Fatalf("summary type = %T, want InspectSummary", summaryAny)
	}

	if summary.Status != xaipipeline.InspectStatusInvalid {
		t.Fatalf("Status = %q, want %q", summary.Status, xaipipeline.InspectStatusInvalid)
	}
	if len(summary.Issues) == 0 {
		t.Fatal("Issues is empty, want invalid reason")
	}
}

func TestBuildXAIInspectSummary_ReturnsBatchSummaryForEpisodeRoot(t *testing.T) {
	t.Parallel()

	outputDir := t.TempDir()
	writeCompleteXAIInspectArtifacts(t, filepath.Join(outputDir, "episode_001"))
	writeCompleteXAIInspectArtifacts(t, filepath.Join(outputDir, "episode_002"))

	summaryAny, err := buildXAIInspectSummary(outputDir)
	if err != nil {
		t.Fatalf("buildXAIInspectSummary() error = %v", err)
	}
	summary, ok := summaryAny.(xaipipeline.BatchInspectSummary)
	if !ok {
		t.Fatalf("summary type = %T, want BatchInspectSummary", summaryAny)
	}
	if summary.Status != xaipipeline.InspectStatusComplete {
		t.Fatalf("Status = %q, want complete", summary.Status)
	}
	if summary.TotalEpisodes != 2 || summary.CompleteEpisodes != 2 {
		t.Fatalf("batch summary = %#v", summary)
	}
}

func TestBuildXAIInspectSummary_ReturnsInvalidBatchForAmbiguousSingleAndBatchRoot(t *testing.T) {
	t.Parallel()

	outputDir := t.TempDir()
	writeCompleteXAIInspectArtifacts(t, outputDir)
	writeCompleteXAIInspectArtifacts(t, filepath.Join(outputDir, "episode_001"))
	writeCompleteXAIInspectArtifacts(t, filepath.Join(outputDir, "episode_002"))

	summaryAny, err := buildXAIInspectSummary(outputDir)
	if err != nil {
		t.Fatalf("buildXAIInspectSummary() error = %v", err)
	}
	summary, ok := summaryAny.(xaipipeline.BatchInspectSummary)
	if !ok {
		t.Fatalf("summary type = %T, want BatchInspectSummary", summaryAny)
	}
	if summary.Status != xaipipeline.InspectStatusInvalid {
		t.Fatalf("Status = %q, want invalid", summary.Status)
	}
	if summary.TotalEpisodes != 2 || summary.CompleteEpisodes != 2 {
		t.Fatalf("batch summary = %#v", summary)
	}
	if len(summary.Issues) == 0 || !strings.Contains(summary.Issues[0], "root xai_manifest.json") {
		t.Fatalf("Issues = %#v, want root manifest issue", summary.Issues)
	}
}

func TestBuildXAIInspectSummary_ReturnsInvalidBatchWhenRootManifestArtifactIsDirectory(t *testing.T) {
	t.Parallel()

	outputDir := t.TempDir()
	if err := os.Mkdir(filepath.Join(outputDir, "xai_manifest.json"), 0755); err != nil {
		t.Fatalf("mkdir root manifest artifact: %v", err)
	}
	writeCompleteXAIInspectArtifacts(t, filepath.Join(outputDir, "episode_001"))

	summaryAny, err := buildXAIInspectSummary(outputDir)
	if err != nil {
		t.Fatalf("buildXAIInspectSummary() error = %v", err)
	}
	summary, ok := summaryAny.(xaipipeline.BatchInspectSummary)
	if !ok {
		t.Fatalf("summary type = %T, want BatchInspectSummary", summaryAny)
	}
	if summary.Status != xaipipeline.InspectStatusInvalid {
		t.Fatalf("Status = %q, want invalid", summary.Status)
	}
	if summary.TotalEpisodes != 1 || summary.CompleteEpisodes != 1 {
		t.Fatalf("batch summary = %#v", summary)
	}
	if !inspectIssuesContain(summary.Issues, "root xai_manifest.json is not allowed in xAI-native batch output") {
		t.Fatalf("Issues = %#v, want root manifest issue", summary.Issues)
	}
}

func TestBuildXAIInspectSummary_ReturnsInvalidBatchForRootLegacyRemotionProps(t *testing.T) {
	t.Parallel()

	outputDir := t.TempDir()
	writeCompleteXAIInspectArtifacts(t, filepath.Join(outputDir, "episode_001"))
	writeCompleteXAIInspectArtifacts(t, filepath.Join(outputDir, "episode_002"))
	writeXAIInspectFile(t, filepath.Join(outputDir, "remotion_props.json"))

	summaryAny, err := buildXAIInspectSummary(outputDir)
	if err != nil {
		t.Fatalf("buildXAIInspectSummary() error = %v", err)
	}
	summary, ok := summaryAny.(xaipipeline.BatchInspectSummary)
	if !ok {
		t.Fatalf("summary type = %T, want BatchInspectSummary", summaryAny)
	}
	if summary.Status != xaipipeline.InspectStatusInvalid {
		t.Fatalf("Status = %q, want invalid", summary.Status)
	}
	if summary.TotalEpisodes != 2 || summary.CompleteEpisodes != 2 {
		t.Fatalf("batch summary = %#v", summary)
	}
	if !inspectIssuesContain(summary.Issues, "legacy remotion_props.json artifact is not allowed in xAI-native batch output") {
		t.Fatalf("Issues = %#v, want legacy remotion_props issue", summary.Issues)
	}
}

func TestBuildXAIInspectSummary_ReturnsInvalidBatchForMalformedEpisodeDirs(t *testing.T) {
	t.Parallel()

	outputDir := t.TempDir()
	writeCompleteXAIInspectArtifacts(t, filepath.Join(outputDir, "episode_001"))
	if err := os.MkdirAll(filepath.Join(outputDir, "episode_1"), 0755); err != nil {
		t.Fatalf("mkdir malformed episode dir: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(outputDir, "episode_bad"), 0755); err != nil {
		t.Fatalf("mkdir malformed episode dir: %v", err)
	}

	summaryAny, err := buildXAIInspectSummary(outputDir)
	if err != nil {
		t.Fatalf("buildXAIInspectSummary() error = %v", err)
	}
	summary, ok := summaryAny.(xaipipeline.BatchInspectSummary)
	if !ok {
		t.Fatalf("summary type = %T, want BatchInspectSummary", summaryAny)
	}
	if summary.Status != xaipipeline.InspectStatusInvalid {
		t.Fatalf("Status = %q, want invalid", summary.Status)
	}
	if summary.TotalEpisodes != 1 || summary.CompleteEpisodes != 1 {
		t.Fatalf("batch summary = %#v", summary)
	}
	for _, want := range []string{
		`malformed episode directory "episode_1": want episode_###`,
		`malformed episode directory "episode_bad": want episode_###`,
	} {
		if !inspectIssuesContain(summary.Issues, want) {
			t.Fatalf("Issues = %#v, want %q", summary.Issues, want)
		}
	}
}

func TestBuildXAIInspectSummary_ReturnsInvalidBatchForOnlyMalformedEpisodeDirs(t *testing.T) {
	t.Parallel()

	outputDir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(outputDir, "episode_1"), 0755); err != nil {
		t.Fatalf("mkdir malformed episode dir: %v", err)
	}

	summaryAny, err := buildXAIInspectSummary(outputDir)
	if err != nil {
		t.Fatalf("buildXAIInspectSummary() error = %v", err)
	}
	summary, ok := summaryAny.(xaipipeline.BatchInspectSummary)
	if !ok {
		t.Fatalf("summary type = %T, want BatchInspectSummary", summaryAny)
	}
	if summary.Status != xaipipeline.InspectStatusInvalid {
		t.Fatalf("Status = %q, want invalid", summary.Status)
	}
	if summary.TotalEpisodes != 0 {
		t.Fatalf("TotalEpisodes = %d, want 0", summary.TotalEpisodes)
	}
	if !inspectIssuesContain(summary.Issues, `malformed episode directory "episode_1": want episode_###`) {
		t.Fatalf("Issues = %#v, want malformed episode issue", summary.Issues)
	}
}

func TestBuildXAIInspectSummary_ReturnsInvalidBatchForSymlinkedEpisodeDir(t *testing.T) {
	t.Parallel()

	outputDir := t.TempDir()
	externalEpisode := filepath.Join(t.TempDir(), "external-episode")
	writeCompleteXAIInspectArtifacts(t, externalEpisode)
	if err := os.Symlink(externalEpisode, filepath.Join(outputDir, "episode_001")); err != nil {
		t.Skipf("symlink not available: %v", err)
	}

	summaryAny, err := buildXAIInspectSummary(outputDir)
	if err != nil {
		t.Fatalf("buildXAIInspectSummary() error = %v", err)
	}
	summary, ok := summaryAny.(xaipipeline.BatchInspectSummary)
	if !ok {
		t.Fatalf("summary type = %T, want BatchInspectSummary", summaryAny)
	}
	if summary.Status != xaipipeline.InspectStatusInvalid {
		t.Fatalf("Status = %q, want invalid", summary.Status)
	}
	if summary.TotalEpisodes != 0 {
		t.Fatalf("TotalEpisodes = %d, want 0", summary.TotalEpisodes)
	}
	if !inspectIssuesContain(summary.Issues, `episode directory "episode_001" is a symlink; xAI-native batch episodes must be output-local directories`) {
		t.Fatalf("Issues = %#v, want symlink episode issue", summary.Issues)
	}
}

func TestBuildXAIInspectSummary_ReturnsInvalidBatchForMissingEpisodeNumber(t *testing.T) {
	t.Parallel()

	outputDir := t.TempDir()
	writeCompleteXAIInspectArtifacts(t, filepath.Join(outputDir, "episode_001"))
	writeCompleteXAIInspectArtifacts(t, filepath.Join(outputDir, "episode_003"))

	summaryAny, err := buildXAIInspectSummary(outputDir)
	if err != nil {
		t.Fatalf("buildXAIInspectSummary() error = %v", err)
	}
	summary, ok := summaryAny.(xaipipeline.BatchInspectSummary)
	if !ok {
		t.Fatalf("summary type = %T, want BatchInspectSummary", summaryAny)
	}
	if summary.Status != xaipipeline.InspectStatusInvalid {
		t.Fatalf("Status = %q, want invalid", summary.Status)
	}
	if summary.TotalEpisodes != 2 || summary.CompleteEpisodes != 2 {
		t.Fatalf("batch summary = %#v", summary)
	}
	if !inspectIssuesContain(summary.Issues, "missing episode_002 directory") {
		t.Fatalf("Issues = %#v, want missing episode issue", summary.Issues)
	}
}

func TestRunXAIInspect_StrictCompleteSucceeds(t *testing.T) {
	t.Parallel()

	outputDir := t.TempDir()
	writeCompleteXAIInspectArtifacts(t, outputDir)

	var out bytes.Buffer
	if err := runXAIInspect(outputDir, true, &out); err != nil {
		t.Fatalf("runXAIInspect() error = %v", err)
	}

	var summary xaipipeline.InspectSummary
	if err := json.Unmarshal(out.Bytes(), &summary); err != nil {
		t.Fatalf("decode stdout JSON: %v\n%s", err, out.String())
	}
	if summary.Status != xaipipeline.InspectStatusComplete {
		t.Fatalf("Status = %q, want %q", summary.Status, xaipipeline.InspectStatusComplete)
	}
	if summary.StoryHash != cmdInspectStoryHash {
		t.Fatalf("StoryHash = %q, want %q", summary.StoryHash, cmdInspectStoryHash)
	}
	if summary.VideoModel != cmdInspectVideoModel {
		t.Fatalf("VideoModel = %q, want %q", summary.VideoModel, cmdInspectVideoModel)
	}
}

func TestRunXAIInspect_StrictBatchCompleteSucceeds(t *testing.T) {
	t.Parallel()

	outputDir := t.TempDir()
	writeCompleteXAIInspectArtifacts(t, filepath.Join(outputDir, "episode_001"))
	writeCompleteXAIInspectArtifacts(t, filepath.Join(outputDir, "episode_002"))

	var out bytes.Buffer
	if err := runXAIInspect(outputDir, true, &out); err != nil {
		t.Fatalf("runXAIInspect() error = %v", err)
	}

	var summary xaipipeline.BatchInspectSummary
	if err := json.Unmarshal(out.Bytes(), &summary); err != nil {
		t.Fatalf("decode stdout JSON: %v\n%s", err, out.String())
	}
	if summary.Status != xaipipeline.InspectStatusComplete || summary.TotalEpisodes != 2 {
		t.Fatalf("batch summary = %#v", summary)
	}
	if summary.StoryHash != cmdInspectStoryHash {
		t.Fatalf("StoryHash = %q, want %q", summary.StoryHash, cmdInspectStoryHash)
	}
	if summary.VideoModel != cmdInspectVideoModel {
		t.Fatalf("VideoModel = %q, want %q", summary.VideoModel, cmdInspectVideoModel)
	}
}

func TestRunXAIInspect_StrictBatchWithRootLegacyRemotionPropsWritesJSONAndReturnsError(t *testing.T) {
	t.Parallel()

	outputDir := t.TempDir()
	writeCompleteXAIInspectArtifacts(t, filepath.Join(outputDir, "episode_001"))
	writeCompleteXAIInspectArtifacts(t, filepath.Join(outputDir, "episode_002"))
	writeXAIInspectFile(t, filepath.Join(outputDir, "remotion_props.json"))

	var out bytes.Buffer
	err := runXAIInspect(outputDir, true, &out)
	if err == nil {
		t.Fatal("runXAIInspect() error = nil, want strict invalid batch error")
	}
	if !strings.Contains(err.Error(), "invalid") {
		t.Fatalf("error = %q, want invalid status", err.Error())
	}

	var summary xaipipeline.BatchInspectSummary
	if decodeErr := json.Unmarshal(out.Bytes(), &summary); decodeErr != nil {
		t.Fatalf("decode stdout JSON: %v\n%s", decodeErr, out.String())
	}
	if summary.Status != xaipipeline.InspectStatusInvalid {
		t.Fatalf("Status = %q, want %q", summary.Status, xaipipeline.InspectStatusInvalid)
	}
	if !inspectIssuesContain(summary.Issues, "legacy remotion_props.json artifact is not allowed in xAI-native batch output") {
		t.Fatalf("Issues = %#v, want legacy remotion_props issue", summary.Issues)
	}
}

func TestRunXAIInspect_StrictCompleteWithLegacyRemotionPropsWritesJSONAndReturnsError(t *testing.T) {
	t.Parallel()

	outputDir := t.TempDir()
	writeCompleteXAIInspectArtifacts(t, outputDir)
	writeXAIInspectFile(t, filepath.Join(outputDir, "remotion_props.json"))

	var out bytes.Buffer
	err := runXAIInspect(outputDir, true, &out)
	if err == nil {
		t.Fatal("runXAIInspect() error = nil, want strict invalid legacy artifact error")
	}
	if !strings.Contains(err.Error(), "invalid") {
		t.Fatalf("error = %q, want invalid status", err.Error())
	}

	var summary xaipipeline.InspectSummary
	if decodeErr := json.Unmarshal(out.Bytes(), &summary); decodeErr != nil {
		t.Fatalf("decode stdout JSON: %v\n%s", decodeErr, out.String())
	}
	if summary.Status != xaipipeline.InspectStatusInvalid {
		t.Fatalf("Status = %q, want %q", summary.Status, xaipipeline.InspectStatusInvalid)
	}
	if !inspectIssuesContain(summary.Issues, "legacy remotion_props.json artifact is not allowed in xAI-native output") {
		t.Fatalf("Issues = %#v, want legacy remotion_props issue", summary.Issues)
	}
}

func TestRunXAIInspect_StrictPartialWritesJSONAndReturnsError(t *testing.T) {
	t.Parallel()

	outputDir := t.TempDir()
	writeXAIInspectJSON(t, filepath.Join(outputDir, "xai_manifest.json"), xaipipeline.Manifest{
		ProjectID: "partial",
		Format:    "portrait",
		FPS:       24,
		Width:     720,
		Height:    1280,
		Shots: []xaipipeline.Shot{
			{Index: 1, Prompt: "shot", DurationSec: 8},
		},
	})

	var out bytes.Buffer
	err := runXAIInspect(outputDir, true, &out)
	if err == nil {
		t.Fatal("runXAIInspect() error = nil, want strict partial error")
	}
	if !strings.Contains(err.Error(), "partial") {
		t.Fatalf("error = %q, want partial status", err.Error())
	}

	var summary xaipipeline.InspectSummary
	if decodeErr := json.Unmarshal(out.Bytes(), &summary); decodeErr != nil {
		t.Fatalf("decode stdout JSON: %v\n%s", decodeErr, out.String())
	}
	if summary.Status != xaipipeline.InspectStatusPartial {
		t.Fatalf("Status = %q, want %q", summary.Status, xaipipeline.InspectStatusPartial)
	}
}

func TestRunXAIInspect_StrictInvalidWritesJSONAndReturnsError(t *testing.T) {
	t.Parallel()

	var out bytes.Buffer
	err := runXAIInspect(t.TempDir(), true, &out)
	if err == nil {
		t.Fatal("runXAIInspect() error = nil, want strict invalid error")
	}
	if !strings.Contains(err.Error(), "invalid") {
		t.Fatalf("error = %q, want invalid status", err.Error())
	}

	var summary xaipipeline.InspectSummary
	if decodeErr := json.Unmarshal(out.Bytes(), &summary); decodeErr != nil {
		t.Fatalf("decode stdout JSON: %v\n%s", decodeErr, out.String())
	}
	if summary.Status != xaipipeline.InspectStatusInvalid {
		t.Fatalf("Status = %q, want %q", summary.Status, xaipipeline.InspectStatusInvalid)
	}
}

func inspectIssuesContain(issues []string, want string) bool {
	for _, issue := range issues {
		if issue == want {
			return true
		}
	}
	return false
}

func writeXAIInspectJSON(t *testing.T, path string, value any) {
	t.Helper()

	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		t.Fatalf("marshal %s: %v", path, err)
	}
	if err := os.WriteFile(path, data, 0644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

func writeXAIInspectFile(t *testing.T, path string) {
	t.Helper()

	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatalf("mkdir %s: %v", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, []byte("artifact"), 0644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

func writeCompleteXAIInspectArtifacts(t *testing.T, outputDir string) {
	t.Helper()

	if err := os.MkdirAll(outputDir, 0755); err != nil {
		t.Fatalf("mkdir %s: %v", outputDir, err)
	}
	writeXAIInspectJSON(t, filepath.Join(outputDir, "xai_manifest.json"), xaipipeline.Manifest{
		ProjectID:  "complete",
		StoryHash:  cmdInspectStoryHash,
		VideoModel: cmdInspectVideoModel,
		Format:     "portrait",
		FPS:        24,
		Width:      720,
		Height:     1280,
		Shots: []xaipipeline.Shot{
			{Index: 1, Prompt: "shot", DurationSec: 8, VideoPath: "shots/shot_001.mp4"},
		},
	})
	writeXAIInspectJSON(t, filepath.Join(outputDir, "xai_run_metadata.json"), xaipipeline.RunMetadata{
		Planned:        true,
		VideoModel:     cmdInspectVideoModel,
		GeneratedShots: []int{1},
	})
	writeXAIInspectJSON(t, filepath.Join(outputDir, "render_metadata.json"), xaipipeline.RenderMetadata{
		Path:        filepath.Join(outputDir, "output_xai.mp4"),
		Width:       720,
		Height:      1280,
		FPS:         24,
		DurationSec: 8,
		CodecName:   "h264",
		SizeBytes:   12,
		HasAudio:    false,
	})
	writeXAIInspectFile(t, filepath.Join(outputDir, "shots", "shot_001.mp4"))
	writeXAIInspectFile(t, filepath.Join(outputDir, "normalized", "shot_001.mp4"))
	writeXAIInspectFile(t, filepath.Join(outputDir, "hyperframes", "index.html"))
	writeXAIInspectFile(t, filepath.Join(outputDir, "hyperframes", "package.json"))
	writeXAIInspectFile(t, filepath.Join(outputDir, "timeline_hyperframes.mp4"))
	writeXAIInspectFile(t, filepath.Join(outputDir, "output_xai.mp4"))
	writeXAIInspectFile(t, filepath.Join(outputDir, "preview_frame.jpg"))
}
