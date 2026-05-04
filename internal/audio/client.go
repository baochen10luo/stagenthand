package audio

import "context"

// Client is the interface for Text-to-Speech (TTS) services.
type Client interface {
	// GenerateSpeech converts text to spoken audio and returns the raw audio bytes (e.g. MP3).
	GenerateSpeech(ctx context.Context, text string) ([]byte, error)
}

// ClientWithExt extends Client with a file extension hint.
// Implementations that output a format other than MP3 (e.g. WAV) should
// implement this so the audio batcher uses the correct file extension.
type ClientWithExt interface {
	Client
	FileExt() string
}
