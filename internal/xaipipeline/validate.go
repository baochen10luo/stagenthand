package xaipipeline

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"html"
	"image/jpeg"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	nethtml "golang.org/x/net/html"
)

const (
	hyperFramesPackageName    = "xai-video-timeline"
	hyperFramesPackageVersion = "1.0.0"
)

type hyperFramesPackageManifest struct {
	Name    string `json:"name"`
	Version string `json:"version"`
	Private bool   `json:"private"`
}

type ValidationStatus string

const (
	ValidationStatusValid   ValidationStatus = "valid"
	ValidationStatusInvalid ValidationStatus = "invalid"
)

type ValidationSummary struct {
	OutputDir       string           `json:"output_dir"`
	Status          ValidationStatus `json:"status"`
	StoryHash       string           `json:"story_hash,omitempty"`
	VideoModel      string           `json:"video_model,omitempty"`
	Inspect         InspectSummary   `json:"inspect"`
	FFprobeMetadata *RenderMetadata  `json:"ffprobe_metadata,omitempty"`
	Issues          []string         `json:"issues,omitempty"`
}

func ValidateOutputDir(ctx context.Context, outputDir string, validator OutputValidator) (ValidationSummary, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return ValidationSummary{}, err
	}
	inspect, err := InspectOutputDir(outputDir)
	if err != nil {
		return ValidationSummary{}, err
	}

	summary := ValidationSummary{
		OutputDir:  inspect.OutputDir,
		Status:     ValidationStatusInvalid,
		StoryHash:  inspect.StoryHash,
		VideoModel: inspect.VideoModel,
		Inspect:    inspect,
	}
	if inspect.Status != InspectStatusComplete {
		summary.Issues = append(summary.Issues, fmt.Sprintf("inspect status %s: output is not complete", inspect.Status))
		summary.Issues = append(summary.Issues, inspect.Issues...)
		for _, artifact := range inspect.MissingArtifacts {
			summary.Issues = append(summary.Issues, fmt.Sprintf("missing artifact %s", artifact))
		}
		return finalizeValidationStatus(summary), nil
	}

	spec := validationSpecFromInspect(inspect)
	summary.Issues = append(summary.Issues, validateLegacyArtifacts(inspect)...)
	summary.Issues = append(summary.Issues, validateManifestContract(inspect)...)
	summary.Issues = append(summary.Issues, validateProviderMetadata(inspect)...)
	summary.Issues = append(summary.Issues, validatePersistedRenderMetadata(inspect, spec)...)
	summary.Issues = append(summary.Issues, validateShotArtifacts(inspect)...)
	summary.Issues = append(summary.Issues, validateNoStagedShotArtifacts(inspect.OutputDir)...)
	summary.Issues = append(summary.Issues, validateNoStagedNormalizedArtifacts(inspect.OutputDir)...)
	summary.Issues = append(summary.Issues, validateNoStagedMetadataArtifacts(inspect.OutputDir)...)
	summary.Issues = append(summary.Issues, validateNoStagedHyperFramesProjectArtifacts(inspect.OutputDir)...)
	summary.Issues = append(summary.Issues, validateNoStagedTimelineArtifacts(inspect.OutputDir)...)
	summary.Issues = append(summary.Issues, validateNoStagedFinalOutputArtifacts(inspect.OutputDir)...)
	summary.Issues = append(summary.Issues, validateNoStagedPreviewArtifacts(inspect.OutputDir)...)
	summary.Issues = append(summary.Issues, validateNoSymlinkedArtifacts(inspect)...)
	summary.Issues = append(summary.Issues, validateHyperFramesArtifacts(inspect)...)
	summary.Issues = append(summary.Issues, validatePreviewFrameArtifact(inspect.Artifacts.PreviewFrame, spec)...)

	if validator == nil {
		validator = NewFFprobeOutputValidator()
	}
	rawShotIssues := validateRawShotMetadata(ctx, inspect, validator)
	if err := ctx.Err(); err != nil {
		return ValidationSummary{}, err
	}
	summary.Issues = append(summary.Issues, rawShotIssues...)
	normalizedShotIssues := validateNormalizedShotMetadata(ctx, inspect, validator)
	if err := ctx.Err(); err != nil {
		return ValidationSummary{}, err
	}
	summary.Issues = append(summary.Issues, normalizedShotIssues...)
	timelineIssues := validateHyperFramesTimelineMetadata(ctx, inspect, validator)
	if err := ctx.Err(); err != nil {
		return ValidationSummary{}, err
	}
	summary.Issues = append(summary.Issues, timelineIssues...)
	metadata, issues := validateFFprobeMetadata(ctx, validator, inspect.OutputDir, inspect.Artifacts.OutputVideo, spec, "ffprobe validation", "ffprobe metadata")
	if err := ctx.Err(); err != nil {
		return ValidationSummary{}, err
	}
	summary.Issues = append(summary.Issues, issues...)
	if len(issues) == 0 {
		summary.FFprobeMetadata = &metadata
	}

	return finalizeValidationStatus(summary), nil
}

func validateLegacyArtifacts(inspect InspectSummary) []string {
	return singleOutputLegacyArtifactIssues(inspect.OutputDir)
}

func validateRawShotMetadata(ctx context.Context, inspect InspectSummary, validator OutputValidator) []string {
	var issues []string
	for _, shot := range inspect.ShotSummaries {
		if shot.VideoPath == "" {
			continue
		}
		spec := RenderValidationSpec{ExpectedDurationSec: inspectShotDuration(shot)}
		_, shotIssues := validateFFprobeMetadata(
			ctx,
			validator,
			inspect.OutputDir,
			canonicalRawShotPath(inspect.OutputDir, shot.Index),
			spec,
			fmt.Sprintf("raw shot ffprobe validation for shot %d", shot.Index),
			fmt.Sprintf("raw shot ffprobe metadata for shot %d", shot.Index),
		)
		issues = append(issues, shotIssues...)
		if err := ctx.Err(); err != nil {
			return issues
		}
	}
	return issues
}

func validateNormalizedShotMetadata(ctx context.Context, inspect InspectSummary, validator OutputValidator) []string {
	var issues []string
	for _, shot := range inspect.ShotSummaries {
		path := filepath.Join(inspect.OutputDir, "normalized", fmt.Sprintf("shot_%03d.mp4", shot.Index))
		spec := validationSpecFromInspect(inspect)
		spec.ExpectedDurationSec = inspectShotDuration(shot)
		_, shotIssues := validateFFprobeMetadata(
			ctx,
			validator,
			inspect.OutputDir,
			path,
			spec,
			fmt.Sprintf("normalized shot ffprobe validation for shot %d", shot.Index),
			fmt.Sprintf("normalized shot ffprobe metadata for shot %d", shot.Index),
		)
		issues = append(issues, shotIssues...)
		if err := ctx.Err(); err != nil {
			return issues
		}
	}
	return issues
}

func validateHyperFramesTimelineMetadata(ctx context.Context, inspect InspectSummary, validator OutputValidator) []string {
	spec := validationSpecFromInspect(inspect)
	spec.CodecName = ""
	spec.PixelFormat = ""
	_, issues := validateFFprobeMetadata(
		ctx,
		validator,
		inspect.OutputDir,
		filepath.Join(inspect.OutputDir, "timeline_hyperframes.mp4"),
		spec,
		"hyperframes timeline ffprobe validation",
		"hyperframes timeline ffprobe metadata",
	)
	return issues
}

func validateFFprobeMetadata(ctx context.Context, validator OutputValidator, outputDir string, path string, spec RenderValidationSpec, validationLabel string, metadataLabel string) (RenderMetadata, []string) {
	metadata, err := validator.Validate(ctx, path, spec)
	if err != nil {
		return RenderMetadata{}, []string{fmt.Sprintf("%s: %v", validationLabel, err)}
	}
	if err := validateRenderMetadata(metadata, spec); err != nil {
		return RenderMetadata{}, []string{fmt.Sprintf("%s: %v", metadataLabel, err)}
	}
	if err := validateRenderMetadataPath(outputDir, metadata.Path, path); err != nil {
		return RenderMetadata{}, []string{fmt.Sprintf("%s: path: %v", metadataLabel, err)}
	}
	if err := validateRenderMetadataSize(path, metadata.SizeBytes); err != nil {
		return RenderMetadata{}, []string{fmt.Sprintf("%s: size_bytes: %v", metadataLabel, err)}
	}
	return metadata, nil
}

