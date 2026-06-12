package cmd

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"image/jpeg"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/baochen10luo/stagenthand/internal/xaipipeline"
)

const cmdValidStoryHash = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"

func TestRunXAIValidate_ValidWritesJSONAndSucceeds(t *testing.T) {
	t.Parallel()

	outputDir := t.TempDir()
	writeCompleteXAIValidateArtifacts(t, outputDir)
	validator := &cmdStubOutputValidator{
		metadata: xaipipeline.RenderMetadata{
			Path:        filepath.Join(outputDir, "output_xai.mp4"),
			Width:       720,
			Height:      1280,
			FPS:         24,
			DurationSec: 8,
			CodecName:   "h264",
			PixelFormat: "yuv420p",
			SizeBytes:   xaiValidateMP4SizeBytes(),
			HasAudio:    false,
		},
	}

	var out bytes.Buffer
	if err := runXAIValidate(context.Background(), outputDir, validator, &out); err != nil {
		t.Fatalf("runXAIValidate() error = %v", err)
	}

	var summary xaipipeline.ValidationSummary
	if err := json.Unmarshal(out.Bytes(), &summary); err != nil {
		t.Fatalf("decode stdout JSON: %v\n%s", err, out.String())
	}
	if summary.Status != xaipipeline.ValidationStatusValid {
		t.Fatalf("Status = %q, want %q", summary.Status, xaipipeline.ValidationStatusValid)
	}
	if summary.StoryHash != cmdValidStoryHash {
		t.Fatalf("StoryHash = %q, want %q", summary.StoryHash, cmdValidStoryHash)
	}
	if summary.VideoModel != "grok-imagine-video" {
		t.Fatalf("VideoModel = %q, want grok-imagine-video", summary.VideoModel)
	}
}

func TestRunXAIValidate_InvalidWritesJSONAndReturnsError(t *testing.T) {
	t.Parallel()

	outputDir := t.TempDir()
	writeCompleteXAIValidateArtifacts(t, outputDir)
	validator := &cmdStubOutputValidator{err: errors.New("ffprobe failed")}

	var out bytes.Buffer
	err := runXAIValidate(context.Background(), outputDir, validator, &out)
	if err == nil {
		t.Fatal("runXAIValidate() error = nil, want invalid validation error")
	}
	if !strings.Contains(err.Error(), "invalid") {
		t.Fatalf("error = %q, want invalid status", err.Error())
	}

	var summary xaipipeline.ValidationSummary
	if decodeErr := json.Unmarshal(out.Bytes(), &summary); decodeErr != nil {
		t.Fatalf("decode stdout JSON: %v\n%s", decodeErr, out.String())
	}
	if summary.Status != xaipipeline.ValidationStatusInvalid {
		t.Fatalf("Status = %q, want %q", summary.Status, xaipipeline.ValidationStatusInvalid)
	}
}

func TestRunXAIValidate_LegacyRemotionPropsWritesJSONAndReturnsError(t *testing.T) {
	t.Parallel()

	outputDir := t.TempDir()
	writeCompleteXAIValidateArtifacts(t, outputDir)
	writeXAIInspectFile(t, filepath.Join(outputDir, "remotion_props.json"))
	validator := &cmdStubOutputValidator{
		metadata: xaipipeline.RenderMetadata{
			Path:        filepath.Join(outputDir, "output_xai.mp4"),
			Width:       720,
			Height:      1280,
			FPS:         24,
			DurationSec: 8,
			CodecName:   "h264",
			PixelFormat: "yuv420p",
			SizeBytes:   xaiValidateMP4SizeBytes(),
			HasAudio:    false,
		},
	}

	var out bytes.Buffer
	err := runXAIValidate(context.Background(), outputDir, validator, &out)
	if err == nil {
		t.Fatal("runXAIValidate() error = nil, want invalid legacy artifact validation error")
	}
	if !strings.Contains(err.Error(), "invalid") {
		t.Fatalf("error = %q, want invalid status", err.Error())
	}

	var summary xaipipeline.ValidationSummary
	if decodeErr := json.Unmarshal(out.Bytes(), &summary); decodeErr != nil {
		t.Fatalf("decode stdout JSON: %v\n%s", decodeErr, out.String())
	}
	if summary.Status != xaipipeline.ValidationStatusInvalid {
		t.Fatalf("Status = %q, want %q", summary.Status, xaipipeline.ValidationStatusInvalid)
	}
	if !validationIssuesContain(summary.Issues, "legacy remotion_props.json artifact is not allowed in xAI-native output") {
		t.Fatalf("Issues = %#v, want legacy remotion_props issue", summary.Issues)
	}
}

