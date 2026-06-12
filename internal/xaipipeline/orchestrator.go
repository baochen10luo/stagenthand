package xaipipeline

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

type Orchestrator struct {
	deps Deps
}

func NewOrchestrator(deps Deps) *Orchestrator {
	return &Orchestrator{deps: deps}
}

func (o *Orchestrator) Run(ctx context.Context, story []byte, opts RunOptions) (*Result, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if o.deps.Renderer == nil {
		return nil, errors.New("xai pipeline renderer is nil")
	}

	storyText := strings.TrimSpace(string(story))
	if storyText == "" {
		return nil, errors.New("xai pipeline story is empty")
	}
	if err := validateRunOptions(opts); err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	format := normalizeRequestedFormat(opts.Format)
	if err := validateSupportedFormat(format); err != nil {
		return nil, err
	}
	videoModel := normalizeVideoModel(opts.VideoModel)

	if outputDir := strings.TrimSpace(opts.OutputDir); outputDir != "" {
		if err := validateOutputRootPath(outputDir, "xai output dir", "xAI-native output root"); err != nil {
			return nil, err
		}
	} else {
		outputBaseDir, err := resolveOutputBaseDir(opts)
		if err != nil {
			return nil, err
		}
		if err := validateOutputRootPath(outputBaseDir, "xai output base dir", "xAI-native output base"); err != nil {
			return nil, err
		}
	}

	if outputDir := strings.TrimSpace(opts.OutputDir); outputDir != "" && !opts.ForceReplan {
		if manifest, ok := reusableManifestForInput(ctx, outputDir, storyText, format, videoModel, opts.TargetShots, o.deps.Validator, opts.ForceRegenerate); ok {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
			metadata := RunMetadata{
				ManifestReused:  true,
				VideoModel:      manifest.VideoModel,
				ForceReplan:     opts.ForceReplan,
				ForceRegenerate: opts.ForceRegenerate,
			}
			return o.runManifest(ctx, manifest, outputDir, storyText, opts.ForceRegenerate, metadata)
		}
	}

	if o.deps.Planner == nil {
		return nil, errors.New("xai pipeline planner is nil")
	}
	manifest, err := o.deps.Planner.Plan(ctx, PlanInput{
		Story:       storyText,
		TargetShots: opts.TargetShots,
		Format:      format,
	})
	if err != nil {
		return nil, fmt.Errorf("xai plan: %w", err)
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if err := normalizeManifest(&manifest, format, videoModel); err != nil {
		return nil, err
	}
	if err := validateTargetShots(manifest, opts.TargetShots); err != nil {
		return nil, err
	}

	outputDir, err := resolveOutputDir(opts, manifest.ProjectID)
	if err != nil {
		return nil, err
	}
	metadata := RunMetadata{
		Planned:         true,
		VideoModel:      manifest.VideoModel,
		ForceReplan:     opts.ForceReplan,
		ForceRegenerate: opts.ForceRegenerate,
	}
	return o.runManifest(ctx, manifest, outputDir, storyText, opts.ForceRegenerate, metadata)
}

func validateRunOptions(opts RunOptions) error {
	if opts.TargetShots < 0 {
		return errors.New("xai pipeline target shots must be zero or greater")
	}
	return nil
}

func normalizeRequestedFormat(format string) string {
	normalized := strings.ToLower(strings.TrimSpace(format))
	if normalized == "" {
		return defaultFormat
	}
	return normalized
}

func normalizeVideoModel(model string) string {
	model = strings.TrimSpace(model)
	if model == "" {
		return defaultVideoModel
	}
	return model
}

func (o *Orchestrator) runManifest(ctx context.Context, manifest Manifest, outputDir string, storyText string, forceRegenerate bool, metadata RunMetadata) (*Result, error) {
	if err := validateOutputRootPath(outputDir, "xai output dir", "xAI-native output root"); err != nil {
		return nil, err
	}
	if err := os.MkdirAll(outputDir, 0755); err != nil {
		return nil, fmt.Errorf("create xai output dir: %w", err)
	}
	manifest.StoryHash = storyHash(storyText)
	if metadata.VideoModel == "" {
		metadata.VideoModel = manifest.VideoModel
	}
	committedShots, err := o.generateMissingShots(ctx, outputDir, &manifest, forceRegenerate, &metadata)
	if err != nil {
		return nil, err
	}

	manifestPath := filepath.Join(outputDir, "xai_manifest.json")
	runMetadataPath := filepath.Join(outputDir, "xai_run_metadata.json")
	if err := writeRunArtifacts(manifestPath, manifest, runMetadataPath, metadata); err != nil {
		rollbackCommittedGeneratedShots(committedShots)
		return nil, err
	}
	cleanupCommittedGeneratedShotBackups(committedShots)

	outputVideo, err := o.deps.Renderer.Render(ctx, manifest, outputDir)
	if err != nil {
		return nil, fmt.Errorf("xai render: %w", err)
	}
	return &Result{
		Manifest:           manifest,
		RunMetadata:        metadata,
		OutputDir:          outputDir,
		OutputVideo:        outputVideo,
		ManifestPath:       manifestPath,
		RunMetadataPath:    runMetadataPath,
		RenderMetadataPath: filepath.Join(outputDir, "render_metadata.json"),
		PreviewFramePath:   filepath.Join(outputDir, "preview_frame.jpg"),
	}, nil
}

func reusableManifestForInput(ctx context.Context, outputDir string, storyText string, format string, videoModel string, targetShots int, validator ShotValidator, forceRegenerate bool) (Manifest, bool) {
	manifest, ok := readManifest(filepath.Join(outputDir, "xai_manifest.json"))
	if !ok {
		return Manifest{}, false
	}
	if forceRegenerate {
		manifest.VideoModel = normalizeVideoModel(videoModel)
	}
	if err := normalizeManifest(&manifest, format, videoModel); err != nil {
		return Manifest{}, false
	}
	if manifest.StoryHash == "" || manifest.StoryHash != storyHash(storyText) {
		return Manifest{}, false
	}
	if manifest.Format != format {
		return Manifest{}, false
	}
	if err := validateTargetShots(manifest, targetShots); err != nil {
		return Manifest{}, false
	}
	if forceRegenerate {
		return manifest, true
	}
	if !manifestHasReusableShotEvidence(ctx, outputDir, manifest, validator) {
		return Manifest{}, false
	}
	return manifest, true
}

func manifestHasReusableShotEvidence(ctx context.Context, outputDir string, manifest Manifest, validator ShotValidator) bool {
	if validator == nil {
		return false
	}
	for _, shot := range manifest.Shots {
		if shot.VideoPath != canonicalShotVideoPath(shot.Index) {
			return false
		}
		if shot.PromptHash == "" || shot.PromptHash != shotCacheHash(shot, manifest.VideoModel) {
			return false
		}
		if err := validateShotProviderMetadata(shot.Index, shot.XAIRequestID, shot.XAIStatus); err != nil {
			return false
		}
		if !validator.ValidShot(ctx, filepath.Join(outputDir, canonicalShotVideoPath(shot.Index)), shotValidationSpec(shot)) {
			return false
		}
	}
	return true
}

func shotValidationSpec(shot Shot) RenderValidationSpec {
	duration := shot.DurationSec
	if duration <= 0 {
		duration = defaultDurationSec
	}
	return RenderValidationSpec{ExpectedDurationSec: duration}
}

func resolveOutputDir(opts RunOptions, projectID string) (string, error) {
	outputDir := strings.TrimSpace(opts.OutputDir)
	if outputDir != "" {
		return outputDir, nil
	}
	outputBaseDir, err := resolveOutputBaseDir(opts)
	if err != nil {
		return "", err
	}
	return filepath.Join(outputBaseDir, projectID), nil
}

func resolveOutputBaseDir(opts RunOptions) (string, error) {
	shandHome := strings.TrimSpace(opts.ShandHome)
	if shandHome == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("resolve user home for xai output dir: %w", err)
		}
		shandHome = filepath.Join(home, ".shand")
	}
	return filepath.Join(shandHome, "projects"), nil
}

