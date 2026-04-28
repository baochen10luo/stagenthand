package notion

import (
	"context"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/baochen10luo/stagenthand/internal/domain"
)

// ReadPanels reads all panel rows from the database inside the story sub-page
// titled storyTitle on pageID (the parent page, e.g. Phase-02).
// Rows are returned as domain.Panel values in 幕號 order.
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

// findDatabase finds the child_database inside the story sub-page titled
// storyTitle on pageID. It first locates the child_page, then returns the
// first child_database found inside it.
func findDatabase(ctx context.Context, pageID, storyTitle, token string) (string, error) {
	blocks, err := listBlockChildren(ctx, pageID, token)
	if err != nil {
		return "", fmt.Errorf("list Notion page children: %w", err)
	}

	// Find the story child_page by title.
	storyPageID := ""
	for _, block := range blocks {
		if block.Type == "child_page" && block.ChildPage != nil && block.ChildPage.Title == storyTitle {
			storyPageID = block.ID
			break
		}
	}
	if storyPageID == "" {
		return "", fmt.Errorf("story page %q not found on page %s", storyTitle, pageID)
	}

	// Find first child_database inside the story page.
	storyBlocks, err := listBlockChildren(ctx, storyPageID, token)
	if err != nil {
		return "", fmt.Errorf("list story page children: %w", err)
	}
	for _, block := range storyBlocks {
		if block.Type == "child_database" {
			return block.ID, nil
		}
	}
	return "", fmt.Errorf("no database found in story page %q", storyTitle)
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
