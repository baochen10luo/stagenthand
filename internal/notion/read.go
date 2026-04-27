package notion

import (
	"context"
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"strings"

	"github.com/baochen10luo/stagenthand/internal/domain"
)

// ReadPanels reads all panel rows from the Notion DB titled storyTitle on pageID.
// Rows are returned as domain.Panel values in 幕號 order. ImageURL is set to the
// 插圖 filename so callers can match panels to local image files.
func ReadPanels(ctx context.Context, pageID, storyTitle, token string) ([]domain.Panel, error) {
	dbID, err := findDatabase(ctx, pageID, storyTitle, token)
	if err != nil {
		return nil, err
	}

	rows, err := queryDatabase(ctx, dbID, token, nil)
	if err != nil {
		return nil, fmt.Errorf("query Notion database: %w", err)
	}

	type entry struct {
		idx int
		p   domain.Panel
	}
	var entries []entry

	for _, row := range rows {
		title := propertyText(row.Properties["幕號"])
		if title == "封面" || title == "" {
			continue
		}
		idx := parsePanelIndex(title)
		p := domain.Panel{
			SceneNumber: 1,
			PanelNumber: idx,
			DurationSec: 4.0,
		}
		if v, ok := row.Properties["字幕文字"]; ok {
			p.Dialogue = propertyText(v)
		}
		if v, ok := row.Properties["Grok 提示詞"]; ok {
			p.Description = propertyText(v)
		}
		if v, ok := row.Properties["插圖"]; ok {
			p.ImageURL = propertyText(v) // filename only; caller resolves to full path
		}
		entries = append(entries, entry{idx: idx, p: p})
	}

	sort.Slice(entries, func(i, j int) bool { return entries[i].idx < entries[j].idx })

	panels := make([]domain.Panel, len(entries))
	for i, e := range entries {
		panels[i] = e.p
	}
	return panels, nil
}

// findDatabase returns the ID of the Notion child_database on pageID whose title
// matches storyTitle. Returns an error if no matching database is found.
func findDatabase(ctx context.Context, pageID, storyTitle, token string) (string, error) {
	blocks, err := listBlockChildren(ctx, pageID, token)
	if err != nil {
		return "", fmt.Errorf("list Notion page children: %w", err)
	}
	for _, block := range blocks {
		if block.Type != "child_database" {
			continue
		}
		dbURL := "https://api.notion.com/v1/databases/" + block.ID
		var dbInfo struct {
			Title []textItem `json:"title"`
		}
		if err := doJSON(ctx, http.MethodGet, dbURL, token, "", &dbInfo); err != nil {
			continue
		}
		if len(dbInfo.Title) > 0 && dbInfo.Title[0].PlainText == storyTitle {
			return block.ID, nil
		}
	}
	return "", fmt.Errorf("Notion database %q not found on page %s", storyTitle, pageID)
}

// parsePanelIndex extracts the numeric index from a row title like "幕 01" → 1.
func parsePanelIndex(title string) int {
	parts := strings.Fields(title)
	if len(parts) < 2 {
		return 0
	}
	n, _ := strconv.Atoi(parts[len(parts)-1])
	return n
}