func TestRunXAIValidate_BatchValidWritesJSONAndSucceeds(t *testing.T) {
	t.Parallel()

	outputDir := t.TempDir()
	writeCompleteXAIValidateArtifacts(t, filepath.Join(outputDir, "episode_001"))
	writeCompleteXAIValidateArtifacts(t, filepath.Join(outputDir, "episode_002"))
	validator := &cmdStubOutputValidator{
		metadata: xaipipeline.RenderMetadata{
			Width:       720,
			Height:      1280,
			FPS:         24,
			DurationSec: 8,
			CodecName:   "h264",
			PixelFormat: "yuv420p",
			SizeBytes:   xaiValidateMP4SizeBytes(),
			HasAudio:    false,
		},
	}

	var out bytes.Buffer
	if err := runXAIValidate(context.Background(), outputDir, validator, &out); err != nil {
		t.Fatalf("runXAIValidate() error = %v", err)
	}

	var summary xaipipeline.BatchValidationSummary
	if err := json.Unmarshal(out.Bytes(), &summary); err != nil {
		t.Fatalf("decode stdout JSON: %v\n%s", err, out.String())
	}
	if summary.Status != xaipipeline.ValidationStatusValid || summary.TotalEpisodes != 2 || summary.ValidEpisodes != 2 {
		t.Fatalf("batch summary = %#v", summary)
	}
	if summary.StoryHash != cmdValidStoryHash {
		t.Fatalf("StoryHash = %q, want %q", summary.StoryHash, cmdValidStoryHash)
	}
	if summary.VideoModel != "grok-imagine-video" {
		t.Fatalf("VideoModel = %q, want grok-imagine-video", summary.VideoModel)
	}
}

func TestRunXAIValidate_AmbiguousSingleAndBatchRootWritesInvalidBatchJSON(t *testing.T) {
	t.Parallel()

	outputDir := t.TempDir()
	writeCompleteXAIValidateArtifacts(t, outputDir)
	writeCompleteXAIValidateArtifacts(t, filepath.Join(outputDir, "episode_001"))
	writeCompleteXAIValidateArtifacts(t, filepath.Join(outputDir, "episode_002"))
	validator := &cmdStubOutputValidator{
		metadata: xaipipeline.RenderMetadata{
			Width:       720,
			Height:      1280,
			FPS:         24,
			DurationSec: 8,
			CodecName:   "h264",
			PixelFormat: "yuv420p",
			SizeBytes:   xaiValidateMP4SizeBytes(),
			HasAudio:    false,
		},
	}

	var out bytes.Buffer
	err := runXAIValidate(context.Background(), outputDir, validator, &out)
	if err == nil {
		t.Fatal("runXAIValidate() error = nil, want invalid ambiguous batch validation")
	}
	if !strings.Contains(err.Error(), "invalid") {
		t.Fatalf("error = %q, want invalid status", err.Error())
	}

	var summary xaipipeline.BatchValidationSummary
	if decodeErr := json.Unmarshal(out.Bytes(), &summary); decodeErr != nil {
		t.Fatalf("decode stdout JSON: %v\n%s", decodeErr, out.String())
	}
	if summary.Status != xaipipeline.ValidationStatusInvalid || summary.TotalEpisodes != 2 {
		t.Fatalf("batch summary = %#v", summary)
	}
	if len(summary.Issues) == 0 || !strings.Contains(summary.Issues[0], "root xai_manifest.json") {
		t.Fatalf("Issues = %#v, want root manifest issue", summary.Issues)
	}
}

func TestRunXAIValidate_AmbiguousBatchRootDoesNotValidateRootSingleOutput(t *testing.T) {
	t.Parallel()

	outputDir := t.TempDir()
	writeCompleteXAIValidateArtifacts(t, outputDir)
	writeCompleteXAIValidateArtifacts(t, filepath.Join(outputDir, "episode_001"))
	validator := &cmdStubOutputValidator{
		metadata: xaipipeline.RenderMetadata{
			Width:       720,
			Height:      1280,
			FPS:         24,
			DurationSec: 8,
			CodecName:   "h264",
			PixelFormat: "yuv420p",
			SizeBytes:   xaiValidateMP4SizeBytes(),
			HasAudio:    false,
		},
	}

	var out bytes.Buffer
	err := runXAIValidate(context.Background(), outputDir, validator, &out)
	if err == nil {
		t.Fatal("runXAIValidate() error = nil, want invalid ambiguous batch validation")
	}

	rootOutput := filepath.Join(outputDir, "output_xai.mp4")
	for _, path := range validator.paths {
		if path == rootOutput {
			t.Fatalf("validator paths = %#v, should not validate root single output before batch detection", validator.paths)
		}
	}
	if !validationPathsContain(validator.paths, filepath.Join(outputDir, "episode_001", "output_xai.mp4")) {
		t.Fatalf("validator paths = %#v, want episode output validation", validator.paths)
	}
}

