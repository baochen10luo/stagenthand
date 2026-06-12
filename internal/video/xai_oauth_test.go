package video_test

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/baochen10luo/stagenthand/internal/video"
)

type stubVideoBearerTokenSource struct {
	token string
	err   error
}

func (s stubVideoBearerTokenSource) BearerToken(context.Context) (string, error) {
	return s.token, s.err
}

type spyVideoBearerTokenSource struct {
	calls int
}

func (s *spyVideoBearerTokenSource) BearerToken(context.Context) (string, error) {
	s.calls++
	return "oauth-access", nil
}

type failVideoHTTPDoer struct {
	t *testing.T
}

func (d failVideoHTTPDoer) Do(req *http.Request) (*http.Response, error) {
	d.t.Helper()
	d.t.Fatalf("unexpected HTTP request: %s %s", req.Method, req.URL.String())
	return nil, nil
}

type trackingVideoBody struct {
	data      *strings.Reader
	readCalls int
	closed    bool
}

func (b *trackingVideoBody) Read(p []byte) (int, error) {
	b.readCalls++
	return b.data.Read(p)
}

func (b *trackingVideoBody) Close() error {
	b.closed = true
	return nil
}

type cancelAfterSubmitVideoHTTPDoer struct {
	t         *testing.T
	cancel    context.CancelFunc
	body      *trackingVideoBody
	pollCalls int
}

func (d *cancelAfterSubmitVideoHTTPDoer) Do(req *http.Request) (*http.Response, error) {
	d.t.Helper()
	switch req.URL.Path {
	case "/v1/videos/generations":
		d.cancel()
		return &http.Response{
			StatusCode: http.StatusOK,
			Body:       d.body,
		}, nil
	case "/v1/videos/req_cancel_submit_123":
		d.pollCalls++
		return videoHTTPResponse(http.StatusOK, `{"status":"done","video":{"url":"https://example.invalid/download.mp4"}}`), nil
	default:
		d.t.Fatalf("unexpected HTTP request: %s %s", req.Method, req.URL.String())
		return nil, nil
	}
}

type cancelAfterPollVideoHTTPDoer struct {
	t             *testing.T
	cancel        context.CancelFunc
	body          *trackingVideoBody
	downloadCalls int
}

func (d *cancelAfterPollVideoHTTPDoer) Do(req *http.Request) (*http.Response, error) {
	d.t.Helper()
	switch req.URL.Path {
	case "/v1/videos/generations":
		return videoHTTPResponse(http.StatusOK, `{"request_id":"req_cancel_123"}`), nil
	case "/v1/videos/req_cancel_123":
		d.cancel()
		if d.body != nil {
			return &http.Response{
				StatusCode: http.StatusOK,
				Body:       d.body,
			}, nil
		}
		return videoHTTPResponse(http.StatusOK, `{"status":"done","video":{"url":"https://example.invalid/download.mp4"}}`), nil
	case "/download.mp4":
		d.downloadCalls++
		return videoHTTPResponse(http.StatusOK, "downloaded-mp4-bytes"), nil
	default:
		d.t.Fatalf("unexpected HTTP request: %s %s", req.Method, req.URL.String())
		return nil, nil
	}
}

type cancelAfterDownloadVideoHTTPDoer struct {
	t      *testing.T
	cancel context.CancelFunc
	body   *trackingVideoBody
}

func (d *cancelAfterDownloadVideoHTTPDoer) Do(req *http.Request) (*http.Response, error) {
	d.t.Helper()
	switch req.URL.Path {
	case "/v1/videos/generations":
		return videoHTTPResponse(http.StatusOK, `{"request_id":"req_cancel_download_123"}`), nil
	case "/v1/videos/req_cancel_download_123":
		return videoHTTPResponse(http.StatusOK, `{"status":"done","video":{"url":"https://example.invalid/download.mp4"}}`), nil
	case "/download.mp4":
		d.cancel()
		return &http.Response{
			StatusCode: http.StatusOK,
			Body:       d.body,
		}, nil
	default:
		d.t.Fatalf("unexpected HTTP request: %s %s", req.Method, req.URL.String())
		return nil, nil
	}
}

func videoHTTPResponse(status int, body string) *http.Response {
	return &http.Response{
		StatusCode: status,
		Body:       io.NopCloser(strings.NewReader(body)),
	}
}

func TestXAIOAuthClient_GenerateVideo_RejectsEmptyPromptBeforeAuth(t *testing.T) {
	t.Parallel()

	tokenSource := &spyVideoBearerTokenSource{}
	client := video.NewXAIOAuthClient("https://example.invalid", "grok-imagine-video", tokenSource, failVideoHTTPDoer{t: t})
	_, err := client.GenerateVideo(context.Background(), "", " \t\n")

	if err == nil {
		t.Fatal("GenerateVideo() error = nil, want prompt validation error")
	}
	if !strings.Contains(err.Error(), "prompt is empty") {
		t.Fatalf("GenerateVideo() error = %q, want prompt is empty", err.Error())
	}
	if tokenSource.calls != 0 {
		t.Fatalf("BearerToken() calls = %d, want 0", tokenSource.calls)
	}
}

