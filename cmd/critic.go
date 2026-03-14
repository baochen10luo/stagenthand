package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"os"

	"github.com/baochen10luo/stagenthand/internal/llm"
	"github.com/baochen10luo/stagenthand/internal/video"
	"github.com/spf13/cobra"
)

var criticVideoPath string
var criticPropsPath string

var criticCmd = &cobra.Command{
	Use:   "critic",
	Short: "Run the AI Critic on a rendered MP4 video and its JSON configuration",
	RunE: func(cmd *cobra.Command, args []string) error {
		if criticVideoPath == "" || criticPropsPath == "" {
			return fmt.Errorf("--video and --props are required")
		}

		ctx := context.Background()
		if cfg == nil {
			return fmt.Errorf("global config not initialized")
		}

		bedrockClient, err := llm.NewBedrockClient(cfg.LLM.AWSAccessKeyID, cfg.LLM.AWSSecretAccessKey, cfg.LLM.AWSRegion, "amazon.nova-pro-v1:0")
		if err != nil {
			return fmt.Errorf("failed to create bedrock client: %w", err)
		}

		propsBytes, err := os.ReadFile(criticPropsPath)
		if err != nil {
			return fmt.Errorf("failed to read props file: %w", err)
		}

		fmt.Printf("🔍 AI Critic is evaluating video: %s\n", criticVideoPath)
		fmt.Printf("   Against input specification: %s\n", criticPropsPath)
		fmt.Println("   Sending video and text to Amazon Nova Pro. This may take a minute...")

		criticAgent := video.NewCritic(bedrockClient)
		eval, err := criticAgent.Evaluate(ctx, criticVideoPath, propsBytes)
		if err != nil {
			return fmt.Errorf("evaluation failed: %w", err)
		}

		fmt.Println("\n📊 Evaluation Results:")
		fmt.Printf("   1. Visual Coherence (A): %d/10\n", eval.VisualScore)
		fmt.Printf("   2. Audio-Visual Sync (B): %d/10\n", eval.AudioSyncScore)
		fmt.Printf("   3. Directive Adherence (C): %d/10\n", eval.AdherenceScore)
		fmt.Printf("   4. Narrative Tone (D): %d/10\n", eval.ToneScore)
		total := eval.VisualScore + eval.AudioSyncScore + eval.AdherenceScore + eval.ToneScore
		fmt.Printf("   Total Score: %d/40\n", total)

		fmt.Printf("\n📝 Feedback:\n%s\n", eval.Feedback)

		approved := eval.CheckApproval()
		eval.Action = "APPROVE"
		if !approved {
			eval.Action = "REJECT"
		}
		fmt.Printf("\n🏁 Decision: %s\n", eval.Action)

		if !approved {
			fmt.Println("   [!] Video DID NOT meet convergence threshold. Action: RETRY PIPELINE.")
		} else {
			fmt.Println("   [OK] Video meets convergence threshold. Action: PUBLISH.")
		}

		// Output result JSON for debugging or CI/CD
		b, _ := json.MarshalIndent(eval, "", "  ")
		fmt.Printf("\nRaw JSON:\n%s\n", string(b))

		return nil
	},
}

func init() {
	criticCmd.Flags().StringVarP(&criticVideoPath, "video", "v", "", "Path to the rendered MP4 file")
	criticCmd.Flags().StringVarP(&criticPropsPath, "props", "p", "", "Path to the remotion_props.json used to generate the video")
	rootCmd.AddCommand(criticCmd)
}
