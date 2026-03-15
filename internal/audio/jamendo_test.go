package audio

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

func TestJamendoClient_SearchAndDownload(t *testing.T) {
	// 1. Create a fake Jamendo API server
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Check the URL path to distinguish between search and download
		if r.URL.Path == "/search/" {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(`{
				"headers": {"status": "success", "code": 0},
				"results": [{"id": "123", "name": "Fake Track", "audio": "http://` + r.Host + `/download/123.mp3"}]
			}`))
		} else if r.URL.Path == "/download/123.mp3" {
			w.Header().Set("Content-Type", "audio/mpeg")
			w.WriteHeader(http.StatusOK)
			w.Write([]byte("fake-mp3-music"))
		} else {
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer ts.Close()

	client := NewJamendoClient("test-key")
	client.baseURL = ts.URL + "/search/"

	ctx := context.Background()
	data, err := client.SearchAndDownload(ctx, "happy")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if string(data) != "fake-mp3-music" {
		t.Errorf("got %q, want %q", string(data), "fake-mp3-music")
	}
}

func TestJamendoClient_NoResults(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{
			"headers": {"status": "success", "code": 0},
			"results": []
		}`))
	}))
	defer ts.Close()

	client := NewJamendoClient("test-key")
	client.baseURL = ts.URL

	_, err := client.SearchAndDownload(context.Background(), "notfound")
	if err == nil {
		t.Error("expected error for no results, got nil")
	}
}

func TestJamendoClient_APIError(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer ts.Close()

	client := NewJamendoClient("test-key")
	client.baseURL = ts.URL

	_, err := client.SearchAndDownload(context.Background(), "error")
	if err == nil {
		t.Error("expected error for non-200 status, got nil")
	}
}

// TestSearchAndDownload_MultiTagFallback: 全 tag 搜尋回傳空，fallback 第一個單 tag 成功
func TestSearchAndDownload_MultiTagFallback(t *testing.T) {
	var searchCount atomic.Int32

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/search/") {
			w.Header().Set("Content-Type", "application/json")
			tags := r.URL.Query().Get("tags")
			n := searchCount.Add(1)
			// 第一次（多 tag）回傳空；第二次（單 tag "space"）回傳一筆
			if n == 1 || tags != "space" {
				w.Write([]byte(`{"headers":{"status":"success","code":0},"results":[]}`))
			} else {
				w.Write([]byte(`{"headers":{"status":"success","code":0},"results":[{"id":"1","name":"Space Track","audio":"http://` + r.Host + `/dl/1.mp3"}]}`))
			}
		} else if r.URL.Path == "/dl/1.mp3" {
			w.Header().Set("Content-Type", "audio/mpeg")
			w.Write([]byte("space-music-bytes"))
		} else {
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer ts.Close()

	client := NewJamendoClient("test-key")
	client.baseURL = ts.URL + "/search/"

	data, err := client.SearchAndDownload(context.Background(), "space+adventure+romance+suspense")
	if err != nil {
		t.Fatalf("expected success via fallback, got error: %v", err)
	}
	if string(data) != "space-music-bytes" {
		t.Errorf("got %q, want %q", string(data), "space-music-bytes")
	}
	if searchCount.Load() < 2 {
		t.Errorf("expected at least 2 search requests (multi-tag + single-tag fallback), got %d", searchCount.Load())
	}
}

// TestSearchAndDownload_AllTagsFail_FallbackCinematic: 所有 tag 失敗，最終 "cinematic" fallback 成功
func TestSearchAndDownload_AllTagsFail_FallbackCinematic(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/search/") {
			w.Header().Set("Content-Type", "application/json")
			tags := r.URL.Query().Get("tags")
			if tags == "cinematic" {
				w.Write([]byte(`{"headers":{"status":"success","code":0},"results":[{"id":"99","name":"Cinematic","audio":"http://` + r.Host + `/dl/99.mp3"}]}`))
			} else {
				w.Write([]byte(`{"headers":{"status":"success","code":0},"results":[]}`))
			}
		} else if r.URL.Path == "/dl/99.mp3" {
			w.Header().Set("Content-Type", "audio/mpeg")
			w.Write([]byte("cinematic-music-bytes"))
		} else {
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer ts.Close()

	client := NewJamendoClient("test-key")
	client.baseURL = ts.URL + "/search/"

	data, err := client.SearchAndDownload(context.Background(), "foo+bar+baz")
	if err != nil {
		t.Fatalf("expected success via cinematic fallback, got error: %v", err)
	}
	if string(data) != "cinematic-music-bytes" {
		t.Errorf("got %q, want %q", string(data), "cinematic-music-bytes")
	}
}

// TestSearchAndDownload_FirstAttemptSuccess: 第一次多 tag 搜尋成功，只發一次 search 請求
func TestSearchAndDownload_FirstAttemptSuccess(t *testing.T) {
	var searchCount atomic.Int32

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/search/") {
			searchCount.Add(1)
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte(`{"headers":{"status":"success","code":0},"results":[{"id":"7","name":"Multi Track","audio":"http://` + r.Host + `/dl/7.mp3"}]}`))
		} else if r.URL.Path == "/dl/7.mp3" {
			w.Header().Set("Content-Type", "audio/mpeg")
			w.Write([]byte("multi-tag-music"))
		} else {
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer ts.Close()

	client := NewJamendoClient("test-key")
	client.baseURL = ts.URL + "/search/"

	data, err := client.SearchAndDownload(context.Background(), "happy+upbeat+energetic")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if string(data) != "multi-tag-music" {
		t.Errorf("got %q, want %q", string(data), "multi-tag-music")
	}
	if n := searchCount.Load(); n != 1 {
		t.Errorf("expected exactly 1 search request, got %d", n)
	}
}

// TestSearchAndDownload_AllFail: 所有嘗試都失敗，回傳包含 "no tracks found" 的 error
func TestSearchAndDownload_AllFail(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/search/") {
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte(`{"headers":{"status":"success","code":0},"results":[]}`))
		} else {
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer ts.Close()

	client := NewJamendoClient("test-key")
	client.baseURL = ts.URL + "/search/"

	_, err := client.SearchAndDownload(context.Background(), "alpha+beta+gamma")
	if err == nil {
		t.Fatal("expected error when all tags fail, got nil")
	}
	if !strings.Contains(err.Error(), "no tracks found") {
		t.Errorf("error message should contain 'no tracks found', got: %v", err)
	}
}
