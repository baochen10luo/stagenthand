package notion

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/baochen10luo/stagenthand/internal/domain"
)

type fakeNotionTransport struct {
	t        *testing.T
	requests []fakeNotionRequest
	handler  func(*http.Request, string) (int, string)
}

type fakeNotionRequest struct {
	Method string
	Path   string
	Query  string
	Body   string
}

func (rt *fakeNotionTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	rt.t.Helper()
	body, err := io.ReadAll(req.Body)
	if err != nil {
		rt.t.Fatalf("read request body: %v", err)
	}
	if got := req.Header.Get("Authorization"); got != "Bearer notion-token" {
		rt.t.Fatalf("Authorization = %q, want Bearer notion-token", got)
	}
	if got := req.Header.Get("Notion-Version"); got == "" {
		rt.t.Fatal("Notion-Version header is empty")
	}
	rt.requests = append(rt.requests, fakeNotionRequest{
		Method: req.Method,
		Path:   req.URL.Path,
		Query:  req.URL.RawQuery,
		Body:   string(body),
	})
	status, response := rt.handler(req, string(body))
	return &http.Response{
		StatusCode: status,
		Status:     http.StatusText(status),
		Header:     make(http.Header),
		Body:       io.NopCloser(strings.NewReader(response)),
		Request:    req,
	}, nil
}

func withFakeNotionTransport(t *testing.T, handler func(*http.Request, string) (int, string)) *fakeNotionTransport {
	t.Helper()
	oldTransport := http.DefaultTransport
	rt := &fakeNotionTransport{t: t, handler: handler}
	http.DefaultTransport = rt
	t.Cleanup(func() {
		http.DefaultTransport = oldTransport
	})
	return rt
}

func notionJSON(t *testing.T, v any) string {
	t.Helper()
	data, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal response: %v", err)
	}
	return string(data)
}

func notionTitleValue(text string) propertyValue {
	item := textItem{PlainText: text}
	item.Text.Content = text
	return propertyValue{Type: "title", Title: []textItem{item}}
}

func notionRichTextValue(text string) propertyValue {
	item := textItem{PlainText: text}
	item.Text.Content = text
	return propertyValue{Type: "rich_text", RichText: []textItem{item}}
}

func TestNotionPanelDialogueHelpers(t *testing.T) {
	panel := domain.Panel{
		DialogueLines: []domain.DialogueLine{
			{Speaker: "旁白", Text: "opening"},
			{Speaker: " Alice ", Text: "hello"},
			{Speaker: "Alice", Text: "again"},
			{Speaker: "Bob", Text: "reply"},
		},
	}

	if got := normalizeSpeaker(" (VO) calm "); got != "" {
		t.Fatalf("normalizeSpeaker() = %q, want narrator empty string", got)
	}
	if got := normalizeSpeaker(" Alice "); got != "Alice" {
		t.Fatalf("normalizeSpeaker() = %q, want Alice", got)
	}
	if got := panelLineType(panel); got != "對話" {
		t.Fatalf("panelLineType() = %q, want 對話", got)
	}
	if got := panelSpeakers(panel); got != "Alice、Bob" {
		t.Fatalf("panelSpeakers() = %q, want Alice、Bob", got)
	}
	if got := panelLineType(domain.Panel{DialogueLines: []domain.DialogueLine{{Speaker: "vo", Text: "only narration"}}}); got != "旁白" {
		t.Fatalf("narration panelLineType() = %q, want 旁白", got)
	}
}

