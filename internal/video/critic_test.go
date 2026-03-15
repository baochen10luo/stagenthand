package video

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// mockCriticClient implements llm.VideoCriticClient
type mockCriticClient struct {
	output        []byte
	err           error
	receivedBytes []byte // captures what was sent to ReviewVideo
}

func (m *mockCriticClient) ReviewVideo(ctx context.Context, systemPrompt string, propsJSONData []byte, mediaType string, mediaData []byte) ([]byte, error) {
	m.receivedBytes = mediaData
	return m.output, m.err
}

func TestCheckApproval(t *testing.T) {
	tests := []struct {
		name     string
		eval     Evaluation
		approved bool
	}{
		{"All perfect", Evaluation{VisualScore: 10, AudioSyncScore: 10, AdherenceScore: 10, ToneScore: 10}, true},
		{"Low Visual FATAL", Evaluation{VisualScore: 7, AudioSyncScore: 10, AdherenceScore: 10, ToneScore: 10}, false},
		{"Low Audio FATAL", Evaluation{VisualScore: 10, AudioSyncScore: 7, AdherenceScore: 10, ToneScore: 10}, false},
		{"Low Total Score (<32)", Evaluation{VisualScore: 8, AudioSyncScore: 8, AdherenceScore: 7, ToneScore: 8}, false},
		{"Barely passes (32)", Evaluation{VisualScore: 8, AudioSyncScore: 8, AdherenceScore: 8, ToneScore: 8}, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.eval.CheckApproval(); got != tt.approved {
				t.Errorf("CheckApproval() = %v, want %v", got, tt.approved)
			}
		})
	}
}

func TestCritic_Evaluate(t *testing.T) {
	client := &mockCriticClient{
		output: []byte(`{"visual_score": 9, "audio_sync_score": 9, "adherence_score": 9, "tone_score": 9, "action": "APPROVE", "feedback": "Good"}`),
	}
	critic := NewCritic(client)

	eval, err := critic.Evaluate(context.Background(), "../../test_storyboard.json", []byte("{}"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if eval.Action != "APPROVE" {
		t.Errorf("expected APPROVE, got %s", eval.Action)
	}
}

// TestCompressForCritic_SmallFile verifies that files under 20MB are returned as-is without compression.
func TestCompressForCritic_SmallFile(t *testing.T) {
	dir := t.TempDir()
	smallFile := filepath.Join(dir, "small.mp4")

	// Write a file smaller than 20MB (1KB of dummy data)
	data := make([]byte, 1024)
	if err := os.WriteFile(smallFile, data, 0644); err != nil {
		t.Fatalf("failed to create small test file: %v", err)
	}

	usePath, cleanup, err := compressForCritic(smallFile)
	if err != nil {
		t.Fatalf("unexpected error for small file: %v", err)
	}
	defer cleanup()

	if usePath != smallFile {
		t.Errorf("expected usePath == original path %q, got %q", smallFile, usePath)
	}

	// Verify cleanup is a no-op (original file should still exist after calling it)
	cleanup()
	if _, statErr := os.Stat(smallFile); statErr != nil {
		t.Errorf("small file was deleted by cleanup, but it should not have been: %v", statErr)
	}
}

// TestCompressForCritic_FileNotFound verifies that a non-existent path returns an error.
func TestCompressForCritic_FileNotFound(t *testing.T) {
	_, _, err := compressForCritic("/nonexistent/path/to/video.mp4")
	if err == nil {
		t.Error("expected error for non-existent file, got nil")
	}
}

// TestEvaluate_AutoCompressOnLargeInput tests the integration of compressForCritic within Evaluate.
// It uses table-driven cases: small file passes through, non-existent file returns error.
func TestEvaluate_AutoCompressOnLargeInput(t *testing.T) {
	approveJSON, _ := json.Marshal(Evaluation{
		VisualScore: 9, AudioSyncScore: 9, AdherenceScore: 9, ToneScore: 9, Action: "APPROVE", Feedback: "ok",
	})

	dir := t.TempDir()
	smallFile := filepath.Join(dir, "video.mp4")
	if err := os.WriteFile(smallFile, []byte("fake video bytes"), 0644); err != nil {
		t.Fatalf("setup: %v", err)
	}

	tests := []struct {
		name      string
		videoPath string
		wantErr   bool
	}{
		{
			name:      "small file passes through without compression",
			videoPath: smallFile,
			wantErr:   false,
		},
		{
			name:      "non-existent file returns error",
			videoPath: "/tmp/does_not_exist_12345.mp4",
			wantErr:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := &mockCriticClient{output: approveJSON}
			critic := NewCritic(client)

			eval, err := critic.Evaluate(context.Background(), tt.videoPath, []byte("{}"))
			if tt.wantErr {
				if err == nil {
					t.Error("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if eval.Action != "APPROVE" {
				t.Errorf("expected APPROVE, got %s", eval.Action)
			}
			// For small file: verify the LLM received the original bytes
			if string(client.receivedBytes) != "fake video bytes" {
				t.Errorf("expected LLM to receive original bytes, got %q", string(client.receivedBytes))
			}
		})
	}
}
