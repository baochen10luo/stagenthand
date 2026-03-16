package video

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// mockCriticClient implements llm.VideoCriticClient
type mockCriticClient struct {
	output         []byte
	err            error
	receivedBytes  []byte  // captures what was sent to ReviewVideo
	receivedPrompt string  // captures the systemPrompt passed to ReviewVideo
}

func (m *mockCriticClient) ReviewVideo(ctx context.Context, systemPrompt string, propsJSONData []byte, mediaType string, mediaData []byte) ([]byte, error) {
	m.receivedBytes = mediaData
	m.receivedPrompt = systemPrompt
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

// TestMockClient verifies that MockClient captures arguments and returns configured values.
func TestMockClient(t *testing.T) {
	m := &MockClient{MockVideoBytes: []byte("video"), MockErr: nil}
	b, err := m.GenerateVideo(context.Background(), "http://img", "a prompt")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if string(b) != "video" {
		t.Errorf("expected %q, got %q", "video", string(b))
	}
	if m.CapturedURL != "http://img" {
		t.Errorf("CapturedURL = %q, want %q", m.CapturedURL, "http://img")
	}
	if m.CapturedPrompt != "a prompt" {
		t.Errorf("CapturedPrompt = %q, want %q", m.CapturedPrompt, "a prompt")
	}
}

// TestCompressForCritic_LargeFileNoFFmpeg verifies that a file over 20MB with no ffmpeg returns an error.
func TestCompressForCritic_LargeFileNoFFmpeg(t *testing.T) {
	dir := t.TempDir()
	largeFile := filepath.Join(dir, "large.mp4")

	// 21MB — exceeds the 20MB threshold
	data := make([]byte, 21*1024*1024)
	if err := os.WriteFile(largeFile, data, 0644); err != nil {
		t.Fatalf("failed to create large test file: %v", err)
	}

	// Hide ffmpeg from PATH so LookPath fails
	t.Setenv("PATH", "")

	_, _, err := compressForCritic(largeFile)
	if err == nil {
		t.Error("expected error when ffmpeg is not in PATH, got nil")
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

// TestNewMotionCritic verifies that NewMotionCritic uses the motionCriticPrompt.
func TestNewMotionCritic(t *testing.T) {
	approveJSON, _ := json.Marshal(Evaluation{
		VisualScore: 9, AudioSyncScore: 9, AdherenceScore: 9, ToneScore: 9, Action: "APPROVE", Feedback: "ok",
	})

	client := &mockCriticClient{output: approveJSON}
	critic := NewMotionCritic(client)

	// Create a small temp video file
	dir := t.TempDir()
	videoFile := filepath.Join(dir, "motion.mp4")
	if err := os.WriteFile(videoFile, []byte("fake motion video"), 0644); err != nil {
		t.Fatalf("setup: %v", err)
	}

	_, err := critic.Evaluate(context.Background(), videoFile, []byte("{}"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify the system prompt sent to ReviewVideo contains "Motion Director"
	if !strings.Contains(client.receivedPrompt, "Motion Director") {
		t.Errorf("expected systemPrompt to contain 'Motion Director', got %q", client.receivedPrompt[:80])
	}
}

// TestEvaluate_InvalidJSON verifies that Evaluate returns an error when the LLM returns invalid JSON.
func TestEvaluate_InvalidJSON(t *testing.T) {
	client := &mockCriticClient{output: []byte("this is not valid JSON at all")}
	critic := NewCritic(client)

	dir := t.TempDir()
	videoFile := filepath.Join(dir, "video.mp4")
	if err := os.WriteFile(videoFile, []byte("fake video"), 0644); err != nil {
		t.Fatalf("setup: %v", err)
	}

	_, err := critic.Evaluate(context.Background(), videoFile, []byte("{}"))
	if err == nil {
		t.Fatal("expected error for invalid JSON response, got nil")
	}
	if !strings.Contains(err.Error(), "failed to parse critic evaluation json") {
		t.Errorf("expected JSON parse error, got: %v", err)
	}
}

// TestEvaluate_ReadFileError verifies that Evaluate returns an error when the compressed path is unreadable.
// This covers the os.ReadFile error branch (line 108) in Evaluate.
func TestEvaluate_ReadFileError(t *testing.T) {
	// Create a directory where a file is expected — ReadFile on a dir returns an error
	dir := t.TempDir()
	videoDir := filepath.Join(dir, "video.mp4")
	if err := os.Mkdir(videoDir, 0755); err != nil {
		t.Fatalf("setup: %v", err)
	}

	client := &mockCriticClient{output: []byte(`{}`)}
	critic := NewCritic(client)

	_, err := critic.Evaluate(context.Background(), videoDir, []byte("{}"))
	if err == nil {
		t.Fatal("expected error when video path is a directory, got nil")
	}
	if !strings.Contains(err.Error(), "could not read video file for critique") {
		t.Errorf("expected read error, got: %v", err)
	}
}

// TestCompressForCritic_LargeFileCompressionFails verifies the ffmpeg compression error path.
// When a file exceeds 20MB but is not a valid video, ffmpeg fails and returns an error.
func TestCompressForCritic_LargeFileCompressionFails(t *testing.T) {
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		t.Skip("ffmpeg not available")
	}

	dir := t.TempDir()
	largeFile := filepath.Join(dir, "large.mp4")

	// Create a file > 20MB with garbage data (not a valid video)
	data := make([]byte, 21*1024*1024)
	if err := os.WriteFile(largeFile, data, 0644); err != nil {
		t.Fatalf("write: %v", err)
	}

	_, _, err := compressForCritic(largeFile)
	if err == nil {
		t.Fatal("expected error when ffmpeg fails to compress invalid video, got nil")
	}
	if !strings.Contains(err.Error(), "ffmpeg compression failed") {
		t.Errorf("expected ffmpeg compression error, got: %v", err)
	}
}

// TestEvaluate_ReviewVideoError verifies that Evaluate returns an error when ReviewVideo fails.
func TestEvaluate_ReviewVideoError(t *testing.T) {
	client := &mockCriticClient{err: fmt.Errorf("LLM API unavailable")}
	critic := NewCritic(client)

	dir := t.TempDir()
	videoFile := filepath.Join(dir, "video.mp4")
	if err := os.WriteFile(videoFile, []byte("fake video"), 0644); err != nil {
		t.Fatalf("setup: %v", err)
	}

	_, err := critic.Evaluate(context.Background(), videoFile, []byte("{}"))
	if err == nil {
		t.Fatal("expected error when ReviewVideo fails, got nil")
	}
	if !strings.Contains(err.Error(), "video analysis by LLM failed") {
		t.Errorf("expected LLM failure error, got: %v", err)
	}
}