func normalizeManifest(m *Manifest, format string, videoModel string) error {
	m.ProjectID = strings.TrimSpace(m.ProjectID)
	if m.ProjectID == "" {
		return errors.New("xai manifest project_id is empty")
	}
	if err := validateProjectID(m.ProjectID); err != nil {
		return err
	}
	if len(m.Shots) == 0 {
		return errors.New("xai manifest has no shots")
	}
	videoModel = normalizeVideoModel(videoModel)
	if strings.TrimSpace(m.VideoModel) == "" {
		m.VideoModel = videoModel
	} else {
		m.VideoModel = strings.TrimSpace(m.VideoModel)
	}
	if m.VideoModel != videoModel {
		return fmt.Errorf("xai manifest video_model %q does not match requested video model %q", m.VideoModel, videoModel)
	}
	if strings.TrimSpace(m.Format) == "" {
		m.Format = format
	} else {
		m.Format = strings.TrimSpace(m.Format)
	}
	if err := validateSupportedFormat(m.Format); err != nil {
		return err
	}
	if m.Format != format {
		return fmt.Errorf("xai manifest format %q does not match requested format %q", m.Format, format)
	}
	if m.FPS == 0 {
		m.FPS = defaultFPS
	}
	if m.Width == 0 {
		m.Width = defaultWidth
	}
	if m.Height == 0 {
		m.Height = defaultHeight
	}
	if err := validateStableRenderSpec(*m); err != nil {
		return err
	}

	for i := range m.Shots {
		shot := &m.Shots[i]
		if shot.Index == 0 {
			shot.Index = i + 1
		}
		shot.Prompt = strings.TrimSpace(shot.Prompt)
		if shot.Prompt == "" {
			return fmt.Errorf("xai manifest shot %d prompt is empty", shot.Index)
		}
		shot.Subtitle = strings.TrimSpace(shot.Subtitle)
		if shot.DurationSec <= 0 {
			shot.DurationSec = defaultDurationSec
		}
		shot.AspectRatio = strings.TrimSpace(shot.AspectRatio)
		if shot.AspectRatio == "" {
			shot.AspectRatio = defaultAspectRatio
		}
		shot.Resolution = strings.TrimSpace(shot.Resolution)
		if shot.Resolution == "" {
			shot.Resolution = defaultResolution
		}
		if err := validateStableShotGenerationSpec(*shot); err != nil {
			return err
		}
		transition, err := normalizeTransitionOut(shot.TransitionOut)
		if err != nil {
			return fmt.Errorf("xai manifest shot %d %w", shot.Index, err)
		}
		shot.TransitionOut = transition
	}
	if err := validateManifestShotIndexes(m.Shots); err != nil {
		return err
	}
	return nil
}

