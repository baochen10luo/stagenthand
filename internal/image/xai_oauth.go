package image

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const (
	defaultXAIImageBaseURL    = "https://api.x.ai/v1"
	defaultXAIImageModel      = "grok-imagine-image-quality"
	defaultXAIImageAspect     = "9:16"
	defaultXAIImageResolution = "1k"
)

type XAIOAuthImageTokenSource interface {
	BearerToken(ctx context.Context) (string, error)
}

type XAIImageHTTPDoer interface {
	Do(req *http.Request) (*http.Response, error)
}

type XAIImageOptions struct {
	Model       string
	AspectRatio string
	Resolution  string
	References  []string
}

type XAIImageResult struct {
	Data           []byte
	Model          string
	Prompt         string
	AspectRatio    string
	Resolution     string
	ReferenceCount int
	URL            string
	MimeType       string
	RevisedPrompt  string
}

// XAIOAuthImageClient calls xAI Imagine image generation/edit endpoints.
type XAIOAuthImageClient struct {
	baseURL     string
	model       string
	aspectRatio string
	resolution  string
	tokenSource XAIOAuthImageTokenSource
	client      XAIImageHTTPDoer
}

func NewXAIOAuthImageClient(baseURL, model, aspectRatio, resolution string, tokenSource XAIOAuthImageTokenSource, client XAIImageHTTPDoer) *XAIOAuthImageClient {
	if client == nil {
		client = &http.Client{Timeout: 120 * time.Second}
	}
	model = strings.TrimSpace(model)
	if model == "" {
		model = defaultXAIImageModel
	}
	aspectRatio = strings.TrimSpace(aspectRatio)
	if aspectRatio == "" {
		aspectRatio = defaultXAIImageAspect
	}
	resolution = strings.TrimSpace(resolution)
	if resolution == "" {
		resolution = defaultXAIImageResolution
	}
	return &XAIOAuthImageClient{
		baseURL:     normalizeXAIImageBaseURL(baseURL),
		model:       model,
		aspectRatio: aspectRatio,
		resolution:  resolution,
		tokenSource: tokenSource,
		client:      client,
	}
}

func normalizeXAIImageBaseURL(baseURL string) string {
	base := strings.TrimRight(strings.TrimSpace(baseURL), "/")
	if base == "" {
		base = defaultXAIImageBaseURL
	}
	for _, suffix := range []string{"/v1/images/generations", "/v1/images/edits", "/v1/images", "/images/generations", "/images/edits", "/images"} {
		if strings.HasSuffix(base, suffix) {
			return strings.TrimSuffix(base, suffix) + "/v1"
		}
	}
	if strings.HasSuffix(base, "/v1") {
		return base
	}
	return base + "/v1"
}

func (c *XAIOAuthImageClient) GenerateImage(ctx context.Context, prompt string, characterRefs []string) ([]byte, error) {
	result, err := c.Create(ctx, prompt, XAIImageOptions{References: characterRefs})
	if err != nil {
		return nil, err
	}
	return result.Data, nil
}

func (c *XAIOAuthImageClient) Create(ctx context.Context, prompt string, options XAIImageOptions) (XAIImageResult, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	prompt = strings.TrimSpace(prompt)
	if prompt == "" {
		return XAIImageResult{}, errors.New("xai image prompt is empty")
	}
	if c.tokenSource == nil {
		return XAIImageResult{}, errors.New("xai image token source is nil")
	}
	if err := ctx.Err(); err != nil {
		return XAIImageResult{}, err
	}
	token, err := c.tokenSource.BearerToken(ctx)
	if err != nil {
		return XAIImageResult{}, fmt.Errorf("xai image bearer token: %w", err)
	}
	token = strings.TrimSpace(token)
	if token == "" {
		return XAIImageResult{}, errors.New("xai image bearer token is empty")
	}

	model := firstNonBlank(options.Model, c.model, defaultXAIImageModel)
	aspectRatio := firstNonBlank(options.AspectRatio, c.aspectRatio, defaultXAIImageAspect)
	resolution := firstNonBlank(options.Resolution, c.resolution, defaultXAIImageResolution)

	body := map[string]any{
		"model":           model,
		"prompt":          prompt,
		"response_format": "b64_json",
		"aspect_ratio":    aspectRatio,
		"resolution":      resolution,
	}
	endpoint := c.baseURL + "/images/generations"
	refs := trimEmptyStrings(options.References)
	if len(refs) > 0 {
		if len(refs) > 3 {
			return XAIImageResult{}, fmt.Errorf("xai image edit supports at most 3 references, got %d", len(refs))
		}
		images, err := xaiReferenceImages(refs)
		if err != nil {
			return XAIImageResult{}, err
		}
		body["images"] = images
		endpoint = c.baseURL + "/images/edits"
	}

	raw, err := json.Marshal(body)
	if err != nil {
		return XAIImageResult{}, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(raw))
	if err != nil {
		return XAIImageResult{}, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "StagentHand/1 xAI-OAuth")

	resp, err := c.client.Do(req)
	if err != nil {
		return XAIImageResult{}, fmt.Errorf("xai image request failed: %w", err)
	}
	defer resp.Body.Close()
	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return XAIImageResult{}, err
	}
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		msg := xaiImageErrorMessage(respBody)
		if msg == "" {
			msg = http.StatusText(resp.StatusCode)
		}
		return XAIImageResult{}, fmt.Errorf("xai image request failed with HTTP %d: %s", resp.StatusCode, msg)
	}

	imageItem, err := parseXAIImageResponse(respBody)
	if err != nil {
		return XAIImageResult{}, err
	}
	data := imageItem.Data
	if len(data) == 0 && imageItem.URL != "" {
		data, err = c.downloadImage(ctx, imageItem.URL)
		if err != nil {
			return XAIImageResult{}, err
		}
	}
	if len(data) == 0 {
		return XAIImageResult{}, errors.New("xai image response had no image bytes")
	}
	return XAIImageResult{
		Data:           data,
		Model:          model,
		Prompt:         prompt,
		AspectRatio:    aspectRatio,
		Resolution:     resolution,
		ReferenceCount: len(refs),
		URL:            imageItem.URL,
		MimeType:       imageItem.MimeType,
		RevisedPrompt:  imageItem.RevisedPrompt,
	}, nil
}