func validateHyperFramesArtifacts(inspect InspectSummary) []string {
	var issues []string
	indexPath := filepath.Join(inspect.OutputDir, "hyperframes", "index.html")
	packagePath := filepath.Join(inspect.OutputDir, "hyperframes", "package.json")
	indexPresent := true
	if err := validateNonEmptyArtifact(indexPath); err != nil {
		issues = append(issues, fmt.Sprintf("missing hyperframes project artifact: %v", err))
		indexPresent = false
	}
	if err := validateNonEmptyArtifact(packagePath); err != nil {
		issues = append(issues, fmt.Sprintf("missing hyperframes project artifact: %v", err))
	} else {
		issues = append(issues, validateHyperFramesPackageManifest(packagePath)...)
	}
	if err := validateMP4Artifact(filepath.Join(inspect.OutputDir, "timeline_hyperframes.mp4")); err != nil {
		issues = append(issues, fmt.Sprintf("hyperframes timeline artifact %v", err))
	}
	if !indexPresent {
		return issues
	}

	indexData, err := os.ReadFile(indexPath)
	if err != nil {
		issues = append(issues, fmt.Sprintf("read hyperframes project artifact: %v", err))
		return issues
	}
	issues = append(issues, validateHyperFramesProjectSpec(indexData, validationSpecFromInspect(inspect))...)
	issues = append(issues, validateHyperFramesRuntimeHooks(indexData)...)
	issues = append(issues, validateHyperFramesRuntimeWiring(indexData, inspect.ShotSummaries)...)
	issues = append(issues, validateHyperFramesNoRawShotReferences(indexData, inspect.OutputDir)...)
	issues = append(issues, validateHyperFramesNormalizedShotReferences(indexData, inspect.ShotSummaries)...)
	issues = append(issues, validateHyperFramesVideoSources(indexData, inspect.ShotSummaries)...)
	issues = append(issues, validateHyperFramesVideoClipMetadata(indexData, inspect.ShotSummaries)...)
	issues = append(issues, validateHyperFramesShotTiming(indexData, inspect.ShotSummaries)...)
	issues = append(issues, validateHyperFramesSubtitles(indexData, inspect.ShotSummaries)...)
	issues = append(issues, validateHyperFramesTransitions(indexData, inspect.ShotSummaries)...)
	return issues
}

func validateHyperFramesPackageManifest(path string) []string {
	data, err := os.ReadFile(path)
	if err != nil {
		return []string{fmt.Sprintf("read hyperframes package.json: %v", err)}
	}
	var manifest hyperFramesPackageManifest
	if err := json.Unmarshal(data, &manifest); err != nil {
		return []string{fmt.Sprintf("hyperframes package.json invalid JSON: %v", err)}
	}

	var issues []string
	if manifest.Name != hyperFramesPackageName {
		issues = append(issues, fmt.Sprintf("hyperframes package.json name %q, want %q", manifest.Name, hyperFramesPackageName))
	}
	if manifest.Version != hyperFramesPackageVersion {
		issues = append(issues, fmt.Sprintf("hyperframes package.json version %q, want %q", manifest.Version, hyperFramesPackageVersion))
	}
	if !manifest.Private {
		issues = append(issues, "hyperframes package.json private must be true")
	}
	return issues
}

func validateHyperFramesRuntimeHooks(indexData []byte) []string {
	var issues []string
	if !hasHyperFramesCompositionID(indexData) {
		issues = append(issues, `hyperframes project missing runtime hook: data-composition-id="xai-video"`)
	}

	scriptText := hyperFramesScriptText(indexData)
	for _, expected := range []string{
		`applyShotVisibility`,
		`fadeSeconds`,
		`style.opacity`,
		`window.__timelines["xai-video"]`,
		`window.__timelines["xai-video"] = timeline`,
		`window.__hf`,
		`window.__hf = { seek:`,
		`timeline.seek`,
		`shot.video.currentTime`,
		`var local = t - start`,
		`var target =`,
		`shot.video.currentTime = target`,
	} {
		if !containsJavaScriptCodeFragment(scriptText, expected) {
			issues = append(issues, fmt.Sprintf("hyperframes project missing runtime hook: %s", expected))
		}
	}
	return issues
}

func validateHyperFramesRuntimeWiring(indexData []byte, shots []InspectShot) []string {
	scriptText := hyperFramesScriptText(indexData)
	var issues []string
	for _, shot := range shots {
		expectedVideoReference := fmt.Sprintf(`document.getElementById("video-%d")`, shot.Index)
		if !containsJavaScriptCodeFragment(scriptText, expectedVideoReference) {
			issues = append(issues, fmt.Sprintf("hyperframes project runtime missing video clip reference: %s", expectedVideoReference))
		}
		if strings.TrimSpace(shot.Subtitle) == "" {
			continue
		}
		expectedSubtitleReference := fmt.Sprintf(`document.getElementById("subtitle-%d")`, shot.Index)
		if !containsJavaScriptCodeFragment(scriptText, expectedSubtitleReference) {
			issues = append(issues, fmt.Sprintf("hyperframes project runtime missing subtitle clip reference: %s", expectedSubtitleReference))
		}
	}
	return issues
}

func hasHyperFramesCompositionID(indexData []byte) bool {
	_, ok := hyperFramesCompositionAttributes(indexData)
	return ok
}

func hyperFramesCompositionAttributes(indexData []byte) (map[string]string, bool) {
	root, err := nethtml.Parse(bytes.NewReader(indexData))
	if err != nil {
		return nil, false
	}
	var attrs map[string]string
	var walk func(*nethtml.Node)
	walk = func(node *nethtml.Node) {
		if attrs != nil {
			return
		}
		if node.Type == nethtml.ElementNode {
			nodeAttrs := make(map[string]string, len(node.Attr))
			for _, attr := range node.Attr {
				nodeAttrs[strings.ToLower(attr.Key)] = attr.Val
			}
			if nodeAttrs["data-composition-id"] == "xai-video" {
				attrs = nodeAttrs
				return
			}
		}
		for child := node.FirstChild; child != nil; child = child.NextSibling {
			walk(child)
		}
	}
	walk(root)
	return attrs, attrs != nil
}

func hyperFramesScriptText(indexData []byte) string {
	root, err := nethtml.Parse(bytes.NewReader(indexData))
	if err != nil {
		return ""
	}
	var builder strings.Builder
	var walk func(*nethtml.Node, bool)
	walk = func(node *nethtml.Node, inScript bool) {
		if node.Type == nethtml.ElementNode && strings.EqualFold(node.Data, "script") {
			inScript = true
		}
		if inScript && node.Type == nethtml.TextNode {
			builder.WriteString(node.Data)
			builder.WriteByte('\n')
		}
		for child := node.FirstChild; child != nil; child = child.NextSibling {
			walk(child, inScript)
		}
	}
	walk(root, false)
	return builder.String()
}