func validateProjectID(projectID string) error {
	if projectID == "" {
		return errors.New("xai manifest project_id is empty")
	}
	if len(projectID) > 80 {
		return fmt.Errorf("xai manifest project_id %q is too long: want at most 80 characters", projectID)
	}
	for i, r := range projectID {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			continue
		}
		if i > 0 && (r == '-' || r == '_') {
			continue
		}
		return fmt.Errorf("xai manifest project_id %q is not safe for an output directory: use lowercase letters, digits, hyphen, or underscore", projectID)
	}
	return nil
}

func validateStableRenderSpec(m Manifest) error {
	if m.Width != defaultWidth || m.Height != defaultHeight {
		return fmt.Errorf("xai manifest render dimensions %dx%d, want %dx%d", m.Width, m.Height, defaultWidth, defaultHeight)
	}
	if m.FPS != defaultFPS {
		return fmt.Errorf("xai manifest fps %d, want %d", m.FPS, defaultFPS)
	}
	return nil
}

func validateStableShotGenerationSpec(shot Shot) error {
	if shot.AspectRatio != defaultAspectRatio {
		return fmt.Errorf("xai manifest shot %d aspect_ratio %q, want %q", shot.Index, shot.AspectRatio, defaultAspectRatio)
	}
	if shot.Resolution != defaultResolution {
		return fmt.Errorf("xai manifest shot %d resolution %q, want %q", shot.Index, shot.Resolution, defaultResolution)
	}
	return nil
}

func normalizeTransitionOut(value string) (string, error) {
	transition := strings.ToLower(strings.TrimSpace(value))
	if transition == "" {
		return defaultTransition, nil
	}
	if isSupportedTransitionOut(transition) {
		return transition, nil
	}
	return "", fmt.Errorf("transition_out %q is not supported: first stable xAI-native pipeline supports cut or fade only", transition)
}

func isSupportedTransitionOut(transition string) bool {
	switch transition {
	case "cut", "fade":
		return true
	default:
		return false
	}
}

