package xaipipeline

import "context"

const (
	defaultFormat      = "portrait"
	defaultFPS         = 24
	defaultWidth       = 720
	defaultHeight      = 1280
	defaultDurationSec = 8.0
	defaultAspectRatio = "9:16"
	defaultResolution  = "720p"
	defaultVideoModel  = "grok-imagine-video"
	defaultTransition  = "cut"
	defaultCodecName   = "h264"
	defaultPixelFormat = "yuv420p"
)

// Manifest is the xAI-native production manifest. It is the source of truth for
// shot generation, resume behavior, timeline rendering, and final validation.
type Manifest struct {
	ProjectID  string `json:"project_id"`
	StoryHash  string `json:"story_hash,omitempty"`
	VideoModel string `json:"video_model,omitempty"`
	Format     string `json:"format"`
	FPS        int    `json:"fps"`
	Width      int    `json:"width"`
	Height     int    `json:"height"`
	Shots      []Shot `json:"shots"`
}

type Shot struct {
	Index         int     `json:"index"`
	Prompt        string  `json:"prompt"`
	PromptHash    string  `json:"prompt_hash,omitempty"`
	XAIRequestID  string  `json:"xai_request_id,omitempty"`
	XAIStatus     string  `json:"xai_status,omitempty"`
	DurationSec   float64 `json:"duration_sec"`
	AspectRatio   string  `json:"aspect_ratio"`
	Resolution    string  `json:"resolution"`
	VideoPath     string  `json:"video_path,omitempty"`
	Subtitle      string  `json:"subtitle,omitempty"`
	TransitionOut string  `json:"transition_out,omitempty"`
}

type PlanInput struct {
	Story       string
	TargetShots int
	Format      string
}

type RunOptions struct {
	OutputDir       string
	ShandHome       string
	TargetShots     int
	Format          string
	VideoModel      string
	ForceReplan     bool
	ForceRegenerate bool
}

type RunMetadata struct {
	Planned         bool           `json:"planned"`
	ManifestReused  bool           `json:"manifest_reused"`
	VideoModel      string         `json:"video_model,omitempty"`
	ForceReplan     bool           `json:"force_replan"`
	ForceRegenerate bool           `json:"force_regenerate"`
	GeneratedShots  []int          `json:"generated_shots,omitempty"`
	ReusedShots     []int          `json:"reused_shots,omitempty"`
	ShotDecisions   []ShotDecision `json:"shot_decisions,omitempty"`
}

type ShotDecision struct {
	Index        int    `json:"index"`
	Decision     string `json:"decision"`
	VideoPath    string `json:"video_path"`
	PromptHash   string `json:"prompt_hash,omitempty"`
	XAIRequestID string `json:"xai_request_id,omitempty"`
	XAIStatus    string `json:"xai_status,omitempty"`
}

type Result struct {
	Manifest           Manifest    `json:"manifest"`
	RunMetadata        RunMetadata `json:"run_metadata"`
	OutputDir          string      `json:"output_dir"`
	OutputVideo        string      `json:"output_video"`
	ManifestPath       string      `json:"xai_manifest"`
	RunMetadataPath    string      `json:"run_metadata_path"`
	RenderMetadataPath string      `json:"render_metadata"`
	PreviewFramePath   string      `json:"preview_frame"`
}

// Planner turns raw story input into an xAI-native shot manifest.
type Planner interface {
	Plan(ctx context.Context, input PlanInput) (Manifest, error)
}

// ShotGenerator generates one MP4 for a planned shot. It remains available for
// direct byte-oriented callers; the xAI-native orchestrator requires
// ShotResultGenerator so provider metadata is returned with every generated
// shot.
type ShotGenerator interface {
	GenerateShot(ctx context.Context, shot Shot) ([]byte, error)
}

type ShotGenerationResult struct {
	Data      []byte
	RequestID string
	Status    string
}

type ShotResultGenerator interface {
	GenerateShotResult(ctx context.Context, shot Shot) (ShotGenerationResult, error)
}

// Renderer composes generated shots into the final output using deterministic
// tooling such as HyperFrames and FFmpeg.
type Renderer interface {
	Render(ctx context.Context, manifest Manifest, outputDir string) (string, error)
}

// ShotValidator validates cached/generated MP4s for resume decisions.
type ShotValidator interface {
	ValidShot(ctx context.Context, path string, spec RenderValidationSpec) bool
}

type Deps struct {
	Planner       Planner
	ShotGenerator ShotResultGenerator
	Renderer      Renderer
	Validator     ShotValidator
}
