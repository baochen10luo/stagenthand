package llm_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/baochen10luo/stagenthand/internal/llm"
	"github.com/stretchr/testify/require"
)

type stubBearerTokenSource struct {
	token string
	err   error
}

func (s stubBearerTokenSource) BearerToken(context.Context) (string, error) {
	return s.token, s.err
}

type spyLLMBearerTokenSource struct {
	calls int
}

func (s *spyLLMBearerTokenSource) BearerToken(context.Context) (string, error) {
	s.calls++
	return "oauth-access", nil
}

type failLLMHTTPDoer struct {
	t *testing.T
}

func (d failLLMHTTPDoer) Do(req *http.Request) (*http.Response, error) {
	d.t.Helper()
	d.t.Fatalf("unexpected HTTP request: %s %s", req.Method, req.URL.String())
	return nil, nil
}

type trackingLLMBody struct {
	data      *bytes.Buffer
	readCalls int
	closed    bool
}

func (b *trackingLLMBody) Read(p []byte) (int, error) {
	b.readCalls++
	return b.data.Read(p)
}

func (b *trackingLLMBody) Close() error {
	b.closed = true
	return nil
}

type cancelAfterLLMResponseDoer struct {
	t      *testing.T
	cancel context.CancelFunc
	body   *trackingLLMBody
}

func (d cancelAfterLLMResponseDoer) Do(req *http.Request) (*http.Response, error) {
	d.t.Helper()
	require.Equal(d.t, http.MethodPost, req.Method)
	require.Equal(d.t, "/v1/responses", req.URL.Path)
	d.cancel()
	return &http.Response{
		StatusCode: http.StatusOK,
		Body:       d.body,
	}, nil
}

func TestXAIOAuthClient_GenerateTransformation_UsesResponsesEndpoint(t *testing.T) {
	t.Parallel()

	var captured struct {
		method string
		path   string
		auth   string
		body   map[string]any
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		captured.method = r.Method
		captured.path = r.URL.Path
		captured.auth = r.Header.Get("Authorization")
		require.NoError(t, json.NewDecoder(r.Body).Decode(&captured.body))

		_ = json.NewEncoder(w).Encode(map[string]any{
			"output_text": "{\"ok\":true}",
		})
	}))
	defer server.Close()

	client := llm.NewXAIOAuthClient(server.URL+"/v1", "grok-4.3", stubBearerTokenSource{token: "oauth-access"}, server.Client())
	got, err := client.GenerateTransformation(context.Background(), "System prompt", []byte(`{"title":"test"}`))
	require.NoError(t, err)
	require.JSONEq(t, `{"ok":true}`, string(got))

	require.Equal(t, http.MethodPost, captured.method)
	require.Equal(t, "/v1/responses", captured.path)
	require.Equal(t, "Bearer oauth-access", captured.auth)
	require.Equal(t, "grok-4.3", captured.body["model"])
	require.Equal(t, "System prompt", captured.body["instructions"])
	input, ok := captured.body["input"].([]any)
	require.True(t, ok)
	require.Len(t, input, 1)
	msg, ok := input[0].(map[string]any)
	require.True(t, ok)
	require.Equal(t, "user", msg["role"])
	require.Equal(t, `{"title":"test"}`, msg["content"])
}

func TestXAIOAuthClient_GenerateTransformation_NilContextUsesBackground(t *testing.T) {
	t.Parallel()

	var capturedPath string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedPath = r.URL.Path
		_ = json.NewEncoder(w).Encode(map[string]any{
			"output_text": "{\"ok\":true}",
		})
	}))
	defer server.Close()

	client := llm.NewXAIOAuthClient(server.URL, "grok-4.3", stubBearerTokenSource{token: "oauth-access"}, server.Client())
	got, err := client.GenerateTransformation(nil, "System prompt", []byte("input"))

	require.NoError(t, err)
	require.JSONEq(t, `{"ok":true}`, string(got))
	require.Equal(t, "/v1/responses", capturedPath)
}

func TestXAIOAuthClient_GenerateTransformation_TrimsModelInRequest(t *testing.T) {
	t.Parallel()

	var captured map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.NoError(t, json.NewDecoder(r.Body).Decode(&captured))
		_ = json.NewEncoder(w).Encode(map[string]any{
			"output_text": "{\"ok\":true}",
		})
	}))
	defer server.Close()

	client := llm.NewXAIOAuthClient(server.URL, " grok-4.3 ", stubBearerTokenSource{token: "oauth-access"}, server.Client())
	got, err := client.GenerateTransformation(context.Background(), "System prompt", []byte("input"))
	require.NoError(t, err)
	require.JSONEq(t, `{"ok":true}`, string(got))
	require.Equal(t, "grok-4.3", captured["model"])
}