func containsJavaScriptCodeFragment(script string, fragment string) bool {
	const (
		stateNormal = iota
		stateLineComment
		stateBlockComment
		stateSingleQuote
		stateDoubleQuote
		stateTemplate
		stateRegex
	)

	state := stateNormal
	escaped := false
	regexCharClass := false
	for i := 0; i < len(script); i++ {
		if state == stateNormal && strings.HasPrefix(script[i:], fragment) {
			return true
		}

		current := script[i]
		var next byte
		if i+1 < len(script) {
			next = script[i+1]
		}

		switch state {
		case stateNormal:
			switch {
			case current == '/' && next == '/':
				state = stateLineComment
				i++
			case current == '/' && next == '*':
				state = stateBlockComment
				i++
			case current == '/' && canStartJavaScriptRegexLiteral(script, i):
				state = stateRegex
				escaped = false
				regexCharClass = false
			case current == '\'':
				state = stateSingleQuote
				escaped = false
			case current == '"':
				state = stateDoubleQuote
				escaped = false
			case current == '`':
				state = stateTemplate
				escaped = false
			}
		case stateLineComment:
			if current == '\n' || current == '\r' {
				state = stateNormal
			}
		case stateBlockComment:
			if current == '*' && next == '/' {
				state = stateNormal
				i++
			}
		case stateSingleQuote:
			if escaped {
				escaped = false
			} else if current == '\\' {
				escaped = true
			} else if current == '\'' {
				state = stateNormal
			}
		case stateDoubleQuote:
			if escaped {
				escaped = false
			} else if current == '\\' {
				escaped = true
			} else if current == '"' {
				state = stateNormal
			}
		case stateTemplate:
			if escaped {
				escaped = false
			} else if current == '\\' {
				escaped = true
			} else if current == '`' {
				state = stateNormal
			}
		case stateRegex:
			if escaped {
				escaped = false
			} else if current == '\\' {
				escaped = true
			} else if current == '[' {
				regexCharClass = true
			} else if current == ']' {
				regexCharClass = false
			} else if current == '/' && !regexCharClass {
				state = stateNormal
			}
		}
	}
	return false
}

func canStartJavaScriptRegexLiteral(script string, slashIndex int) bool {
	for i := slashIndex - 1; i >= 0; i-- {
		current := script[i]
		if current == ' ' || current == '\t' || current == '\n' || current == '\r' {
			continue
		}
		if strings.ContainsRune("=({[,;:!?", rune(current)) {
			return true
		}
		if isJavaScriptIdentifierByte(current) {
			start := i
			for start > 0 && isJavaScriptIdentifierByte(script[start-1]) {
				start--
			}
			switch script[start : i+1] {
			case "return", "throw", "case", "delete", "void", "typeof", "yield", "await", "else", "do":
				return true
			default:
				return false
			}
		}
		return false
	}
	return true
}

func isJavaScriptIdentifierByte(value byte) bool {
	return value >= 'a' && value <= 'z' ||
		value >= 'A' && value <= 'Z' ||
		value >= '0' && value <= '9' ||
		value == '_' ||
		value == '$'
}

func validateHyperFramesNoRawShotReferences(indexData []byte, outputDir string) []string {
	for _, rawDirReference := range hyperFramesRawShotDirReferences(outputDir) {
		offset := bytes.Index(indexData, []byte(rawDirReference))
		if offset < 0 {
			continue
		}
		reference := rawDirReference + artifactReferenceSuffix(indexData[offset+len(rawDirReference):])
		return []string{fmt.Sprintf("hyperframes project must not reference raw xAI shots: %s", reference)}
	}
	return nil
}

func hyperFramesRawShotDirReferences(outputDir string) []string {
	absoluteRawDir := filepath.ToSlash(filepath.Join(outputDir, "shots")) + "/"
	rawDirURL := url.URL{Scheme: "file", Path: absoluteRawDir}
	return []string{
		filepath.ToSlash(filepath.Join("..", "shots")) + "/",
		absoluteRawDir,
		rawDirURL.String(),
	}
}

func artifactReferenceSuffix(data []byte) string {
	end := len(data)
	for i, b := range data {
		if b == '"' || b == '\'' || b == '<' || b == '>' || b == ')' || b == ' ' || b == '\n' || b == '\t' || b == '\r' {
			end = i
			break
		}
	}
	return string(data[:end])
}

func validateHyperFramesNormalizedShotReferences(indexData []byte, shots []InspectShot) []string {
	allowed := make(map[string]struct{}, len(shots))
	for _, shot := range shots {
		allowed[filepath.ToSlash(filepath.Join("..", "normalized", fmt.Sprintf("shot_%03d.mp4", shot.Index)))] = struct{}{}
	}
	counts := make(map[string]int, len(shots))

	normalizedDirReference := filepath.ToSlash(filepath.Join("..", "normalized")) + "/"
	var issues []string
	search := indexData
	for {
		offset := bytes.Index(search, []byte(normalizedDirReference))
		if offset < 0 {
			break
		}
		reference := normalizedDirReference + artifactReferenceSuffix(search[offset+len(normalizedDirReference):])
		if _, ok := allowed[reference]; !ok {
			issues = append(issues, fmt.Sprintf("hyperframes project references normalized shot outside manifest: %s", reference))
		} else {
			counts[reference]++
			if counts[reference] > 1 {
				issues = append(issues, fmt.Sprintf("hyperframes project references normalized shot more than once: %s", reference))
			}
		}
		search = search[offset+len(normalizedDirReference):]
	}
	return issues
}

func validateHyperFramesVideoSources(indexData []byte, shots []InspectShot) []string {
	allowed := make(map[string]struct{}, len(shots))
	for _, shot := range shots {
		allowed[filepath.ToSlash(filepath.Join("..", "normalized", fmt.Sprintf("shot_%03d.mp4", shot.Index)))] = struct{}{}
	}

	var issues []string
	for _, source := range hyperFramesVideoSources(indexData) {
		if !source.ok {
			issues = append(issues, "hyperframes project video tag missing src")
		} else if _, ok := allowed[source.src]; !ok {
			issues = append(issues, fmt.Sprintf("hyperframes project video source is not manifest-owned normalized shot: %s", source.src))
		}
	}
	return issues
}

type hyperFramesVideoSource struct {
	src   string
	ok    bool
	attrs map[string]string
}

func hyperFramesVideoSources(indexData []byte) []hyperFramesVideoSource {
	root, err := nethtml.Parse(bytes.NewReader(indexData))
	if err != nil {
		return []hyperFramesVideoSource{{ok: false}}
	}
	var sources []hyperFramesVideoSource
	var walk func(*nethtml.Node)
	walk = func(node *nethtml.Node) {
		if node.Type == nethtml.ElementNode && strings.EqualFold(node.Data, "video") {
			source := hyperFramesVideoSource{
				attrs: htmlNodeAttributes(node),
			}
			source.src, source.ok = source.attrs["src"]
			sources = append(sources, source)
		}
		for child := node.FirstChild; child != nil; child = child.NextSibling {
			walk(child)
		}
	}
	walk(root)
	return sources
}

func hyperFramesVideoSourceBySrc(sources []hyperFramesVideoSource, src string) (hyperFramesVideoSource, int, bool) {
	for index, source := range sources {
		if source.ok && source.src == src {
			return source, index, true
		}
	}
	return hyperFramesVideoSource{}, -1, false
}

func validateHyperFramesVideoClipMetadata(indexData []byte, shots []InspectShot) []string {
	var issues []string
	sources := hyperFramesVideoSources(indexData)
	for shotPosition, shot := range shots {
		expectedSrc := filepath.ToSlash(filepath.Join("..", "normalized", fmt.Sprintf("shot_%03d.mp4", shot.Index)))
		source, _, ok := hyperFramesVideoSourceBySrc(sources, expectedSrc)
		if !ok {
			continue
		}

		expectedID := fmt.Sprintf("video-%d", shot.Index)
		if source.attrs["id"] != expectedID {
			issues = append(issues, fmt.Sprintf("hyperframes project shot %d video clip missing id=\"%s\"", shot.Index, expectedID))
		}
		if !htmlClassHasTokens(source.attrs["class"], "shot-video", "clip") {
			issues = append(issues, fmt.Sprintf("hyperframes project shot %d video clip missing class=\"shot-video clip\"", shot.Index))
		}
		expectedTrackIndex := fmt.Sprintf("%d", shotPosition*2)
		if source.attrs["data-track-index"] != expectedTrackIndex {
			issues = append(issues, fmt.Sprintf("hyperframes project shot %d video clip data-track-index mismatch: want data-track-index=\"%s\"", shot.Index, expectedTrackIndex))
		}
	}
	return issues
}