func validateSupportedFormat(format string) error {
	if format != defaultFormat {
		return fmt.Errorf("unsupported xai-native format %q: first stable xAI-native pipeline supports portrait only", format)
	}
	return nil
}

func validateManifestShotIndexes(shots []Shot) error {
	seen := make(map[int]struct{}, len(shots))
	for position, shot := range shots {
		if shot.Index < 1 || shot.Index > len(shots) {
			return fmt.Errorf("xai manifest shot indexes must be contiguous from 1: shot position %d has index %d", position+1, shot.Index)
		}
		if _, ok := seen[shot.Index]; ok {
			return fmt.Errorf("xai manifest duplicate shot index %d", shot.Index)
		}
		seen[shot.Index] = struct{}{}
	}
	for position, shot := range shots {
		wantIndex := position + 1
		if shot.Index != wantIndex {
			return fmt.Errorf("xai manifest shot indexes must match shot order from 1: shot position %d has index %d, want %d", position+1, shot.Index, wantIndex)
		}
	}
	return nil
}

func validateTargetShots(manifest Manifest, targetShots int) error {
	if targetShots <= 0 {
		return nil
	}
	if len(manifest.Shots) != targetShots {
		return fmt.Errorf("xai manifest has %d shots, want exactly %d", len(manifest.Shots), targetShots)
	}
	return nil
}

func (o *Orchestrator) generateMissingShots(ctx context.Context, outputDir string, manifest *Manifest, forceRegenerate bool, metadata *RunMetadata) ([]*stagedGeneratedShot, error) {
	shotsDir := filepath.Join(outputDir, "shots")
	if err := os.MkdirAll(shotsDir, 0755); err != nil {
		return nil, fmt.Errorf("create xai shots dir: %w", err)
	}

	previous, _ := readManifest(filepath.Join(outputDir, "xai_manifest.json"))
	var stagedShots []*stagedGeneratedShot
	defer cleanupStagedGeneratedShots(stagedShots)

	for i := range manifest.Shots {
		shot := &manifest.Shots[i]
		shot.PromptHash = shotCacheHash(*shot, manifest.VideoModel)
		relPath := canonicalShotVideoPath(shot.Index)
		absPath := filepath.Join(outputDir, relPath)
		shot.VideoPath = relPath

		if !forceRegenerate &&
			previousShotMatches(previous, *shot) &&
			previousShotHasProviderMetadata(previous, shot.Index) &&
			o.deps.Validator != nil &&
			o.deps.Validator.ValidShot(ctx, absPath, shotValidationSpec(*shot)) {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
			copyPreviousShotProviderMetadata(previous, shot)
			metadata.ReusedShots = append(metadata.ReusedShots, shot.Index)
			metadata.ShotDecisions = append(metadata.ShotDecisions, shotDecision(*shot, "reused"))
			continue
		}

		if o.deps.ShotGenerator == nil {
			return nil, fmt.Errorf("generate xai shot %d: xai pipeline shot generator is nil", shot.Index)
		}
		result, err := o.deps.ShotGenerator.GenerateShotResult(ctx, *shot)
		if err != nil {
			return nil, fmt.Errorf("generate xai shot %d: %w", shot.Index, err)
		}
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if len(result.Data) == 0 {
			return nil, fmt.Errorf("generate xai shot %d: empty video bytes", shot.Index)
		}
		providerMetadata, err := normalizeShotProviderMetadata(shot.Index, result.RequestID, result.Status)
		if err != nil {
			return nil, fmt.Errorf("generate xai shot %d: %w", shot.Index, err)
		}
		if o.deps.Validator == nil {
			return nil, fmt.Errorf("generate xai shot %d: xai pipeline shot validator is nil", shot.Index)
		}
		staged, err := stageValidatedShot(ctx, o.deps.Validator, shot.Index, absPath, result.Data, shotValidationSpec(*shot))
		if err != nil {
			return nil, fmt.Errorf("generate xai shot %d: %w", shot.Index, err)
		}
		staged.shot = shot
		staged.requestID = providerMetadata.requestID
		staged.status = providerMetadata.status
		stagedShots = append(stagedShots, staged)
	}

	if err := commitStagedGeneratedShots(stagedShots); err != nil {
		return nil, err
	}
	for _, staged := range stagedShots {
		staged.shot.XAIRequestID = staged.requestID
		staged.shot.XAIStatus = staged.status
		metadata.GeneratedShots = append(metadata.GeneratedShots, staged.shot.Index)
		metadata.ShotDecisions = append(metadata.ShotDecisions, shotDecision(*staged.shot, "generated"))
	}
	return stagedShots, nil
}