func TestRunXAIValidate_MalformedBatchEpisodeDirsWritesInvalidBatchJSON(t *testing.T) {
	t.Parallel()

	outputDir := t.TempDir()
	writeCompleteXAIValidateArtifacts(t, filepath.Join(outputDir, "episode_001"))
	if err := os.MkdirAll(filepath.Join(outputDir, "episode_1"), 0755); err != nil {
		t.Fatalf("mkdir malformed episode dir: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(outputDir, "episode_bad"), 0755); err != nil {
		t.Fatalf("mkdir malformed episode dir: %v", err)
	}
	validator := &cmdStubOutputValidator{
		metadata: xaipipeline.RenderMetadata{
			Width:       720,
			Height:      1280,
			FPS:         24,
			DurationSec: 8,
			CodecName:   "h264",
			PixelFormat: "yuv420p",
			SizeBytes:   xaiValidateMP4SizeBytes(),
			HasAudio:    false,
		},
	}

	var out bytes.Buffer
	err := runXAIValidate(context.Background(), outputDir, validator, &out)
	if err == nil {
		t.Fatal("runXAIValidate() error = nil, want invalid malformed batch validation")
	}
	if !strings.Contains(err.Error(), "invalid") {
		t.Fatalf("error = %q, want invalid status", err.Error())
	}

	var summary xaipipeline.BatchValidationSummary
	if decodeErr := json.Unmarshal(out.Bytes(), &summary); decodeErr != nil {
		t.Fatalf("decode stdout JSON: %v\n%s", decodeErr, out.String())
	}
	if summary.Status != xaipipeline.ValidationStatusInvalid || summary.TotalEpisodes != 1 || summary.ValidEpisodes != 1 {
		t.Fatalf("batch summary = %#v", summary)
	}
	for _, want := range []string{
		`malformed episode directory "episode_1": want episode_###`,
		`malformed episode directory "episode_bad": want episode_###`,
	} {
		if !validationIssuesContain(summary.Issues, want) {
			t.Fatalf("Issues = %#v, want %q", summary.Issues, want)
		}
	}
}

func TestRunXAIValidate_OnlyMalformedBatchEpisodeDirsWritesInvalidBatchJSON(t *testing.T) {
	t.Parallel()

	outputDir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(outputDir, "episode_1"), 0755); err != nil {
		t.Fatalf("mkdir malformed episode dir: %v", err)
	}
	validator := &cmdStubOutputValidator{
		metadata: xaipipeline.RenderMetadata{
			Width:       720,
			Height:      1280,
			FPS:         24,
			DurationSec: 8,
			CodecName:   "h264",
			PixelFormat: "yuv420p",
			SizeBytes:   xaiValidateMP4SizeBytes(),
			HasAudio:    false,
		},
	}

	var out bytes.Buffer
	err := runXAIValidate(context.Background(), outputDir, validator, &out)
	if err == nil {
		t.Fatal("runXAIValidate() error = nil, want invalid malformed batch validation")
	}
	if !strings.Contains(err.Error(), "invalid") {
		t.Fatalf("error = %q, want invalid status", err.Error())
	}

	var summary xaipipeline.BatchValidationSummary
	if decodeErr := json.Unmarshal(out.Bytes(), &summary); decodeErr != nil {
		t.Fatalf("decode stdout JSON: %v\n%s", decodeErr, out.String())
	}
	if summary.Status != xaipipeline.ValidationStatusInvalid || summary.TotalEpisodes != 0 {
		t.Fatalf("batch summary = %#v", summary)
	}
	if !validationIssuesContain(summary.Issues, `malformed episode directory "episode_1": want episode_###`) {
		t.Fatalf("Issues = %#v, want malformed episode issue", summary.Issues)
	}
}

