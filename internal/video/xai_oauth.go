package video

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const (
	defaultXAIBaseURL    = "https://api.x.ai/v1"
	defaultXAIVideoModel = "grok-imagine-video"
	defaultXAIDuration   = 8
	defaultXAIAspect     = "9:16"
	defaultXAIResolution = "720p"
	defaultXAIPollEvery  = 5 * time.Second
	defaultXAIPollMax    = 4 * time.Minute
)

type BearerTokenSource interface {
	BearerToken(ctx context.Context) (string, error)
}

type HTTPDoer interface {
	Do(req *http.Request) (*http.Response, error)
}

type GenerateVideoOptions struct {
	DurationSec float64
	AspectRatio string
	Resolution  string
}

type GenerateVideoResult struct {
	Data      []byte
	RequestID string
	Status    string
	VideoURL  string
}

// XAIOAuthClient generates per-panel video shots using a Hermes xAI OAuth token.
type XAIOAuthClient struct {
	baseURL     string
	model       string
	tokenSource BearerTokenSource
	client      HTTPDoer
}

func NewXAIOAuthClient(baseURL, model string, tokenSource BearerTokenSource, client HTTPDoer) *XAIOAuthClient {
	model = strings.TrimSpace(model)
	if model == "" {
		model = defaultXAIVideoModel
	}
	if client == nil {
		client = newXAIVideoHTTPClient()
	}
	return &XAIOAuthClient{
		baseURL:     normalizeXAIVideoBaseURL(baseURL),
		model:       model,
		tokenSource: tokenSource,
		client:      client,
	}
}

