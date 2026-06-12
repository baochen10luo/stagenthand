package xaipipeline_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/baochen10luo/stagenthand/internal/xaipipeline"
)

type stubPlanner struct {
	calls     int
	input     xaipipeline.PlanInput
	out       xaipipeline.Manifest
	err       error
	afterPlan func()
}

func (s *stubPlanner) Plan(_ context.Context, input xaipipeline.PlanInput) (xaipipeline.Manifest, error) {
	s.calls++
	s.input = input
	if s.afterPlan != nil {
		s.afterPlan()
	}
	return s.out, s.err
}

type stubShotGenerator struct {
	calls         []xaipipeline.Shot
	data          []byte
	dataByIndex   map[int][]byte
	requestIDs    map[int]string
	statuses      map[int]string
	err           error
	omitMetadata  bool
	afterGenerate func(shot xaipipeline.Shot)
}

func (s *stubShotGenerator) GenerateShot(_ context.Context, shot xaipipeline.Shot) ([]byte, error) {
	result, err := s.GenerateShotResult(context.Background(), shot)
	return result.Data, err
}

func (s *stubShotGenerator) GenerateShotResult(_ context.Context, shot xaipipeline.Shot) (xaipipeline.ShotGenerationResult, error) {
	s.calls = append(s.calls, shot)
	if s.err != nil {
		return xaipipeline.ShotGenerationResult{}, s.err
	}
	requestID := s.requestIDs[shot.Index]
	status := s.statuses[shot.Index]
	if !s.omitMetadata {
		if requestID == "" {
			requestID = "req_stub"
		}
		if status == "" {
			status = "done"
		}
	}
	data := s.data
	if s.dataByIndex != nil {
		data = s.dataByIndex[shot.Index]
	}
	if s.afterGenerate != nil {
		s.afterGenerate(shot)
	}
	return xaipipeline.ShotGenerationResult{
		Data:      data,
		RequestID: requestID,
		Status:    status,
	}, nil
}

type stubRenderer struct {
	calls     int
	manifest  xaipipeline.Manifest
	outputDir string
	err       error
}

func (s *stubRenderer) Render(_ context.Context, manifest xaipipeline.Manifest, outputDir string) (string, error) {
	s.calls++
	s.manifest = manifest
	s.outputDir = outputDir
	if s.err != nil {
		return "", s.err
	}
	return filepath.Join(outputDir, "output_xai.mp4"), nil
}

type stubValidator struct {
	valid  map[string]bool
	fn     func(path string) bool
	specFn func(path string, spec xaipipeline.RenderValidationSpec) bool
}

func (s stubValidator) ValidShot(_ context.Context, path string, spec xaipipeline.RenderValidationSpec) bool {
	if s.specFn != nil {
		return s.specFn(path, spec)
	}
	if s.fn != nil {
		return s.fn(path)
	}
	if s.valid != nil {
		if valid, ok := s.valid[path]; ok {
			return valid
		}
	}
	info, err := os.Stat(path)
	return err == nil && !info.IsDir() && info.Size() > 0
}

func TestOrchestrator_RunPlansGeneratesShotsAndRenders(t *testing.T) {
	t.Parallel()

	outputDir := t.TempDir()
	planner := &stubPlanner{out: xaipipeline.Manifest{
		ProjectID: "last-glow",
		Shots: []xaipipeline.Shot{
			{Index: 1, Prompt: "wide wasteland", Subtitle: "first"},
			{Index: 2, Prompt: "robot finds flower", Subtitle: "second"},
		},
	}}
	generator := &stubShotGenerator{
		data:       []byte("mp4-bytes"),
		requestIDs: map[int]string{1: "req_001", 2: "req_002"},
		statuses:   map[int]string{1: "done", 2: "done"},
	}
	renderer := &stubRenderer{}

	orch := xaipipeline.NewOrchestrator(xaipipeline.Deps{
		Planner:       planner,
		ShotGenerator: generator,
		Renderer:      renderer,
		Validator:     stubValidator{},
	})

	result, err := orch.Run(context.Background(), []byte("一個機器人找到最後一朵花"), xaipipeline.RunOptions{
		OutputDir:   outputDir,
		TargetShots: 2,
		Format:      " PORTRAIT ",
	})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	if planner.calls != 1 {
		t.Fatalf("planner calls = %d, want 1", planner.calls)
	}
	if planner.input.TargetShots != 2 {
		t.Fatalf("planner target shots = %d, want 2", planner.input.TargetShots)
	}
	if planner.input.Format != "portrait" {
		t.Fatalf("planner format = %q, want portrait", planner.input.Format)
	}
	if len(generator.calls) != 2 {
		t.Fatalf("generator calls = %d, want 2", len(generator.calls))
	}
	if generator.calls[0].Prompt != "wide wasteland" {
		t.Fatalf("first prompt = %q", generator.calls[0].Prompt)
	}
	if renderer.calls != 1 {
		t.Fatalf("renderer calls = %d, want 1", renderer.calls)
	}
	if renderer.outputDir != outputDir {
		t.Fatalf("renderer output dir = %q, want %q", renderer.outputDir, outputDir)
	}

	wantShot := filepath.Join(outputDir, "shots", "shot_001.mp4")
	if got := result.Manifest.Shots[0].VideoPath; got != "shots/shot_001.mp4" {
		t.Fatalf("manifest shot video path = %q, want shots/shot_001.mp4", got)
	}
	if result.Manifest.Shots[0].XAIRequestID != "req_001" {
		t.Fatalf("xai_request_id = %q, want req_001", result.Manifest.Shots[0].XAIRequestID)
	}
	if result.Manifest.Shots[0].XAIStatus != "done" {
		t.Fatalf("xai_status = %q, want done", result.Manifest.Shots[0].XAIStatus)
	}
	if data, err := os.ReadFile(wantShot); err != nil || string(data) != "mp4-bytes" {
		t.Fatalf("shot file = %q err=%v data=%q", wantShot, err, string(data))
	}
	if _, err := os.Stat(filepath.Join(outputDir, "xai_manifest.json")); err != nil {
		t.Fatalf("xai_manifest.json missing: %v", err)
	}
	if _, err := os.Stat(filepath.Join(outputDir, "remotion_props.json")); !os.IsNotExist(err) {
		t.Fatalf("xAI-native pipeline should not write remotion_props.json, stat err=%v", err)
	}
	if result.OutputVideo != filepath.Join(outputDir, "output_xai.mp4") {
		t.Fatalf("output video = %q", result.OutputVideo)
	}
	if result.OutputDir != outputDir {
		t.Fatalf("output dir = %q, want %q", result.OutputDir, outputDir)
	}
	if result.Manifest.StoryHash == "" {
		t.Fatal("manifest story_hash is empty")
	}
	if !result.RunMetadata.Planned {
		t.Fatal("run metadata planned = false, want true")
	}
	if result.RunMetadata.ManifestReused {
		t.Fatal("run metadata manifest_reused = true, want false")
	}
	assertEqualInts(t, result.RunMetadata.GeneratedShots, []int{1, 2})
	assertEqualInts(t, result.RunMetadata.ReusedShots, nil)
	assertShotDecision(t, result.RunMetadata.ShotDecisions[0], xaipipeline.ShotDecision{
		Index:        1,
		Decision:     "generated",
		VideoPath:    "shots/shot_001.mp4",
		PromptHash:   result.Manifest.Shots[0].PromptHash,
		XAIRequestID: "req_001",
		XAIStatus:    "done",
	})
	if result.ManifestPath != filepath.Join(outputDir, "xai_manifest.json") {
		t.Fatalf("manifest path = %q", result.ManifestPath)
	}
	if result.RunMetadataPath != filepath.Join(outputDir, "xai_run_metadata.json") {
		t.Fatalf("run metadata path = %q", result.RunMetadataPath)
	}
	if result.RenderMetadataPath != filepath.Join(outputDir, "render_metadata.json") {
		t.Fatalf("render metadata path = %q", result.RenderMetadataPath)
	}
	if result.PreviewFramePath != filepath.Join(outputDir, "preview_frame.jpg") {
		t.Fatalf("preview frame path = %q", result.PreviewFramePath)
	}
}

func TestOrchestrator_RunNilContextUsesBackground(t *testing.T) {
	t.Parallel()

	outputDir := t.TempDir()
	planner := &stubPlanner{out: xaipipeline.Manifest{
		ProjectID: "nil-context",
		Shots: []xaipipeline.Shot{
			{Index: 1, Prompt: "wide wasteland"},
		},
	}}
	generator := &stubShotGenerator{
		data:       []byte("mp4-bytes"),
		requestIDs: map[int]string{1: "req_001"},
		statuses:   map[int]string{1: "done"},
	}
	renderer := &stubRenderer{}
	orch := xaipipeline.NewOrchestrator(xaipipeline.Deps{
		Planner:       planner,
		ShotGenerator: generator,
		Renderer:      renderer,
		Validator:     stubValidator{},
	})

	result, err := orch.Run(nil, []byte("story"), xaipipeline.RunOptions{
		OutputDir:   outputDir,
		TargetShots: 1,
		Format:      "portrait",
	})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if result == nil || result.OutputDir != outputDir {
		t.Fatalf("Run() result = %+v, want output dir %q", result, outputDir)
	}
	if planner.calls != 1 {
		t.Fatalf("planner calls = %d, want 1", planner.calls)
	}
	if renderer.calls != 1 {
		t.Fatalf("renderer calls = %d, want 1", renderer.calls)
	}
}

func TestOrchestrator_RunNormalizesPlannerSubtitles(t *testing.T) {
	t.Parallel()

	outputDir := t.TempDir()
	planner := &stubPlanner{out: xaipipeline.Manifest{
		ProjectID: "subtitle-trim",
		Shots: []xaipipeline.Shot{
			{Index: 1, Prompt: "wide wasteland", Subtitle: " \n 第一個字幕 \t "},
		},
	}}
	generator := &stubShotGenerator{
		data:       []byte("mp4-bytes"),
		requestIDs: map[int]string{1: "req_001"},
		statuses:   map[int]string{1: "done"},
	}
	renderer := &stubRenderer{}

	orch := xaipipeline.NewOrchestrator(xaipipeline.Deps{
		Planner:       planner,
		ShotGenerator: generator,
		Renderer:      renderer,
		Validator:     stubValidator{},
	})

	result, err := orch.Run(context.Background(), []byte("story"), xaipipeline.RunOptions{
		OutputDir:   outputDir,
		TargetShots: 1,
		Format:      "portrait",
	})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	if got := generator.calls[0].Subtitle; got != "第一個字幕" {
		t.Fatalf("generator subtitle = %q, want trimmed subtitle", got)
	}
	if got := renderer.manifest.Shots[0].Subtitle; got != "第一個字幕" {
		t.Fatalf("renderer subtitle = %q, want trimmed subtitle", got)
	}
	if got := result.Manifest.Shots[0].Subtitle; got != "第一個字幕" {
		t.Fatalf("result manifest subtitle = %q, want trimmed subtitle", got)
	}

	var persisted xaipipeline.Manifest
	data, err := os.ReadFile(filepath.Join(outputDir, "xai_manifest.json"))
	if err != nil {
		t.Fatalf("read xai_manifest.json: %v", err)
	}
	if err := json.Unmarshal(data, &persisted); err != nil {
		t.Fatalf("unmarshal xai_manifest.json: %v", err)
	}
	if got := persisted.Shots[0].Subtitle; got != "第一個字幕" {
		t.Fatalf("persisted manifest subtitle = %q, want trimmed subtitle", got)
	}
}

func TestOrchestrator_RunNormalizesShotTransitionsBeforeGeneration(t *testing.T) {
	t.Parallel()

	outputDir := t.TempDir()
	planner := &stubPlanner{out: xaipipeline.Manifest{
		ProjectID: "transition-normalize",
		Shots: []xaipipeline.Shot{
			{Index: 1, Prompt: "wide shot"},
			{Index: 2, Prompt: "close shot", TransitionOut: " Fade "},
		},
	}}
	generator := &stubShotGenerator{data: []byte("mp4-bytes")}
	renderer := &stubRenderer{}
	orch := xaipipeline.NewOrchestrator(xaipipeline.Deps{
		Planner:       planner,
		ShotGenerator: generator,
		Renderer:      renderer,
		Validator:     stubValidator{},
	})

	result, err := orch.Run(context.Background(), []byte("story"), xaipipeline.RunOptions{
		OutputDir:   outputDir,
		TargetShots: 2,
		Format:      "portrait",
	})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	if got := generator.calls[0].TransitionOut; got != "cut" {
		t.Fatalf("generated shot 1 transition_out = %q, want cut", got)
	}
	if got := generator.calls[1].TransitionOut; got != "fade" {
		t.Fatalf("generated shot 2 transition_out = %q, want fade", got)
	}
	if got := renderer.manifest.Shots[0].TransitionOut; got != "cut" {
		t.Fatalf("rendered shot 1 transition_out = %q, want cut", got)
	}
	if got := renderer.manifest.Shots[1].TransitionOut; got != "fade" {
		t.Fatalf("rendered shot 2 transition_out = %q, want fade", got)
	}
	if got := result.Manifest.Shots[0].TransitionOut; got != "cut" {
		t.Fatalf("result shot 1 transition_out = %q, want cut", got)
	}
}

func TestOrchestrator_RunRejectsUnsupportedShotTransitionBeforeGeneration(t *testing.T) {
	t.Parallel()

	planner := &stubPlanner{out: xaipipeline.Manifest{
		ProjectID: "bad-transition",
		Shots: []xaipipeline.Shot{
			{Index: 1, Prompt: "wide shot", TransitionOut: "spin"},
		},
	}}
	generator := &stubShotGenerator{data: []byte("mp4-bytes")}
	renderer := &stubRenderer{}
	orch := xaipipeline.NewOrchestrator(xaipipeline.Deps{
		Planner:       planner,
		ShotGenerator: generator,
		Renderer:      renderer,
		Validator:     stubValidator{},
	})

	_, err := orch.Run(context.Background(), []byte("story"), xaipipeline.RunOptions{
		OutputDir: t.TempDir(),
		Format:    "portrait",
	})
	if err == nil {
		t.Fatal("Run() error = nil, want unsupported transition error")
	}
	if !strings.Contains(err.Error(), `transition_out "spin"`) {
		t.Fatalf("Run() error = %v, want unsupported transition_out error", err)
	}
	if len(generator.calls) != 0 {
		t.Fatalf("generator calls = %d, want 0 before transition validation", len(generator.calls))
	}
	if renderer.calls != 0 {
		t.Fatalf("renderer calls = %d, want 0 before transition validation", renderer.calls)
	}
}

func TestOrchestrator_RunDefaultsOutputDirToProjectUnderShandHome(t *testing.T) {
	t.Parallel()

	shandHome := t.TempDir()
	planner := &stubPlanner{out: xaipipeline.Manifest{
		ProjectID: "default-output",
		Shots: []xaipipeline.Shot{
			{Index: 1, Prompt: "single shot"},
		},
	}}
	generator := &stubShotGenerator{data: []byte("mp4-bytes")}
	renderer := &stubRenderer{}
	orch := xaipipeline.NewOrchestrator(xaipipeline.Deps{
		Planner:       planner,
		ShotGenerator: generator,
		Renderer:      renderer,
		Validator:     stubValidator{},
	})

	result, err := orch.Run(context.Background(), []byte("story"), xaipipeline.RunOptions{
		ShandHome: shandHome,
	})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	wantOutputDir := filepath.Join(shandHome, "projects", "default-output")
	if renderer.outputDir != wantOutputDir {
		t.Fatalf("renderer output dir = %q, want %q", renderer.outputDir, wantOutputDir)
	}
	if result.OutputVideo != filepath.Join(wantOutputDir, "output_xai.mp4") {
		t.Fatalf("output video = %q", result.OutputVideo)
	}
	if _, err := os.Stat(filepath.Join(wantOutputDir, "xai_manifest.json")); err != nil {
		t.Fatalf("xai_manifest.json missing: %v", err)
	}
	if metadata := readRunMetadata(t, wantOutputDir); !metadata.Planned || metadata.ManifestReused {
		t.Fatalf("run metadata = %+v, want planned non-reused run", metadata)
	}
}

