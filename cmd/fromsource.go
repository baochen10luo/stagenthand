package cmd

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/baochen10luo/stagenthand/internal/domain"
	"github.com/baochen10luo/stagenthand/internal/llm"
	"github.com/spf13/cobra"
)

var (
	fromSourceStoryDir    string
	fromSourceProjectID   string
	fromSourceAuthor      string
	fromSourceBGMTags     string
	fromSourceColorFilter string
)

const fromSourceSystemPrompt = `你是影片字幕分段專家，專門處理繁體中文紀念短片腳本。

給定故事原文和圖片數量 N，將原文分成恰好 N 段，每段對應一張圖片。

規則：
1. 保留原始文字，禁止改寫或意譯
2. 分成恰好 N 段（不多不少）
3. 每段輸出：
   - tts_text：該段完整原文（供 TTS 朗讀，保持原話）
   - speaker：旁白留空 ""，引號內角色直接說話才填角色名
   - subtitle_lines：2–3 條字幕，每條不超過 18 個中文字，直接從 tts_text 截取原文

只輸出 JSON，不要加任何說明或 markdown，格式：
{"panels":[{"tts_text":"...","speaker":"","subtitle_lines":["...","..."]}]}`

var fromSourceCmd = &cobra.Command{
	Use:   "from-source",
	Short: "Build storyboard manifest from original story .txt + images using LLM segmentation",
	RunE: func(cmd *cobra.Command, args []string) error {
		if fromSourceStoryDir == "" {
			return stageError("from-source", "missing_flag", "--story-dir is required")
		}

		// ── 1. Find .txt file ─────────────────────────────────────────────
		txtPath, err := findSingleFile(fromSourceStoryDir, ".txt")
		if err != nil {
			return stageError("from-source", "no_txt", err.Error())
		}
		storyBytes, err := os.ReadFile(txtPath)
		if err != nil {
			return stageError("from-source", "read_error", err.Error())
		}
		storyText := strings.TrimSpace(string(storyBytes))
		storyTitle := strings.TrimSpace(strings.SplitN(storyText, "\n", 2)[0])

		// ── 2. Find + sort images ─────────────────────────────────────────
		images, err := findNumberedImages(fromSourceStoryDir)
		if err != nil {
			return stageError("from-source", "no_images", err.Error())
		}
		n := len(images)
		fmt.Fprintf(os.Stderr, "[Info] found %d images in %s\n", n, fromSourceStoryDir)

		// ── 3. Derive project ID + paths ──────────────────────────────────
		projectID := fromSourceProjectID
		if projectID == "" {
			projectID = sanitizeProjectID(filepath.Base(fromSourceStoryDir))
		}
		shandHome, _ := os.UserHomeDir()
		shandHome = filepath.Join(shandHome, ".shand")
		outputDir := filepath.Join(shandHome, "projects", projectID)
		imgDir := filepath.Join(outputDir, "images")
		if err := os.MkdirAll(imgDir, 0755); err != nil {
			return stageError("from-source", "mkdir_error", err.Error())
		}

		// ── 4. Copy images ────────────────────────────────────────────────
		for i, src := range images {
			idx := i + 1
			dst := filepath.Join(imgDir, fmt.Sprintf("scene_%d_panel_%d.png", idx, idx))
			if err := copyFileBytes(src, dst); err != nil {
				return stageError("from-source", "copy_error", err.Error())
			}
		}
		fmt.Fprintf(os.Stderr, "[Info] images copied → %s\n", imgDir)

		// ── 5. LLM segmentation ───────────────────────────────────────────
		llmClient, err := llm.NewClient(cfg.LLM.Provider, dryRun, cfg)
		if err != nil {
			return stageError("from-source", "llm_init", err.Error())
		}

		userInput := fmt.Sprintf("N: %d\n\n故事原文：\n%s", n, storyText)
		fmt.Fprintf(os.Stderr, "[Info] calling LLM to segment %d panels...\n", n)

		raw, err := llmClient.GenerateTransformation(cmd.Context(), fromSourceSystemPrompt, []byte(userInput))
		if err != nil {
			return stageError("from-source", "llm_error", err.Error())
		}

		// ── 6. Parse LLM JSON ─────────────────────────────────────────────
		var llmOut struct {
			Panels []struct {
				TTSText       string   `json:"tts_text"`
				Speaker       string   `json:"speaker"`
				SubtitleLines []string `json:"subtitle_lines"`
			} `json:"panels"`
		}
		clean := extractJSON(raw)
		if err := json.Unmarshal(clean, &llmOut); err != nil {
			return stageError("from-source", "parse_error",
				fmt.Sprintf("%v\nraw output: %s", err, string(raw)))
		}
		if len(llmOut.Panels) != n {
			fmt.Fprintf(os.Stderr, "[Warning] LLM returned %d panels, expected %d — using what we got\n",
				len(llmOut.Panels), n)
			if len(llmOut.Panels) > n {
				llmOut.Panels = llmOut.Panels[:n]
			}
		}

		// ── 7. Build domain panels ────────────────────────────────────────
		motions := []string{
			"ken_burns_in", "static", "ken_burns_out", "pan_left", "pan_right",
			"ken_burns_in", "static", "ken_burns_out", "pan_right", "ken_burns_in", "static",
		}
		panels := make([]domain.Panel, len(llmOut.Panels))
		for i, p := range llmOut.Panels {
			idx := i + 1
			motion := motions[i%len(motions)]

			dlLines := make([]domain.DialogueLine, len(p.SubtitleLines))
			for j, t := range p.SubtitleLines {
				dlLines[j] = domain.DialogueLine{Speaker: p.Speaker, Text: t, Emotion: "neutral"}
			}

			panels[i] = domain.Panel{
				SceneNumber: idx,
				PanelNumber: idx,
				Dialogue:    p.TTSText,
				ImageURL:    filepath.Join(imgDir, fmt.Sprintf("scene_%d_panel_%d.png", idx, idx)),
				DurationSec: 5.0,
				Directive: &domain.PanelDirective{
					MotionEffect:    motion,
					MotionIntensity: 0.05,
					TransitionIn:    "fade",
					TransitionOut:   "fade",
					SubtitleEffect:  "fade",
					SubtitlePosition: "bottom",
				},
				DialogueLines: dlLines,
			}
		}

		// ── 8. Build + write manifest ─────────────────────────────────────
		bgmTags := fromSourceBGMTags
		if bgmTags == "" {
			bgmTags = "cinematic+emotional+strings"
		}
		colorFilter := fromSourceColorFilter
		if colorFilter == "" {
			colorFilter = "cinematic"
		}

		manifest := domain.StoryboardManifest{
			ProjectID:   projectID,
			StoryTitle:  storyTitle,
			Author:      fromSourceAuthor,
			Language:    "zh-TW",
			BGMTags:     bgmTags,
			ColorFilter: colorFilter,
			TotalPanels: n,
			Panels:      panels,
		}

		manifestPath := filepath.Join(outputDir, "storyboard_manifest.json")
		data, _ := json.MarshalIndent(manifest, "", "  ")
		if err := os.WriteFile(manifestPath, data, 0644); err != nil {
			return stageError("from-source", "write_error", err.Error())
		}
		fmt.Fprintf(os.Stderr, "[Info] manifest written → %s\n", manifestPath)

		return json.NewEncoder(os.Stdout).Encode(map[string]any{
			"project_id":  projectID,
			"story_title": storyTitle,
			"panel_count": len(panels),
			"output_dir":  outputDir,
			"manifest":    manifestPath,
		})
	},
}

