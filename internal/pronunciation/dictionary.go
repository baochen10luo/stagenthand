package pronunciation

import "sort"

// Phoneme represents the pronunciation of a single character.
type Phoneme struct {
	Char string // the Chinese character, e.g. "角"
	IPA  string // IPA pronunciation, e.g. "tɕɥe³⁵"
}

// Entry is a sequence of phonemes for a multi-character word.
type Entry []Phoneme

// Dictionary provides lookup of pronunciation overrides for known words.
type Dictionary interface {
	Lookup(word string) (Entry, bool)
}

// MutableDictionary extends Dictionary with an Add method.
type MutableDictionary interface {
	Dictionary
	Add(word string, phonemes ...Phoneme)
}

// MapDict is a simple in-memory dictionary backed by a map.
type MapDict map[string]Entry

func (d MapDict) Lookup(word string) (Entry, bool) {
	e, ok := d[word]
	return e, ok
}

func (d MapDict) Add(word string, phonemes ...Phoneme) {
	d[word] = Entry(phonemes)
}

// SortedKeys returns dictionary keys sorted by descending length.
// Longer words first prevents partial replacement of substrings.
func (d MapDict) SortedKeys() []string {
	keys := make([]string, 0, len(d))
	for k := range d {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool {
		if len(keys[i]) != len(keys[j]) {
			return len(keys[i]) > len(keys[j])
		}
		return keys[i] < keys[j]
	})
	return keys
}

// DefaultDictionary returns a built-in dictionary of common Taiwanese Mandarin 破音字.
func DefaultDictionary() Dictionary {
	return MapDict{
		"角色": {{Char: "角", IPA: "tɕɥe³⁵"}, {Char: "色", IPA: "sɤ⁵¹"}},
		"主角": {{Char: "主", IPA: "tʂu²¹⁴"}, {Char: "角", IPA: "tɕɥe³⁵"}},
		"提供": {{Char: "提", IPA: "tʰi³⁵"}, {Char: "供", IPA: "kʊŋ⁵⁵"}},
		"因為": {{Char: "因", IPA: "in⁵⁵"}, {Char: "為", IPA: "wei⁵¹"}},
		"什麼": {{Char: "什", IPA: "ʂən³⁵"}, {Char: "麼", IPA: "mə"}},
		"瞭解": {{Char: "瞭", IPA: "ljɑʊ²¹⁴"}, {Char: "解", IPA: "tɕjɛ²¹⁴"}},
		"目的": {{Char: "目", IPA: "mu⁵¹"}, {Char: "的", IPA: "ti⁵¹"}},
		"音樂": {{Char: "音", IPA: "in⁵⁵"}, {Char: "樂", IPA: "ɥɛ⁵¹"}},
		"快樂": {{Char: "快", IPA: "kʰwai⁵¹"}, {Char: "樂", IPA: "lɤ⁵¹"}},
		"主角光環": {{Char: "主", IPA: "tʂu²¹⁴"}, {Char: "角", IPA: "tɕɥe³⁵"}, {Char: "光", IPA: "kwɑŋ⁵⁵"}, {Char: "環", IPA: "xwan³⁵"}},
	}
}