func TestRunXAIValidate_SymlinkedBatchEpisodeDirWritesInvalidBatchJSON(t *testing.T) {
	t.Parallel()

	outputDir := t.TempDir()
	externalEpisode := filepath.Join(t.TempDir(), "external-episode")
	writeCompleteXAIValidateArtifacts(t, externalEpisode)
	if err := os.Symlink(externalEpisode, filepath.Join(outputDir, "episode_001")); err != nil {
		t.Skipf("symlink not available: %v", err)
	}
	validator := &cmdStubOutputValidator{
		metadata: xaipipeline.RenderMetadata{
			Width:       720,
			Height:      1280,
			FPS:         24,
			DurationSec: 8,
			CodecName:   "h264",
			PixelFormat: "yuv420p",
			SizeBytes:   xaiValidateMP4SizeBytes(),
			HasAudio:    false,
		},
	}

	var out bytes.Buffer
	err := runXAIValidate(context.Background(), outputDir, validator, &out)
	if err == nil {
		t.Fatal("runXAIValidate() error = nil, want invalid symlinked batch validation")
	}
	if !strings.Contains(err.Error(), "invalid") {
		t.Fatalf("error = %q, want invalid status", err.Error())
	}

	var summary xaipipeline.BatchValidationSummary
	if decodeErr := json.Unmarshal(out.Bytes(), &summary); decodeErr != nil {
		t.Fatalf("decode stdout JSON: %v\n%s", decodeErr, out.String())
	}
	if summary.Status != xaipipeline.ValidationStatusInvalid || summary.TotalEpisodes != 0 {
		t.Fatalf("batch summary = %#v", summary)
	}
	if !validationIssuesContain(summary.Issues, `episode directory "episode_001" is a symlink; xAI-native batch episodes must be output-local directories`) {
		t.Fatalf("Issues = %#v, want symlink episode issue", summary.Issues)
	}
}

func TestRunXAIValidate_MissingBatchEpisodeNumberWritesInvalidBatchJSON(t *testing.T) {
	t.Parallel()

	outputDir := t.TempDir()
	writeCompleteXAIValidateArtifacts(t, filepath.Join(outputDir, "episode_001"))
	writeCompleteXAIValidateArtifacts(t, filepath.Join(outputDir, "episode_003"))
	validator := &cmdStubOutputValidator{
		metadata: xaipipeline.RenderMetadata{
			Width:       720,
			Height:      1280,
			FPS:         24,
			DurationSec: 8,
			CodecName:   "h264",
			PixelFormat: "yuv420p",
			SizeBytes:   xaiValidateMP4SizeBytes(),
			HasAudio:    false,
		},
	}

	var out bytes.Buffer
	err := runXAIValidate(context.Background(), outputDir, validator, &out)
	if err == nil {
		t.Fatal("runXAIValidate() error = nil, want invalid gapped batch validation")
	}
	if !strings.Contains(err.Error(), "invalid") {
		t.Fatalf("error = %q, want invalid status", err.Error())
	}

	var summary xaipipeline.BatchValidationSummary
	if decodeErr := json.Unmarshal(out.Bytes(), &summary); decodeErr != nil {
		t.Fatalf("decode stdout JSON: %v\n%s", decodeErr, out.String())
	}
	if summary.Status != xaipipeline.ValidationStatusInvalid || summary.TotalEpisodes != 2 || summary.ValidEpisodes != 2 {
		t.Fatalf("batch summary = %#v", summary)
	}
	if !validationIssuesContain(summary.Issues, "missing episode_002 directory") {
		t.Fatalf("Issues = %#v, want missing episode issue", summary.Issues)
	}
}

