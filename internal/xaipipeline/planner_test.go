package xaipipeline_test

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/baochen10luo/stagenthand/internal/xaipipeline"
)

type stubTransformer struct {
	systemPrompt  string
	inputData     []byte
	output        []byte
	err           error
	calls         int
	afterGenerate func()
}

func (s *stubTransformer) GenerateTransformation(_ context.Context, systemPrompt string, inputData []byte) ([]byte, error) {
	s.calls++
	s.systemPrompt = systemPrompt
	s.inputData = inputData
	if s.afterGenerate != nil {
		s.afterGenerate()
	}
	return s.output, s.err
}

func TestLLMPlanner_PlanParsesManifest(t *testing.T) {
	t.Parallel()

	transformer := &stubTransformer{output: []byte(`{
		"project_id": "robot-flower",
		"shots": [
			{"index": 1, "prompt": "wide shot", "subtitle": "first"},
			{"index": 2, "prompt": "close shot", "duration_sec": 6}
		]
	}`)}
	planner := xaipipeline.NewLLMPlanner(transformer)

	got, err := planner.Plan(context.Background(), xaipipeline.PlanInput{
		Story:       "機器人找到花",
		TargetShots: 2,
		Format:      "portrait",
	})
	if err != nil {
		t.Fatalf("Plan() error = %v", err)
	}

	if got.ProjectID != "robot-flower" {
		t.Fatalf("ProjectID = %q, want robot-flower", got.ProjectID)
	}
	if len(got.Shots) != 2 {
		t.Fatalf("shots = %d, want 2", len(got.Shots))
	}
	if got.Shots[0].Prompt != "wide shot" {
		t.Fatalf("first prompt = %q", got.Shots[0].Prompt)
	}
	if !strings.Contains(transformer.systemPrompt, "xAI-native video pipeline") {
		t.Fatalf("system prompt did not describe xAI-native pipeline: %q", transformer.systemPrompt)
	}
	if !strings.Contains(transformer.systemPrompt, "exactly target_shots") {
		t.Fatalf("system prompt should require exactly target_shots when provided: %q", transformer.systemPrompt)
	}
	if !strings.Contains(transformer.systemPrompt, `transition_out "cut" by default`) {
		t.Fatalf("system prompt should constrain default transition_out: %q", transformer.systemPrompt)
	}
	if !strings.Contains(transformer.systemPrompt, `"fade" is also supported`) {
		t.Fatalf("system prompt should constrain supported transition_out values: %q", transformer.systemPrompt)
	}
	if !strings.Contains(string(transformer.inputData), `"target_shots":2`) {
		t.Fatalf("input data missing target_shots: %s", string(transformer.inputData))
	}
	if !strings.Contains(string(transformer.inputData), "機器人找到花") {
		t.Fatalf("input data missing story: %s", string(transformer.inputData))
	}
}

func TestLLMPlanner_PlanTrimsRequestInput(t *testing.T) {
	t.Parallel()

	transformer := &stubTransformer{output: []byte(`{
		"project_id": "robot-flower",
		"shots": [{"index": 1, "prompt": "wide shot"}]
	}`)}
	planner := xaipipeline.NewLLMPlanner(transformer)

	_, err := planner.Plan(context.Background(), xaipipeline.PlanInput{
		Story:  "  機器人找到花  ",
		Format: " PORTRAIT ",
	})
	if err != nil {
		t.Fatalf("Plan() error = %v", err)
	}

	var req struct {
		Story  string `json:"story"`
		Format string `json:"format,omitempty"`
	}
	if err := json.Unmarshal(transformer.inputData, &req); err != nil {
		t.Fatalf("unmarshal planner request: %v", err)
	}
	if req.Story != "機器人找到花" {
		t.Fatalf("story = %q, want trimmed story", req.Story)
	}
	if req.Format != "portrait" {
		t.Fatalf("format = %q, want portrait", req.Format)
	}
}

func TestLLMPlanner_PlanDefaultsBlankFormatBeforeTransformer(t *testing.T) {
	t.Parallel()

	transformer := &stubTransformer{output: []byte(`{
		"project_id": "robot-flower",
		"shots": [{"index": 1, "prompt": "wide shot"}]
	}`)}
	planner := xaipipeline.NewLLMPlanner(transformer)

	_, err := planner.Plan(context.Background(), xaipipeline.PlanInput{
		Story:  "機器人找到花",
		Format: " \t ",
	})
	if err != nil {
		t.Fatalf("Plan() error = %v", err)
	}

	var req struct {
		Format string `json:"format"`
	}
	if err := json.Unmarshal(transformer.inputData, &req); err != nil {
		t.Fatalf("unmarshal planner request: %v", err)
	}
	if req.Format != "portrait" {
		t.Fatalf("format = %q, want default portrait", req.Format)
	}
}

func TestLLMPlanner_PlanRejectsCanceledContextBeforeTransformer(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	transformer := &stubTransformer{output: []byte(`{
		"project_id": "robot-flower",
		"shots": [{"index": 1, "prompt": "wide shot"}]
	}`)}
	planner := xaipipeline.NewLLMPlanner(transformer)

	got, err := planner.Plan(ctx, xaipipeline.PlanInput{Story: "story"})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Plan() error = %v, want context.Canceled", err)
	}
	if got.ProjectID != "" || len(got.Shots) != 0 {
		t.Fatalf("Plan() = %+v, want zero manifest", got)
	}
	if transformer.calls != 0 {
		t.Fatalf("transformer calls = %d, want 0", transformer.calls)
	}
}