func TestNotionMetadataHelpers(t *testing.T) {
	if got := formatDuration(92.5); got != "1:32" {
		t.Fatalf("formatDuration() = %q, want 1:32", got)
	}
	if got := fileUploadPayload("file-123"); got["type"] != "file_upload" {
		t.Fatalf("fileUploadPayload() = %#v", got)
	}
	if got := rtPlain("plain")["text"].(map[string]any)["content"]; got != "plain" {
		t.Fatalf("rtPlain content = %v", got)
	}
	bold := rtBold("bold")
	if got := bold["annotations"].(map[string]any)["bold"]; got != true {
		t.Fatalf("rtBold bold = %v, want true", got)
	}
	chunks := splitText(strings.Repeat("世", 2001), 2000)
	if len(chunks) != 2 || len([]rune(chunks[0])) != 2000 || len([]rune(chunks[1])) != 1 {
		t.Fatalf("splitText() chunks = %#v", chunks)
	}
	payload := richTextPayload("hello")
	if got := payload[0]["text"].(map[string]any)["content"]; got != "hello" {
		t.Fatalf("richTextPayload content = %v, want hello", got)
	}
	if got := propertyText(notionRichTextValue("edited")); got != "edited" {
		t.Fatalf("propertyText rich text = %q, want edited", got)
	}
	if got := propertyText(notionTitleValue("幕 01")); got != "幕 01" {
		t.Fatalf("propertyText title = %q, want 幕 01", got)
	}
}

func TestListBlockChildrenPaginates(t *testing.T) {
	rt := withFakeNotionTransport(t, func(req *http.Request, _ string) (int, string) {
		if req.URL.Path != "/v1/blocks/page-1/children" {
			t.Fatalf("path = %s", req.URL.Path)
		}
		if req.URL.Query().Get("start_cursor") == "" {
			return http.StatusOK, notionJSON(t, blockChildrenResponse{
				Results: []blockResult{{ID: "a", Type: "child_page", ChildPage: &struct {
					Title string `json:"title"`
				}{Title: "Story"}}},
				HasMore:    true,
				NextCursor: "cursor-2",
			})
		}
		return http.StatusOK, notionJSON(t, blockChildrenResponse{
			Results: []blockResult{{ID: "b", Type: "child_database", ChildDatabase: &struct {
				Title string `json:"title"`
			}{Title: "分鏡表"}}},
		})
	})

	got, err := listBlockChildren(context.Background(), "page-1", "notion-token")
	if err != nil {
		t.Fatalf("listBlockChildren() error = %v", err)
	}
	if len(got) != 2 || got[0].ID != "a" || got[1].ID != "b" {
		t.Fatalf("listBlockChildren() = %#v", got)
	}
	if len(rt.requests) != 2 || rt.requests[1].Query != "start_cursor=cursor-2" {
		t.Fatalf("requests = %#v", rt.requests)
	}
}

func TestQueryDatabasePaginatesWithSorts(t *testing.T) {
	rt := withFakeNotionTransport(t, func(req *http.Request, body string) (int, string) {
		if req.URL.Path != "/v1/databases/db-1/query" {
			t.Fatalf("path = %s", req.URL.Path)
		}
		if len(rtRequestsBodies(body)) == 0 {
			t.Fatalf("empty request body")
		}
		if !strings.Contains(body, "sorts") {
			t.Fatalf("query body missing sorts: %s", body)
		}
		if !strings.Contains(body, "start_cursor") {
			return http.StatusOK, notionJSON(t, queryResponse{
				Results:    []pageResult{{ID: "row-1"}},
				HasMore:    true,
				NextCursor: "cursor-2",
			})
		}
		return http.StatusOK, notionJSON(t, queryResponse{Results: []pageResult{{ID: "row-2"}}})
	})

	got, err := queryDatabase(context.Background(), "db-1", "notion-token", []map[string]any{{"timestamp": "created_time", "direction": "ascending"}})
	if err != nil {
		t.Fatalf("queryDatabase() error = %v", err)
	}
	if len(got) != 2 || got[0].ID != "row-1" || got[1].ID != "row-2" {
		t.Fatalf("queryDatabase() = %#v", got)
	}
	if len(rt.requests) != 2 {
		t.Fatalf("requests = %#v, want 2 requests", rt.requests)
	}
}

func rtRequestsBodies(body string) []string {
	return []string{body}
}

