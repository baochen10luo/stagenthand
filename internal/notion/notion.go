// Package notion provides a Notion Database HITL checkpoint for the shand pipeline.
// The only exported entry point is HITL.
package notion

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"github.com/baochen10luo/stagenthand/internal/domain"
)

// HITL writes the panel storyboard to a Notion Database, optionally waits for
// user confirmation via stdin, then reads back any edits and returns the updated panels.
func HITL(
	ctx context.Context,
	panels []domain.Panel,
	imagePaths []string,
	coverImage string,
	storyTitle string,
	pageID string,
	token string,
	skipWait bool,
) ([]domain.Panel, error) {
	if token == "" {
		fmt.Fprintln(os.Stderr, "[Warning] NOTION_API_KEY is empty; skipping Notion HITL checkpoint")
		return panels, nil
	}
	if pageID == "" {
		return panels, fmt.Errorf("NOTION_GROK_PAGE_ID is empty")
	}

	dbTitle := storyTitle

	dbID, err := findOrCreateDatabase(ctx, pageID, token, dbTitle)
	if err != nil {
		return panels, err
	}
	pageIDMap, err := upsertPanelRows(ctx, dbID, panels, imagePaths, coverImage, token)
	if err != nil {
		return panels, err
	}

	if skipWait {
		fmt.Fprintln(os.Stderr, "[Info] 分鏡稿已推送，可前往 Notion 審核編輯")
	} else {
		fmt.Fprintln(os.Stderr, "在 Notion 確認/編輯各幕內容後，按 Enter 繼續...")
		_, _ = bufio.NewReader(os.Stdin).ReadString('\n')
	}

	return readPanelRows(ctx, dbID, panels, pageIDMap, token)
}

// ── internal types ────────────────────────────────────────────────────────────

type textItem struct {
	PlainText string `json:"plain_text"`
	Text      struct {
		Content string `json:"content"`
	} `json:"text"`
}

type propertyValue struct {
	Type     string     `json:"type"`
	Title    []textItem `json:"title,omitempty"`
	RichText []textItem `json:"rich_text,omitempty"`
	Checkbox bool       `json:"checkbox,omitempty"`
}

type blockResult struct {
	ID   string `json:"id"`
	Type string `json:"type"`
}

type pageResult struct {
	ID         string                   `json:"id"`
	Type       string                   `json:"type"`
	Properties map[string]propertyValue `json:"properties"`
}

type blockChildrenResponse struct {
	Results    []blockResult `json:"results"`
	HasMore    bool          `json:"has_more"`
	NextCursor string        `json:"next_cursor"`
}

type queryResponse struct {
	Results    []pageResult `json:"results"`
	HasMore    bool         `json:"has_more"`
	NextCursor string       `json:"next_cursor"`
}

// requiredProperties defines the expected schema for the HITL database.
var requiredProperties = map[string]map[string]any{
	"幕號":       {"title": map[string]any{}},
	"插圖":       {"rich_text": map[string]any{}},
	"Grok 提示詞": {"rich_text": map[string]any{}},
	"字幕文字":     {"rich_text": map[string]any{}},
	"類型":       {"select": map[string]any{}},
	"說話者":      {"rich_text": map[string]any{}},
	"審核通過":     {"checkbox": map[string]any{}},
	"備註":       {"rich_text": map[string]any{}},
}

// normalizeSpeaker converts known narrator/VO labels to "" (empty = narrator/旁白).
// LLMs sometimes produce "旁白", "VO", "narrator" etc; this collapses them all.
func normalizeSpeaker(s string) string {
	t := strings.ToLower(strings.TrimSpace(s))
	if t == "" || t == "旁白" || t == "narrator" || t == "vo" || t == "(vo)" {
		return ""
	}
	if strings.HasPrefix(t, "旁白") || strings.HasPrefix(t, "vo ") || strings.HasPrefix(t, "(vo)") {
		return ""
	}
	return strings.TrimSpace(s)
}

// panelLineType returns "旁白" when all dialogue lines are narration, else "對話".
func panelLineType(panel domain.Panel) string {
	for _, dl := range panel.DialogueLines {
		if normalizeSpeaker(dl.Speaker) != "" {
			return "對話"
		}
	}
	return "旁白"
}

