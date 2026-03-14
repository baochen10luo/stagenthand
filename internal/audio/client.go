package audio

import "context"

// Client is the interface for Text-to-Speech (TTS) services.
type Client interface {
	// GenerateSpeech converts text to spoken audio and returns the raw audio bytes (e.g. MP3).
	GenerateSpeech(ctx context.Context, text string) ([]byte, error)
}
