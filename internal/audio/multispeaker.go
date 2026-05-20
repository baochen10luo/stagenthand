package audio

import (
	"context"
	"fmt"
	"html"
	"os/exec"
	"strings"

	"github.com/baochen10luo/stagenthand/internal/character"
	"github.com/baochen10luo/stagenthand/internal/domain"
	"github.com/baochen10luo/stagenthand/internal/pronunciation"
)

// MultiSpeakerClient routes each DialogueLine to the correct TTS voice.
type MultiSpeakerClient interface {
	GenerateSpeechForLine(ctx context.Context, line domain.DialogueLine) ([]byte, error)
}

// PollyMultiSpeakerClient routes each DialogueLine to a per-character Polly voice.
// Unknown speakers fall back to the default language voice.
type PollyMultiSpeakerClient struct {
	region          string
	accessKey       string
	secretKey       string
	defaultLanguage string
	registry        character.Registry
	defaultClient   *PollyCLIClient
	// speakerClients caches per-speaker clients (lazy-init)
	speakerClients map[string]*PollyCLIClient
	// commandFactoryOverride, when set, is injected into newly-created speaker clients (for testing)
	commandFactoryOverride func(ctx context.Context, name string, args ...string) *exec.Cmd
	// PronunciationProcessor applies dictionary-based pronunciation overrides
	// to the SSML text before synthesis (e.g., 破音字 correction).
	PronunciationProcessor pronunciation.Processor
}

// NewPollyMultiSpeakerClient creates a multi-speaker TTS client backed by AWS Polly CLI.
// defaultLanguage is used as the fallback voice for unknown speakers.
func NewPollyMultiSpeakerClient(region, accessKey, secretKey, defaultLanguage string, registry character.Registry) *PollyMultiSpeakerClient {
	defaultClient := NewPollyCLIClientWithLanguage(region, accessKey, secretKey, defaultLanguage)
	return &PollyMultiSpeakerClient{
		region:          region,
		accessKey:       accessKey,
		secretKey:       secretKey,
		defaultLanguage: defaultLanguage,
		registry:        registry,
		defaultClient:   defaultClient,
		speakerClients:  make(map[string]*PollyCLIClient),
		PronunciationProcessor: defaultClient.PronunciationProcessor,
	}
}

// GenerateSpeechForLine generates TTS audio for a single dialogue line.
// It uses the character registry to look up per-speaker voices and maps
// the Emotion field to SSML prosody tags.
func (c *PollyMultiSpeakerClient) GenerateSpeechForLine(ctx context.Context, line domain.DialogueLine) ([]byte, error) {
	if line.Text == "" {
		return nil, nil
	}

	pollyClient, err := c.clientForSpeaker(ctx, line.Speaker)
	if err != nil {
		return nil, fmt.Errorf("multispeaker: get client for speaker %q: %w", line.Speaker, err)
	}

	ssml := formatSSMLWithEmotion(line.Text, line.Emotion)
	if c.PronunciationProcessor != nil {
		ssml = c.PronunciationProcessor.Process(ssml)
	}
	return pollyClient.SynthesizeSSML(ctx, ssml)
}

// clientForSpeaker returns (or lazily creates) a PollyCLIClient for the given speaker.
// Falls back to the default client for unknown speakers.
func (c *PollyMultiSpeakerClient) clientForSpeaker(ctx context.Context, speaker string) (*PollyCLIClient, error) {
	if speaker == "" {
		return c.defaultClient, nil
	}

	// Check cache
	if cl, ok := c.speakerClients[speaker]; ok {
		return cl, nil
	}

	// Look up registry
	if c.registry != nil {
		meta, err := c.registry.GetMeta(ctx, speaker)
		if err != nil {
			return nil, fmt.Errorf("registry.GetMeta(%q): %w", speaker, err)
		}
		if meta != nil && meta.VoiceID != "" {
			cl := c.buildClientWithVoice(meta.VoiceID)
			c.speakerClients[speaker] = cl
			return cl, nil
		}
	}

	// Not found in registry — use default
	c.speakerClients[speaker] = c.defaultClient
	return c.defaultClient, nil
}

// buildClientWithVoice creates a PollyCLIClient with an explicit voiceID.
func (c *PollyMultiSpeakerClient) buildClientWithVoice(voiceID string) *PollyCLIClient {
	base := NewPollyCLIClientWithLanguage(c.region, c.accessKey, c.secretKey, c.defaultLanguage)
	base.voiceID = voiceID
	base.PronunciationProcessor = c.PronunciationProcessor
	if c.commandFactoryOverride != nil {
		base.commandFactory = c.commandFactoryOverride
	}
	return base
}

// formatSSMLWithEmotion produces SSML for the given text and emotion.
// It maps emotions to SSML prosody hints and delegates whisper to formatSSML.
func formatSSMLWithEmotion(text, emotion string) string {
	// Handle whisper via the existing mechanism — prefix the text so formatSSML detects it
	if strings.EqualFold(emotion, "whisper") {
		return formatSSML("Whisper: " + text)
	}

	// Clean the text (strip prefixes, stage directions, quotes)
	cleaned := cleanText(text)
	if cleaned == "" {
		return "<speak></speak>"
	}

	// XML escape
	safe := html.EscapeString(strings.TrimSpace(cleaned))
	if safe == "" {
		return "<speak></speak>"
	}

	// Wrap in emotion prosody
	var inner string
	switch strings.ToLower(emotion) {
	case "angry":
		inner = fmt.Sprintf(`<prosody rate="fast" pitch="+5%%">%s</prosody>`, safe)
	case "sad":
		inner = fmt.Sprintf(`<prosody rate="slow" pitch="-5%%">%s</prosody>`, safe)
	case "happy":
		inner = fmt.Sprintf(`<prosody rate="medium" pitch="+3%%">%s</prosody>`, safe)
	default:
		inner = safe
	}

	// Wrap in default 90% rate
	return fmt.Sprintf(`<speak><prosody rate="90%%">%s</prosody></speak>`, inner)
}