func validateHyperFramesShotTiming(indexData []byte, shots []InspectShot) []string {
	var issues []string
	sources := hyperFramesVideoSources(indexData)
	previousSourceIndex := -1
	cursor := 0.0
	for _, shot := range shots {
		expected := filepath.ToSlash(filepath.Join("..", "normalized", fmt.Sprintf("shot_%03d.mp4", shot.Index)))
		source, sourceIndex, ok := hyperFramesVideoSourceBySrc(sources, expected)
		if !ok {
			issues = append(issues, fmt.Sprintf("hyperframes project missing normalized shot reference for shot %d: %s", shot.Index, expected))
			cursor += inspectShotDuration(shot)
			continue
		}
		if previousSourceIndex >= 0 && sourceIndex <= previousSourceIndex {
			issues = append(issues, fmt.Sprintf("hyperframes project normalized shot references are out of manifest order at shot %d: %s", shot.Index, expected))
		}
		previousSourceIndex = sourceIndex

		expectedStartValue := fmt.Sprintf("%.3f", cursor)
		expectedStart := fmt.Sprintf(`data-start="%s"`, expectedStartValue)
		if source.attrs["data-start"] != expectedStartValue {
			issues = append(issues, fmt.Sprintf("hyperframes project shot %d data-start mismatch: want %s", shot.Index, expectedStart))
		}
		duration := inspectShotDuration(shot)
		expectedDurationValue := fmt.Sprintf("%.3f", duration)
		expectedDuration := fmt.Sprintf(`data-duration="%s"`, expectedDurationValue)
		if source.attrs["data-duration"] != expectedDurationValue {
			issues = append(issues, fmt.Sprintf("hyperframes project shot %d data-duration mismatch: want %s", shot.Index, expectedDuration))
		}
		cursor += duration
	}
	return issues
}

func validateHyperFramesSubtitles(indexData []byte, shots []InspectShot) []string {
	var issues []string
	cursor := 0.0
	for shotPosition, shot := range shots {
		duration := inspectShotDuration(shot)
		subtitle := strings.TrimSpace(shot.Subtitle)
		if subtitle == "" {
			cursor += duration
			continue
		}

		expectedID := fmt.Sprintf(`id="subtitle-%d"`, shot.Index)
		subtitleElement, ok := hyperFramesElementByID(indexData, fmt.Sprintf("subtitle-%d", shot.Index))
		if !ok {
			issues = append(issues, fmt.Sprintf("hyperframes project missing subtitle clip for shot %d: %s", shot.Index, expectedID))
			cursor += duration
			continue
		}
		if !htmlClassHasTokens(subtitleElement.attrs["class"], "subtitle", "clip") {
			issues = append(issues, fmt.Sprintf("hyperframes project subtitle clip for shot %d missing %s", shot.Index, `class="subtitle clip"`))
		}
		expectedStart := fmt.Sprintf("%.3f", cursor)
		if subtitleElement.attrs["data-start"] != expectedStart {
			issues = append(issues, fmt.Sprintf("hyperframes project subtitle clip for shot %d missing %s", shot.Index, fmt.Sprintf(`data-start="%s"`, expectedStart)))
		}
		expectedDuration := fmt.Sprintf("%.3f", duration)
		if subtitleElement.attrs["data-duration"] != expectedDuration {
			issues = append(issues, fmt.Sprintf("hyperframes project subtitle clip for shot %d missing %s", shot.Index, fmt.Sprintf(`data-duration="%s"`, expectedDuration)))
		}
		expectedTrackIndex := fmt.Sprintf("%d", shotPosition*2+1)
		if subtitleElement.attrs["data-track-index"] != expectedTrackIndex {
			issues = append(issues, fmt.Sprintf("hyperframes project subtitle clip for shot %d missing %s", shot.Index, fmt.Sprintf(`data-track-index="%s"`, expectedTrackIndex)))
		}
		if strings.TrimSpace(subtitleElement.text) != shot.Subtitle {
			issues = append(issues, fmt.Sprintf("hyperframes project subtitle clip for shot %d missing %s", shot.Index, html.EscapeString(shot.Subtitle)))
		}
		cursor += duration
	}
	return issues
}

type hyperFramesElement struct {
	attrs map[string]string
	text  string
}

func hyperFramesElementByID(indexData []byte, id string) (hyperFramesElement, bool) {
	root, err := nethtml.Parse(bytes.NewReader(indexData))
	if err != nil {
		return hyperFramesElement{}, false
	}
	var found *nethtml.Node
	var walk func(*nethtml.Node)
	walk = func(node *nethtml.Node) {
		if found != nil {
			return
		}
		if node.Type == nethtml.ElementNode {
			for _, attr := range node.Attr {
				if strings.EqualFold(attr.Key, "id") && attr.Val == id {
					found = node
					return
				}
			}
		}
		for child := node.FirstChild; child != nil; child = child.NextSibling {
			walk(child)
		}
	}
	walk(root)
	if found == nil {
		return hyperFramesElement{}, false
	}
	return hyperFramesElement{
		attrs: htmlNodeAttributes(found),
		text:  htmlNodeText(found),
	}, true
}

func htmlNodeAttributes(node *nethtml.Node) map[string]string {
	attrs := make(map[string]string, len(node.Attr))
	for _, attr := range node.Attr {
		attrs[strings.ToLower(attr.Key)] = attr.Val
	}
	return attrs
}

func htmlNodeText(node *nethtml.Node) string {
	var builder strings.Builder
	var walk func(*nethtml.Node)
	walk = func(current *nethtml.Node) {
		if current.Type == nethtml.TextNode {
			builder.WriteString(current.Data)
		}
		for child := current.FirstChild; child != nil; child = child.NextSibling {
			walk(child)
		}
	}
	walk(node)
	return builder.String()
}

func htmlClassHasTokens(class string, required ...string) bool {
	tokens := make(map[string]struct{})
	for _, token := range strings.Fields(class) {
		tokens[token] = struct{}{}
	}
	for _, token := range required {
		if _, ok := tokens[token]; !ok {
			return false
		}
	}
	return true
}

func validateHyperFramesTransitions(indexData []byte, shots []InspectShot) []string {
	var issues []string
	sources := hyperFramesVideoSources(indexData)
	for _, shot := range shots {
		transition := strings.TrimSpace(shot.TransitionOut)
		if transition == "" {
			continue
		}

		expected := filepath.ToSlash(filepath.Join("..", "normalized", fmt.Sprintf("shot_%03d.mp4", shot.Index)))
		source, _, ok := hyperFramesVideoSourceBySrc(sources, expected)
		if !ok {
			continue
		}
		expectedTransition := fmt.Sprintf(`data-transition-out="%s"`, html.EscapeString(transition))
		if source.attrs["data-transition-out"] != transition {
			issues = append(issues, fmt.Sprintf("hyperframes project shot %d missing transition metadata: want %s", shot.Index, expectedTransition))
		}
	}
	return issues
}

func hyperFramesDivElementForReference(indexData []byte, referenceOffset int) ([]byte, bool) {
	elementStart := bytes.LastIndex(indexData[:referenceOffset], []byte("<div"))
	if elementStart < 0 {
		return nil, false
	}
	elementEndOffset := bytes.Index(indexData[referenceOffset:], []byte("</div>"))
	if elementEndOffset < 0 {
		return nil, false
	}
	return indexData[elementStart : referenceOffset+elementEndOffset+len("</div>")], true
}

func inspectShotDuration(shot InspectShot) float64 {
	if shot.DurationSec > 0 {
		return shot.DurationSec
	}
	return defaultDurationSec
}