// panelSpeakers returns a comma-joined list of unique non-narrator speakers.
func panelSpeakers(panel domain.Panel) string {
	seen := map[string]bool{}
	var out []string
	for _, dl := range panel.DialogueLines {
		if s := normalizeSpeaker(dl.Speaker); s != "" && !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
	}
	return strings.Join(out, "、")
}

// ── database management ───────────────────────────────────────────────────────

func findOrCreateDatabase(ctx context.Context, pageID, token, title string) (string, error) {
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
			Title     []textItem              `json:"title"`
			Properties map[string]json.RawMessage `json:"properties"`
		}
		if err := doJSON(ctx, http.MethodGet, dbURL, token, "", &dbInfo); err != nil {
			continue
		}
		if len(dbInfo.Title) == 0 || dbInfo.Title[0].PlainText != title {
			continue
		}

		missing := map[string]any{}
		for col, schema := range requiredProperties {
			if _, ok := dbInfo.Properties[col]; !ok {
				missing[col] = schema
			}
		}
		if len(missing) > 0 {
			patchBody, _ := json.Marshal(map[string]any{"properties": missing})
			if err := doJSON(ctx, http.MethodPatch, dbURL, token, string(patchBody), nil); err != nil {
				fmt.Fprintf(os.Stderr, "[Warning] Notion DB schema patch: %v\n", err)
			}
		}

		return block.ID, nil
	}

	// No existing database found — create one.
	payload := map[string]any{
		"parent": map[string]any{"type": "page_id", "page_id": pageID},
		"title":  richTextPayload(title),
		"properties": map[string]any{
			"幕號":       map[string]any{"title": map[string]any{}},
			"插圖":       map[string]any{"rich_text": map[string]any{}},
			"Grok 提示詞": map[string]any{"rich_text": map[string]any{}},
			"字幕文字":     map[string]any{"rich_text": map[string]any{}},
			"審核通過":     map[string]any{"checkbox": map[string]any{}},
			"備註":       map[string]any{"rich_text": map[string]any{}},
		},
	}
	body, _ := json.Marshal(payload)
	var resp struct {
		ID string `json:"id"`
	}
	if err := doJSON(ctx, http.MethodPost, "https://api.notion.com/v1/databases", token, string(body), &resp); err != nil {
		return "", fmt.Errorf("create Notion database: %w", err)
	}
	if resp.ID == "" {
		return "", fmt.Errorf("create Notion database: empty database id")
	}
	return resp.ID, nil
}

func ensurePageHeader(ctx context.Context, pageID, token, dbTitle, storyTitle string, total int) error {
	blocks, err := listBlockChildren(ctx, pageID, token)
	if err != nil {
		return fmt.Errorf("list Notion page children for header: %w", err)
	}

	insertAfter := ""
	hasDatabase := false
	for i, block := range blocks {
		if block.Type == "heading_1" {
			return nil // header already present
		}
		if block.Type == "child_database" {
			hasDatabase = true
			if i > 0 {
				insertAfter = blocks[i-1].ID
			}
			break
		}
	}

	payload := map[string]any{
		"children": []map[string]any{
			{
				"type": "heading_1",
				"heading_1": map[string]any{
					"rich_text": richTextPayload("🎬 " + dbTitle),
				},
			},
			{
				"type": "paragraph",
				"paragraph": map[string]any{
					"rich_text": richTextPayload(fmt.Sprintf(
						"專案：%s　總幕數：%d",
						storyTitle, total,
					)),
				},
			},
		},
	}
	if hasDatabase && insertAfter != "" {
		payload["after"] = insertAfter
	}

	body, _ := json.Marshal(payload)
	if err := doJSON(ctx, http.MethodPatch, "https://api.notion.com/v1/blocks/"+pageID+"/children", token, string(body), nil); err != nil {
		return fmt.Errorf("create Notion page header: %w", err)
	}
	return nil
}

