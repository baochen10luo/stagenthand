package pronunciation

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
	"unicode"
)

// STTAuditResult holds the comparison between original dialogue and STT transcription.
type STTAuditResult struct {
	PanelIndex   int              `json:"panel_index"`
	SceneNumber  int              `json:"scene_number"`
	PanelNumber  int              `json:"panel_number"`
	Original     string           `json:"original"`
	Transcribed  string           `json:"transcribed"`
	Mismatches   []CharMismatch   `json:"mismatches,omitempty"`
	Pass         bool             `json:"pass"`
	ErrorMessage string           `json:"error_message,omitempty"`
}

// CharMismatch describes a single character that was transcribed differently.
type CharMismatch struct {
	Position         int      `json:"position"`          // character index in original
	Expected         string   `json:"expected"`          // the original character
	Got              string   `json:"got"`               // what STT returned
	HomophoneSuggest []string `json:"homophone_suggest,omitempty"` // replacement suggestions
	LikelyTTS        bool     `json:"likely_tts"`        // true if got ≠ any known homophone of expected (likely real TTS error)
}

// STTAuditConfig configures the STT round-trip auditor.
type STTAuditConfig struct {
	STTURL   string // aiark STT API URL
	APIKey   string
	Homophones HomophoneMap // custom homophone map; nil = use DefaultHomophones
}

// STTAuditor performs STT round-trip audits on TTS audio.
type STTAuditor struct {
	cfg    STTAuditConfig
	client *http.Client
}

// Cfg returns the auditor's config (for accessing homophones).
func (a *STTAuditor) Cfg() STTAuditConfig { return a.cfg }

// NewSTTAuditor creates a new STT auditor.
func NewSTTAuditor(cfg STTAuditConfig) *STTAuditor {
	if cfg.STTURL == "" {
		cfg.STTURL = "https://aiark.com.tw/v1/audio/transcriptions"
	}
	if cfg.Homophones == nil {
		cfg.Homophones = DefaultHomophones()
	}
	return &STTAuditor{
		cfg:    cfg,
		client: &http.Client{Timeout: 120 * time.Second},
	}
}

