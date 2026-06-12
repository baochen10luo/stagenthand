package xaipipeline

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"html/template"
	"os"
	"path/filepath"
	"strings"
)

type HyperFramesExecutor interface {
	RenderWithFPS(ctx context.Context, projectDir string, outputPath string, fps int) error
}

type VideoFinalizer interface {
	Finalize(ctx context.Context, inputPath string, outputPath string) error
}

type ShotNormalizer interface {
	Normalize(ctx context.Context, inputPath string, outputPath string, spec RenderSpec) error
}

type PreviewExtractor interface {
	Extract(ctx context.Context, inputPath string, outputPath string) error
}

type RenderSpec struct {
	Width  int
	Height int
	FPS    int
}

type HyperFramesFFmpegRenderer struct {
	executor        HyperFramesExecutor
	finalizer       VideoFinalizer
	normalizer      ShotNormalizer
	outputValidator OutputValidator
	previewer       PreviewExtractor
}

func NewHyperFramesFFmpegRenderer(executor HyperFramesExecutor, finalizer VideoFinalizer) *HyperFramesFFmpegRenderer {
	if finalizer == nil {
		finalizer = FFmpegFinalizer{}
	}
	return &HyperFramesFFmpegRenderer{
		executor:  executor,
		finalizer: finalizer,
	}
}

func (r *HyperFramesFFmpegRenderer) WithShotNormalizer(normalizer ShotNormalizer) *HyperFramesFFmpegRenderer {
	r.normalizer = normalizer
	return r
}

func (r *HyperFramesFFmpegRenderer) WithOutputValidator(validator OutputValidator) *HyperFramesFFmpegRenderer {
	r.outputValidator = validator
	return r
}

func (r *HyperFramesFFmpegRenderer) WithPreviewExtractor(extractor PreviewExtractor) *HyperFramesFFmpegRenderer {
	r.previewer = extractor
	return r
}

func (r *HyperFramesFFmpegRenderer) Render(ctx context.Context, manifest Manifest, outputDir string) (string, error) {
	ctx = contextOrBackground(ctx)
	if r.executor == nil {
		return "", errors.New("hyperframes executor is nil")
	}
	if r.finalizer == nil {
		return "", errors.New("video finalizer is nil")
	}
	if len(manifest.Shots) == 0 {
		return "", errors.New("xai render manifest has no shots")
	}
	if r.normalizer == nil {
		return "", errors.New("xai render shot normalizer is nil")
	}
	if r.outputValidator == nil {
		return "", errors.New("xai render output validator is nil")
	}
	if r.previewer == nil {
		return "", errors.New("xai render preview extractor is nil")
	}
	if err := validateRenderManifestSpec(manifest); err != nil {
		return "", err
	}
	if err := ctx.Err(); err != nil {
		return "", err
	}
	manifestHash, err := manifestIdentityHash(manifest)
	if err != nil {
		return "", err
	}
	manifestProjectID := manifest.ProjectID
	absOutputDir, err := filepath.Abs(outputDir)
	if err != nil {
		return "", fmt.Errorf("resolve xai render output dir: %w", err)
	}
	outputDir = absOutputDir

	renderManifest := cloneRenderManifest(manifest)
	normalized, err := r.normalizeShots(ctx, renderManifest, outputDir)
	if err != nil {
		return "", err
	}
	renderManifest = normalized
	if err := r.validateNormalizedShots(ctx, renderManifest, outputDir); err != nil {
		return "", err
	}

	projectDir := filepath.Join(outputDir, "hyperframes")
	if err := os.MkdirAll(projectDir, 0755); err != nil {
		return "", fmt.Errorf("create hyperframes project dir: %w", err)
	}
	if err := writeHyperFramesVideoProject(renderManifest, outputDir, projectDir); err != nil {
		return "", err
	}
	if err := ctx.Err(); err != nil {
		return "", err
	}

	timelinePath := filepath.Join(outputDir, "timeline_hyperframes.mp4")
	timelineFile, err := r.stageHyperFramesTimeline(ctx, renderManifest, outputDir, projectDir, timelinePath)
	if err != nil {
		return "", err
	}
	timelineFiles := []*stagedArtifactFile{timelineFile}
	defer cleanupStagedArtifactFiles(timelineFiles)
	if err := ctx.Err(); err != nil {
		return "", err
	}
	if err := commitStagedArtifactFiles(timelineFiles); err != nil {
		return "", err
	}
	if err := ctx.Err(); err != nil {
		return "", err
	}

	outputPath := filepath.Join(outputDir, "output_xai.mp4")
	finalFile, metadata, err := stageFinalOutput(ctx, r.finalizer, r.outputValidator, outputDir, timelinePath, outputPath, renderValidationSpec(renderManifest))
	if err != nil {
		return "", err
	}
	finalFiles := []*stagedArtifactFile{finalFile}
	defer func() {
		cleanupStagedArtifactFiles(finalFiles)
	}()
	metadata.Path = outputPath
	metadata.ProjectID = manifestProjectID
	metadata.ManifestHash = manifestHash
	metadataFile, err := stageRenderMetadata(outputDir, metadata)
	if err != nil {
		return "", err
	}
	finalFiles = append(finalFiles, metadataFile)
	if err := ctx.Err(); err != nil {
		return "", err
	}
	previewPath := filepath.Join(outputDir, "preview_frame.jpg")
	previewFile, err := stagePreviewFrame(ctx, r.previewer, finalFile.tempPath, previewPath, renderValidationSpec(renderManifest))
	if err != nil {
		return "", err
	}
	finalFiles = append(finalFiles, previewFile)
	if err := ctx.Err(); err != nil {
		return "", err
	}
	if err := commitStagedArtifactFiles(finalFiles); err != nil {
		return "", err
	}
	return outputPath, nil
}