func TestXAIOAuthClient_GenerateVideo_RejectsCanceledContextBeforeAuth(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	tokenSource := &spyVideoBearerTokenSource{}
	client := video.NewXAIOAuthClient("https://example.invalid", "grok-imagine-video", tokenSource, failVideoHTTPDoer{t: t})
	_, err := client.GenerateVideo(ctx, "", "make it move")

	if !errors.Is(err, context.Canceled) {
		t.Fatalf("GenerateVideo() error = %v, want context.Canceled", err)
	}
	if tokenSource.calls != 0 {
		t.Fatalf("BearerToken() calls = %d, want 0", tokenSource.calls)
	}
}

func TestXAIOAuthClient_GenerateVideoWithOptionsResult_NilContextUsesBackground(t *testing.T) {
	t.Parallel()

	var capturedSubmitPath string
	var capturedPollPath string
	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/videos/generations":
			capturedSubmitPath = r.URL.Path
			_ = json.NewEncoder(w).Encode(map[string]any{"request_id": "req_nil_context_123"})
		case "/v1/videos/req_nil_context_123":
			capturedPollPath = r.URL.Path
			_ = json.NewEncoder(w).Encode(map[string]any{
				"status": "done",
				"video":  map[string]any{"url": server.URL + "/download.mp4"},
			})
		case "/download.mp4":
			_, _ = w.Write([]byte("fake-mp4-bytes"))
		default:
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
	}))
	defer server.Close()

	client := video.NewXAIOAuthClient(server.URL, "grok-imagine-video", stubVideoBearerTokenSource{token: "oauth-access"}, server.Client())
	got, err := client.GenerateVideoWithOptionsResult(nil, "", "make it move", video.GenerateVideoOptions{})

	if err != nil {
		t.Fatalf("GenerateVideoWithOptionsResult() error = %v", err)
	}
	if string(got.Data) != "fake-mp4-bytes" {
		t.Fatalf("GenerateVideoWithOptionsResult().Data = %q, want fake-mp4-bytes", string(got.Data))
	}
	if got.RequestID != "req_nil_context_123" {
		t.Fatalf("GenerateVideoWithOptionsResult().RequestID = %q, want req_nil_context_123", got.RequestID)
	}
	if capturedSubmitPath != "/v1/videos/generations" {
		t.Fatalf("submit path = %q, want /v1/videos/generations", capturedSubmitPath)
	}
	if capturedPollPath != "/v1/videos/req_nil_context_123" {
		t.Fatalf("poll path = %q, want /v1/videos/req_nil_context_123", capturedPollPath)
	}
}

func TestXAIOAuthClient_GenerateVideoWithOptionsResult_RejectsCanceledContextBeforeReadingSubmitResponse(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	body := &trackingVideoBody{data: strings.NewReader(`{"request_id":"req_cancel_submit_123"}`)}
	doer := &cancelAfterSubmitVideoHTTPDoer{t: t, cancel: cancel, body: body}
	client := video.NewXAIOAuthClient("https://example.invalid", "grok-imagine-video", stubVideoBearerTokenSource{token: "oauth-access"}, doer)
	got, err := client.GenerateVideoWithOptionsResult(ctx, "", "make it move", video.GenerateVideoOptions{})

	if !errors.Is(err, context.Canceled) {
		t.Fatalf("GenerateVideoWithOptionsResult() error = %v, want context.Canceled", err)
	}
	if got.Data != nil || got.RequestID != "" || got.Status != "" || got.VideoURL != "" {
		t.Fatalf("GenerateVideoWithOptionsResult() = %#v, want zero result on canceled context", got)
	}
	if body.readCalls != 0 {
		t.Fatalf("submit response body reads = %d, want 0", body.readCalls)
	}
	if !body.closed {
		t.Fatal("submit response body should be closed")
	}
	if doer.pollCalls != 0 {
		t.Fatalf("poll calls = %d, want 0", doer.pollCalls)
	}
}

func TestXAIOAuthClient_GenerateVideoWithOptionsResult_RejectsCanceledContextBeforeReadingPollResponse(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	body := &trackingVideoBody{data: strings.NewReader(`{"status":"done","video":{"url":"https://example.invalid/download.mp4"}}`)}
	doer := &cancelAfterPollVideoHTTPDoer{t: t, cancel: cancel, body: body}
	client := video.NewXAIOAuthClient("https://example.invalid", "grok-imagine-video", stubVideoBearerTokenSource{token: "oauth-access"}, doer)
	got, err := client.GenerateVideoWithOptionsResult(ctx, "", "make it move", video.GenerateVideoOptions{})

	if !errors.Is(err, context.Canceled) {
		t.Fatalf("GenerateVideoWithOptionsResult() error = %v, want context.Canceled", err)
	}
	if got.Data != nil || got.RequestID != "" || got.Status != "" || got.VideoURL != "" {
		t.Fatalf("GenerateVideoWithOptionsResult() = %#v, want zero result on canceled context", got)
	}
	if body.readCalls != 0 {
		t.Fatalf("poll response body reads = %d, want 0", body.readCalls)
	}
	if !body.closed {
		t.Fatal("poll response body should be closed")
	}
	if doer.downloadCalls != 0 {
		t.Fatalf("download calls = %d, want 0", doer.downloadCalls)
	}
}

