package xaipipeline

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

type BatchInspectSummary struct {
	OutputDir        string                `json:"output_dir"`
	Status           InspectStatus         `json:"status"`
	StoryHash        string                `json:"story_hash,omitempty"`
	VideoModel       string                `json:"video_model,omitempty"`
	TotalEpisodes    int                   `json:"total_episodes"`
	CompleteEpisodes int                   `json:"complete_episodes"`
	PartialEpisodes  int                   `json:"partial_episodes"`
	InvalidEpisodes  int                   `json:"invalid_episodes"`
	Episodes         []BatchInspectEpisode `json:"episodes,omitempty"`
	Issues           []string              `json:"issues,omitempty"`
	MissingArtifacts []string              `json:"missing_artifacts,omitempty"`
}

type BatchInspectEpisode struct {
	Episode   int            `json:"episode"`
	OutputDir string         `json:"output_dir"`
	Status    InspectStatus  `json:"status"`
	Inspect   InspectSummary `json:"inspect"`
}

var batchRootSingleOutputArtifacts = []string{
	manifestFilename,
	runMetadataFilename,
	renderMetadataFilename,
	outputVideoFilename,
	previewFrameFilename,
	"timeline_hyperframes.mp4",
	"shots",
	"normalized",
	"hyperframes",
}

func InspectBatchOutputDir(outputDir string) (BatchInspectSummary, error) {
	if outputDir == "" {
		return BatchInspectSummary{}, fmt.Errorf("xai batch inspect output dir is empty")
	}

	absOutputDir, err := filepath.Abs(outputDir)
	if err != nil {
		return BatchInspectSummary{}, fmt.Errorf("resolve xai batch inspect output dir: %w", err)
	}
	summary := BatchInspectSummary{
		OutputDir: absOutputDir,
		Status:    InspectStatusInvalid,
	}
	summary.Issues = append(summary.Issues, inspectBatchRootIssues(absOutputDir)...)
	summary.Issues = append(summary.Issues, batchRootLegacyArtifactIssues(absOutputDir)...)

	episodes, episodeIssues, err := inspectBatchEpisodeDirs(absOutputDir)
	if err != nil {
		return BatchInspectSummary{}, err
	}
	summary.Issues = append(summary.Issues, episodeIssues...)
	if len(episodes) == 0 {
		summary.Issues = append(summary.Issues, "no episode_### directories found")
		return summary, nil
	}

	for _, episode := range episodes {
		inspect, inspectErr := InspectOutputDir(episode.OutputDir)
		if inspectErr != nil {
			return BatchInspectSummary{}, inspectErr
		}
		entry := BatchInspectEpisode{
			Episode:   episode.Number,
			OutputDir: inspect.OutputDir,
			Status:    inspect.Status,
			Inspect:   inspect,
		}
		summary.Episodes = append(summary.Episodes, entry)
		summary.TotalEpisodes++
		switch inspect.Status {
		case InspectStatusComplete:
			summary.CompleteEpisodes++
		case InspectStatusPartial:
			summary.PartialEpisodes++
			for _, artifact := range inspect.MissingArtifacts {
				summary.MissingArtifacts = append(summary.MissingArtifacts, fmt.Sprintf("episode_%03d/%s", episode.Number, artifact))
			}
		default:
			summary.InvalidEpisodes++
			for _, issue := range inspect.Issues {
				summary.Issues = append(summary.Issues, fmt.Sprintf("episode_%03d: %s", episode.Number, issue))
			}
			for _, artifact := range inspect.MissingArtifacts {
				summary.MissingArtifacts = append(summary.MissingArtifacts, fmt.Sprintf("episode_%03d/%s", episode.Number, artifact))
			}
		}
	}
	summary.Issues = append(summary.Issues, inspectBatchStoryHashIssues(&summary)...)
	summary.Issues = append(summary.Issues, inspectBatchVideoModelIssues(&summary)...)

	return finalizeBatchInspectStatus(summary), nil
}

func inspectBatchRootIssues(outputDir string) []string {
	var issues []string
	for _, artifact := range batchRootSingleOutputArtifacts {
		if batchRootPathExists(filepath.Join(outputDir, artifact)) {
			issues = append(issues, fmt.Sprintf("root %s is not allowed in xAI-native batch output", artifact))
		}
	}
	return issues
}

func batchRootLegacyArtifactIssues(outputDir string) []string {
	path := filepath.Join(outputDir, "remotion_props.json")
	if batchRootPathExists(path) {
		return []string{"legacy remotion_props.json artifact is not allowed in xAI-native batch output"}
	}
	return nil
}

func batchRootPathExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

type batchEpisodeDir struct {
	Number    int
	OutputDir string
}

func inspectBatchEpisodeDirs(outputDir string) ([]batchEpisodeDir, []string, error) {
	entries, err := os.ReadDir(outputDir)
	if err != nil {
		return nil, nil, fmt.Errorf("read xai batch output dir: %w", err)
	}

	var episodes []batchEpisodeDir
	var issues []string
	for _, entry := range entries {
		name := entry.Name()
		if !strings.HasPrefix(name, "episode_") {
			continue
		}
		if entry.Type()&os.ModeSymlink != 0 {
			issues = append(issues, fmt.Sprintf("episode directory %q is a symlink; xAI-native batch episodes must be output-local directories", name))
			continue
		}
		if !entry.IsDir() {
			issues = append(issues, fmt.Sprintf("episode directory %q is not a directory", name))
			continue
		}
		number, ok := parseBatchEpisodeDirName(name)
		if !ok {
			issues = append(issues, fmt.Sprintf("malformed episode directory %q: want episode_###", name))
			continue
		}
		episodes = append(episodes, batchEpisodeDir{
			Number:    number,
			OutputDir: filepath.Join(outputDir, name),
		})
	}
	sort.Slice(episodes, func(i, j int) bool {
		return episodes[i].Number < episodes[j].Number
	})
	for position, episode := range episodes {
		want := position + 1
		if episode.Number != want {
			issues = append(issues, fmt.Sprintf("missing episode_%03d directory", want))
		}
	}
	return episodes, issues, nil
}

func parseBatchEpisodeDirName(name string) (int, bool) {
	if !strings.HasPrefix(name, "episode_") {
		return 0, false
	}
	suffix := strings.TrimPrefix(name, "episode_")
	if len(suffix) != 3 {
		return 0, false
	}
	for _, r := range suffix {
		if r < '0' || r > '9' {
			return 0, false
		}
	}
	number, err := strconv.Atoi(suffix)
	if err != nil || number <= 0 {
		return 0, false
	}
	return number, true
}

func finalizeBatchInspectStatus(summary BatchInspectSummary) BatchInspectSummary {
	switch {
	case summary.InvalidEpisodes > 0 || len(summary.Issues) > 0:
		summary.Status = InspectStatusInvalid
	case summary.PartialEpisodes > 0 || len(summary.MissingArtifacts) > 0:
		summary.Status = InspectStatusPartial
	default:
		summary.Status = InspectStatusComplete
	}
	return summary
}

func inspectBatchVideoModelIssues(summary *BatchInspectSummary) []string {
	var issues []string
	for _, episode := range summary.Episodes {
		if episode.Status != InspectStatusComplete {
			continue
		}
		videoModel := episode.Inspect.VideoModel
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

func inspectBatchStoryHashIssues(summary *BatchInspectSummary) []string {
	var issues []string
	for _, episode := range summary.Episodes {
		if episode.Status != InspectStatusComplete {
			continue
		}
		storyHash := episode.Inspect.StoryHash
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