func cloneRenderManifest(manifest Manifest) Manifest {
	cloned := manifest
	if len(manifest.Shots) > 0 {
		cloned.Shots = append([]Shot(nil), manifest.Shots...)
	}
	return cloned
}

func validateRenderManifestSpec(manifest Manifest) error {
	if err := validateProjectID(manifest.ProjectID); err != nil {
		return err
	}
	format := strings.TrimSpace(manifest.Format)
	if manifest.Format != "" && manifest.Format != format {
		return fmt.Errorf("xai render format %q is not canonical, want %q", manifest.Format, format)
	}
	if format == "" {
		format = defaultFormat
	}
	if format != defaultFormat {
		return fmt.Errorf("xai render format %q, want %q", manifest.Format, defaultFormat)
	}

	width := manifest.Width
	if width == 0 {
		width = defaultWidth
	}
	height := manifest.Height
	if height == 0 {
		height = defaultHeight
	}
	if width != defaultWidth || height != defaultHeight {
		return fmt.Errorf("xai render dimensions %dx%d, want %dx%d", width, height, defaultWidth, defaultHeight)
	}

	fps := manifest.FPS
	if fps == 0 {
		fps = defaultFPS
	}
	if fps != defaultFPS {
		return fmt.Errorf("xai render fps %d, want %d", fps, defaultFPS)
	}
	if err := validateManifestShotIndexes(manifest.Shots); err != nil {
		return err
	}
	for _, shot := range manifest.Shots {
		if shot.VideoPath == "" {
			return fmt.Errorf("xai render shot %d has no video_path", shot.Index)
		}
		wantPath := canonicalShotVideoPath(shot.Index)
		if shot.VideoPath != wantPath {
			return fmt.Errorf("xai render shot %d video_path %q, want %q", shot.Index, shot.VideoPath, wantPath)
		}
		if shot.DurationSec <= 0 {
			return fmt.Errorf("xai render shot %d duration_sec %.3f, want positive duration", shot.Index, shot.DurationSec)
		}
		prompt := strings.TrimSpace(shot.Prompt)
		if prompt == "" {
			return fmt.Errorf("xai render shot %d prompt is empty", shot.Index)
		}
		if shot.Prompt != prompt {
			return fmt.Errorf("xai render shot %d prompt %q is not canonical, want %q", shot.Index, shot.Prompt, prompt)
		}
		aspectRatio := strings.TrimSpace(shot.AspectRatio)
		if shot.AspectRatio != "" && (shot.AspectRatio != aspectRatio || aspectRatio != defaultAspectRatio) {
			return fmt.Errorf("xai render shot %d aspect_ratio %q, want %q", shot.Index, shot.AspectRatio, defaultAspectRatio)
		}
		resolution := strings.TrimSpace(shot.Resolution)
		if shot.Resolution != "" && (shot.Resolution != resolution || resolution != defaultResolution) {
			return fmt.Errorf("xai render shot %d resolution %q, want %q", shot.Index, shot.Resolution, defaultResolution)
		}
		subtitle := strings.TrimSpace(shot.Subtitle)
		if shot.Subtitle != subtitle {
			return fmt.Errorf("xai render shot %d subtitle %q is not canonical, want %q", shot.Index, shot.Subtitle, subtitle)
		}
		transition := strings.TrimSpace(shot.TransitionOut)
		if transition == "" {
			continue
		}
		if transition != shot.TransitionOut || !isSupportedTransitionOut(transition) {
			return fmt.Errorf("xai render shot %d transition_out %q is not supported: want cut or fade", shot.Index, shot.TransitionOut)
		}
	}
	return nil
}