func TestXAIOAuthClient_GenerateTransformation_TrimsBearerTokenBeforeRequest(t *testing.T) {
	t.Parallel()

	var capturedAuth string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedAuth = r.Header.Get("Authorization")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"output_text": "{\"ok\":true}",
		})
	}))
	defer server.Close()

	client := llm.NewXAIOAuthClient(server.URL, "grok-4.3", stubBearerTokenSource{token: " oauth-access "}, server.Client())
	got, err := client.GenerateTransformation(context.Background(), "System prompt", []byte("input"))
	require.NoError(t, err)
	require.JSONEq(t, `{"ok":true}`, string(got))
	require.Equal(t, "Bearer oauth-access", capturedAuth)
}

func TestXAIOAuthClient_GenerateTransformation_RejectsEmptyBearerTokenBeforeRequest(t *testing.T) {
	t.Parallel()

	client := llm.NewXAIOAuthClient("https://example.invalid", "grok-4.3", stubBearerTokenSource{token: " \t\n"}, failLLMHTTPDoer{t: t})
	_, err := client.GenerateTransformation(context.Background(), "System prompt", []byte("input"))

	require.Error(t, err)
	require.Contains(t, err.Error(), "bearer token is empty")
}

func TestXAIOAuthClient_GenerateTransformation_RejectsCanceledContextBeforeAuth(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	tokenSource := &spyLLMBearerTokenSource{}
	client := llm.NewXAIOAuthClient("https://example.invalid", "grok-4.3", tokenSource, failLLMHTTPDoer{t: t})
	got, err := client.GenerateTransformation(ctx, "System prompt", []byte("input"))

	require.Error(t, err)
	require.True(t, errors.Is(err, context.Canceled), "error = %v, want context.Canceled", err)
	require.Nil(t, got)
	require.Equal(t, 0, tokenSource.calls)
}

func TestXAIOAuthClient_GenerateTransformation_RejectsCanceledContextBeforeReadingResponse(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	body := &trackingLLMBody{data: bytes.NewBufferString(`{"output_text":"{\"ok\":true}"}`)}
	client := llm.NewXAIOAuthClient("https://example.invalid", "grok-4.3", stubBearerTokenSource{token: "oauth-access"}, cancelAfterLLMResponseDoer{
		t:      t,
		cancel: cancel,
		body:   body,
	})
	got, err := client.GenerateTransformation(ctx, "System prompt", []byte("input"))

	require.Error(t, err)
	require.True(t, errors.Is(err, context.Canceled), "error = %v, want context.Canceled", err)
	require.Nil(t, got)
	require.Equal(t, 0, body.readCalls)
	require.True(t, body.closed)
}

func TestXAIOAuthClient_GenerateTransformation_RejectsNonSuccessResponseStatus(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusFound)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"output_text": "{\"ok\":true}",
		})
	}))
	defer server.Close()

	client := llm.NewXAIOAuthClient(server.URL, "grok-4.3", stubBearerTokenSource{token: "oauth-access"}, server.Client())
	got, err := client.GenerateTransformation(context.Background(), "System prompt", []byte("input"))

	require.Error(t, err)
	require.Nil(t, got)
	require.Contains(t, err.Error(), "HTTP 302")
}

func TestXAIOAuthClient_GenerateTransformation_DefaultClientRejectsResponseRedirect(t *testing.T) {
	t.Parallel()

	var redirectCalls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/responses":
			http.Redirect(w, r, "/v1/responses/redirected", http.StatusFound)
		case "/v1/responses/redirected":
			redirectCalls.Add(1)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"output_text": "{\"ok\":true}",
			})
		default:
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
	}))
	defer server.Close()

	client := llm.NewXAIOAuthClient(server.URL, "grok-4.3", stubBearerTokenSource{token: "oauth-access"}, nil)
	got, err := client.GenerateTransformation(context.Background(), "System prompt", []byte("input"))

	require.Error(t, err)
	require.Nil(t, got)
	require.Equal(t, int32(0), redirectCalls.Load())
	require.Contains(t, err.Error(), "HTTP 302")
}

func TestXAIOAuthClient_GenerateTransformation_FallsBackToOutputArray(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"output": []any{
				map[string]any{
					"content": []any{
						map[string]any{"type": "output_text", "text": "```json\n{\"ok\":true}\n```"},
					},
				},
			},
		})
	}))
	defer server.Close()

	client := llm.NewXAIOAuthClient(server.URL, "grok-4.3", stubBearerTokenSource{token: "oauth-access"}, server.Client())
	got, err := client.GenerateTransformation(context.Background(), "System prompt", []byte("input"))
	require.NoError(t, err)
	require.JSONEq(t, `{"ok":true}`, string(bytes.TrimSpace(got)))
}