func TestXAIOAuthClient_GenerateVideoWithOptionsResult_RejectsCanceledContextBeforeDownload(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	doer := &cancelAfterPollVideoHTTPDoer{t: t, cancel: cancel}
	client := video.NewXAIOAuthClient("https://example.invalid", "grok-imagine-video", stubVideoBearerTokenSource{token: "oauth-access"}, doer)
	got, err := client.GenerateVideoWithOptionsResult(ctx, "", "make it move", video.GenerateVideoOptions{})

	if !errors.Is(err, context.Canceled) {
		t.Fatalf("GenerateVideoWithOptionsResult() error = %v, want context.Canceled", err)
	}
	if got.Data != nil || got.RequestID != "" || got.Status != "" || got.VideoURL != "" {
		t.Fatalf("GenerateVideoWithOptionsResult() = %#v, want zero result on canceled context", got)
	}
	if doer.downloadCalls != 0 {
		t.Fatalf("download calls = %d, want 0", doer.downloadCalls)
	}
}

func TestXAIOAuthClient_GenerateVideoWithOptionsResult_RejectsCanceledContextBeforeReadingDownloadResponse(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	body := &trackingVideoBody{data: strings.NewReader("downloaded-mp4-bytes")}
	doer := &cancelAfterDownloadVideoHTTPDoer{t: t, cancel: cancel, body: body}
	client := video.NewXAIOAuthClient("https://example.invalid", "grok-imagine-video", stubVideoBearerTokenSource{token: "oauth-access"}, doer)
	got, err := client.GenerateVideoWithOptionsResult(ctx, "", "make it move", video.GenerateVideoOptions{})

	if !errors.Is(err, context.Canceled) {
		t.Fatalf("GenerateVideoWithOptionsResult() error = %v, want context.Canceled", err)
	}
	if got.Data != nil || got.RequestID != "" || got.Status != "" || got.VideoURL != "" {
		t.Fatalf("GenerateVideoWithOptionsResult() = %#v, want zero result on canceled context", got)
	}
	if body.readCalls != 0 {
		t.Fatalf("download response body reads = %d, want 0", body.readCalls)
	}
	if !body.closed {
		t.Fatal("download response body should be closed")
	}
}

func TestXAIOAuthClient_GenerateVideo_TrimsBearerTokenBeforeRequest(t *testing.T) {
	t.Parallel()

	var capturedAuth string
	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/videos/generations":
			capturedAuth = r.Header.Get("Authorization")
			_ = json.NewEncoder(w).Encode(map[string]any{"request_id": "req_123"})
		case "/v1/videos/req_123":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"status": "done",
				"video":  map[string]any{"url": server.URL + "/download.mp4"},
			})
		case "/download.mp4":
			_, _ = w.Write([]byte("fake-mp4-bytes"))
		default:
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
	}))
	defer server.Close()

	client := video.NewXAIOAuthClient(server.URL, "grok-imagine-video", stubVideoBearerTokenSource{token: " oauth-access "}, server.Client())
	_, err := client.GenerateVideo(context.Background(), "", "make it move")
	if err != nil {
		t.Fatalf("GenerateVideo() error = %v", err)
	}
	if capturedAuth != "Bearer oauth-access" {
		t.Fatalf("Authorization = %q, want Bearer oauth-access", capturedAuth)
	}
}

func TestXAIOAuthClient_GenerateVideo_RejectsEmptyBearerTokenBeforeRequest(t *testing.T) {
	t.Parallel()

	client := video.NewXAIOAuthClient("https://example.invalid", "grok-imagine-video", stubVideoBearerTokenSource{token: " \t\n"}, failVideoHTTPDoer{t: t})
	_, err := client.GenerateVideo(context.Background(), "", "make it move")

	if err == nil {
		t.Fatal("GenerateVideo() error = nil, want bearer token validation error")
	}
	if !strings.Contains(err.Error(), "bearer token is empty") {
		t.Fatalf("GenerateVideo() error = %q, want bearer token is empty", err.Error())
	}
}