func newXAIVideoHTTPClient() *http.Client {
	return &http.Client{
		Timeout: 2 * time.Minute,
		CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
}

func normalizeXAIVideoBaseURL(baseURL string) string {
	base := strings.TrimRight(strings.TrimSpace(baseURL), "/")
	if base == "" {
		base = defaultXAIBaseURL
	}
	for _, suffix := range []string{"/v1/videos/generations", "/v1/videos", "/v1/responses"} {
		if strings.HasSuffix(base, suffix) {
			return strings.TrimSuffix(base, suffix) + "/v1"
		}
	}
	if strings.HasSuffix(base, "/v1") {
		return base
	}
	return base + "/v1"
}

func (c *XAIOAuthClient) GenerateVideo(ctx context.Context, imageURL string, prompt string) ([]byte, error) {
	return c.GenerateVideoWithOptions(ctx, imageURL, prompt, GenerateVideoOptions{})
}

func (c *XAIOAuthClient) GenerateVideoWithOptions(ctx context.Context, imageURL string, prompt string, options GenerateVideoOptions) ([]byte, error) {
	result, err := c.GenerateVideoWithOptionsResult(ctx, imageURL, prompt, options)
	if err != nil {
		return nil, err
	}
	return result.Data, nil
}

func (c *XAIOAuthClient) GenerateVideoWithOptionsResult(ctx context.Context, imageURL string, prompt string, options GenerateVideoOptions) (GenerateVideoResult, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	prompt = strings.TrimSpace(prompt)
	if prompt == "" {
		return GenerateVideoResult{}, errors.New("xai oauth video prompt is empty")
	}
	if c.tokenSource == nil {
		return GenerateVideoResult{}, errors.New("xai oauth video token source is nil")
	}
	if err := ctx.Err(); err != nil {
		return GenerateVideoResult{}, err
	}
	token, err := c.tokenSource.BearerToken(ctx)
	if err != nil {
		return GenerateVideoResult{}, fmt.Errorf("xai oauth video bearer token: %w", err)
	}
	token = strings.TrimSpace(token)
	if token == "" {
		return GenerateVideoResult{}, errors.New("xai oauth video bearer token is empty")
	}

	reqBody := map[string]any{
		"model":        c.model,
		"prompt":       prompt,
		"duration":     xaiDuration(options.DurationSec),
		"aspect_ratio": xaiAspectRatio(options.AspectRatio),
		"resolution":   xaiResolution(options.Resolution),
	}
	imageURL = strings.TrimSpace(imageURL)
	if imageURL != "" {
		reqBody["image"] = map[string]string{"url": imageURL}
	}

	bodyData, err := json.Marshal(reqBody)
	if err != nil {
		return GenerateVideoResult{}, fmt.Errorf("marshal xai oauth video request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/videos/generations", bytes.NewReader(bodyData))
	if err != nil {
		return GenerateVideoResult{}, fmt.Errorf("create xai oauth video request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("X-Idempotency-Key", fmt.Sprintf("shand-%d", time.Now().UnixNano()))

	resp, err := c.client.Do(req)
	if err != nil {
		return GenerateVideoResult{}, fmt.Errorf("xai oauth video request failed: %w", err)
	}
	defer resp.Body.Close()
	if err := ctx.Err(); err != nil {
		return GenerateVideoResult{}, err
	}

	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return GenerateVideoResult{}, err
	}
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return GenerateVideoResult{}, fmt.Errorf("xai oauth video request failed with HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(raw)))
	}

	var res struct {
		RequestID string `json:"request_id"`
	}
	if err := json.Unmarshal(raw, &res); err != nil {
		return GenerateVideoResult{}, fmt.Errorf("decode xai oauth video response: %w", err)
	}
	requestID := strings.TrimSpace(res.RequestID)
	if requestID == "" {
		return GenerateVideoResult{}, errors.New("xai oauth video response did not include request_id")
	}

	poll, err := c.pollVideoURL(ctx, token, requestID)
	if err != nil {
		return GenerateVideoResult{}, err
	}
	if err := ctx.Err(); err != nil {
		return GenerateVideoResult{}, err
	}
	data, err := c.downloadVideo(ctx, poll.URL)
	if err != nil {
		return GenerateVideoResult{}, err
	}
	return GenerateVideoResult{
		Data:      data,
		RequestID: requestID,
		Status:    poll.Status,
		VideoURL:  poll.URL,
	}, nil
}

func xaiDuration(durationSec float64) float64 {
	if durationSec <= 0 {
		return defaultXAIDuration
	}
	return durationSec
}

func xaiAspectRatio(aspectRatio string) string {
	if strings.TrimSpace(aspectRatio) == "" {
		return defaultXAIAspect
	}
	return strings.TrimSpace(aspectRatio)
}

func xaiResolution(resolution string) string {
	if strings.TrimSpace(resolution) == "" {
		return defaultXAIResolution
	}
	return strings.TrimSpace(resolution)
}

type xaiPollResult struct {
	URL    string
	Status string
}

func (c *XAIOAuthClient) pollVideoURL(ctx context.Context, token string, requestID string) (xaiPollResult, error) {
	deadline := time.Now().Add(defaultXAIPollMax)
	for {
		if err := ctx.Err(); err != nil {
			return xaiPollResult{}, err
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/videos/"+url.PathEscape(requestID), nil)
		if err != nil {
			return xaiPollResult{}, fmt.Errorf("create xai oauth video poll request: %w", err)
		}
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("Accept", "application/json")

		resp, err := c.client.Do(req)
		if err != nil {
			return xaiPollResult{}, fmt.Errorf("xai oauth video poll failed: %w", err)
		}
		if ctxErr := ctx.Err(); ctxErr != nil {
			if closeErr := resp.Body.Close(); closeErr != nil {
				return xaiPollResult{}, closeErr
			}
			return xaiPollResult{}, ctxErr
		}
		raw, readErr := io.ReadAll(resp.Body)
		closeErr := resp.Body.Close()
		if readErr != nil {
			return xaiPollResult{}, readErr
		}
		if closeErr != nil {
			return xaiPollResult{}, closeErr
		}
		if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
			return xaiPollResult{}, fmt.Errorf("xai oauth video poll failed with HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(raw)))
		}

		var res struct {
			Status string `json:"status"`
			Video  struct {
				URL string `json:"url"`
			} `json:"video"`
			Error struct {
				Message string `json:"message"`
			} `json:"error"`
			Message string `json:"message"`
		}
		if err := json.Unmarshal(raw, &res); err != nil {
			return xaiPollResult{}, fmt.Errorf("decode xai oauth video poll response: %w", err)
		}

		status := strings.ToLower(strings.TrimSpace(res.Status))
		switch status {
		case "done":
			videoURL := strings.TrimSpace(res.Video.URL)
			if videoURL == "" {
				return xaiPollResult{}, errors.New("xai oauth video generation completed without video url")
			}
			return xaiPollResult{URL: videoURL, Status: "done"}, nil
		case "failed", "error", "expired", "cancelled":
			msg := strings.TrimSpace(res.Error.Message)
			if msg == "" {
				msg = strings.TrimSpace(res.Message)
			}
			if msg == "" {
				msg = status
			}
			return xaiPollResult{}, fmt.Errorf("xai oauth video generation ended with status %q: %s", status, msg)
		}

		if time.Now().After(deadline) {
			return xaiPollResult{}, fmt.Errorf("timed out waiting for xai oauth video generation, last status %q", status)
		}

		timer := time.NewTimer(defaultXAIPollEvery)
		select {
		case <-ctx.Done():
			timer.Stop()
			return xaiPollResult{}, ctx.Err()
		case <-timer.C:
		}
	}
}

func (c *XAIOAuthClient) downloadVideo(ctx context.Context, url string) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("create xai oauth video download request: %w", err)
	}
	resp, err := c.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("download xai oauth video url: %w", err)
	}
	defer resp.Body.Close()
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return nil, fmt.Errorf("download xai oauth video url failed with HTTP %d", resp.StatusCode)
	}
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if len(data) == 0 {
		return nil, errors.New("download xai oauth video url returned empty video")
	}
	return data, nil
}
