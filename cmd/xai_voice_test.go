package cmd

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/baochen10luo/stagenthand/config"
	"github.com/baochen10luo/stagenthand/internal/audio"
)

type stubXAIVoiceSynthesizer struct {
	failVoices map[string]error
	calls      []audio.XAITTSOptions
}

func (s *stubXAIVoiceSynthesizer) Synthesize(ctx context.Context, text string, opts audio.XAITTSOptions) (audio.XAITTSResult, error) {
	if err := ctx.Err(); err != nil {
		return audio.XAITTSResult{}, err
	}
	s.calls = append(s.calls, opts)
	if err := s.failVoices[opts.VoiceID]; err != nil {
		return audio.XAITTSResult{}, err
	}
	return audio.XAITTSResult{
		Data:     []byte("audio-" + opts.VoiceID),
		VoiceID:  opts.VoiceID,
		Language: opts.Language,
		Codec:    opts.Codec,
	}, nil
}

func TestRunXAIVoiceProbeWritesOneOutputPerVoice(t *testing.T) {
	oldFactory := newXAIVoiceSynthesizer
	t.Cleanup(func() { newXAIVoiceSynthesizer = oldFactory })

	synth := &stubXAIVoiceSynthesizer{}
	newXAIVoiceSynthesizer = func(*config.Config) (xaiVoiceSynthesizer, error) {
		return synth, nil
	}

	outputDir := t.TempDir()
	var out strings.Builder
	err := runXAIVoiceProbe(context.Background(), &config.Config{}, xaiVoiceProbeOptions{
		Text:      "測試 xAI voice",
		Voices:    "eve,ara",
		Language:  "zh",
		Codec:     "mp3",
		OutputDir: outputDir,
	}, &out)
	if err != nil {
		t.Fatalf("runXAIVoiceProbe() error = %v", err)
	}

	for _, voice := range []string{"eve", "ara"} {
		path := filepath.Join(outputDir, voice+".mp3")
		got, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read %s: %v", path, err)
		}
		if string(got) != "audio-"+voice {
			t.Fatalf("%s = %q", path, got)
		}
		if !strings.Contains(out.String(), path) {
			t.Fatalf("summary did not include %s: %s", path, out.String())
		}
	}
	if len(synth.calls) != 2 {
		t.Fatalf("calls = %d, want 2", len(synth.calls))
	}
	if synth.calls[0].Language != "zh" || synth.calls[1].Language != "zh" {
		t.Fatalf("language override not propagated: %#v", synth.calls)
	}
}

func TestRunXAIVoiceProbeRandomUsesOneVoice(t *testing.T) {
	oldFactory := newXAIVoiceSynthesizer
	t.Cleanup(func() { newXAIVoiceSynthesizer = oldFactory })

	synth := &stubXAIVoiceSynthesizer{}
	newXAIVoiceSynthesizer = func(*config.Config) (xaiVoiceSynthesizer, error) {
		return synth, nil
	}

	output := filepath.Join(t.TempDir(), "voice.mp3")
	var out strings.Builder
	err := runXAIVoiceProbe(context.Background(), nil, xaiVoiceProbeOptions{
		Text:   "hello",
		Voices: "eve,ara,zed",
		Random: true,
		Seed:   9,
		Output: output,
	}, &out)
	if err != nil {
		t.Fatalf("runXAIVoiceProbe() error = %v", err)
	}

	if len(synth.calls) != 1 {
		t.Fatalf("calls = %d, want 1", len(synth.calls))
	}
	if _, err := os.Stat(output); err != nil {
		t.Fatalf("random probe output missing: %v", err)
	}
	if !strings.Contains(out.String(), `"random": true`) {
		t.Fatalf("summary missing random flag: %s", out.String())
	}
}

func TestRunXAIVoiceProbeReturnsErrorWhenAllVoicesFail(t *testing.T) {
	oldFactory := newXAIVoiceSynthesizer
	t.Cleanup(func() { newXAIVoiceSynthesizer = oldFactory })

	newXAIVoiceSynthesizer = func(*config.Config) (xaiVoiceSynthesizer, error) {
		return &stubXAIVoiceSynthesizer{
			failVoices: map[string]error{
				"eve": errors.New("not entitled"),
				"ara": errors.New("not entitled"),
			},
		}, nil
	}

	var out strings.Builder
	err := runXAIVoiceProbe(context.Background(), nil, xaiVoiceProbeOptions{
		Text:   "hello",
		Voices: "eve,ara",
	}, &out)
	if err == nil {
		t.Fatal("runXAIVoiceProbe() error = nil, want all-failed error")
	}
	if !strings.Contains(err.Error(), "failed for all 2") {
		t.Fatalf("error = %v", err)
	}
	if !strings.Contains(out.String(), `"status": "error"`) {
		t.Fatalf("summary missing failed result: %s", out.String())
	}
}