func TestXAIOAuthClient_GenerateVideo_UsesOAuthBearer(t *testing.T) {
	t.Parallel()

	var captured struct {
		submitPath string
		pollPath   string
		auth       string
		body       map[string]any
	}
	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/videos/generations":
			captured.submitPath = r.URL.Path
			captured.auth = r.Header.Get("Authorization")
			if err := json.NewDecoder(r.Body).Decode(&captured.body); err != nil {
				t.Fatalf("Decode() error = %v", err)
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"request_id": "req_123"})
		case "/v1/videos/req_123":
			captured.pollPath = r.URL.Path
			_ = json.NewEncoder(w).Encode(map[string]any{
				"status": "done",
				"video":  map[string]any{"url": server.URL + "/download.mp4"},
			})
		case "/download.mp4":
			_, _ = w.Write([]byte("fake-mp4-bytes"))
		default:
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
	}))
	defer server.Close()

	client := video.NewXAIOAuthClient(server.URL, "grok-imagine-video", stubVideoBearerTokenSource{token: "oauth-access"}, server.Client())
	got, err := client.GenerateVideo(context.Background(), "https://example.com/panel.png", "make it move")
	if err != nil {
		t.Fatalf("GenerateVideo() error = %v", err)
	}

	if string(got) != "fake-mp4-bytes" {
		t.Fatalf("GenerateVideo() = %q, want fake-mp4-bytes", string(got))
	}
	if captured.submitPath != "/v1/videos/generations" {
		t.Fatalf("submit path = %q, want /v1/videos/generations", captured.submitPath)
	}
	if captured.pollPath != "/v1/videos/req_123" {
		t.Fatalf("poll path = %q, want /v1/videos/req_123", captured.pollPath)
	}
	if captured.auth != "Bearer oauth-access" {
		t.Fatalf("Authorization = %q, want Bearer oauth-access", captured.auth)
	}
	if captured.body["model"] != "grok-imagine-video" {
		t.Fatalf("model = %q, want grok-imagine-video", captured.body["model"])
	}
	if captured.body["prompt"] != "make it move" {
		t.Fatalf("prompt = %q, want make it move", captured.body["prompt"])
	}
	image, ok := captured.body["image"].(map[string]any)
	if !ok {
		t.Fatalf("image payload = %#v, want object", captured.body["image"])
	}
	if image["url"] != "https://example.com/panel.png" {
		t.Fatalf("image.url = %q, want https://example.com/panel.png", image["url"])
	}
}

func TestXAIOAuthClient_GenerateVideo_NormalizesFullEndpointBaseURL(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		basePath string
	}{
		{name: "responses endpoint", basePath: "/v1/responses"},
		{name: "video generation endpoint", basePath: "/v1/videos/generations"},
		{name: "videos collection endpoint", basePath: "/v1/videos"},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			var capturedSubmitPath string
			var capturedPollPath string
			var server *httptest.Server
			server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case "/v1/videos/generations":
					capturedSubmitPath = r.URL.Path
					_ = json.NewEncoder(w).Encode(map[string]any{"request_id": "req_base_123"})
				case "/v1/videos/req_base_123":
					capturedPollPath = r.URL.Path
					_ = json.NewEncoder(w).Encode(map[string]any{
						"status": "done",
						"video":  map[string]any{"url": server.URL + "/download.mp4"},
					})
				case "/download.mp4":
					_, _ = w.Write([]byte("fake-mp4-bytes"))
				default:
					http.Error(w, "unexpected path: "+r.URL.Path, http.StatusNotFound)
				}
			}))
			defer server.Close()

			client := video.NewXAIOAuthClient(server.URL+tt.basePath, "grok-imagine-video", stubVideoBearerTokenSource{token: "oauth-access"}, server.Client())
			got, err := client.GenerateVideo(context.Background(), "", "make it move")
			if err != nil {
				t.Fatalf("GenerateVideo() error = %v", err)
			}
			if string(got) != "fake-mp4-bytes" {
				t.Fatalf("GenerateVideo() = %q, want fake-mp4-bytes", string(got))
			}
			if capturedSubmitPath != "/v1/videos/generations" {
				t.Fatalf("submit path = %q, want /v1/videos/generations", capturedSubmitPath)
			}
			if capturedPollPath != "/v1/videos/req_base_123" {
				t.Fatalf("poll path = %q, want /v1/videos/req_base_123", capturedPollPath)
			}
		})
	}
}

func TestXAIOAuthClient_GenerateVideo_TrimsImageURLInRequest(t *testing.T) {
	t.Parallel()

	var captured map[string]any
	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/videos/generations":
			if err := json.NewDecoder(r.Body).Decode(&captured); err != nil {
				t.Fatalf("Decode() error = %v", err)
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"request_id": "req_123"})
		case "/v1/videos/req_123":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"status": "done",
				"video":  map[string]any{"url": server.URL + "/download.mp4"},
			})
		case "/download.mp4":
			_, _ = w.Write([]byte("fake-mp4-bytes"))
		default:
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
	}))
	defer server.Close()

	client := video.NewXAIOAuthClient(server.URL, "grok-imagine-video", stubVideoBearerTokenSource{token: "oauth-access"}, server.Client())
	_, err := client.GenerateVideo(context.Background(), " https://example.com/panel.png ", "make it move")
	if err != nil {
		t.Fatalf("GenerateVideo() error = %v", err)
	}

	image, ok := captured["image"].(map[string]any)
	if !ok {
		t.Fatalf("image payload = %#v, want object", captured["image"])
	}
	if image["url"] != "https://example.com/panel.png" {
		t.Fatalf("image.url = %q, want https://example.com/panel.png", image["url"])
	}
}

func TestXAIOAuthClient_GenerateVideo_TrimsPromptInRequest(t *testing.T) {
	t.Parallel()

	var captured map[string]any
	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/videos/generations":
			if err := json.NewDecoder(r.Body).Decode(&captured); err != nil {
				t.Fatalf("Decode() error = %v", err)
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"request_id": "req_123"})
		case "/v1/videos/req_123":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"status": "done",
				"video":  map[string]any{"url": server.URL + "/download.mp4"},
			})
		case "/download.mp4":
			_, _ = w.Write([]byte("fake-mp4-bytes"))
		default:
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
	}))
	defer server.Close()

	client := video.NewXAIOAuthClient(server.URL, "grok-imagine-video", stubVideoBearerTokenSource{token: "oauth-access"}, server.Client())
	_, err := client.GenerateVideo(context.Background(), "", " make it move ")
	if err != nil {
		t.Fatalf("GenerateVideo() error = %v", err)
	}
	if captured["prompt"] != "make it move" {
		t.Fatalf("prompt = %q, want make it move", captured["prompt"])
	}
}