func TestXAIOAuthClient_GenerateTransformation_FallsBackWhenTopLevelOutputTextIsWhitespace(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"output_text": " \t\n",
			"output": []any{
				map[string]any{
					"content": []any{
						map[string]any{"type": "output_text", "text": "{\"ok\":true}"},
					},
				},
			},
		})
	}))
	defer server.Close()

	client := llm.NewXAIOAuthClient(server.URL, "grok-4.3", stubBearerTokenSource{token: "oauth-access"}, server.Client())
	got, err := client.GenerateTransformation(context.Background(), "System prompt", []byte("input"))

	require.NoError(t, err)
	require.JSONEq(t, `{"ok":true}`, string(got))
}

func TestXAIOAuthClient_GenerateTransformation_RejectsNonOutputTextContent(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"output": []any{
				map[string]any{
					"content": []any{
						map[string]any{"type": "refusal", "text": "not a manifest"},
					},
				},
			},
		})
	}))
	defer server.Close()

	client := llm.NewXAIOAuthClient(server.URL, "grok-4.3", stubBearerTokenSource{token: "oauth-access"}, server.Client())
	got, err := client.GenerateTransformation(context.Background(), "System prompt", []byte("input"))

	require.Error(t, err)
	require.Nil(t, got)
	require.Contains(t, err.Error(), "output_text")
}

func TestXAIOAuthClient_GenerateTransformation_RejectsEmptyContentAfterCleanup(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"output_text": "```json\n\n```",
		})
	}))
	defer server.Close()

	client := llm.NewXAIOAuthClient(server.URL, "grok-4.3", stubBearerTokenSource{token: "oauth-access"}, server.Client())
	_, err := client.GenerateTransformation(context.Background(), "System prompt", []byte("input"))

	require.Error(t, err)
	require.Contains(t, err.Error(), "response was empty after cleanup")
}

func TestXAIOAuthClient_GenerateVisionTransformation_SendsImageInputs(t *testing.T) {
	t.Parallel()

	var captured map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/v1/responses", r.URL.Path)
		require.Equal(t, "Bearer oauth-access", r.Header.Get("Authorization"))
		require.NoError(t, json.NewDecoder(r.Body).Decode(&captured))
		_ = json.NewEncoder(w).Encode(map[string]any{
			"output_text": "{\"status\":\"pass\"}",
		})
	}))
	defer server.Close()

	client := llm.NewXAIOAuthClient(server.URL, "grok-4.3", stubBearerTokenSource{token: "oauth-access"}, server.Client())
	got, err := client.GenerateVisionTransformation(context.Background(), "Audit prompt", "story text", []llm.XAIImageInput{
		{Data: []byte{0xff, 0xd8, 0xff, 0xdb}, MimeType: "image/jpeg"},
	})

	require.NoError(t, err)
	require.JSONEq(t, `{"status":"pass"}`, string(got))
	require.Equal(t, "grok-4.3", captured["model"])
	require.Equal(t, "Audit prompt", captured["instructions"])
	input := captured["input"].([]any)
	require.Len(t, input, 1)
	msg := input[0].(map[string]any)
	require.Equal(t, "user", msg["role"])
	content := msg["content"].([]any)
	require.Len(t, content, 2)
	require.Equal(t, map[string]any{"type": "input_text", "text": "story text"}, content[0])
	image := content[1].(map[string]any)
	require.Equal(t, "input_image", image["type"])
	require.True(t, strings.HasPrefix(image["image_url"].(string), "data:image/jpeg;base64,"))
}

func TestXAIOAuthClient_GenerateVisionTransformation_RejectsNonImages(t *testing.T) {
	t.Parallel()

	client := llm.NewXAIOAuthClient("https://example.invalid", "grok-4.3", stubBearerTokenSource{token: "oauth-access"}, failLLMHTTPDoer{t: t})
	got, err := client.GenerateVisionTransformation(context.Background(), "Audit prompt", "story text", []llm.XAIImageInput{
		{Data: []byte("not an image"), MimeType: "text/plain"},
	})

	require.Error(t, err)
	require.Nil(t, got)
	require.Contains(t, err.Error(), "not an image")
}

func TestBuildXAIResponsesURL(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		baseURL string
		want    string
	}{
		{name: "with v1", baseURL: "https://api.x.ai/v1", want: "https://api.x.ai/v1/responses"},
		{name: "with slash", baseURL: "https://api.x.ai/v1/", want: "https://api.x.ai/v1/responses"},
		{name: "without v1", baseURL: "https://api.x.ai", want: "https://api.x.ai/v1/responses"},
		{name: "full responses endpoint", baseURL: "https://api.x.ai/v1/responses", want: "https://api.x.ai/v1/responses"},
		{name: "videos collection endpoint", baseURL: "https://api.x.ai/v1/videos", want: "https://api.x.ai/v1/responses"},
		{name: "video generation endpoint", baseURL: "https://api.x.ai/v1/videos/generations", want: "https://api.x.ai/v1/responses"},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, tt.want, llm.BuildXAIResponsesURL(tt.baseURL))
		})
	}
}
