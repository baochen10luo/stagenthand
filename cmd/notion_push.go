package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/baochen10luo/stagenthand/internal/domain"
	"github.com/baochen10luo/stagenthand/internal/notion"
	"github.com/spf13/cobra"
)

var (
	notionPushOutputDir  string
	notionPushPageID     string
)

var notionPushCmd = &cobra.Command{
	Use:   "notion-push",
	Short: "Push storyboard manifest to a Notion database for review",
	Long: `Reads storyboard_manifest.json from --output-dir and uploads each panel
(image + dialogue + description) to a Notion database on the given page.
Does not wait for human approval — use rough-cut after reviewing in Notion.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		if notionPushOutputDir == "" {
			return stageError("notion-push", "missing_flag", "--output-dir is required")
		}

		pageID := notionPushPageID
		if pageID == "" {
			pageID = os.Getenv("NOTION_GROK_PAGE_ID")
		}
		if pageID == "" {
			pageID = "3485ee2ef54d8034bb8ceabf27c3f29c"
		}

		token := os.Getenv("NOTION_API_KEY")
		if token == "" {
			return stageError("notion-push", "missing_env", "NOTION_API_KEY is not set")
		}

		manifestPath := filepath.Join(notionPushOutputDir, "storyboard_manifest.json")
		data, err := os.ReadFile(manifestPath)
		if err != nil {
			return stageError("notion-push", "read_error",
				fmt.Sprintf("could not read %s: %v", manifestPath, err))
		}

		var manifest domain.StoryboardManifest
		if err := json.Unmarshal(data, &manifest); err != nil {
			return stageError("notion-push", "parse_error",
				fmt.Sprintf("could not parse storyboard_manifest.json: %v", err))
		}

		// Collect image paths from panel ImageURLs (skip empty ones).
		imagePaths := make([]string, len(manifest.Panels))
		for i, p := range manifest.Panels {
			imagePaths[i] = p.ImageURL
		}

		// Cover image: first non-empty path in the list.
		coverImage := ""
		for _, p := range imagePaths {
			if p != "" {
				coverImage = p
				break
			}
		}

		// HITL with skipWait=true: upload rows, print Notion URL, return immediately.
		_, err = notion.HITL(cmd.Context(), manifest.Panels, imagePaths, coverImage,
			manifest.StoryTitle, pageID, token, true)
		if err != nil {
			return stageError("notion-push", "notion_error", err.Error())
		}

		dbURL := "https://www.notion.so/" + strings.ReplaceAll(pageID, "-", "")
		fmt.Fprintf(os.Stderr, "[Info] Notion 分鏡表已更新：%s\n", dbURL)
		fmt.Fprintf(os.Stderr, "[Info] 在 Notion 編輯完成後，執行 rough-cut 產出粗剪影片\n")

		result := map[string]any{
			"project_id":   manifest.ProjectID,
			"story_title":  manifest.StoryTitle,
			"panel_count":  len(manifest.Panels),
			"notion_page":  dbURL,
		}
		return json.NewEncoder(os.Stdout).Encode(result)
	},
}

func init() {
	notionPushCmd.Flags().StringVar(&notionPushOutputDir, "output-dir", "",
		"project output directory containing storyboard_manifest.json")
	notionPushCmd.Flags().StringVar(&notionPushPageID, "notion-page-id", "",
		"Notion page ID (overrides NOTION_GROK_PAGE_ID env var)")
	rootCmd.AddCommand(notionPushCmd)
}
