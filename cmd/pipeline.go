package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/baochen10luo/stagenthand/internal/audio"
	"github.com/baochen10luo/stagenthand/internal/domain"
	"github.com/baochen10luo/stagenthand/internal/image"
	"github.com/baochen10luo/stagenthand/internal/llm"
	"github.com/baochen10luo/stagenthand/internal/pipeline"
	"github.com/baochen10luo/stagenthand/internal/remotion"
	"github.com/baochen10luo/stagenthand/internal/store"
	"github.com/spf13/cobra"
)

var (
	pipelineSkipHITL  bool
	pipelineOutputDir string
)

var pipelineCmd = &cobra.Command{
	Use:   "pipeline",
	Short: "Run the full AI short drama pipeline end-to-end",
	Long: `Reads a story prompt or storyboard JSON from stdin.
Runs the complete pipeline: story → outline → storyboard → images → remotion props → mp4.

Output files are written to --output-dir (default: ~/.shand/projects/<project-id>/).
Use --skip-hitl for a fully automated run without human checkpoints.
Use --dry-run to validate the full pipeline without calling external APIs or generating files.`,
	RunE: runPipeline,
}

func runPipeline(cmd *cobra.Command, args []string) error {
	inputData, err := io.ReadAll(os.Stdin)
	if err != nil {
		return stageError("pipeline", "stdin_read_error", fmt.Sprintf("reading stdin: %v", err))
	}

	// Build LLM client
	provider := "mock"
	if cfg != nil && cfg.LLM.Provider != "" {
		provider = cfg.LLM.Provider
	}
	llmClient, err := llm.NewClient(provider, dryRun, cfg)
	if err != nil {
		return stageError("pipeline", "llm_init_error", err.Error())
	}

	// Build image client (used as BatchGenerateImages adapter)
	imgProvider := "mock"
	if cfg != nil && cfg.Image.Provider != "" {
		imgProvider = cfg.Image.Provider
	}
	imgClient, err := image.NewClient(imgProvider, dryRun, cfg)
	if err != nil {
		return stageError("pipeline", "image_init_error", err.Error())
	}

	shandHome, _ := os.UserHomeDir()
	shandHome = filepath.Join(shandHome, ".shand")

	// Build checkpoint store
	db, err := store.New(cfg.Store.DBPath)
	if err != nil {
		return stageError("pipeline", "db_init_error", err.Error())
	}
	ckptRepo := store.NewGormCheckpointRepository(db)
	ckptGate := pipeline.NewCheckpointGate(ckptRepo)

	// Build audio client (Polly)
	audioClient := audio.NewPollyCLIClient(cfg.LLM.AWSRegion, cfg.LLM.AWSAccessKeyID, cfg.LLM.AWSSecretAccessKey)

	// Wire orchestrator
	orch := pipeline.NewOrchestrator(pipeline.OrchestratorDeps{
		LLM:         llmClient,
		Images:      pipeline.NewImageClientBatcher(imgClient, shandHome),
		Audio:       pipeline.NewAudioClientBatcher(audioClient, shandHome),
		Checkpoints: ckptGate,
		DryRun:      dryRun,
		SkipHITL:    pipelineSkipHITL,
	})

	result, err := orch.Run(context.Background(), inputData)
	if err != nil {
		return stageError("pipeline", "pipeline_error", err.Error())
	}

	// Write remotion props
	props := remotion.PanelsToProps(result.Storyboard.ProjectID, result.Panels, cfg.Image.Width, cfg.Image.Height, 24)
	if err := writeResults(result, props); err != nil {
		return stageError("pipeline", "output_error", err.Error())
	}

	// Emit final summary to stdout (JSON)
	summary := map[string]any{
		"project_id": props.ProjectID,
		"panels":     len(props.Panels),
		"dry_run":    dryRun,
	}
	return json.NewEncoder(os.Stdout).Encode(summary)
}

// writeResults writes pipeline artefacts to the output directory.
func writeResults(result *pipeline.PipelineResult, props domain.RemotionProps) error {
	if pipelineOutputDir == "" {
		home, _ := os.UserHomeDir()
		pipelineOutputDir = filepath.Join(home, ".shand", "projects", props.ProjectID)
	}

	if err := os.MkdirAll(pipelineOutputDir, 0755); err != nil {
		return fmt.Errorf("creating output dir %s: %w", pipelineOutputDir, err)
	}

	propsPath := filepath.Join(pipelineOutputDir, "remotion_props.json")
	f, err := os.Create(propsPath)
	if err != nil {
		return fmt.Errorf("creating props file: %w", err)
	}
	defer f.Close()
	return json.NewEncoder(f).Encode(props)
}

func init() {
	pipelineCmd.Flags().BoolVar(&pipelineSkipHITL, "skip-hitl", false, "skip all human-in-the-loop checkpoints")
	pipelineCmd.Flags().StringVar(&pipelineOutputDir, "output-dir", "", "output directory (default: ~/.shand/projects/<project-id>)")
	rootCmd.AddCommand(pipelineCmd)
}