func clearDatabaseRows(ctx context.Context, dbID, token string) error {
	rows, err := queryDatabase(ctx, dbID, token, nil)
	if err != nil {
		return fmt.Errorf("query Notion database for cleanup: %w", err)
	}
	for _, row := range rows {
		if err := doJSON(ctx, http.MethodDelete, "https://api.notion.com/v1/blocks/"+row.ID, token, "", nil); err != nil {
			return fmt.Errorf("delete Notion row %s: %w", row.ID, err)
		}
	}
	return nil
}

// upsertPanelRows upserts panel rows into an existing Notion database.
// Existing rows are matched by "幕號" title and updated; new rows are created.
// Rows not in the new panels list are preserved.
func upsertPanelRows(ctx context.Context, dbID string, panels []domain.Panel, imagePaths []string, coverImage string, token string) (map[string]int, error) {
	existingRows, err := queryDatabase(ctx, dbID, token, nil)
	if err != nil {
		return nil, fmt.Errorf("query existing rows: %w", err)
	}
	rowByTitle := make(map[string]string)
	for _, row := range existingRows {
		if title := propertyText(row.Properties["幕號"]); title != "" {
			rowByTitle[title] = row.ID
		}
	}

	pageIDMap := make(map[string]int)

	if coverImage != "" {
		coverFileID := ""
		if fid, err := uploadImage(ctx, coverImage, token); err == nil {
			coverFileID = fid
		} else {
			fmt.Fprintf(os.Stderr, "[Warning] Notion cover image upload: %v\n", err)
		}
		if existingID, ok := rowByTitle["封面"]; ok {
			updateRow(ctx, existingID, coverImage, coverFileID, "（封面圖片，不進入 I2V）", "", "旁白", "", token)
		} else {
			createCoverRow(ctx, dbID, coverImage, coverFileID, token)
		}
		delete(rowByTitle, "封面")
	}

	for i, panel := range panels {
		panelTitle := fmt.Sprintf("幕 %02d", i+1)
		imageName := "—"
		if i < len(imagePaths) && imagePaths[i] != "" {
			imageName = filepath.Base(imagePaths[i])
		}
		fileID := ""
		if i < len(imagePaths) && imagePaths[i] != "" {
			if fid, err := uploadImage(ctx, imagePaths[i], token); err == nil {
				fileID = fid
			} else {
				fmt.Fprintf(os.Stderr, "[Warning] Notion image upload panel %d: %v\n", i+1, err)
			}
		}
		lt := panelLineType(panel)
		speakers := panelSpeakers(panel)
		if existingID, ok := rowByTitle[panelTitle]; ok {
			updateRow(ctx, existingID, imageName, fileID, panel.Description, panel.Dialogue, lt, speakers, token)
			pageIDMap[existingID] = i
			delete(rowByTitle, panelTitle)
		} else {
			createdID := createPanelRow(ctx, dbID, panelTitle, imageName, fileID, panel.Description, panel.Dialogue, lt, speakers, token)
			if createdID != "" {
				pageIDMap[createdID] = i
			}
		}
	}
	return pageIDMap, nil
}

func updateRow(ctx context.Context, pageID, imageName string, fileID, description, dialogue, lineType, speakers, token string) {
	payload := map[string]any{
		"properties": map[string]any{
			"插圖":       map[string]any{"rich_text": richTextPayload(imageName)},
			"Grok 提示詞": map[string]any{"rich_text": richTextPayload(description)},
			"字幕文字":     map[string]any{"rich_text": richTextPayload(dialogue)},
			"類型":       map[string]any{"select": map[string]any{"name": lineType}},
			"說話者":      map[string]any{"rich_text": richTextPayload(speakers)},
		},
	}
	if fileID != "" {
		payload["cover"] = fileUploadPayload(fileID)
	}
	body, _ := json.Marshal(payload)
	_ = doJSON(ctx, http.MethodPatch, "https://api.notion.com/v1/pages/"+pageID, token, string(body), nil)
}