func TestOrchestrator_RunRejectsUnsafeProjectIDBeforeDefaultOutputDir(t *testing.T) {
	t.Parallel()

	shandHome := t.TempDir()
	planner := &stubPlanner{out: xaipipeline.Manifest{
		ProjectID: "../escaped",
		Shots: []xaipipeline.Shot{
			{Index: 1, Prompt: "single shot"},
		},
	}}
	generator := &stubShotGenerator{data: []byte("mp4-bytes")}
	renderer := &stubRenderer{}
	orch := xaipipeline.NewOrchestrator(xaipipeline.Deps{
		Planner:       planner,
		ShotGenerator: generator,
		Renderer:      renderer,
		Validator:     stubValidator{},
	})

	_, err := orch.Run(context.Background(), []byte("story"), xaipipeline.RunOptions{
		ShandHome: shandHome,
	})
	if err == nil {
		t.Fatal("Run() error = nil, want unsafe project_id error")
	}
	if !strings.Contains(err.Error(), "project_id") {
		t.Fatalf("Run() error = %v, want project_id error", err)
	}
	if len(generator.calls) != 0 {
		t.Fatalf("generator calls = %d, want 0", len(generator.calls))
	}
	if renderer.calls != 0 {
		t.Fatalf("renderer calls = %d, want 0", renderer.calls)
	}
	if _, err := os.Stat(filepath.Join(shandHome, "escaped")); !os.IsNotExist(err) {
		t.Fatalf("escaped output dir was created, stat err=%v", err)
	}
}

func TestOrchestrator_RunRejectsSymlinkedShandHomeBeforePlanning(t *testing.T) {
	t.Parallel()

	externalHome := t.TempDir()
	parent := t.TempDir()
	shandHome := filepath.Join(parent, "shand-home")
	if err := os.Symlink(externalHome, shandHome); err != nil {
		t.Skipf("symlink not available: %v", err)
	}

	planner := &stubPlanner{out: xaipipeline.Manifest{
		ProjectID: "default-output",
		Shots: []xaipipeline.Shot{
			{Index: 1, Prompt: "single shot"},
		},
	}}
	generator := &stubShotGenerator{data: []byte("mp4-bytes")}
	renderer := &stubRenderer{}
	orch := xaipipeline.NewOrchestrator(xaipipeline.Deps{
		Planner:       planner,
		ShotGenerator: generator,
		Renderer:      renderer,
		Validator:     stubValidator{},
	})

	_, err := orch.Run(context.Background(), []byte("story"), xaipipeline.RunOptions{
		ShandHome: shandHome,
	})
	if err == nil {
		t.Fatal("Run() error = nil, want symlinked shand home error")
	}
	if !strings.Contains(err.Error(), "xai output base dir") || !strings.Contains(err.Error(), "symlink") {
		t.Fatalf("Run() error = %v, want symlinked shand home output base error", err)
	}
	if planner.calls != 0 {
		t.Fatalf("planner calls = %d, want 0", planner.calls)
	}
	if len(generator.calls) != 0 {
		t.Fatalf("generator calls = %d, want 0", len(generator.calls))
	}
	if renderer.calls != 0 {
		t.Fatalf("renderer calls = %d, want 0", renderer.calls)
	}
	if _, statErr := os.Stat(filepath.Join(externalHome, "projects")); !os.IsNotExist(statErr) {
		t.Fatalf("external projects dir should not be created through symlinked shand home, stat err=%v", statErr)
	}
}

func TestOrchestrator_RunRejectsSymlinkedShandHomeAncestorBeforePlanning(t *testing.T) {
	t.Parallel()

	externalAncestor := t.TempDir()
	parent := t.TempDir()
	linkedAncestor := filepath.Join(parent, "linked-ancestor")
	if err := os.Symlink(externalAncestor, linkedAncestor); err != nil {
		t.Skipf("symlink not available: %v", err)
	}
	shandHome := filepath.Join(linkedAncestor, "missing-shand-home")

	planner := &stubPlanner{out: xaipipeline.Manifest{
		ProjectID: "default-output",
		Shots: []xaipipeline.Shot{
			{Index: 1, Prompt: "single shot"},
		},
	}}
	generator := &stubShotGenerator{data: []byte("mp4-bytes")}
	renderer := &stubRenderer{}
	orch := xaipipeline.NewOrchestrator(xaipipeline.Deps{
		Planner:       planner,
		ShotGenerator: generator,
		Renderer:      renderer,
		Validator:     stubValidator{},
	})

	_, err := orch.Run(context.Background(), []byte("story"), xaipipeline.RunOptions{
		ShandHome: shandHome,
	})
	if err == nil {
		t.Fatal("Run() error = nil, want symlinked shand home ancestor error")
	}
	if !strings.Contains(err.Error(), "xai output base dir") || !strings.Contains(err.Error(), "symlink") {
		t.Fatalf("Run() error = %v, want symlinked shand home ancestor output base error", err)
	}
	if planner.calls != 0 {
		t.Fatalf("planner calls = %d, want 0", planner.calls)
	}
	if len(generator.calls) != 0 {
		t.Fatalf("generator calls = %d, want 0", len(generator.calls))
	}
	if renderer.calls != 0 {
		t.Fatalf("renderer calls = %d, want 0", renderer.calls)
	}
	if _, statErr := os.Stat(filepath.Join(externalAncestor, "missing-shand-home")); !os.IsNotExist(statErr) {
		t.Fatalf("external shand home should not be created through symlink ancestor, stat err=%v", statErr)
	}
}

func TestOrchestrator_RunRejectsSymlinkedOutputDirBeforePlanning(t *testing.T) {
	t.Parallel()

	externalOutput := t.TempDir()
	parent := t.TempDir()
	outputDir := filepath.Join(parent, "xai-output")
	if err := os.Symlink(externalOutput, outputDir); err != nil {
		t.Skipf("symlink not available: %v", err)
	}

	planner := &stubPlanner{out: xaipipeline.Manifest{
		ProjectID: "symlink-output",
		Shots: []xaipipeline.Shot{
			{Index: 1, Prompt: "single shot"},
		},
	}}
	generator := &stubShotGenerator{data: []byte("mp4-bytes")}
	renderer := &stubRenderer{}
	orch := xaipipeline.NewOrchestrator(xaipipeline.Deps{
		Planner:       planner,
		ShotGenerator: generator,
		Renderer:      renderer,
		Validator:     stubValidator{},
	})

	_, err := orch.Run(context.Background(), []byte("story"), xaipipeline.RunOptions{OutputDir: outputDir})
	if err == nil {
		t.Fatal("Run() error = nil, want symlinked output dir error")
	}
	if !strings.Contains(err.Error(), "xai output dir") || !strings.Contains(err.Error(), "symlink") {
		t.Fatalf("Run() error = %v, want symlink output dir error", err)
	}
	if planner.calls != 0 {
		t.Fatalf("planner calls = %d, want 0", planner.calls)
	}
	if len(generator.calls) != 0 {
		t.Fatalf("generator calls = %d, want 0", len(generator.calls))
	}
	if renderer.calls != 0 {
		t.Fatalf("renderer calls = %d, want 0", renderer.calls)
	}
	if _, statErr := os.Stat(filepath.Join(externalOutput, "xai_manifest.json")); !os.IsNotExist(statErr) {
		t.Fatalf("external output should not receive artifacts, stat err=%v", statErr)
	}
}

func TestOrchestrator_RunRejectsExistingFileOutputDirBeforePlanning(t *testing.T) {
	t.Parallel()

	outputDir := filepath.Join(t.TempDir(), "xai-output")
	if err := os.WriteFile(outputDir, []byte("not a directory"), 0644); err != nil {
		t.Fatalf("write output file: %v", err)
	}

	planner := &stubPlanner{out: xaipipeline.Manifest{
		ProjectID: "file-output",
		Shots: []xaipipeline.Shot{
			{Index: 1, Prompt: "single shot"},
		},
	}}
	generator := &stubShotGenerator{data: []byte("mp4-bytes")}
	renderer := &stubRenderer{}
	orch := xaipipeline.NewOrchestrator(xaipipeline.Deps{
		Planner:       planner,
		ShotGenerator: generator,
		Renderer:      renderer,
		Validator:     stubValidator{},
	})

	_, err := orch.Run(context.Background(), []byte("story"), xaipipeline.RunOptions{OutputDir: outputDir})
	if err == nil {
		t.Fatal("Run() error = nil, want file output dir error")
	}
	if !strings.Contains(err.Error(), "xai output dir") || !strings.Contains(err.Error(), "not a directory") {
		t.Fatalf("Run() error = %v, want file output dir error", err)
	}
	if planner.calls != 0 {
		t.Fatalf("planner calls = %d, want 0", planner.calls)
	}
	if len(generator.calls) != 0 {
		t.Fatalf("generator calls = %d, want 0", len(generator.calls))
	}
	if renderer.calls != 0 {
		t.Fatalf("renderer calls = %d, want 0", renderer.calls)
	}
}

func TestOrchestrator_RunRejectsOutputDirUnderFileParentBeforePlanning(t *testing.T) {
	t.Parallel()

	parent := t.TempDir()
	fileParent := filepath.Join(parent, "not-a-dir")
	if err := os.WriteFile(fileParent, []byte("not a directory"), 0644); err != nil {
		t.Fatalf("write parent file: %v", err)
	}
	outputDir := filepath.Join(fileParent, "xai-output")

	planner := &stubPlanner{out: xaipipeline.Manifest{
		ProjectID: "file-parent-output",
		Shots: []xaipipeline.Shot{
			{Index: 1, Prompt: "single shot"},
		},
	}}
	generator := &stubShotGenerator{data: []byte("mp4-bytes")}
	renderer := &stubRenderer{}
	orch := xaipipeline.NewOrchestrator(xaipipeline.Deps{
		Planner:       planner,
		ShotGenerator: generator,
		Renderer:      renderer,
		Validator:     stubValidator{},
	})

	_, err := orch.Run(context.Background(), []byte("story"), xaipipeline.RunOptions{OutputDir: outputDir})
	if err == nil {
		t.Fatal("Run() error = nil, want file parent output dir error")
	}
	if !strings.Contains(err.Error(), "xai output dir") || !strings.Contains(err.Error(), "not a directory") {
		t.Fatalf("Run() error = %v, want file parent output dir error", err)
	}
	if planner.calls != 0 {
		t.Fatalf("planner calls = %d, want 0", planner.calls)
	}
	if len(generator.calls) != 0 {
		t.Fatalf("generator calls = %d, want 0", len(generator.calls))
	}
	if renderer.calls != 0 {
		t.Fatalf("renderer calls = %d, want 0", renderer.calls)
	}
}

func TestOrchestrator_RunRejectsShandHomeUnderFileParentBeforePlanning(t *testing.T) {
	t.Parallel()

	parent := t.TempDir()
	fileParent := filepath.Join(parent, "not-a-dir")
	if err := os.WriteFile(fileParent, []byte("not a directory"), 0644); err != nil {
		t.Fatalf("write parent file: %v", err)
	}
	shandHome := filepath.Join(fileParent, "shand-home")

	planner := &stubPlanner{out: xaipipeline.Manifest{
		ProjectID: "default-output",
		Shots: []xaipipeline.Shot{
			{Index: 1, Prompt: "single shot"},
		},
	}}
	generator := &stubShotGenerator{data: []byte("mp4-bytes")}
	renderer := &stubRenderer{}
	orch := xaipipeline.NewOrchestrator(xaipipeline.Deps{
		Planner:       planner,
		ShotGenerator: generator,
		Renderer:      renderer,
		Validator:     stubValidator{},
	})

	_, err := orch.Run(context.Background(), []byte("story"), xaipipeline.RunOptions{
		ShandHome: shandHome,
	})
	if err == nil {
		t.Fatal("Run() error = nil, want file parent shand home error")
	}
	if !strings.Contains(err.Error(), "xai output base dir") || !strings.Contains(err.Error(), "not a directory") {
		t.Fatalf("Run() error = %v, want file parent output base error", err)
	}
	if planner.calls != 0 {
		t.Fatalf("planner calls = %d, want 0", planner.calls)
	}
	if len(generator.calls) != 0 {
		t.Fatalf("generator calls = %d, want 0", len(generator.calls))
	}
	if renderer.calls != 0 {
		t.Fatalf("renderer calls = %d, want 0", renderer.calls)
	}
}

func TestOrchestrator_RunForceReplanRejectsSymlinkedOutputDirBeforePlanning(t *testing.T) {
	t.Parallel()

	externalOutput := t.TempDir()
	parent := t.TempDir()
	outputDir := filepath.Join(parent, "xai-output")
	if err := os.Symlink(externalOutput, outputDir); err != nil {
		t.Skipf("symlink not available: %v", err)
	}

	planner := &stubPlanner{out: xaipipeline.Manifest{
		ProjectID: "force-replan-symlink-output",
		Shots: []xaipipeline.Shot{
			{Index: 1, Prompt: "single shot"},
		},
	}}
	generator := &stubShotGenerator{data: []byte("mp4-bytes")}
	renderer := &stubRenderer{}
	orch := xaipipeline.NewOrchestrator(xaipipeline.Deps{
		Planner:       planner,
		ShotGenerator: generator,
		Renderer:      renderer,
		Validator:     stubValidator{},
	})

	_, err := orch.Run(context.Background(), []byte("story"), xaipipeline.RunOptions{
		OutputDir:   outputDir,
		ForceReplan: true,
	})
	if err == nil {
		t.Fatal("Run() error = nil, want symlinked output dir error")
	}
	if !strings.Contains(err.Error(), "xai output dir") || !strings.Contains(err.Error(), "symlink") {
		t.Fatalf("Run() error = %v, want symlink output dir error", err)
	}
	if planner.calls != 0 {
		t.Fatalf("planner calls = %d, want 0", planner.calls)
	}
	if len(generator.calls) != 0 {
		t.Fatalf("generator calls = %d, want 0", len(generator.calls))
	}
	if renderer.calls != 0 {
		t.Fatalf("renderer calls = %d, want 0", renderer.calls)
	}
}

func TestOrchestrator_RunRejectsSymlinkedOutputDirParentBeforePlanning(t *testing.T) {
	t.Parallel()

	externalParent := t.TempDir()
	parent := t.TempDir()
	linkedParent := filepath.Join(parent, "linked-parent")
	if err := os.Symlink(externalParent, linkedParent); err != nil {
		t.Skipf("symlink not available: %v", err)
	}
	outputDir := filepath.Join(linkedParent, "xai-output")

	planner := &stubPlanner{out: xaipipeline.Manifest{
		ProjectID: "symlink-parent",
		Shots: []xaipipeline.Shot{
			{Index: 1, Prompt: "single shot"},
		},
	}}
	generator := &stubShotGenerator{data: []byte("mp4-bytes")}
	renderer := &stubRenderer{}
	orch := xaipipeline.NewOrchestrator(xaipipeline.Deps{
		Planner:       planner,
		ShotGenerator: generator,
		Renderer:      renderer,
		Validator:     stubValidator{},
	})

	_, err := orch.Run(context.Background(), []byte("story"), xaipipeline.RunOptions{OutputDir: outputDir})
	if err == nil {
		t.Fatal("Run() error = nil, want symlinked output dir parent error")
	}
	if !strings.Contains(err.Error(), "xai output dir") || !strings.Contains(err.Error(), "symlink") {
		t.Fatalf("Run() error = %v, want symlink parent output dir error", err)
	}
	if planner.calls != 0 {
		t.Fatalf("planner calls = %d, want 0", planner.calls)
	}
	if len(generator.calls) != 0 {
		t.Fatalf("generator calls = %d, want 0", len(generator.calls))
	}
	if renderer.calls != 0 {
		t.Fatalf("renderer calls = %d, want 0", renderer.calls)
	}
	if _, statErr := os.Stat(filepath.Join(externalParent, "xai-output")); !os.IsNotExist(statErr) {
		t.Fatalf("external output should not be created through symlink parent, stat err=%v", statErr)
	}
}

func TestOrchestrator_RunRejectsExistingOutputDirUnderSymlinkedParentBeforePlanning(t *testing.T) {
	t.Parallel()

	externalParent := t.TempDir()
	parent := t.TempDir()
	linkedParent := filepath.Join(parent, "linked-parent")
	if err := os.Symlink(externalParent, linkedParent); err != nil {
		t.Skipf("symlink not available: %v", err)
	}
	outputDir := filepath.Join(linkedParent, "xai-output")
	if err := os.MkdirAll(filepath.Join(externalParent, "xai-output"), 0755); err != nil {
		t.Fatalf("mkdir external output: %v", err)
	}

	planner := &stubPlanner{out: xaipipeline.Manifest{
		ProjectID: "existing-symlink-parent",
		Shots: []xaipipeline.Shot{
			{Index: 1, Prompt: "single shot"},
		},
	}}
	generator := &stubShotGenerator{data: []byte("mp4-bytes")}
	renderer := &stubRenderer{}
	orch := xaipipeline.NewOrchestrator(xaipipeline.Deps{
		Planner:       planner,
		ShotGenerator: generator,
		Renderer:      renderer,
		Validator:     stubValidator{},
	})

	_, err := orch.Run(context.Background(), []byte("story"), xaipipeline.RunOptions{OutputDir: outputDir})
	if err == nil {
		t.Fatal("Run() error = nil, want existing output dir under symlinked parent error")
	}
	if !strings.Contains(err.Error(), "xai output dir") || !strings.Contains(err.Error(), "symlink") {
		t.Fatalf("Run() error = %v, want symlink parent output dir error", err)
	}
	if planner.calls != 0 {
		t.Fatalf("planner calls = %d, want 0", planner.calls)
	}
	if len(generator.calls) != 0 {
		t.Fatalf("generator calls = %d, want 0", len(generator.calls))
	}
	if renderer.calls != 0 {
		t.Fatalf("renderer calls = %d, want 0", renderer.calls)
	}
	if _, statErr := os.Stat(filepath.Join(externalParent, "xai-output", "xai_manifest.json")); !os.IsNotExist(statErr) {
		t.Fatalf("external output should not receive artifacts, stat err=%v", statErr)
	}
}

