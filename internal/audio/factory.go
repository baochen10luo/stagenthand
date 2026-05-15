package audio

import (
	"context"
	"fmt"

	"github.com/baochen10luo/stagenthand/config"
	"github.com/baochen10luo/stagenthand/internal/character"
)

// NewTTSClient creates a TTS Client based on cfg.Audio.VoiceProvider.
// lang is the TTS language (e.g. "zh-TW") and comes from the manifest at runtime.
// Falls back to Polly when voice_provider is empty.
func NewTTSClient(dryRun bool, cfg *config.Config, lang string) (Client, error) {
	provider := cfg.Audio.VoiceProvider
	if dryRun {
		provider = "mock"
	}
	switch provider {
	case "aiark":
		return NewAiarkTTSClientWithVoiceID(
			cfg.Audio.AiarkTTSBaseURL,
			cfg.Audio.AiarkTTSAPIKey,
			lang,
			cfg.Audio.AiarkTTSVoice,
			cfg.Audio.AiarkTTSVoiceID,
		), nil
	case "polly", "":
		return NewPollyCLIClientWithLanguage(
			cfg.AWS.Region,
			cfg.AWS.AccessKeyID,
			cfg.AWS.SecretAccessKey,
			lang,
		), nil
	case "mock":
		return &mockTTSClient{}, nil
	default:
		return nil, fmt.Errorf("unsupported voice_provider: %s", provider)
	}
}

// NewMusicClientFromConfig creates a MusicClient based on cfg.Audio.MusicProvider.
// Falls back to Jamendo when music_provider is empty.
func NewMusicClientFromConfig(dryRun bool, cfg *config.Config) (MusicClient, error) {
	provider := cfg.Audio.MusicProvider
	if dryRun {
		provider = "mock"
	}
	switch provider {
	case "aiark":
		return NewAiarkMusicClient(
			cfg.Audio.AiarkMusicBaseURL,
			cfg.Audio.AiarkMusicAPIKey,
		), nil
	case "jamendo", "":
		return NewJamendoClient(cfg.Audio.JamendoClientID), nil
	case "mock":
		return &mockMusicClient{}, nil
	default:
		return nil, fmt.Errorf("unsupported music_provider: %s", provider)
	}
}

// NewMultiSpeakerTTSClientFromConfig creates a MultiSpeakerClient via the config's
// voice_provider setting. cmd/pipeline.go should call this instead of constructing
// provider-specific clients directly, so the provider can be swapped via config.
func NewMultiSpeakerTTSClientFromConfig(dryRun bool, cfg *config.Config, lang string, reg character.Registry) (MultiSpeakerClient, error) {
	if dryRun {
		return &MockMultiSpeakerClient{}, nil
	}
	provider := cfg.Audio.VoiceProvider
	switch provider {
	case "polly", "":
		return NewPollyMultiSpeakerClient(cfg.AWS.Region, cfg.AWS.AccessKeyID, cfg.AWS.SecretAccessKey, lang, reg), nil
	default:
		return nil, fmt.Errorf("multi-speaker not supported for voice_provider: %s", provider)
	}
}

// mockTTSClient is used in dry-run mode.
type mockTTSClient struct{}

func (m *mockTTSClient) GenerateSpeech(_ context.Context, _ string) ([]byte, error) {
	return []byte{}, nil
}

// mockMusicClient is used in dry-run mode.
type mockMusicClient struct{}

func (m *mockMusicClient) SearchAndDownload(_ context.Context, _ string) ([]byte, error) {
	return []byte{}, nil
}
