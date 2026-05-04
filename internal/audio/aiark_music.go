package audio

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

// AiarkMusicClient generates background music via the aiark ACE-Step API.
// Implements MusicClient (SearchAndDownload).
type AiarkMusicClient struct {
	baseURL        string
	apiKey         string
	duration       int
	httpClient     *http.Client
	pollInterval   time.Duration
	pollTimeout    time.Duration
	imageAdminURL  string // optional: called before generation to free image-api VRAM
}

// NewAiarkMusicClient creates a music client for the aiark ACE-Step endpoint.
// imageAdminURL is optional — when set, it is called before each generation to unload the image model from VRAM.
func NewAiarkMusicClient(baseURL, apiKey string, imageAdminURL ...string) *AiarkMusicClient {
	if baseURL == "" {
		baseURL = "https://aiark.com.tw/music"
	}
	c := &AiarkMusicClient{
		baseURL:      strings.TrimRight(baseURL, "/"),
		apiKey:       apiKey,
		duration:     60,
		httpClient:   &http.Client{Timeout: 30 * time.Second},
		pollInterval: 3 * time.Second,
		pollTimeout:  900 * time.Second,
	}
	if len(imageAdminURL) > 0 && imageAdminURL[0] != "" {
		c.imageAdminURL = imageAdminURL[0]
	}
	return c
}

const (
	releaseTaskTimeout = 180 * time.Second // ACE-Step container startup can take ~60-120s
	releaseTaskRetries = 5
	retryBackoff       = 30 * time.Second
	adminUnloadWait    = 30 * time.Second // wait for container to fully stop after admin/unload
)

// SearchAndDownload generates music from tags using ACE-Step and returns WAV bytes.
// tags format: "cinematic+emotional+strings" → converted to prompt.
// Automatically retries on VRAM shortage (503) and container startup transients (502).
func (c *AiarkMusicClient) SearchAndDownload(ctx context.Context, tags string) ([]byte, error) {
	prompt := strings.ReplaceAll(tags, "+", " ")

	taskID, err := c.releaseTaskWithRetry(ctx, prompt)
	if err != nil {
		return nil, err
	}

	fileURL, err := c.pollUntilDone(ctx, taskID)
	if err != nil {
		return nil, fmt.Errorf("aiark music poll task %s: %w", taskID, err)
	}

	data, err := c.downloadAudio(ctx, fileURL)
	if err != nil {
		return nil, err
	}

	// Proactively free ACE-Step VRAM after download so the GPU lock is released
	// immediately rather than waiting until the next request finds it occupied.
	go func() {
		_ = c.adminUnload(context.Background())
	}()

	return data, nil
}

// releaseTaskWithRetry submits a music task, retrying on VRAM shortage and 502 errors.
// On VRAM error: unloads image-api and ACE-Step, waits for containers to stop, then retries.
// On 502 (container starting): waits and retries without unloading.
// Uses a longer HTTP timeout so music-api has time to start the ACE-Step container.
func (c *AiarkMusicClient) releaseTaskWithRetry(ctx context.Context, prompt string) (string, error) {
	longClient := &http.Client{Timeout: releaseTaskTimeout}

	for attempt := 0; attempt < releaseTaskRetries; attempt++ {
		taskID, err := c.releaseTaskWith(ctx, longClient, prompt)
		if err == nil {
			return taskID, nil
		}

		isVRAM := isVRAMError(err)
		isTransient := strings.Contains(err.Error(), "status 502") || strings.Contains(err.Error(), "deadline exceeded")

		if isVRAM {
			// Free image-api VRAM then stop ACE-Step container so it restarts clean.
			if c.imageAdminURL != "" {
				_ = c.callAdminUnload(ctx, c.imageAdminURL)
			}
			_ = c.adminUnload(ctx)
			select {
			case <-ctx.Done():
				return "", ctx.Err()
			case <-time.After(adminUnloadWait):
			}
			continue
		}

		if isTransient && attempt < releaseTaskRetries-1 {
			select {
			case <-ctx.Done():
				return "", ctx.Err()
			case <-time.After(retryBackoff):
			}
			continue
		}

		return "", fmt.Errorf("aiark music release_task: %w", err)
	}
	return "", fmt.Errorf("aiark music release_task: exhausted %d retries", releaseTaskRetries)
}

