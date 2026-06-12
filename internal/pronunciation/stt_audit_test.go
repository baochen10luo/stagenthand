package pronunciation

import (
	"testing"
)

func TestFindCharMismatches_ExactMatch(t *testing.T) {
	orig := "保羅一直以為自己出生在一個再平凡不過的家庭"
	trans := "保羅一直以為自己出生在一個再平凡不過的家庭"
	mm := findCharMismatches(orig, trans)
	if len(mm) != 0 {
		t.Errorf("expected 0 mismatches, got %d: %v", len(mm), mm)
	}
}

func TestFindCharMismatches_OneSubstitution(t *testing.T) {
	orig := "保羅一直"
	trans := "保鑼一直"
	mm := findCharMismatches(orig, trans)
	if len(mm) != 1 {
		t.Fatalf("expected 1 mismatch, got %d: %v", len(mm), mm)
	}
	if mm[0].Expected != "羅" {
		t.Errorf("expected '羅', got '%s'", mm[0].Expected)
	}
	if mm[0].Got != "鑼" {
		t.Errorf("expected '鑼', got '%s'", mm[0].Got)
	}
}

func TestFindCharMismatches_SubstitutionWithInsertion(t *testing.T) {
	// TTS says 哥 instead of 公, STT also adds extra char
	orig := "阿公在家"
	trans := "阿哥在家了"
	mm := findCharMismatches(orig, trans)
	if len(mm) == 0 {
		t.Fatal("expected at least 1 mismatch (公→哥)")
	}
	found := false
	for _, m := range mm {
		if m.Expected == "公" && m.Got == "哥" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected mismatch 公→哥, got: %v", mm)
	}
}

func TestFindCharMismatches_Deletion(t *testing.T) {
	// STT missed a character
	orig := "溫文儒雅"
	trans := "溫文雅"
	mm := findCharMismatches(orig, trans)
	if len(mm) == 0 {
		t.Fatal("expected at least 1 mismatch")
	}
	if mm[0].Got != "(missing)" {
		t.Logf("Expected deletion, got: %+v", mm[0])
	}
}

func TestFindCharMismatches_PunctuationIgnored(t *testing.T) {
	orig := "你知道嗎？我們老家"
	trans := "你知道嗎我們老家"
	mm := findCharMismatches(orig, trans)
	if len(mm) != 0 {
		t.Errorf("expected 0 mismatches (punctuation ignored), got %d: %v", len(mm), mm)
	}
}

func TestFindCharMismatches_EnglishLetters(t *testing.T) {
	orig := "Paul的故事"
	trans := "Paul的故事"
	mm := findCharMismatches(orig, trans)
	if len(mm) != 0 {
		t.Errorf("expected 0 mismatches for English letters, got %d: %v", len(mm), mm)
	}
}

func TestAudit_Pass(t *testing.T) {
	auditor := NewSTTAuditor(STTAuditConfig{})
	result := auditor.Audit("保羅一直以為", "保羅一直以為", 0, 1, 1)
	if !result.Pass {
		t.Errorf("expected pass, got mismatches: %v", result.Mismatches)
	}
}

func TestAudit_Fail(t *testing.T) {
	auditor := NewSTTAuditor(STTAuditConfig{})
	result := auditor.Audit("阿公", "阿哥", 0, 1, 1)
	if result.Pass {
		t.Fatal("expected fail")
	}
	if len(result.Mismatches) != 1 {
		t.Fatalf("expected 1 mismatch, got %d", len(result.Mismatches))
	}
	mm := result.Mismatches[0]
	if mm.Expected != "公" {
		t.Errorf("expected '公', got '%s'", mm.Expected)
	}
	if mm.Got != "哥" {
		t.Errorf("expected '哥', got '%s'", mm.Got)
	}
	if len(mm.HomophoneSuggest) == 0 {
		t.Error("expected homophone suggestions for '公'")
	}
}

func TestBuildCorrectedInput(t *testing.T) {
	mm := []CharMismatch{
		{Position: 0, Expected: "公", Got: "哥"},
	}
	homophones := HomophoneMap{
		"公": {"工", "功"},
	}
	corrected, replacements := BuildCorrectedInput("公保羅", mm, homophones)
	if corrected != "工保羅" {
		t.Errorf("expected '工保羅', got '%s'", corrected)
	}
	if len(replacements) != 1 {
		t.Errorf("expected 1 replacement, got %d", len(replacements))
	}
}

func TestHomophoneLookup(t *testing.T) {
	h := DefaultHomophones()
	if sugg := h.Lookup("公"); len(sugg) == 0 {
		t.Error("expected suggestions for '公'")
	} else {
		t.Logf("公 → %v", sugg)
	}
	if sugg := h.Lookup("角"); len(sugg) == 0 {
		t.Error("expected suggestions for '角'")
	} else {
		t.Logf("角 → %v", sugg)
	}
	if h.Has("不存在") {
		t.Error("'不存在' should not be in homophone map")
	}
}