func TestLLMPlanner_PlanRejectsCanceledContextAfterTransformerBeforeManifest(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	transformer := &stubTransformer{
		output: []byte(`{
			"project_id": "robot-flower",
			"shots": [{"index": 1, "prompt": "wide shot"}]
		}`),
		afterGenerate: cancel,
	}
	planner := xaipipeline.NewLLMPlanner(transformer)

	got, err := planner.Plan(ctx, xaipipeline.PlanInput{Story: "story"})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Plan() error = %v, want context.Canceled", err)
	}
	if got.ProjectID != "" || len(got.Shots) != 0 {
		t.Fatalf("Plan() = %+v, want zero manifest", got)
	}
	if transformer.calls != 1 {
		t.Fatalf("transformer calls = %d, want 1", transformer.calls)
	}
}

func TestLLMPlanner_PlanRejectsEmptyStoryBeforeTransformer(t *testing.T) {
	t.Parallel()

	transformer := &stubTransformer{output: []byte(`{"project_id":"unused"}`)}
	planner := xaipipeline.NewLLMPlanner(transformer)
	_, err := planner.Plan(context.Background(), xaipipeline.PlanInput{Story: " \t\n"})

	if err == nil {
		t.Fatal("Plan() error = nil, want empty story error")
	}
	if !strings.Contains(err.Error(), "story is empty") {
		t.Fatalf("Plan() error = %v, want story is empty", err)
	}
	if transformer.calls != 0 {
		t.Fatalf("transformer calls = %d, want 0", transformer.calls)
	}
}

func TestLLMPlanner_PlanRejectsNilTransformer(t *testing.T) {
	t.Parallel()

	planner := xaipipeline.NewLLMPlanner(nil)
	_, err := planner.Plan(context.Background(), xaipipeline.PlanInput{
		Story: "story",
	})

	if err == nil {
		t.Fatal("Plan() error = nil, want nil transformer error")
	}
	if !strings.Contains(err.Error(), "transformer is nil") {
		t.Fatalf("Plan() error = %v, want transformer is nil", err)
	}
}

func TestLLMPlanner_PlanRejectsEmptyTransformerOutput(t *testing.T) {
	t.Parallel()

	planner := xaipipeline.NewLLMPlanner(&stubTransformer{output: []byte(" \t\n")})
	_, err := planner.Plan(context.Background(), xaipipeline.PlanInput{
		Story: "story",
	})

	if err == nil {
		t.Fatal("Plan() error = nil, want empty output error")
	}
	if !strings.Contains(err.Error(), "llm output is empty") {
		t.Fatalf("Plan() error = %v, want llm output is empty", err)
	}
}

func TestLLMPlanner_PlanRejectsNullTransformerOutput(t *testing.T) {
	t.Parallel()

	planner := xaipipeline.NewLLMPlanner(&stubTransformer{output: []byte("null")})
	_, err := planner.Plan(context.Background(), xaipipeline.PlanInput{
		Story: "story",
	})

	if err == nil {
		t.Fatal("Plan() error = nil, want null output error")
	}
	if !strings.Contains(err.Error(), "llm output is not a manifest object") {
		t.Fatalf("Plan() error = %v, want manifest object error", err)
	}
}

func TestLLMPlanner_PlanRejectsNegativeTargetShotsBeforeTransformer(t *testing.T) {
	t.Parallel()

	transformer := &stubTransformer{output: []byte(`{"project_id":"unused"}`)}
	planner := xaipipeline.NewLLMPlanner(transformer)
	_, err := planner.Plan(context.Background(), xaipipeline.PlanInput{
		Story:       "story",
		TargetShots: -1,
	})

	if err == nil {
		t.Fatal("Plan() error = nil, want target shots error")
	}
	if !strings.Contains(err.Error(), "target shots must be zero or greater") {
		t.Fatalf("Plan() error = %v, want target shots validation error", err)
	}
	if transformer.calls != 0 {
		t.Fatalf("transformer calls = %d, want 0", transformer.calls)
	}
}

func TestLLMPlanner_PlanRejectsUnsupportedFormatBeforeTransformer(t *testing.T) {
	t.Parallel()

	transformer := &stubTransformer{output: []byte(`{"project_id":"unused"}`)}
	planner := xaipipeline.NewLLMPlanner(transformer)
	_, err := planner.Plan(context.Background(), xaipipeline.PlanInput{
		Story:  "story",
		Format: "landscape",
	})

	if err == nil {
		t.Fatal("Plan() error = nil, want unsupported format error")
	}
	if !strings.Contains(err.Error(), "supports portrait only") {
		t.Fatalf("Plan() error = %v, want portrait-only validation error", err)
	}
	if transformer.calls != 0 {
		t.Fatalf("transformer calls = %d, want 0", transformer.calls)
	}
}

func TestLLMPlanner_PlanRejectsInvalidJSON(t *testing.T) {
	t.Parallel()

	planner := xaipipeline.NewLLMPlanner(&stubTransformer{output: []byte(`not json`)})
	if _, err := planner.Plan(context.Background(), xaipipeline.PlanInput{Story: "story"}); err == nil {
		t.Fatal("Plan() error = nil, want error")
	}
}