type stagedGeneratedShot struct {
	shot        *Shot
	tempPath    string
	outputPath  string
	requestID   string
	status      string
	backupPath  string
	hadPrevious bool
	committed   bool
}

func stageValidatedShot(ctx context.Context, validator ShotValidator, shotIndex int, outputPath string, data []byte, spec RenderValidationSpec) (*stagedGeneratedShot, error) {
	tempFile, err := os.CreateTemp(filepath.Dir(outputPath), fmt.Sprintf(".shot_%03d_*.tmp.mp4", shotIndex))
	if err != nil {
		return nil, fmt.Errorf("create temporary xai shot: %w", err)
	}
	tempPath := tempFile.Name()
	removeTemp := true
	defer func() {
		if removeTemp {
			_ = os.Remove(tempPath)
		}
	}()

	if _, err := tempFile.Write(data); err != nil {
		_ = tempFile.Close()
		return nil, fmt.Errorf("write temporary xai shot: %w", err)
	}
	if err := tempFile.Close(); err != nil {
		return nil, fmt.Errorf("close temporary xai shot: %w", err)
	}
	if err := os.Chmod(tempPath, 0644); err != nil {
		return nil, fmt.Errorf("chmod temporary xai shot: %w", err)
	}
	if !validator.ValidShot(ctx, tempPath, spec) {
		return nil, fmt.Errorf("generated xai shot failed validation: %s", tempPath)
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	removeTemp = false
	return &stagedGeneratedShot{
		tempPath:   tempPath,
		outputPath: outputPath,
	}, nil
}

func commitStagedGeneratedShots(shots []*stagedGeneratedShot) error {
	var committed []*stagedGeneratedShot
	for _, shot := range shots {
		if err := commitStagedGeneratedShot(shot); err != nil {
			rollbackCommittedGeneratedShots(committed)
			return fmt.Errorf("commit xai shot %d: %w", shot.shot.Index, err)
		}
		committed = append(committed, shot)
	}
	return nil
}

func commitStagedGeneratedShot(shot *stagedGeneratedShot) error {
	info, err := os.Lstat(shot.outputPath)
	switch {
	case err == nil && info.IsDir():
		return fmt.Errorf("%s is a directory", shot.outputPath)
	case err == nil:
		backupPath, err := createArtifactBackupPath(shot.outputPath)
		if err != nil {
			return err
		}
		if err := os.Rename(shot.outputPath, backupPath); err != nil {
			return fmt.Errorf("backup existing xai shot: %w", err)
		}
		shot.backupPath = backupPath
		shot.hadPrevious = true
	case os.IsNotExist(err):
	default:
		return fmt.Errorf("stat existing xai shot: %w", err)
	}

	if err := os.Rename(shot.tempPath, shot.outputPath); err != nil {
		if shot.hadPrevious {
			_ = os.Rename(shot.backupPath, shot.outputPath)
		}
		return err
	}
	shot.committed = true
	return nil
}

func createArtifactBackupPath(outputPath string) (string, error) {
	backup, err := os.CreateTemp(filepath.Dir(outputPath), fmt.Sprintf(".%s_*.bak", filepath.Base(outputPath)))
	if err != nil {
		return "", fmt.Errorf("create artifact backup path: %w", err)
	}
	backupPath := backup.Name()
	if err := backup.Close(); err != nil {
		_ = os.Remove(backupPath)
		return "", fmt.Errorf("close artifact backup placeholder: %w", err)
	}
	if err := os.Remove(backupPath); err != nil {
		return "", fmt.Errorf("remove artifact backup placeholder: %w", err)
	}
	return backupPath, nil
}

func rollbackCommittedGeneratedShots(shots []*stagedGeneratedShot) {
	for i := len(shots) - 1; i >= 0; i-- {
		shot := shots[i]
		_ = os.Remove(shot.outputPath)
		if shot.hadPrevious {
			_ = os.Rename(shot.backupPath, shot.outputPath)
		}
	}
}

func cleanupStagedGeneratedShots(shots []*stagedGeneratedShot) {
	for _, shot := range shots {
		if shot != nil && !shot.committed {
			_ = os.Remove(shot.tempPath)
		}
	}
}

func cleanupCommittedGeneratedShotBackups(shots []*stagedGeneratedShot) {
	for _, shot := range shots {
		if shot != nil && shot.committed && shot.hadPrevious {
			_ = os.Remove(shot.backupPath)
		}
	}
}

type shotProviderMetadata struct {
	requestID string
	status    string
}

func normalizeShotProviderMetadata(shotIndex int, requestID string, status string) (shotProviderMetadata, error) {
	requestID = strings.TrimSpace(requestID)
	status = strings.ToLower(strings.TrimSpace(status))
	if requestID == "" {
		return shotProviderMetadata{}, fmt.Errorf("missing xai provider metadata: shot %d xai_request_id is empty", shotIndex)
	}
	if status == "" {
		return shotProviderMetadata{}, fmt.Errorf("missing xai provider metadata: shot %d xai_status is empty", shotIndex)
	}
	if !isReusableShotProviderStatus(status) {
		return shotProviderMetadata{}, fmt.Errorf("invalid xai provider metadata: shot %d xai_status %q is not reusable", shotIndex, status)
	}
	return shotProviderMetadata{requestID: requestID, status: status}, nil
}

func validateShotProviderMetadata(shotIndex int, requestID string, status string) error {
	_, err := normalizeShotProviderMetadata(shotIndex, requestID, status)
	if err != nil {
		return err
	}
	return nil
}

func isReusableShotProviderStatus(status string) bool {
	switch strings.TrimSpace(status) {
	case "done", "dry_run":
		return true
	default:
		return false
	}
}

func canonicalShotVideoPath(shotIndex int) string {
	return filepath.ToSlash(filepath.Join("shots", fmt.Sprintf("shot_%03d.mp4", shotIndex)))
}

func shotDecision(shot Shot, decision string) ShotDecision {
	return ShotDecision{
		Index:        shot.Index,
		Decision:     decision,
		VideoPath:    shot.VideoPath,
		PromptHash:   shot.PromptHash,
		XAIRequestID: shot.XAIRequestID,
		XAIStatus:    shot.XAIStatus,
	}
}

func readManifest(path string) (Manifest, bool) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Manifest{}, false
	}
	var manifest Manifest
	if err := json.Unmarshal(data, &manifest); err != nil {
		return Manifest{}, false
	}
	return manifest, true
}