func (r *HyperFramesFFmpegRenderer) normalizeShots(ctx context.Context, manifest Manifest, outputDir string) (Manifest, error) {
	normalizedDir := filepath.Join(outputDir, "normalized")
	if err := os.MkdirAll(normalizedDir, 0755); err != nil {
		return manifest, fmt.Errorf("create normalized shots dir: %w", err)
	}

	spec := RenderSpec{
		Width:  manifest.Width,
		Height: manifest.Height,
		FPS:    manifest.FPS,
	}
	if spec.Width == 0 {
		spec.Width = defaultWidth
	}
	if spec.Height == 0 {
		spec.Height = defaultHeight
	}
	if spec.FPS == 0 {
		spec.FPS = defaultFPS
	}

	for i := range manifest.Shots {
		shot := &manifest.Shots[i]
		if shot.VideoPath == "" {
			return manifest, fmt.Errorf("xai render shot %d has no video_path", shot.Index)
		}
		wantPath := canonicalShotVideoPath(shot.Index)
		if shot.VideoPath != wantPath {
			return manifest, fmt.Errorf("xai render shot %d video_path %q, want %q", shot.Index, shot.VideoPath, wantPath)
		}
	}

	var stagedFiles []*stagedArtifactFile
	defer cleanupStagedArtifactFiles(stagedFiles)
	for i := range manifest.Shots {
		shot := &manifest.Shots[i]
		inputPath := canonicalRawShotPath(outputDir, shot.Index)
		relPath := filepath.ToSlash(filepath.Join("normalized", fmt.Sprintf("shot_%03d.mp4", shot.Index)))
		outputPath := filepath.Join(outputDir, relPath)
		staged, err := r.stageNormalizedShot(ctx, manifest, *shot, outputDir, inputPath, outputPath, spec)
		if err != nil {
			return manifest, err
		}
		stagedFiles = append(stagedFiles, staged)
		shot.VideoPath = relPath
	}
	if err := commitStagedArtifactFiles(stagedFiles); err != nil {
		return manifest, err
	}
	if err := ctx.Err(); err != nil {
		return manifest, err
	}
	return manifest, nil
}