func TestFindOrCreateStoryPageUsesExistingChildPage(t *testing.T) {
	withFakeNotionTransport(t, func(req *http.Request, _ string) (int, string) {
		if req.Method != http.MethodGet {
			t.Fatalf("method = %s, want GET only", req.Method)
		}
		return http.StatusOK, notionJSON(t, blockChildrenResponse{
			Results: []blockResult{{ID: "story-existing", Type: "child_page", ChildPage: &struct {
				Title string `json:"title"`
			}{Title: "Wanted Story"}}},
		})
	})

	got, err := findOrCreateStoryPage(context.Background(), "parent-page", "notion-token", "Wanted Story")
	if err != nil {
		t.Fatalf("findOrCreateStoryPage() error = %v", err)
	}
	if got != "story-existing" {
		t.Fatalf("findOrCreateStoryPage() = %q, want story-existing", got)
	}
}

func TestFindOrCreateStoryPageCreatesMissingChildPage(t *testing.T) {
	rt := withFakeNotionTransport(t, func(req *http.Request, body string) (int, string) {
		switch req.Method + " " + req.URL.Path {
		case "GET /v1/blocks/parent-page/children":
			return http.StatusOK, notionJSON(t, blockChildrenResponse{})
		case "POST /v1/pages":
			if !strings.Contains(body, "New Story") {
				t.Fatalf("create story body missing title: %s", body)
			}
			return http.StatusOK, `{"id":"story-new"}`
		default:
			t.Fatalf("unexpected request: %s %s", req.Method, req.URL.Path)
			return http.StatusInternalServerError, "{}"
		}
	})

	got, err := findOrCreateStoryPage(context.Background(), "parent-page", "notion-token", "New Story")
	if err != nil {
		t.Fatalf("findOrCreateStoryPage() error = %v", err)
	}
	if got != "story-new" {
		t.Fatalf("findOrCreateStoryPage() = %q, want story-new", got)
	}
	if len(rt.requests) != 2 {
		t.Fatalf("requests = %#v, want list + create", rt.requests)
	}
}

func TestFindOrCreateDatabasePatchesExistingSchema(t *testing.T) {
	var sawPatch bool
	withFakeNotionTransport(t, func(req *http.Request, body string) (int, string) {
		switch req.Method + " " + req.URL.Path {
		case "GET /v1/blocks/story-page/children":
			return http.StatusOK, notionJSON(t, blockChildrenResponse{
				Results: []blockResult{{ID: "db-existing", Type: "child_database", ChildDatabase: &struct {
					Title string `json:"title"`
				}{Title: "分鏡表"}}},
			})
		case "GET /v1/databases/db-existing":
			return http.StatusOK, `{"properties":{"幕號":{}}}`
		case "PATCH /v1/databases/db-existing":
			sawPatch = true
			if !strings.Contains(body, "Grok 提示詞") || !strings.Contains(body, "審核通過") {
				t.Fatalf("schema patch body missing required properties: %s", body)
			}
			return http.StatusOK, `{}`
		default:
			t.Fatalf("unexpected request: %s %s", req.Method, req.URL.Path)
			return http.StatusInternalServerError, "{}"
		}
	})

	got, err := findOrCreateDatabase(context.Background(), "story-page", "notion-token")
	if err != nil {
		t.Fatalf("findOrCreateDatabase() error = %v", err)
	}
	if got != "db-existing" {
		t.Fatalf("findOrCreateDatabase() = %q, want db-existing", got)
	}
	if !sawPatch {
		t.Fatal("expected schema patch request")
	}
}

