package xaipipeline

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

const (
	manifestFilename       = "xai_manifest.json"
	runMetadataFilename    = "xai_run_metadata.json"
	renderMetadataFilename = "render_metadata.json"
	outputVideoFilename    = "output_xai.mp4"
	previewFrameFilename   = "preview_frame.jpg"
)

type InspectStatus string

const (
	InspectStatusComplete InspectStatus = "complete"
	InspectStatusPartial  InspectStatus = "partial"
	InspectStatusInvalid  InspectStatus = "invalid"
)

type InspectSummary struct {
	OutputDir        string           `json:"output_dir"`
	Status           InspectStatus    `json:"status"`
	Manifest         Manifest         `json:"-"`
	ProjectID        string           `json:"project_id"`
	StoryHash        string           `json:"story_hash,omitempty"`
	VideoModel       string           `json:"video_model,omitempty"`
	Format           string           `json:"format"`
	FPS              int              `json:"fps"`
	Width            int              `json:"width"`
	Height           int              `json:"height"`
	Shots            int              `json:"shots"`
	ShotSummaries    []InspectShot    `json:"shot_summaries,omitempty"`
	Artifacts        InspectArtifacts `json:"artifacts"`
	RunMetadata      *RunMetadata     `json:"run_metadata,omitempty"`
	RenderMetadata   *RenderMetadata  `json:"render_metadata,omitempty"`
	MissingArtifacts []string         `json:"missing_artifacts,omitempty"`
	Issues           []string         `json:"issues,omitempty"`
}

type InspectShot struct {
	Index         int     `json:"index"`
	Prompt        string  `json:"prompt,omitempty"`
	Subtitle      string  `json:"subtitle,omitempty"`
	TransitionOut string  `json:"transition_out,omitempty"`
	VideoPath     string  `json:"video_path,omitempty"`
	PromptHash    string  `json:"prompt_hash,omitempty"`
	XAIRequestID  string  `json:"xai_request_id,omitempty"`
	XAIStatus     string  `json:"xai_status,omitempty"`
	DurationSec   float64 `json:"duration_sec,omitempty"`
	AspectRatio   string  `json:"aspect_ratio,omitempty"`
	Resolution    string  `json:"resolution,omitempty"`
}

type InspectArtifacts struct {
	XAIManifest        string `json:"xai_manifest"`
	RunMetadata        string `json:"run_metadata"`
	RenderMetadata     string `json:"render_metadata"`
	OutputVideo        string `json:"output_video"`
	PreviewFrame       string `json:"preview_frame"`
	OutputVideoExists  bool   `json:"output_video_exists"`
	PreviewFrameExists bool   `json:"preview_frame_exists"`
}

func InspectOutputDir(outputDir string) (InspectSummary, error) {
	if outputDir == "" {
		return InspectSummary{}, errors.New("xai inspect output dir is empty")
	}

	absOutputDir, err := filepath.Abs(outputDir)
	if err != nil {
		return InspectSummary{}, fmt.Errorf("resolve xai inspect output dir: %w", err)
	}

	artifacts := InspectArtifacts{
		XAIManifest:    filepath.Join(absOutputDir, manifestFilename),
		RunMetadata:    filepath.Join(absOutputDir, runMetadataFilename),
		RenderMetadata: filepath.Join(absOutputDir, renderMetadataFilename),
		OutputVideo:    filepath.Join(absOutputDir, outputVideoFilename),
		PreviewFrame:   filepath.Join(absOutputDir, previewFrameFilename),
	}

	summary := InspectSummary{
		OutputDir: absOutputDir,
		Status:    InspectStatusInvalid,
		Artifacts: artifacts,
	}
	summary.Issues = append(summary.Issues, singleOutputLegacyArtifactIssues(absOutputDir)...)

	var manifest Manifest
	manifestLoaded, issue, err := readInspectRequiredJSON(artifacts.XAIManifest, &manifest)
	if err != nil {
		return InspectSummary{}, err
	}
	if !manifestLoaded {
		summary.MissingArtifacts = append(summary.MissingArtifacts, manifestFilename)
		summary.Issues = append(summary.Issues, issue)
		return finalizeInspectStatus(summary), nil
	}
	if issue != "" {
		summary.Issues = append(summary.Issues, issue)
		return finalizeInspectStatus(summary), nil
	}

	summary.Manifest = manifest
	summary.ProjectID = manifest.ProjectID
	summary.StoryHash = manifest.StoryHash
	summary.VideoModel = manifest.VideoModel
	summary.Format = manifest.Format
	summary.FPS = manifest.FPS
	summary.Width = manifest.Width
	summary.Height = manifest.Height
	summary.Shots = len(manifest.Shots)
	summary.ShotSummaries = inspectShots(manifest.Shots)
	summary.MissingArtifacts = append(summary.MissingArtifacts, missingProductionArtifacts(absOutputDir, manifest.Shots)...)

	var runMetadata RunMetadata
	runLoaded, issue, err := readInspectOptionalJSON(artifacts.RunMetadata, &runMetadata)
	if err != nil {
		return InspectSummary{}, err
	}
	if runLoaded {
		summary.RunMetadata = &runMetadata
	} else if issue != "" {
		summary.Issues = append(summary.Issues, issue)
	} else {
		summary.MissingArtifacts = append(summary.MissingArtifacts, runMetadataFilename)
	}

	var renderMetadata RenderMetadata
	renderLoaded, issue, err := readInspectOptionalJSON(artifacts.RenderMetadata, &renderMetadata)
	if err != nil {
		return InspectSummary{}, err
	}
	if renderLoaded {
		summary.RenderMetadata = &renderMetadata
	} else if issue != "" {
		summary.Issues = append(summary.Issues, issue)
	} else {
		summary.MissingArtifacts = append(summary.MissingArtifacts, renderMetadataFilename)
	}

	outputExists, err := inspectArtifactExists(artifacts.OutputVideo)
	if err != nil {
		return InspectSummary{}, err
	}
	summary.Artifacts.OutputVideoExists = outputExists
	if !outputExists {
		summary.MissingArtifacts = append(summary.MissingArtifacts, outputVideoFilename)
	}

	previewExists, err := inspectArtifactExists(artifacts.PreviewFrame)
	if err != nil {
		return InspectSummary{}, err
	}
	summary.Artifacts.PreviewFrameExists = previewExists
	if !previewExists {
		summary.MissingArtifacts = append(summary.MissingArtifacts, previewFrameFilename)
	}

	return finalizeInspectStatus(summary), nil
}