func (r *HyperFramesFFmpegRenderer) stageNormalizedShot(ctx context.Context, manifest Manifest, shot Shot, outputDir string, inputPath string, outputPath string, spec RenderSpec) (*stagedArtifactFile, error) {
	tempFile, err := createMediaArtifactTempFile(outputPath)
	if err != nil {
		return nil, fmt.Errorf("normalize xai shot %d: create temporary file: %w", shot.Index, err)
	}
	tempPath := tempFile.Name()
	removeTemp := true
	defer func() {
		if removeTemp {
			_ = os.Remove(tempPath)
		}
	}()
	if err := tempFile.Close(); err != nil {
		return nil, fmt.Errorf("normalize xai shot %d: close temporary file: %w", shot.Index, err)
	}

	if err := r.normalizer.Normalize(ctx, inputPath, tempPath, spec); err != nil {
		return nil, fmt.Errorf("normalize xai shot %d: %w", shot.Index, err)
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	_, issues := validateFFprobeMetadata(
		ctx,
		r.outputValidator,
		outputDir,
		tempPath,
		renderValidationSpecForShot(manifest, shot),
		fmt.Sprintf("validate normalized xai shot %d", shot.Index),
		fmt.Sprintf("validate normalized xai shot %d metadata", shot.Index),
	)
	if len(issues) > 0 {
		return nil, errors.New(issues[0])
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	removeTemp = false
	return &stagedArtifactFile{
		label:      fmt.Sprintf("write normalized xai shot %d", shot.Index),
		tempPath:   tempPath,
		outputPath: outputPath,
	}, nil
}

func (r *HyperFramesFFmpegRenderer) validateNormalizedShots(ctx context.Context, manifest Manifest, outputDir string) error {
	for _, shot := range manifest.Shots {
		path := filepath.Join(outputDir, shot.VideoPath)
		spec := renderValidationSpecForShot(manifest, shot)
		_, issues := validateFFprobeMetadata(
			ctx,
			r.outputValidator,
			outputDir,
			path,
			spec,
			fmt.Sprintf("validate normalized xai shot %d", shot.Index),
			fmt.Sprintf("validate normalized xai shot %d metadata", shot.Index),
		)
		if len(issues) > 0 {
			return errors.New(issues[0])
		}
		if err := ctx.Err(); err != nil {
			return err
		}
	}
	return nil
}

func (r *HyperFramesFFmpegRenderer) validateHyperFramesTimeline(ctx context.Context, manifest Manifest, outputDir string, timelinePath string) error {
	spec := renderValidationSpec(manifest)
	spec.CodecName = ""
	spec.PixelFormat = ""
	_, issues := validateFFprobeMetadata(
		ctx,
		r.outputValidator,
		outputDir,
		timelinePath,
		spec,
		"validate hyperframes timeline",
		"validate hyperframes timeline metadata",
	)
	if len(issues) > 0 {
		return errors.New(issues[0])
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	return nil
}

func (r *HyperFramesFFmpegRenderer) stageHyperFramesTimeline(ctx context.Context, manifest Manifest, outputDir string, projectDir string, outputPath string) (*stagedArtifactFile, error) {
	tempFile, err := createMediaArtifactTempFile(outputPath)
	if err != nil {
		return nil, fmt.Errorf("hyperframes render: create temporary file: %w", err)
	}
	tempPath := tempFile.Name()
	removeTemp := true
	defer func() {
		if removeTemp {
			_ = os.Remove(tempPath)
		}
	}()
	if err := tempFile.Close(); err != nil {
		return nil, fmt.Errorf("hyperframes render: close temporary file: %w", err)
	}

	if err := r.executor.RenderWithFPS(ctx, projectDir, tempPath, renderFPS(manifest)); err != nil {
		return nil, fmt.Errorf("hyperframes render: %w", err)
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if err := r.validateHyperFramesTimeline(ctx, manifest, outputDir, tempPath); err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	removeTemp = false
	return &stagedArtifactFile{
		label:      "write hyperframes timeline",
		tempPath:   tempPath,
		outputPath: outputPath,
	}, nil
}

func renderValidationSpec(manifest Manifest) RenderValidationSpec {
	spec := RenderValidationSpec{
		Width:          manifest.Width,
		Height:         manifest.Height,
		FPS:            manifest.FPS,
		CodecName:      defaultCodecName,
		PixelFormat:    defaultPixelFormat,
		RequireNoAudio: true,
	}
	if spec.Width == 0 {
		spec.Width = defaultWidth
	}
	if spec.Height == 0 {
		spec.Height = defaultHeight
	}
	if spec.FPS == 0 {
		spec.FPS = defaultFPS
	}
	for _, shot := range manifest.Shots {
		duration := shot.DurationSec
		if duration <= 0 {
			duration = defaultDurationSec
		}
		spec.ExpectedDurationSec += duration
	}
	return spec
}

func renderValidationSpecForShot(manifest Manifest, shot Shot) RenderValidationSpec {
	spec := renderValidationSpec(manifest)
	if shot.DurationSec > 0 {
		spec.ExpectedDurationSec = shot.DurationSec
	} else {
		spec.ExpectedDurationSec = defaultDurationSec
	}
	return spec
}

func renderFPS(manifest Manifest) int {
	if manifest.FPS <= 0 {
		return defaultFPS
	}
	return manifest.FPS
}

func stageRenderMetadata(outputDir string, metadata RenderMetadata) (*stagedArtifactFile, error) {
	data, err := json.MarshalIndent(metadata, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("marshal render metadata: %w", err)
	}
	data = append(data, '\n')
	file, err := stageArtifactFile("write render metadata", filepath.Join(outputDir, "render_metadata.json"), data)
	if err != nil {
		return nil, err
	}
	return file, nil
}

func stageFinalOutput(ctx context.Context, finalizer VideoFinalizer, validator OutputValidator, outputDir string, inputPath string, outputPath string, spec RenderValidationSpec) (*stagedArtifactFile, RenderMetadata, error) {
	tempFile, err := createMediaArtifactTempFile(outputPath)
	if err != nil {
		return nil, RenderMetadata{}, fmt.Errorf("ffmpeg finalize: create temporary file: %w", err)
	}
	tempPath := tempFile.Name()
	removeTemp := true
	defer func() {
		if removeTemp {
			_ = os.Remove(tempPath)
		}
	}()
	if err := tempFile.Close(); err != nil {
		return nil, RenderMetadata{}, fmt.Errorf("ffmpeg finalize: close temporary file: %w", err)
	}

	if err := finalizer.Finalize(ctx, inputPath, tempPath); err != nil {
		return nil, RenderMetadata{}, fmt.Errorf("ffmpeg finalize: %w", err)
	}
	if err := ctx.Err(); err != nil {
		return nil, RenderMetadata{}, err
	}
	metadata, issues := validateFFprobeMetadata(ctx, validator, outputDir, tempPath, spec, "validate final output", "validate final output metadata")
	if len(issues) > 0 {
		return nil, RenderMetadata{}, errors.New(issues[0])
	}
	if err := ctx.Err(); err != nil {
		return nil, RenderMetadata{}, err
	}
	removeTemp = false
	return &stagedArtifactFile{
		label:      "write final output",
		tempPath:   tempPath,
		outputPath: outputPath,
	}, metadata, nil
}

func stagePreviewFrame(ctx context.Context, previewer PreviewExtractor, inputPath string, outputPath string, spec RenderValidationSpec) (*stagedArtifactFile, error) {
	tempFile, err := createMediaArtifactTempFile(outputPath)
	if err != nil {
		return nil, fmt.Errorf("extract preview frame: create temporary file: %w", err)
	}
	tempPath := tempFile.Name()
	removeTemp := true
	defer func() {
		if removeTemp {
			_ = os.Remove(tempPath)
		}
	}()
	if err := tempFile.Close(); err != nil {
		return nil, fmt.Errorf("extract preview frame: close temporary file: %w", err)
	}

	if err := previewer.Extract(ctx, inputPath, tempPath); err != nil {
		return nil, fmt.Errorf("extract preview frame: %w", err)
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if issues := validatePreviewFrameArtifact(tempPath, spec); len(issues) > 0 {
		return nil, fmt.Errorf("validate preview frame: %s", issues[0])
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	removeTemp = false
	return &stagedArtifactFile{
		label:      "write preview frame",
		tempPath:   tempPath,
		outputPath: outputPath,
	}, nil
}

func createMediaArtifactTempFile(outputPath string) (*os.File, error) {
	suffix := ".tmp"
	if ext := filepath.Ext(outputPath); ext != "" {
		suffix += ext
	}
	return os.CreateTemp(filepath.Dir(outputPath), fmt.Sprintf(".%s_*%s", filepath.Base(outputPath), suffix))
}

type hyperFramesVideoData struct {
	ProjectID     string
	Width         int
	Height        int
	TotalDuration float64
	Shots         []hyperFramesVideoShot
}

type hyperFramesVideoShot struct {
	Index              int
	VideoTrackIndex    int
	SubtitleTrackIndex int
	VideoSrc           string
	StartSec           float64
	DurationSec        float64
	Subtitle           string
	TransitionOut      string
}

func writeHyperFramesVideoProject(manifest Manifest, outputDir string, projectDir string) error {
	data, err := buildHyperFramesVideoData(manifest, outputDir, projectDir)
	if err != nil {
		return err
	}

	tmpl, err := template.New("xai-video").Parse(hyperFramesVideoTemplate)
	if err != nil {
		return fmt.Errorf("parse xai hyperframes template: %w", err)
	}
	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, data); err != nil {
		return fmt.Errorf("execute xai hyperframes template: %w", err)
	}
	packageJSON, err := json.Marshal(hyperFramesPackageManifest{
		Name:    hyperFramesPackageName,
		Version: hyperFramesPackageVersion,
		Private: true,
	})
	if err != nil {
		return fmt.Errorf("marshal xai hyperframes package.json: %w", err)
	}

	indexFile, err := stageArtifactFile("write xai hyperframes index.html", filepath.Join(projectDir, "index.html"), buf.Bytes())
	if err != nil {
		return err
	}
	packageFile, err := stageArtifactFile("write xai hyperframes package.json", filepath.Join(projectDir, "package.json"), packageJSON)
	if err != nil {
		cleanupStagedArtifactFiles([]*stagedArtifactFile{indexFile})
		return err
	}
	files := []*stagedArtifactFile{indexFile, packageFile}
	defer cleanupStagedArtifactFiles(files)
	return commitStagedArtifactFiles(files)
}

func buildHyperFramesVideoData(manifest Manifest, outputDir string, projectDir string) (hyperFramesVideoData, error) {
	width := manifest.Width
	if width == 0 {
		width = defaultWidth
	}
	height := manifest.Height
	if height == 0 {
		height = defaultHeight
	}

	data := hyperFramesVideoData{
		ProjectID: manifest.ProjectID,
		Width:     width,
		Height:    height,
		Shots:     make([]hyperFramesVideoShot, 0, len(manifest.Shots)),
	}
	cursor := 0.0
	for _, shot := range manifest.Shots {
		if shot.VideoPath == "" {
			return data, fmt.Errorf("xai render shot %d has no video_path", shot.Index)
		}
		duration := shot.DurationSec
		if duration <= 0 {
			duration = defaultDurationSec
		}
		src, err := timelineVideoSrc(outputDir, projectDir, shot.VideoPath)
		if err != nil {
			return data, fmt.Errorf("resolve xai render shot %d path: %w", shot.Index, err)
		}
		data.Shots = append(data.Shots, hyperFramesVideoShot{
			Index:              shot.Index,
			VideoTrackIndex:    len(data.Shots) * 2,
			SubtitleTrackIndex: len(data.Shots)*2 + 1,
			VideoSrc:           src,
			StartSec:           cursor,
			DurationSec:        duration,
			Subtitle:           shot.Subtitle,
			TransitionOut:      shot.TransitionOut,
		})
		cursor += duration
	}
	data.TotalDuration = cursor
	return data, nil
}

func timelineVideoSrc(outputDir string, projectDir string, videoPath string) (string, error) {
	absPath := videoPath
	if !filepath.IsAbs(absPath) {
		absPath = filepath.Join(outputDir, videoPath)
	}
	rel, err := filepath.Rel(projectDir, absPath)
	if err != nil {
		return "", err
	}
	return filepath.ToSlash(rel), nil
}

type FFmpegFinalizer struct{}

func (f FFmpegFinalizer) Finalize(ctx context.Context, inputPath string, outputPath string) error {
	cmd, err := mediaToolCommand(ctx, "ffmpeg",
		"-i", inputPath,
		"-c:v", "libx264",
		"-pix_fmt", "yuv420p",
		"-movflags", "+faststart",
		"-an",
		"-y",
		outputPath,
	)
	if err != nil {
		return err
	}
	cmd.Stdout = os.Stderr
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("ffmpeg finalize failed: %w", err)
	}
	return nil
}

const hyperFramesVideoTemplate = `<!DOCTYPE html>
<html lang="en">
<head>
  <meta charset="UTF-8">
  <title>{{.ProjectID}}</title>
  <link rel="icon" href="data:,">
  <style>
    * { margin: 0; padding: 0; box-sizing: border-box; }
    body { width: {{.Width}}px; height: {{.Height}}px; overflow: hidden; background: #000; }
    .shot-video {
      position: absolute;
      inset: 0;
      width: {{.Width}}px;
      height: {{.Height}}px;
      background: #000;
      object-fit: cover;
      display: none;
      opacity: 0;
    }
    .subtitle {
      position: absolute;
      left: 8%;
      right: 8%;
      bottom: 9%;
      color: #fff;
      font-family: Arial, sans-serif;
      font-size: 38px;
      line-height: 1.45;
      text-align: center;
      text-shadow: 0 2px 16px rgba(0,0,0,0.9), 0 0 32px rgba(0,0,0,0.75);
      white-space: pre-wrap;
      display: none;
      opacity: 0;
    }
  </style>
</head>
<body>
  <div data-composition-id="xai-video"
       data-start="0"
       data-width="{{.Width}}"
       data-height="{{.Height}}"
       data-duration="{{printf "%.3f" .TotalDuration}}">
{{range .Shots}}    <video id="video-{{.Index}}" class="shot-video clip" src="{{.VideoSrc}}" data-start="{{printf "%.3f" .StartSec}}" data-duration="{{printf "%.3f" .DurationSec}}" data-track-index="{{.VideoTrackIndex}}"{{if .TransitionOut}} data-transition-out="{{.TransitionOut}}"{{end}} preload="auto" muted playsinline></video>
{{if .Subtitle}}    <div id="subtitle-{{.Index}}" class="subtitle clip" data-start="{{printf "%.3f" .StartSec}}" data-duration="{{printf "%.3f" .DurationSec}}" data-track-index="{{.SubtitleTrackIndex}}">{{.Subtitle}}</div>
{{end}}
{{end}}  </div>
  <script>
  (function () {
    var shots = [
{{range .Shots}}      { video: document.getElementById("video-{{.Index}}"), subtitle: document.getElementById("subtitle-{{.Index}}"), start: {{printf "%.3f" .StartSec}}, duration: {{printf "%.3f" .DurationSec}}, transitionOut: "{{.TransitionOut}}" },
{{end}}    ];
    var fadeSeconds = 0.4;
    for (var i = 0; i < shots.length; i++) {
      shots[i].previousTransitionOut = i > 0 ? shots[i - 1].transitionOut : "cut";
    }

    function clamp01(value) {
      return Math.max(0, Math.min(1, value));
    }

    function transitionOpacity(shot, local) {
      var opacity = 1;
      if (shot.previousTransitionOut === "fade" && local < fadeSeconds) {
        opacity *= clamp01(local / fadeSeconds);
      }
      if (shot.transitionOut === "fade" && shot.duration - local < fadeSeconds) {
        opacity *= clamp01((shot.duration - local) / fadeSeconds);
      }
      return opacity;
    }

    function applyShotVisibility(shot, opacity) {
      var display = opacity > 0.001 ? "block" : "none";
      shot.video.style.display = display;
      shot.video.style.opacity = String(opacity);
      if (shot.subtitle) {
        shot.subtitle.style.display = display;
        shot.subtitle.style.opacity = String(opacity);
      }
    }

    function seek(t) {
      t = Number(t) || 0;
      for (var i = 0; i < shots.length; i++) {
        var shot = shots[i];
        var start = Number(shot.start);
        var duration = Number(shot.duration);
        var local = t - start;
        var active = local >= 0 && local < duration;
        var opacity = 0;
        if (active && Number.isFinite(local)) {
          var target = Math.max(0, Math.min(local, Math.max(0, duration - 0.001)));
          if (Math.abs(shot.video.currentTime - target) > 0.03) {
            shot.video.currentTime = target;
          }
          shot.duration = duration;
          opacity = transitionOpacity(shot, local);
        }
        applyShotVisibility(shot, opacity);
      }
    }

    window.__timelines = window.__timelines || {};
    var timeline = {
      seek: function (t) { seek(Number(t) || 0); return timeline; },
      pause: function () { return timeline; }
    };
    window.__timelines["xai-video"] = timeline;
    window.__hf = { seek: timeline.seek };
    timeline.seek(0);
  }());
  </script>
</body>
</html>`