func previousShotMatches(previous Manifest, current Shot) bool {
	if len(previous.Shots) == 0 {
		return false
	}
	for _, prev := range previous.Shots {
		if prev.Index != current.Index {
			continue
		}
		return prev.PromptHash != "" && prev.PromptHash == current.PromptHash
	}
	return false
}

func previousShotHasProviderMetadata(previous Manifest, shotIndex int) bool {
	for _, prev := range previous.Shots {
		if prev.Index != shotIndex {
			continue
		}
		return validateShotProviderMetadata(prev.Index, prev.XAIRequestID, prev.XAIStatus) == nil
	}
	return false
}

func copyPreviousShotProviderMetadata(previous Manifest, current *Shot) {
	for _, prev := range previous.Shots {
		if prev.Index != current.Index {
			continue
		}
		metadata, err := normalizeShotProviderMetadata(prev.Index, prev.XAIRequestID, prev.XAIStatus)
		if err != nil {
			return
		}
		current.XAIRequestID = metadata.requestID
		current.XAIStatus = metadata.status
		return
	}
}

func shotCacheHash(shot Shot, videoModel string) string {
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
	sum := sha256.Sum256([]byte(fmt.Sprintf(
		"%s\n%s\n%.3f\n%s\n%s",
		normalizeVideoModel(videoModel),
		strings.TrimSpace(shot.Prompt),
		duration,
		aspectRatio,
		resolution,
	)))
	return hex.EncodeToString(sum[:])
}

func storyHash(storyText string) string {
	sum := sha256.Sum256([]byte(strings.TrimSpace(storyText)))
	return hex.EncodeToString(sum[:])
}

