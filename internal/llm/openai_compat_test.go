package llm_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/baochen10luo/stagenthand/internal/llm"
	"github.com/stretchr/testify/assert"
)

func TestOpenAICompatibleClient_GenerateTransformation(t *testing.T) {
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

		client := llm.NewOpenAICompatibleClient(server.URL, "test-key", "gemini-2.5-pro")
		ctx := context.Background()
		res, err := client.GenerateTransformation(ctx, "SysPrompt", []byte(`input`))

		assert.NoError(t, err)
		assert.Equal(t, []byte(dummyJSON), res)
	})

	t.Run("api error 400", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusBadRequest)
			w.Write([]byte(`{"error": {"message": "bad request"}}`))
		}))
		defer server.Close()

		client := llm.NewOpenAICompatibleClient(server.URL, "test-key", "gemini-2.5-pro")
		_, err := client.GenerateTransformation(context.Background(), "", nil)
		assert.ErrorContains(t, err, "bad request")
	})

	t.Run("empty choices", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(`{"choices": []}`))
		}))
		defer server.Close()

		client := llm.NewOpenAICompatibleClient(server.URL, "test-key", "gemini-2.5-pro")
		_, err := client.GenerateTransformation(context.Background(), "", nil)
		assert.ErrorContains(t, err, "empty choices")
	})

	t.Run("http connection failure", func(t *testing.T) {
		client := llm.NewOpenAICompatibleClient("http://127.0.0.1:0", "test-key", "model")
		_, err := client.GenerateTransformation(context.Background(), "", nil)
		assert.ErrorContains(t, err, "http request failed")
	})

	t.Run("api error without message body returns unknown", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusUnauthorized)
			w.Write([]byte(`{}`))
		}))
		defer server.Close()

		client := llm.NewOpenAICompatibleClient(server.URL, "bad-key", "model")
		_, err := client.GenerateTransformation(context.Background(), "", nil)
		assert.ErrorContains(t, err, "unknown API error")
	})
}

func TestNewOpenAICompatibleClient_Defaults(t *testing.T) {
	t.Parallel()

	// When baseURL and model are empty, defaults should be applied and the client should be non-nil.
	client := llm.NewOpenAICompatibleClient("", "", "")
	assert.NotNil(t, client)
}
