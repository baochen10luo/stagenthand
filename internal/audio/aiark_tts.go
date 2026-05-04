package audio

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// AiarkTTSClient calls the aiark Qwen3-TTS API (POST /tts/v1/audio/speech).
// It returns WAV bytes and implements ClientWithExt ("wav").
type AiarkTTSClient struct {
	baseURL    string
	apiKey     string
	language   string
	voice      string
	voiceID    string // when set, uses voice_id (voice clone) instead of voice name
	httpClient *http.Client
}

// NewAiarkTTSClient creates a TTS client pointed at the aiark TTS endpoint.
// language maps zh-TW → "Chinese"; voice is the speaker name (default "Ryan").
func NewAiarkTTSClient(baseURL, apiKey, language, voice string) *AiarkTTSClient {
	return NewAiarkTTSClientWithVoiceID(baseURL, apiKey, language, voice, "")
}

// NewAiarkTTSClientWithVoiceID creates a TTS client that uses voice cloning via voice_id.
// When voiceID is non-empty, it takes precedence over voice name.
func NewAiarkTTSClientWithVoiceID(baseURL, apiKey, language, voice, voiceID string) *AiarkTTSClient {
	if baseURL == "" {
		baseURL = "https://aiark.com.tw/tts"
	}
	if voice == "" && voiceID == "" {
		voice = "Ryan"
	}
	return &AiarkTTSClient{
		baseURL:    strings.TrimRight(baseURL, "/"),
		apiKey:     apiKey,
		language:   mapLanguage(language),
		voice:      voice,
		voiceID:    voiceID,
		httpClient: &http.Client{Timeout: 120 * time.Second},
	}
}

// mapLanguage converts shand language codes to Qwen3-TTS language strings.
func mapLanguage(lang string) string {
	switch strings.ToLower(lang) {
	case "zh-tw", "zh-cn", "zh":
		return "Chinese"
	case "ja-jp", "ja":
		return "Japanese"
	case "ko-kr", "ko":
		return "Korean"
	case "en-us", "en":
		return "English"
	default:
		if lang == "" {
			return "Chinese"
		}
		return lang
	}
}

// FileExt implements ClientWithExt — aiark TTS outputs WAV.
func (c *AiarkTTSClient) FileExt() string { return "wav" }

// GenerateSpeech calls POST /tts/v1/audio/speech and downloads the resulting WAV.
func (c *AiarkTTSClient) GenerateSpeech(ctx context.Context, text string) ([]byte, error) {
	payload := map[string]any{
		"input":    text,
		"language": c.language,
		"voice":    c.voice,
		"model":    "aiark/qwen3-tts-1.7b-base",
	}
	if c.voiceID != "" {
		payload["voice_id"] = c.voiceID
		delete(payload, "voice")
	}
	body, _ := json.Marshal(payload)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/v1/audio/speech", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	if c.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+c.apiKey)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("aiark tts generate: %w", err)
	}
	defer resp.Body.Close()
	respBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("aiark tts status %d: %s", resp.StatusCode, string(respBody))
	}

	var result struct {
		File string `json:"file"`
	}
	if err := json.Unmarshal(respBody, &result); err != nil || result.File == "" {
		return nil, fmt.Errorf("aiark tts: unexpected response: %s", string(respBody))
	}

	fileURL := result.File
	if !strings.HasPrefix(fileURL, "http") {
		fileURL = c.baseURL + fileURL
	}

	dlReq, err := http.NewRequestWithContext(ctx, http.MethodGet, fileURL, nil)
	if err != nil {
		return nil, err
	}
	if c.apiKey != "" {
		dlReq.Header.Set("Authorization", "Bearer "+c.apiKey)
	}
	dlResp, err := c.httpClient.Do(dlReq)
	if err != nil {
		return nil, fmt.Errorf("aiark tts download: %w", err)
	}
	defer dlResp.Body.Close()
	if dlResp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("aiark tts download status %d", dlResp.StatusCode)
	}
	return io.ReadAll(dlResp.Body)
}
