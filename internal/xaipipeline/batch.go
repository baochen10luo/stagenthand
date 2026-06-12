package xaipipeline

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

// Runner is the minimal dependency needed for xAI-native batch production.
type Runner interface {
	Run(ctx context.Context, story []byte, opts RunOptions) (*Result, error)
}

type BatchOptions struct {
	Episodes    int
	Concurrency int
	RunOptions  RunOptions
}

type BatchEpisodeResult struct {
	Episode int     `json:"episode"`
	Result  *Result `json:"result,omitempty"`
	Error   string  `json:"error,omitempty"`
}

type BatchResult struct {
	TotalEpisodes int                  `json:"total_episodes"`
	Succeeded     int                  `json:"succeeded"`
	Failed        int                  `json:"failed"`
	OutputDir     string               `json:"output_dir"`
	Episodes      []BatchEpisodeResult `json:"episodes"`
}

func RunBatch(ctx context.Context, runner Runner, story []byte, opts BatchOptions) (*BatchResult, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if runner == nil {
		return nil, errors.New("xai batch runner is nil")
	}
	if opts.Episodes <= 0 {
		return nil, errors.New("xai batch episodes must be greater than zero")
	}
	if opts.Concurrency <= 0 {
		opts.Concurrency = 2
	}
	if strings.TrimSpace(string(story)) == "" {
		return nil, errors.New("xai batch story is empty")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	outputRoot := strings.TrimSpace(opts.RunOptions.OutputDir)
	if outputRoot == "" {
		return nil, errors.New("xai batch output dir is required")
	}
	if err := prepareBatchOutputRoot(outputRoot, opts.Episodes); err != nil {
		return nil, err
	}

	results := make([]BatchEpisodeResult, opts.Episodes)
	sem := make(chan struct{}, opts.Concurrency)
	var wg sync.WaitGroup

	for episode := 1; episode <= opts.Episodes; episode++ {
		episode := episode
		wg.Add(1)
		go func() {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()

			runOpts := opts.RunOptions
			runOpts.OutputDir = filepath.Join(outputRoot, fmt.Sprintf("episode_%03d", episode))
			result, err := runner.Run(ctx, story, runOpts)
			episodeResult := BatchEpisodeResult{Episode: episode}
			if err != nil {
				episodeResult.Error = err.Error()
			} else if result == nil {
				episodeResult.Error = fmt.Sprintf("xai batch episode %d result is nil", episode)
			} else {
				episodeResult.Result = result
			}
			results[episode-1] = episodeResult
		}()
	}
	wg.Wait()

	return tallyBatch(outputRoot, results), nil
}

func prepareBatchOutputRoot(outputRoot string, episodes int) error {
	if err := validateOutputRootPath(outputRoot, "xai batch output dir", "xAI-native batch root"); err != nil {
		return err
	}

	info, err := os.Lstat(outputRoot)
	switch {
	case err == nil && info.Mode()&os.ModeSymlink != 0:
		return fmt.Errorf("xai batch output dir %q is a symlink; xAI-native batch root must be an output-local directory", outputRoot)
	case err == nil && !info.IsDir():
		return fmt.Errorf("xai batch output dir %q is not a directory", outputRoot)
	case err == nil:
	case os.IsNotExist(err):
		if err := os.MkdirAll(outputRoot, 0755); err != nil {
			return fmt.Errorf("create xai batch output dir: %w", err)
		}
	default:
		return fmt.Errorf("stat xai batch output dir: %w", err)
	}

	var issues []string
	issues = append(issues, inspectBatchRootIssues(outputRoot)...)
	issues = append(issues, batchRootLegacyArtifactIssues(outputRoot)...)
	existingEpisodes, episodeIssues, err := inspectBatchEpisodeDirs(outputRoot)
	if err != nil {
		return err
	}
	issues = append(issues, episodeIssues...)
	for _, episode := range existingEpisodes {
		if episode.Number > episodes {
			issues = append(issues, fmt.Sprintf("unexpected existing episode_%03d directory for --episodes %d", episode.Number, episodes))
		}
	}
	if len(issues) > 0 {
		return errors.New(strings.Join(issues, "; "))
	}
	return nil
}

func validateOutputRootPath(outputRoot string, label string, rootDescription string) error {
	cleanRoot := filepath.Clean(outputRoot)
	info, err := os.Lstat(cleanRoot)
	if err == nil {
		if info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("%s %q is a symlink; %s must be an output-local directory", label, outputRoot, rootDescription)
		}
		if !info.IsDir() {
			return fmt.Errorf("%s %q is not a directory; %s must be an output-local directory", label, outputRoot, rootDescription)
		}
	}
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("stat %s: %w", label, err)
	}

	parent := filepath.Dir(cleanRoot)
	directParent := parent
	if parent == "." || parent == cleanRoot {
		return nil
	}
	for {
		info, err := os.Lstat(parent)
		if err == nil {
			if info.Mode()&os.ModeSymlink != 0 {
				if isRootLevelPath(parent) {
					next := filepath.Dir(parent)
					if next == "." || next == parent {
						return nil
					}
					parent = next
					continue
				}
				if parent == directParent {
					return fmt.Errorf("%s %q has symlink parent %q; %s must be an output-local directory", label, outputRoot, parent, rootDescription)
				}
				return fmt.Errorf("%s %q has symlink ancestor %q; %s must be an output-local directory", label, outputRoot, parent, rootDescription)
			}
		}
		if err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("stat %s parent: %w", label, err)
		}
		next := filepath.Dir(parent)
		if next == "." || next == parent {
			return nil
		}
		parent = next
	}
}

func isRootLevelPath(path string) bool {
	parent := filepath.Dir(path)
	if parent == path {
		return true
	}
	volume := filepath.VolumeName(path)
	if volume != "" {
		return parent == volume+string(os.PathSeparator)
	}
	return parent == string(os.PathSeparator)
}

func tallyBatch(outputRoot string, results []BatchEpisodeResult) *BatchResult {
	batch := &BatchResult{
		TotalEpisodes: len(results),
		OutputDir:     outputRoot,
		Episodes:      results,
	}
	for _, result := range results {
		if result.Error != "" {
			batch.Failed++
		} else {
			batch.Succeeded++
		}
	}
	return batch
}
