package cmd

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestFromSourceFindSingleFile(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "story.txt"), []byte("story"), 0o600); err != nil {
		t.Fatalf("write story: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "notes.md"), []byte("notes"), 0o600); err != nil {
		t.Fatalf("write notes: %v", err)
	}

	got, err := findSingleFile(dir, ".txt")
	if err != nil {
		t.Fatalf("findSingleFile() error = %v", err)
	}
	if got != filepath.Join(dir, "story.txt") {
		t.Fatalf("findSingleFile() = %q, want story.txt", got)
	}

	_, err = findSingleFile(dir, ".json")
	if err == nil || !strings.Contains(err.Error(), "no .json file") {
		t.Fatalf("findSingleFile() missing error = %v, want no .json file", err)
	}
}

func TestFromSourceFindNumberedImagesSortsByNumericSuffix(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	for _, name := range []string{"panel_10.png", "panel_2.png", "panel_1.png", "cover.png", "panel_3.jpg"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(name), 0o600); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}

	got, err := findNumberedImages(dir)
	if err != nil {
		t.Fatalf("findNumberedImages() error = %v", err)
	}
	want := []string{
		filepath.Join(dir, "panel_1.png"),
		filepath.Join(dir, "panel_2.png"),
		filepath.Join(dir, "panel_10.png"),
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("findNumberedImages() = %#v, want %#v", got, want)
	}

	empty := t.TempDir()
	_, err = findNumberedImages(empty)
	if err == nil || !strings.Contains(err.Error(), "no _N.png images") {
		t.Fatalf("findNumberedImages() missing error = %v, want no _N.png images", err)
	}
}

func TestFromSourceSanitizeProjectIDAndCopyFile(t *testing.T) {
	t.Parallel()

	if got := sanitizeProjectID("  My 故事: 2026!! "); got != "my_2026" {
		t.Fatalf("sanitizeProjectID() = %q, want my_2026", got)
	}

	dir := t.TempDir()
	src := filepath.Join(dir, "src.txt")
	dst := filepath.Join(dir, "dst.txt")
	if err := os.WriteFile(src, []byte("payload"), 0o600); err != nil {
		t.Fatalf("write src: %v", err)
	}
	if err := copyFileBytes(src, dst); err != nil {
		t.Fatalf("copyFileBytes() error = %v", err)
	}
	data, err := os.ReadFile(dst)
	if err != nil {
		t.Fatalf("read dst: %v", err)
	}
	if string(data) != "payload" {
		t.Fatalf("copied data = %q, want payload", string(data))
	}
}

func TestFromSourceSplitToSubtitleLines(t *testing.T) {
	t.Parallel()

	if got := splitToSubtitleLines("短句"); !reflect.DeepEqual(got, []string{"短句"}) {
		t.Fatalf("short split = %#v", got)
	}

	got := splitToSubtitleLines("第一句很長很長很長。第二句也很長很長！第三句繼續說？第四句收尾。")
	if len(got) != 3 {
		t.Fatalf("sentence split len = %d, want 3: %#v", len(got), got)
	}
	if strings.Join(got, "") != "第一句很長很長很長。第二句也很長很長！第三句繼續說？第四句收尾。" {
		t.Fatalf("sentence split changed text: %#v", got)
	}

	got = splitToSubtitleLines("第一段很長很長，第二段也很長；第三段繼續說，第四段收尾")
	if len(got) != 3 {
		t.Fatalf("clause split len = %d, want 3: %#v", len(got), got)
	}
}

func TestFromSourceExtractJSONOrArray(t *testing.T) {
	t.Parallel()

	array := extractJSONOrArray([]byte("```json\n[{\"tts_text\":\"a\"}]\n```"))
	var wrapped struct {
		Panels []map[string]string `json:"panels"`
	}
	if err := json.Unmarshal(array, &wrapped); err != nil {
		t.Fatalf("array wrapper JSON invalid: %v\n%s", err, array)
	}
	if len(wrapped.Panels) != 1 || wrapped.Panels[0]["tts_text"] != "a" {
		t.Fatalf("wrapped panels = %#v", wrapped.Panels)
	}

	object := extractJSONOrArray([]byte("prefix ```json\n{\"panels\":[{\"tts_text\":\"b\"}]}\n``` suffix"))
	if err := json.Unmarshal(object, &wrapped); err != nil {
		t.Fatalf("object JSON invalid: %v\n%s", err, object)
	}
	if wrapped.Panels[0]["tts_text"] != "b" {
		t.Fatalf("object panels = %#v", wrapped.Panels)
	}
}