func TestXAIOAuthClient_GenerateVideo_TrimsModelInRequest(t *testing.T) {
	t.Parallel()

	var captured map[string]any
	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/videos/generations":
			if err := json.NewDecoder(r.Body).Decode(&captured); err != nil {
				t.Fatalf("Decode() error = %v", err)
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"request_id": "req_123"})
		case "/v1/videos/req_123":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"status": "done",
				"video":  map[string]any{"url": server.URL + "/download.mp4"},
			})
		case "/download.mp4":
			_, _ = w.Write([]byte("fake-mp4-bytes"))
		default:
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
	}))
	defer server.Close()

	client := video.NewXAIOAuthClient(server.URL, " grok-imagine-video ", stubVideoBearerTokenSource{token: "oauth-access"}, server.Client())
	_, err := client.GenerateVideo(context.Background(), "", "make it move")
	if err != nil {
		t.Fatalf("GenerateVideo() error = %v", err)
	}
	if captured["model"] != "grok-imagine-video" {
		t.Fatalf("model = %q, want grok-imagine-video", captured["model"])
	}
}

func TestXAIOAuthClient_GenerateVideo_DownloadsReturnedURL(t *testing.T) {
	t.Parallel()

	videoServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("downloaded-mp4-bytes"))
	}))
	defer videoServer.Close()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/videos/generations":
			_ = json.NewEncoder(w).Encode(map[string]any{"request_id": "req_123"})
		case "/v1/videos/req_123":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"status": "done",
				"video":  map[string]any{"url": videoServer.URL},
			})
		default:
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
	}))
	defer server.Close()

	client := video.NewXAIOAuthClient(server.URL, "grok-imagine-video", stubVideoBearerTokenSource{token: "oauth-access"}, server.Client())
	got, err := client.GenerateVideo(context.Background(), "", "make it move")
	if err != nil {
		t.Fatalf("GenerateVideo() error = %v", err)
	}

	if string(got) != "downloaded-mp4-bytes" {
		t.Fatalf("GenerateVideo() = %q, want downloaded-mp4-bytes", string(got))
	}
}

func TestXAIOAuthClient_GenerateVideoWithOptionsResult_ReturnsRequestMetadata(t *testing.T) {
	t.Parallel()

	videoServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("downloaded-mp4-bytes"))
	}))
	defer videoServer.Close()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/videos/generations":
			_ = json.NewEncoder(w).Encode(map[string]any{"request_id": "req_meta_123"})
		case "/v1/videos/req_meta_123":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"status": "done",
				"video":  map[string]any{"url": videoServer.URL},
			})
		default:
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
	}))
	defer server.Close()

	client := video.NewXAIOAuthClient(server.URL, "grok-imagine-video", stubVideoBearerTokenSource{token: "oauth-access"}, server.Client())
	got, err := client.GenerateVideoWithOptionsResult(context.Background(), "", "make it move", video.GenerateVideoOptions{})
	if err != nil {
		t.Fatalf("GenerateVideoWithOptionsResult() error = %v", err)
	}

	if string(got.Data) != "downloaded-mp4-bytes" {
		t.Fatalf("Data = %q, want downloaded-mp4-bytes", string(got.Data))
	}
	if got.RequestID != "req_meta_123" {
		t.Fatalf("RequestID = %q, want req_meta_123", got.RequestID)
	}
	if got.Status != "done" {
		t.Fatalf("Status = %q, want done", got.Status)
	}
	if got.VideoURL != videoServer.URL {
		t.Fatalf("VideoURL = %q, want %q", got.VideoURL, videoServer.URL)
	}
}

func TestXAIOAuthClient_GenerateVideoWithOptionsResult_RejectsNonSuccessSubmitStatus(t *testing.T) {
	t.Parallel()

	var pollCalls atomic.Int32
	var downloadCalls atomic.Int32
	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/videos/generations":
			w.WriteHeader(http.StatusFound)
			_ = json.NewEncoder(w).Encode(map[string]any{"request_id": "req_submit_status_123"})
		case "/v1/videos/req_submit_status_123":
			pollCalls.Add(1)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"status": "done",
				"video":  map[string]any{"url": server.URL + "/download.mp4"},
			})
		case "/download.mp4":
			downloadCalls.Add(1)
			_, _ = w.Write([]byte("downloaded-mp4-bytes"))
		default:
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
	}))
	defer server.Close()

	client := video.NewXAIOAuthClient(server.URL, "grok-imagine-video", stubVideoBearerTokenSource{token: "oauth-access"}, server.Client())
	got, err := client.GenerateVideoWithOptionsResult(context.Background(), "", "make it move", video.GenerateVideoOptions{})
	if err == nil {
		t.Fatal("GenerateVideoWithOptionsResult() error = nil, want submit status error")
	}
	if got.Data != nil || got.RequestID != "" || got.Status != "" || got.VideoURL != "" {
		t.Fatalf("GenerateVideoWithOptionsResult() = %#v, want zero result for bad submit status", got)
	}
	if pollCalls.Load() != 0 {
		t.Fatalf("poll calls = %d, want 0", pollCalls.Load())
	}
	if downloadCalls.Load() != 0 {
		t.Fatalf("download calls = %d, want 0", downloadCalls.Load())
	}
	if !strings.Contains(err.Error(), "HTTP 302") {
		t.Fatalf("GenerateVideoWithOptionsResult() error = %q, want HTTP 302", err.Error())
	}
}