func writeRunArtifacts(manifestPath string, manifest Manifest, runMetadataPath string, metadata RunMetadata) error {
	manifestData, err := marshalManifest(manifest)
	if err != nil {
		return err
	}
	runMetadataData, err := marshalRunMetadata(metadata)
	if err != nil {
		return err
	}

	manifestFile, err := stageArtifactFile("write xai manifest", manifestPath, manifestData)
	if err != nil {
		return err
	}
	runMetadataFile, err := stageArtifactFile("write xai run metadata", runMetadataPath, runMetadataData)
	if err != nil {
		cleanupStagedArtifactFiles([]*stagedArtifactFile{manifestFile})
		return err
	}
	files := []*stagedArtifactFile{manifestFile, runMetadataFile}
	defer cleanupStagedArtifactFiles(files)
	return commitStagedArtifactFiles(files)
}

func marshalManifest(manifest Manifest) ([]byte, error) {
	data, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("marshal xai manifest: %w", err)
	}
	return data, nil
}

func marshalRunMetadata(metadata RunMetadata) ([]byte, error) {
	data, err := json.MarshalIndent(metadata, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("marshal xai run metadata: %w", err)
	}
	return data, nil
}

type stagedArtifactFile struct {
	label       string
	tempPath    string
	outputPath  string
	backupPath  string
	hadPrevious bool
	committed   bool
}

func stageArtifactFile(label string, outputPath string, data []byte) (*stagedArtifactFile, error) {
	tempFile, err := os.CreateTemp(filepath.Dir(outputPath), fmt.Sprintf(".%s_*.tmp", filepath.Base(outputPath)))
	if err != nil {
		return nil, fmt.Errorf("%s: create temporary file: %w", label, err)
	}
	tempPath := tempFile.Name()
	removeTemp := true
	defer func() {
		if removeTemp {
			_ = os.Remove(tempPath)
		}
	}()
	if _, err := tempFile.Write(data); err != nil {
		_ = tempFile.Close()
		return nil, fmt.Errorf("%s: write temporary file: %w", label, err)
	}
	if err := tempFile.Close(); err != nil {
		return nil, fmt.Errorf("%s: close temporary file: %w", label, err)
	}
	if err := os.Chmod(tempPath, 0644); err != nil {
		return nil, fmt.Errorf("%s: chmod temporary file: %w", label, err)
	}
	removeTemp = false
	return &stagedArtifactFile{
		label:      label,
		tempPath:   tempPath,
		outputPath: outputPath,
	}, nil
}

func commitStagedArtifactFiles(files []*stagedArtifactFile) error {
	var committed []*stagedArtifactFile
	for _, file := range files {
		if err := commitStagedArtifactFile(file); err != nil {
			rollbackCommittedArtifactFiles(committed)
			return err
		}
		committed = append(committed, file)
	}
	for _, file := range committed {
		if file.hadPrevious {
			_ = os.Remove(file.backupPath)
		}
	}
	return nil
}

func commitStagedArtifactFile(file *stagedArtifactFile) error {
	info, err := os.Lstat(file.outputPath)
	switch {
	case err == nil && info.IsDir():
		return fmt.Errorf("%s: %s is a directory", file.label, file.outputPath)
	case err == nil:
		backupPath, err := createArtifactBackupPath(file.outputPath)
		if err != nil {
			return fmt.Errorf("%s: %w", file.label, err)
		}
		if err := os.Rename(file.outputPath, backupPath); err != nil {
			return fmt.Errorf("%s: backup existing file: %w", file.label, err)
		}
		file.backupPath = backupPath
		file.hadPrevious = true
	case os.IsNotExist(err):
	default:
		return fmt.Errorf("%s: stat existing file: %w", file.label, err)
	}

	if err := os.Rename(file.tempPath, file.outputPath); err != nil {
		if file.hadPrevious {
			_ = os.Rename(file.backupPath, file.outputPath)
		}
		return fmt.Errorf("%s: commit file: %w", file.label, err)
	}
	file.committed = true
	return nil
}

func rollbackCommittedArtifactFiles(files []*stagedArtifactFile) {
	for i := len(files) - 1; i >= 0; i-- {
		file := files[i]
		_ = os.Remove(file.outputPath)
		if file.hadPrevious {
			_ = os.Rename(file.backupPath, file.outputPath)
		}
	}
}

func cleanupStagedArtifactFiles(files []*stagedArtifactFile) {
	for _, file := range files {
		if file != nil && !file.committed {
			_ = os.Remove(file.tempPath)
		}
	}
}
