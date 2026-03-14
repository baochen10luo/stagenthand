package audio

import (
	"context"
	"fmt"
	"html"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
)

// PollyCLIClient uses the AWS CLI to generate speech.
// It bypasses the need for the heavy AWS Go SDK for a simple MVP.
type PollyCLIClient struct {
	voiceID      string
	languageCode string
	region       string
	accessKey    string
	secretKey    string
}

// NewPollyCLIClient creates a new TTS client backed by the AWS CLI.
func NewPollyCLIClient(region, accessKey, secretKey string) *PollyCLIClient {
	if region == "" {
		region = "us-east-1"
	}
	return &PollyCLIClient{
		voiceID:      "Zhiyu",
		languageCode: "cmn-CN",
		region:       region,
		accessKey:    accessKey,
		secretKey:    secretKey,
	}
}

func (c *PollyCLIClient) GenerateSpeech(ctx context.Context, text string) ([]byte, error) {
	if text == "" {
		return nil, nil // No text, no audio
	}

	// Use a temp file because AWS CLI wants to write to a file
	tmpFile := filepath.Join(os.TempDir(), fmt.Sprintf("polly_%d.mp3", os.Getpid()))
	defer os.Remove(tmpFile)

	ssmlText := formatSSML(text)

	// Command: aws polly synthesize-speech --text-type ssml --text "<speak>...</speak>" --output-format mp3 --voice-id Zhiyu out.mp3
	cmd := exec.CommandContext(ctx, "aws", "polly", "synthesize-speech",
		"--engine", "neural",
		"--text-type", "ssml",
		"--text", ssmlText,
		"--output-format", "mp3",
		"--voice-id", c.voiceID,
		"--language-code", c.languageCode,
		"--region", c.region,
		tmpFile,
	)

	// Inherit environment and inject AWS credentials
	cmd.Env = os.Environ()
	if c.accessKey != "" && c.secretKey != "" {
		cmd.Env = append(cmd.Env,
			fmt.Sprintf("AWS_ACCESS_KEY_ID=%s", c.accessKey),
			fmt.Sprintf("AWS_SECRET_ACCESS_KEY=%s", c.secretKey),
		)
	}

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

// formatSSML parses the raw dialogue text and wraps it in SSML.
// It detects common script cues like "Whisper:" and maps them to Amazon Polly effects.
func formatSSML(dialogue string) string {
	// 1. Detect and strip whisper tag (case-insensitive)
	isWhisper := false
	whisperRegex := regexp.MustCompile(`(?i)^(whisper:\s*|\[whispers?\]\s*|\(whispers?\)\s*)`)
	if whisperRegex.MatchString(dialogue) {
		isWhisper = true
		dialogue = whisperRegex.ReplaceAllString(dialogue, "")
	}

	// 2. Strip standard narrator/character name prefixes (e.g. "Narrator: ", "SYSTEM: ")
	prefixRegex := regexp.MustCompile(`^[\p{L}0-9\s]+:\s*`)
	dialogue = prefixRegex.ReplaceAllString(dialogue, "")

	// 3. Strip stage directions in brackets/parentheses e.g. [sighs]
	tagsRegex := regexp.MustCompile(`\[.*?\]|\(.*?\)`)
	dialogue = tagsRegex.ReplaceAllString(dialogue, "")

	// 4. Scrub quote marks to avoid TTS awkward pauses and XML collision
	dialogue = strings.ReplaceAll(dialogue, "\"", "")
	dialogue = strings.ReplaceAll(dialogue, "'", "")

	// 5. XML Escape to protect SSML parser
	safeText := html.EscapeString(strings.TrimSpace(dialogue))

	if safeText == "" {
		return "<speak></speak>"
	}

	// 6. Wrap in SSML
	if isWhisper {
		safeText = fmt.Sprintf("<amazon:effect name=\"whispered\">%s</amazon:effect>", safeText)
	}
	
	// Default to 90% speech rate to add dramatic pauses and avoid rushed GPS-like reading.
	return fmt.Sprintf("<speak><prosody rate=\"90%%\">%s</prosody></speak>", safeText)
}
