package image_test

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	ximage "github.com/baochen10luo/stagenthand/internal/image"
)

type stubXAIImageTokenSource struct {
	token string
}

func (s stubXAIImageTokenSource) BearerToken(context.Context) (string, error) {
	return s.token, nil
}

func TestXAIOAuthImageClientGenerateUsesImageGenerationEndpoint(t *testing.T) {
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
		writeXAIImageB64Response(t, w, []byte("png-bytes"))
	}))
	defer server.Close()

	client := ximage.NewXAIOAuthImageClient(server.URL, "grok-imagine-test", "9:16", "1k", stubXAIImageTokenSource{token: " oauth-access "}, server.Client())
	got, err := client.GenerateImage(context.Background(), "moose character sheet", nil)
	if err != nil {
		t.Fatalf("GenerateImage() error = %v", err)
	}

	if string(got) != "png-bytes" {
		t.Fatalf("GenerateImage() = %q", got)
	}
	if captured.Path != "/v1/images/generations" {
		t.Fatalf("Path = %q, want /v1/images/generations", captured.Path)
	}
	if captured.Authorization != "Bearer oauth-access" {
		t.Fatalf("Authorization = %q", captured.Authorization)
	}
	if captured.Payload["model"] != "grok-imagine-test" {
		t.Fatalf("model payload = %#v", captured.Payload["model"])
	}
	if captured.Payload["response_format"] != "b64_json" {
		t.Fatalf("response_format payload = %#v", captured.Payload["response_format"])
	}
	if _, ok := captured.Payload["images"]; ok {
		t.Fatalf("generation payload unexpectedly included images: %#v", captured.Payload)
	}
}

func TestXAIOAuthImageClientGenerateUsesXAIModelDefault(t *testing.T) {
	t.Parallel()

	var payload map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		writeXAIImageB64Response(t, w, []byte("png-bytes"))
	}))
	defer server.Close()

	client := ximage.NewXAIOAuthImageClient(server.URL, "", "", "", stubXAIImageTokenSource{token: "oauth-access"}, server.Client())
	if _, err := client.GenerateImage(context.Background(), "moose", nil); err != nil {
		t.Fatalf("GenerateImage() error = %v", err)
	}

	if payload["model"] != "grok-imagine-image-quality" {
		t.Fatalf("model = %#v, want xAI default", payload["model"])
	}
}

func TestXAIOAuthImageClientEditSendsReferenceImages(t *testing.T) {
	t.Parallel()

	refPath := filepath.Join(t.TempDir(), "ref.png")
	if err := os.WriteFile(refPath, []byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n', 0, 0, 0, 0}, 0644); err != nil {
		t.Fatalf("write ref: %v", err)
	}

	var payload map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/images/edits" {
			t.Fatalf("Path = %q, want /v1/images/edits", r.URL.Path)
		}
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		writeXAIImageB64Response(t, w, []byte("edited-bytes"))
	}))
	defer server.Close()

	client := ximage.NewXAIOAuthImageClient(server.URL+"/v1/images", "", "", "", stubXAIImageTokenSource{token: "oauth-access"}, server.Client())
	got, err := client.GenerateImage(context.Background(), "scene still", []string{refPath, "https://example.com/style.jpg"})
	if err != nil {
		t.Fatalf("GenerateImage() error = %v", err)
	}

	if string(got) != "edited-bytes" {
		t.Fatalf("GenerateImage() = %q", got)
	}
	images, ok := payload["images"].([]any)
	if !ok || len(images) != 2 {
		t.Fatalf("images payload = %#v", payload["images"])
	}
	first, ok := images[0].(map[string]any)
	if !ok {
		t.Fatalf("first image payload = %#v", images[0])
	}
	if first["type"] != "image_url" {
		t.Fatalf("first image type = %#v", first["type"])
	}
	if url, _ := first["url"].(string); !strings.HasPrefix(url, "data:image/png;base64,") {
		t.Fatalf("first image url = %q", url)
	}
	second, ok := images[1].(map[string]any)
	if !ok || second["url"] != "https://example.com/style.jpg" {
		t.Fatalf("second image payload = %#v", images[1])
	}
}

func TestXAIOAuthImageClientRejectsTooManyReferences(t *testing.T) {
	t.Parallel()

	client := ximage.NewXAIOAuthImageClient("https://example.invalid", "", "", "", stubXAIImageTokenSource{token: "oauth-access"}, nil)
	_, err := client.GenerateImage(context.Background(), "scene", []string{"a", "b", "c", "d"})
	if err == nil {
		t.Fatal("GenerateImage() error = nil, want too many references error")
	}
	if !strings.Contains(err.Error(), "at most 3") {
		t.Fatalf("error = %v, want max references error", err)
	}
}

func writeXAIImageB64Response(t *testing.T, w http.ResponseWriter, data []byte) {
	t.Helper()
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(map[string]any{
		"data": []map[string]any{
			{"b64_json": base64.StdEncoding.EncodeToString(data), "mime_type": "image/png"},
		},
	}); err != nil {
		t.Fatalf("encode response: %v", err)
	}
}
