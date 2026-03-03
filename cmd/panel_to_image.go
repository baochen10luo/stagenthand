package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/baochen10luo/stagenthand/internal/domain"
	"github.com/baochen10luo/stagenthand/internal/image"
	"github.com/spf13/cobra"
)

var (
	outputDir string
)

func runPanelToImage(cmd *cobra.Command, args []string) error {
	inputData, err := io.ReadAll(os.Stdin)
	if err != nil {
		return fmt.Errorf("reading stdin: %w", err)
	}

	var panel domain.Panel
	if err := json.Unmarshal(inputData, &panel); err != nil {
		return fmt.Errorf("parsing panel json: %w", err)
	}

	provider := "mock"
	if cfg != nil && cfg.Image.Provider != "" {
		provider = cfg.Image.Provider
	}

	// This assumes image client factory will exist similar to llm.NewClient
	client, err := image.NewClient(provider, dryRun, cfg)
	if err != nil {
		return fmt.Errorf("image client factory: %w", err)
	}

	imgBytes, err := client.GenerateImage(context.Background(), panel.Description, panel.CharacterRefs)
	if err != nil {
		return fmt.Errorf("image generation failed: %w", err)
	}

	// Actual writing to disk
	if err := os.MkdirAll(outputDir, 0755); err != nil {
		return fmt.Errorf("failed to create output dir: %w", err)
	}

	if len(imgBytes) > 0 {
		fileName := fmt.Sprintf("scene_%d_panel_%d.png", panel.SceneNumber, panel.PanelNumber)
		filePath := filepath.Join(outputDir, fileName)
		if err := os.WriteFile(filePath, imgBytes, 0644); err != nil {
			return fmt.Errorf("writing image to disk: %w", err)
		}
		panel.ImageURL = filePath
	} else {
		panel.ImageURL = "error.png"
	}

	outBytes, _ := json.Marshal(panel)
	fmt.Fprintln(os.Stdout, string(outBytes))
	return nil
}

var panelToImageCmd = &cobra.Command{
	Use:   "panel-to-image",
	Short: "Generate one image for a selected panel in dry-run mode",
	RunE:  runPanelToImage,
}

func init() {
	panelToImageCmd.Flags().StringVar(&outputDir, "out-dir", ".", "Directory to output simulated images")
	rootCmd.AddCommand(panelToImageCmd)
}
