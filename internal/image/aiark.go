package image

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// AiarkImageClient implements image.Client against the aiark image API
// (POST /v1/images/generations → GET /files/{filename}).
type AiarkImageClient struct {
	baseURL    string
	apiKey     string
	model      string
	width      int
	height     int
	httpClient *http.Client
}

func NewAiarkImageClient(baseURL, apiKey, model string, width, height int) *AiarkImageClient {
	if baseURL == "" {
		baseURL = "https://aiark.com.tw/image"
	}
	if model == "" {
		model = "aiark/qwen-image-2512-q4km"
	}
	if width == 0 {
		width = 576
	}
	if height == 0 {
		height = 1024
	}
	return &AiarkImageClient{
		baseURL:    strings.TrimRight(baseURL, "/"),
		apiKey:     apiKey,
		model:      model,
		width:      width,
		height:     height,
		httpClient: &http.Client{Timeout: 600 * time.Second},
	}
}

const (
	vramRetryMax     = 10
	vramRetryDelay   = 60 * time.Second
	recycleRetryMax  = 6
	recycleRetryDelay = 15 * time.Second
)

func (c *AiarkImageClient) GenerateImage(ctx context.Context, prompt string, _ []string) ([]byte, error) {
	payload := map[string]any{
		"prompt":              prompt,
		"model":               c.model,
		"width":               c.width,
		"height":              c.height,
		"num_inference_steps": 16,
	}
	body, _ := json.Marshal(payload)

	vramAttempts := 0
	recycleAttempts := 0

	for {
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/v1/images/generations", bytes.NewReader(body))
		if err != nil {
			return nil, err
		}
		req.Header.Set("Content-Type", "application/json")
		if c.apiKey != "" {
			req.Header.Set("Authorization", "Bearer "+c.apiKey)
		}

		resp, err := c.httpClient.Do(req)
		if err != nil {
			return nil, fmt.Errorf("aiark image generate: %w", err)
		}
		respBody, _ := io.ReadAll(resp.Body)
		resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			bodyStr := string(respBody)
			if strings.Contains(bodyStr, "insufficient_vram") {
				vramAttempts++
				if vramAttempts > vramRetryMax {
					return nil, fmt.Errorf("aiark image: GPU VRAM 持續不足，已重試 %d 次", vramRetryMax)
				}
				fmt.Printf("[Info] aiark image: GPU VRAM 不足，等待 %s 後重試 (%d/%d)...\n", vramRetryDelay, vramAttempts, vramRetryMax)
				select {
				case <-ctx.Done():
					return nil, ctx.Err()
				case <-time.After(vramRetryDelay):
				}
				continue
			}
			// Retry on 5xx (service recycling after IMAGE_RECYCLE_AFTER_REQUEST=true)
			if resp.StatusCode >= 500 {
				recycleAttempts++
				if recycleAttempts > recycleRetryMax {
					return nil, fmt.Errorf("aiark image generate status %d: %s", resp.StatusCode, bodyStr)
				}
				fmt.Printf("[Info] aiark image: 服務重啟中 (HTTP %d)，等待 %s 後重試 (%d/%d)...\n", resp.StatusCode, recycleRetryDelay, recycleAttempts, recycleRetryMax)
				select {
				case <-ctx.Done():
					return nil, ctx.Err()
				case <-time.After(recycleRetryDelay):
				}
				continue
			}
			return nil, fmt.Errorf("aiark image generate status %d: %s", resp.StatusCode, bodyStr)
		}
		recycleAttempts = 0 // reset on success

		// Parse response — aiark returns either OpenAI-style {"data":[{"url":"..."}]}
		// or its own format {"created":"...","data":[{"url":"..."}]}.
		var result struct {
			Data []struct {
				URL string `json:"url"`
			} `json:"data"`
		}
		if err := json.Unmarshal(respBody, &result); err != nil || len(result.Data) == 0 {
			return nil, fmt.Errorf("aiark image: unexpected response: %s", string(respBody))
		}

		fileURL := result.Data[0].URL
		if !strings.HasPrefix(fileURL, "http") {
			fileURL = c.baseURL + fileURL
		}
		return c.downloadFileWithRetry(ctx, fileURL)
	}
}

func (c *AiarkImageClient) downloadFileWithRetry(ctx context.Context, url string) ([]byte, error) {
	for attempt := 0; attempt <= recycleRetryMax; attempt++ {
		if attempt > 0 {
			fmt.Printf("[Info] aiark image: 下載失敗，等待 %s 後重試 (%d/%d)...\n", recycleRetryDelay, attempt, recycleRetryMax)
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(recycleRetryDelay):
			}
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
		if err != nil {
			return nil, err
		}
		if c.apiKey != "" {
			req.Header.Set("Authorization", "Bearer "+c.apiKey)
		}
		resp, err := c.httpClient.Do(req)
		if err != nil {
			return nil, fmt.Errorf("aiark image download: %w", err)
		}
		data, readErr := io.ReadAll(resp.Body)
		resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			if resp.StatusCode >= 500 {
				continue
			}
			return nil, fmt.Errorf("aiark image download status %d: %s", resp.StatusCode, url)
		}
		if readErr != nil {
			return nil, fmt.Errorf("aiark image download read: %w", readErr)
		}
		// If gateway returns a 200 with an error page instead of PNG, retry.
		pngSig := []byte{0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A}
		if len(data) < 8 || !bytes.Equal(data[:8], pngSig) {
			fmt.Printf("[Info] aiark image: 下載回應非PNG（len=%d），等待 %s 後重試 (%d/%d)...\n", len(data), recycleRetryDelay, attempt+1, recycleRetryMax)
			continue
		}
		return data, nil
	}
	return nil, fmt.Errorf("aiark image: 下載持續失敗，已重試 %d 次: %s", recycleRetryMax, url)
}
