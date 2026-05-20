package pronunciation

import (
	"testing"
)

func TestMapDict_Lookup(t *testing.T) {
	d := MapDict{
		"角色": {{Char: "角", IPA: "tɕɥe³⁵"}, {Char: "色", IPA: "sɤ⁵¹"}},
		"主角": {{Char: "主", IPA: "tʂu²¹⁴"}, {Char: "角", IPA: "tɕɥe³⁵"}},
	}

	t.Run("exact match", func(t *testing.T) {
		entry, ok := d.Lookup("角色")
		if !ok {
			t.Fatal("expected to find 角色")
		}
		if len(entry) != 2 {
			t.Fatalf("expected 2 phonemes, got %d", len(entry))
		}
		if entry[0].Char != "角" || entry[0].IPA != "tɕɥe³⁵" {
			t.Errorf("first phoneme: got %+v", entry[0])
		}
		if entry[1].Char != "色" || entry[1].IPA != "sɤ⁵¹" {
			t.Errorf("second phoneme: got %+v", entry[1])
		}
	})

	t.Run("another exact match", func(t *testing.T) {
		entry, ok := d.Lookup("主角")
		if !ok {
			t.Fatal("expected to find 主角")
		}
		if entry[0].IPA != "tʂu²¹⁴" {
			t.Errorf("expected 主 IPA, got %q", entry[0].IPA)
		}
	})

	t.Run("no match", func(t *testing.T) {
		_, ok := d.Lookup("提供")
		if ok {
			t.Error("expected no match for 提供")
		}
	})

	t.Run("substring no match", func(t *testing.T) {
		_, ok := d.Lookup("角")
		if ok {
			t.Error("expected no match for 角 alone (only in compound)")
		}
	})

	t.Run("empty string", func(t *testing.T) {
		_, ok := d.Lookup("")
		if ok {
			t.Error("expected no match for empty string")
		}
	})
}

func TestMapDict_Add(t *testing.T) {
	d := make(MapDict)
	d.Add("提供", Phoneme{Char: "提", IPA: "tʰi³⁵"}, Phoneme{Char: "供", IPA: "kʊŋ⁵⁵"})

	entry, ok := d.Lookup("提供")
	if !ok {
		t.Fatal("expected to find 提供 after Add")
	}
	if len(entry) != 2 {
		t.Fatalf("expected 2 phonemes, got %d", len(entry))
	}
	if entry[1].Char != "供" || entry[1].IPA != "kʊŋ⁵⁵" {
		t.Errorf("second phoneme: got %+v", entry[1])
	}
}

func TestMapDict_SortedKeys(t *testing.T) {
	d := MapDict{
		"角色": {{Char: "角", IPA: "tɕɥe³⁵"}},
		"主角": {{Char: "主", IPA: "tʂu²¹⁴"}},
		"成為": {{Char: "成", IPA: "tʂʰəŋ³⁵"}},
	}

	keys := d.SortedKeys()
	if len(keys) != 3 {
		t.Fatalf("expected 3 keys, got %d", len(keys))
	}
	// Must be descending length: 成為(2) == 主角(2) == 角色(2), tie-break lexicographic
	for i := 1; i < len(keys); i++ {
		if len(keys[i-1]) < len(keys[i]) {
			t.Errorf("keys not sorted by desc length: %q (%d) before %q (%d)",
				keys[i-1], len(keys[i-1]), keys[i], len(keys[i]))
		}
	}
}

func TestDefaultDictionary(t *testing.T) {
	d := DefaultDictionary()
	if d == nil {
		t.Fatal("DefaultDictionary() returned nil")
	}
	// Should have at least a few common entries
	entries := []string{"角色", "主角", "提供", "因為"}
	found := 0
	for _, w := range entries {
		if _, ok := d.Lookup(w); ok {
			found++
			t.Logf("found default entry: %s", w)
		}
	}
	if found == 0 {
		t.Error("DefaultDictionary should contain at least some common 破音字")
	}
}
