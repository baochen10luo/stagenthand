package domain_test

import (
	"testing"
	"time"

	"github.com/castle-studio-work/stagenthand/internal/domain"
)

func TestJobStatus_String(t *testing.T) {
	tests := []struct {
		status domain.JobStatus
		want   string
	}{
		{domain.JobStatusPending, "pending"},
		{domain.JobStatusRunning, "running"},
		{domain.JobStatusWaitingHITL, "waiting_hitl"},
		{domain.JobStatusCompleted, "completed"},
		{domain.JobStatusFailed, "failed"},
	}
	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			if got := tt.status.String(); got != tt.want {
				t.Errorf("JobStatus.String() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestCheckpointStage_String(t *testing.T) {
	tests := []struct {
		stage domain.CheckpointStage
		want  string
	}{
		{domain.StageOutline, "outline"},
		{domain.StageStoryboard, "storyboard"},
		{domain.StageImages, "images"},
		{domain.StageFinal, "final"},
	}
	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			if got := tt.stage.String(); got != tt.want {
				t.Errorf("CheckpointStage.String() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestPanel_HasCharacterRefs(t *testing.T) {
	p1 := domain.Panel{CharacterRefs: []string{"char_a.png"}}
	p2 := domain.Panel{CharacterRefs: []string{}}

	if !p1.HasCharacterRefs() {
		t.Error("expected p1.HasCharacterRefs() == true")
	}
	if p2.HasCharacterRefs() {
		t.Error("expected p2.HasCharacterRefs() == false")
	}
}

func TestJob_IsTerminal(t *testing.T) {
	cases := []struct {
		status   domain.JobStatus
		terminal bool
	}{
		{domain.JobStatusCompleted, true},
		{domain.JobStatusFailed, true},
		{domain.JobStatusPending, false},
		{domain.JobStatusRunning, false},
		{domain.JobStatusWaitingHITL, false},
	}
	for _, c := range cases {
		j := domain.Job{Status: c.status, CreatedAt: time.Now()}
		if got := j.IsTerminal(); got != c.terminal {
			t.Errorf("Job{%s}.IsTerminal() = %v, want %v", c.status, got, c.terminal)
		}
	}
}
