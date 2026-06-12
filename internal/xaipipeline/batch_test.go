package xaipipeline_test

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/baochen10luo/stagenthand/internal/xaipipeline"
)

type stubBatchRunner struct {
	mu        sync.Mutex
	calls     []batchCall
	failOn    map[int]error
	nilOn     map[int]bool
	inFlight  int
	maxFlight int
}

type batchCall struct {
	Input string
	Opts  xaipipeline.RunOptions
}

func (s *stubBatchRunner) Run(_ context.Context, story []byte, opts xaipipeline.RunOptions) (*xaipipeline.Result, error) {
	s.mu.Lock()
	s.inFlight++
	if s.inFlight > s.maxFlight {
		s.maxFlight = s.inFlight
	}
	s.calls = append(s.calls, batchCall{Input: string(story), Opts: opts})
	s.mu.Unlock()

	time.Sleep(10 * time.Millisecond)

	s.mu.Lock()
	s.inFlight--
	s.mu.Unlock()

	if err := s.failOn[episodeFromOutputDir(opts.OutputDir)]; err != nil {
		return nil, err
	}
	if s.nilOn[episodeFromOutputDir(opts.OutputDir)] {
		return nil, nil
	}
	return &xaipipeline.Result{
		Manifest: xaipipeline.Manifest{
			ProjectID: "episode",
			Shots:     []xaipipeline.Shot{{Index: 1}},
		},
		OutputDir:   opts.OutputDir,
		OutputVideo: filepath.Join(opts.OutputDir, "output_xai.mp4"),
	}, nil
}

func TestRunBatch_RunsEpisodesInSeparateOutputDirs(t *testing.T) {
	runner := &stubBatchRunner{}
	root := t.TempDir()

	got, err := xaipipeline.RunBatch(context.Background(), runner, []byte("story"), xaipipeline.BatchOptions{
		Episodes:    3,
		Concurrency: 2,
		RunOptions: xaipipeline.RunOptions{
			OutputDir:       root,
			ShandHome:       "/tmp/shand-home",
			TargetShots:     2,
			Format:          "portrait",
			ForceReplan:     true,
			ForceRegenerate: true,
		},
	})
	if err != nil {
		t.Fatalf("RunBatch: %v", err)
	}
	if got.TotalEpisodes != 3 || got.Succeeded != 3 || got.Failed != 0 {
		t.Fatalf("batch tally = %+v", got)
	}
	if len(got.Episodes) != 3 {
		t.Fatalf("episodes = %d, want 3", len(got.Episodes))
	}
	if len(runner.calls) != 3 {
		t.Fatalf("calls = %d, want 3", len(runner.calls))
	}

	seenDirs := map[string]bool{}
	for i, call := range runner.calls {
		if call.Input != "story" {
			t.Fatalf("call %d input = %q", i+1, call.Input)
		}
		seenDirs[call.Opts.OutputDir] = true
		if call.Opts.TargetShots != 2 || call.Opts.Format != "portrait" {
			t.Fatalf("call %d opts = %+v", i+1, call.Opts)
		}
		if !call.Opts.ForceReplan || !call.Opts.ForceRegenerate {
			t.Fatalf("call %d force flags not preserved: %+v", i+1, call.Opts)
		}
	}

	for i := range got.Episodes {
		wantDir := filepath.Join(root, "episode_"+zeroPad3(i+1))
		if !seenDirs[wantDir] {
			t.Fatalf("runner did not receive output dir %q; seen=%v", wantDir, seenDirs)
		}
		if got.Episodes[i].Episode != i+1 {
			t.Fatalf("episode index = %d, want %d", got.Episodes[i].Episode, i+1)
		}
		if got.Episodes[i].Result == nil || got.Episodes[i].Result.OutputDir != wantDir {
			t.Fatalf("episode %d result = %+v", i+1, got.Episodes[i].Result)
		}
	}
	if runner.maxFlight > 2 {
		t.Fatalf("max in-flight = %d, want <= 2", runner.maxFlight)
	}
}