func validateHyperFramesProjectSpec(indexData []byte, spec RenderValidationSpec) []string {
	var issues []string
	attrs, ok := hyperFramesCompositionAttributes(indexData)
	if !ok {
		attrs = map[string]string{}
	}
	if spec.Width > 0 {
		expected := fmt.Sprintf(`data-width="%d"`, spec.Width)
		if attrs["data-width"] != fmt.Sprintf("%d", spec.Width) {
			issues = append(issues, fmt.Sprintf("hyperframes project data-width mismatch: want %s", expected))
		}
	}
	if spec.Height > 0 {
		expected := fmt.Sprintf(`data-height="%d"`, spec.Height)
		if attrs["data-height"] != fmt.Sprintf("%d", spec.Height) {
			issues = append(issues, fmt.Sprintf("hyperframes project data-height mismatch: want %s", expected))
		}
	}
	if spec.ExpectedDurationSec > 0 {
		expected := fmt.Sprintf(`data-duration="%.3f"`, spec.ExpectedDurationSec)
		if attrs["data-duration"] != fmt.Sprintf("%.3f", spec.ExpectedDurationSec) {
			issues = append(issues, fmt.Sprintf("hyperframes project data-duration mismatch: want %s", expected))
		}
	}
	return issues
}

func validatePreviewFrameArtifact(path string, spec RenderValidationSpec) []string {
	file, err := os.Open(path)
	if err != nil {
		return []string{fmt.Sprintf("preview frame: %v", err)}
	}
	defer file.Close()

	config, err := jpeg.DecodeConfig(file)
	if err != nil {
		return []string{fmt.Sprintf("preview frame is not a readable JPEG: %v", err)}
	}
	if spec.Width > 0 && config.Width != spec.Width {
		return []string{fmt.Sprintf("preview frame dimensions %dx%d, want %dx%d", config.Width, config.Height, spec.Width, spec.Height)}
	}
	if spec.Height > 0 && config.Height != spec.Height {
		return []string{fmt.Sprintf("preview frame dimensions %dx%d, want %dx%d", config.Width, config.Height, spec.Width, spec.Height)}
	}
	return nil
}

func validateShotArtifacts(inspect InspectSummary) []string {
	var issues []string
	for _, shot := range inspect.ShotSummaries {
		if shot.VideoPath == "" {
			issues = append(issues, fmt.Sprintf("shot %d missing video_path", shot.Index))
		} else if err := validateMP4Artifact(canonicalRawShotPath(inspect.OutputDir, shot.Index)); err != nil {
			issues = append(issues, fmt.Sprintf("shot artifact for shot %d %v", shot.Index, err))
		}

		normalizedRelPath := filepath.ToSlash(filepath.Join("normalized", fmt.Sprintf("shot_%03d.mp4", shot.Index)))
		if err := validateMP4Artifact(filepath.Join(inspect.OutputDir, normalizedRelPath)); err != nil {
			issues = append(issues, fmt.Sprintf("normalized shot artifact for shot %d %v", shot.Index, err))
		}
	}
	return issues
}

func validateNoStagedShotArtifacts(outputDir string) []string {
	return validateNoStagedArtifacts(outputDir, "shots", "shots dir", "staged xAI shot artifact is not allowed", isStagedShotArtifactName)
}

func isStagedShotArtifactName(name string) bool {
	if !strings.HasPrefix(name, ".shot_") {
		return false
	}
	return strings.Contains(name, ".tmp.mp4") || strings.HasSuffix(name, ".bak")
}

func validateNoStagedNormalizedArtifacts(outputDir string) []string {
	return validateNoStagedArtifacts(outputDir, "normalized", "normalized dir", "staged xAI normalized artifact is not allowed", isStagedNormalizedArtifactName)
}

func isStagedNormalizedArtifactName(name string) bool {
	return strings.HasPrefix(name, ".shot_") && isStagedTempOrBackupName(name, ".mp4")
}

func validateNoStagedHyperFramesProjectArtifacts(outputDir string) []string {
	return validateNoStagedArtifacts(outputDir, "hyperframes", "hyperframes dir", "staged xAI HyperFrames project artifact is not allowed", isStagedHyperFramesProjectArtifactName)
}

func isStagedHyperFramesProjectArtifactName(name string) bool {
	for _, prefix := range []string{".index.html_", ".package.json_"} {
		if strings.HasPrefix(name, prefix) && (strings.HasSuffix(name, ".tmp") || strings.HasSuffix(name, ".bak")) {
			return true
		}
	}
	return false
}

func validateNoStagedMetadataArtifacts(outputDir string) []string {
	return validateNoStagedArtifacts(outputDir, "", "output dir", "staged xAI metadata artifact is not allowed", isStagedMetadataArtifactName)
}

func isStagedMetadataArtifactName(name string) bool {
	for _, prefix := range []string{".xai_manifest.json_", ".xai_run_metadata.json_", ".render_metadata.json_"} {
		if strings.HasPrefix(name, prefix) && (strings.HasSuffix(name, ".tmp") || strings.HasSuffix(name, ".bak")) {
			return true
		}
	}
	return false
}

func validateNoStagedTimelineArtifacts(outputDir string) []string {
	return validateNoStagedArtifacts(outputDir, "", "output dir", "staged xAI timeline artifact is not allowed", isStagedTimelineArtifactName)
}

func isStagedTimelineArtifactName(name string) bool {
	return strings.HasPrefix(name, ".timeline_hyperframes.mp4_") && isStagedTempOrBackupName(name, ".mp4")
}

func validateNoStagedFinalOutputArtifacts(outputDir string) []string {
	return validateNoStagedArtifacts(outputDir, "", "output dir", "staged xAI final output artifact is not allowed", isStagedFinalOutputArtifactName)
}

func isStagedFinalOutputArtifactName(name string) bool {
	return strings.HasPrefix(name, ".output_xai.mp4_") && isStagedTempOrBackupName(name, ".mp4")
}

func validateNoStagedPreviewArtifacts(outputDir string) []string {
	return validateNoStagedArtifacts(outputDir, "", "output dir", "staged xAI preview artifact is not allowed", isStagedPreviewArtifactName)
}

func isStagedPreviewArtifactName(name string) bool {
	return strings.HasPrefix(name, ".preview_frame.jpg_") && isStagedTempOrBackupName(name, ".jpg")
}

func isStagedTempOrBackupName(name string, mediaExt string) bool {
	return strings.HasSuffix(name, ".tmp") || strings.HasSuffix(name, ".tmp"+mediaExt) || strings.HasSuffix(name, ".bak")
}

func validateNoStagedArtifacts(outputDir string, relDir string, readLabel string, issuePrefix string, match func(string) bool) []string {
	dir := outputDir
	if relDir != "" {
		dir = filepath.Join(outputDir, relDir)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return []string{fmt.Sprintf("read %s for staged artifacts: %v", readLabel, err)}
	}
	var issues []string
	for _, entry := range entries {
		name := entry.Name()
		if !match(name) {
			continue
		}
		issuePath := name
		if relDir != "" {
			issuePath = filepath.ToSlash(filepath.Join(relDir, name))
		}
		issues = append(issues, fmt.Sprintf("%s: %s", issuePrefix, issuePath))
	}
	return issues
}

func validateNoSymlinkedArtifacts(inspect InspectSummary) []string {
	relPaths := []string{
		manifestFilename,
		runMetadataFilename,
		renderMetadataFilename,
		filepath.ToSlash(filepath.Join("hyperframes", "index.html")),
		filepath.ToSlash(filepath.Join("hyperframes", "package.json")),
		"timeline_hyperframes.mp4",
		outputVideoFilename,
		previewFrameFilename,
	}
	for _, shot := range inspect.ShotSummaries {
		relPaths = append(relPaths,
			filepath.ToSlash(filepath.Join("shots", fmt.Sprintf("shot_%03d.mp4", shot.Index))),
			filepath.ToSlash(filepath.Join("normalized", fmt.Sprintf("shot_%03d.mp4", shot.Index))),
		)
	}

	var issues []string
	for _, relPath := range relPaths {
		if issue := validateNoSymlinkPathComponent(inspect.OutputDir, relPath); issue != "" {
			issues = append(issues, issue)
		}
	}
	return issues
}