// Transcribe sends audio bytes to the STT API and returns the transcribed text.
func (a *STTAuditor) Transcribe(ctx context.Context, audioData []byte) (string, error) {
	boundary := "----STTAuditBoundary"
	var buf bytes.Buffer

	// Write multipart form data (no model param — aiark uses default faster-whisper)
	buf.WriteString(fmt.Sprintf("--%s\r\n", boundary))
	buf.WriteString("Content-Disposition: form-data; name=\"file\"; filename=\"audio.wav\"\r\n")
	buf.WriteString("Content-Type: audio/wav\r\n\r\n")
	buf.Write(audioData)
	buf.WriteString("\r\n")
	buf.WriteString(fmt.Sprintf("--%s--\r\n", boundary))

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, a.cfg.STTURL, &buf)
	if err != nil {
		return "", fmt.Errorf("stt request: %w", err)
	}
	req.Header.Set("Content-Type", fmt.Sprintf("multipart/form-data; boundary=%s", boundary))
	req.Header.Set("Authorization", "Bearer "+a.cfg.APIKey)

	resp, err := a.client.Do(req)
	if err != nil {
		return "", fmt.Errorf("stt request: %w", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("stt status %d: %s", resp.StatusCode, string(body))
	}

	var result struct {
		Text string `json:"text"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return "", fmt.Errorf("stt parse: %w", err)
	}
	return strings.TrimSpace(result.Text), nil
}

// Audit compares original dialogue with STT transcription and reports mismatches.
// Both strings are normalized to Traditional Chinese before comparison.
func (a *STTAuditor) Audit(original, transcribed string, panelIdx, sceneNum, panelNum int) STTAuditResult {
	result := STTAuditResult{
		PanelIndex:  panelIdx,
		SceneNumber: sceneNum,
		PanelNumber: panelNum,
		Original:    original,
		Transcribed: transcribed,
	}

	if transcribed == "" {
		result.ErrorMessage = "empty transcription"
		return result
	}

	// Normalize both to Traditional Chinese for fair comparison
	origNorm := NormalizeToTraditional(original)
	transNorm := NormalizeToTraditional(transcribed)

	mismatches := findCharMismatches(origNorm, transNorm)
	for i := range mismatches {
		mm := &mismatches[i]
		sugg := a.cfg.Homophones.Lookup(mm.Expected)
		if len(sugg) > 0 {
			mm.HomophoneSuggest = sugg
		}
		// LikelyTTS: if "got" is not among known homophones of "expected",
		// the STT heard a different sound → likely a real TTS mispronunciation.
		mm.LikelyTTS = true
		for _, s := range sugg {
			if s == mm.Got {
				mm.LikelyTTS = false // same pronunciation, STT ambiguity
				break
			}
		}
	}
	result.Mismatches = mismatches
	result.Pass = len(mismatches) == 0
	return result
}

// AuditPanelText is a convenience to audit dialogue text directly.
func (a *STTAuditor) AuditPanelText(original, transcribed string) []CharMismatch {
	return findCharMismatches(original, transcribed)
}

// BuildCorrectedInput builds a TTS input with problematic characters replaced by homophones.
// It returns the corrected text AND a map of replacements applied for logging.
// Only replaces characters that have exactly one homophone suggestion (safe replacement).
// For characters with multiple suggestions, uses the first one.
func BuildCorrectedInput(original string, mismatches []CharMismatch, homophones HomophoneMap) (string, map[int]string) {
	replacements := make(map[int]string)
	runes := []rune(original)

	for _, mm := range mismatches {
		if mm.Position < 0 || mm.Position >= len(runes) {
			continue
		}
		sugg := homophones.Lookup(string(runes[mm.Position]))
		if len(sugg) == 0 {
			continue
		}
		// Use the first suggestion
		replacement := sugg[0]
		runes[mm.Position] = []rune(replacement)[0]
		replacements[mm.Position] = replacement
	}

	return string(runes), replacements
}

// findCharMismatches aligns original and transcribed text character-by-character
// using a Needleman-Wunsch-like global alignment, then reports mismatches.
func findCharMismatches(original, transcribed string) []CharMismatch {
	origRunes := filterHanAndLetters(original)
	transRunes := filterHanAndLetters(transcribed)

	if len(origRunes) == 0 {
		return nil
	}

	// Build alignment using edit distance with traceback
	alignedOrig, alignedTrans := align(origRunes, transRunes)

	var mismatches []CharMismatch
	origPos := 0
	transPos := 0

	for i := range alignedOrig {
		if alignedOrig[i] == '-' {
			// Deletion in original: STT added an extra character
			transPos++
			continue
		}
		if alignedTrans[i] == '-' {
			// Insertion in original: STT missed a character
			if alignedOrig[i] != ' ' {
				// Report character that was not transcribed
				srcRune := alignedOrig[i]
				// Find position in original string
				pos := findRunePosition(original, srcRune, origPos)
				mismatches = append(mismatches, CharMismatch{
					Position: pos,
					Expected: string(srcRune),
					Got:      "(missing)",
				})
			}
			origPos++
			continue
		}
		if alignedOrig[i] != alignedTrans[i] {
			// Substitution
			pos := findRunePosition(original, alignedOrig[i], origPos)
			mismatches = append(mismatches, CharMismatch{
				Position: pos,
				Expected: string(alignedOrig[i]),
				Got:      string(alignedTrans[i]),
			})
		}
		origPos++
		transPos++
	}

	return mismatches
}

// findRunePosition finds the position (in runes) of a given rune in the original string,
// starting the search from the given offset.
func findRunePosition(s string, target rune, offset int) int {
	runes := []rune(s)
	count := 0
	for i, r := range runes {
		if unicode.Is(unicode.Han, r) || unicode.IsLetter(r) {
			if count >= offset && r == target {
				return i
			}
			count++
		}
	}
	return offset
}

// filterHanAndLetters keeps only Han characters and letters, removing punctuation/spaces.
func filterHanAndLetters(s string) []rune {
	var result []rune
	for _, r := range s {
		if isSignificantRune(r) {
			result = append(result, r)
		}
	}
	return result
}

func isSignificantRune(r rune) bool {
	return unicode.Is(unicode.Han, r) || unicode.IsLetter(r)
}

// align performs global alignment of two rune slices, returns aligned pairs.
// Uses Needleman-Wunsch with match=1, mismatch=-1, gap=-1.
func align(a, b []rune) ([]rune, []rune) {
	// Build DP matrix
	n, m := len(a), len(b)
	dp := make([][]int, n+1)
	for i := range dp {
		dp[i] = make([]int, m+1)
		dp[i][0] = -i
	}
	for j := range dp[0] {
		dp[0][j] = -j
	}
	for i := 1; i <= n; i++ {
		for j := 1; j <= m; j++ {
			score := -1
			if a[i-1] == b[j-1] {
				score = 1
			}
			dp[i][j] = max3(
				dp[i-1][j-1]+score,
				dp[i-1][j]-1,
				dp[i][j-1]-1,
			)
		}
	}

	// Traceback
	var alignedA, alignedB []rune
	i, j := n, m
	for i > 0 || j > 0 {
		if i > 0 && j > 0 && dp[i][j] == dp[i-1][j-1]+scoreMatch(a[i-1], b[j-1]) {
			alignedA = append([]rune{a[i-1]}, alignedA...)
			alignedB = append([]rune{b[j-1]}, alignedB...)
			i--
			j--
		} else if i > 0 && dp[i][j] == dp[i-1][j]-1 {
			alignedA = append([]rune{a[i-1]}, alignedA...)
			alignedB = append([]rune{'-'}, alignedB...)
			i--
		} else {
			alignedA = append([]rune{'-'}, alignedA...)
			alignedB = append([]rune{b[j-1]}, alignedB...)
			j--
		}
	}
	return alignedA, alignedB
}

func scoreMatch(a, b rune) int {
	if a == b {
		return 1
	}
	return -1
}

func max3(a, b, c int) int {
	if a >= b && a >= c {
		return a
	}
	if b >= a && b >= c {
		return b
	}
	return c
}