// findSingleFile returns the first file with the given extension in dir.
func findSingleFile(dir, ext string) (string, error) {
	var found string
	_ = filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err == nil && !d.IsDir() && strings.HasSuffix(path, ext) && found == "" {
			found = path
		}
		return nil
	})
	if found == "" {
		return "", fmt.Errorf("no %s file in %s", ext, dir)
	}
	return found, nil
}

// findNumberedImages returns image paths sorted by their _N suffix.
func findNumberedImages(dir string) ([]string, error) {
	re := regexp.MustCompile(`_(\d+)\.png$`)
	type entry struct {
		path string
		num  int
	}
	var items []entry
	_ = filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err == nil && !d.IsDir() {
			if m := re.FindStringSubmatch(filepath.Base(path)); m != nil {
				var num int
				fmt.Sscanf(m[1], "%d", &num)
				items = append(items, entry{path, num})
			}
		}
		return nil
	})
	if len(items) == 0 {
		return nil, fmt.Errorf("no _N.png images in %s", dir)
	}
	sort.Slice(items, func(i, j int) bool { return items[i].num < items[j].num })
	paths := make([]string, len(items))
	for i, e := range items {
		paths[i] = e.path
	}
	return paths, nil
}

var nonAlnumRe = regexp.MustCompile(`[^a-zA-Z0-9]+`)

func sanitizeProjectID(s string) string {
	return strings.ToLower(strings.Trim(nonAlnumRe.ReplaceAllString(s, "_"), "_"))
}

func copyFileBytes(src, dst string) error {
	data, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	return os.WriteFile(dst, data, 0644)
}

// extractJSON strips markdown fences and returns the first JSON object found.
func extractJSON(raw []byte) []byte {
	s := bytes.TrimSpace(raw)
	// strip ```json ... ``` or ``` ... ```
	if i := bytes.Index(s, []byte("```")); i != -1 {
		s = s[i:]
		if j := bytes.Index(s[3:], []byte("```")); j != -1 {
			s = s[3 : 3+j]
			// skip language tag line
			if nl := bytes.Index(s, []byte("\n")); nl != -1 {
				s = bytes.TrimSpace(s[nl:])
			}
		}
	}
	// find outermost { }
	start := bytes.IndexByte(s, '{')
	end := bytes.LastIndexByte(s, '}')
	if start != -1 && end > start {
		return s[start : end+1]
	}
	return s
}

func init() {
	fromSourceCmd.Flags().StringVar(&fromSourceStoryDir, "story-dir", "",
		"folder containing story .txt and _N.png images")
	fromSourceCmd.Flags().StringVar(&fromSourceProjectID, "project-id", "",
		"project ID (defaults to sanitized dir name)")
	fromSourceCmd.Flags().StringVar(&fromSourceAuthor, "author", "",
		"story author name")
	fromSourceCmd.Flags().StringVar(&fromSourceBGMTags, "bgm-tags", "",
		"BGM tags e.g. 'cinematic+emotional+strings'")
	fromSourceCmd.Flags().StringVar(&fromSourceColorFilter, "color-filter", "",
		"color filter: cinematic|vintage|none")
	rootCmd.AddCommand(fromSourceCmd)
}
