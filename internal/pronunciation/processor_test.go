package pronunciation

import (
	"strings"
	"testing"
)

func TestOverrideProcessor_Process(t *testing.T) {
	dict := MapDict{
		"角色": {{Char: "角", IPA: "tɕɥe³⁵"}, {Char: "色", IPA: "sɤ⁵¹"}},
		"主角": {{Char: "主", IPA: "tʂu²¹⁴"}, {Char: "角", IPA: "tɕɥe³⁵"}},
	}
	p := NewOverrideProcessor(dict)

	t.Run("replaces known word in SSML", func(t *testing.T) {
		input := `<speak><prosody rate="90%">他是主角</prosody></speak>`
		got := p.Process(input)
		want := `<speak><prosody rate="90%">他是` +
			`<phoneme alphabet="ipa" ph="tʂu²¹⁴">主</phoneme>` +
			`<phoneme alphabet="ipa" ph="tɕɥe³⁵">角</phoneme>` +
			`</prosody></speak>`
		if got != want {
			t.Errorf("Process mismatch.\ngot:  %s\nwant: %s", got, want)
		}
	})

	t.Run("replaces multiple different words", func(t *testing.T) {
		input := `<speak><prosody rate="90%">他是主角，扮演重要角色</prosody></speak>`
		got := p.Process(input)
		// Both 主角 and 角色 should be replaced
		if !strings.Contains(got, `<phoneme alphabet="ipa" ph="tʂu²¹⁴">主</phoneme>`) {
			t.Error("expected 主角 to be replaced")
		}
		if !strings.Contains(got, `<phoneme alphabet="ipa" ph="sɤ⁵¹">色</phoneme>`) {
			t.Error("expected 角色's 色 to be replaced")
		}
		// Non-dictionary text preserved
		if !strings.Contains(got, "他是") || !strings.Contains(got, "扮演重要") {
			t.Error("expected non-dictionary text to be preserved")
		}
	})

	t.Run("no match leaves SSML unchanged", func(t *testing.T) {
		input := `<speak><prosody rate="90%">今天天氣很好</prosody></speak>`
		got := p.Process(input)
		if got != input {
			t.Errorf("expected unchanged, got: %s", got)
		}
	})

	t.Run("longer word takes priority over shorter subword", func(t *testing.T) {
		extDict := MapDict{
			"主角光環": {{Char: "主", IPA: "tʂu²¹⁴"}, {Char: "角", IPA: "tɕɥe³⁵"}, {Char: "光", IPA: "kwɑŋ⁵⁵"}, {Char: "環", IPA: "xwan³⁵"}},
			"主角":     {{Char: "主", IPA: "tʂu²¹⁴"}, {Char: "角", IPA: "tɕɥe³⁵"}},
		}
		p := NewOverrideProcessor(extDict)
		input := `<speak><prosody rate="90%">他是主角光環</prosody></speak>`
		got := p.Process(input)
		// "主角光環" should be wrapped as a single 4-char unit, not "主角" + "光環"
		// Verify 主 appears only once (not double-replaced)
		// The output should have one <phoneme> for 主 (from 主角光環), not two (主角 + 主角光環)
		// Let's verify: count occurrences of 主 between phoneme tags
		mainPhoneme := `<phoneme alphabet="ipa" ph="tʂu²¹⁴">主</phoneme>`
		if strings.Count(got, mainPhoneme) != 1 {
			t.Errorf("expected 1 occurrence of 主 phoneme (not double-replaced), got %d: %s",
				strings.Count(got, mainPhoneme), got)
		}
	})

	t.Run("empty dictionary is no-op", func(t *testing.T) {
		p := NewOverrideProcessor(MapDict{})
		input := `<speak><prosody rate="90%">主角</prosody></speak>`
		got := p.Process(input)
		if got != input {
			t.Errorf("expected unchanged with empty dict, got: %s", got)
		}
	})

	t.Run("same word appears multiple times", func(t *testing.T) {
		input := `<speak><prosody rate="90%">主角來了，主角走了</prosody></speak>`
		got := p.Process(input)
		if strings.Count(got, `<phoneme alphabet="ipa" ph="tɕɥe³⁵">角</phoneme>`) != 2 {
			t.Errorf("expected 2 occurrences of 角 phoneme, got: %s", got)
		}
	})
}
