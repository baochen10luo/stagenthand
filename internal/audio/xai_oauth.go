package audio

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/rand"
	"net/http"
	"strings"
	"time"
)

const (
	defaultXAITTSBaseURL    = "https://api.x.ai/v1"
	defaultXAITTSVoiceID    = "eve"
	defaultXAITTSLanguage   = "en"
	defaultXAITTSCodec      = "mp3"
	defaultXAITTSSampleRate = 24000
	defaultXAITTSBitRate    = 128000
)

// XAITTSVoice is a candidate xAI voice ID. Probe a voice before treating it as
// production-ready because OAuth entitlements can differ by account.
type XAITTSVoice struct {
	ID       string `json:"id"`
	Language string `json:"language"`
	Label    string `json:"label,omitempty"`
}

// DefaultXAITTSVoices returns the local candidate set known to work with the
// Hermes xAI TTS shape. The probe CLI verifies actual account support.
func DefaultXAITTSVoices() []XAITTSVoice {
	return []XAITTSVoice{
		{ID: "eve", Language: "en", Label: "default"},
		{ID: "ara", Language: "fr", Label: "hermes-observed"},
	}
}

// PickXAITTSVoice deterministically chooses a voice from a candidate set.
func PickXAITTSVoice(voices []XAITTSVoice, seed int64) (XAITTSVoice, error) {
	if len(voices) == 0 {
		return XAITTSVoice{}, errors.New("xai tts voice list is empty")
	}
	if seed == 0 {
		seed = time.Now().UnixNano()
	}
	return voices[rand.New(rand.NewSource(seed)).Intn(len(voices))], nil
}

// XAITTSTokenSource abstracts OAuth bearer retrieval.
type XAITTSTokenSource interface {
	BearerToken(ctx context.Context) (string, error)
}

// XAITTSHTTPDoer allows the client to be tested with httptest.Server.
type XAITTSHTTPDoer interface {
	Do(req *http.Request) (*http.Response, error)
}

// XAITTSOptions controls a single xAI TTS request.
type XAITTSOptions struct {
	VoiceID    string
	Language   string
	Codec      string
	SampleRate int
	BitRate    int
}

// XAITTSResult is the audio response plus effective request choices.
type XAITTSResult struct {
	Data     []byte
	VoiceID  string
	Language string
	Codec    string
}

// XAIOAuthTTSClient calls xAI OAuth TTS (POST /v1/tts).
type XAIOAuthTTSClient struct {
	baseURL     string
	tokenSource XAITTSTokenSource
	client      XAITTSHTTPDoer
}

func NewXAIOAuthTTSClient(baseURL string, tokenSource XAITTSTokenSource, client XAITTSHTTPDoer) *XAIOAuthTTSClient {
	if client == nil {
		client = &http.Client{Timeout: 60 * time.Second}
	}
	return &XAIOAuthTTSClient{
		baseURL:     normalizeXAITTSBaseURL(baseURL),
		tokenSource: tokenSource,
		client:      client,
	}
}

func normalizeXAITTSBaseURL(baseURL string) string {
	base := strings.TrimRight(strings.TrimSpace(baseURL), "/")
	if base == "" {
		base = defaultXAITTSBaseURL
	}
	for _, suffix := range []string{"/v1/tts", "/tts"} {
		if strings.HasSuffix(base, suffix) {
			return strings.TrimSuffix(base, suffix) + "/v1"
		}
	}
	if strings.HasSuffix(base, "/v1") {
		return base
	}
	return base + "/v1"
}

func (c *XAIOAuthTTSClient) FileExt() string {
	return defaultXAITTSCodec
}

func (c *XAIOAuthTTSClient) GenerateSpeech(ctx context.Context, text string) ([]byte, error) {
	result, err := c.Synthesize(ctx, text, XAITTSOptions{})
	if err != nil {
		return nil, err
	}
	return result.Data, nil
}

func (c *XAIOAuthTTSClient) Synthesize(ctx context.Context, text string, options XAITTSOptions) (XAITTSResult, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	text = strings.TrimSpace(text)
	if text == "" {
		return XAITTSResult{}, errors.New("xai tts text is empty")
	}
	if c.tokenSource == nil {
		return XAITTSResult{}, errors.New("xai tts token source is nil")
	}
	if err := ctx.Err(); err != nil {
		return XAITTSResult{}, err
	}
	token, err := c.tokenSource.BearerToken(ctx)
	if err != nil {
		return XAITTSResult{}, fmt.Errorf("xai tts bearer token: %w", err)
	}
	token = strings.TrimSpace(token)
	if token == "" {
		return XAITTSResult{}, errors.New("xai tts bearer token is empty")
	}

	voiceID := strings.TrimSpace(options.VoiceID)
	if voiceID == "" {
		voiceID = defaultXAITTSVoiceID
	}
	language := strings.TrimSpace(options.Language)
	if language == "" {
		language = defaultXAITTSLanguage
	}
	codec := strings.TrimPrefix(strings.ToLower(strings.TrimSpace(options.Codec)), ".")
	if codec == "" {
		codec = defaultXAITTSCodec
	}

	payload := map[string]any{
		"text":     text,
		"voice_id": voiceID,
		"language": language,
	}
	if codec != defaultXAITTSCodec ||
		options.SampleRate > 0 && options.SampleRate != defaultXAITTSSampleRate ||
		codec == defaultXAITTSCodec && options.BitRate > 0 && options.BitRate != defaultXAITTSBitRate {
		format := map[string]any{"codec": codec}
		if options.SampleRate > 0 {
			format["sample_rate"] = options.SampleRate
		}
		if codec == defaultXAITTSCodec && options.BitRate > 0 {
			format["bit_rate"] = options.BitRate
		}
		payload["output_format"] = format
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return XAITTSResult{}, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/tts", bytes.NewReader(body))
	if err != nil {
		return XAITTSResult{}, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "audio/*,application/json")
	req.Header.Set("User-Agent", "StagentHand/1 xAI-OAuth")

	resp, err := c.client.Do(req)
	if err != nil {
		return XAITTSResult{}, fmt.Errorf("xai tts request failed: %w", err)
	}
	defer resp.Body.Close()
	if err := ctx.Err(); err != nil {
		return XAITTSResult{}, err
	}
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return XAITTSResult{}, err
	}
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		msg := xaiTTSErrorMessage(raw)
		if msg == "" {
			msg = http.StatusText(resp.StatusCode)
		}
		return XAITTSResult{}, fmt.Errorf("xai tts request failed with HTTP %d: %s", resp.StatusCode, msg)
	}
	if len(raw) == 0 {
		return XAITTSResult{}, errors.New("xai tts response was empty")
	}
	return XAITTSResult{
		Data:     raw,
		VoiceID:  voiceID,
		Language: language,
		Codec:    codec,
	}, nil
}

func xaiTTSErrorMessage(raw []byte) string {
	var payload struct {
		Error struct {
			Message string `json:"message"`
			Code    string `json:"code"`
		} `json:"error"`
		Message string `json:"message"`
	}
	if err := json.Unmarshal(raw, &payload); err == nil {
		if payload.Error.Message != "" {
			return payload.Error.Message
		}
		if payload.Error.Code != "" {
			return payload.Error.Code
		}
		if payload.Message != "" {
			return payload.Message
		}
	}
	return strings.TrimSpace(string(raw))
}