func TestFindOrCreateDatabaseCreatesWhenMissing(t *testing.T) {
	withFakeNotionTransport(t, func(req *http.Request, body string) (int, string) {
		switch req.Method + " " + req.URL.Path {
		case "GET /v1/blocks/story-page/children":
			return http.StatusOK, notionJSON(t, blockChildrenResponse{})
		case "POST /v1/databases":
			if !strings.Contains(body, "分鏡表") || !strings.Contains(body, "Grok 提示詞") {
				t.Fatalf("create database body missing schema: %s", body)
			}
			return http.StatusOK, `{"id":"db-new"}`
		default:
			t.Fatalf("unexpected request: %s %s", req.Method, req.URL.Path)
			return http.StatusInternalServerError, "{}"
		}
	})

	got, err := findOrCreateDatabase(context.Background(), "story-page", "notion-token")
	if err != nil {
		t.Fatalf("findOrCreateDatabase() error = %v", err)
	}
	if got != "db-new" {
		t.Fatalf("findOrCreateDatabase() = %q, want db-new", got)
	}
}

func TestHITLSkipsWhenTokenEmpty(t *testing.T) {
	panels := []domain.Panel{{Description: "keep"}}

	got, pageID, err := HITL(context.Background(), panels, nil, "", "Story", "parent-page", "", true, nil)
	if err != nil {
		t.Fatalf("HITL() error = %v", err)
	}
	if pageID != "" {
		t.Fatalf("pageID = %q, want empty", pageID)
	}
	if len(got) != 1 || got[0].Description != "keep" {
		t.Fatalf("HITL() panels = %#v", got)
	}
}

func TestHITLErrorsWhenPageIDEmpty(t *testing.T) {
	_, _, err := HITL(context.Background(), nil, nil, "", "Story", "", "notion-token", true, nil)
	if err == nil || !strings.Contains(err.Error(), "NOTION_GROK_PAGE_ID") {
		t.Fatalf("HITL() error = %v, want missing page id", err)
	}
}

func TestHITLUpsertsRowsAndReadsEdits(t *testing.T) {
	queryCalls := 0
	withFakeNotionTransport(t, func(req *http.Request, body string) (int, string) {
		switch req.Method + " " + req.URL.Path {
		case "GET /v1/blocks/parent-page/children":
			return http.StatusOK, notionJSON(t, blockChildrenResponse{
				Results: []blockResult{{ID: "story-page", Type: "child_page", ChildPage: &struct {
					Title string `json:"title"`
				}{Title: "Story"}}},
			})
		case "GET /v1/blocks/story-page/children":
			return http.StatusOK, notionJSON(t, blockChildrenResponse{
				Results: []blockResult{
					{ID: "callout-1", Type: "callout"},
					{ID: "db-1", Type: "child_database", ChildDatabase: &struct {
						Title string `json:"title"`
					}{Title: "分鏡表"}},
				},
			})
		case "GET /v1/databases/db-1":
			return http.StatusOK, `{"properties":{"幕號":{},"插圖":{},"Grok 提示詞":{},"字幕文字":{},"類型":{},"說話者":{},"審核通過":{},"備註":{}}}`
		case "POST /v1/databases/db-1/query":
			queryCalls++
			if queryCalls == 1 {
				return http.StatusOK, notionJSON(t, queryResponse{
					Results: []pageResult{{ID: "row-1", Properties: map[string]propertyValue{
						"幕號": notionTitleValue("幕 01"),
					}}},
				})
			}
			return http.StatusOK, notionJSON(t, queryResponse{
				Results: []pageResult{
					{ID: "row-1", Properties: map[string]propertyValue{
						"Grok 提示詞": notionRichTextValue("edited prompt 1"),
						"字幕文字":     notionRichTextValue("edited subtitle 1"),
					}},
					{ID: "row-2", Properties: map[string]propertyValue{
						"Grok 提示詞": notionRichTextValue("edited prompt 2"),
						"字幕文字":     notionRichTextValue("edited subtitle 2"),
					}},
				},
			})
		case "PATCH /v1/pages/row-1":
			if !strings.Contains(body, "first prompt") || !strings.Contains(body, "Alice") {
				t.Fatalf("update body missing panel data: %s", body)
			}
			return http.StatusOK, `{}`
		case "POST /v1/pages":
			if !strings.Contains(body, "幕 02") || !strings.Contains(body, "second prompt") {
				t.Fatalf("create body missing panel 2 data: %s", body)
			}
			return http.StatusOK, `{"id":"row-2"}`
		default:
			t.Fatalf("unexpected request: %s %s body=%s", req.Method, req.URL.Path, body)
			return http.StatusInternalServerError, "{}"
		}
	})

	panels := []domain.Panel{
		{
			Description: "first prompt",
			Dialogue:    "first subtitle",
			DialogueLines: []domain.DialogueLine{
				{Speaker: "Alice", Text: "first subtitle"},
			},
		},
		{
			Description: "second prompt",
			Dialogue:    "second subtitle",
			DialogueLines: []domain.DialogueLine{
				{Speaker: "旁白", Text: "second subtitle"},
			},
		},
	}

	got, storyPageID, err := HITL(context.Background(), panels, nil, "", "Story", "parent-page", "notion-token", true, nil)
	if err != nil {
		t.Fatalf("HITL() error = %v", err)
	}
	if storyPageID != "story-page" {
		t.Fatalf("storyPageID = %q, want story-page", storyPageID)
	}
	if got[0].Description != "edited prompt 1" || got[1].Dialogue != "edited subtitle 2" {
		t.Fatalf("updated panels = %#v", got)
	}
}