func TestXAIOAuthClient_GenerateVideoWithOptionsResult_DefaultClientRejectsSubmitRedirect(t *testing.T) {
	t.Parallel()

	var redirectCalls atomic.Int32
	var pollCalls atomic.Int32
	var downloadCalls atomic.Int32
	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/videos/generations":
			http.Redirect(w, r, "/v1/videos/generations/redirected", http.StatusFound)
		case "/v1/videos/generations/redirected":
			redirectCalls.Add(1)
			_ = json.NewEncoder(w).Encode(map[string]any{"request_id": "req_redirect_123"})
		case "/v1/videos/req_redirect_123":
			pollCalls.Add(1)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"status": "done",
				"video":  map[string]any{"url": server.URL + "/download.mp4"},
			})
		case "/download.mp4":
			downloadCalls.Add(1)
			_, _ = w.Write([]byte("downloaded-mp4-bytes"))
		default:
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
	}))
	defer server.Close()

	client := video.NewXAIOAuthClient(server.URL, "grok-imagine-video", stubVideoBearerTokenSource{token: "oauth-access"}, nil)
	got, err := client.GenerateVideoWithOptionsResult(context.Background(), "", "make it move", video.GenerateVideoOptions{})
	if err == nil {
		t.Fatal("GenerateVideoWithOptionsResult() error = nil, want submit redirect error")
	}
	if got.Data != nil || got.RequestID != "" || got.Status != "" || got.VideoURL != "" {
		t.Fatalf("GenerateVideoWithOptionsResult() = %#v, want zero result for submit redirect", got)
	}
	if redirectCalls.Load() != 0 {
		t.Fatalf("redirect calls = %d, want 0", redirectCalls.Load())
	}
	if pollCalls.Load() != 0 {
		t.Fatalf("poll calls = %d, want 0", pollCalls.Load())
	}
	if downloadCalls.Load() != 0 {
		t.Fatalf("download calls = %d, want 0", downloadCalls.Load())
	}
	if !strings.Contains(err.Error(), "HTTP 302") {
		t.Fatalf("GenerateVideoWithOptionsResult() error = %q, want HTTP 302", err.Error())
	}
}

func TestXAIOAuthClient_GenerateVideoWithOptionsResult_RejectsEmptyDownloadedVideo(t *testing.T) {
	t.Parallel()

	videoServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	defer videoServer.Close()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/videos/generations":
			_ = json.NewEncoder(w).Encode(map[string]any{"request_id": "req_empty_download_123"})
		case "/v1/videos/req_empty_download_123":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"status": "done",
				"video":  map[string]any{"url": videoServer.URL},
			})
		default:
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
	}))
	defer server.Close()

	client := video.NewXAIOAuthClient(server.URL, "grok-imagine-video", stubVideoBearerTokenSource{token: "oauth-access"}, server.Client())
	got, err := client.GenerateVideoWithOptionsResult(context.Background(), "", "make it move", video.GenerateVideoOptions{})
	if err == nil {
		t.Fatal("GenerateVideoWithOptionsResult() error = nil, want empty downloaded video error")
	}
	if got.Data != nil || got.RequestID != "" || got.Status != "" || got.VideoURL != "" {
		t.Fatalf("GenerateVideoWithOptionsResult() = %#v, want zero result for empty downloaded video", got)
	}
	if !strings.Contains(err.Error(), "empty") {
		t.Fatalf("GenerateVideoWithOptionsResult() error = %q, want empty video error", err.Error())
	}
}

func TestXAIOAuthClient_GenerateVideoWithOptionsResult_RejectsNonSuccessDownloadStatus(t *testing.T) {
	t.Parallel()

	videoServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusFound)
		_, _ = w.Write([]byte("not video bytes"))
	}))
	defer videoServer.Close()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/videos/generations":
			_ = json.NewEncoder(w).Encode(map[string]any{"request_id": "req_download_status_123"})
		case "/v1/videos/req_download_status_123":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"status": "done",
				"video":  map[string]any{"url": videoServer.URL},
			})
		default:
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
	}))
	defer server.Close()

	client := video.NewXAIOAuthClient(server.URL, "grok-imagine-video", stubVideoBearerTokenSource{token: "oauth-access"}, server.Client())
	got, err := client.GenerateVideoWithOptionsResult(context.Background(), "", "make it move", video.GenerateVideoOptions{})
	if err == nil {
		t.Fatal("GenerateVideoWithOptionsResult() error = nil, want download status error")
	}
	if got.Data != nil || got.RequestID != "" || got.Status != "" || got.VideoURL != "" {
		t.Fatalf("GenerateVideoWithOptionsResult() = %#v, want zero result for bad download status", got)
	}
	if !strings.Contains(err.Error(), "HTTP 302") {
		t.Fatalf("GenerateVideoWithOptionsResult() error = %q, want HTTP 302", err.Error())
	}
}

