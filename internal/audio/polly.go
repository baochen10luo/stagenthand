package audio

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
)

// PollyCLIClient uses the AWS CLI to generate speech.
// It bypasses the need for the heavy AWS Go SDK for a simple MVP.
type PollyCLIClient struct {
	voiceID      string
	languageCode string
}

// NewPollyCLIClient creates a new TTS client backed by the AWS CLI.
func NewPollyCLIClient() *PollyCLIClient {
	return &PollyCLIClient{
		voiceID:      "Zhiyu",
		languageCode: "cmn-CN",
	}
}

func (c *PollyCLIClient) GenerateSpeech(ctx context.Context, text string) ([]byte, error) {
	if text == "" {
		return nil, nil // No text, no audio
	}

	// Use a temp file because AWS CLI wants to write to a file
	tmpFile := filepath.Join(os.TempDir(), fmt.Sprintf("polly_%d.mp3", os.Getpid()))
	defer os.Remove(tmpFile)

	// Command: aws polly synthesize-speech --text "Hello" --output-format mp3 --voice-id Zhiyu out.mp3
	cmd := exec.CommandContext(ctx, "aws", "polly", "synthesize-speech",
		"--text", text,
		"--output-format", "mp3",
		"--voice-id", c.voiceID,
		"--language-code", c.languageCode,
		tmpFile,
	)

	// Inherit environment for AWS credentials
	cmd.Env = os.Environ()

	out, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("aws polly error: %s - %w", string(out), err)
	}

	audioBytes, err := os.ReadFile(tmpFile)
	if err != nil {
		return nil, fmt.Errorf("failed to read synthesized polly file: %w", err)
	}

	return audioBytes, nil
}
