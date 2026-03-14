// Package domain defines pure data structures for StagentHand.
// No external dependencies, no methods with side effects.
package domain

import "time"

// JobStatus represents the lifecycle state of a pipeline job.
type JobStatus string

const (
	JobStatusPending     JobStatus = "pending"
	JobStatusRunning     JobStatus = "running"
	JobStatusWaitingHITL JobStatus = "waiting_hitl"
	JobStatusCompleted   JobStatus = "completed"
	JobStatusFailed      JobStatus = "failed"
)

// String implements fmt.Stringer.
func (s JobStatus) String() string { return string(s) }

// IsTerminal returns true if the status is a final state (no further transitions).
func (j Job) IsTerminal() bool {
	return j.Status == JobStatusCompleted || j.Status == JobStatusFailed
}

// CheckpointStage identifies one of the four HITL pause points.
type CheckpointStage string

const (
	StageOutline    CheckpointStage = "outline"
	StageStoryboard CheckpointStage = "storyboard"
	StageImages     CheckpointStage = "images"
	StageFinal      CheckpointStage = "final"
)

// String implements fmt.Stringer.
func (s CheckpointStage) String() string { return string(s) }

// CheckpointStatus represents whether a HITL checkpoint has been resolved.
type CheckpointStatus string

const (
	CheckpointStatusPending  CheckpointStatus = "pending"
	CheckpointStatusApproved CheckpointStatus = "approved"
	CheckpointStatusRejected CheckpointStatus = "rejected"
)

// --- Core domain types ---

// Project is the top-level entity representing a short drama project.
type Project struct {
	ID          string    `json:"id"`
	Title       string    `json:"title"`
	Description string    `json:"description"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

// Outline is the high-level structural plan produced from a story prompt.
type Outline struct {
	ProjectID string    `json:"project_id"`
	Episodes  []Episode `json:"episodes"`
	CreatedAt time.Time `json:"created_at"`
}

// Episode represents a single episode within an outline.
type Episode struct {
	Number      int    `json:"number"`
	Title       string `json:"title"`
	Synopsis    string `json:"synopsis"`
	Hook        string `json:"hook"`        // opening hook
	Cliffhanger string `json:"cliffhanger"` // ending cliffhanger
}

// Storyboard is the detailed scene-by-scene plan for a single episode.
type Storyboard struct {
	ProjectID string    `json:"project_id"`
	Episode   int       `json:"episode"`
	Scenes    []Scene   `json:"scenes"`
	CreatedAt time.Time `json:"created_at"`
}

// Scene is a narrative unit within a storyboard.
type Scene struct {
	Number      int     `json:"number"`
	Description string  `json:"description"`
	Panels      []Panel `json:"panels"`
}

// Panel represents a single image frame with associated metadata.
type Panel struct {
	SceneNumber   int      `json:"scene_number"`
	PanelNumber   int      `json:"panel_number"`
	Description   string   `json:"description"`         // image generation prompt
	Dialogue      string   `json:"dialogue"`            // subtitle text
	CharacterRefs []string `json:"character_refs"`      // paths to character reference images
	ImageURL      string   `json:"image_url,omitempty"` // populated after generation
	AudioURL      string   `json:"audio_url,omitempty"` // populated after TTS generation
	DurationSec   float64  `json:"duration_sec"`        // display duration in Remotion
}

// HasCharacterRefs returns true if any character reference images are specified.
func (p Panel) HasCharacterRefs() bool {
	return len(p.CharacterRefs) > 0
}

// Job tracks an async pipeline task (e.g. image generation).
type Job struct {
	ID        string    `json:"id"`
	ProjectID string    `json:"project_id"`
	Type      string    `json:"type"` // e.g. "panel-to-image", "pipeline"
	Status    JobStatus `json:"status"`
	Error     string    `json:"error,omitempty"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// Checkpoint represents a HITL pause point awaiting human or agent approval.
type Checkpoint struct {
	ID        string           `json:"id"`
	JobID     string           `json:"job_id"`
	Stage     CheckpointStage  `json:"stage"`
	Status    CheckpointStatus `json:"status"`
	Notes     string           `json:"notes,omitempty"`
	CreatedAt time.Time        `json:"created_at"`
	UpdatedAt time.Time        `json:"updated_at"`
}

// RemotionProps is the JSON payload passed to the Remotion template.
type RemotionProps struct {
	ProjectID string  `json:"project_id"`
	Title     string  `json:"title"`
	Panels    []Panel `json:"panels"`
	FPS       int     `json:"fps"`    // default 24
	Width     int     `json:"width"`  // default 1024
	Height    int     `json:"height"` // default 576
}