func validateNoSymlinkPathComponent(outputDir string, relPath string) string {
	cleanRel := filepath.Clean(filepath.FromSlash(relPath))
	if cleanRel == "." || filepath.IsAbs(cleanRel) || cleanRel == ".." || strings.HasPrefix(cleanRel, ".."+string(filepath.Separator)) {
		return fmt.Sprintf("xAI artifact %s is not output-local", filepath.ToSlash(relPath))
	}

	parts := strings.Split(cleanRel, string(filepath.Separator))
	current := outputDir
	for i, part := range parts {
		current = filepath.Join(current, part)
		info, err := os.Lstat(current)
		if err != nil {
			return fmt.Sprintf("xAI artifact %s stat failed: %v", filepath.ToSlash(relPath), err)
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return fmt.Sprintf("xAI artifact %s contains symlink path component %s", filepath.ToSlash(relPath), filepath.ToSlash(filepath.Join(parts[:i+1]...)))
		}
		if i < len(parts)-1 && !info.IsDir() {
			return fmt.Sprintf("xAI artifact %s parent %s is not a directory", filepath.ToSlash(relPath), filepath.ToSlash(filepath.Join(parts[:i+1]...)))
		}
		if i == len(parts)-1 && info.IsDir() {
			return fmt.Sprintf("xAI artifact %s is a directory", filepath.ToSlash(relPath))
		}
	}
	return ""
}

func canonicalRawShotPath(outputDir string, shotIndex int) string {
	return filepath.Join(outputDir, "shots", fmt.Sprintf("shot_%03d.mp4", shotIndex))
}

func validateNonEmptyArtifact(path string) error {
	info, err := os.Stat(path)
	if err != nil {
		return err
	}
	if info.IsDir() {
		return fmt.Errorf("%s is a directory", path)
	}
	if info.Size() == 0 {
		return fmt.Errorf("%s is empty", path)
	}
	return nil
}

func validateMP4Artifact(path string) error {
	if err := validateNonEmptyArtifact(path); err != nil {
		return err
	}
	if !fileHasMP4FtypMagic(path) {
		return fmt.Errorf("is not an MP4")
	}
	return nil
}

func validateManifestContract(inspect InspectSummary) []string {
	var issues []string
	if err := validateProjectID(inspect.ProjectID); err != nil {
		issues = append(issues, err.Error())
	}
	if inspect.StoryHash == "" {
		issues = append(issues, "manifest missing story_hash")
	} else if !isLowerHexSHA256(inspect.StoryHash) {
		issues = append(issues, "manifest story_hash must be 64 lowercase hex characters")
	}
	videoModel := strings.TrimSpace(inspect.VideoModel)
	if videoModel == "" {
		issues = append(issues, "manifest missing video_model")
	} else if inspect.VideoModel != videoModel {
		issues = append(issues, fmt.Sprintf("manifest video_model %q is not canonical, want %q", inspect.VideoModel, videoModel))
	}
	format := strings.TrimSpace(inspect.Format)
	if inspect.Format != "" && inspect.Format != format {
		issues = append(issues, fmt.Sprintf("manifest format %q is not canonical, want %q", inspect.Format, format))
	} else if inspect.Format != defaultFormat {
		issues = append(issues, fmt.Sprintf("manifest format %q, want %q", inspect.Format, defaultFormat))
	}
	if inspect.Width != defaultWidth || inspect.Height != defaultHeight {
		issues = append(issues, fmt.Sprintf("manifest render dimensions %dx%d, want %dx%d", inspect.Width, inspect.Height, defaultWidth, defaultHeight))
	}
	if inspect.FPS != defaultFPS {
		issues = append(issues, fmt.Sprintf("manifest fps %d, want %d", inspect.FPS, defaultFPS))
	}
	seenIndexes := make(map[int]struct{}, len(inspect.ShotSummaries))
	for _, shot := range inspect.ShotSummaries {
		if shot.Index < 1 || shot.Index > len(inspect.ShotSummaries) {
			issues = append(issues, fmt.Sprintf("manifest shot indexes must be contiguous from 1: shot position %d has index %d", len(seenIndexes)+1, shot.Index))
		}
		if _, ok := seenIndexes[shot.Index]; ok {
			issues = append(issues, fmt.Sprintf("manifest duplicate shot index %d", shot.Index))
		}
		seenIndexes[shot.Index] = struct{}{}
		prompt := strings.TrimSpace(shot.Prompt)
		if prompt == "" {
			issues = append(issues, fmt.Sprintf("manifest shot %d prompt is empty", shot.Index))
		} else if shot.Prompt != prompt {
			issues = append(issues, fmt.Sprintf("manifest shot %d prompt %q is not canonical, want %q", shot.Index, shot.Prompt, prompt))
		}
		if shot.DurationSec <= 0 {
			issues = append(issues, fmt.Sprintf("manifest shot %d duration_sec %.3f, want positive duration", shot.Index, shot.DurationSec))
		}
		aspectRatio := strings.TrimSpace(shot.AspectRatio)
		if shot.AspectRatio != "" && shot.AspectRatio != aspectRatio {
			issues = append(issues, fmt.Sprintf("manifest shot %d aspect_ratio %q is not canonical, want %q", shot.Index, shot.AspectRatio, aspectRatio))
		} else if shot.AspectRatio != "" && shot.AspectRatio != defaultAspectRatio {
			issues = append(issues, fmt.Sprintf("manifest shot %d aspect_ratio %q, want %q", shot.Index, shot.AspectRatio, defaultAspectRatio))
		}
		resolution := strings.TrimSpace(shot.Resolution)
		if shot.Resolution != "" && shot.Resolution != resolution {
			issues = append(issues, fmt.Sprintf("manifest shot %d resolution %q is not canonical, want %q", shot.Index, shot.Resolution, resolution))
		} else if shot.Resolution != "" && shot.Resolution != defaultResolution {
			issues = append(issues, fmt.Sprintf("manifest shot %d resolution %q, want %q", shot.Index, shot.Resolution, defaultResolution))
		}
		subtitle := strings.TrimSpace(shot.Subtitle)
		if shot.Subtitle != subtitle {
			issues = append(issues, fmt.Sprintf("manifest shot %d subtitle %q is not canonical, want %q", shot.Index, shot.Subtitle, subtitle))
		}
		transition, err := normalizeTransitionOut(shot.TransitionOut)
		if err != nil {
			issues = append(issues, fmt.Sprintf("manifest shot %d %v", shot.Index, err))
		} else if strings.TrimSpace(shot.TransitionOut) != "" && shot.TransitionOut != transition {
			issues = append(issues, fmt.Sprintf("manifest shot %d transition_out %q is not canonical, want %q", shot.Index, shot.TransitionOut, transition))
		}
		expectedVideoPath := filepath.ToSlash(filepath.Join("shots", fmt.Sprintf("shot_%03d.mp4", shot.Index)))
		videoPath := strings.TrimSpace(shot.VideoPath)
		if shot.VideoPath != "" && shot.VideoPath != videoPath {
			issues = append(issues, fmt.Sprintf("manifest shot %d video_path %q is not canonical, want %q", shot.Index, shot.VideoPath, videoPath))
		} else if shot.VideoPath != "" && shot.VideoPath != expectedVideoPath {
			issues = append(issues, fmt.Sprintf("manifest shot %d video_path %q, want %q", shot.Index, shot.VideoPath, expectedVideoPath))
		}
	}
	for position, shot := range inspect.ShotSummaries {
		wantIndex := position + 1
		if shot.Index != wantIndex {
			issues = append(issues, fmt.Sprintf("manifest shot indexes must match shot order from 1: shot position %d has index %d, want %d", position+1, shot.Index, wantIndex))
		}
	}
	return issues
}

func isLowerHexSHA256(value string) bool {
	if len(value) != 64 {
		return false
	}
	for _, r := range value {
		if (r >= '0' && r <= '9') || (r >= 'a' && r <= 'f') {
			continue
		}
		return false
	}
	return true
}

