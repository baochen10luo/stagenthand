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

// languageConfig maps a BCP-47 language tag to the AWS Polly voice and language code.
type languageConfig struct {
	voiceID      string
	languageCode string
}

// languageMap defines supported TTS languages. Keys are BCP-47 tags.
var languageMap = map[string]languageConfig{
	"zh-TW":  {voiceID: "Zhiyu", languageCode: "cmn-CN"},
	"cmn-CN": {voiceID: "Zhiyu", languageCode: "cmn-CN"},
	"en-US":  {voiceID: "Joanna", languageCode: "en-US"},
	"en-GB":  {voiceID: "Amy", languageCode: "en-GB"},
	"ja-JP":  {voiceID: "Takumi", languageCode: "ja-JP"},
	"ko-KR":  {voiceID: "Seoyeon", languageCode: "ko-KR"},
}

// defaultLanguageConfig is the fallback when a language is not found in languageMap.
var defaultLanguageConfig = languageConfig{voiceID: "Zhiyu", languageCode: "cmn-CN"}

// PollyCLIClient uses the AWS CLI to generate speech.
// It bypasses the need for the heavy AWS Go SDK for a simple MVP.
type PollyCLIClient struct {
	voiceID      string
	languageCode string
	region       string
	accessKey    string
	secretKey    string
	// commandFactory allows mocking exec.Command for testing
	commandFactory func(ctx context.Context, name string, args ...string) *exec.Cmd
}

// NewPollyCLIClient creates a new TTS client backed by the AWS CLI.
// Defaults to zh-TW (Zhiyu voice, cmn-CN language code).
func NewPollyCLIClient(region, accessKey, secretKey string) *PollyCLIClient {
	return NewPollyCLIClientWithLanguage(region, accessKey, secretKey, "zh-TW")
}

// NewPollyCLIClientWithLanguage creates a new TTS client with the specified language.
// If the language is not supported or empty, it falls back to zh-TW.
func NewPollyCLIClientWithLanguage(region, accessKey, secretKey, language string) *PollyCLIClient {
	if region == "" {
		region = "us-east-1"
	}
	cfg, ok := languageMap[language]
	if !ok {
		cfg = defaultLanguageConfig
	}
	return &PollyCLIClient{
		voiceID:      cfg.voiceID,
		languageCode: cfg.languageCode,
		region:       region,
		accessKey:    accessKey,
		secretKey:    secretKey,
		commandFactory: func(ctx context.Context, name string, args ...string) *exec.Cmd {
			return exec.CommandContext(ctx, name, args...)
		},
	}
}

func (c *PollyCLIClient) GenerateSpeech(ctx context.Context, text string) ([]byte, error) {
	if text == "" {
		return nil, nil // No text, no audio
	}
	ssmlText := formatSSML(text)
	return c.SynthesizeSSML(ctx, ssmlText)
}

// SynthesizeSSML calls AWS Polly with a pre-built SSML string and returns raw MP3 bytes.
// This is used by MultiSpeakerClient to avoid double-formatting.
func (c *PollyCLIClient) SynthesizeSSML(ctx context.Context, ssmlText string) ([]byte, error) {
	if ssmlText == "" || ssmlText == "<speak></speak>" {
		return nil, nil
	}

	// Use a temp file because AWS CLI wants to write to a file
	tmpFile := filepath.Join(os.TempDir(), fmt.Sprintf("polly_%d.mp3", os.Getpid()))
	defer os.Remove(tmpFile)

	// Command: aws polly synthesize-speech --text-type ssml --text "<speak>...</speak>" --output-format mp3 --voice-id Zhiyu out.mp3
	cmd := c.commandFactory(ctx, "aws", "polly", "synthesize-speech",
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

// cleanText strips character name prefixes, stage directions, and quote marks
// from raw dialogue text. Used by both formatSSML and formatSSMLWithEmotion.
func cleanText(dialogue string) string {
	// Strip standard narrator/character name prefixes (e.g. "Narrator: ", "SYSTEM: ")
	prefixRegex := regexp.MustCompile(`^[\p{L}0-9\s]+:\s*`)
	dialogue = prefixRegex.ReplaceAllString(dialogue, "")

	// Strip stage directions in brackets/parentheses e.g. [sighs]
	tagsRegex := regexp.MustCompile(`\[.*?\]|\(.*?\)`)
	dialogue = tagsRegex.ReplaceAllString(dialogue, "")

	// Scrub quote marks to avoid TTS awkward pauses and XML collision
	dialogue = strings.ReplaceAll(dialogue, "\"", "")
	dialogue = strings.ReplaceAll(dialogue, "'", "")

	return strings.TrimSpace(dialogue)
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

	// 5. Rewrite numeric hyphen ranges so Polly reads them as "之" instead of a minus pause.
	hyphenNumRegex := regexp.MustCompile(`(\d+)-(\d+)`)
	dialogue = hyphenNumRegex.ReplaceAllString(dialogue, "${1}之${2}")

	dialogue = strings.TrimSpace(dialogue)

	// 6. XML Escape while preserving full-width bracket segments and adding a pause after each one.
	var builder strings.Builder
	for len(dialogue) > 0 {
		start := strings.IndexRune(dialogue, '【')
		if start == -1 {
			builder.WriteString(html.EscapeString(dialogue))
			break
		}

		if start > 0 {
			builder.WriteString(html.EscapeString(dialogue[:start]))
		}

		endOffset := strings.IndexRune(dialogue[start+len("【"):], '】')
		if endOffset == -1 {
			builder.WriteString(html.EscapeString(dialogue[start:]))
			break
		}

		end := start + len("【") + endOffset
		inner := dialogue[start+len("【") : end]
		builder.WriteString("【")
		builder.WriteString(html.EscapeString(inner))
		builder.WriteString("】<break time=\"500ms\"/>")

		dialogue = dialogue[end+len("】"):]
	}

	safeText := builder.String()

	if safeText == "" {
		return "<speak></speak>"
	}

	// 7. Wrap in SSML
	if isWhisper {
		safeText = fmt.Sprintf("<amazon:effect name=\"whispered\">%s</amazon:effect>", safeText)
	}
	
	// Default to 90% speech rate to add dramatic pauses and avoid rushed GPS-like reading.
	return fmt.Sprintf("<speak><prosody rate=\"90%%\">%s</prosody></speak>", safeText)
}
