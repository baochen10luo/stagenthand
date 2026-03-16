package pipeline_test

import (
	"context"
	"strings"
	"testing"

	"github.com/baochen10luo/stagenthand/internal/pipeline"
)

func TestOrchestrator_PropsCritic(t *testing.T) {
	storyboardJSON := []byte(`{"project_id":"pc","episode":1,"scenes":[{"number":1,"description":"scene"}]}`)
	panelsJSON := []byte(`{"panels":[{"scene_number":1,"panel_number":1,"description":"hero","dialogue":"Hello","character_refs":[],"duration_sec":3.0}]}`)

	tests := []struct {
		name             string
		critic           pipeline.PropsCriticEvaluator
		wantLLMCallCount int // how many times LLM is called for panels generation
	}{
		{
			name:             "nil PropsCritic skips evaluation",
			critic:           nil,
			wantLLMCallCount: 1,
		},
		{
			name: "OK=true passes without retry",
			critic: &pipeline.MockPropsCriticEvaluator{
				Results: []*pipeline.PropsCriticResult{
					{OK: true, Issues: nil},
				},
			},
			wantLLMCallCount: 1,
		},
		{
			name: "OK=false triggers one retry",
			critic: &pipeline.MockPropsCriticEvaluator{
				Results: []*pipeline.PropsCriticResult{
					{OK: false, Issues: []string{"missing dialogue in panel 2"}},
				},
			},
			wantLLMCallCount: 2,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			llmCallCount := 0
			llm := &mockTransformer{
				GenerateFunc: func(_ context.Context, systemPrompt string, _ []byte) ([]byte, error) {
					if strings.HasPrefix(systemPrompt, pipeline.PromptStoryboardToPanels) {
						llmCallCount++
						return panelsJSON, nil
					}
					return nil, nil
				},
			}

			orch := pipeline.NewOrchestrator(pipeline.OrchestratorDeps{
				LLM:         llm,
				Images:      &mockImageBatcher{},
				Checkpoints: &mockCheckpointStore{approved: true},
				PropsCritic: tt.critic,
				DryRun:      true,
				SkipHITL:    true,
			})

			_, err := orch.Run(context.Background(), storyboardJSON)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if llmCallCount != tt.wantLLMCallCount {
				t.Errorf("LLM panels calls = %d, want %d", llmCallCount, tt.wantLLMCallCount)
			}
		})
	}
}