func TestXAIOAuthClient_GenerateVideoWithOptionsResult_TrimsRequestIDBeforePolling(t *testing.T) {
	t.Parallel()

	videoServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("downloaded-mp4-bytes"))
	}))
	defer videoServer.Close()

	var pollPath string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/videos/generations":
			_ = json.NewEncoder(w).Encode(map[string]any{"request_id": " req_trim_123 "})
		case "/v1/videos/req_trim_123":
			pollPath = r.URL.Path
			_ = json.NewEncoder(w).Encode(map[string]any{
				"status": "done",
				"video":  map[string]any{"url": videoServer.URL},
			})
		default:
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
	}))
	defer server.Close()

	client := video.NewXAIOAuthClient(server.URL, "grok-imagine-video", stubVideoBearerTokenSource{token: "oauth-access"}, server.Client())
	got, err := client.GenerateVideoWithOptionsResult(context.Background(), "", "make it move", video.GenerateVideoOptions{})
	if err != nil {
		t.Fatalf("GenerateVideoWithOptionsResult() error = %v", err)
	}
	if pollPath != "/v1/videos/req_trim_123" {
		t.Fatalf("poll path = %q, want /v1/videos/req_trim_123", pollPath)
	}
	if got.RequestID != "req_trim_123" {
		t.Fatalf("RequestID = %q, want req_trim_123", got.RequestID)
	}
}

func TestXAIOAuthClient_GenerateVideoWithOptionsResult_TrimsVideoURLBeforeDownloading(t *testing.T) {
	t.Parallel()

	videoServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("downloaded-mp4-bytes"))
	}))
	defer videoServer.Close()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/videos/generations":
			_ = json.NewEncoder(w).Encode(map[string]any{"request_id": "req_video_url_123"})
		case "/v1/videos/req_video_url_123":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"status": "done",
				"video":  map[string]any{"url": " " + videoServer.URL + " "},
			})
		default:
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
	}))
	defer server.Close()

	client := video.NewXAIOAuthClient(server.URL, "grok-imagine-video", stubVideoBearerTokenSource{token: "oauth-access"}, server.Client())
	got, err := client.GenerateVideoWithOptionsResult(context.Background(), "", "make it move", video.GenerateVideoOptions{})
	if err != nil {
		t.Fatalf("GenerateVideoWithOptionsResult() error = %v", err)
	}
	if string(got.Data) != "downloaded-mp4-bytes" {
		t.Fatalf("Data = %q, want downloaded-mp4-bytes", string(got.Data))
	}
	if got.VideoURL != videoServer.URL {
		t.Fatalf("VideoURL = %q, want %q", got.VideoURL, videoServer.URL)
	}
}

func TestXAIOAuthClient_GenerateVideoWithOptionsResult_CanonicalizesDoneStatus(t *testing.T) {
	t.Parallel()

	videoServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("downloaded-mp4-bytes"))
	}))
	defer videoServer.Close()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/videos/generations":
			_ = json.NewEncoder(w).Encode(map[string]any{"request_id": "req_status_123"})
		case "/v1/videos/req_status_123":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"status": " DONE ",
				"video":  map[string]any{"url": videoServer.URL},
			})
		default:
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
	}))
	defer server.Close()

	client := video.NewXAIOAuthClient(server.URL, "grok-imagine-video", stubVideoBearerTokenSource{token: "oauth-access"}, server.Client())
	got, err := client.GenerateVideoWithOptionsResult(context.Background(), "", "make it move", video.GenerateVideoOptions{})
	if err != nil {
		t.Fatalf("GenerateVideoWithOptionsResult() error = %v", err)
	}
	if got.Status != "done" {
		t.Fatalf("Status = %q, want done", got.Status)
	}
}

func TestXAIOAuthClient_GenerateVideoWithOptionsResult_RejectsNonSuccessPollStatus(t *testing.T) {
	t.Parallel()

	var downloadCalls atomic.Int32
	videoServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		downloadCalls.Add(1)
		_, _ = w.Write([]byte("downloaded-mp4-bytes"))
	}))
	defer videoServer.Close()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/videos/generations":
			_ = json.NewEncoder(w).Encode(map[string]any{"request_id": "req_poll_status_123"})
		case "/v1/videos/req_poll_status_123":
			w.WriteHeader(http.StatusFound)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"status": "done",
				"video":  map[string]any{"url": videoServer.URL},
			})
		default:
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
	}))
	defer server.Close()

	client := video.NewXAIOAuthClient(server.URL, "grok-imagine-video", stubVideoBearerTokenSource{token: "oauth-access"}, server.Client())
	got, err := client.GenerateVideoWithOptionsResult(context.Background(), "", "make it move", video.GenerateVideoOptions{})
	if err == nil {
		t.Fatal("GenerateVideoWithOptionsResult() error = nil, want poll status error")
	}
	if got.Data != nil || got.RequestID != "" || got.Status != "" || got.VideoURL != "" {
		t.Fatalf("GenerateVideoWithOptionsResult() = %#v, want zero result for bad poll status", got)
	}
	if downloadCalls.Load() != 0 {
		t.Fatalf("download calls = %d, want 0", downloadCalls.Load())
	}
	if !strings.Contains(err.Error(), "HTTP 302") {
		t.Fatalf("GenerateVideoWithOptionsResult() error = %q, want HTTP 302", err.Error())
	}
}

