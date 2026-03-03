package llm

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/go-resty/resty/v2"
)

// GeminiClient connects (via a Zeabur proxy or OpenAI-compatible endpoint) 
// to generate text output for our pipeline steps.
type GeminiClient struct {
	client *resty.Client
	apiKey string
	model  string
}

// NewGeminiClient handles exponential backoff and sets up resty.
func NewGeminiClient(baseURL, apiKey, model string) *GeminiClient {
	if baseURL == "" {
		baseURL = "https://pgb.zeabur.app/v1"
	}
	if model == "" {
		model = "gemini-2.5-pro"
	}

	r := resty.New().
		SetBaseURL(baseURL).
		SetTimeout(120 * time.Second).
		SetRetryCount(3).
		SetRetryWaitTime(2 * time.Second).
		SetRetryMaxWaitTime(10 * time.Second)

	return &GeminiClient{
		client: r,
		apiKey: apiKey,
		model:  model,
	}
}

// GenerateTransformation hits a standard Chat Completions endpoint.
func (c *GeminiClient) GenerateTransformation(ctx context.Context, systemPrompt string, inputData []byte) ([]byte, error) {
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
		return nil, fmt.Errorf("http request failed: %w", err)
	}

	if resp.IsError() {
		errMsg := "unknown API error"
		if resBody.Error != nil && resBody.Error.Message != "" {
			errMsg = resBody.Error.Message
		}
		return nil, fmt.Errorf("API error (status %d): %s", resp.StatusCode(), errMsg)
	}

	if len(resBody.Choices) == 0 || resBody.Choices[0].Message.Content == "" {
		return nil, errors.New("API returned empty choices or content")
	}

	return []byte(resBody.Choices[0].Message.Content), nil
}