func TestRunBatch_NilContextUsesBackground(t *testing.T) {
	runner := &stubBatchRunner{}
	root := t.TempDir()

	got, err := xaipipeline.RunBatch(nil, runner, []byte("story"), xaipipeline.BatchOptions{
		Episodes:    1,
		Concurrency: 1,
		RunOptions:  xaipipeline.RunOptions{OutputDir: root},
	})
	if err != nil {
		t.Fatalf("RunBatch() error = %v", err)
	}
	if got == nil || got.Succeeded != 1 || got.Failed != 0 {
		t.Fatalf("RunBatch() = %+v, want one successful episode", got)
	}
	if len(runner.calls) != 1 {
		t.Fatalf("runner calls = %d, want 1", len(runner.calls))
	}
}

func TestRunBatch_TalliesEpisodeFailures(t *testing.T) {
	runner := &stubBatchRunner{failOn: map[int]error{2: errors.New("episode failed")}}
	root := t.TempDir()

	got, err := xaipipeline.RunBatch(context.Background(), runner, []byte("story"), xaipipeline.BatchOptions{
		Episodes:    3,
		Concurrency: 3,
		RunOptions:  xaipipeline.RunOptions{OutputDir: root},
	})
	if err != nil {
		t.Fatalf("RunBatch: %v", err)
	}
	if got.Succeeded != 2 || got.Failed != 1 {
		t.Fatalf("batch tally = %+v", got)
	}
	if got.Episodes[1].Error != "episode failed" {
		t.Fatalf("episode 2 error = %q", got.Episodes[1].Error)
	}
	if got.Episodes[0].Result == nil || got.Episodes[2].Result == nil {
		t.Fatalf("successful episode results missing: %+v", got.Episodes)
	}
}

func TestRunBatch_TalliesNilEpisodeResultAsFailure(t *testing.T) {
	runner := &stubBatchRunner{nilOn: map[int]bool{2: true}}
	root := t.TempDir()

	got, err := xaipipeline.RunBatch(context.Background(), runner, []byte("story"), xaipipeline.BatchOptions{
		Episodes:    3,
		Concurrency: 3,
		RunOptions:  xaipipeline.RunOptions{OutputDir: root},
	})
	if err != nil {
		t.Fatalf("RunBatch: %v", err)
	}
	if got.Succeeded != 2 || got.Failed != 1 {
		t.Fatalf("batch tally = %+v, want 2 succeeded and 1 failed", got)
	}
	if got.Episodes[1].Result != nil {
		t.Fatalf("episode 2 result = %+v, want nil", got.Episodes[1].Result)
	}
	if !strings.Contains(got.Episodes[1].Error, "episode 2 result is nil") {
		t.Fatalf("episode 2 error = %q, want nil result error", got.Episodes[1].Error)
	}
}