func TestRunXAIValidate_BatchInvalidWritesJSONAndReturnsError(t *testing.T) {
	t.Parallel()

	outputDir := t.TempDir()
	writeCompleteXAIValidateArtifacts(t, filepath.Join(outputDir, "episode_001"))
	if err := os.MkdirAll(filepath.Join(outputDir, "episode_002"), 0755); err != nil {
		t.Fatalf("mkdir episode_002: %v", err)
	}
	writeXAIInspectJSON(t, filepath.Join(outputDir, "episode_002", "xai_manifest.json"), xaipipeline.Manifest{
		ProjectID: "partial",
		Format:    "portrait",
		FPS:       24,
		Width:     720,
		Height:    1280,
		Shots:     []xaipipeline.Shot{{Index: 1, Prompt: "shot", DurationSec: 8}},
	})
	validator := &cmdStubOutputValidator{
		metadata: xaipipeline.RenderMetadata{
			Width:       720,
			Height:      1280,
			FPS:         24,
			DurationSec: 8,
			CodecName:   "h264",
			PixelFormat: "yuv420p",
			SizeBytes:   xaiValidateMP4SizeBytes(),
			HasAudio:    false,
		},
	}

	var out bytes.Buffer
	err := runXAIValidate(context.Background(), outputDir, validator, &out)
	if err == nil {
		t.Fatal("runXAIValidate() error = nil, want invalid batch validation error")
	}
	if !strings.Contains(err.Error(), "invalid") {
		t.Fatalf("error = %q, want invalid status", err.Error())
	}

	var summary xaipipeline.BatchValidationSummary
	if decodeErr := json.Unmarshal(out.Bytes(), &summary); decodeErr != nil {
		t.Fatalf("decode stdout JSON: %v\n%s", decodeErr, out.String())
	}
	if summary.Status != xaipipeline.ValidationStatusInvalid || summary.InvalidEpisodes != 1 {
		t.Fatalf("batch summary = %#v", summary)
	}
}

type cmdStubOutputValidator struct {
	metadata xaipipeline.RenderMetadata
	err      error
	paths    []string
}

func (s *cmdStubOutputValidator) Validate(_ context.Context, path string, _ xaipipeline.RenderValidationSpec) (xaipipeline.RenderMetadata, error) {
	s.paths = append(s.paths, path)
	metadata := s.metadata
	metadata.Path = path
	return metadata, s.err
}

func validationIssuesContain(issues []string, want string) bool {
	for _, issue := range issues {
		if issue == want {
			return true
		}
	}
	return false
}

func validationPathsContain(paths []string, want string) bool {
	for _, path := range paths {
		if path == want {
			return true
		}
	}
	return false
}

func writeCompleteXAIValidateArtifacts(t *testing.T, outputDir string) {
	t.Helper()

	if err := os.MkdirAll(outputDir, 0755); err != nil {
		t.Fatalf("mkdir %s: %v", outputDir, err)
	}
	promptHash := cmdValidationPromptHash("shot", 8, "9:16", "720p")
	manifest := xaipipeline.Manifest{
		ProjectID:  "cmd-validate",
		StoryHash:  cmdValidStoryHash,
		VideoModel: "grok-imagine-video",
		Format:     "portrait",
		FPS:        24,
		Width:      720,
		Height:     1280,
		Shots: []xaipipeline.Shot{
			{
				Index:         1,
				Prompt:        "shot",
				PromptHash:    promptHash,
				XAIRequestID:  "req_123",
				XAIStatus:     "done",
				DurationSec:   8,
				VideoPath:     "shots/shot_001.mp4",
				TransitionOut: "cut",
			},
		},
	}
	writeXAIInspectJSON(t, filepath.Join(outputDir, "xai_manifest.json"), manifest)
	writeXAIInspectJSON(t, filepath.Join(outputDir, "xai_run_metadata.json"), xaipipeline.RunMetadata{
		Planned:        true,
		VideoModel:     "grok-imagine-video",
		GeneratedShots: []int{1},
		ShotDecisions: []xaipipeline.ShotDecision{
			{
				Index:        1,
				Decision:     "generated",
				VideoPath:    "shots/shot_001.mp4",
				PromptHash:   promptHash,
				XAIRequestID: "req_123",
				XAIStatus:    "done",
			},
		},
	})
	writeXAIInspectJSON(t, filepath.Join(outputDir, "render_metadata.json"), xaipipeline.RenderMetadata{
		Path:         filepath.Join(outputDir, "output_xai.mp4"),
		ProjectID:    manifest.ProjectID,
		ManifestHash: cmdValidationManifestHash(manifest),
		Width:        720,
		Height:       1280,
		FPS:          24,
		DurationSec:  8,
		CodecName:    "h264",
		PixelFormat:  "yuv420p",
		SizeBytes:    xaiValidateMP4SizeBytes(),
		HasAudio:     false,
	})
	writeXAIValidateMP4(t, filepath.Join(outputDir, "shots", "shot_001.mp4"))
	writeXAIValidateMP4(t, filepath.Join(outputDir, "normalized", "shot_001.mp4"))
	writeXAIValidateHyperFramesIndex(t, filepath.Join(outputDir, "hyperframes", "index.html"), 1)
	writeXAIValidateHyperFramesPackage(t, filepath.Join(outputDir, "hyperframes", "package.json"))
	writeXAIValidateMP4(t, filepath.Join(outputDir, "timeline_hyperframes.mp4"))
	writeXAIValidateMP4(t, filepath.Join(outputDir, "output_xai.mp4"))
	writeXAIValidateJPEG(t, filepath.Join(outputDir, "preview_frame.jpg"))
}

