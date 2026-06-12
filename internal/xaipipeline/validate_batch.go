package xaipipeline

import (
	"context"
	"fmt"
	"path/filepath"
)

type BatchValidationSummary struct {
	OutputDir       string                   `json:"output_dir"`
	Status          ValidationStatus         `json:"status"`
	StoryHash       string                   `json:"story_hash,omitempty"`
	VideoModel      string                   `json:"video_model,omitempty"`
	TotalEpisodes   int                      `json:"total_episodes"`
	ValidEpisodes   int                      `json:"valid_episodes"`
	InvalidEpisodes int                      `json:"invalid_episodes"`
	Episodes        []BatchValidationEpisode `json:"episodes,omitempty"`
	Issues          []string                 `json:"issues,omitempty"`
}

type BatchValidationEpisode struct {
	Episode    int               `json:"episode"`
	OutputDir  string            `json:"output_dir"`
	Status     ValidationStatus  `json:"status"`
	Validation ValidationSummary `json:"validation"`
}

func ValidateBatchOutputDir(ctx context.Context, outputDir string, validator OutputValidator) (BatchValidationSummary, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return BatchValidationSummary{}, err
	}
	if outputDir == "" {
		return BatchValidationSummary{}, fmt.Errorf("xai batch validate output dir is empty")
	}
	absOutputDir, err := filepath.Abs(outputDir)
	if err != nil {
		return BatchValidationSummary{}, fmt.Errorf("resolve xai batch validate output dir: %w", err)
	}

	summary := BatchValidationSummary{
		OutputDir: absOutputDir,
		Status:    ValidationStatusInvalid,
	}
	summary.Issues = append(summary.Issues, validateBatchLegacyArtifacts(absOutputDir)...)
	episodes, episodeIssues, err := inspectBatchEpisodeDirs(absOutputDir)
	if err != nil {
		return BatchValidationSummary{}, err
	}
	summary.Issues = append(summary.Issues, episodeIssues...)
	if len(episodes) == 0 {
		summary.Issues = append(summary.Issues, "no episode_### directories found")
		return summary, nil
	}

	for _, episode := range episodes {
		validation, validationErr := ValidateOutputDir(ctx, episode.OutputDir, validator)
		if validationErr != nil {
			return BatchValidationSummary{}, validationErr
		}
		entry := BatchValidationEpisode{
			Episode:    episode.Number,
			OutputDir:  validation.OutputDir,
			Status:     validation.Status,
			Validation: validation,
		}
		summary.Episodes = append(summary.Episodes, entry)
		summary.TotalEpisodes++
		if validation.Status == ValidationStatusValid {
			summary.ValidEpisodes++
			continue
		}
		summary.InvalidEpisodes++
		for _, issue := range validation.Issues {
			summary.Issues = append(summary.Issues, fmt.Sprintf("episode_%03d: %s", episode.Number, issue))
		}
	}
	summary.Issues = append(summary.Issues, validateBatchStoryHashConsistency(&summary)...)
	summary.Issues = append(summary.Issues, validateBatchVideoModelConsistency(&summary)...)

	return finalizeBatchValidationStatus(summary), nil
}

func validateBatchLegacyArtifacts(outputDir string) []string {
	issues := inspectBatchRootIssues(outputDir)
	issues = append(issues, batchRootLegacyArtifactIssues(outputDir)...)
	return issues
}

func finalizeBatchValidationStatus(summary BatchValidationSummary) BatchValidationSummary {
	if summary.InvalidEpisodes == 0 && len(summary.Issues) == 0 {
		summary.Status = ValidationStatusValid
		return summary
	}
	summary.Status = ValidationStatusInvalid
	return summary
}

func validateBatchVideoModelConsistency(summary *BatchValidationSummary) []string {
	var issues []string
	for _, episode := range summary.Episodes {
		videoModel := episode.Validation.Inspect.VideoModel
		if videoModel == "" {
			continue
		}
		if summary.VideoModel == "" {
			summary.VideoModel = videoModel
			continue
		}
		if videoModel != summary.VideoModel {
			issues = append(issues, fmt.Sprintf(
				"episode_%03d video_model=%q, want batch video_model %q",
				episode.Episode,
				videoModel,
				summary.VideoModel,
			))
		}
	}
	return issues
}

func validateBatchStoryHashConsistency(summary *BatchValidationSummary) []string {
	var issues []string
	for _, episode := range summary.Episodes {
		storyHash := episode.Validation.Inspect.StoryHash
		if storyHash == "" {
			continue
		}
		if summary.StoryHash == "" {
			summary.StoryHash = storyHash
			continue
		}
		if storyHash != summary.StoryHash {
			issues = append(issues, fmt.Sprintf(
				"episode_%03d story_hash=%q, want batch story_hash %q",
				episode.Episode,
				storyHash,
				summary.StoryHash,
			))
		}
	}
	return issues
}
