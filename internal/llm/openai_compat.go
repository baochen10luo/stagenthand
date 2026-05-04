package llm

import (
	"context"
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/go-resty/resty/v2"
)

// OpenAICompatibleClient connects (via a proxy or standard endpoint)
// to generate text output for our pipeline steps.
type OpenAICompatibleClient struct {
	client *resty.Client
	apiKey string
	model  string
}

// NewOpenAICompatibleClient handles exponential backoff and sets up resty.
func NewOpenAICompatibleClient(baseURL, apiKey, model string) *OpenAICompatibleClient {
	return NewOpenAICompatibleClientWithHeaders(baseURL, apiKey, model, nil)
}

// NewOpenAICompatibleClientWithHeaders creates a client with additional request headers.
func NewOpenAICompatibleClientWithHeaders(baseURL, apiKey, model string, extraHeaders map[string]string) *OpenAICompatibleClient {
	if baseURL == "" {
		baseURL = "https://pgb.zeabur.app/v1"
	}
	if model == "" {
		model = "gemini-2.5-pro"
	}

	r := resty.New().
		SetBaseURL(baseURL).
		SetTimeout(1800 * time.Second)

	for k, v := range extraHeaders {
		r.SetHeader(k, v)
	}

	return &OpenAICompatibleClient{
		client: r,
		apiKey: apiKey,
		model:  model,
	}
}

const (
	llmRetryMax   = 24
	llmRetryDelay = 10 * time.Second
)

// GenerateTransformation hits a standard Chat Completions endpoint.
// On 5xx responses (e.g. GPU VRAM full), retries up to llmRetryMax times
// with llmRetryDelay between attempts.
func (c *OpenAICompatibleClient) GenerateTransformation(ctx context.Context, systemPrompt string, inputData []byte) ([]byte, error) {
	type Message struct {
		Role    string `json:"role"`
		Content string `json:"content"`
	}

	type ChatRequest struct {
		Model          string    `json:"model"`
		ResponseFormat *struct {
			Type string `json:"type"`
		} `json:"response_format,omitempty"`
		Messages []Message `json:"messages"`
	}

	type ChatResponse struct {
		Choices []struct {
			Message Message `json:"message"`
		} `json:"choices"`
		Error *struct {
			Message string `json:"message"`
		} `json:"error,omitempty"`
	}

	reqBody := ChatRequest{
		Model: c.model,
		ResponseFormat: &struct {
			Type string `json:"type"`
		}{Type: "json_object"},
		Messages: []Message{
			{Role: "system", Content: systemPrompt},
			{Role: "user", Content: string(inputData)},
		},
	}

	var lastErr error
	for attempt := 0; attempt <= llmRetryMax; attempt++ {
		var resBody ChatResponse
		req := c.client.R().
			SetContext(ctx).
			SetHeader("Authorization", "Bearer "+c.apiKey).
			SetHeader("Content-Type", "application/json").
			SetBody(reqBody).
			SetResult(&resBody).
			SetError(&resBody)

		resp, err := req.Post("/chat/completions")
		if err != nil {
			lastErr = fmt.Errorf("http request failed: %w", err)
			if attempt < llmRetryMax {
				fmt.Fprintf(os.Stderr, "[Info] LLM request error，%s 後重試 (%d/%d): %v\n", llmRetryDelay, attempt+1, llmRetryMax, err)
				select {
				case <-ctx.Done():
					return nil, ctx.Err()
				case <-time.After(llmRetryDelay):
				}
			}
			continue
		}

		if resp.IsError() {
			errMsg := "unknown API error"
			if resBody.Error != nil && resBody.Error.Message != "" {
				errMsg = resBody.Error.Message
			}
			lastErr = fmt.Errorf("API error (status %d): %s", resp.StatusCode(), errMsg)
			// Retry on 5xx and 409 (GPU busy); fail immediately on other 4xx.
			if resp.StatusCode() < 500 && resp.StatusCode() != 409 {
				return nil, lastErr
			}
			if attempt < llmRetryMax {
				label := "5xx"
				if resp.StatusCode() == 409 {
					label = "GPU busy (409)"
				}
				fmt.Fprintf(os.Stderr, "[Info] LLM %s，%s 後重試 (%d/%d)...\n", label, llmRetryDelay, attempt+1, llmRetryMax)
				select {
				case <-ctx.Done():
					return nil, ctx.Err()
				case <-time.After(llmRetryDelay):
				}
			}
			continue
		}

		if len(resBody.Choices) == 0 || resBody.Choices[0].Message.Content == "" {
			return nil, errors.New("API returned empty choices or content")
		}

		return []byte(resBody.Choices[0].Message.Content), nil
	}

	return nil, fmt.Errorf("LLM 重試 %d 次仍失敗: %w", llmRetryMax, lastErr)
}