func TestRunBatch_RequiresOutputDir(t *testing.T) {
	_, err := xaipipeline.RunBatch(context.Background(), &stubBatchRunner{}, []byte("story"), xaipipeline.BatchOptions{
		Episodes: 2,
	})
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestRunBatch_RejectsEmptyStoryBeforePreparingOutputRoot(t *testing.T) {
	runner := &stubBatchRunner{}
	outputRoot := filepath.Join(t.TempDir(), "batch-root")

	got, err := xaipipeline.RunBatch(context.Background(), runner, []byte(" \n\t "), xaipipeline.BatchOptions{
		Episodes:   2,
		RunOptions: xaipipeline.RunOptions{OutputDir: outputRoot},
	})
	if err == nil {
		t.Fatal("RunBatch() error = nil, want empty story error")
	}
	if !strings.Contains(err.Error(), "story is empty") {
		t.Fatalf("RunBatch() error = %v, want empty story error", err)
	}
	if got != nil {
		t.Fatalf("RunBatch() result = %+v, want nil on story preflight failure", got)
	}
	if len(runner.calls) != 0 {
		t.Fatalf("runner calls = %d, want 0", len(runner.calls))
	}
	if _, err := os.Stat(outputRoot); !os.IsNotExist(err) {
		t.Fatalf("output root should not be created before story validation, stat err=%v", err)
	}
}

func TestRunBatch_RejectsCanceledContextBeforePreparingOutputRoot(t *testing.T) {
	runner := &stubBatchRunner{}
	outputRoot := filepath.Join(t.TempDir(), "batch-root")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	got, err := xaipipeline.RunBatch(ctx, runner, []byte("story"), xaipipeline.BatchOptions{
		Episodes:   2,
		RunOptions: xaipipeline.RunOptions{OutputDir: outputRoot},
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("RunBatch() error = %v, want context.Canceled", err)
	}
	if got != nil {
		t.Fatalf("RunBatch() result = %+v, want nil on canceled context", got)
	}
	if len(runner.calls) != 0 {
		t.Fatalf("runner calls = %d, want 0", len(runner.calls))
	}
	if _, err := os.Stat(outputRoot); !os.IsNotExist(err) {
		t.Fatalf("output root should not be created after context cancellation, stat err=%v", err)
	}
}

func TestRunBatch_RejectsSymlinkedOutputRootBeforeRunningEpisodes(t *testing.T) {
	runner := &stubBatchRunner{}
	externalRoot := t.TempDir()
	parent := t.TempDir()
	outputRoot := filepath.Join(parent, "batch-root")
	if err := os.Symlink(externalRoot, outputRoot); err != nil {
		t.Skipf("symlink not available: %v", err)
	}

	got, err := xaipipeline.RunBatch(context.Background(), runner, []byte("story"), xaipipeline.BatchOptions{
		Episodes:   2,
		RunOptions: xaipipeline.RunOptions{OutputDir: outputRoot},
	})
	if err == nil {
		t.Fatal("RunBatch() error = nil, want symlinked output root error")
	}
	if !strings.Contains(err.Error(), "xai batch output dir") || !strings.Contains(err.Error(), "symlink") {
		t.Fatalf("RunBatch() error = %v, want symlinked output root error", err)
	}
	if got != nil {
		t.Fatalf("RunBatch() result = %+v, want nil on preflight failure", got)
	}
	if len(runner.calls) != 0 {
		t.Fatalf("runner calls = %d, want 0", len(runner.calls))
	}
}

func TestRunBatch_RejectsExistingFileOutputRootBeforeRunningEpisodes(t *testing.T) {
	runner := &stubBatchRunner{}
	outputRoot := filepath.Join(t.TempDir(), "batch-root")
	if err := os.WriteFile(outputRoot, []byte("not a directory"), 0644); err != nil {
		t.Fatalf("write output file: %v", err)
	}

	got, err := xaipipeline.RunBatch(context.Background(), runner, []byte("story"), xaipipeline.BatchOptions{
		Episodes:   2,
		RunOptions: xaipipeline.RunOptions{OutputDir: outputRoot},
	})
	if err == nil {
		t.Fatal("RunBatch() error = nil, want file output root error")
	}
	if !strings.Contains(err.Error(), "xai batch output dir") || !strings.Contains(err.Error(), "not a directory") {
		t.Fatalf("RunBatch() error = %v, want file output root error", err)
	}
	if got != nil {
		t.Fatalf("RunBatch() result = %+v, want nil on preflight failure", got)
	}
	if len(runner.calls) != 0 {
		t.Fatalf("runner calls = %d, want 0", len(runner.calls))
	}
}

func TestRunBatch_RejectsOutputRootUnderFileParentBeforeRunningEpisodes(t *testing.T) {
	runner := &stubBatchRunner{}
	parent := t.TempDir()
	fileParent := filepath.Join(parent, "not-a-dir")
	if err := os.WriteFile(fileParent, []byte("not a directory"), 0644); err != nil {
		t.Fatalf("write parent file: %v", err)
	}
	outputRoot := filepath.Join(fileParent, "batch-root")

	got, err := xaipipeline.RunBatch(context.Background(), runner, []byte("story"), xaipipeline.BatchOptions{
		Episodes:   2,
		RunOptions: xaipipeline.RunOptions{OutputDir: outputRoot},
	})
	if err == nil {
		t.Fatal("RunBatch() error = nil, want file parent output root error")
	}
	if !strings.Contains(err.Error(), "xai batch output dir") || !strings.Contains(err.Error(), "not a directory") {
		t.Fatalf("RunBatch() error = %v, want file parent output root error", err)
	}
	if got != nil {
		t.Fatalf("RunBatch() result = %+v, want nil on preflight failure", got)
	}
	if len(runner.calls) != 0 {
		t.Fatalf("runner calls = %d, want 0", len(runner.calls))
	}
}

func TestRunBatch_RejectsSymlinkedOutputRootParentBeforeRunningEpisodes(t *testing.T) {
	runner := &stubBatchRunner{}
	externalParent := t.TempDir()
	parent := t.TempDir()
	linkedParent := filepath.Join(parent, "linked-parent")
	if err := os.Symlink(externalParent, linkedParent); err != nil {
		t.Skipf("symlink not available: %v", err)
	}
	outputRoot := filepath.Join(linkedParent, "batch-root")

	got, err := xaipipeline.RunBatch(context.Background(), runner, []byte("story"), xaipipeline.BatchOptions{
		Episodes:   2,
		RunOptions: xaipipeline.RunOptions{OutputDir: outputRoot},
	})
	if err == nil {
		t.Fatal("RunBatch() error = nil, want symlinked output root parent error")
	}
	if !strings.Contains(err.Error(), "xai batch output dir") || !strings.Contains(err.Error(), "symlink") {
		t.Fatalf("RunBatch() error = %v, want symlinked output root parent error", err)
	}
	if got != nil {
		t.Fatalf("RunBatch() result = %+v, want nil on preflight failure", got)
	}
	if len(runner.calls) != 0 {
		t.Fatalf("runner calls = %d, want 0", len(runner.calls))
	}
	if _, statErr := os.Stat(filepath.Join(externalParent, "batch-root")); !os.IsNotExist(statErr) {
		t.Fatalf("external batch root should not be created through symlink parent, stat err=%v", statErr)
	}
}

func TestRunBatch_RejectsExistingOutputRootUnderSymlinkedParentBeforeRunningEpisodes(t *testing.T) {
	runner := &stubBatchRunner{}
	externalParent := t.TempDir()
	parent := t.TempDir()
	linkedParent := filepath.Join(parent, "linked-parent")
	if err := os.Symlink(externalParent, linkedParent); err != nil {
		t.Skipf("symlink not available: %v", err)
	}
	outputRoot := filepath.Join(linkedParent, "batch-root")
	if err := os.MkdirAll(filepath.Join(externalParent, "batch-root"), 0755); err != nil {
		t.Fatalf("mkdir external batch root: %v", err)
	}

	got, err := xaipipeline.RunBatch(context.Background(), runner, []byte("story"), xaipipeline.BatchOptions{
		Episodes:   2,
		RunOptions: xaipipeline.RunOptions{OutputDir: outputRoot},
	})
	if err == nil {
		t.Fatal("RunBatch() error = nil, want existing output root under symlinked parent error")
	}
	if !strings.Contains(err.Error(), "xai batch output dir") || !strings.Contains(err.Error(), "symlink") {
		t.Fatalf("RunBatch() error = %v, want symlinked output root parent error", err)
	}
	if got != nil {
		t.Fatalf("RunBatch() result = %+v, want nil on preflight failure", got)
	}
	if len(runner.calls) != 0 {
		t.Fatalf("runner calls = %d, want 0", len(runner.calls))
	}
}

func TestRunBatch_RejectsSymlinkedOutputRootAncestorBeforeRunningEpisodes(t *testing.T) {
	runner := &stubBatchRunner{}
	externalAncestor := t.TempDir()
	parent := t.TempDir()
	linkedAncestor := filepath.Join(parent, "linked-ancestor")
	if err := os.Symlink(externalAncestor, linkedAncestor); err != nil {
		t.Skipf("symlink not available: %v", err)
	}
	outputRoot := filepath.Join(linkedAncestor, "missing-parent", "batch-root")

	got, err := xaipipeline.RunBatch(context.Background(), runner, []byte("story"), xaipipeline.BatchOptions{
		Episodes:   2,
		RunOptions: xaipipeline.RunOptions{OutputDir: outputRoot},
	})
	if err == nil {
		t.Fatal("RunBatch() error = nil, want symlinked output root ancestor error")
	}
	if !strings.Contains(err.Error(), "xai batch output dir") || !strings.Contains(err.Error(), "symlink") {
		t.Fatalf("RunBatch() error = %v, want symlinked output root ancestor error", err)
	}
	if got != nil {
		t.Fatalf("RunBatch() result = %+v, want nil on preflight failure", got)
	}
	if len(runner.calls) != 0 {
		t.Fatalf("runner calls = %d, want 0", len(runner.calls))
	}
	if _, statErr := os.Stat(filepath.Join(externalAncestor, "missing-parent")); !os.IsNotExist(statErr) {
		t.Fatalf("external batch root should not be created through symlink ancestor, stat err=%v", statErr)
	}
}

func TestRunBatch_RejectsExistingOutputRootUnderSymlinkedAncestorBeforeRunningEpisodes(t *testing.T) {
	runner := &stubBatchRunner{}
	externalAncestor := t.TempDir()
	parent := t.TempDir()
	linkedAncestor := filepath.Join(parent, "linked-ancestor")
	if err := os.Symlink(externalAncestor, linkedAncestor); err != nil {
		t.Skipf("symlink not available: %v", err)
	}
	outputRoot := filepath.Join(linkedAncestor, "existing-parent", "batch-root")
	if err := os.MkdirAll(filepath.Join(externalAncestor, "existing-parent", "batch-root"), 0755); err != nil {
		t.Fatalf("mkdir external batch root: %v", err)
	}

	got, err := xaipipeline.RunBatch(context.Background(), runner, []byte("story"), xaipipeline.BatchOptions{
		Episodes:   2,
		RunOptions: xaipipeline.RunOptions{OutputDir: outputRoot},
	})
	if err == nil {
		t.Fatal("RunBatch() error = nil, want existing output root under symlinked ancestor error")
	}
	if !strings.Contains(err.Error(), "xai batch output dir") || !strings.Contains(err.Error(), "symlink") {
		t.Fatalf("RunBatch() error = %v, want symlinked output root ancestor error", err)
	}
	if got != nil {
		t.Fatalf("RunBatch() result = %+v, want nil on preflight failure", got)
	}
	if len(runner.calls) != 0 {
		t.Fatalf("runner calls = %d, want 0", len(runner.calls))
	}
}

func TestRunBatch_RejectsSymlinkedEpisodeDirBeforeRunningEpisodes(t *testing.T) {
	runner := &stubBatchRunner{}
	outputRoot := t.TempDir()
	externalEpisode := filepath.Join(t.TempDir(), "external-episode")
	if err := os.MkdirAll(externalEpisode, 0755); err != nil {
		t.Fatalf("mkdir external episode: %v", err)
	}
	if err := os.Symlink(externalEpisode, filepath.Join(outputRoot, "episode_001")); err != nil {
		t.Skipf("symlink not available: %v", err)
	}

	got, err := xaipipeline.RunBatch(context.Background(), runner, []byte("story"), xaipipeline.BatchOptions{
		Episodes:   2,
		RunOptions: xaipipeline.RunOptions{OutputDir: outputRoot},
	})
	if err == nil {
		t.Fatal("RunBatch() error = nil, want symlinked episode dir error")
	}
	if !strings.Contains(err.Error(), `episode directory "episode_001" is a symlink`) {
		t.Fatalf("RunBatch() error = %v, want symlinked episode dir error", err)
	}
	if got != nil {
		t.Fatalf("RunBatch() result = %+v, want nil on preflight failure", got)
	}
	if len(runner.calls) != 0 {
		t.Fatalf("runner calls = %d, want 0", len(runner.calls))
	}
}

func TestRunBatch_RejectsExistingEpisodeBeyondRequestedCountBeforeRunningEpisodes(t *testing.T) {
	runner := &stubBatchRunner{}
	outputRoot := t.TempDir()
	for episode := 1; episode <= 3; episode++ {
		if err := os.MkdirAll(filepath.Join(outputRoot, "episode_"+zeroPad3(episode)), 0755); err != nil {
			t.Fatalf("mkdir episode %d: %v", episode, err)
		}
	}

	got, err := xaipipeline.RunBatch(context.Background(), runner, []byte("story"), xaipipeline.BatchOptions{
		Episodes:   2,
		RunOptions: xaipipeline.RunOptions{OutputDir: outputRoot},
	})
	if err == nil {
		t.Fatal("RunBatch() error = nil, want unexpected existing episode error")
	}
	if !strings.Contains(err.Error(), "unexpected existing episode_003 directory") {
		t.Fatalf("RunBatch() error = %v, want unexpected episode_003 error", err)
	}
	if got != nil {
		t.Fatalf("RunBatch() result = %+v, want nil on preflight failure", got)
	}
	if len(runner.calls) != 0 {
		t.Fatalf("runner calls = %d, want 0", len(runner.calls))
	}
}

func zeroPad3(n int) string {
	return fmt.Sprintf("%03d", n)
}

func episodeFromOutputDir(outputDir string) int {
	base := filepath.Base(outputDir)
	number := strings.TrimPrefix(base, "episode_")
	episode, _ := strconv.Atoi(number)
	return episode
}
