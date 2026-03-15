package cmd

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/baochen10luo/stagenthand/internal/domain"
	"github.com/baochen10luo/stagenthand/internal/remotion"
	"github.com/spf13/cobra"
)

var remtonProjectID string

var storyboardToRemotionCmd = &cobra.Command{
	Use:   "storyboard-to-remotion-props",
	Short: "Convert a storyboard with image URLs into Remotion props JSON",
	Long: `Reads a Storyboard JSON (or flat Panel array) from stdin.
Outputs a RemotionProps JSON to stdout, ready to pipe into remotion-render.

Accepted input formats:
  - domain.Storyboard         ({"project_id": ..., "scenes": [...]})
  - wrapped panels object     ({"project_id": ..., "panels": [...]})
  - []domain.Panel            ([{"scene_number":1, "panel_number":1, ...}])`,
	RunE: func(cmd *cobra.Command, args []string) error {
		raw, err := os.ReadFile("/dev/stdin")
		if err != nil {
			return fmt.Errorf("failed to read stdin: %w", err)
		}

		props, err := parseAndConvert(raw, cfg.Image.Width, cfg.Image.Height, remtonProjectID)
		if err != nil {
			return err
		}

		return json.NewEncoder(os.Stdout).Encode(props)
	},
}

// wrappedPanels matches {"project_id":"...","panels":[...],"directives":{...}}
type wrappedPanels struct {
	ProjectID  string            `json:"project_id"`
	Panels     []domain.Panel    `json:"panels"`
	Directives *domain.Directives `json:"directives,omitempty"`
}

// parseAndConvert is extracted for testability (no os.Stdin dependency).
func parseAndConvert(raw []byte, width, height int, projectIDFlag string) (domain.RemotionProps, error) {
	// Try Storyboard first
	var sb domain.Storyboard
	if err := json.Unmarshal(raw, &sb); err == nil && len(sb.Scenes) > 0 {
		return remotion.StoryboardToProps(sb, width, height, 24), nil
	}

	// Try wrapped panels object {"project_id":"...","panels":[...]}
	var wp wrappedPanels
	if err := json.Unmarshal(raw, &wp); err == nil && len(wp.Panels) > 0 {
		projectID := wp.ProjectID
		if projectIDFlag != "" {
			projectID = projectIDFlag
		}
		if projectID == "" {
			projectID = "default"
		}
		return remotion.PanelsToProps(projectID, wp.Panels, width, height, 24, "", wp.Directives), nil
	}

	// Try flat Panel array
	var panels []domain.Panel
	if err := json.Unmarshal(raw, &panels); err == nil && len(panels) > 0 {
		projectID := projectIDFlag
		if projectID == "" {
			projectID = "default"
		}
		return remotion.PanelsToProps(projectID, panels, width, height, 24, "", sb.Directives), nil
	}

	return domain.RemotionProps{}, fmt.Errorf("unrecognized input: expected Storyboard or []Panel JSON")
}

func init() {
	rootCmd.AddCommand(storyboardToRemotionCmd)
	storyboardToRemotionCmd.Flags().StringVar(&remtonProjectID, "project-id", "", "Override project ID for path normalization")
}