func TestWriteMetadataBlocksMovesDatabaseAndWritesStoryHeader(t *testing.T) {
	moveCalls := 0
	withFakeNotionTransport(t, func(req *http.Request, body string) (int, string) {
		switch req.Method + " " + req.URL.Path {
		case "GET /v1/blocks/story-page/children":
			return http.StatusOK, notionJSON(t, blockChildrenResponse{
				Results: []blockResult{{ID: "db-1", Type: "child_database", ChildDatabase: &struct {
					Title string `json:"title"`
				}{Title: "分鏡表"}}},
			})
		case "PATCH /v1/databases/db-1":
			moveCalls++
			if moveCalls == 1 && !strings.Contains(body, "parent-page") {
				t.Fatalf("first move body = %s, want parent-page", body)
			}
			if moveCalls == 2 && !strings.Contains(body, "story-page") {
				t.Fatalf("second move body = %s, want story-page", body)
			}
			return http.StatusOK, `{}`
		case "PATCH /v1/blocks/story-page/children":
			for _, want := range []string{"作者", "Bao", "故事簡介", "主要角色", "Programmer", "分鏡表"} {
				if !strings.Contains(body, want) {
					t.Fatalf("metadata body missing %q: %s", want, body)
				}
			}
			return http.StatusOK, `{}`
		default:
			t.Fatalf("unexpected request: %s %s body=%s", req.Method, req.URL.Path, body)
			return http.StatusInternalServerError, "{}"
		}
	})

	err := writeMetadataBlocks(context.Background(), "story-page", "parent-page", "notion-token", &domain.StoryboardManifest{
		Author:      "Bao",
		Category:    "short drama",
		Language:    "zh-TW",
		Synopsis:    "A small test story",
		TotalPanels: 2,
		TotalDurSec: 12,
		BGMTags:     "warm",
		ColorFilter: "cinematic",
		StylePrompt: "film look",
		Characters: []domain.CharacterMeta{
			{Name: "Programmer", Role: "lead", Description: "works late"},
		},
	}, "")
	if err != nil {
		t.Fatalf("writeMetadataBlocks() error = %v", err)
	}
	if moveCalls != 2 {
		t.Fatalf("moveCalls = %d, want 2", moveCalls)
	}
}

