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
}