func TestXAIOAuthClient_GenerateVideoWithOptionsResult_RejectsTerminalGenerationStatuses(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		status       string
		errorMessage string
		message      string
		wantStatus   string
		wantMessage  string
	}{
		{
			name:         "failed status with error message",
			status:       " FAILED ",
			errorMessage: " policy blocked ",
			wantStatus:   "failed",
			wantMessage:  "policy blocked",
		},
		{
			name:        "error status with top-level message",
			status:      "error",
			message:     "provider unavailable",
			wantStatus:  "error",
			wantMessage: "provider unavailable",
		},
		{
			name:        "expired status falls back to status",
			status:      "expired",
			wantStatus:  "expired",
			wantMessage: "expired",
		},
		{
			name:        "cancelled status falls back to status",
			status:      "cancelled",
			wantStatus:  "cancelled",
			wantMessage: "cancelled",
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			var downloadCalls atomic.Int32
			var server *httptest.Server
			server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case "/v1/videos/generations":
					_ = json.NewEncoder(w).Encode(map[string]any{"request_id": "req_terminal_123"})
				case "/v1/videos/req_terminal_123":
					response := map[string]any{
						"status": tt.status,
						"video":  map[string]any{"url": server.URL + "/download.mp4"},
					}
					if tt.errorMessage != "" {
						response["error"] = map[string]any{"message": tt.errorMessage}
					}
					if tt.message != "" {
						response["message"] = tt.message
					}
					_ = json.NewEncoder(w).Encode(response)
				case "/download.mp4":
					downloadCalls.Add(1)
					http.Error(w, "terminal status must not download video", http.StatusInternalServerError)
				default:
					t.Fatalf("unexpected path: %s", r.URL.Path)
				}
			}))
			defer server.Close()

			client := video.NewXAIOAuthClient(server.URL, "grok-imagine-video", stubVideoBearerTokenSource{token: "oauth-access"}, server.Client())
			got, err := client.GenerateVideoWithOptionsResult(context.Background(), "", "make it move", video.GenerateVideoOptions{})
			if err == nil {
				t.Fatal("GenerateVideoWithOptionsResult() error = nil, want terminal status error")
			}
			if got.Data != nil || got.RequestID != "" || got.Status != "" || got.VideoURL != "" {
				t.Fatalf("GenerateVideoWithOptionsResult() = %#v, want zero result on terminal status", got)
			}
			if downloadCalls.Load() != 0 {
				t.Fatalf("download calls = %d, want 0", downloadCalls.Load())
			}
			if !strings.Contains(err.Error(), `status "`+tt.wantStatus+`"`) {
				t.Fatalf("GenerateVideoWithOptionsResult() error = %q, want status %q", err.Error(), tt.wantStatus)
			}
			if !strings.Contains(err.Error(), tt.wantMessage) {
				t.Fatalf("GenerateVideoWithOptionsResult() error = %q, want message %q", err.Error(), tt.wantMessage)
			}
		})
	}
}

func TestXAIOAuthClient_GenerateVideoWithOptions_UsesRequestedGenerationOptions(t *testing.T) {
	t.Parallel()

	var captured map[string]any
	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/videos/generations":
			if err := json.NewDecoder(r.Body).Decode(&captured); err != nil {
				t.Fatalf("Decode() error = %v", err)
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"request_id": "req_123"})
		case "/v1/videos/req_123":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"status": "done",
				"video":  map[string]any{"url": server.URL + "/download.mp4"},
			})
		case "/download.mp4":
			_, _ = w.Write([]byte("fake-mp4-bytes"))
		default:
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
	}))
	defer server.Close()

	client := video.NewXAIOAuthClient(server.URL, "grok-imagine-video", stubVideoBearerTokenSource{token: "oauth-access"}, server.Client())
	_, err := client.GenerateVideoWithOptions(context.Background(), "", "custom shot", video.GenerateVideoOptions{
		DurationSec: 6.5,
		AspectRatio: "16:9",
		Resolution:  "1080p",
	})
	if err != nil {
		t.Fatalf("GenerateVideoWithOptions() error = %v", err)
	}

	if captured["duration"] != 6.5 {
		t.Fatalf("duration = %#v, want 6.5", captured["duration"])
	}
	if captured["aspect_ratio"] != "16:9" {
		t.Fatalf("aspect_ratio = %#v, want 16:9", captured["aspect_ratio"])
	}
	if captured["resolution"] != "1080p" {
		t.Fatalf("resolution = %#v, want 1080p", captured["resolution"])
	}
}
