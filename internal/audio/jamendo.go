package audio

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
)

// MusicClient defines the interface for fetching background music.
type MusicClient interface {
	SearchAndDownload(ctx context.Context, tags string) ([]byte, error)
}

// JamendoClient implements MusicClient using the Jamendo v3 API.
type JamendoClient struct {
	clientID string
	baseURL  string
}

func NewJamendoClient(clientID string) *JamendoClient {
	if clientID == "" {
		// Public test key commonly used in docs, though limits apply.
		clientID = "56d30c95"
	}
	return &JamendoClient{clientID: clientID, baseURL: "https://api.jamendo.com/v3.0/tracks/"}
}

// SearchAndDownload searches Jamendo by tags with fallback strategy:
//  1. Try multi-tag AND search first.
//  2. If no results, try each individual tag in sequence.
//  3. If all individual tags fail and "cinematic" hasn't been tried, try "cinematic".
//  4. If everything fails, return an error containing "no tracks found".
func (c *JamendoClient) SearchAndDownload(ctx context.Context, tags string) ([]byte, error) {
	// Normalise separators: "space+adventure" or "space, adventure" → ["space", "adventure"]
	normalizedAll := strings.ReplaceAll(tags, ",", "+")
	parts := strings.FieldsFunc(normalizedAll, func(r rune) bool { return r == '+' || r == ' ' })

	// Deduplicate and trim individual tags for later fallback use.
	seen := make(map[string]bool)
	var individualTags []string
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		low := strings.ToLower(p)
		if !seen[low] {
			seen[low] = true
			individualTags = append(individualTags, p)
		}
	}

	// Build the full multi-tag query string (space-separated → QueryEscape → "+")
	multiTagQuery := strings.Join(individualTags, " ")

	// --- Attempt 1: full multi-tag AND search ---
	if audioURL, err := c.searchByTag(ctx, multiTagQuery); err != nil {
		return nil, err // hard error (non-200, decode failure, etc.)
	} else if audioURL != "" {
		return c.downloadAudio(ctx, audioURL)
	}

	// --- Attempt 2: individual tag fallback ---
	triedTags := []string{multiTagQuery}
	for _, tag := range individualTags {
		audioURL, err := c.searchByTag(ctx, tag)
		if err != nil {
			return nil, err
		}
		triedTags = append(triedTags, tag)
		if audioURL != "" {
			return c.downloadAudio(ctx, audioURL)
		}
	}

	// --- Attempt 3: cinematic fallback (if not already tried) ---
	if !seen["cinematic"] {
		audioURL, err := c.searchByTag(ctx, "cinematic")
		if err != nil {
			return nil, err
		}
		triedTags = append(triedTags, "cinematic")
		if audioURL != "" {
			return c.downloadAudio(ctx, audioURL)
		}
	}

	return nil, fmt.Errorf("no tracks found after trying %d tags: %v", len(triedTags), triedTags)
}

// searchByTag calls the Jamendo search API for a single (or space-joined multi) tag query.
// It returns the audio URL of the first result, or "" if no results were found.
// A non-nil error indicates a hard failure (network, non-200, decode error).
func (c *JamendoClient) searchByTag(ctx context.Context, tag string) (string, error) {
	apiURL := fmt.Sprintf("%s?client_id=%s&format=json&limit=1&tags=%s",
		c.baseURL, c.clientID, url.QueryEscape(tag))

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, apiURL, nil)
	if err != nil {
		return "", fmt.Errorf("failed to create jamendo request: %w", err)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("jamendo API request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("jamendo API returned status %d", resp.StatusCode)
	}

	var result struct {
		Headers struct {
			Status       string `json:"status"`
			ErrorCode    int    `json:"code"`
			ErrorMessage string `json:"error_message"`
		} `json:"headers"`
		Results []struct {
			ID    string `json:"id"`
			Name  string `json:"name"`
			Audio string `json:"audio"` // URL to the mp3
		} `json:"results"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", fmt.Errorf("failed to decode jamendo response: %w", err)
	}

	if result.Headers.Status != "success" {
		return "", fmt.Errorf("jamendo API error: %s", result.Headers.ErrorMessage)
	}
	if len(result.Results) == 0 {
		return "", nil // soft empty — caller decides fallback
	}

	track := result.Results[0]
	if track.Audio == "" {
		return "", fmt.Errorf("jamendo track %s has no audio url", track.ID)
	}

	return track.Audio, nil
}

func (c *JamendoClient) downloadAudio(ctx context.Context, audioURL string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, audioURL, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create download request: %w", err)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("audio download failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("audio download returned status %d", resp.StatusCode)
	}

	return io.ReadAll(resp.Body)
}