func validatePersistedRenderMetadata(inspect InspectSummary, spec RenderValidationSpec) []string {
	if inspect.RenderMetadata == nil {
		return []string{"missing render_metadata.json"}
	}
	var issues []string
	if issue := validateRenderMetadataProjectID(inspect.Manifest, *inspect.RenderMetadata); issue != "" {
		issues = append(issues, issue)
	}
	if issue := validateRenderMetadataManifestHash(inspect.Manifest, *inspect.RenderMetadata); issue != "" {
		issues = append(issues, issue)
	}
	if err := validateRenderMetadataPath(inspect.OutputDir, inspect.RenderMetadata.Path, inspect.Artifacts.OutputVideo); err != nil {
		issues = append(issues, fmt.Sprintf("render metadata path: %v", err))
	}
	if err := validateRenderMetadata(*inspect.RenderMetadata, spec); err != nil {
		issues = append(issues, fmt.Sprintf("render metadata: %v", err))
	}
	if err := validateRenderMetadataSize(inspect.Artifacts.OutputVideo, inspect.RenderMetadata.SizeBytes); err != nil {
		issues = append(issues, fmt.Sprintf("render metadata size_bytes: %v", err))
	}
	return issues
}

func validateRenderMetadataProjectID(manifest Manifest, metadata RenderMetadata) string {
	if metadata.ProjectID == "" {
		return "render metadata project_id is empty"
	}
	if metadata.ProjectID != manifest.ProjectID {
		return fmt.Sprintf("render metadata project_id %q, want %q", metadata.ProjectID, manifest.ProjectID)
	}
	return ""
}

func validateRenderMetadataManifestHash(manifest Manifest, metadata RenderMetadata) string {
	if metadata.ManifestHash == "" {
		return "render metadata manifest_hash is empty"
	}
	if !isLowerHexSHA256(metadata.ManifestHash) {
		return "render metadata manifest_hash must be 64 lowercase hex characters"
	}
	wantHash, err := manifestIdentityHash(manifest)
	if err != nil {
		return err.Error()
	}
	if metadata.ManifestHash != wantHash {
		return fmt.Sprintf("render metadata manifest_hash %q, want %q", metadata.ManifestHash, wantHash)
	}
	return ""
}

func validateRenderMetadataPath(outputDir string, metadataPath string, outputVideoPath string) error {
	metadataPath = filepath.Clean(metadataPath)
	if metadataPath == "." {
		return fmt.Errorf("missing path")
	}
	if !filepath.IsAbs(metadataPath) {
		metadataPath = filepath.Join(outputDir, metadataPath)
	}
	metadataPath, err := filepath.Abs(metadataPath)
	if err != nil {
		return fmt.Errorf("resolve metadata path: %w", err)
	}
	outputVideoPath, err = filepath.Abs(outputVideoPath)
	if err != nil {
		return fmt.Errorf("resolve output video path: %w", err)
	}
	if metadataPath != outputVideoPath {
		return fmt.Errorf("%q, want %q", metadataPath, outputVideoPath)
	}
	return nil
}

func validateRenderMetadataSize(outputVideoPath string, sizeBytes int64) error {
	if sizeBytes <= 0 {
		return fmt.Errorf("%d, want positive output file size", sizeBytes)
	}
	info, err := os.Stat(outputVideoPath)
	if err != nil {
		return fmt.Errorf("stat output video: %w", err)
	}
	if info.Size() != sizeBytes {
		return fmt.Errorf("%d, want %d", sizeBytes, info.Size())
	}
	return nil
}

func validateProviderMetadata(inspect InspectSummary) []string {
	var issues []string
	if len(inspect.ShotSummaries) == 0 {
		return append(issues, "manifest has no shot summaries")
	}

	for _, shot := range inspect.ShotSummaries {
		if shot.PromptHash == "" {
			issues = append(issues, fmt.Sprintf("shot %d missing prompt_hash", shot.Index))
		} else {
			promptHash := strings.TrimSpace(shot.PromptHash)
			expectedHash := shotCacheHash(Shot{
				Prompt:      shot.Prompt,
				DurationSec: shot.DurationSec,
				AspectRatio: shot.AspectRatio,
				Resolution:  shot.Resolution,
			}, inspect.VideoModel)
			if shot.PromptHash != promptHash {
				issues = append(issues, fmt.Sprintf("shot %d prompt_hash %q is not canonical, want %q", shot.Index, shot.PromptHash, promptHash))
			} else if !isLowerHexSHA256(shot.PromptHash) {
				issues = append(issues, fmt.Sprintf("shot %d prompt_hash must be 64 lowercase hex characters", shot.Index))
			} else if shot.PromptHash != expectedHash {
				issues = append(issues, fmt.Sprintf("shot %d prompt_hash=%q, want %q", shot.Index, shot.PromptHash, expectedHash))
			}
		}
		requestID := strings.TrimSpace(shot.XAIRequestID)
		if requestID == "" {
			issues = append(issues, fmt.Sprintf("shot %d missing xai_request_id", shot.Index))
		} else if shot.XAIRequestID != requestID {
			issues = append(issues, fmt.Sprintf("shot %d xai_request_id %q is not canonical, want %q", shot.Index, shot.XAIRequestID, requestID))
		}
		status := strings.ToLower(strings.TrimSpace(shot.XAIStatus))
		if status == "" {
			issues = append(issues, fmt.Sprintf("shot %d missing xai_status", shot.Index))
		} else if status != "done" {
			issues = append(issues, fmt.Sprintf("shot %d xai_status=%q, want done", shot.Index, shot.XAIStatus))
		} else if shot.XAIStatus != status {
			issues = append(issues, fmt.Sprintf("shot %d xai_status %q is not canonical, want %q", shot.Index, shot.XAIStatus, status))
		}
	}

	if inspect.RunMetadata == nil {
		return append(issues, "missing xai_run_metadata.json")
	}
	issues = append(issues, validateRunMetadataOrigin(*inspect.RunMetadata)...)
	issues = append(issues, validateRunMetadataVideoModel(inspect.VideoModel, *inspect.RunMetadata)...)
	shotIndexes := make(map[int]struct{}, len(inspect.ShotSummaries))
	for _, shot := range inspect.ShotSummaries {
		shotIndexes[shot.Index] = struct{}{}
	}
	decisions := make(map[int]ShotDecision, len(inspect.RunMetadata.ShotDecisions))
	for _, decision := range inspect.RunMetadata.ShotDecisions {
		if _, ok := decisions[decision.Index]; ok {
			issues = append(issues, fmt.Sprintf("run metadata duplicate shot decision for shot %d", decision.Index))
		}
		if _, ok := shotIndexes[decision.Index]; !ok {
			issues = append(issues, fmt.Sprintf("run metadata shot decision for unknown shot %d", decision.Index))
		}
		decisions[decision.Index] = decision
	}
	issues = append(issues, validateRunMetadataShotSets(*inspect.RunMetadata, shotIndexes, decisions)...)
	issues = append(issues, validateRunMetadataForceRegenerate(*inspect.RunMetadata, decisions)...)
	for _, shot := range inspect.ShotSummaries {
		decision, ok := decisions[shot.Index]
		if !ok {
			issues = append(issues, fmt.Sprintf("run metadata missing shot decision for shot %d", shot.Index))
			continue
		}
		decisionRequestID := strings.TrimSpace(decision.XAIRequestID)
		if decisionRequestID == "" {
			issues = append(issues, fmt.Sprintf("run metadata shot %d missing xai_request_id", shot.Index))
		} else if decision.XAIRequestID != decisionRequestID {
			issues = append(issues, fmt.Sprintf("run metadata shot %d xai_request_id %q is not canonical, want %q", shot.Index, decision.XAIRequestID, decisionRequestID))
		}
		decisionStatus := strings.ToLower(strings.TrimSpace(decision.XAIStatus))
		if decisionStatus == "" {
			issues = append(issues, fmt.Sprintf("run metadata shot %d missing xai_status", shot.Index))
		} else if decisionStatus != "done" {
			issues = append(issues, fmt.Sprintf("run metadata shot %d xai_status=%q, want done", shot.Index, decision.XAIStatus))
		} else if decision.XAIStatus != decisionStatus {
			issues = append(issues, fmt.Sprintf("run metadata shot %d xai_status %q is not canonical, want %q", shot.Index, decision.XAIStatus, decisionStatus))
		}
		if decision.PromptHash == "" {
			issues = append(issues, fmt.Sprintf("run metadata shot %d missing prompt_hash", shot.Index))
		} else {
			decisionPromptHash := strings.TrimSpace(decision.PromptHash)
			if decision.PromptHash != decisionPromptHash {
				issues = append(issues, fmt.Sprintf("run metadata shot %d prompt_hash %q is not canonical, want %q", shot.Index, decision.PromptHash, decisionPromptHash))
			} else if !isLowerHexSHA256(decision.PromptHash) {
				issues = append(issues, fmt.Sprintf("run metadata shot %d prompt_hash must be 64 lowercase hex characters", shot.Index))
			}
		}
		decisionVideoPath := strings.TrimSpace(decision.VideoPath)
		if decision.VideoPath != decisionVideoPath {
			issues = append(issues, fmt.Sprintf("run metadata shot %d video_path %q is not canonical, want %q", shot.Index, decision.VideoPath, decisionVideoPath))
		} else if decision.VideoPath != shot.VideoPath {
			issues = append(issues, fmt.Sprintf("run metadata shot %d video_path=%q, want %q", shot.Index, decision.VideoPath, shot.VideoPath))
		}
		if decision.PromptHash != shot.PromptHash {
			issues = append(issues, fmt.Sprintf("run metadata shot %d prompt_hash=%q, want %q", shot.Index, decision.PromptHash, shot.PromptHash))
		}
		if decision.XAIRequestID != shot.XAIRequestID {
			issues = append(issues, fmt.Sprintf("run metadata shot %d xai_request_id=%q, want %q", shot.Index, decision.XAIRequestID, shot.XAIRequestID))
		}
		if decision.XAIStatus != shot.XAIStatus {
			issues = append(issues, fmt.Sprintf("run metadata shot %d xai_status=%q, want manifest status %q", shot.Index, decision.XAIStatus, shot.XAIStatus))
		}
	}
	return issues
}