func createCoverRow(ctx context.Context, dbID, coverImage string, coverFileID string, token string) {
	payload := map[string]any{
		"parent": map[string]any{"type": "database_id", "database_id": dbID},
		"properties": map[string]any{
			"幕號":       map[string]any{"title": richTextPayload("封面")},
			"插圖":       map[string]any{"rich_text": richTextPayload(filepath.Base(coverImage))},
			"Grok 提示詞": map[string]any{"rich_text": richTextPayload("（封面圖片，不進入 I2V）")},
			"字幕文字":     map[string]any{"rich_text": richTextPayload("")},
			"審核通過":     map[string]any{"checkbox": true},
		},
	}
	if coverFileID != "" {
		payload["cover"] = fileUploadPayload(coverFileID)
	}
	body, _ := json.Marshal(payload)
	var created struct {
		ID string `json:"id"`
	}
	if err := doJSON(ctx, http.MethodPost, "https://api.notion.com/v1/pages", token, string(body), &created); err != nil {
		fmt.Fprintf(os.Stderr, "[Warning] Notion cover row creation: %v\n", err)
	} else if coverFileID != "" && created.ID != "" {
		addImageBlock(ctx, created.ID, coverFileID, token, "cover")
	}
}

func createPanelRow(ctx context.Context, dbID, panelTitle, imageName string, fileID string, description, dialogue, lineType, speakers, token string) string {
	payload := map[string]any{
		"parent": map[string]any{"type": "database_id", "database_id": dbID},
		"properties": map[string]any{
			"幕號":       map[string]any{"title": richTextPayload(panelTitle)},
			"插圖":       map[string]any{"rich_text": richTextPayload(imageName)},
			"Grok 提示詞": map[string]any{"rich_text": richTextPayload(description)},
			"字幕文字":     map[string]any{"rich_text": richTextPayload(dialogue)},
			"類型":       map[string]any{"select": map[string]any{"name": lineType}},
			"說話者":      map[string]any{"rich_text": richTextPayload(speakers)},
			"審核通過":     map[string]any{"checkbox": false},
		},
	}
	if fileID != "" {
		payload["cover"] = fileUploadPayload(fileID)
	}
	body, _ := json.Marshal(payload)
	var created struct {
		ID string `json:"id"`
	}
	if err := doJSON(ctx, http.MethodPost, "https://api.notion.com/v1/pages", token, string(body), &created); err != nil {
		fmt.Fprintf(os.Stderr, "[Warning] Notion row creation %s: %v\n", panelTitle, err)
		return ""
	}
	if created.ID != "" && fileID != "" {
		addImageBlock(ctx, created.ID, fileID, token, panelTitle)
	}
	return created.ID
}

