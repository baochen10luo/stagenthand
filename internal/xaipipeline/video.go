package xaipipeline

import (
	"context"
	"errors"
	"fmt"
	"strings"
)

type VideoClient interface {
	GenerateVideo(ctx context.Context, imageURL string, prompt string, options VideoOptions) ([]byte, error)
}

type VideoResultClient interface {
	GenerateVideoResult(ctx context.Context, imageURL string, prompt string, options VideoOptions) (VideoGenerationResult, error)
}

type VideoOptions struct {
	DurationSec float64
	AspectRatio string
	Resolution  string
}

type VideoGenerationResult struct {
	Data      []byte
	RequestID string
	Status    string
}

type VideoShotGenerator struct {
	client VideoResultClient
}

func NewVideoShotGenerator(client VideoResultClient) *VideoShotGenerator {
	return &VideoShotGenerator{client: client}
}

func (g *VideoShotGenerator) GenerateShot(ctx context.Context, shot Shot) ([]byte, error) {
	result, err := g.GenerateShotResult(ctx, shot)
	if err != nil {
		return nil, err
	}
	return result.Data, nil
}

func (g *VideoShotGenerator) GenerateShotResult(ctx context.Context, shot Shot) (ShotGenerationResult, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if g.client == nil {
		return ShotGenerationResult{}, errors.New("xai video shot generator client is nil")
	}
	prompt := strings.TrimSpace(shot.Prompt)
	if prompt == "" {
		return ShotGenerationResult{}, fmt.Errorf("xai video shot generator shot %d prompt is empty", shot.Index)
	}
	options := normalizedVideoOptions(shot)
	if err := ctx.Err(); err != nil {
		return ShotGenerationResult{}, err
	}
	result, err := g.client.GenerateVideoResult(ctx, "", prompt, options)
	if err != nil {
		return ShotGenerationResult{}, err
	}
	if err := ctx.Err(); err != nil {
		return ShotGenerationResult{}, err
	}
	if len(result.Data) == 0 {
		return ShotGenerationResult{}, fmt.Errorf("xai video shot generator shot %d video data is empty", shot.Index)
	}
	providerMetadata, err := normalizeLiveVideoProviderMetadata(shot.Index, result.RequestID, result.Status)
	if err != nil {
		return ShotGenerationResult{}, err
	}
	return ShotGenerationResult{
		Data:      result.Data,
		RequestID: providerMetadata.requestID,
		Status:    providerMetadata.status,
	}, nil
}

func normalizeLiveVideoProviderMetadata(shotIndex int, requestID string, status string) (shotProviderMetadata, error) {
	metadata, err := normalizeShotProviderMetadata(shotIndex, requestID, status)
	if err != nil {
		return shotProviderMetadata{}, err
	}
	if metadata.status != "done" {
		return shotProviderMetadata{}, fmt.Errorf("invalid xai video provider metadata: shot %d xai_status %q is not done", shotIndex, metadata.status)
	}
	return metadata, nil
}

func normalizedVideoOptions(shot Shot) VideoOptions {
	duration := shot.DurationSec
	if duration <= 0 {
		duration = defaultDurationSec
	}
	aspectRatio := strings.TrimSpace(shot.AspectRatio)
	if aspectRatio == "" {
		aspectRatio = defaultAspectRatio
	}
	resolution := strings.TrimSpace(shot.Resolution)
	if resolution == "" {
		resolution = defaultResolution
	}
	return VideoOptions{
		DurationSec: duration,
		AspectRatio: aspectRatio,
		Resolution:  resolution,
	}
}
