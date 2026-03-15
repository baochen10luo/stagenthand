package video

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"github.com/baochen10luo/stagenthand/internal/llm"
)

const maxVideoBytes = 20 * 1024 * 1024 // 20MB

// compressForCritic compresses a video file if it exceeds maxVideoBytes.
// Returns the path to use (original or temp), and a cleanup function.
// If no compression is needed, cleanup is a no-op.
func compressForCritic(videoPath string) (usePath string, cleanup func(), err error) {
	info, err := os.Stat(videoPath)
	if err != nil {
		return "", func() {}, err
	}

	// Small enough: use as-is
	if info.Size() <= maxVideoBytes {
		return videoPath, func() {}, nil
	}

	// Check ffmpeg availability before attempting compression
	if _, lookErr := exec.LookPath("ffmpeg"); lookErr != nil {
		return "", func() {}, fmt.Errorf("ffmpeg not found, please install ffmpeg")
	}

	// Need compression: run ffmpeg
	tmpFile := filepath.Join(os.TempDir(), fmt.Sprintf("critic_compressed_%d.mp4", time.Now().UnixNano()))

	cmd := exec.Command("ffmpeg",
		"-i", videoPath,
		"-vf", "scale=640:-2",
		"-b:v", "500k",
		"-y",
		tmpFile,
	)
	if out, runErr := cmd.CombinedOutput(); runErr != nil {
		return "", func() {}, fmt.Errorf("ffmpeg compression failed: %s: %w", string(out), runErr)
	}

	cleanup = func() { os.Remove(tmpFile) }
	return tmpFile, cleanup, nil
}

// Evaluation represents the multi-dimensional scoring and feedback from the AI Critic.
type Evaluation struct {
	VisualScore    int    `json:"visual_score"`    // 0-10: Visual Coherence
	AudioSyncScore int    `json:"audio_sync_score"` // 0-10: Audio-Visual Sync
	AdherenceScore int    `json:"adherence_score"`  // 0-10: Directive Adherence
	ToneScore      int    `json:"tone_score"`       // 0-10: Narrative Tone
	Feedback       string `json:"feedback"`         // Specific advice for JSON JSON configuration modifications
	Action         string `json:"action"`           // "APPROVE" or "REJECT"
}

// CheckApproval returns true if the video meets the hard-stop convergence criteria.
func (e *Evaluation) CheckApproval() bool {
	// Linus rules: objective failures are fatal.
	if e.VisualScore < 8 || e.AudioSyncScore < 8 {
		return false
	}
	// Subjective/Directive failures are tolerable if the total score is high enough.
	total := e.VisualScore + e.AudioSyncScore + e.AdherenceScore + e.ToneScore
	return total >= 32
}

// Critic evaluates a generated video MP4 against the original JSON configuration
// using an advanced multi-modal foundation model.
type Critic struct {
	client llm.VideoCriticClient
}

// NewCritic creates a new AI Critic with the given multi-modal LLM client.
func NewCritic(client llm.VideoCriticClient) *Critic {
	return &Critic{client: client}
}

// Evaluate uses the configured multi-modal model to watch the video, read the directives,
// and return a structured Evaluation.
// If the video exceeds 20MB, it is automatically compressed via ffmpeg before being sent to the model.
func (c *Critic) Evaluate(ctx context.Context, videoPath string, propsJSONData []byte) (*Evaluation, error) {
	// Auto-compress if video is too large for the model
	evalPath, cleanup, err := compressForCritic(videoPath)
	if err != nil {
		return nil, fmt.Errorf("failed to prepare video for critique: %w", err)
	}
	defer cleanup()

	videoBytes, err := os.ReadFile(evalPath)
	if err != nil {
		return nil, fmt.Errorf("could not read video file for critique: %w", err)
	}

	systemPrompt := `You are an elite, uncompromising AI Film Critic and Technical Director evaluating an AI-generated video. You demand absolute perfection in continuity and storytelling.
You will be provided with:
1. The rendered MP4 video.
2. The original 'RemotionProps' JSON that generated the video.

Your job is to strictly evaluate the video across 4 dimensions, scoring each out of 10. YOU MUST BE HARSH. Do not give passes for "AI limitations".
1. 'visual_score': Check for glitches and STYLE DRIFT. Visual consistency is paramount. FATAL FLAW: CHARACTER CONSISTENCY. If a main character's face, clothing, or prominent features drastically change between scenes without narrative reason, score < 6. FATAL FLAW: If the on-screen subtitles display leaked metadata tags like "VO:", "V.O.", "Narrator:", or raw quotes, this destroys immersion. If you see this, score < 5.
2. 'audio_sync_score': Check for audio ducking and voice naturalness. FATAL FLAW: When voiceover starts, the background music (BGM) must elegantly duck (fade down texturally) and fade back up when the voice stops. If the BGM rudely cuts off or drowns out the voice, score < 6. Also check for subtitle desync.
3. 'adherence_score': BGM Contextual Match. The BGM must fit the StylePrompt and narrative atmosphere. If a dark cyberpunk scene plays epic heroic music, or a sad desolate scene plays upbeat pop, score < 5. Did the video obey the visual directives inside the JSON?
4. 'tone_score': Narrative Completeness. Does the pacing give the viewer breathing room? If a short line rushes abruptly to the next scene without a dramatic pause, penalize it. If a viewer watching this would be confused about the story, or if the ending (e.g., just saying "Not tonight") lacks context and narrative closure, score < 6.

If 'visual_score' or 'audio_sync_score' are below 8, or the total score is below 32, you MUST provide 'action': 'REJECT'. Otherwise, 'APPROVE'.
In 'feedback', relentlessly pinpoint the artistic and technical flaws. Be specific.

Respond ONLY with valid JSON matching EXACTLY this structure, with no markdown formatting around it:
{
  "visual_score": 10,
  "audio_sync_score": 10,
  "adherence_score": 10,
  "tone_score": 10,
  "action": "APPROVE",
  "feedback": "..."
}`

	respBytes, err := c.client.ReviewVideo(ctx, systemPrompt, propsJSONData, "mp4", videoBytes)
	if err != nil {
		return nil, fmt.Errorf("video analysis by LLM failed: %w", err)
	}

	var eval Evaluation
	if err := json.Unmarshal(respBytes, &eval); err != nil {
		return nil, fmt.Errorf("failed to parse critic evaluation json: %w (raw response: %s)", err, string(respBytes))
	}

	return &eval, nil
}