// writePanelRows writes one DB row per panel and returns a map of
// Notion page UUID (with dashes) → panel index.
func writePanelRows(ctx context.Context, dbID string, panels []domain.Panel, imagePaths []string, coverImage string, token string) (map[string]int, error) {
	if coverImage != "" {
		coverFileID := ""
		if fid, err := uploadImage(ctx, coverImage, token); err == nil {
			coverFileID = fid
		} else {
			fmt.Fprintf(os.Stderr, "[Warning] Notion cover image upload: %v\n", err)
		}

		coverPayload := map[string]any{
			"parent": map[string]any{"type": "database_id", "database_id": dbID},
			"properties": map[string]any{
				"幕號":       map[string]any{"title": richTextPayload("封面")},
				"插圖":       map[string]any{"rich_text": richTextPayload(filepath.Base(coverImage))},
				"Grok 提示詞": map[string]any{"rich_text": richTextPayload("（封面圖片，不進入 I2V）")},
				"字幕文字":     map[string]any{"rich_text": richTextPayload("")},
				"審核通過":     map[string]any{"checkbox": true},
			},
		}
		if coverFileID != "" {
			coverPayload["cover"] = fileUploadPayload(coverFileID)
		}

		coverBody, _ := json.Marshal(coverPayload)
		var coverCreated struct {
			ID string `json:"id"`
		}
		if err := doJSON(ctx, http.MethodPost, "https://api.notion.com/v1/pages", token, string(coverBody), &coverCreated); err != nil {
			fmt.Fprintf(os.Stderr, "[Warning] Notion cover row creation: %v\n", err)
		} else if coverFileID != "" && coverCreated.ID != "" {
			addImageBlock(ctx, coverCreated.ID, coverFileID, token, "cover")
		}
	}

	pageIDMap := make(map[string]int, len(panels))
	for i, panel := range panels {
		imageName := "—"
		if i < len(imagePaths) && imagePaths[i] != "" {
			imageName = filepath.Base(imagePaths[i])
		}

		fileID := ""
		if i < len(imagePaths) && imagePaths[i] != "" {
			if fid, err := uploadImage(ctx, imagePaths[i], token); err == nil {
				fileID = fid
			} else {
				fmt.Fprintf(os.Stderr, "[Warning] Notion image upload panel %d: %v\n", i+1, err)
			}
		}

		payload := map[string]any{
			"parent": map[string]any{"type": "database_id", "database_id": dbID},
			"properties": map[string]any{
				"幕號":       map[string]any{"title": richTextPayload(fmt.Sprintf("幕 %02d", i+1))},
				"插圖":       map[string]any{"rich_text": richTextPayload(imageName)},
				"Grok 提示詞": map[string]any{"rich_text": richTextPayload(panel.Description)},
				"字幕文字":     map[string]any{"rich_text": richTextPayload(panel.Dialogue)},
				"審核通過":     map[string]any{"checkbox": false},
			},
		}
		if fileID != "" {
			payload["cover"] = fileUploadPayload(fileID)
		}

		body, _ := json.Marshal(payload)
		var created struct {
			ID string `json:"id"`
		}
		if err := doJSON(ctx, http.MethodPost, "https://api.notion.com/v1/pages", token, string(body), &created); err != nil {
			return nil, fmt.Errorf("create Notion row %d: %w", i+1, err)
		}
		if created.ID != "" {
			pageIDMap[created.ID] = i
			if fileID != "" {
				addImageBlock(ctx, created.ID, fileID, token, fmt.Sprintf("panel %d", i+1))
			}
		}
	}
	return pageIDMap, nil
}

func addImageBlock(ctx context.Context, pageID, fileID, token, label string) {
	imgBlock := map[string]any{
		"children": []any{
			map[string]any{
				"object": "block",
				"type":   "image",
				"image":  fileUploadPayload(fileID),
			},
		},
	}
	body, _ := json.Marshal(imgBlock)
	if err := doJSON(ctx, http.MethodPatch, "https://api.notion.com/v1/blocks/"+pageID+"/children", token, string(body), nil); err != nil {
		fmt.Fprintf(os.Stderr, "[Warning] Notion image block (%s): %v\n", label, err)
	}
}

func readPanelRows(ctx context.Context, dbID string, panels []domain.Panel, pageIDMap map[string]int, token string) ([]domain.Panel, error) {
	rows, err := queryDatabase(ctx, dbID, token, nil)
	if err != nil {
		return nil, fmt.Errorf("read Notion HITL rows: %w", err)
	}

	updated := append([]domain.Panel(nil), panels...)
	for _, row := range rows {
		idx, ok := pageIDMap[row.ID]
		if !ok {
			continue
		}
		if v, ok := row.Properties["Grok 提示詞"]; ok {
			updated[idx].Description = propertyText(v)
		}
		if v, ok := row.Properties["字幕文字"]; ok {
			updated[idx].Dialogue = propertyText(v)
		}
	}
	return updated, nil
}

// ── low-level API helpers ─────────────────────────────────────────────────────

func listBlockChildren(ctx context.Context, blockID, token string) ([]blockResult, error) {
	cursor := ""
	var blocks []blockResult
	for {
		endpoint := "https://api.notion.com/v1/blocks/" + blockID + "/children"
		if cursor != "" {
			endpoint += "?start_cursor=" + url.QueryEscape(cursor)
		}
		var resp blockChildrenResponse
		if err := doJSON(ctx, http.MethodGet, endpoint, token, "", &resp); err != nil {
			return nil, err
		}
		blocks = append(blocks, resp.Results...)
		if !resp.HasMore || resp.NextCursor == "" {
			break
		}
		cursor = resp.NextCursor
	}
	return blocks, nil
}