func TestClearDatabaseRowsDeletesEveryQueriedRow(t *testing.T) {
	var deleted []string
	withFakeNotionTransport(t, func(req *http.Request, _ string) (int, string) {
		switch req.Method + " " + req.URL.Path {
		case "POST /v1/databases/db-1/query":
			return http.StatusOK, notionJSON(t, queryResponse{
				Results: []pageResult{{ID: "row-1"}, {ID: "row-2"}},
			})
		case "DELETE /v1/blocks/row-1", "DELETE /v1/blocks/row-2":
			deleted = append(deleted, req.URL.Path)
			return http.StatusOK, `{}`
		default:
			t.Fatalf("unexpected request: %s %s", req.Method, req.URL.Path)
			return http.StatusInternalServerError, "{}"
		}
	})

	if err := clearDatabaseRows(context.Background(), "db-1", "notion-token"); err != nil {
		t.Fatalf("clearDatabaseRows() error = %v", err)
	}
	if strings.Join(deleted, ",") != "/v1/blocks/row-1,/v1/blocks/row-2" {
		t.Fatalf("deleted = %#v", deleted)
	}
}

func TestWritePanelRowsCreatesRowsAndReturnsPageMap(t *testing.T) {
	postCalls := 0
	withFakeNotionTransport(t, func(req *http.Request, body string) (int, string) {
		if req.Method != http.MethodPost || req.URL.Path != "/v1/pages" {
			t.Fatalf("unexpected request: %s %s", req.Method, req.URL.Path)
		}
		postCalls++
		if !strings.Contains(body, "幕 0") {
			t.Fatalf("row body missing title: %s", body)
		}
		return http.StatusOK, fmt.Sprintf(`{"id":"row-%d"}`, postCalls)
	})

	got, err := writePanelRows(context.Background(), "db-1", []domain.Panel{
		{Description: "prompt 1", Dialogue: "subtitle 1"},
		{Description: "prompt 2", Dialogue: "subtitle 2"},
	}, nil, "", "notion-token")
	if err != nil {
		t.Fatalf("writePanelRows() error = %v", err)
	}
	if got["row-1"] != 0 || got["row-2"] != 1 {
		t.Fatalf("page map = %#v", got)
	}
}

func TestCreateRowsWithFileIDsAttachImageBlocks(t *testing.T) {
	var imageBlocks []string
	withFakeNotionTransport(t, func(req *http.Request, body string) (int, string) {
		switch req.Method + " " + req.URL.Path {
		case "POST /v1/pages":
			if strings.Contains(body, "封面") {
				return http.StatusOK, `{"id":"cover-row"}`
			}
			if strings.Contains(body, "幕 01") && strings.Contains(body, "file-panel") {
				return http.StatusOK, `{"id":"panel-row"}`
			}
			t.Fatalf("unexpected page body: %s", body)
			return http.StatusInternalServerError, "{}"
		case "PATCH /v1/blocks/cover-row/children", "PATCH /v1/blocks/panel-row/children":
			imageBlocks = append(imageBlocks, body)
			return http.StatusOK, `{}`
		default:
			t.Fatalf("unexpected request: %s %s body=%s", req.Method, req.URL.Path, body)
			return http.StatusInternalServerError, "{}"
		}
	})

	createCoverRow(context.Background(), "db-1", "/tmp/cover.png", "file-cover", "notion-token")
	got := createPanelRow(context.Background(), "db-1", "幕 01", "panel.png", "file-panel", "prompt", "subtitle", "旁白", "", "notion-token")
	if got != "panel-row" {
		t.Fatalf("createPanelRow() = %q, want panel-row", got)
	}
	if len(imageBlocks) != 2 || !strings.Contains(imageBlocks[0], "file-cover") || !strings.Contains(imageBlocks[1], "file-panel") {
		t.Fatalf("image blocks = %#v", imageBlocks)
	}
}