func firstNonBlank(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func trimEmptyStrings(values []string) []string {
	out := make([]string, 0, len(values))
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			out = append(out, strings.TrimSpace(value))
		}
	}
	return out
}

func xaiReferenceImages(refs []string) ([]map[string]string, error) {
	images := make([]map[string]string, 0, len(refs))
	for _, ref := range refs {
		url, err := xaiReferenceImageURL(ref)
		if err != nil {
			return nil, err
		}
		images = append(images, map[string]string{
			"type": "image_url",
			"url":  url,
		})
	}
	return images, nil
}

func xaiReferenceImageURL(ref string) (string, error) {
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return "", errors.New("xai image reference is empty")
	}
	if strings.HasPrefix(ref, "http://") || strings.HasPrefix(ref, "https://") || strings.HasPrefix(ref, "data:image/") {
		return ref, nil
	}
	data, err := os.ReadFile(ref)
	if err != nil {
		return "", fmt.Errorf("read xai image reference %s: %w", ref, err)
	}
	mimeType := http.DetectContentType(data)
	if !strings.HasPrefix(mimeType, "image/") {
		switch strings.ToLower(filepath.Ext(ref)) {
		case ".jpg", ".jpeg":
			mimeType = "image/jpeg"
		case ".png":
			mimeType = "image/png"
		case ".webp":
			mimeType = "image/webp"
		default:
			return "", fmt.Errorf("xai image reference %s is not an image", ref)
		}
	}
	return "data:" + mimeType + ";base64," + base64.StdEncoding.EncodeToString(data), nil
}

type xaiImageResponseItem struct {
	Data          []byte
	URL           string
	MimeType      string
	RevisedPrompt string
}

func parseXAIImageResponse(raw []byte) (xaiImageResponseItem, error) {
	var payload struct {
		Data []struct {
			B64JSON       string `json:"b64_json"`
			URL           string `json:"url"`
			MimeType      string `json:"mime_type"`
			RevisedPrompt string `json:"revised_prompt"`
		} `json:"data"`
	}
	if err := json.Unmarshal(raw, &payload); err != nil {
		return xaiImageResponseItem{}, err
	}
	if len(payload.Data) == 0 {
		return xaiImageResponseItem{}, errors.New("xai image response did not include data")
	}
	item := payload.Data[0]
	out := xaiImageResponseItem{
		URL:           strings.TrimSpace(item.URL),
		MimeType:      strings.TrimSpace(item.MimeType),
		RevisedPrompt: strings.TrimSpace(item.RevisedPrompt),
	}
	if strings.TrimSpace(item.B64JSON) != "" {
		decoded, err := base64.StdEncoding.DecodeString(strings.TrimSpace(item.B64JSON))
		if err != nil {
			return xaiImageResponseItem{}, fmt.Errorf("decode xai image b64_json: %w", err)
		}
		out.Data = decoded
	}
	return out, nil
}

func (c *XAIOAuthImageClient) downloadImage(ctx context.Context, imageURL string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, imageURL, nil)
	if err != nil {
		return nil, err
	}
	resp, err := c.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("download xai image: %w", err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return nil, fmt.Errorf("download xai image failed with HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(raw)))
	}
	return raw, nil
}

func xaiImageErrorMessage(raw []byte) string {
	var payload struct {
		Error struct {
			Message string `json:"message"`
			Code    string `json:"code"`
		} `json:"error"`
		Message string `json:"message"`
	}
	if err := json.Unmarshal(raw, &payload); err == nil {
		if payload.Error.Message != "" {
			return payload.Error.Message
		}
		if payload.Error.Code != "" {
			return payload.Error.Code
		}
		if payload.Message != "" {
			return payload.Message
		}
	}
	return strings.TrimSpace(string(raw))
}
