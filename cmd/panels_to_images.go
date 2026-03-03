package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"

	"github.com/baochen10luo/stagenthand/internal/domain"
	"github.com/baochen10luo/stagenthand/internal/image"
	"github.com/spf13/cobra"
)

var (
	panelsOutputDir string
	workers         int
)

type GeneratorRequest struct {
	Index int
	Panel domain.Panel
}

func runPanelsToImages(cmd *cobra.Command, args []string) error {
	inputData, err := io.ReadAll(os.Stdin)
	if err != nil {
		return fmt.Errorf("reading stdin: %w", err)
	}

	var payload struct {
		ProjectID string         `json:"project_id"`
		Episode   int            `json:"episode"`
		Panels    []domain.Panel `json:"panels"`
	}

	if err := json.Unmarshal(inputData, &payload); err != nil {
		return fmt.Errorf("parsing panels payload: %w", err)
	}

	if err := os.MkdirAll(panelsOutputDir, 0755); err != nil {
		return fmt.Errorf("creating panels output dir: %w", err)
	}

	provider := "mock"
	if cfg != nil && cfg.Image.Provider != "" {
		provider = cfg.Image.Provider
	}

	ctx := context.Background()

	var wg sync.WaitGroup
	reqChan := make(chan GeneratorRequest, len(payload.Panels))
	errChan := make(chan error, len(payload.Panels))

	// Worker Pool
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			client, err := image.NewClient(provider, dryRun, cfg)
			if err != nil {
				errChan <- fmt.Errorf("client factory error: %w", err)
				return
			}

			for req := range reqChan {
				imgBytes, err := client.GenerateImage(ctx, req.Panel.Description, req.Panel.CharacterRefs)
				if err != nil {
					errChan <- fmt.Errorf("failed panel scene_%d_panel_%d: %w", req.Panel.SceneNumber, req.Panel.PanelNumber, err)
					req.Panel.ImageURL = "error.png"
				} else if len(imgBytes) > 0 {
					fileName := fmt.Sprintf("scene_%d_panel_%d.png", req.Panel.SceneNumber, req.Panel.PanelNumber)
					filePath := filepath.Join(panelsOutputDir, fileName)
					if err := os.WriteFile(filePath, imgBytes, 0644); err != nil {
						errChan <- fmt.Errorf("write error for panel %d: %w", req.Panel.PanelNumber, err)
						req.Panel.ImageURL = "error.png"
					} else {
						req.Panel.ImageURL = filePath
					}
				} else {
					req.Panel.ImageURL = "error.png"
				}

				payload.Panels[req.Index] = req.Panel
			}
		}()
	}

	for i, p := range payload.Panels {
		reqChan <- GeneratorRequest{Index: i, Panel: p}
	}
	close(reqChan)

	wg.Wait()
	close(errChan)

	var errs []error
	for e := range errChan {
		errs = append(errs, e)
		if verbose {
			fmt.Fprintf(os.Stderr, "Worker error: %v\n", e)
		}
	}

	outBytes, _ := json.Marshal(payload)
	fmt.Fprintln(os.Stdout, string(outBytes))

	if len(errs) > 0 {
		return fmt.Errorf("completed with %d errors", len(errs))
	}
	return nil
}

var panelsToImagesCmd = &cobra.Command{
	Use:   "panels-to-images",
	Short: "Generate all images for an array of panels concurrently",
	RunE:  runPanelsToImages,
}

func init() {
	panelsToImagesCmd.Flags().StringVar(&panelsOutputDir, "out-dir", ".", "Directory to save generated images")
	panelsToImagesCmd.Flags().IntVar(&workers, "workers", 3, "Number of concurrent workers")
	rootCmd.AddCommand(panelsToImagesCmd)
}