func missingProductionArtifacts(outputDir string, shots []Shot) []string {
	var missing []string
	for _, shot := range shots {
		rawShotPath := filepath.ToSlash(filepath.Join("shots", fmt.Sprintf("shot_%03d.mp4", shot.Index)))
		if !inspectArtifactPresent(filepath.Join(outputDir, rawShotPath)) {
			missing = append(missing, rawShotPath)
		}

		normalizedPath := filepath.ToSlash(filepath.Join("normalized", fmt.Sprintf("shot_%03d.mp4", shot.Index)))
		if !inspectArtifactPresent(filepath.Join(outputDir, normalizedPath)) {
			missing = append(missing, normalizedPath)
		}
	}
	if !inspectArtifactPresent(filepath.Join(outputDir, "hyperframes", "index.html")) {
		missing = append(missing, "hyperframes/index.html")
	}
	if !inspectArtifactPresent(filepath.Join(outputDir, "hyperframes", "package.json")) {
		missing = append(missing, "hyperframes/package.json")
	}
	if !inspectArtifactPresent(filepath.Join(outputDir, "timeline_hyperframes.mp4")) {
		missing = append(missing, "timeline_hyperframes.mp4")
	}
	return missing
}

func inspectArtifactPresent(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir() && info.Size() > 0
}

func singleOutputLegacyArtifactIssues(outputDir string) []string {
	if inspectPathExists(filepath.Join(outputDir, "remotion_props.json")) {
		return []string{"legacy remotion_props.json artifact is not allowed in xAI-native output"}
	}
	return nil
}

func inspectPathExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func inspectShots(shots []Shot) []InspectShot {
	if len(shots) == 0 {
		return nil
	}
	summaries := make([]InspectShot, 0, len(shots))
	for _, shot := range shots {
		summaries = append(summaries, InspectShot{
			Index:         shot.Index,
			Prompt:        shot.Prompt,
			Subtitle:      shot.Subtitle,
			TransitionOut: shot.TransitionOut,
			VideoPath:     shot.VideoPath,
			PromptHash:    shot.PromptHash,
			XAIRequestID:  shot.XAIRequestID,
			XAIStatus:     shot.XAIStatus,
			DurationSec:   shot.DurationSec,
			AspectRatio:   shot.AspectRatio,
			Resolution:    shot.Resolution,
		})
	}
	return summaries
}

func readInspectRequiredJSON(path string, target any) (bool, string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return false, fmt.Sprintf("missing required %s", filepath.Base(path)), nil
		}
		return false, "", fmt.Errorf("read %s: %w", filepath.Base(path), err)
	}
	if err := json.Unmarshal(data, target); err != nil {
		return true, fmt.Sprintf("parse %s: %v", filepath.Base(path), err), nil
	}
	return true, "", nil
}

func readInspectOptionalJSON(path string, target any) (bool, string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return false, "", nil
		}
		return false, "", fmt.Errorf("read %s: %w", filepath.Base(path), err)
	}
	if err := json.Unmarshal(data, target); err != nil {
		return false, fmt.Sprintf("parse %s: %v", filepath.Base(path), err), nil
	}
	return true, "", nil
}

func inspectArtifactExists(path string) (bool, error) {
	info, err := os.Stat(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return false, nil
		}
		return false, fmt.Errorf("stat %s: %w", filepath.Base(path), err)
	}
	return !info.IsDir(), nil
}

func finalizeInspectStatus(summary InspectSummary) InspectSummary {
	switch {
	case len(summary.Issues) > 0:
		summary.Status = InspectStatusInvalid
	case len(summary.MissingArtifacts) > 0:
		summary.Status = InspectStatusPartial
	default:
		summary.Status = InspectStatusComplete
	}
	return summary
}