func TestOrchestrator_RunRejectsSymlinkedOutputDirAncestorBeforePlanning(t *testing.T) {
	t.Parallel()

	externalAncestor := t.TempDir()
	parent := t.TempDir()
	linkedAncestor := filepath.Join(parent, "linked-ancestor")
	if err := os.Symlink(externalAncestor, linkedAncestor); err != nil {
		t.Skipf("symlink not available: %v", err)
	}
	outputDir := filepath.Join(linkedAncestor, "missing-parent", "xai-output")

	planner := &stubPlanner{out: xaipipeline.Manifest{
		ProjectID: "symlink-ancestor",
		Shots: []xaipipeline.Shot{
			{Index: 1, Prompt: "single shot"},
		},
	}}
	generator := &stubShotGenerator{data: []byte("mp4-bytes")}
	renderer := &stubRenderer{}
	orch := xaipipeline.NewOrchestrator(xaipipeline.Deps{
		Planner:       planner,
		ShotGenerator: generator,
		Renderer:      renderer,
		Validator:     stubValidator{},
	})

	_, err := orch.Run(context.Background(), []byte("story"), xaipipeline.RunOptions{OutputDir: outputDir})
	if err == nil {
		t.Fatal("Run() error = nil, want symlinked output dir ancestor error")
	}
	if !strings.Contains(err.Error(), "xai output dir") || !strings.Contains(err.Error(), "symlink") {
		t.Fatalf("Run() error = %v, want symlink ancestor output dir error", err)
	}
	if planner.calls != 0 {
		t.Fatalf("planner calls = %d, want 0", planner.calls)
	}
	if len(generator.calls) != 0 {
		t.Fatalf("generator calls = %d, want 0", len(generator.calls))
	}
	if renderer.calls != 0 {
		t.Fatalf("renderer calls = %d, want 0", renderer.calls)
	}
	if _, statErr := os.Stat(filepath.Join(externalAncestor, "missing-parent")); !os.IsNotExist(statErr) {
		t.Fatalf("external output should not be created through symlink ancestor, stat err=%v", statErr)
	}
}

func TestOrchestrator_RunRejectsExistingOutputDirUnderSymlinkedAncestorBeforePlanning(t *testing.T) {
	t.Parallel()

	externalAncestor := t.TempDir()
	parent := t.TempDir()
	linkedAncestor := filepath.Join(parent, "linked-ancestor")
	if err := os.Symlink(externalAncestor, linkedAncestor); err != nil {
		t.Skipf("symlink not available: %v", err)
	}
	outputDir := filepath.Join(linkedAncestor, "existing-parent", "xai-output")
	if err := os.MkdirAll(filepath.Join(externalAncestor, "existing-parent", "xai-output"), 0755); err != nil {
		t.Fatalf("mkdir external output: %v", err)
	}

	planner := &stubPlanner{out: xaipipeline.Manifest{
		ProjectID: "existing-symlink-ancestor",
		Shots: []xaipipeline.Shot{
			{Index: 1, Prompt: "single shot"},
		},
	}}
	generator := &stubShotGenerator{data: []byte("mp4-bytes")}
	renderer := &stubRenderer{}
	orch := xaipipeline.NewOrchestrator(xaipipeline.Deps{
		Planner:       planner,
		ShotGenerator: generator,
		Renderer:      renderer,
		Validator:     stubValidator{},
	})

	_, err := orch.Run(context.Background(), []byte("story"), xaipipeline.RunOptions{OutputDir: outputDir})
	if err == nil {
		t.Fatal("Run() error = nil, want existing output dir under symlinked ancestor error")
	}
	if !strings.Contains(err.Error(), "xai output dir") || !strings.Contains(err.Error(), "symlink") {
		t.Fatalf("Run() error = %v, want symlink ancestor output dir error", err)
	}
	if planner.calls != 0 {
		t.Fatalf("planner calls = %d, want 0", planner.calls)
	}
	if len(generator.calls) != 0 {
		t.Fatalf("generator calls = %d, want 0", len(generator.calls))
	}
	if renderer.calls != 0 {
		t.Fatalf("renderer calls = %d, want 0", renderer.calls)
	}
	if _, statErr := os.Stat(filepath.Join(externalAncestor, "existing-parent", "xai-output", "xai_manifest.json")); !os.IsNotExist(statErr) {
		t.Fatalf("external output should not receive artifacts, stat err=%v", statErr)
	}
}

