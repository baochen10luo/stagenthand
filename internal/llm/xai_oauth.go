package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

const defaultXAIBaseURL = "https://api.x.ai/v1"

// BearerTokenSource abstracts OAuth token retrieval.
type BearerTokenSource interface {
	BearerToken(ctx context.Context) (string, error)
}

// HTTPDoer allows the client to be tested with httptest.Server.
type HTTPDoer interface {
	Do(req *http.Request) (*http.Response, error)
}

// XAIOAuthClient speaks the xAI /v1/responses API with an OAuth bearer token.
type XAIOAuthClient struct {
	baseURL     string
	model       string
	tokenSource BearerTokenSource
	client      HTTPDoer
}

// NewXAIOAuthClient creates an xAI OAuth client.
func NewXAIOAuthClient(baseURL, model string, tokenSource BearerTokenSource, client HTTPDoer) *XAIOAuthClient {
	baseURL = strings.TrimSpace(baseURL)
	model = strings.TrimSpace(model)
	if baseURL == "" {
		baseURL = defaultXAIBaseURL
	}
	if model == "" {
		model = "grok-4.3"
	}
	if client == nil {
		client = newXAIHTTPClient()
	}
	return &XAIOAuthClient{
		baseURL:     baseURL,
		model:       model,
		tokenSource: tokenSource,
		client:      client,
	}
}

func newXAIHTTPClient() *http.Client {
	return &http.Client{
		Timeout: 60 * time.Second,
		CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
}

// BuildXAIResponsesURL normalizes the xAI base URL to the /v1/responses endpoint.
func BuildXAIResponsesURL(baseURL string) string {
	base := strings.TrimRight(strings.TrimSpace(baseURL), "/")
	if base == "" {
		base = defaultXAIBaseURL
	}
	for _, suffix := range []string{"/v1/videos/generations", "/v1/videos"} {
		if strings.HasSuffix(base, suffix) {
			base = strings.TrimSuffix(base, suffix) + "/v1"
			break
		}
	}
	if strings.HasSuffix(base, "/responses") {
		return base
	}
	if strings.HasSuffix(base, "/v1") {
		return base + "/responses"
	}
	return base + "/v1/responses"
}

// GenerateTransformation implements Client.
func (c *XAIOAuthClient) GenerateTransformation(ctx context.Context, systemPrompt string, inputData []byte) ([]byte, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if c.tokenSource == nil {
		return nil, errors.New("xai oauth token source is nil")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	token, err := c.tokenSource.BearerToken(ctx)
	if err != nil {
		return nil, fmt.Errorf("xai oauth bearer token: %w", err)
	}
	token = strings.TrimSpace(token)
	if token == "" {
		return nil, errors.New("xai oauth bearer token is empty")
	}

	type request struct {
		Model        string `json:"model"`
		Instructions string `json:"instructions,omitempty"`
		Input        []any  `json:"input"`
	}
	body := request{
		Model:        c.model,
		Instructions: strings.TrimSpace(systemPrompt),
		Input: []any{
			map[string]any{
				"role":    "user",
				"content": string(inputData),
			},
		},
	}

	reqBody, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, BuildXAIResponsesURL(c.baseURL), bytes.NewReader(reqBody))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")

	resp, err := c.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("xai oauth request failed: %w", err)
	}
	defer resp.Body.Close()
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		msg := xaiResponseErrorMessage(raw)
		if msg == "" {
			msg = http.StatusText(resp.StatusCode)
		}
		if resp.StatusCode == http.StatusForbidden {
			return nil, &XAIEntitlementError{StatusCode: resp.StatusCode, Message: msg}
		}
		return nil, fmt.Errorf("xAI OAuth request failed with HTTP %d: %s", resp.StatusCode, msg)
	}

	content, err := xaiResponseText(raw)
	if err != nil {
		return nil, err
	}
	content = stripThinkTags(strings.TrimSpace(content))
	content = stripMarkdownFence(content)
	content = strings.TrimSpace(content)
	if content == "" {
		return nil, errors.New("xAI response was empty after cleanup")
	}
	return []byte(content), nil
}

// XAIEntitlementError indicates the account is authenticated but not allowed
// to access the requested xAI surface or model.
type XAIEntitlementError struct {
	StatusCode int
	Message    string
}

func (e *XAIEntitlementError) Error() string {
	if e.Message == "" {
		return "xAI OAuth request was forbidden; subscription tier or entitlement may not allow this model/API surface"
	}
	return fmt.Sprintf("xAI OAuth request was forbidden: %s (check subscription tier or OAuth entitlement)", e.Message)
}

type xaiResponsesResponse struct {
	OutputText string `json:"output_text"`
	Output     []struct {
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
	} `json:"output"`
}

func xaiResponseText(raw []byte) (string, error) {
	var resp xaiResponsesResponse
	if err := json.Unmarshal(raw, &resp); err != nil {
		return "", err
	}
	if strings.TrimSpace(resp.OutputText) != "" {
		return resp.OutputText, nil
	}
	for _, output := range resp.Output {
		for _, content := range output.Content {
			if content.Type == "output_text" && content.Text != "" {
				return content.Text, nil
			}
		}
	}
	return "", errors.New("xAI response did not include output_text content")
}

func xaiResponseErrorMessage(raw []byte) string {
	var payload struct {
		Error struct {
			Message string `json:"message"`
			Code    string `json:"code"`
		} `json:"error"`
	}
	if err := json.Unmarshal(raw, &payload); err == nil {
		if payload.Error.Message != "" {
			return payload.Error.Message
		}
		if payload.Error.Code != "" {
			return payload.Error.Code
		}
	}
	return strings.TrimSpace(string(raw))
}
