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
	"github.com/baochen10luo/stagenthand/internal/video"
	"github.com/spf13/cobra"
)

var (
	pipelineSkipHITL   bool
	pipelineOutputDir  string
	pipelineLanguage   string
	pipelineMaxRetries int
	pipelineEpisodes   int
	pipelineBatchConc  int
)

var pipelineCmd = &cobra.Command{
	Use:   "pipeline",
	Short: "Run the full AI short drama pipeline end-to-end",
	Long: `Reads a story prompt or storyboard JSON from stdin.
Runs the complete pipeline: story → outline → storyboard → images → remotion props → mp4.

Output files are written to --output-dir (default: ~/.shand/projects/<project-id>/).
Use --skip-hitl for a fully automated run without human checkpoints.
Use --dry-run to validate the full pipeline without calling external APIs or generating files.
Use --language to set the TTS/dialogue language (default: zh-TW).
Use --episodes N to produce multiple episodes in batch mode.`,
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

	// Build audio client (Polly) with language support
	audioClient := audio.NewPollyCLIClientWithLanguage(
		cfg.LLM.AWSRegion, cfg.LLM.AWSAccessKeyID, cfg.LLM.AWSSecretAccessKey,
		pipelineLanguage,
	)

	// Build music client (Jamendo)
	musicClient := audio.NewJamendoClient(cfg.Audio.JamendoClientID)

	// Build critic evaluator if max retries > 0 and AWS credentials are available
	var criticEvaluator pipeline.VideoCriticEvaluator
	if pipelineMaxRetries > 0 && cfg != nil &&
		cfg.LLM.AWSAccessKeyID != "" && cfg.LLM.AWSSecretAccessKey != "" {
		bedrockClient, bedrockErr := llm.NewBedrockClient(
			cfg.LLM.AWSAccessKeyID,
			cfg.LLM.AWSSecretAccessKey,
			cfg.LLM.AWSRegion,
			cfg.LLM.Model,
		)
		if bedrockErr == nil && bedrockClient != nil {
			criticEvaluator = newVideoCriticAdapter(video.NewCritic(bedrockClient))
		}
	}

	// Wire orchestrator
	deps := pipeline.OrchestratorDeps{
		LLM:         llmClient,
		Images:      pipeline.NewImageClientBatcher(imgClient, shandHome),
		Audio:       pipeline.NewAudioClientBatcher(audioClient, shandHome),
		Music:       pipeline.NewMusicClientBatcher(musicClient, shandHome),
		Checkpoints: ckptGate,
		DryRun:      dryRun,
		SkipHITL:    pipelineSkipHITL,
		Language:    pipelineLanguage,
	}
	orch := pipeline.NewOrchestrator(deps)

	// Batch mode
	if pipelineEpisodes > 1 {
		batchCfg := pipeline.BatchConfig{
			Episodes:    pipelineEpisodes,
			Concurrency: pipelineBatchConc,
		}
		batchResult, err := pipeline.RunBatch(context.Background(), orch, inputData, batchCfg)
		if err != nil {
			return stageError("pipeline", "batch_error", err.Error())
		}
		return json.NewEncoder(os.Stdout).Encode(batchResult)
	}

	result, err := orch.Run(context.Background(), inputData)
	if err != nil {
		return stageError("pipeline", "pipeline_error", err.Error())
	}

	// Write remotion props
	props := remotion.PanelsToProps(result.Storyboard.ProjectID, result.Panels, cfg.Image.Width, cfg.Image.Height, 24, result.Storyboard.BGMURL, result.Storyboard.Directives)
	if err := writeResults(result, props); err != nil {
		return stageError("pipeline", "output_error", err.Error())
	}

	// Render + AI Critic loop (only when --max-retries > 0)
	var criticAttempts int
	var criticApproved bool
	var finalVideoPath string

	if pipelineMaxRetries > 0 {
		executor := remotion.NewCLIExecutor(dryRun)

		rawTemplatePath := ""
		if cfg != nil && cfg.Remotion.TemplatePath != "" {
			rawTemplatePath = cfg.Remotion.TemplatePath
		} else {
			rawTemplatePath = "./remotion-template"
		}
		templatePath, _ := filepath.Abs(rawTemplatePath)

		composition := "ShortDrama"
		if cfg != nil && cfg.Remotion.Composition != "" {
			composition = cfg.Remotion.Composition
		}

		propsPath := filepath.Join(pipelineOutputDir, "remotion_props.json")

		for attempt := 0; attempt <= pipelineMaxRetries; attempt++ {
			outputPath := filepath.Join(pipelineOutputDir, fmt.Sprintf("output_v%d.mp4", attempt+1))

			// Render mp4
			renderErr := executor.Render(cmd.Context(), templatePath, composition, propsPath, outputPath)
			if renderErr != nil {
				fmt.Fprintf(os.Stderr, "[Warning] render attempt %d failed: %v\n", attempt+1, renderErr)
				break
			}
			finalVideoPath = outputPath

			// Evaluate with critic (skip if no critic configured)
			if criticEvaluator == nil {
				break
			}

			propsJSON, _ := json.Marshal(props)
			eval, evalErr := criticEvaluator.Evaluate(cmd.Context(), outputPath, propsJSON)
			criticAttempts++
			if evalErr != nil {
				fmt.Fprintf(os.Stderr, "[Warning] critic evaluation failed: %v\n", evalErr)
				break
			}

			if eval.IsApproved() {
				criticApproved = true
				break
			}

			// REJECT: adjust props for next attempt (only if there are more attempts)
			if attempt < pipelineMaxRetries {
				if props.Directives == nil {
					props.Directives = &domain.Directives{}
				}
				if eval.VisualScore < 8 {
					props.Directives.StylePrompt = "highly detailed, 8K, " + props.Directives.StylePrompt
				}
				if eval.AudioSyncScore < 8 {
					depth := props.Directives.DuckingDepth - 0.1
					if depth < 0.1 {
						depth = 0.1
					}
					props.Directives.DuckingDepth = depth
				}
				if eval.ToneScore < 6 {
					for i := range props.Panels {
						props.Panels[i].DurationSec *= 1.2
					}
				}

				// Write updated props for next render
				if err := writeResults(result, props); err != nil {
					fmt.Fprintf(os.Stderr, "[Warning] failed to write updated props: %v\n", err)
					break
				}
			}
		}
	}

	// Emit final summary to stdout (JSON)
	summary := map[string]any{
		"project_id":      props.ProjectID,
		"panels":          len(props.Panels),
		"dry_run":         dryRun,
		"critic_attempts": criticAttempts,
		"critic_approved": criticApproved,
		"output_video":    finalVideoPath,
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
	pipelineCmd.Flags().StringVar(&pipelineLanguage, "language", "zh-TW", "TTS/dialogue language (zh-TW, en-US, en-GB, ja-JP, ko-KR, cmn-CN)")
	pipelineCmd.Flags().IntVar(&pipelineMaxRetries, "max-retries", 0, "maximum AI Critic retry attempts; also triggers automatic remotion render after props generation")
	pipelineCmd.Flags().IntVar(&pipelineEpisodes, "episodes", 1, "number of episodes to produce in batch mode")
	pipelineCmd.Flags().IntVar(&pipelineBatchConc, "batch-concurrency", 2, "max concurrent workers in batch mode")
	rootCmd.AddCommand(pipelineCmd)
}