func TestOrchestrator_RunSkipsValidCachedShots(t *testing.T) {
	t.Parallel()

	outputDir := t.TempDir()
	cachedShot := filepath.Join(outputDir, "shots", "shot_001.mp4")
	if err := os.MkdirAll(filepath.Dir(cachedShot), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(cachedShot, []byte("cached"), 0644); err != nil {
		t.Fatal(err)
	}
	writePreviousManifest(t, outputDir, xaipipeline.Manifest{
		ProjectID: "resume",
		Shots: []xaipipeline.Shot{
			{Index: 1, Prompt: "cached shot", DurationSec: 8, AspectRatio: "9:16", Resolution: "720p", VideoPath: "shots/shot_001.mp4", PromptHash: testShotPromptHash("cached shot"), XAIRequestID: "req_cached", XAIStatus: "done"},
			{Index: 2, Prompt: "new shot", VideoPath: "shots/shot_002.mp4", XAIRequestID: "req_old_new", XAIStatus: "done"},
		},
	})

	planner := &stubPlanner{out: xaipipeline.Manifest{
		ProjectID: "resume",
		Shots: []xaipipeline.Shot{
			{Index: 1, Prompt: "cached shot"},
			{Index: 2, Prompt: "new shot"},
		},
	}}
	generator := &stubShotGenerator{data: []byte("new")}
	renderer := &stubRenderer{}

	orch := xaipipeline.NewOrchestrator(xaipipeline.Deps{
		Planner:       planner,
		ShotGenerator: generator,
		Renderer:      renderer,
		Validator:     stubValidator{valid: map[string]bool{cachedShot: true}},
	})

	result, err := orch.Run(context.Background(), []byte("story"), xaipipeline.RunOptions{OutputDir: outputDir})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if len(generator.calls) != 1 {
		t.Fatalf("generator calls = %d, want 1", len(generator.calls))
	}
	if generator.calls[0].Index != 2 {
		t.Fatalf("generated shot index = %d, want 2", generator.calls[0].Index)
	}
	if data, err := os.ReadFile(cachedShot); err != nil || string(data) != "cached" {
		t.Fatalf("cached shot overwritten: err=%v data=%q", err, string(data))
	}
	if result.Manifest.Shots[0].VideoPath != "shots/shot_001.mp4" {
		t.Fatalf("cached shot path = %q", result.Manifest.Shots[0].VideoPath)
	}
	assertEqualInts(t, result.RunMetadata.GeneratedShots, []int{2})
	assertEqualInts(t, result.RunMetadata.ReusedShots, []int{1})
}

func TestOrchestrator_RunRegeneratesCachedShotWhenValidatorRejectsRequestedShotSpec(t *testing.T) {
	t.Parallel()

	outputDir := t.TempDir()
	cachedShot := filepath.Join(outputDir, "shots", "shot_001.mp4")
	if err := os.MkdirAll(filepath.Dir(cachedShot), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(cachedShot, []byte("cached-wrong-duration"), 0644); err != nil {
		t.Fatal(err)
	}
	writePreviousManifest(t, outputDir, xaipipeline.Manifest{
		ProjectID: "resume-spec",
		Shots: []xaipipeline.Shot{
			{
				Index:        1,
				Prompt:       "cached shot",
				DurationSec:  8,
				AspectRatio:  "9:16",
				Resolution:   "720p",
				VideoPath:    "shots/shot_001.mp4",
				PromptHash:   testShotPromptHash("cached shot"),
				XAIRequestID: "req_cached",
				XAIStatus:    "done",
			},
		},
	})

	planner := &stubPlanner{out: xaipipeline.Manifest{
		ProjectID: "resume-spec",
		Shots: []xaipipeline.Shot{
			{Index: 1, Prompt: "cached shot"},
		},
	}}
	generator := &stubShotGenerator{
		data:       []byte("fresh"),
		requestIDs: map[int]string{1: "req_fresh"},
		statuses:   map[int]string{1: "done"},
	}
	renderer := &stubRenderer{}
	checkedCachedSpec := false
	validator := stubValidator{specFn: func(path string, spec xaipipeline.RenderValidationSpec) bool {
		if path == cachedShot {
			checkedCachedSpec = true
			if spec.ExpectedDurationSec != 8 {
				t.Fatalf("cached shot validation duration = %.3f, want 8", spec.ExpectedDurationSec)
			}
			return false
		}
		return spec.ExpectedDurationSec == 8
	}}

	orch := xaipipeline.NewOrchestrator(xaipipeline.Deps{
		Planner:       planner,
		ShotGenerator: generator,
		Renderer:      renderer,
		Validator:     validator,
	})

	result, err := orch.Run(context.Background(), []byte("story"), xaipipeline.RunOptions{OutputDir: outputDir})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if !checkedCachedSpec {
		t.Fatal("cached shot was not validated against the requested shot spec")
	}
	if len(generator.calls) != 1 {
		t.Fatalf("generator calls = %d, want 1", len(generator.calls))
	}
	if data, err := os.ReadFile(cachedShot); err != nil || string(data) != "fresh" {
		t.Fatalf("cached shot should be regenerated after spec validation fails: err=%v data=%q", err, string(data))
	}
	assertEqualInts(t, result.RunMetadata.GeneratedShots, []int{1})
	assertEqualInts(t, result.RunMetadata.ReusedShots, nil)
}

func TestOrchestrator_RunRegeneratesCachedShotWhenVideoModelChanges(t *testing.T) {
	t.Parallel()

	outputDir := t.TempDir()
	cachedShot := filepath.Join(outputDir, "shots", "shot_001.mp4")
	if err := os.MkdirAll(filepath.Dir(cachedShot), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(cachedShot, []byte("cached-from-old-model"), 0644); err != nil {
		t.Fatal(err)
	}
	writePreviousManifest(t, outputDir, xaipipeline.Manifest{
		ProjectID:  "model-cache",
		VideoModel: "grok-imagine-video-old",
		StoryHash:  testStoryHash("story"),
		Format:     "portrait",
		FPS:        24,
		Width:      720,
		Height:     1280,
		Shots: []xaipipeline.Shot{
			{
				Index:        1,
				Prompt:       "cached shot",
				DurationSec:  8,
				AspectRatio:  "9:16",
				Resolution:   "720p",
				VideoPath:    "shots/shot_001.mp4",
				PromptHash:   testShotPromptHashForModel("cached shot", "grok-imagine-video-old"),
				XAIRequestID: "req_old_model",
				XAIStatus:    "done",
			},
		},
	})

	planner := &stubPlanner{out: xaipipeline.Manifest{
		ProjectID: "model-cache",
		Shots: []xaipipeline.Shot{
			{Index: 1, Prompt: "cached shot"},
		},
	}}
	generator := &stubShotGenerator{
		data:       []byte("fresh-from-new-model"),
		requestIDs: map[int]string{1: "req_new_model"},
		statuses:   map[int]string{1: "done"},
	}
	renderer := &stubRenderer{}
	orch := xaipipeline.NewOrchestrator(xaipipeline.Deps{
		Planner:       planner,
		ShotGenerator: generator,
		Renderer:      renderer,
		Validator:     stubValidator{valid: map[string]bool{cachedShot: true}},
	})

	result, err := orch.Run(context.Background(), []byte("story"), xaipipeline.RunOptions{
		OutputDir:  outputDir,
		VideoModel: "grok-imagine-video-new",
	})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if len(generator.calls) != 1 {
		t.Fatalf("generator calls = %d, want 1", len(generator.calls))
	}
	if data, err := os.ReadFile(cachedShot); err != nil || string(data) != "fresh-from-new-model" {
		t.Fatalf("cached shot should be regenerated for model change: err=%v data=%q", err, string(data))
	}
	if result.Manifest.VideoModel != "grok-imagine-video-new" {
		t.Fatalf("manifest video_model = %q, want grok-imagine-video-new", result.Manifest.VideoModel)
	}
	if result.RunMetadata.VideoModel != "grok-imagine-video-new" {
		t.Fatalf("run metadata video_model = %q, want grok-imagine-video-new", result.RunMetadata.VideoModel)
	}
	if result.Manifest.Shots[0].PromptHash == testShotPromptHashForModel("cached shot", "grok-imagine-video-old") {
		t.Fatal("prompt_hash still matches old video model")
	}
	assertEqualInts(t, result.RunMetadata.GeneratedShots, []int{1})
	assertEqualInts(t, result.RunMetadata.ReusedShots, nil)
}

func TestOrchestrator_RunTrimsReusedCachedShotProviderMetadata(t *testing.T) {
	t.Parallel()

	outputDir := t.TempDir()
	cachedShot := filepath.Join(outputDir, "shots", "shot_001.mp4")
	if err := os.MkdirAll(filepath.Dir(cachedShot), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(cachedShot, []byte("cached"), 0644); err != nil {
		t.Fatal(err)
	}
	writePreviousManifest(t, outputDir, xaipipeline.Manifest{
		ProjectID: "trim-provider-metadata",
		Shots: []xaipipeline.Shot{
			{Index: 1, Prompt: "cached shot", DurationSec: 8, AspectRatio: "9:16", Resolution: "720p", VideoPath: "shots/shot_001.mp4", PromptHash: testShotPromptHash("cached shot"), XAIRequestID: " req_cached ", XAIStatus: " done "},
		},
	})

	planner := &stubPlanner{out: xaipipeline.Manifest{
		ProjectID: "trim-provider-metadata",
		Shots: []xaipipeline.Shot{
			{Index: 1, Prompt: "cached shot"},
		},
	}}
	generator := &stubShotGenerator{data: []byte("fresh")}
	renderer := &stubRenderer{}
	orch := xaipipeline.NewOrchestrator(xaipipeline.Deps{
		Planner:       planner,
		ShotGenerator: generator,
		Renderer:      renderer,
		Validator:     stubValidator{valid: map[string]bool{cachedShot: true}},
	})

	result, err := orch.Run(context.Background(), []byte("story"), xaipipeline.RunOptions{OutputDir: outputDir})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if len(generator.calls) != 0 {
		t.Fatalf("generator calls = %d, want 0", len(generator.calls))
	}
	if result.Manifest.Shots[0].XAIRequestID != "req_cached" {
		t.Fatalf("xai_request_id = %q, want req_cached", result.Manifest.Shots[0].XAIRequestID)
	}
	if result.Manifest.Shots[0].XAIStatus != "done" {
		t.Fatalf("xai_status = %q, want done", result.Manifest.Shots[0].XAIStatus)
	}
	assertShotDecision(t, result.RunMetadata.ShotDecisions[0], xaipipeline.ShotDecision{
		Index:        1,
		Decision:     "reused",
		VideoPath:    "shots/shot_001.mp4",
		PromptHash:   result.Manifest.Shots[0].PromptHash,
		XAIRequestID: "req_cached",
		XAIStatus:    "done",
	})
}

func TestOrchestrator_RunCanonicalizesReusedCachedShotProviderStatus(t *testing.T) {
	t.Parallel()

	outputDir := t.TempDir()
	cachedShot := filepath.Join(outputDir, "shots", "shot_001.mp4")
	if err := os.MkdirAll(filepath.Dir(cachedShot), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(cachedShot, []byte("cached"), 0644); err != nil {
		t.Fatal(err)
	}
	writePreviousManifest(t, outputDir, xaipipeline.Manifest{
		ProjectID: "canonical-provider-status",
		Shots: []xaipipeline.Shot{
			{Index: 1, Prompt: "cached shot", DurationSec: 8, AspectRatio: "9:16", Resolution: "720p", VideoPath: "shots/shot_001.mp4", PromptHash: testShotPromptHash("cached shot"), XAIRequestID: "req_cached", XAIStatus: "DONE"},
		},
	})

	planner := &stubPlanner{out: xaipipeline.Manifest{
		ProjectID: "canonical-provider-status",
		Shots: []xaipipeline.Shot{
			{Index: 1, Prompt: "cached shot"},
		},
	}}
	generator := &stubShotGenerator{data: []byte("fresh")}
	orch := xaipipeline.NewOrchestrator(xaipipeline.Deps{
		Planner:       planner,
		ShotGenerator: generator,
		Renderer:      &stubRenderer{},
		Validator:     stubValidator{valid: map[string]bool{cachedShot: true}},
	})

	result, err := orch.Run(context.Background(), []byte("story"), xaipipeline.RunOptions{OutputDir: outputDir})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if len(generator.calls) != 0 {
		t.Fatalf("generator calls = %d, want 0", len(generator.calls))
	}
	if result.Manifest.Shots[0].XAIStatus != "done" {
		t.Fatalf("xai_status = %q, want done", result.Manifest.Shots[0].XAIStatus)
	}
	if result.RunMetadata.ShotDecisions[0].XAIStatus != "done" {
		t.Fatalf("run metadata xai_status = %q, want done", result.RunMetadata.ShotDecisions[0].XAIStatus)
	}
}

func TestOrchestrator_RunRegeneratesCachedShotWhenProviderMetadataMissing(t *testing.T) {
	t.Parallel()

	outputDir := t.TempDir()
	cachedShot := filepath.Join(outputDir, "shots", "shot_001.mp4")
	if err := os.MkdirAll(filepath.Dir(cachedShot), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(cachedShot, []byte("cached-without-metadata"), 0644); err != nil {
		t.Fatal(err)
	}
	writePreviousManifest(t, outputDir, xaipipeline.Manifest{
		ProjectID: "missing-provider-metadata",
		Shots: []xaipipeline.Shot{
			{Index: 1, Prompt: "cached shot", VideoPath: "shots/shot_001.mp4"},
		},
	})

	planner := &stubPlanner{out: xaipipeline.Manifest{
		ProjectID: "missing-provider-metadata",
		Shots: []xaipipeline.Shot{
			{Index: 1, Prompt: "cached shot"},
		},
	}}
	generator := &stubShotGenerator{
		data:       []byte("fresh"),
		requestIDs: map[int]string{1: "req_fresh"},
		statuses:   map[int]string{1: "done"},
	}
	renderer := &stubRenderer{}
	orch := xaipipeline.NewOrchestrator(xaipipeline.Deps{
		Planner:       planner,
		ShotGenerator: generator,
		Renderer:      renderer,
		Validator:     stubValidator{valid: map[string]bool{cachedShot: true}},
	})

	result, err := orch.Run(context.Background(), []byte("story"), xaipipeline.RunOptions{OutputDir: outputDir})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if len(generator.calls) != 1 {
		t.Fatalf("generator calls = %d, want 1", len(generator.calls))
	}
	if data, err := os.ReadFile(cachedShot); err != nil || string(data) != "fresh" {
		t.Fatalf("cached shot should be regenerated: err=%v data=%q", err, string(data))
	}
	if result.Manifest.Shots[0].XAIRequestID != "req_fresh" {
		t.Fatalf("xai_request_id = %q, want req_fresh", result.Manifest.Shots[0].XAIRequestID)
	}
	assertEqualInts(t, result.RunMetadata.GeneratedShots, []int{1})
	assertEqualInts(t, result.RunMetadata.ReusedShots, nil)
}

func TestOrchestrator_RunRegeneratesCachedShotWhenPreviousPromptHashMissing(t *testing.T) {
	t.Parallel()

	outputDir := t.TempDir()
	cachedShot := filepath.Join(outputDir, "shots", "shot_001.mp4")
	if err := os.MkdirAll(filepath.Dir(cachedShot), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(cachedShot, []byte("cached-without-prompt-hash"), 0644); err != nil {
		t.Fatal(err)
	}
	writePreviousManifest(t, outputDir, xaipipeline.Manifest{
		ProjectID: "missing-prompt-hash",
		Shots: []xaipipeline.Shot{
			{Index: 1, Prompt: "cached shot", DurationSec: 8, AspectRatio: "9:16", Resolution: "720p", VideoPath: "shots/shot_001.mp4", XAIRequestID: "req_cached", XAIStatus: "done"},
		},
	})

	planner := &stubPlanner{out: xaipipeline.Manifest{
		ProjectID: "missing-prompt-hash",
		Shots: []xaipipeline.Shot{
			{Index: 1, Prompt: "cached shot"},
		},
	}}
	generator := &stubShotGenerator{
		data:       []byte("fresh"),
		requestIDs: map[int]string{1: "req_fresh"},
		statuses:   map[int]string{1: "done"},
	}
	renderer := &stubRenderer{}
	orch := xaipipeline.NewOrchestrator(xaipipeline.Deps{
		Planner:       planner,
		ShotGenerator: generator,
		Renderer:      renderer,
		Validator:     stubValidator{valid: map[string]bool{cachedShot: true}},
	})

	result, err := orch.Run(context.Background(), []byte("story"), xaipipeline.RunOptions{OutputDir: outputDir})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if len(generator.calls) != 1 {
		t.Fatalf("generator calls = %d, want 1", len(generator.calls))
	}
	if data, err := os.ReadFile(cachedShot); err != nil || string(data) != "fresh" {
		t.Fatalf("cached shot should be regenerated: err=%v data=%q", err, string(data))
	}
	if result.Manifest.Shots[0].PromptHash == "" {
		t.Fatal("prompt_hash is empty after regeneration")
	}
	if result.Manifest.Shots[0].XAIRequestID != "req_fresh" {
		t.Fatalf("xai_request_id = %q, want req_fresh", result.Manifest.Shots[0].XAIRequestID)
	}
	assertEqualInts(t, result.RunMetadata.GeneratedShots, []int{1})
	assertEqualInts(t, result.RunMetadata.ReusedShots, nil)
}

func TestOrchestrator_RunRegeneratesCachedShotWhenPreviousStatusIncomplete(t *testing.T) {
	t.Parallel()

	outputDir := t.TempDir()
	cachedShot := filepath.Join(outputDir, "shots", "shot_001.mp4")
	if err := os.MkdirAll(filepath.Dir(cachedShot), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(cachedShot, []byte("cached-pending"), 0644); err != nil {
		t.Fatal(err)
	}
	writePreviousManifest(t, outputDir, xaipipeline.Manifest{
		ProjectID: "incomplete-status",
		Shots: []xaipipeline.Shot{
			{Index: 1, Prompt: "cached shot", DurationSec: 8, AspectRatio: "9:16", Resolution: "720p", VideoPath: "shots/shot_001.mp4", PromptHash: testShotPromptHash("cached shot"), XAIRequestID: "req_cached", XAIStatus: "pending"},
		},
	})

	planner := &stubPlanner{out: xaipipeline.Manifest{
		ProjectID: "incomplete-status",
		Shots: []xaipipeline.Shot{
			{Index: 1, Prompt: "cached shot"},
		},
	}}
	generator := &stubShotGenerator{
		data:       []byte("fresh"),
		requestIDs: map[int]string{1: "req_fresh"},
		statuses:   map[int]string{1: "done"},
	}
	renderer := &stubRenderer{}
	orch := xaipipeline.NewOrchestrator(xaipipeline.Deps{
		Planner:       planner,
		ShotGenerator: generator,
		Renderer:      renderer,
		Validator:     stubValidator{valid: map[string]bool{cachedShot: true}},
	})

	result, err := orch.Run(context.Background(), []byte("story"), xaipipeline.RunOptions{OutputDir: outputDir})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if len(generator.calls) != 1 {
		t.Fatalf("generator calls = %d, want 1", len(generator.calls))
	}
	if data, err := os.ReadFile(cachedShot); err != nil || string(data) != "fresh" {
		t.Fatalf("cached shot should be regenerated: err=%v data=%q", err, string(data))
	}
	if result.Manifest.Shots[0].XAIStatus != "done" {
		t.Fatalf("xai_status = %q, want done", result.Manifest.Shots[0].XAIStatus)
	}
	assertEqualInts(t, result.RunMetadata.GeneratedShots, []int{1})
	assertEqualInts(t, result.RunMetadata.ReusedShots, nil)
}

func TestOrchestrator_RunRegeneratesCachedShotWhenPromptChanges(t *testing.T) {
	t.Parallel()

	outputDir := t.TempDir()
	cachedShot := filepath.Join(outputDir, "shots", "shot_001.mp4")
	if err := os.MkdirAll(filepath.Dir(cachedShot), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(cachedShot, []byte("stale"), 0644); err != nil {
		t.Fatal(err)
	}
	writePreviousManifest(t, outputDir, xaipipeline.Manifest{
		ProjectID: "resume",
		Shots: []xaipipeline.Shot{
			{Index: 1, Prompt: "old prompt", VideoPath: "shots/shot_001.mp4"},
		},
	})

	planner := &stubPlanner{out: xaipipeline.Manifest{
		ProjectID: "resume",
		Shots: []xaipipeline.Shot{
			{Index: 1, Prompt: "new prompt"},
		},
	}}
	generator := &stubShotGenerator{data: []byte("fresh")}
	renderer := &stubRenderer{}

	orch := xaipipeline.NewOrchestrator(xaipipeline.Deps{
		Planner:       planner,
		ShotGenerator: generator,
		Renderer:      renderer,
		Validator:     stubValidator{valid: map[string]bool{cachedShot: true}},
	})

	_, err := orch.Run(context.Background(), []byte("story"), xaipipeline.RunOptions{OutputDir: outputDir})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if len(generator.calls) != 1 {
		t.Fatalf("generator calls = %d, want 1", len(generator.calls))
	}
	if data, err := os.ReadFile(cachedShot); err != nil || string(data) != "fresh" {
		t.Fatalf("stale shot not regenerated: err=%v data=%q", err, string(data))
	}
}

func TestOrchestrator_RunReusesSameOutputDirWithoutRegeneratingUnchangedShots(t *testing.T) {
	t.Parallel()

	outputDir := t.TempDir()
	planner := &stubPlanner{out: xaipipeline.Manifest{
		ProjectID: "resume",
		Shots: []xaipipeline.Shot{
			{Index: 1, Prompt: "cached first"},
			{Index: 2, Prompt: "cached second"},
		},
	}}
	generator := &stubShotGenerator{data: []byte("generated")}
	renderer := &stubRenderer{}

	orch := xaipipeline.NewOrchestrator(xaipipeline.Deps{
		Planner:       planner,
		ShotGenerator: generator,
		Renderer:      renderer,
		Validator: stubValidator{fn: func(path string) bool {
			info, err := os.Stat(path)
			return err == nil && info.Size() > 0
		}},
	})

	if _, err := orch.Run(context.Background(), []byte("story"), xaipipeline.RunOptions{OutputDir: outputDir}); err != nil {
		t.Fatalf("first Run() error = %v", err)
	}
	if len(generator.calls) != 2 {
		t.Fatalf("first run generator calls = %d, want 2", len(generator.calls))
	}
	if renderer.calls != 1 {
		t.Fatalf("first run renderer calls = %d, want 1", renderer.calls)
	}

	generator.calls = nil
	result, err := orch.Run(context.Background(), []byte("story"), xaipipeline.RunOptions{OutputDir: outputDir})
	if err != nil {
		t.Fatalf("second Run() error = %v", err)
	}
	if len(generator.calls) != 0 {
		t.Fatalf("second run generator calls = %d, want 0", len(generator.calls))
	}
	if renderer.calls != 2 {
		t.Fatalf("renderer calls after second run = %d, want 2", renderer.calls)
	}
	for _, shot := range result.Manifest.Shots {
		if shot.PromptHash == "" {
			t.Fatalf("shot %d prompt hash is empty", shot.Index)
		}
		path := filepath.Join(outputDir, shot.VideoPath)
		if data, err := os.ReadFile(path); err != nil || string(data) != "generated" {
			t.Fatalf("cached shot %d changed: err=%v data=%q", shot.Index, err, string(data))
		}
	}
}

func TestOrchestrator_RunReusesCompleteExistingManifestBeforePlanning(t *testing.T) {
	t.Parallel()

	outputDir := t.TempDir()
	story := []byte("same story")
	validator := stubValidator{fn: func(path string) bool {
		info, err := os.Stat(path)
		return err == nil && info.Size() > 0
	}}

	firstPlanner := &stubPlanner{out: xaipipeline.Manifest{
		ProjectID: "resume-complete",
		Shots: []xaipipeline.Shot{
			{Index: 1, Prompt: "cached first"},
			{Index: 2, Prompt: "cached second"},
		},
	}}
	firstGenerator := &stubShotGenerator{data: []byte("generated")}
	firstRenderer := &stubRenderer{}
	firstRun := xaipipeline.NewOrchestrator(xaipipeline.Deps{
		Planner:       firstPlanner,
		ShotGenerator: firstGenerator,
		Renderer:      firstRenderer,
		Validator:     validator,
	})

	if _, err := firstRun.Run(context.Background(), story, xaipipeline.RunOptions{
		OutputDir:   outputDir,
		TargetShots: 2,
		Format:      "portrait",
	}); err != nil {
		t.Fatalf("first Run() error = %v", err)
	}
	if firstPlanner.calls != 1 {
		t.Fatalf("first planner calls = %d, want 1", firstPlanner.calls)
	}
	if len(firstGenerator.calls) != 2 {
		t.Fatalf("first generator calls = %d, want 2", len(firstGenerator.calls))
	}

	secondRenderer := &stubRenderer{}
	secondRun := xaipipeline.NewOrchestrator(xaipipeline.Deps{
		Renderer:  secondRenderer,
		Validator: validator,
	})

	result, err := secondRun.Run(context.Background(), story, xaipipeline.RunOptions{
		OutputDir:   outputDir,
		TargetShots: 2,
		Format:      "portrait",
	})
	if err != nil {
		t.Fatalf("second Run() error = %v", err)
	}
	if secondRenderer.calls != 1 {
		t.Fatalf("second renderer calls = %d, want 1", secondRenderer.calls)
	}
	if result.Manifest.ProjectID != "resume-complete" {
		t.Fatalf("project_id = %q", result.Manifest.ProjectID)
	}
	if result.RunMetadata.Planned {
		t.Fatal("run metadata planned = true, want false")
	}
	if !result.RunMetadata.ManifestReused {
		t.Fatal("run metadata manifest_reused = false, want true")
	}
	assertEqualInts(t, result.RunMetadata.GeneratedShots, nil)
	assertEqualInts(t, result.RunMetadata.ReusedShots, []int{1, 2})
	assertShotDecision(t, result.RunMetadata.ShotDecisions[0], xaipipeline.ShotDecision{
		Index:        1,
		Decision:     "reused",
		VideoPath:    "shots/shot_001.mp4",
		PromptHash:   result.Manifest.Shots[0].PromptHash,
		XAIRequestID: result.Manifest.Shots[0].XAIRequestID,
		XAIStatus:    result.Manifest.Shots[0].XAIStatus,
	})
	for _, shot := range result.Manifest.Shots {
		if data, err := os.ReadFile(filepath.Join(outputDir, shot.VideoPath)); err != nil || string(data) != "generated" {
			t.Fatalf("cached shot %d changed: err=%v data=%q", shot.Index, err, string(data))
		}
	}
}

func TestOrchestrator_RunRejectsCanceledContextAfterResumeValidationBeforeArtifacts(t *testing.T) {
	t.Parallel()

	outputDir := t.TempDir()
	story := []byte("same story")
	firstPlanner := &stubPlanner{out: xaipipeline.Manifest{
		ProjectID: "resume-cancel",
		Shots: []xaipipeline.Shot{
			{Index: 1, Prompt: "cached first"},
		},
	}}
	firstGenerator := &stubShotGenerator{data: []byte("generated")}
	firstRun := xaipipeline.NewOrchestrator(xaipipeline.Deps{
		Planner:       firstPlanner,
		ShotGenerator: firstGenerator,
		Renderer:      &stubRenderer{},
		Validator: stubValidator{fn: func(path string) bool {
			info, err := os.Stat(path)
			return err == nil && info.Size() > 0
		}},
	})

	if _, err := firstRun.Run(context.Background(), story, xaipipeline.RunOptions{
		OutputDir:   outputDir,
		TargetShots: 1,
		Format:      "portrait",
	}); err != nil {
		t.Fatalf("first Run() error = %v", err)
	}
	runMetadataPath := filepath.Join(outputDir, "xai_run_metadata.json")
	beforeMetadata, err := os.ReadFile(runMetadataPath)
	if err != nil {
		t.Fatalf("read initial run metadata: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	secondPlanner := &stubPlanner{}
	secondRenderer := &stubRenderer{}
	secondRun := xaipipeline.NewOrchestrator(xaipipeline.Deps{
		Planner:  secondPlanner,
		Renderer: secondRenderer,
		Validator: stubValidator{fn: func(path string) bool {
			info, err := os.Stat(path)
			if err == nil && info.Size() > 0 {
				cancel()
				return true
			}
			return false
		}},
	})

	got, err := secondRun.Run(ctx, story, xaipipeline.RunOptions{
		OutputDir:   outputDir,
		TargetShots: 1,
		Format:      "portrait",
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("second Run() error = %v, want context.Canceled", err)
	}
	if got != nil {
		t.Fatalf("second Run() result = %+v, want nil on canceled context", got)
	}
	if secondPlanner.calls != 0 {
		t.Fatalf("second planner calls = %d, want 0", secondPlanner.calls)
	}
	if secondRenderer.calls != 0 {
		t.Fatalf("second renderer calls = %d, want 0", secondRenderer.calls)
	}
	afterMetadata, err := os.ReadFile(runMetadataPath)
	if err != nil {
		t.Fatalf("read run metadata after cancellation: %v", err)
	}
	if string(afterMetadata) != string(beforeMetadata) {
		t.Fatalf("run metadata changed after cancellation\nbefore=%s\nafter=%s", beforeMetadata, afterMetadata)
	}
}

func TestOrchestrator_RunRejectsCanceledContextAfterPerShotCacheValidationBeforeArtifacts(t *testing.T) {
	t.Parallel()

	outputDir := t.TempDir()
	cachedShot := filepath.Join(outputDir, "shots", "shot_001.mp4")
	if err := os.MkdirAll(filepath.Dir(cachedShot), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(cachedShot, []byte("cached"), 0644); err != nil {
		t.Fatal(err)
	}
	writePreviousManifest(t, outputDir, xaipipeline.Manifest{
		ProjectID: "per-shot-cancel",
		Format:    "portrait",
		FPS:       24,
		Width:     720,
		Height:    1280,
		Shots: []xaipipeline.Shot{
			{
				Index:        1,
				Prompt:       "cached first",
				PromptHash:   testShotPromptHash("cached first"),
				XAIRequestID: " req_cached ",
				XAIStatus:    " Done ",
				DurationSec:  8,
				AspectRatio:  "9:16",
				Resolution:   "720p",
				VideoPath:    "shots/shot_001.mp4",
			},
		},
	})
	manifestPath := filepath.Join(outputDir, "xai_manifest.json")
	beforeManifest, err := os.ReadFile(manifestPath)
	if err != nil {
		t.Fatalf("read previous manifest: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	planner := &stubPlanner{out: xaipipeline.Manifest{
		ProjectID: "per-shot-cancel",
		Shots: []xaipipeline.Shot{
			{Index: 1, Prompt: "cached first"},
		},
	}}
	renderer := &stubRenderer{}
	orch := xaipipeline.NewOrchestrator(xaipipeline.Deps{
		Planner:  planner,
		Renderer: renderer,
		Validator: stubValidator{fn: func(path string) bool {
			info, err := os.Stat(path)
			if err == nil && info.Size() > 0 {
				cancel()
				return true
			}
			return false
		}},
	})

	got, err := orch.Run(ctx, []byte("same story"), xaipipeline.RunOptions{
		OutputDir:   outputDir,
		TargetShots: 1,
		Format:      "portrait",
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Run() error = %v, want context.Canceled", err)
	}
	if got != nil {
		t.Fatalf("Run() result = %+v, want nil on canceled context", got)
	}
	if planner.calls != 1 {
		t.Fatalf("planner calls = %d, want 1", planner.calls)
	}
	if renderer.calls != 0 {
		t.Fatalf("renderer calls = %d, want 0", renderer.calls)
	}
	afterManifest, err := os.ReadFile(manifestPath)
	if err != nil {
		t.Fatalf("read manifest after cancellation: %v", err)
	}
	if string(afterManifest) != string(beforeManifest) {
		t.Fatalf("manifest changed after cancellation\nbefore=%s\nafter=%s", beforeManifest, afterManifest)
	}
	if _, err := os.Stat(filepath.Join(outputDir, "xai_run_metadata.json")); !os.IsNotExist(err) {
		t.Fatalf("run metadata should not be written after cancellation, stat err=%v", err)
	}
	if data, err := os.ReadFile(cachedShot); err != nil || string(data) != "cached" {
		t.Fatalf("cached shot changed after cancellation: err=%v data=%q", err, string(data))
	}
}

func TestOrchestrator_RunTrimsReusedCompleteManifestProviderMetadata(t *testing.T) {
	t.Parallel()

	outputDir := t.TempDir()
	story := []byte("same story")
	validator := stubValidator{fn: func(path string) bool {
		info, err := os.Stat(path)
		return err == nil && info.Size() > 0
	}}

	firstRun := xaipipeline.NewOrchestrator(xaipipeline.Deps{
		Planner: &stubPlanner{out: xaipipeline.Manifest{
			ProjectID: "trim-complete",
			Shots: []xaipipeline.Shot{
				{Index: 1, Prompt: "cached shot"},
			},
		}},
		ShotGenerator: &stubShotGenerator{data: []byte("generated")},
		Renderer:      &stubRenderer{},
		Validator:     validator,
	})
	if _, err := firstRun.Run(context.Background(), story, xaipipeline.RunOptions{OutputDir: outputDir}); err != nil {
		t.Fatalf("first Run() error = %v", err)
	}

	manifest, ok := readManifestFile(t, filepath.Join(outputDir, "xai_manifest.json"))
	if !ok {
		t.Fatal("xai_manifest.json missing after first run")
	}
	manifest.Shots[0].XAIRequestID = " req_cached "
	manifest.Shots[0].XAIStatus = " done "
	writePreviousManifest(t, outputDir, manifest)

	secondRenderer := &stubRenderer{}
	secondRun := xaipipeline.NewOrchestrator(xaipipeline.Deps{
		Renderer:  secondRenderer,
		Validator: validator,
	})
	result, err := secondRun.Run(context.Background(), story, xaipipeline.RunOptions{OutputDir: outputDir})
	if err != nil {
		t.Fatalf("second Run() error = %v", err)
	}
	if !result.RunMetadata.ManifestReused {
		t.Fatal("manifest_reused = false, want true")
	}
	if result.Manifest.Shots[0].XAIRequestID != "req_cached" {
		t.Fatalf("xai_request_id = %q, want req_cached", result.Manifest.Shots[0].XAIRequestID)
	}
	if result.Manifest.Shots[0].XAIStatus != "done" {
		t.Fatalf("xai_status = %q, want done", result.Manifest.Shots[0].XAIStatus)
	}
	assertShotDecision(t, result.RunMetadata.ShotDecisions[0], xaipipeline.ShotDecision{
		Index:        1,
		Decision:     "reused",
		VideoPath:    "shots/shot_001.mp4",
		PromptHash:   result.Manifest.Shots[0].PromptHash,
		XAIRequestID: "req_cached",
		XAIStatus:    "done",
	})
}

func TestOrchestrator_RunDoesNotReuseCompleteManifestBeforePlanningWhenPromptHashMissing(t *testing.T) {
	t.Parallel()

	outputDir := t.TempDir()
	story := []byte("same story")
	validator := stubValidator{fn: func(path string) bool {
		info, err := os.Stat(path)
		return err == nil && info.Size() > 0
	}}

	firstRun := xaipipeline.NewOrchestrator(xaipipeline.Deps{
		Planner: &stubPlanner{out: xaipipeline.Manifest{
			ProjectID: "legacy-complete",
			Shots: []xaipipeline.Shot{
				{Index: 1, Prompt: "legacy shot"},
			},
		}},
		ShotGenerator: &stubShotGenerator{data: []byte("old")},
		Renderer:      &stubRenderer{},
		Validator:     validator,
	})
	if _, err := firstRun.Run(context.Background(), story, xaipipeline.RunOptions{OutputDir: outputDir}); err != nil {
		t.Fatalf("first Run() error = %v", err)
	}

	manifest, ok := readManifestFile(t, filepath.Join(outputDir, "xai_manifest.json"))
	if !ok {
		t.Fatal("xai_manifest.json missing after first run")
	}
	manifest.Shots[0].PromptHash = ""
	writePreviousManifest(t, outputDir, manifest)

	secondPlanner := &stubPlanner{out: xaipipeline.Manifest{
		ProjectID: "fresh-plan",
		Shots: []xaipipeline.Shot{
			{Index: 1, Prompt: "fresh planned shot"},
		},
	}}
	secondGenerator := &stubShotGenerator{data: []byte("fresh")}
	secondRun := xaipipeline.NewOrchestrator(xaipipeline.Deps{
		Planner:       secondPlanner,
		ShotGenerator: secondGenerator,
		Renderer:      &stubRenderer{},
		Validator:     validator,
	})

	result, err := secondRun.Run(context.Background(), story, xaipipeline.RunOptions{OutputDir: outputDir})
	if err != nil {
		t.Fatalf("second Run() error = %v", err)
	}
	if secondPlanner.calls != 1 {
		t.Fatalf("second planner calls = %d, want 1", secondPlanner.calls)
	}
	if len(secondGenerator.calls) != 1 {
		t.Fatalf("second generator calls = %d, want 1", len(secondGenerator.calls))
	}
	if !result.RunMetadata.Planned || result.RunMetadata.ManifestReused {
		t.Fatalf("run metadata = %+v, want planned non-reused run", result.RunMetadata)
	}
	if result.Manifest.Shots[0].Prompt != "fresh planned shot" {
		t.Fatalf("prompt = %q, want fresh planned shot", result.Manifest.Shots[0].Prompt)
	}
}

func TestOrchestrator_RunDoesNotReuseCompleteManifestBeforePlanningWhenProviderMetadataMissing(t *testing.T) {
	t.Parallel()

	outputDir := t.TempDir()
	story := []byte("same story")
	validator := stubValidator{fn: func(path string) bool {
		info, err := os.Stat(path)
		return err == nil && info.Size() > 0
	}}

	firstRun := xaipipeline.NewOrchestrator(xaipipeline.Deps{
		Planner: &stubPlanner{out: xaipipeline.Manifest{
			ProjectID: "missing-provider-complete",
			Shots: []xaipipeline.Shot{
				{Index: 1, Prompt: "legacy shot"},
			},
		}},
		ShotGenerator: &stubShotGenerator{data: []byte("old")},
		Renderer:      &stubRenderer{},
		Validator:     validator,
	})
	if _, err := firstRun.Run(context.Background(), story, xaipipeline.RunOptions{OutputDir: outputDir}); err != nil {
		t.Fatalf("first Run() error = %v", err)
	}

	manifest, ok := readManifestFile(t, filepath.Join(outputDir, "xai_manifest.json"))
	if !ok {
		t.Fatal("xai_manifest.json missing after first run")
	}
	manifest.Shots[0].XAIRequestID = ""
	manifest.Shots[0].XAIStatus = ""
	writePreviousManifest(t, outputDir, manifest)

	secondPlanner := &stubPlanner{out: xaipipeline.Manifest{
		ProjectID: "fresh-plan",
		Shots: []xaipipeline.Shot{
			{Index: 1, Prompt: "fresh planned shot"},
		},
	}}
	secondGenerator := &stubShotGenerator{data: []byte("fresh")}
	secondRun := xaipipeline.NewOrchestrator(xaipipeline.Deps{
		Planner:       secondPlanner,
		ShotGenerator: secondGenerator,
		Renderer:      &stubRenderer{},
		Validator:     validator,
	})

	result, err := secondRun.Run(context.Background(), story, xaipipeline.RunOptions{OutputDir: outputDir})
	if err != nil {
		t.Fatalf("second Run() error = %v", err)
	}
	if secondPlanner.calls != 1 {
		t.Fatalf("second planner calls = %d, want 1", secondPlanner.calls)
	}
	if len(secondGenerator.calls) != 1 {
		t.Fatalf("second generator calls = %d, want 1", len(secondGenerator.calls))
	}
	if !result.RunMetadata.Planned || result.RunMetadata.ManifestReused {
		t.Fatalf("run metadata = %+v, want planned non-reused run", result.RunMetadata)
	}
	if result.Manifest.Shots[0].Prompt != "fresh planned shot" {
		t.Fatalf("prompt = %q, want fresh planned shot", result.Manifest.Shots[0].Prompt)
	}
}

func TestOrchestrator_RunDoesNotReuseCompleteManifestBeforePlanningWhenStatusIncomplete(t *testing.T) {
	t.Parallel()

	outputDir := t.TempDir()
	story := []byte("same story")
	validator := stubValidator{fn: func(path string) bool {
		info, err := os.Stat(path)
		return err == nil && info.Size() > 0
	}}

	firstRun := xaipipeline.NewOrchestrator(xaipipeline.Deps{
		Planner: &stubPlanner{out: xaipipeline.Manifest{
			ProjectID: "pending-provider-complete",
			Shots: []xaipipeline.Shot{
				{Index: 1, Prompt: "legacy shot"},
			},
		}},
		ShotGenerator: &stubShotGenerator{data: []byte("old")},
		Renderer:      &stubRenderer{},
		Validator:     validator,
	})
	if _, err := firstRun.Run(context.Background(), story, xaipipeline.RunOptions{OutputDir: outputDir}); err != nil {
		t.Fatalf("first Run() error = %v", err)
	}

	manifest, ok := readManifestFile(t, filepath.Join(outputDir, "xai_manifest.json"))
	if !ok {
		t.Fatal("xai_manifest.json missing after first run")
	}
	manifest.Shots[0].XAIStatus = "pending"
	writePreviousManifest(t, outputDir, manifest)

	secondPlanner := &stubPlanner{out: xaipipeline.Manifest{
		ProjectID: "fresh-plan",
		Shots: []xaipipeline.Shot{
			{Index: 1, Prompt: "fresh planned shot"},
		},
	}}
	secondGenerator := &stubShotGenerator{data: []byte("fresh")}
	secondRun := xaipipeline.NewOrchestrator(xaipipeline.Deps{
		Planner:       secondPlanner,
		ShotGenerator: secondGenerator,
		Renderer:      &stubRenderer{},
		Validator:     validator,
	})

	result, err := secondRun.Run(context.Background(), story, xaipipeline.RunOptions{OutputDir: outputDir})
	if err != nil {
		t.Fatalf("second Run() error = %v", err)
	}
	if secondPlanner.calls != 1 {
		t.Fatalf("second planner calls = %d, want 1", secondPlanner.calls)
	}
	if len(secondGenerator.calls) != 1 {
		t.Fatalf("second generator calls = %d, want 1", len(secondGenerator.calls))
	}
	if !result.RunMetadata.Planned || result.RunMetadata.ManifestReused {
		t.Fatalf("run metadata = %+v, want planned non-reused run", result.RunMetadata)
	}
	if result.Manifest.Shots[0].Prompt != "fresh planned shot" {
		t.Fatalf("prompt = %q, want fresh planned shot", result.Manifest.Shots[0].Prompt)
	}
}

func TestOrchestrator_RunDoesNotReuseExistingManifestForDifferentStory(t *testing.T) {
	t.Parallel()

	outputDir := t.TempDir()
	validator := stubValidator{fn: func(path string) bool {
		info, err := os.Stat(path)
		return err == nil && info.Size() > 0
	}}

	firstRun := xaipipeline.NewOrchestrator(xaipipeline.Deps{
		Planner: &stubPlanner{out: xaipipeline.Manifest{
			ProjectID: "changed-story",
			Shots: []xaipipeline.Shot{
				{Index: 1, Prompt: "old shot"},
			},
		}},
		ShotGenerator: &stubShotGenerator{data: []byte("old")},
		Renderer:      &stubRenderer{},
		Validator:     validator,
	})
	if _, err := firstRun.Run(context.Background(), []byte("old story"), xaipipeline.RunOptions{OutputDir: outputDir}); err != nil {
		t.Fatalf("first Run() error = %v", err)
	}

	secondPlanner := &stubPlanner{out: xaipipeline.Manifest{
		ProjectID: "changed-story",
		Shots: []xaipipeline.Shot{
			{Index: 1, Prompt: "new shot"},
		},
	}}
	secondGenerator := &stubShotGenerator{data: []byte("new")}
	secondRun := xaipipeline.NewOrchestrator(xaipipeline.Deps{
		Planner:       secondPlanner,
		ShotGenerator: secondGenerator,
		Renderer:      &stubRenderer{},
		Validator:     validator,
	})

	_, err := secondRun.Run(context.Background(), []byte("new story"), xaipipeline.RunOptions{OutputDir: outputDir})
	if err != nil {
		t.Fatalf("second Run() error = %v", err)
	}
	if secondPlanner.calls != 1 {
		t.Fatalf("second planner calls = %d, want 1", secondPlanner.calls)
	}
	if len(secondGenerator.calls) != 1 {
		t.Fatalf("second generator calls = %d, want 1", len(secondGenerator.calls))
	}
	if data, err := os.ReadFile(filepath.Join(outputDir, "shots", "shot_001.mp4")); err != nil || string(data) != "new" {
		t.Fatalf("shot not regenerated for changed story: err=%v data=%q", err, string(data))
	}
}

func TestOrchestrator_RunForceReplanBypassesReusableManifest(t *testing.T) {
	t.Parallel()

	outputDir := t.TempDir()
	validator := stubValidator{fn: func(path string) bool {
		info, err := os.Stat(path)
		return err == nil && info.Size() > 0
	}}

	firstRun := xaipipeline.NewOrchestrator(xaipipeline.Deps{
		Planner: &stubPlanner{out: xaipipeline.Manifest{
			ProjectID: "force-replan",
			Shots: []xaipipeline.Shot{
				{Index: 1, Prompt: "old shot"},
			},
		}},
		ShotGenerator: &stubShotGenerator{data: []byte("old")},
		Renderer:      &stubRenderer{},
		Validator:     validator,
	})
	if _, err := firstRun.Run(context.Background(), []byte("same story"), xaipipeline.RunOptions{OutputDir: outputDir}); err != nil {
		t.Fatalf("first Run() error = %v", err)
	}

	secondPlanner := &stubPlanner{out: xaipipeline.Manifest{
		ProjectID: "force-replan",
		Shots: []xaipipeline.Shot{
			{Index: 1, Prompt: "new planned shot"},
		},
	}}
	secondGenerator := &stubShotGenerator{data: []byte("new")}
	secondRun := xaipipeline.NewOrchestrator(xaipipeline.Deps{
		Planner:       secondPlanner,
		ShotGenerator: secondGenerator,
		Renderer:      &stubRenderer{},
		Validator:     validator,
	})

	_, err := secondRun.Run(context.Background(), []byte("same story"), xaipipeline.RunOptions{
		OutputDir:   outputDir,
		ForceReplan: true,
	})
	if err != nil {
		t.Fatalf("second Run() error = %v", err)
	}
	if secondPlanner.calls != 1 {
		t.Fatalf("second planner calls = %d, want 1", secondPlanner.calls)
	}
	if len(secondGenerator.calls) != 1 {
		t.Fatalf("second generator calls = %d, want 1", len(secondGenerator.calls))
	}
	if data, err := os.ReadFile(filepath.Join(outputDir, "shots", "shot_001.mp4")); err != nil || string(data) != "new" {
		t.Fatalf("shot not regenerated after force replan: err=%v data=%q", err, string(data))
	}
	metadata := readRunMetadata(t, outputDir)
	if !metadata.Planned || metadata.ManifestReused || !metadata.ForceReplan {
		t.Fatalf("run metadata = %+v, want force replanned run", metadata)
	}
}

func TestOrchestrator_RunForceRegenerateReusesManifestAndRewritesShots(t *testing.T) {
	t.Parallel()

	outputDir := t.TempDir()
	story := []byte("same story")
	validator := stubValidator{fn: func(path string) bool {
		info, err := os.Stat(path)
		return err == nil && info.Size() > 0
	}}

	firstRun := xaipipeline.NewOrchestrator(xaipipeline.Deps{
		Planner: &stubPlanner{out: xaipipeline.Manifest{
			ProjectID: "force-regenerate",
			Shots: []xaipipeline.Shot{
				{Index: 1, Prompt: "first"},
				{Index: 2, Prompt: "second"},
			},
		}},
		ShotGenerator: &stubShotGenerator{data: []byte("old")},
		Renderer:      &stubRenderer{},
		Validator:     validator,
	})
	if _, err := firstRun.Run(context.Background(), story, xaipipeline.RunOptions{OutputDir: outputDir}); err != nil {
		t.Fatalf("first Run() error = %v", err)
	}

	secondGenerator := &stubShotGenerator{data: []byte("new")}
	secondRenderer := &stubRenderer{}
	secondRun := xaipipeline.NewOrchestrator(xaipipeline.Deps{
		ShotGenerator: secondGenerator,
		Renderer:      secondRenderer,
		Validator:     validator,
	})

	result, err := secondRun.Run(context.Background(), story, xaipipeline.RunOptions{
		OutputDir:       outputDir,
		ForceRegenerate: true,
	})
	if err != nil {
		t.Fatalf("second Run() error = %v", err)
	}
	if len(secondGenerator.calls) != 2 {
		t.Fatalf("second generator calls = %d, want 2", len(secondGenerator.calls))
	}
	if secondRenderer.calls != 1 {
		t.Fatalf("second renderer calls = %d, want 1", secondRenderer.calls)
	}
	if result.RunMetadata.Planned {
		t.Fatal("run metadata planned = true, want false")
	}
	if !result.RunMetadata.ManifestReused {
		t.Fatal("run metadata manifest_reused = false, want true")
	}
	if !result.RunMetadata.ForceRegenerate {
		t.Fatal("run metadata force_regenerate = false, want true")
	}
	assertEqualInts(t, result.RunMetadata.GeneratedShots, []int{1, 2})
	assertEqualInts(t, result.RunMetadata.ReusedShots, nil)
	assertShotDecision(t, result.RunMetadata.ShotDecisions[0], xaipipeline.ShotDecision{
		Index:        1,
		Decision:     "generated",
		VideoPath:    "shots/shot_001.mp4",
		PromptHash:   result.Manifest.Shots[0].PromptHash,
		XAIRequestID: "req_stub",
		XAIStatus:    "done",
	})
	for _, shot := range result.Manifest.Shots {
		if data, err := os.ReadFile(filepath.Join(outputDir, shot.VideoPath)); err != nil || string(data) != "new" {
			t.Fatalf("shot %d not regenerated: err=%v data=%q", shot.Index, err, string(data))
		}
	}
}

func TestOrchestrator_RunForceRegenerateReusesManifestWhenVideoModelChanges(t *testing.T) {
	t.Parallel()

	outputDir := t.TempDir()
	story := []byte("same story")
	validator := stubValidator{fn: func(path string) bool {
		info, err := os.Stat(path)
		return err == nil && info.Size() > 0
	}}

	firstRun := xaipipeline.NewOrchestrator(xaipipeline.Deps{
		Planner: &stubPlanner{out: xaipipeline.Manifest{
			ProjectID: "force-regenerate-model",
			Shots: []xaipipeline.Shot{
				{Index: 1, Prompt: "same prompt"},
			},
		}},
		ShotGenerator: &stubShotGenerator{data: []byte("old-model")},
		Renderer:      &stubRenderer{},
		Validator:     validator,
	})
	firstResult, err := firstRun.Run(context.Background(), story, xaipipeline.RunOptions{OutputDir: outputDir})
	if err != nil {
		t.Fatalf("first Run() error = %v", err)
	}
	oldPromptHash := firstResult.Manifest.Shots[0].PromptHash

	secondGenerator := &stubShotGenerator{
		data:       []byte("new-model"),
		requestIDs: map[int]string{1: "req_new_model"},
		statuses:   map[int]string{1: "done"},
	}
	secondRenderer := &stubRenderer{}
	secondRun := xaipipeline.NewOrchestrator(xaipipeline.Deps{
		ShotGenerator: secondGenerator,
		Renderer:      secondRenderer,
		Validator:     validator,
	})

	result, err := secondRun.Run(context.Background(), story, xaipipeline.RunOptions{
		OutputDir:       outputDir,
		VideoModel:      "grok-imagine-video-v2",
		ForceRegenerate: true,
	})
	if err != nil {
		t.Fatalf("second Run() error = %v", err)
	}
	if len(secondGenerator.calls) != 1 {
		t.Fatalf("second generator calls = %d, want 1", len(secondGenerator.calls))
	}
	if result.RunMetadata.Planned {
		t.Fatal("run metadata planned = true, want false")
	}
	if !result.RunMetadata.ManifestReused {
		t.Fatal("run metadata manifest_reused = false, want true")
	}
	if result.Manifest.VideoModel != "grok-imagine-video-v2" {
		t.Fatalf("manifest video_model = %q, want grok-imagine-video-v2", result.Manifest.VideoModel)
	}
	if result.RunMetadata.VideoModel != "grok-imagine-video-v2" {
		t.Fatalf("run metadata video_model = %q, want grok-imagine-video-v2", result.RunMetadata.VideoModel)
	}
	if result.Manifest.Shots[0].PromptHash == oldPromptHash {
		t.Fatal("prompt_hash did not change after video_model changed")
	}
	if data, err := os.ReadFile(filepath.Join(outputDir, "shots", "shot_001.mp4")); err != nil || string(data) != "new-model" {
		t.Fatalf("shot not regenerated with new model: err=%v data=%q", err, string(data))
	}
	assertEqualInts(t, result.RunMetadata.GeneratedShots, []int{1})
	assertEqualInts(t, result.RunMetadata.ReusedShots, nil)
}

func TestOrchestrator_RunFailedForceRegeneratePreservesExistingShot(t *testing.T) {
	t.Parallel()

	outputDir := t.TempDir()
	story := []byte("same story")
	validator := stubValidator{fn: func(path string) bool {
		data, err := os.ReadFile(path)
		return err == nil && string(data) == "old"
	}}

	firstRun := xaipipeline.NewOrchestrator(xaipipeline.Deps{
		Planner: &stubPlanner{out: xaipipeline.Manifest{
			ProjectID: "failed-force-regenerate",
			Shots: []xaipipeline.Shot{
				{Index: 1, Prompt: "first"},
			},
		}},
		ShotGenerator: &stubShotGenerator{data: []byte("old")},
		Renderer:      &stubRenderer{},
		Validator:     validator,
	})
	if _, err := firstRun.Run(context.Background(), story, xaipipeline.RunOptions{OutputDir: outputDir}); err != nil {
		t.Fatalf("first Run() error = %v", err)
	}

	secondRun := xaipipeline.NewOrchestrator(xaipipeline.Deps{
		ShotGenerator: &stubShotGenerator{data: []byte("invalid")},
		Renderer:      &stubRenderer{},
		Validator:     validator,
	})
	_, err := secondRun.Run(context.Background(), story, xaipipeline.RunOptions{
		OutputDir:       outputDir,
		ForceRegenerate: true,
	})
	if err == nil {
		t.Fatal("second Run() error = nil, want generated shot validation error")
	}
	if !strings.Contains(err.Error(), "generated xai shot failed validation") {
		t.Fatalf("second Run() error = %v, want generated shot validation error", err)
	}
	shotPath := filepath.Join(outputDir, "shots", "shot_001.mp4")
	if data, readErr := os.ReadFile(shotPath); readErr != nil || string(data) != "old" {
		t.Fatalf("existing shot should be preserved after failed regenerate: err=%v data=%q", readErr, string(data))
	}
}

func TestOrchestrator_RunFailedMultiShotForceRegeneratePreservesAllExistingShots(t *testing.T) {
	t.Parallel()

	outputDir := t.TempDir()
	story := []byte("same story")
	validator := stubValidator{fn: func(path string) bool {
		data, err := os.ReadFile(path)
		return err == nil && !strings.Contains(string(data), "invalid")
	}}

	firstRun := xaipipeline.NewOrchestrator(xaipipeline.Deps{
		Planner: &stubPlanner{out: xaipipeline.Manifest{
			ProjectID: "failed-multi-force-regenerate",
			Shots: []xaipipeline.Shot{
				{Index: 1, Prompt: "first"},
				{Index: 2, Prompt: "second"},
			},
		}},
		ShotGenerator: &stubShotGenerator{dataByIndex: map[int][]byte{
			1: []byte("old-1"),
			2: []byte("old-2"),
		}},
		Renderer:  &stubRenderer{},
		Validator: validator,
	})
	if _, err := firstRun.Run(context.Background(), story, xaipipeline.RunOptions{OutputDir: outputDir}); err != nil {
		t.Fatalf("first Run() error = %v", err)
	}

	secondRun := xaipipeline.NewOrchestrator(xaipipeline.Deps{
		ShotGenerator: &stubShotGenerator{dataByIndex: map[int][]byte{
			1: []byte("new-1"),
			2: []byte("invalid-2"),
		}},
		Renderer:  &stubRenderer{},
		Validator: validator,
	})
	_, err := secondRun.Run(context.Background(), story, xaipipeline.RunOptions{
		OutputDir:       outputDir,
		ForceRegenerate: true,
	})
	if err == nil {
		t.Fatal("second Run() error = nil, want generated shot validation error")
	}
	if !strings.Contains(err.Error(), "generated xai shot failed validation") {
		t.Fatalf("second Run() error = %v, want generated shot validation error", err)
	}
	for index, want := range map[int]string{1: "old-1", 2: "old-2"} {
		shotPath := filepath.Join(outputDir, "shots", fmt.Sprintf("shot_%03d.mp4", index))
		if data, readErr := os.ReadFile(shotPath); readErr != nil || string(data) != want {
			t.Fatalf("shot %d should be preserved after failed regenerate: err=%v data=%q want=%q", index, readErr, string(data), want)
		}
	}
}

func TestOrchestrator_RunCommitFailureRollsBackAlreadyCommittedShots(t *testing.T) {
	t.Parallel()

	outputDir := t.TempDir()
	story := []byte("same story")
	validator := stubValidator{fn: func(path string) bool {
		info, err := os.Stat(path)
		return err == nil && !info.IsDir() && info.Size() > 0
	}}

	firstRun := xaipipeline.NewOrchestrator(xaipipeline.Deps{
		Planner: &stubPlanner{out: xaipipeline.Manifest{
			ProjectID: "commit-rollback",
			Shots: []xaipipeline.Shot{
				{Index: 1, Prompt: "first"},
				{Index: 2, Prompt: "second"},
			},
		}},
		ShotGenerator: &stubShotGenerator{dataByIndex: map[int][]byte{
			1: []byte("old-1"),
			2: []byte("old-2"),
		}},
		Renderer:  &stubRenderer{},
		Validator: validator,
	})
	if _, err := firstRun.Run(context.Background(), story, xaipipeline.RunOptions{OutputDir: outputDir}); err != nil {
		t.Fatalf("first Run() error = %v", err)
	}

	blockedShotPath := filepath.Join(outputDir, "shots", "shot_002.mp4")
	if err := os.Remove(blockedShotPath); err != nil {
		t.Fatalf("remove shot_002.mp4: %v", err)
	}
	if err := os.Mkdir(blockedShotPath, 0755); err != nil {
		t.Fatalf("mkdir shot_002.mp4 blocker: %v", err)
	}

	secondRun := xaipipeline.NewOrchestrator(xaipipeline.Deps{
		ShotGenerator: &stubShotGenerator{dataByIndex: map[int][]byte{
			1: []byte("new-1"),
			2: []byte("new-2"),
		}},
		Renderer:  &stubRenderer{},
		Validator: validator,
	})
	_, err := secondRun.Run(context.Background(), story, xaipipeline.RunOptions{
		OutputDir:       outputDir,
		ForceRegenerate: true,
	})
	if err == nil {
		t.Fatal("second Run() error = nil, want commit error")
	}
	if !strings.Contains(err.Error(), "commit xai shot 2") {
		t.Fatalf("second Run() error = %v, want commit shot 2 error", err)
	}
	shot1Path := filepath.Join(outputDir, "shots", "shot_001.mp4")
	if data, readErr := os.ReadFile(shot1Path); readErr != nil || string(data) != "old-1" {
		t.Fatalf("shot 1 should be rolled back after shot 2 commit failure: err=%v data=%q", readErr, string(data))
	}
}

func TestOrchestrator_RunMetadataWriteFailurePreservesExistingManifest(t *testing.T) {
	t.Parallel()

	outputDir := t.TempDir()
	story := []byte("same story")
	validator := stubValidator{fn: func(path string) bool {
		info, err := os.Stat(path)
		return err == nil && !info.IsDir() && info.Size() > 0
	}}

	firstRun := xaipipeline.NewOrchestrator(xaipipeline.Deps{
		Planner: &stubPlanner{out: xaipipeline.Manifest{
			ProjectID: "metadata-rollback",
			Shots: []xaipipeline.Shot{
				{Index: 1, Prompt: "old prompt"},
			},
		}},
		ShotGenerator: &stubShotGenerator{data: []byte("old")},
		Renderer:      &stubRenderer{},
		Validator:     validator,
	})
	if _, err := firstRun.Run(context.Background(), story, xaipipeline.RunOptions{OutputDir: outputDir}); err != nil {
		t.Fatalf("first Run() error = %v", err)
	}

	runMetadataPath := filepath.Join(outputDir, "xai_run_metadata.json")
	if err := os.Remove(runMetadataPath); err != nil {
		t.Fatalf("remove run metadata: %v", err)
	}
	if err := os.Mkdir(runMetadataPath, 0755); err != nil {
		t.Fatalf("mkdir run metadata blocker: %v", err)
	}

	secondRenderer := &stubRenderer{}
	secondRun := xaipipeline.NewOrchestrator(xaipipeline.Deps{
		Planner: &stubPlanner{out: xaipipeline.Manifest{
			ProjectID: "metadata-rollback",
			Shots: []xaipipeline.Shot{
				{Index: 1, Prompt: "new prompt"},
			},
		}},
		ShotGenerator: &stubShotGenerator{data: []byte("new")},
		Renderer:      secondRenderer,
		Validator:     validator,
	})
	_, err := secondRun.Run(context.Background(), story, xaipipeline.RunOptions{
		OutputDir:   outputDir,
		ForceReplan: true,
	})
	if err == nil {
		t.Fatal("second Run() error = nil, want run metadata write error")
	}
	if !strings.Contains(err.Error(), "write xai run metadata") {
		t.Fatalf("second Run() error = %v, want run metadata write error", err)
	}
	manifest, ok := readManifestFile(t, filepath.Join(outputDir, "xai_manifest.json"))
	if !ok {
		t.Fatal("xai_manifest.json should remain readable")
	}
	if got := manifest.Shots[0].Prompt; got != "old prompt" {
		t.Fatalf("manifest prompt = %q, want old prompt after metadata write failure", got)
	}
	shotPath := filepath.Join(outputDir, "shots", "shot_001.mp4")
	if data, readErr := os.ReadFile(shotPath); readErr != nil || string(data) != "old" {
		t.Fatalf("shot should remain old after metadata write failure: err=%v data=%q", readErr, string(data))
	}
	if secondRenderer.calls != 0 {
		t.Fatalf("renderer calls = %d, want 0 after metadata write failure", secondRenderer.calls)
	}
}

func TestOrchestrator_RunRejectsGeneratedShotMissingProviderMetadata(t *testing.T) {
	t.Parallel()

	outputDir := t.TempDir()
	planner := &stubPlanner{out: xaipipeline.Manifest{
		ProjectID: "missing-provider-metadata",
		Shots: []xaipipeline.Shot{
			{Index: 1, Prompt: "shot missing metadata"},
		},
	}}
	generator := &stubShotGenerator{
		data:         []byte("mp4"),
		omitMetadata: true,
	}
	renderer := &stubRenderer{}
	orch := xaipipeline.NewOrchestrator(xaipipeline.Deps{
		Planner:       planner,
		ShotGenerator: generator,
		Renderer:      renderer,
	})

	_, err := orch.Run(context.Background(), []byte("story"), xaipipeline.RunOptions{OutputDir: outputDir})
	if err == nil {
		t.Fatal("Run() error = nil, want missing provider metadata error")
	}
	if !strings.Contains(err.Error(), "missing xai provider metadata") {
		t.Fatalf("Run() error = %v, want missing provider metadata", err)
	}
	if renderer.calls != 0 {
		t.Fatalf("renderer calls = %d, want 0", renderer.calls)
	}
	if _, statErr := os.Stat(filepath.Join(outputDir, "xai_manifest.json")); !os.IsNotExist(statErr) {
		t.Fatalf("xai_manifest.json should not be written, stat err=%v", statErr)
	}
}

func TestOrchestrator_RunRejectsGeneratedShotWithIncompleteProviderStatus(t *testing.T) {
	t.Parallel()

	outputDir := t.TempDir()
	planner := &stubPlanner{out: xaipipeline.Manifest{
		ProjectID: "pending-provider-status",
		Shots: []xaipipeline.Shot{
			{Index: 1, Prompt: "shot with pending status"},
		},
	}}
	generator := &stubShotGenerator{
		data:       []byte("mp4"),
		requestIDs: map[int]string{1: "req_pending"},
		statuses:   map[int]string{1: "pending"},
	}
	renderer := &stubRenderer{}
	orch := xaipipeline.NewOrchestrator(xaipipeline.Deps{
		Planner:       planner,
		ShotGenerator: generator,
		Renderer:      renderer,
	})

	_, err := orch.Run(context.Background(), []byte("story"), xaipipeline.RunOptions{OutputDir: outputDir})
	if err == nil {
		t.Fatal("Run() error = nil, want incomplete provider status error")
	}
	if !strings.Contains(err.Error(), "xai_status") || !strings.Contains(err.Error(), "pending") {
		t.Fatalf("Run() error = %v, want incomplete xai_status error", err)
	}
	if renderer.calls != 0 {
		t.Fatalf("renderer calls = %d, want 0", renderer.calls)
	}
	if _, statErr := os.Stat(filepath.Join(outputDir, "shots", "shot_001.mp4")); !os.IsNotExist(statErr) {
		t.Fatalf("shot file should not be written, stat err=%v", statErr)
	}
	if _, statErr := os.Stat(filepath.Join(outputDir, "xai_manifest.json")); !os.IsNotExist(statErr) {
		t.Fatalf("xai_manifest.json should not be written, stat err=%v", statErr)
	}
}

func TestOrchestrator_RunRejectsCanceledContextAfterShotGenerationBeforeArtifacts(t *testing.T) {
	t.Parallel()

	outputDir := t.TempDir()
	ctx, cancel := context.WithCancel(context.Background())
	planner := &stubPlanner{out: xaipipeline.Manifest{
		ProjectID: "cancelled-after-shot-generation",
		Shots: []xaipipeline.Shot{
			{Index: 1, Prompt: "shot canceled after provider"},
		},
	}}
	generator := &stubShotGenerator{
		data:          []byte("mp4"),
		afterGenerate: func(xaipipeline.Shot) { cancel() },
	}
	renderer := &stubRenderer{}
	orch := xaipipeline.NewOrchestrator(xaipipeline.Deps{
		Planner:       planner,
		ShotGenerator: generator,
		Renderer:      renderer,
		Validator:     stubValidator{},
	})

	got, err := orch.Run(ctx, []byte("story"), xaipipeline.RunOptions{OutputDir: outputDir})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Run() error = %v, want context.Canceled", err)
	}
	if got != nil {
		t.Fatalf("Run() result = %+v, want nil on canceled context", got)
	}
	if len(generator.calls) != 1 {
		t.Fatalf("generator calls = %d, want 1", len(generator.calls))
	}
	if renderer.calls != 0 {
		t.Fatalf("renderer calls = %d, want 0", renderer.calls)
	}
	if _, statErr := os.Stat(filepath.Join(outputDir, "shots", "shot_001.mp4")); !os.IsNotExist(statErr) {
		t.Fatalf("shot file should not be written after context cancellation, stat err=%v", statErr)
	}
	if _, statErr := os.Stat(filepath.Join(outputDir, "xai_manifest.json")); !os.IsNotExist(statErr) {
		t.Fatalf("xai_manifest.json should not be written after context cancellation, stat err=%v", statErr)
	}
}

func TestOrchestrator_RunRejectsCanceledContextAfterShotValidationBeforeCommit(t *testing.T) {
	t.Parallel()

	outputDir := t.TempDir()
	ctx, cancel := context.WithCancel(context.Background())
	planner := &stubPlanner{out: xaipipeline.Manifest{
		ProjectID: "cancelled-after-shot-validation",
		Shots: []xaipipeline.Shot{
			{Index: 1, Prompt: "shot canceled after validation"},
		},
	}}
	generator := &stubShotGenerator{data: []byte("mp4")}
	renderer := &stubRenderer{}
	orch := xaipipeline.NewOrchestrator(xaipipeline.Deps{
		Planner:       planner,
		ShotGenerator: generator,
		Renderer:      renderer,
		Validator: stubValidator{fn: func(path string) bool {
			info, err := os.Stat(path)
			if err == nil && info.Size() > 0 {
				cancel()
				return true
			}
			return false
		}},
	})

	got, err := orch.Run(ctx, []byte("story"), xaipipeline.RunOptions{OutputDir: outputDir})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Run() error = %v, want context.Canceled", err)
	}
	if got != nil {
		t.Fatalf("Run() result = %+v, want nil on canceled context", got)
	}
	if len(generator.calls) != 1 {
		t.Fatalf("generator calls = %d, want 1", len(generator.calls))
	}
	if renderer.calls != 0 {
		t.Fatalf("renderer calls = %d, want 0", renderer.calls)
	}
	if _, statErr := os.Stat(filepath.Join(outputDir, "shots", "shot_001.mp4")); !os.IsNotExist(statErr) {
		t.Fatalf("shot file should not be committed after context cancellation, stat err=%v", statErr)
	}
	if _, statErr := os.Stat(filepath.Join(outputDir, "xai_manifest.json")); !os.IsNotExist(statErr) {
		t.Fatalf("xai_manifest.json should not be written after context cancellation, stat err=%v", statErr)
	}
}

func TestOrchestrator_RunCanonicalizesGeneratedShotProviderStatus(t *testing.T) {
	t.Parallel()

	outputDir := t.TempDir()
	planner := &stubPlanner{out: xaipipeline.Manifest{
		ProjectID: "canonical-generated-status",
		Shots: []xaipipeline.Shot{
			{Index: 1, Prompt: "shot with uppercase status"},
		},
	}}
	generator := &stubShotGenerator{
		data:       []byte("mp4"),
		requestIDs: map[int]string{1: "req_done"},
		statuses:   map[int]string{1: " DONE "},
	}
	orch := xaipipeline.NewOrchestrator(xaipipeline.Deps{
		Planner:       planner,
		ShotGenerator: generator,
		Renderer:      &stubRenderer{},
		Validator:     stubValidator{},
	})

	result, err := orch.Run(context.Background(), []byte("story"), xaipipeline.RunOptions{OutputDir: outputDir})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if result.Manifest.Shots[0].XAIStatus != "done" {
		t.Fatalf("xai_status = %q, want done", result.Manifest.Shots[0].XAIStatus)
	}
	if result.RunMetadata.ShotDecisions[0].XAIStatus != "done" {
		t.Fatalf("run metadata xai_status = %q, want done", result.RunMetadata.ShotDecisions[0].XAIStatus)
	}
}

func TestOrchestrator_RunRejectsGeneratedShotThatFailsValidation(t *testing.T) {
	t.Parallel()

	outputDir := t.TempDir()
	planner := &stubPlanner{out: xaipipeline.Manifest{
		ProjectID: "invalid-generated-shot",
		Shots: []xaipipeline.Shot{
			{Index: 1, Prompt: "invalid shot"},
		},
	}}
	generator := &stubShotGenerator{
		data:       []byte("not an mp4"),
		requestIDs: map[int]string{1: "req_invalid"},
		statuses:   map[int]string{1: "done"},
	}
	renderer := &stubRenderer{}
	var validatedPath string
	validator := stubValidator{fn: func(path string) bool {
		validatedPath = path
		return false
	}}
	orch := xaipipeline.NewOrchestrator(xaipipeline.Deps{
		Planner:       planner,
		ShotGenerator: generator,
		Renderer:      renderer,
		Validator:     validator,
	})

	_, err := orch.Run(context.Background(), []byte("story"), xaipipeline.RunOptions{OutputDir: outputDir})
	if err == nil {
		t.Fatal("Run() error = nil, want generated shot validation error")
	}
	if !strings.Contains(err.Error(), "generated xai shot failed validation") {
		t.Fatalf("Run() error = %v, want generated shot validation error", err)
	}
	shotsDir := filepath.Join(outputDir, "shots")
	if filepath.Dir(validatedPath) != shotsDir || !strings.Contains(filepath.Base(validatedPath), ".tmp.mp4") {
		t.Fatalf("validated path = %q, want temporary shot under %q", validatedPath, shotsDir)
	}
	if renderer.calls != 0 {
		t.Fatalf("renderer calls = %d, want 0", renderer.calls)
	}
	if _, statErr := os.Stat(filepath.Join(outputDir, "shots", "shot_001.mp4")); !os.IsNotExist(statErr) {
		t.Fatalf("shot_001.mp4 should not be committed, stat err=%v", statErr)
	}
	if _, statErr := os.Stat(filepath.Join(outputDir, "xai_manifest.json")); !os.IsNotExist(statErr) {
		t.Fatalf("xai_manifest.json should not be written, stat err=%v", statErr)
	}
	if _, statErr := os.Stat(filepath.Join(outputDir, "xai_run_metadata.json")); !os.IsNotExist(statErr) {
		t.Fatalf("xai_run_metadata.json should not be written, stat err=%v", statErr)
	}
}

func TestOrchestrator_RunRequiresShotValidatorForGeneratedShots(t *testing.T) {
	t.Parallel()

	outputDir := t.TempDir()
	planner := &stubPlanner{out: xaipipeline.Manifest{
		ProjectID: "missing-shot-validator",
		Shots: []xaipipeline.Shot{
			{Index: 1, Prompt: "needs validation"},
		},
	}}
	generator := &stubShotGenerator{
		data:       []byte("mp4"),
		requestIDs: map[int]string{1: "req_001"},
		statuses:   map[int]string{1: "done"},
	}
	renderer := &stubRenderer{}
	orch := xaipipeline.NewOrchestrator(xaipipeline.Deps{
		Planner:       planner,
		ShotGenerator: generator,
		Renderer:      renderer,
	})

	_, err := orch.Run(context.Background(), []byte("story"), xaipipeline.RunOptions{OutputDir: outputDir})
	if err == nil {
		t.Fatal("Run() error = nil, want missing shot validator error")
	}
	if !strings.Contains(err.Error(), "xai pipeline shot validator is nil") {
		t.Fatalf("Run() error = %v, want missing shot validator error", err)
	}
	if renderer.calls != 0 {
		t.Fatalf("renderer calls = %d, want 0", renderer.calls)
	}
	if _, statErr := os.Stat(filepath.Join(outputDir, "shots", "shot_001.mp4")); !os.IsNotExist(statErr) {
		t.Fatalf("shot file should not be written without validator, stat err=%v", statErr)
	}
}

func TestOrchestrator_RunRejectsEmptyStory(t *testing.T) {
	t.Parallel()

	orch := xaipipeline.NewOrchestrator(xaipipeline.Deps{
		Planner:       &stubPlanner{},
		ShotGenerator: &stubShotGenerator{},
		Renderer:      &stubRenderer{},
	})

	if _, err := orch.Run(context.Background(), []byte(" \n\t "), xaipipeline.RunOptions{OutputDir: t.TempDir()}); err == nil {
		t.Fatal("Run() error = nil, want error")
	}
}

func TestOrchestrator_RunRejectsCanceledContextBeforePreparingOutputRoot(t *testing.T) {
	t.Parallel()

	outputDir := filepath.Join(t.TempDir(), "xai-output")
	planner := &stubPlanner{out: xaipipeline.Manifest{
		ProjectID: "cancelled-single",
		Shots: []xaipipeline.Shot{
			{Index: 1, Prompt: "wide wasteland"},
		},
	}}
	generator := &stubShotGenerator{data: []byte("mp4-bytes")}
	renderer := &stubRenderer{}
	orch := xaipipeline.NewOrchestrator(xaipipeline.Deps{
		Planner:       planner,
		ShotGenerator: generator,
		Renderer:      renderer,
		Validator:     stubValidator{},
	})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	got, err := orch.Run(ctx, []byte("story"), xaipipeline.RunOptions{OutputDir: outputDir})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Run() error = %v, want context.Canceled", err)
	}
	if got != nil {
		t.Fatalf("Run() result = %+v, want nil on canceled context", got)
	}
	if planner.calls != 0 {
		t.Fatalf("planner calls = %d, want 0", planner.calls)
	}
	if len(generator.calls) != 0 {
		t.Fatalf("generator calls = %d, want 0", len(generator.calls))
	}
	if renderer.calls != 0 {
		t.Fatalf("renderer calls = %d, want 0", renderer.calls)
	}
	if _, err := os.Stat(outputDir); !os.IsNotExist(err) {
		t.Fatalf("output dir should not be created after context cancellation, stat err=%v", err)
	}
}

func TestOrchestrator_RunRejectsCanceledContextAfterPlanningBeforeArtifacts(t *testing.T) {
	t.Parallel()

	outputDir := filepath.Join(t.TempDir(), "xai-output")
	ctx, cancel := context.WithCancel(context.Background())
	planner := &stubPlanner{
		out: xaipipeline.Manifest{
			ProjectID: "cancelled-after-plan",
			Shots: []xaipipeline.Shot{
				{Index: 1, Prompt: "wide wasteland"},
			},
		},
		afterPlan: cancel,
	}
	generator := &stubShotGenerator{data: []byte("mp4-bytes")}
	renderer := &stubRenderer{}
	orch := xaipipeline.NewOrchestrator(xaipipeline.Deps{
		Planner:       planner,
		ShotGenerator: generator,
		Renderer:      renderer,
		Validator:     stubValidator{},
	})

	got, err := orch.Run(ctx, []byte("story"), xaipipeline.RunOptions{OutputDir: outputDir})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Run() error = %v, want context.Canceled", err)
	}
	if got != nil {
		t.Fatalf("Run() result = %+v, want nil on canceled context", got)
	}
	if planner.calls != 1 {
		t.Fatalf("planner calls = %d, want 1", planner.calls)
	}
	if len(generator.calls) != 0 {
		t.Fatalf("generator calls = %d, want 0", len(generator.calls))
	}
	if renderer.calls != 0 {
		t.Fatalf("renderer calls = %d, want 0", renderer.calls)
	}
	if _, err := os.Stat(outputDir); !os.IsNotExist(err) {
		t.Fatalf("output dir should not be created after context cancellation, stat err=%v", err)
	}
}

func writePreviousManifest(t *testing.T, outputDir string, manifest xaipipeline.Manifest) {
	t.Helper()
	data, err := json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(outputDir, "xai_manifest.json"), data, 0644); err != nil {
		t.Fatal(err)
	}
}

func testShotPromptHash(prompt string) string {
	return testShotPromptHashForModel(prompt, "grok-imagine-video")
}

func testShotPromptHashForModel(prompt string, videoModel string) string {
	sum := sha256.Sum256([]byte(fmt.Sprintf(
		"%s\n%s\n%.3f\n%s\n%s",
		strings.TrimSpace(videoModel),
		strings.TrimSpace(prompt),
		8.0,
		"9:16",
		"720p",
	)))
	return hex.EncodeToString(sum[:])
}

func testStoryHash(story string) string {
	sum := sha256.Sum256([]byte(strings.TrimSpace(story)))
	return hex.EncodeToString(sum[:])
}

func readRunMetadata(t *testing.T, outputDir string) xaipipeline.RunMetadata {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(outputDir, "xai_run_metadata.json"))
	if err != nil {
		t.Fatalf("read xai_run_metadata.json: %v", err)
	}
	var metadata xaipipeline.RunMetadata
	if err := json.Unmarshal(data, &metadata); err != nil {
		t.Fatalf("parse xai_run_metadata.json: %v", err)
	}
	return metadata
}

func readManifestFile(t *testing.T, path string) (xaipipeline.Manifest, bool) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		return xaipipeline.Manifest{}, false
	}
	var manifest xaipipeline.Manifest
	if err := json.Unmarshal(data, &manifest); err != nil {
		t.Fatalf("parse xai manifest: %v", err)
	}
	return manifest, true
}

func assertEqualInts(t *testing.T, got []int, want []int) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("ints = %v, want %v", got, want)
	}
	for i := range got {
		if got[i] != want[i] {
			t.Fatalf("ints = %v, want %v", got, want)
		}
	}
}

func assertShotDecision(t *testing.T, got xaipipeline.ShotDecision, want xaipipeline.ShotDecision) {
	t.Helper()
	if got != want {
		t.Fatalf("shot decision = %+v, want %+v", got, want)
	}
}

func TestOrchestrator_RunRejectsPlannerWithNoShots(t *testing.T) {
	t.Parallel()

	orch := xaipipeline.NewOrchestrator(xaipipeline.Deps{
		Planner:       &stubPlanner{out: xaipipeline.Manifest{ProjectID: "empty"}},
		ShotGenerator: &stubShotGenerator{},
		Renderer:      &stubRenderer{},
	})

	if _, err := orch.Run(context.Background(), []byte("story"), xaipipeline.RunOptions{OutputDir: t.TempDir()}); err == nil {
		t.Fatal("Run() error = nil, want error")
	}
}

func TestOrchestrator_RunRejectsUnsupportedFormatBeforePlanning(t *testing.T) {
	t.Parallel()

	planner := &stubPlanner{out: xaipipeline.Manifest{
		ProjectID: "landscape",
		Shots: []xaipipeline.Shot{
			{Index: 1, Prompt: "wide shot"},
		},
	}}
	generator := &stubShotGenerator{data: []byte("mp4")}
	renderer := &stubRenderer{}
	orch := xaipipeline.NewOrchestrator(xaipipeline.Deps{
		Planner:       planner,
		ShotGenerator: generator,
		Renderer:      renderer,
	})

	_, err := orch.Run(context.Background(), []byte("story"), xaipipeline.RunOptions{
		OutputDir: t.TempDir(),
		Format:    "landscape",
	})
	if err == nil {
		t.Fatal("Run() error = nil, want unsupported format error")
	}
	if !strings.Contains(err.Error(), "unsupported xai-native format") {
		t.Fatalf("Run() error = %v, want unsupported format", err)
	}
	if planner.calls != 0 {
		t.Fatalf("planner calls = %d, want 0", planner.calls)
	}
	if len(generator.calls) != 0 {
		t.Fatalf("generator calls = %d, want 0", len(generator.calls))
	}
	if renderer.calls != 0 {
		t.Fatalf("renderer calls = %d, want 0", renderer.calls)
	}
}

func TestOrchestrator_RunRejectsNegativeTargetShotsBeforePlanning(t *testing.T) {
	t.Parallel()

	planner := &stubPlanner{out: xaipipeline.Manifest{
		ProjectID: "negative-target",
		Shots: []xaipipeline.Shot{
			{Index: 1, Prompt: "wide shot"},
		},
	}}
	generator := &stubShotGenerator{data: []byte("mp4")}
	renderer := &stubRenderer{}
	orch := xaipipeline.NewOrchestrator(xaipipeline.Deps{
		Planner:       planner,
		ShotGenerator: generator,
		Renderer:      renderer,
	})

	_, err := orch.Run(context.Background(), []byte("story"), xaipipeline.RunOptions{
		OutputDir:   t.TempDir(),
		TargetShots: -1,
	})
	if err == nil {
		t.Fatal("Run() error = nil, want negative target shots error")
	}
	if !strings.Contains(err.Error(), "target shots must be zero or greater") {
		t.Fatalf("Run() error = %v, want target shots validation error", err)
	}
	if planner.calls != 0 {
		t.Fatalf("planner calls = %d, want 0", planner.calls)
	}
	if len(generator.calls) != 0 {
		t.Fatalf("generator calls = %d, want 0", len(generator.calls))
	}
	if renderer.calls != 0 {
		t.Fatalf("renderer calls = %d, want 0", renderer.calls)
	}
}

func TestOrchestrator_RunRejectsPlannerFormatMismatchBeforeGeneration(t *testing.T) {
	t.Parallel()

	generator := &stubShotGenerator{data: []byte("mp4")}
	renderer := &stubRenderer{}
	orch := xaipipeline.NewOrchestrator(xaipipeline.Deps{
		Planner: &stubPlanner{out: xaipipeline.Manifest{
			ProjectID: "format-mismatch",
			Format:    "landscape",
			Shots: []xaipipeline.Shot{
				{Index: 1, Prompt: "wide shot"},
			},
		}},
		ShotGenerator: generator,
		Renderer:      renderer,
	})

	_, err := orch.Run(context.Background(), []byte("story"), xaipipeline.RunOptions{
		OutputDir: t.TempDir(),
		Format:    "portrait",
	})
	if err == nil {
		t.Fatal("Run() error = nil, want format mismatch error")
	}
	if !strings.Contains(err.Error(), "format") || !strings.Contains(err.Error(), "portrait") {
		t.Fatalf("Run() error = %v, want format mismatch", err)
	}
	if len(generator.calls) != 0 {
		t.Fatalf("generator calls = %d, want 0", len(generator.calls))
	}
	if renderer.calls != 0 {
		t.Fatalf("renderer calls = %d, want 0", renderer.calls)
	}
}

func TestOrchestrator_RunRejectsPlannerRenderDimensionsMismatchBeforeGeneration(t *testing.T) {
	t.Parallel()

	generator := &stubShotGenerator{data: []byte("mp4")}
	renderer := &stubRenderer{}
	orch := xaipipeline.NewOrchestrator(xaipipeline.Deps{
		Planner: &stubPlanner{out: xaipipeline.Manifest{
			ProjectID: "dimension-mismatch",
			Format:    "portrait",
			Width:     1024,
			Height:    576,
			Shots: []xaipipeline.Shot{
				{Index: 1, Prompt: "wide shot"},
			},
		}},
		ShotGenerator: generator,
		Renderer:      renderer,
	})

	_, err := orch.Run(context.Background(), []byte("story"), xaipipeline.RunOptions{
		OutputDir: t.TempDir(),
		Format:    "portrait",
	})
	if err == nil {
		t.Fatal("Run() error = nil, want render dimension error")
	}
	if !strings.Contains(err.Error(), "render dimensions") || !strings.Contains(err.Error(), "720x1280") {
		t.Fatalf("Run() error = %v, want render dimensions mismatch", err)
	}
	if len(generator.calls) != 0 {
		t.Fatalf("generator calls = %d, want 0", len(generator.calls))
	}
	if renderer.calls != 0 {
		t.Fatalf("renderer calls = %d, want 0", renderer.calls)
	}
}

func TestOrchestrator_RunRejectsPlannerFPSMismatchBeforeGeneration(t *testing.T) {
	t.Parallel()

	generator := &stubShotGenerator{data: []byte("mp4")}
	renderer := &stubRenderer{}
	orch := xaipipeline.NewOrchestrator(xaipipeline.Deps{
		Planner: &stubPlanner{out: xaipipeline.Manifest{
			ProjectID: "fps-mismatch",
			Format:    "portrait",
			FPS:       30,
			Width:     720,
			Height:    1280,
			Shots: []xaipipeline.Shot{
				{Index: 1, Prompt: "wide shot"},
			},
		}},
		ShotGenerator: generator,
		Renderer:      renderer,
	})

	_, err := orch.Run(context.Background(), []byte("story"), xaipipeline.RunOptions{
		OutputDir: t.TempDir(),
		Format:    "portrait",
	})
	if err == nil {
		t.Fatal("Run() error = nil, want fps error")
	}
	if !strings.Contains(err.Error(), "fps") || !strings.Contains(err.Error(), "24") {
		t.Fatalf("Run() error = %v, want fps mismatch", err)
	}
	if len(generator.calls) != 0 {
		t.Fatalf("generator calls = %d, want 0", len(generator.calls))
	}
	if renderer.calls != 0 {
		t.Fatalf("renderer calls = %d, want 0", renderer.calls)
	}
}

func TestOrchestrator_RunRejectsPlannerShotAspectRatioMismatchBeforeGeneration(t *testing.T) {
	t.Parallel()

	generator := &stubShotGenerator{data: []byte("mp4")}
	renderer := &stubRenderer{}
	orch := xaipipeline.NewOrchestrator(xaipipeline.Deps{
		Planner: &stubPlanner{out: xaipipeline.Manifest{
			ProjectID: "aspect-mismatch",
			Format:    "portrait",
			FPS:       24,
			Width:     720,
			Height:    1280,
			Shots: []xaipipeline.Shot{
				{Index: 1, Prompt: "wide shot", AspectRatio: "16:9"},
			},
		}},
		ShotGenerator: generator,
		Renderer:      renderer,
	})

	_, err := orch.Run(context.Background(), []byte("story"), xaipipeline.RunOptions{
		OutputDir: t.TempDir(),
		Format:    "portrait",
	})
	if err == nil {
		t.Fatal("Run() error = nil, want aspect ratio error")
	}
	if !strings.Contains(err.Error(), "aspect_ratio") || !strings.Contains(err.Error(), "9:16") {
		t.Fatalf("Run() error = %v, want aspect ratio mismatch", err)
	}
	if len(generator.calls) != 0 {
		t.Fatalf("generator calls = %d, want 0", len(generator.calls))
	}
	if renderer.calls != 0 {
		t.Fatalf("renderer calls = %d, want 0", renderer.calls)
	}
}

func TestOrchestrator_RunRejectsPlannerShotResolutionMismatchBeforeGeneration(t *testing.T) {
	t.Parallel()

	generator := &stubShotGenerator{data: []byte("mp4")}
	renderer := &stubRenderer{}
	orch := xaipipeline.NewOrchestrator(xaipipeline.Deps{
		Planner: &stubPlanner{out: xaipipeline.Manifest{
			ProjectID: "resolution-mismatch",
			Format:    "portrait",
			FPS:       24,
			Width:     720,
			Height:    1280,
			Shots: []xaipipeline.Shot{
				{Index: 1, Prompt: "wide shot", Resolution: "1080p"},
			},
		}},
		ShotGenerator: generator,
		Renderer:      renderer,
	})

	_, err := orch.Run(context.Background(), []byte("story"), xaipipeline.RunOptions{
		OutputDir: t.TempDir(),
		Format:    "portrait",
	})
	if err == nil {
		t.Fatal("Run() error = nil, want resolution error")
	}
	if !strings.Contains(err.Error(), "resolution") || !strings.Contains(err.Error(), "720p") {
		t.Fatalf("Run() error = %v, want resolution mismatch", err)
	}
	if len(generator.calls) != 0 {
		t.Fatalf("generator calls = %d, want 0", len(generator.calls))
	}
	if renderer.calls != 0 {
		t.Fatalf("renderer calls = %d, want 0", renderer.calls)
	}
}

func TestOrchestrator_RunRejectsDuplicateShotIndexesBeforeGeneration(t *testing.T) {
	t.Parallel()

	generator := &stubShotGenerator{data: []byte("mp4")}
	renderer := &stubRenderer{}
	orch := xaipipeline.NewOrchestrator(xaipipeline.Deps{
		Planner: &stubPlanner{out: xaipipeline.Manifest{
			ProjectID: "duplicate-index",
			Shots: []xaipipeline.Shot{
				{Index: 1, Prompt: "first"},
				{Index: 1, Prompt: "second"},
			},
		}},
		ShotGenerator: generator,
		Renderer:      renderer,
	})

	_, err := orch.Run(context.Background(), []byte("story"), xaipipeline.RunOptions{OutputDir: t.TempDir()})
	if err == nil {
		t.Fatal("Run() error = nil, want duplicate shot index error")
	}
	if !strings.Contains(err.Error(), "duplicate shot index 1") {
		t.Fatalf("Run() error = %v, want duplicate shot index", err)
	}
	if len(generator.calls) != 0 {
		t.Fatalf("generator calls = %d, want 0", len(generator.calls))
	}
	if renderer.calls != 0 {
		t.Fatalf("renderer calls = %d, want 0", renderer.calls)
	}
}

func TestOrchestrator_RunRejectsNonContiguousShotIndexesBeforeGeneration(t *testing.T) {
	t.Parallel()

	generator := &stubShotGenerator{data: []byte("mp4")}
	renderer := &stubRenderer{}
	orch := xaipipeline.NewOrchestrator(xaipipeline.Deps{
		Planner: &stubPlanner{out: xaipipeline.Manifest{
			ProjectID: "gap-index",
			Shots: []xaipipeline.Shot{
				{Index: 1, Prompt: "first"},
				{Index: 3, Prompt: "third"},
			},
		}},
		ShotGenerator: generator,
		Renderer:      renderer,
	})

	_, err := orch.Run(context.Background(), []byte("story"), xaipipeline.RunOptions{OutputDir: t.TempDir()})
	if err == nil {
		t.Fatal("Run() error = nil, want non-contiguous shot index error")
	}
	if !strings.Contains(err.Error(), "shot indexes must be contiguous from 1") {
		t.Fatalf("Run() error = %v, want contiguous shot index error", err)
	}
	if len(generator.calls) != 0 {
		t.Fatalf("generator calls = %d, want 0", len(generator.calls))
	}
	if renderer.calls != 0 {
		t.Fatalf("renderer calls = %d, want 0", renderer.calls)
	}
}

func TestOrchestrator_RunRejectsOutOfOrderShotIndexesBeforeGeneration(t *testing.T) {
	t.Parallel()

	generator := &stubShotGenerator{data: []byte("mp4")}
	renderer := &stubRenderer{}
	orch := xaipipeline.NewOrchestrator(xaipipeline.Deps{
		Planner: &stubPlanner{out: xaipipeline.Manifest{
			ProjectID: "out-of-order-index",
			Shots: []xaipipeline.Shot{
				{Index: 2, Prompt: "second"},
				{Index: 1, Prompt: "first"},
			},
		}},
		ShotGenerator: generator,
		Renderer:      renderer,
	})

	_, err := orch.Run(context.Background(), []byte("story"), xaipipeline.RunOptions{OutputDir: t.TempDir()})
	if err == nil {
		t.Fatal("Run() error = nil, want out-of-order shot index error")
	}
	if !strings.Contains(err.Error(), "shot indexes must match shot order from 1") {
		t.Fatalf("Run() error = %v, want ordered shot index error", err)
	}
	if len(generator.calls) != 0 {
		t.Fatalf("generator calls = %d, want 0", len(generator.calls))
	}
	if renderer.calls != 0 {
		t.Fatalf("renderer calls = %d, want 0", renderer.calls)
	}
}

func TestOrchestrator_RunRejectsTargetShotMismatchBeforeGeneration(t *testing.T) {
	t.Parallel()

	generator := &stubShotGenerator{data: []byte("mp4")}
	renderer := &stubRenderer{}
	orch := xaipipeline.NewOrchestrator(xaipipeline.Deps{
		Planner: &stubPlanner{out: xaipipeline.Manifest{
			ProjectID: "too-many",
			Shots: []xaipipeline.Shot{
				{Index: 1, Prompt: "first"},
				{Index: 2, Prompt: "second"},
			},
		}},
		ShotGenerator: generator,
		Renderer:      renderer,
	})

	_, err := orch.Run(context.Background(), []byte("story"), xaipipeline.RunOptions{
		OutputDir:   t.TempDir(),
		TargetShots: 1,
	})
	if err == nil {
		t.Fatal("Run() error = nil, want target shot mismatch")
	}
	if len(generator.calls) != 0 {
		t.Fatalf("generator calls = %d, want 0", len(generator.calls))
	}
	if renderer.calls != 0 {
		t.Fatalf("renderer calls = %d, want 0", renderer.calls)
	}
}