func cmdValidationManifestHash(manifest xaipipeline.Manifest) string {
	data, err := json.Marshal(manifest)
	if err != nil {
		panic(err)
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func cmdValidationPromptHash(prompt string, duration float64, aspectRatio string, resolution string) string {
	sum := sha256.Sum256([]byte(fmt.Sprintf(
		"%s\n%s\n%.3f\n%s\n%s",
		"grok-imagine-video",
		strings.TrimSpace(prompt),
		duration,
		strings.TrimSpace(aspectRatio),
		strings.TrimSpace(resolution),
	)))
	return hex.EncodeToString(sum[:])
}

func writeXAIValidateJPEG(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatalf("mkdir %s: %v", filepath.Dir(path), err)
	}
	file, err := os.Create(path)
	if err != nil {
		t.Fatalf("create jpeg %s: %v", path, err)
	}
	defer func() {
		if err := file.Close(); err != nil {
			t.Fatalf("close jpeg %s: %v", path, err)
		}
	}()
	if err := jpeg.Encode(file, image.NewRGBA(image.Rect(0, 0, 720, 1280)), nil); err != nil {
		t.Fatalf("write jpeg %s: %v", path, err)
	}
}

func writeXAIValidateMP4(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatalf("mkdir %s: %v", filepath.Dir(path), err)
	}
	data := xaiValidateMP4Bytes()
	if err := os.WriteFile(path, data, 0644); err != nil {
		t.Fatalf("write mp4 %s: %v", path, err)
	}
}

func xaiValidateMP4SizeBytes() int64 {
	return int64(len(xaiValidateMP4Bytes()))
}

func xaiValidateMP4Bytes() []byte {
	return []byte{
		0x00, 0x00, 0x00, 0x18,
		'f', 't', 'y', 'p',
		'i', 's', 'o', 'm',
		0x00, 0x00, 0x00, 0x00,
		'i', 's', 'o', 'm',
	}
}

func writeXAIValidateHyperFramesIndex(t *testing.T, path string, shotIndexes ...int) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatalf("mkdir %s: %v", filepath.Dir(path), err)
	}

	var html strings.Builder
	fmt.Fprintf(&html, `<!DOCTYPE html><html><body><div data-composition-id="xai-video" data-width="720" data-height="1280" data-duration="%.3f">`, float64(len(shotIndexes))*8)
	start := 0.0
	for trackIndex, index := range shotIndexes {
		fmt.Fprintf(&html, `<video id="video-%d" class="shot-video clip" src="../normalized/shot_%03d.mp4" data-start="%.3f" data-duration="8.000" data-track-index="%d" data-transition-out="cut"></video>`, index, index, start, trackIndex*2)
		start += 8
	}
	html.WriteString(`</div><script>var shots = [`)
	start = 0
	for _, index := range shotIndexes {
		fmt.Fprintf(&html, `{ video: document.getElementById("video-%d"), subtitle: document.getElementById("subtitle-%d"), start: %.3f, duration: 8.000 },`, index, index, start)
		start += 8
	}
	html.WriteString(`]; function applyShotVisibility(){ var fadeSeconds = 0.4; var node = { style: {} }; node.style.opacity = "1"; } function seek(t){ t = Number(t) || 0; for (var i = 0; i < shots.length; i++) { var shot = shots[i]; var start = Number(shot.start); var duration = Number(shot.duration); var local = t - start; var target = Math.max(0, Math.min(local, Math.max(0, duration - 0.001))); shot.video.currentTime = target; } } var timeline = { seek: function (t) { seek(t); return timeline; } }; window.__timelines = window.__timelines || {}; window.__timelines["xai-video"] = timeline; window.__hf = { seek: timeline.seek };</script></body></html>`)
	if err := os.WriteFile(path, []byte(html.String()), 0644); err != nil {
		t.Fatalf("write hyperframes index %s: %v", path, err)
	}
}

func writeXAIValidateHyperFramesPackage(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatalf("mkdir %s: %v", filepath.Dir(path), err)
	}
	const packageJSON = `{"name":"xai-video-timeline","version":"1.0.0","private":true}`
	if err := os.WriteFile(path, []byte(packageJSON), 0644); err != nil {
		t.Fatalf("write hyperframes package %s: %v", path, err)
	}
}
