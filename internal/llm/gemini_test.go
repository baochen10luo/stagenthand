package llm_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/baochen10luo/stagenthand/internal/llm"
	"github.com/stretchr/testify/assert"
)

func TestGeminiClient_GenerateTransformation(t *testing.T) {
	t.Parallel()

	dummyJSON := `{"outline": true}`

	t.Run("success", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			assert.Equal(t, "/chat/completions", r.URL.Path)
			assert.Equal(t, "Bearer test-key", r.Header.Get("Authorization"))

			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(`{
				"choices": [
					{
						"message": {
							"role": "assistant",
							"content": "{\"outline\": true}"
						}
					}
				]
			}`))
		}))
		defer server.Close()

		client := llm.NewGeminiClient(server.URL, "test-key", "gemini-2.5-pro")

		ctx := context.Background()
		res, err := client.GenerateTransformation(ctx, "SysPrompt", []byte(`input`))

		assert.NoError(t, err)
		assert.Equal(t, []byte(dummyJSON), res)
	})

	t.Run("api error 500", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusInternalServerError)
			w.Write([]byte(`{"error": {"message": "internal server error"}}`))
		}))
		defer server.Close()

		client := llm.NewGeminiClient(server.URL, "key", "model")
		_, err := client.GenerateTransformation(context.Background(), "sys", []byte("in"))
		assert.ErrorContains(t, err, "API error")
		assert.ErrorContains(t, err, "internal server error")
	})

	t.Run("api error without message body", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusUnauthorized)
			w.Write([]byte(`{}`))
		}))
		defer server.Close()

		client := llm.NewGeminiClient(server.URL, "bad-key", "model")
		_, err := client.GenerateTransformation(context.Background(), "sys", []byte("in"))
		assert.ErrorContains(t, err, "unknown API error")
	})

	t.Run("empty choices", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(`{"choices": []}`))
		}))
		defer server.Close()

		client := llm.NewGeminiClient(server.URL, "key", "model")
		_, err := client.GenerateTransformation(context.Background(), "sys", []byte("in"))
		assert.ErrorContains(t, err, "empty choices")
	})

	t.Run("http connection failure", func(t *testing.T) {
		// Use port 0 to guarantee immediate connection refused.
		client := llm.NewGeminiClient("http://127.0.0.1:0", "key", "model")
		_, err := client.GenerateTransformation(context.Background(), "sys", []byte("in"))
		assert.ErrorContains(t, err, "http request failed")
	})
}

func TestNewGeminiClient_Defaults(t *testing.T) {
	t.Parallel()

	// When baseURL and model are empty, NewGeminiClient should apply defaults.
	// We verify the client is constructed (non-nil) without panicking.
	client := llm.NewGeminiClient("", "", "")
	assert.NotNil(t, client)
}