func TestUploadImageCreatesSessionAndUploadsMultipart(t *testing.T) {
	imagePath := filepath.Join(t.TempDir(), "cover.jpg")
	if err := os.WriteFile(imagePath, []byte("fake-jpeg"), 0644); err != nil {
		t.Fatalf("write image: %v", err)
	}
	sawUpload := false
	withFakeNotionTransport(t, func(req *http.Request, body string) (int, string) {
		switch req.Method + " " + req.URL.Path {
		case "POST /v1/file_uploads":
			return http.StatusOK, `{"id":"file-1","upload_url":"https://upload.notion.test/upload"}`
		case "POST /upload":
			sawUpload = true
			if !strings.Contains(req.Header.Get("Content-Type"), "multipart/form-data") {
				t.Fatalf("Content-Type = %q, want multipart", req.Header.Get("Content-Type"))
			}
			if !strings.Contains(body, `filename="cover.jpg"`) || !strings.Contains(body, "image/jpeg") {
				t.Fatalf("upload body missing multipart image data: %s", body)
			}
			return http.StatusOK, `{}`
		default:
			t.Fatalf("unexpected request: %s %s", req.Method, req.URL.Path)
			return http.StatusInternalServerError, "{}"
		}
	})

	got, err := uploadImage(context.Background(), imagePath, "notion-token")
	if err != nil {
		t.Fatalf("uploadImage() error = %v", err)
	}
	if got != "file-1" || !sawUpload {
		t.Fatalf("uploadImage() = %q sawUpload=%v", got, sawUpload)
	}
}

func TestReadPanelsFindsDatabaseSortsRowsAndSkipsCover(t *testing.T) {
	withFakeNotionTransport(t, func(req *http.Request, _ string) (int, string) {
		switch req.Method + " " + req.URL.Path {
		case "GET /v1/blocks/parent-page/children":
			return http.StatusOK, notionJSON(t, blockChildrenResponse{
				Results: []blockResult{{ID: "story-page", Type: "child_page", ChildPage: &struct {
					Title string `json:"title"`
				}{Title: "Story"}}},
			})
		case "GET /v1/blocks/story-page/children":
			return http.StatusOK, notionJSON(t, blockChildrenResponse{
				Results: []blockResult{{ID: "db-1", Type: "child_database"}},
			})
		case "POST /v1/databases/db-1/query":
			return http.StatusOK, notionJSON(t, queryResponse{
				Results: []pageResult{
					{ID: "cover", Properties: map[string]propertyValue{"幕號": notionTitleValue("封面")}},
					{ID: "row-2", Properties: map[string]propertyValue{
						"幕號":       notionTitleValue("幕 02"),
						"字幕文字":     notionRichTextValue("subtitle 2"),
						"Grok 提示詞": notionRichTextValue("prompt 2"),
						"插圖":       notionRichTextValue("panel2.png"),
					}},
					{ID: "row-1", Properties: map[string]propertyValue{
						"幕號":       notionTitleValue("幕 01"),
						"字幕文字":     notionRichTextValue("subtitle 1"),
						"Grok 提示詞": notionRichTextValue("prompt 1"),
						"插圖":       notionRichTextValue("panel1.png"),
					}},
				},
			})
		default:
			t.Fatalf("unexpected request: %s %s", req.Method, req.URL.Path)
			return http.StatusInternalServerError, "{}"
		}
	})

	got, err := ReadPanels(context.Background(), "parent-page", "Story", "notion-token")
	if err != nil {
		t.Fatalf("ReadPanels() error = %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("ReadPanels() len = %d, want 2: %#v", len(got), got)
	}
	if got[0].PanelNumber != 1 || got[0].Description != "prompt 1" || got[1].PanelNumber != 2 {
		t.Fatalf("ReadPanels() = %#v", got)
	}
}

func TestReadPanelsReturnsHelpfulFindDatabaseErrors(t *testing.T) {
	withFakeNotionTransport(t, func(req *http.Request, _ string) (int, string) {
		if req.Method != http.MethodGet || req.URL.Path != "/v1/blocks/parent-page/children" {
			t.Fatalf("unexpected request: %s %s", req.Method, req.URL.Path)
		}
		return http.StatusOK, notionJSON(t, blockChildrenResponse{})
	})

	_, err := ReadPanels(context.Background(), "parent-page", "Missing Story", "notion-token")
	if err == nil || !strings.Contains(err.Error(), "Missing Story") {
		t.Fatalf("ReadPanels() error = %v, want missing story page", err)
	}
}
