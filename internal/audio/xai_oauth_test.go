package audio_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/baochen10luo/stagenthand/internal/audio"
)

type stubXAITTSTokenSource struct {
	token string
	err   error
}

func (s stubXAITTSTokenSource) BearerToken(context.Context) (string, error) {
	return s.token, s.err
}

func TestXAIOAuthTTSClient_SynthesizePostsTTSRequest(t *testing.T) {
	t.Parallel()

	var captured struct {
		Path          string
		Authorization string
		Payload       map[string]any
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		captured.Path = r.URL.Path
		captured.Authorization = r.Header.Get("Authorization")
		if err := json.NewDecoder(r.Body).Decode(&captured.Payload); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		w.Header().Set("Content-Type", "audio/mpeg")
		_, _ = w.Write([]byte("mp3-bytes"))
	}))
	defer server.Close()

	client := audio.NewXAIOAuthTTSClient(server.URL, stubXAITTSTokenSource{token: " oauth-access "}, server.Client())
	got, err := client.Synthesize(context.Background(), "  hello xAI  ", audio.XAITTSOptions{
		VoiceID:  "eve",
		Language: "en",
	})
	if err != nil {
		t.Fatalf("Synthesize() error = %v", err)
	}

	if string(got.Data) != "mp3-bytes" {
		t.Fatalf("Data = %q", got.Data)
	}
	if got.VoiceID != "eve" || got.Language != "en" || got.Codec != "mp3" {
		t.Fatalf("result metadata = %+v", got)
	}
	if captured.Path != "/v1/tts" {
		t.Fatalf("Path = %q, want /v1/tts", captured.Path)
	}
	if captured.Authorization != "Bearer oauth-access" {
		t.Fatalf("Authorization = %q", captured.Authorization)
	}
	if captured.Payload["text"] != "hello xAI" {
		t.Fatalf("text payload = %#v", captured.Payload["text"])
	}
	if captured.Payload["voice_id"] != "eve" {
		t.Fatalf("voice_id payload = %#v", captured.Payload["voice_id"])
	}
	if captured.Payload["language"] != "en" {
		t.Fatalf("language payload = %#v", captured.Payload["language"])
	}
	if _, ok := captured.Payload["model"]; ok {
		t.Fatalf("payload unexpectedly included model: %#v", captured.Payload)
	}
	if _, ok := captured.Payload["output_format"]; ok {
		t.Fatalf("default mp3 payload should omit output_format: %#v", captured.Payload)
	}
}

func TestXAIOAuthTTSClient_SynthesizeCanRequestWAVFormat(t *testing.T) {
	t.Parallel()

	var payload map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		_, _ = w.Write([]byte("wav-bytes"))
	}))
	defer server.Close()

	client := audio.NewXAIOAuthTTSClient(server.URL+"/v1/tts", stubXAITTSTokenSource{token: "oauth-access"}, server.Client())
	_, err := client.Synthesize(context.Background(), "hello", audio.XAITTSOptions{
		VoiceID:    "ara",
		Language:   "fr",
		Codec:      "wav",
		SampleRate: 24000,
	})
	if err != nil {
		t.Fatalf("Synthesize() error = %v", err)
	}

	format, ok := payload["output_format"].(map[string]any)
	if !ok {
		t.Fatalf("output_format = %#v", payload["output_format"])
	}
	if format["codec"] != "wav" {
		t.Fatalf("codec = %#v, want wav", format["codec"])
	}
	if format["sample_rate"] != float64(24000) {
		t.Fatalf("sample_rate = %#v, want 24000", format["sample_rate"])
	}
}

func TestXAIOAuthTTSClient_SynthesizeReturnsResponseError(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`{"error":{"message":"voice not enabled"}}`))
	}))
	defer server.Close()

	client := audio.NewXAIOAuthTTSClient(server.URL, stubXAITTSTokenSource{token: "oauth-access"}, server.Client())
	_, err := client.Synthesize(context.Background(), "hello", audio.XAITTSOptions{})
	if err == nil {
		t.Fatal("Synthesize() error = nil, want HTTP error")
	}
	if !strings.Contains(err.Error(), "voice not enabled") {
		t.Fatalf("error = %v, want parsed message", err)
	}
}

func TestPickXAITTSVoiceDeterministic(t *testing.T) {
	t.Parallel()

	voices := []audio.XAITTSVoice{{ID: "eve"}, {ID: "ara"}, {ID: "test"}}
	first, err := audio.PickXAITTSVoice(voices, 42)
	if err != nil {
		t.Fatalf("PickXAITTSVoice() error = %v", err)
	}
	second, err := audio.PickXAITTSVoice(voices, 42)
	if err != nil {
		t.Fatalf("PickXAITTSVoice() second error = %v", err)
	}
	if first != second {
		t.Fatalf("same seed picked different voices: %+v vs %+v", first, second)
	}
}
