package pronunciation

import (
	"fmt"
	"strings"
)

// Processor applies pronunciation overrides to SSML text.
// It replaces dictionary words with SSML <phoneme> tags for correct TTS reading.
type Processor interface {
	Process(ssml string) string
}

// OverrideProcessor replaces known words in SSML text with phoneme-annotated tags.
type OverrideProcessor struct {
	dict Dictionary
	keys []string
}

// NewOverrideProcessor creates a processor from a dictionary.
func NewOverrideProcessor(dict Dictionary) *OverrideProcessor {
	p := &OverrideProcessor{dict: dict}
	if md, ok := dict.(MapDict); ok {
		p.keys = md.SortedKeys()
	}
	return p
}

// Process replaces dictionary word occurrences in the SSML string with
// <phoneme alphabet="ipa" ph="...">char</phoneme> tags, longest word first.
func (p *OverrideProcessor) Process(ssml string) string {
	if len(p.keys) == 0 {
		return ssml
	}
	result := ssml
	for _, word := range p.keys {
		entry, ok := p.dict.Lookup(word)
		if !ok {
			continue
		}
		result = strings.ReplaceAll(result, word, entry.SSML())
	}
	return result
}

// SSML returns the entry as a string of <phoneme> tags.
func (e Entry) SSML() string {
	var b strings.Builder
	for _, ph := range e {
		b.WriteString(fmt.Sprintf(`<phoneme alphabet="ipa" ph="%s">%s</phoneme>`, ph.IPA, ph.Char))
	}
	return b.String()
}

// NoopProcessor returns the SSML unchanged.
type NoopProcessor struct{}

func (NoopProcessor) Process(ssml string) string { return ssml }
