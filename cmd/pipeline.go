package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/baochen10luo/stagenthand/config"
	"github.com/baochen10luo/stagenthand/internal/audio"
	"github.com/baochen10luo/stagenthand/internal/character"
	"github.com/baochen10luo/stagenthand/internal/domain"
	"github.com/baochen10luo/stagenthand/internal/image"
	"github.com/baochen10luo/stagenthand/internal/llm"
	"github.com/baochen10luo/stagenthand/internal/pipeline"
	"github.com/baochen10luo/stagenthand/internal/remotion"
	"github.com/baochen10luo/stagenthand/internal/render"
	"github.com/baochen10luo/stagenthand/internal/series"
	"github.com/baochen10luo/stagenthand/internal/store"
	"github.com/baochen10luo/stagenthand/internal/video"
	"github.com/spf13/cobra"
)

var (
	pipelineSkipHITL      bool
	pipelineOutputDir     string
	pipelineLanguage      string
	pipelineMaxRetries    int
	pipelineEpisodes      int
	pipelineBatchConc     int
	pipelineFormat        string // "landscape" or "portrait"
	pipelineMultiSpeaker  bool
	pipelineSeriesMemory  bool
	pipelineSeriesWindow  int
	pipelineVideoBackend  string // "remotion" (default) or "nova_reel" or "grok_browser"
	pipelineImageDir      string // pre-existing image directory; skips image generation when set
	pipelineTargetPanels  int    // when > 0, LLM is instructed to generate exactly this many panels
	pipelineI2V           bool   // image-to-video mode: use --image-dir illustrations as I2V references
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

	shandHome, _ := os.UserHomeDir()
	shandHome = filepath.Join(shandHome, ".shand")

	// Build image batcher — use pre-existing images if --image-dir is set
	videoFormat := render.VideoFormat(pipelineFormat)
	var imgBatcher pipeline.ImageBatcher
	if pipelineImageDir != "" {
		if pipelineI2V {
			imgBatcher = pipeline.NewPrebuiltImageBatcherWithOffset(pipelineImageDir, shandHome, 1)
		} else {
			imgBatcher = pipeline.NewPrebuiltImageBatcher(pipelineImageDir, shandHome)
		}
	} else {
		imgProvider := "mock"
		if cfg != nil && cfg.Image.Provider != "" {
			imgProvider = cfg.Image.Provider
		}
		imgClient, err := image.NewClientWithFormat(imgProvider, dryRun, cfg, videoFormat)
		if err != nil {
			return stageError("pipeline", "image_init_error", err.Error())
		}
		imgBatcher = pipeline.NewImageClientBatcher(imgClient, shandHome)
	}

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

	// Build audio batcher: multi-speaker or legacy
	var audioBatcher pipeline.AudioBatcher
	if pipelineMultiSpeaker {
		reg := character.NewFileRegistry(shandHome)
		multiSpeakerClient := audio.NewPollyMultiSpeakerClient(
			cfg.LLM.AWSRegion, cfg.LLM.AWSAccessKeyID, cfg.LLM.AWSSecretAccessKey,
			pipelineLanguage, reg,
		)
		audioBatcher = pipeline.NewMultiSpeakerAudioBatcher(multiSpeakerClient, shandHome)
	} else {
		audioBatcher = pipeline.NewAudioClientBatcher(audioClient, shandHome)
	}

	// Wire orchestrator
	deps := pipeline.OrchestratorDeps{
		LLM:          llmClient,
		Images:       imgBatcher,
		Audio:        audioBatcher,
		Music:        pipeline.NewMusicClientBatcher(musicClient, shandHome),
		Checkpoints:  ckptGate,
		DryRun:       dryRun,
		SkipHITL:     pipelineSkipHITL,
		Language:     pipelineLanguage,
		TargetPanels: pipelineTargetPanels,
	}
	orch := pipeline.NewOrchestrator(deps)

	// Batch mode
	if pipelineEpisodes > 1 {
		batchCfg := pipeline.BatchConfig{
			Episodes:    pipelineEpisodes,
			Concurrency: pipelineBatchConc,
		}

		// Enable series continuity when --series-memory is set and we have > 1 episode
		if pipelineSeriesMemory {
			memoryPath := filepath.Join(pipelineOutputDir, "series_memory.json")
			if pipelineOutputDir == "" {
				home, _ := os.UserHomeDir()
				memoryPath = filepath.Join(home, ".shand", "series_memory.json")
			}
			batchCfg.SeriesRepo = series.NewFileRepository(memoryPath)
			batchCfg.Summarizer = series.NewLLMSummarizer(llmClient)
			batchCfg.WindowSize = pipelineSeriesWindow
			if !pipelineSkipHITL {
				batchCfg.CheckpointGate = ckptGate
			}
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
	props := remotion.PanelsToPropsWithFormat(result.Storyboard.ProjectID, result.Panels, cfg.Image.Width, cfg.Image.Height, 24, result.Storyboard.BGMURL, result.Storyboard.Directives, videoFormat)
	if err := writeResults(result, props); err != nil {
		return stageError("pipeline", "output_error", err.Error())
	}

	// Render + AI Critic loop (only when --max-retries > 0)
	var criticAttempts int
	var criticApproved bool
	var finalVideoPath string
	var retryStrategy string

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

	// Default render (always runs when max-retries == 0)
	if pipelineMaxRetries == 0 {
		outputPath := filepath.Join(pipelineOutputDir, "output_v1.mp4")
		if renderErr := executor.Render(cmd.Context(), templatePath, composition, propsPath, outputPath); renderErr != nil {
			fmt.Fprintf(os.Stderr, "[Warning] render failed: %v\n", renderErr)
		} else {
			finalVideoPath = outputPath
		}
	}

	if pipelineMaxRetries > 0 {
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

			// REJECT: smart routing based on which dimension failed (only if more attempts remain)
			if attempt < pipelineMaxRetries {
				if props.Directives == nil {
					props.Directives = &domain.Directives{}
				}

				if eval.VisualScore < 8 {
					// 視覺路線：需要重生成圖片
					retryStrategy = "visual_regen"

					// 1. 調整 StylePrompt
					props.Directives.StylePrompt = "highly detailed, cinematic lighting, 8K, " + props.Directives.StylePrompt

					// 2. 刪除現有圖片讓 Smart Resume 強制重生成
					imagesDir := filepath.Join(shandHome, "projects", props.ProjectID, "images")
					os.RemoveAll(imagesDir)

					// 3. Marshal props 作為新的 orchestrator 輸入
					propsJSON, _ := json.Marshal(props)

					// 4. 重跑 orchestrator（重生成圖片，Smart Resume 跳過音頻）
					newResult, orchErr := orch.Run(cmd.Context(), propsJSON)
					if orchErr != nil {
						fmt.Fprintf(os.Stderr, "[Warning] visual retry failed: %v\n", orchErr)
						break
					}
					result = newResult

					// 5. 更新 props（含新的 image_url）
					props = remotion.PanelsToPropsWithFormat(result.Storyboard.ProjectID, result.Panels, cfg.Image.Width, cfg.Image.Height, 24, result.Storyboard.BGMURL, result.Storyboard.Directives, videoFormat)
					if err := writeResults(result, props); err != nil {
						fmt.Fprintf(os.Stderr, "[Warning] failed to write updated props after visual retry: %v\n", err)
						break
					}
				} else {
					// 快速路線：只改 props，不動圖片
					retryStrategy = "props_only"

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
					// AdherenceScore < 8：記錄在 feedback，暫不自動修（無法確定方向）

					// 重新寫入更新後的 props（不重跑 orchestrator）
					if err := writeResults(result, props); err != nil {
						fmt.Fprintf(os.Stderr, "[Warning] failed to write updated props: %v\n", err)
						break
					}
				}
			}
		}
	}

	// Stage 2: Dynamic video backend (nova_reel or grok_browser)
	videoBackend := resolveVideoBackend()
	var reelApprovedFlag bool
	if criticApproved && videoBackend == "nova_reel" && !dryRun {
		reelVideoPath, reelErr := runNovaReelStage(cmd.Context(), result.Panels, props, cfg, pipelineOutputDir)
		if reelErr != nil {
			// fallback: log warning, continue with Stage 1 static version
			fmt.Fprintf(os.Stderr, "[Warning] Nova Reel stage failed, using static video: %v\n", reelErr)
		} else {
			// Critic 2: motion-focused
			reelApproved, critic2Err := runReelCritic(cmd.Context(), reelVideoPath, props, cfg)
			if critic2Err != nil || !reelApproved {
				fmt.Fprintf(os.Stderr, "[Info] Reel Critic 2 did not approve, using static video as fallback\n")
			} else {
				finalVideoPath = reelVideoPath
				reelApprovedFlag = true
			}
		}
	} else if videoBackend == "grok_browser" && !dryRun {
		var i2vImages []string
		if pipelineI2V && pipelineImageDir != "" {
			entries, _ := os.ReadDir(pipelineImageDir)
			for _, e := range entries {
				if e.IsDir() {
					continue
				}
				ext := strings.ToLower(filepath.Ext(e.Name()))
				if ext == ".png" || ext == ".jpg" || ext == ".jpeg" {
					i2vImages = append(i2vImages, filepath.Join(pipelineImageDir, e.Name()))
				}
			}
			sort.Strings(i2vImages)
			if len(i2vImages) > 0 {
				i2vImages = i2vImages[1:] // skip cover (_1.png)
			}
		}
		grokVideoPath, grokErr := runGrokBrowserStage(cmd.Context(), result.Panels, props, pipelineOutputDir, i2vImages)
		if grokErr != nil {
			fmt.Fprintf(os.Stderr, "[Warning] Grok Browser stage failed, using static video: %v\n", grokErr)
		} else {
			finalVideoPath = grokVideoPath
			reelApprovedFlag = true
		}
	}

	// HITL: final checkpoint after render (only when a video was produced)
	if !pipelineSkipHITL && finalVideoPath != "" {
		if err := ckptGate.CreateAndWait(cmd.Context(), "pipeline", domain.StageFinal); err != nil {
			return stageError("pipeline", "hitl_final_rejected", err.Error())
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
		"retry_strategy":  retryStrategy,
		"reel_approved":   reelApprovedFlag,
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
	pipelineCmd.Flags().StringVar(&pipelineFormat, "format", "landscape",
		"Output video format: landscape (1024×576) or portrait (576×1024 for TikTok/Reels/Shorts)")
	pipelineCmd.Flags().BoolVar(&pipelineMultiSpeaker, "multi-speaker", false, "enable per-character voice routing using character registry (requires --language)")
	pipelineCmd.Flags().BoolVar(&pipelineSeriesMemory, "series-memory", false,
		"Enable series continuity across episodes. Requires --episodes > 1.")
	pipelineCmd.Flags().IntVar(&pipelineSeriesWindow, "series-window", 3,
		"Number of recent episodes to inject as context. (default: 3)")
	pipelineCmd.Flags().StringVar(&pipelineVideoBackend, "video-backend", "",
		"video backend: remotion (default), nova_reel, or grok_browser")
	pipelineCmd.Flags().StringVar(&pipelineImageDir, "image-dir", "",
		"use pre-existing images from this directory (sorted by filename); skips image generation API")
	pipelineCmd.Flags().IntVar(&pipelineTargetPanels, "panels", 0,
		"target number of panels (0 = auto); used with --i2v to match illustration count")
	pipelineCmd.Flags().BoolVar(&pipelineI2V, "i2v", false,
		"image-to-video mode: skip cover image, use remaining --image-dir illustrations as I2V references in grok_browser stage")
	rootCmd.AddCommand(pipelineCmd)
}

// resolveVideoBackend returns the effective video backend: CLI flag > config > "remotion".
func resolveVideoBackend() string {
	if pipelineVideoBackend != "" {
		return pipelineVideoBackend
	}
	if cfg != nil && cfg.Video.Provider != "" {
		return cfg.Video.Provider
	}
	return "remotion"
}

// runNovaReelStage generates a 6-second dynamic shot for each panel via Nova Reel I2V,
// then concatenates all shots into a single mp4.
func runNovaReelStage(ctx context.Context, panels []domain.Panel, props domain.RemotionProps, appCfg *config.Config, outputDir string) (string, error) {
	// Resolve AWS credentials: prefer video config, fall back to LLM config
	accessKey := appCfg.LLM.AWSAccessKeyID
	secretKey := appCfg.LLM.AWSSecretAccessKey
	region := appCfg.Video.Region
	if region == "" {
		region = appCfg.LLM.AWSRegion
	}
	s3Bucket := appCfg.Video.S3Bucket
	if s3Bucket == "" {
		return "", fmt.Errorf("video.s3_bucket is required for nova_reel backend")
	}

	reelClient, err := video.NewNovaReelClient(accessKey, secretKey, region, s3Bucket)
	if err != nil {
		return "", fmt.Errorf("create nova reel client: %w", err)
	}

	shotsDir := filepath.Join(outputDir, "shots")
	if err := os.MkdirAll(shotsDir, 0755); err != nil {
		return "", fmt.Errorf("create shots dir: %w", err)
	}

	var shotPaths []string
	for i, panel := range panels {
		if panel.ImageURL == "" {
			fmt.Fprintf(os.Stderr, "[Warning] Panel %d has no image, skipping reel shot\n", i+1)
			continue
		}

		shotPath := filepath.Join(shotsDir, fmt.Sprintf("shot_%03d.mp4", i+1))

		// Smart resume: skip if shot already exists and is non-empty
		if info, statErr := os.Stat(shotPath); statErr == nil && info.Size() > 0 {
			shotPaths = append(shotPaths, shotPath)
			continue
		}

		shotBytes, genErr := reelClient.GenerateShot(ctx, panel.ImageURL, panel.Description)
		if genErr != nil {
			return "", fmt.Errorf("generate reel shot for panel %d: %w", i+1, genErr)
		}

		if writeErr := os.WriteFile(shotPath, shotBytes, 0644); writeErr != nil {
			return "", fmt.Errorf("write shot %d: %w", i+1, writeErr)
		}
		shotPaths = append(shotPaths, shotPath)
	}

	if len(shotPaths) == 0 {
		return "", fmt.Errorf("no shots generated for reel")
	}

	reelOutputPath := filepath.Join(outputDir, "output_reel.mp4")
	if err := video.ConcatenateShots(ctx, shotPaths, reelOutputPath); err != nil {
		return "", fmt.Errorf("concatenate reel shots: %w", err)
	}

	return reelOutputPath, nil
}

// runGrokBrowserStage generates a video shot for each panel via opencli grok video (browser automation),
// then concatenates all shots into a single mp4.
func runGrokBrowserStage(ctx context.Context, panels []domain.Panel, props domain.RemotionProps, outputDir string, imagePaths []string) (string, error) {
	if outputDir == "" {
		home, _ := os.UserHomeDir()
		outputDir = filepath.Join(home, ".shand", "projects", props.ProjectID)
	}

	shotsDir := filepath.Join(outputDir, "shots")
	if err := os.MkdirAll(shotsDir, 0755); err != nil {
		return "", fmt.Errorf("create shots dir: %w", err)
	}

	var shotPaths []string
	for i, panel := range panels {
		shotPath := filepath.Join(shotsDir, fmt.Sprintf("shot_%03d.mp4", i+1))

		// Smart resume: skip if shot already exists and is non-empty
		if info, statErr := os.Stat(shotPath); statErr == nil && info.Size() > 0 {
			fmt.Fprintf(os.Stderr, "[Info] Grok shot %d already exists, skipping\n", i+1)
			shotPaths = append(shotPaths, shotPath)
			continue
		}

		prompt := panel.Description

		fmt.Fprintf(os.Stderr, "[Info] Generating Grok video for panel %d/%d: %s\n", i+1, len(panels), prompt)

		// Call opencli grok video; output goes to ~/Downloads/grok-videos/ by default
		// We use --output to write directly into our shots dir
		opencliArgs := []string{
			"grok", "video", prompt,
			"--output", shotsDir,
			"--timeout", "360",
		}
		if len(imagePaths) > i {
			opencliArgs = append(opencliArgs, "--image", imagePaths[i])
		}
		// Run opencli with up to 5 attempts per panel; fail hard if all attempts exhausted
		const maxAttempts = 5
		var runOut []byte
		var panelErr error
		for attempt := 0; attempt < maxAttempts; attempt++ {
			if attempt > 0 {
				fmt.Fprintf(os.Stderr, "[Info] Retrying Grok video panel %d (attempt %d/%d)\n", i+1, attempt+1, maxAttempts)
				select {
				case <-ctx.Done():
					return "", ctx.Err()
				case <-time.After(8 * time.Second):
				}
			}
			execCmd := exec.CommandContext(ctx, "opencli", opencliArgs...)
			execCmd.Env = append(os.Environ(), "OPENCLI_CDP_TARGET=grok.com", "OPENCLI_BROWSER_COMMAND_TIMEOUT=420")
			runOut, panelErr = execCmd.CombinedOutput()
			outStr := string(runOut)
			if panelErr != nil {
				fmt.Fprintf(os.Stderr, "[Warning] opencli panel %d exit error (attempt %d): %v\n", i+1, attempt+1, panelErr)
				panelErr = fmt.Errorf("exit error: %w", panelErr)
				continue
			}
			if strings.Contains(outStr, "[ERROR]") || strings.Contains(outStr, "[TIMEOUT]") {
				fmt.Fprintf(os.Stderr, "[Warning] opencli panel %d adapter error (attempt %d): %s\n", i+1, attempt+1, strings.TrimSpace(outStr))
				panelErr = fmt.Errorf("adapter error: %s", strings.TrimSpace(outStr))
				continue
			}
			panelErr = nil
			break
		}
		if panelErr != nil {
			return "", fmt.Errorf("grok video panel %d failed after %d attempts: %w", i+1, maxAttempts, panelErr)
		}

		// opencli writes to --output dir; find the latest mp4 in shotsDir that isn't already tracked
		entries, readErr := os.ReadDir(shotsDir)
		if readErr != nil {
			return "", fmt.Errorf("read shots dir: %w", readErr)
		}
		var latestMP4 string
		for _, e := range entries {
			if !e.IsDir() && filepath.Ext(e.Name()) == ".mp4" {
				candidate := filepath.Join(shotsDir, e.Name())
				tracked := false
				for _, sp := range shotPaths {
					if sp == candidate {
						tracked = true
						break
					}
				}
				if !tracked && candidate != shotPath {
					latestMP4 = candidate
				}
			}
		}

		// Rename to canonical shot name for deterministic ordering
		if latestMP4 != "" {
			if renameErr := os.Rename(latestMP4, shotPath); renameErr != nil {
				return "", fmt.Errorf("rename shot %d: %w", i+1, renameErr)
			}
		}

		if info, statErr := os.Stat(shotPath); statErr != nil || info.Size() == 0 {
			return "", fmt.Errorf("grok video panel %d: output file missing or empty after %d attempts", i+1, maxAttempts)
		}

		shotPaths = append(shotPaths, shotPath)
	}

	if len(shotPaths) == 0 {
		return "", fmt.Errorf("no shots generated for grok_browser")
	}

	outputPath := filepath.Join(outputDir, "output_grok.mp4")
	if err := video.ConcatenateShots(ctx, shotPaths, outputPath); err != nil {
		return "", fmt.Errorf("concatenate grok shots: %w", err)
	}

	return outputPath, nil
}

// runReelCritic evaluates the reel video with a motion-focused Critic 2 prompt.
func runReelCritic(ctx context.Context, videoPath string, props domain.RemotionProps, appCfg *config.Config) (bool, error) {
	if appCfg.LLM.AWSAccessKeyID == "" || appCfg.LLM.AWSSecretAccessKey == "" {
		// No credentials for critic — assume approved to avoid blocking
		return true, nil
	}

	bedrockClient, err := llm.NewBedrockClient(
		appCfg.LLM.AWSAccessKeyID,
		appCfg.LLM.AWSSecretAccessKey,
		appCfg.LLM.AWSRegion,
		appCfg.LLM.Model,
	)
	if err != nil {
		return false, fmt.Errorf("create bedrock client for reel critic: %w", err)
	}

	critic := video.NewMotionCritic(bedrockClient)
	propsJSON, _ := json.Marshal(props)
	eval, err := critic.Evaluate(ctx, videoPath, propsJSON)
	if err != nil {
		return false, fmt.Errorf("reel critic evaluation: %w", err)
	}

	return eval.CheckApproval(), nil
}