func isVRAMError(err error) bool {
	if err == nil {
		return false
	}
	s := err.Error()
	return strings.Contains(s, "insufficient_vram") || strings.Contains(s, "status 503")
}

func (c *AiarkMusicClient) adminUnload(ctx context.Context) error {
	return c.callAdminUnload(ctx, c.baseURL+"/admin/unload")
}

func (c *AiarkMusicClient) callAdminUnload(ctx context.Context, url string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", "stagenthand/1.0")
	if c.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+c.apiKey)
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	io.ReadAll(resp.Body) //nolint:errcheck
	return nil
}

func (c *AiarkMusicClient) releaseTaskWith(ctx context.Context, client *http.Client, prompt string) (string, error) {
	payload := map[string]any{
		"prompt":            prompt,
		"lyrics":            "[instrumental]",
		"thinking":          false,
		"use_cot_caption":   false,
		"use_cot_language":  false,
		"use_cot_metas":     false,
		"audio_duration":    c.duration,
		"task_type":         "text2music",
		"audio_format":      "wav",
		"use_random_seed":   true,
		"batch_size":        1,
	}
	body, _ := json.Marshal(payload)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/release_task", bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "stagenthand/1.0")
	if c.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+c.apiKey)
	}

	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	respBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("status %d: %s", resp.StatusCode, string(respBody))
	}

	var result struct {
		Data struct {
			TaskID string `json:"task_id"`
		} `json:"data"`
	}
	if err := json.Unmarshal(respBody, &result); err != nil || result.Data.TaskID == "" {
		return "", fmt.Errorf("unexpected response: %s", string(respBody))
	}
	return result.Data.TaskID, nil
}

func (c *AiarkMusicClient) pollUntilDone(ctx context.Context, taskID string) (string, error) {
	deadline := time.Now().Add(c.pollTimeout)
	payload, _ := json.Marshal(map[string]any{"task_id_list": []string{taskID}})

	for time.Now().Before(deadline) {
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		case <-time.After(c.pollInterval):
		}

		req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/query_result", bytes.NewReader(payload))
		if err != nil {
			continue
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("User-Agent", "stagenthand/1.0")
		if c.apiKey != "" {
			req.Header.Set("Authorization", "Bearer "+c.apiKey)
		}

		resp, err := c.httpClient.Do(req)
		if err != nil {
			continue
		}
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			continue
		}

		var qr struct {
			Data []struct {
				Status int    `json:"status"` // 0=queued/running, 1=succeeded, 2=failed
				Result string `json:"result"` // JSON string containing [{file: "..."}]
			} `json:"data"`
		}
		if err := json.Unmarshal(body, &qr); err != nil || len(qr.Data) == 0 {
			continue
		}
		switch qr.Data[0].Status {
		case 1: // succeeded
			var files []struct {
				File string `json:"file"`
			}
			if err := json.Unmarshal([]byte(qr.Data[0].Result), &files); err != nil || len(files) == 0 {
				return "", fmt.Errorf("cannot parse result: %s", qr.Data[0].Result)
			}
			fileURL := files[0].File
			if !strings.HasPrefix(fileURL, "http") {
				fileURL = c.baseURL + fileURL
			}
			return fileURL, nil
		case 2: // failed
			return "", fmt.Errorf("task failed")
		}
	}
	return "", fmt.Errorf("timed out after %s", c.pollTimeout)
}

func (c *AiarkMusicClient) downloadAudio(ctx context.Context, url string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	if c.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+c.apiKey)
	}
	client := &http.Client{Timeout: 120 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("download status %d: %s", resp.StatusCode, url)
	}
	return io.ReadAll(resp.Body)
}
