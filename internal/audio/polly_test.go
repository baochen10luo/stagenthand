package audio

import (
	"context"
	"os"
	"os/exec"
	"testing"
)

func TestFormatSSML(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{"Basic text", "Hello", "<speak><prosody rate=\"90%\">Hello</prosody></speak>"},
		{"Whisper tag", "Whisper: quiet please", "<speak><prosody rate=\"90%\"><amazon:effect name=\"whispered\">quiet please</amazon:effect></prosody></speak>"},
		{"Character prefix", "Narrator: Once upon a time", "<speak><prosody rate=\"90%\">Once upon a time</prosody></speak>"},
		{"Stage directions", "Go away [shouting]", "<speak><prosody rate=\"90%\">Go away</prosody></speak>"},
		{"Scrub quotes", "\"Yes\", he said", "<speak><prosody rate=\"90%\">Yes, he said</prosody></speak>"},
		{"XML escape", "Tom & Jerry", "<speak><prosody rate=\"90%\">Tom &amp; Jerry</prosody></speak>"},
		{"Numeric hyphen to zhi", "版本 3-5 已發布", "<speak><prosody rate=\"90%\">版本 3之5 已發布</prosody></speak>"},
		{"Full-width brackets add break", "提示【重要事項】請記住", "<speak><prosody rate=\"90%\">提示【重要事項】<break time=\"500ms\"/>請記住</prosody></speak>"},
		{"Empty string", "", "<speak></speak>"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := formatSSML(tt.input)
			if got != tt.expected {
				t.Errorf("formatSSML(%q) = %q, want %q", tt.input, got, tt.expected)
			}
		})
	}
}

func TestPollyCLIClient_GenerateSpeech_Basic(t *testing.T) {
	fakeOutput := []byte("fake-mp3-content")
	c := NewPollyCLIClient("us-east-1", "ak", "sk")
	
	// Mock commandFactory to succeed and produce a fake file
	c.commandFactory = func(ctx context.Context, name string, args ...string) *exec.Cmd {
		// The last arg is the output file path in GenerateSpeech
		outPath := args[len(args)-1]
		os.WriteFile(outPath, fakeOutput, 0644)
		// Return a command that does nothing (true)
		return exec.Command("true")
	}

	got, err := c.GenerateSpeech(context.Background(), "Hello")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if string(got) != string(fakeOutput) {
		t.Errorf("GenerateSpeech() = %q, want %q", string(got), string(fakeOutput))
	}
}

func TestPollyCLIClient_GenerateSpeech_Empty(t *testing.T) {
	c := NewPollyCLIClient("us-east-1", "ak", "sk")
	got, err := c.GenerateSpeech(context.Background(), "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != nil {
		t.Errorf("expected nil for empty text, got %v", got)
	}
}

func TestPollyCLIClient_GenerateSpeech_Error(t *testing.T) {
	c := NewPollyCLIClient("us-east-1", "ak", "sk")
	c.commandFactory = func(ctx context.Context, name string, args ...string) *exec.Cmd {
		// Return a command that fails
		return exec.Command("false")
	}

	_, err := c.GenerateSpeech(context.Background(), "Fail")
	if err == nil {
		t.Fatal("expected error from failed command, got nil")
	}
}

func TestNewPollyCLIClientWithLanguage_LanguageMapping(t *testing.T) {
	tests := []struct {
		language         string
		expectedVoiceID  string
		expectedLangCode string
	}{
		{"zh-TW", "Zhiyu", "cmn-CN"},
		{"cmn-CN", "Zhiyu", "cmn-CN"},
		{"en-US", "Joanna", "en-US"},
		{"en-GB", "Amy", "en-GB"},
		{"ja-JP", "Takumi", "ja-JP"},
		{"ko-KR", "Seoyeon", "ko-KR"},
	}
	for _, tt := range tests {
		t.Run(tt.language, func(t *testing.T) {
			c := NewPollyCLIClientWithLanguage("us-east-1", "ak", "sk", tt.language)
			if c.voiceID != tt.expectedVoiceID {
				t.Errorf("voiceID = %q, want %q", c.voiceID, tt.expectedVoiceID)
			}
			if c.languageCode != tt.expectedLangCode {
				t.Errorf("languageCode = %q, want %q", c.languageCode, tt.expectedLangCode)
			}
		})
	}
}

func TestNewPollyCLIClientWithLanguage_UnknownFallback(t *testing.T) {
	c := NewPollyCLIClientWithLanguage("us-east-1", "ak", "sk", "fr-FR")
	if c.voiceID != "Zhiyu" {
		t.Errorf("unknown language: voiceID = %q, want Zhiyu", c.voiceID)
	}
	if c.languageCode != "cmn-CN" {
		t.Errorf("unknown language: languageCode = %q, want cmn-CN", c.languageCode)
	}
}

func TestNewPollyCLIClientWithLanguage_EmptyFallback(t *testing.T) {
	c := NewPollyCLIClientWithLanguage("us-east-1", "ak", "sk", "")
	if c.voiceID != "Zhiyu" {
		t.Errorf("empty language: voiceID = %q, want Zhiyu", c.voiceID)
	}
	if c.languageCode != "cmn-CN" {
		t.Errorf("empty language: languageCode = %q, want cmn-CN", c.languageCode)
	}
}