func queryDatabase(ctx context.Context, dbID, token string, sorts []map[string]any) ([]pageResult, error) {
	cursor := ""
	var rows []pageResult
	for {
		payload := map[string]any{}
		if len(sorts) > 0 {
			payload["sorts"] = sorts
		}
		if cursor != "" {
			payload["start_cursor"] = cursor
		}
		body := "{}"
		if len(payload) > 0 {
			b, _ := json.Marshal(payload)
			body = string(b)
		}
		var resp queryResponse
		if err := doJSON(ctx, http.MethodPost, "https://api.notion.com/v1/databases/"+dbID+"/query", token, body, &resp); err != nil {
			return nil, err
		}
		rows = append(rows, resp.Results...)
		if !resp.HasMore || resp.NextCursor == "" {
			break
		}
		cursor = resp.NextCursor
	}
	return rows, nil
}

func uploadImage(ctx context.Context, imagePath, token string) (string, error) {
	imgBytes, err := os.ReadFile(imagePath)
	if err != nil {
		return "", fmt.Errorf("read image: %w", err)
	}

	ext := strings.ToLower(filepath.Ext(imagePath))
	contentType := "image/png"
	if ext == ".jpg" || ext == ".jpeg" {
		contentType = "image/jpeg"
	}

	var session struct {
		ID        string `json:"id"`
		UploadURL string `json:"upload_url"`
	}
	if err := doJSON(ctx, http.MethodPost, "https://api.notion.com/v1/file_uploads", token, "{}", &session); err != nil {
		return "", fmt.Errorf("create file upload session: %w", err)
	}

	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	partHeader := make(map[string][]string)
	partHeader["Content-Disposition"] = []string{fmt.Sprintf(`form-data; name="file"; filename=%q`, filepath.Base(imagePath))}
	partHeader["Content-Type"] = []string{contentType}
	fw, err := mw.CreatePart(partHeader)
	if err != nil {
		return "", fmt.Errorf("create form part: %w", err)
	}
	if _, err := fw.Write(imgBytes); err != nil {
		return "", fmt.Errorf("write form part: %w", err)
	}
	mw.Close()

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, session.UploadURL, &buf)
	if err != nil {
		return "", fmt.Errorf("create upload request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Notion-Version", "2022-06-28")
	req.Header.Set("Content-Type", mw.FormDataContentType())

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("upload request failed: %w", err)
	}
	defer resp.Body.Close()
	respBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("upload failed %s: %s", resp.Status, strings.TrimSpace(string(respBody)))
	}
	return session.ID, nil
}

func fileUploadPayload(id string) map[string]any {
	return map[string]any{
		"type":        "file_upload",
		"file_upload": map[string]any{"id": id},
	}
}

func doJSON(ctx context.Context, method, endpoint, token, body string, dest any) error {
	req, err := http.NewRequestWithContext(ctx, method, endpoint, strings.NewReader(body))
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Notion-Version", "2022-06-28")
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()
	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("read response body: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("status %s: %s", resp.Status, strings.TrimSpace(string(respBody)))
	}
	if dest == nil || len(respBody) == 0 {
		return nil
	}
	if err := json.Unmarshal(respBody, dest); err != nil {
		return fmt.Errorf("decode response: %w", err)
	}
	return nil
}

func richTextPayload(content string) []map[string]any {
	chunks := splitText(content, 2000)
	rt := make([]map[string]any, 0, len(chunks))
	for _, chunk := range chunks {
		rt = append(rt, map[string]any{"type": "text", "text": map[string]any{"content": chunk}})
	}
	return rt
}

func splitText(content string, limit int) []string {
	if content == "" {
		return []string{}
	}
	runes := []rune(content)
	if len(runes) <= limit {
		return []string{content}
	}
	var chunks []string
	for start := 0; start < len(runes); start += limit {
		end := start + limit
		if end > len(runes) {
			end = len(runes)
		}
		chunks = append(chunks, string(runes[start:end]))
	}
	return chunks
}

func propertyText(v propertyValue) string {
	items := v.RichText
	if len(items) == 0 {
		items = v.Title
	}
	var b strings.Builder
	for _, item := range items {
		text := item.PlainText
		if text == "" {
			text = item.Text.Content
		}
		b.WriteString(text)
	}
	return b.String()
}