func validateRunMetadataVideoModel(manifestVideoModel string, metadata RunMetadata) []string {
	runVideoModel := strings.TrimSpace(metadata.VideoModel)
	if runVideoModel == "" {
		return []string{"run metadata missing video_model"}
	}
	if metadata.VideoModel != runVideoModel {
		return []string{fmt.Sprintf("run metadata video_model %q is not canonical, want %q", metadata.VideoModel, runVideoModel)}
	}
	wantVideoModel := normalizeVideoModel(manifestVideoModel)
	if runVideoModel != wantVideoModel {
		return []string{fmt.Sprintf("run metadata video_model=%q, want manifest video_model %q", runVideoModel, wantVideoModel)}
	}
	return nil
}

func validateRunMetadataForceRegenerate(metadata RunMetadata, decisions map[int]ShotDecision) []string {
	if !metadata.ForceRegenerate {
		return nil
	}
	var issues []string
	for _, index := range metadata.ReusedShots {
		issues = append(issues, fmt.Sprintf("run metadata force_regenerate cannot reuse shot %d", index))
	}
	for index, decision := range decisions {
		if strings.ToLower(strings.TrimSpace(decision.Decision)) == "reused" {
			issues = append(issues, fmt.Sprintf("run metadata force_regenerate cannot have reused decision for shot %d", index))
		}
	}
	return issues
}

func validateRunMetadataOrigin(metadata RunMetadata) []string {
	var issues []string
	if metadata.Planned && metadata.ManifestReused {
		issues = append(issues, "run metadata planned and manifest_reused cannot both be true")
	}
	if !metadata.Planned && !metadata.ManifestReused {
		issues = append(issues, "run metadata must set exactly one of planned or manifest_reused")
	}
	if metadata.ForceReplan && metadata.ManifestReused {
		issues = append(issues, "run metadata force_replan cannot reuse manifest")
	}
	return issues
}

func validateRunMetadataShotSets(metadata RunMetadata, shotIndexes map[int]struct{}, decisions map[int]ShotDecision) []string {
	var issues []string
	generated := make(map[int]struct{}, len(metadata.GeneratedShots))
	reused := make(map[int]struct{}, len(metadata.ReusedShots))

	for _, index := range metadata.GeneratedShots {
		if _, ok := generated[index]; ok {
			issues = append(issues, fmt.Sprintf("run metadata duplicate generated_shots entry for shot %d", index))
		}
		if _, ok := shotIndexes[index]; !ok {
			issues = append(issues, fmt.Sprintf("run metadata generated_shots contains unknown shot %d", index))
		}
		generated[index] = struct{}{}
	}
	for _, index := range metadata.ReusedShots {
		if _, ok := reused[index]; ok {
			issues = append(issues, fmt.Sprintf("run metadata duplicate reused_shots entry for shot %d", index))
		}
		if _, ok := shotIndexes[index]; !ok {
			issues = append(issues, fmt.Sprintf("run metadata reused_shots contains unknown shot %d", index))
		}
		if _, ok := generated[index]; ok {
			issues = append(issues, fmt.Sprintf("run metadata shot %d appears in both generated_shots and reused_shots", index))
		}
		reused[index] = struct{}{}
	}

	for index, decision := range decisions {
		normalizedDecision := strings.ToLower(strings.TrimSpace(decision.Decision))
		if decision.Decision != normalizedDecision {
			issues = append(issues, fmt.Sprintf("run metadata shot %d decision %q is not canonical, want %q", index, decision.Decision, normalizedDecision))
		}
		switch normalizedDecision {
		case "generated":
			if _, ok := generated[index]; !ok {
				issues = append(issues, fmt.Sprintf("run metadata shot %d decision=generated missing from generated_shots", index))
			}
		case "reused":
			if _, ok := reused[index]; !ok {
				issues = append(issues, fmt.Sprintf("run metadata shot %d decision=reused missing from reused_shots", index))
			}
		default:
			issues = append(issues, fmt.Sprintf("run metadata shot %d decision=%q, want generated or reused", index, decision.Decision))
		}
	}
	for index := range generated {
		if decision, ok := decisions[index]; ok && strings.ToLower(strings.TrimSpace(decision.Decision)) != "generated" {
			issues = append(issues, fmt.Sprintf("run metadata generated_shots contains shot %d with decision=%q, want generated", index, decision.Decision))
		}
		if _, ok := decisions[index]; !ok {
			issues = append(issues, fmt.Sprintf("run metadata generated_shots contains shot %d without a shot decision", index))
		}
	}
	for index := range reused {
		if decision, ok := decisions[index]; ok && strings.ToLower(strings.TrimSpace(decision.Decision)) != "reused" {
			issues = append(issues, fmt.Sprintf("run metadata reused_shots contains shot %d with decision=%q, want reused", index, decision.Decision))
		}
		if _, ok := decisions[index]; !ok {
			issues = append(issues, fmt.Sprintf("run metadata reused_shots contains shot %d without a shot decision", index))
		}
	}
	return issues
}

func validationSpecFromInspect(inspect InspectSummary) RenderValidationSpec {
	spec := RenderValidationSpec{
		Width:          inspect.Width,
		Height:         inspect.Height,
		FPS:            inspect.FPS,
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
	for _, shot := range inspect.ShotSummaries {
		duration := shot.DurationSec
		if duration <= 0 {
			duration = defaultDurationSec
		}
		spec.ExpectedDurationSec += duration
	}
	return spec
}

func finalizeValidationStatus(summary ValidationSummary) ValidationSummary {
	if len(summary.Issues) == 0 {
		summary.Status = ValidationStatusValid
		return summary
	}
	summary.Status = ValidationStatusInvalid
	return summary
}
