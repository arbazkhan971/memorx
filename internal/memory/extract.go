package memory

import (
	"regexp"
	"strings"
	"unicode"
)

// ExtractedItem represents a single piece of information extracted from conversation text.
type ExtractedItem struct {
	Content string
	Type    string // decision, blocker, progress, fact, next_step
	Fact    *FactTriple
}

// FactTriple represents a subject-predicate-object triple extracted from text.
type FactTriple struct {
	Subject   string
	Predicate string
	Object    string
}

// sentenceEnd matches sentence boundaries (period, exclamation, question mark followed by space or end).
var sentenceEnd = regexp.MustCompile(`[.!?](?:\s|$)`)

// splitSentences splits text into sentences, handling common abbreviations.
func splitSentences(text string) []string {
	// Normalize newlines into sentence boundaries.
	text = strings.ReplaceAll(text, "\n", ". ")
	// Collapse multiple dots/spaces.
	text = regexp.MustCompile(`\.(\s*\.)+`).ReplaceAllString(text, ".")
	text = regexp.MustCompile(`\s+`).ReplaceAllString(text, " ")
	text = strings.TrimSpace(text)
	if text == "" {
		return nil
	}

	indices := sentenceEnd.FindAllStringIndex(text, -1)
	if len(indices) == 0 {
		s := strings.TrimSpace(text)
		if s != "" {
			return []string{s}
		}
		return nil
	}

	var sentences []string
	prev := 0
	for _, idx := range indices {
		s := strings.TrimSpace(text[prev:idx[1]])
		if s != "" {
			sentences = append(sentences, s)
		}
		prev = idx[1]
	}
	// Remaining text after last sentence end.
	if prev < len(text) {
		s := strings.TrimSpace(text[prev:])
		if s != "" {
			sentences = append(sentences, s)
		}
	}
	return sentences
}

// keyword lists for classification
var (
	decisionKeywords = []string{"decided", "chose", "selected", "agreed", "went with"}
	blockerKeywords  = []string{"blocked", "stuck", "can't", "cannot", "waiting on", "depends on"}
	progressKeywords = []string{"done", "completed", "finished", "implemented", "shipped"}
	nextStepKeywords = []string{"next", "todo", "need to", "should", "will"}
	factPredicates   = []string{"uses", "is", "runs on", "built with"}
)

// containsKeyword returns the first keyword found in the lowercased sentence, or "".
func containsKeyword(lower string, keywords []string) string {
	for _, kw := range keywords {
		if strings.Contains(lower, kw) {
			return kw
		}
	}
	return ""
}

// extractFact attempts to extract a subject-predicate-object triple from a sentence
// that contains one of the fact predicates.
func extractFact(sentence, lower string) *FactTriple {
	for _, pred := range factPredicates {
		idx := strings.Index(lower, pred)
		if idx < 0 {
			continue
		}
		subject := strings.TrimSpace(sentence[:idx])
		object := strings.TrimSpace(sentence[idx+len(pred):])

		// Clean up trailing punctuation from object.
		object = strings.TrimRight(object, ".!?,;:")
		object = strings.TrimSpace(object)

		// Take last meaningful word(s) as subject if it's a long phrase.
		subject = lastNounPhrase(subject)
		if subject == "" || object == "" {
			continue
		}
		return &FactTriple{
			Subject:   subject,
			Predicate: pred,
			Object:    object,
		}
	}
	return nil
}

// lastNounPhrase returns the last meaningful noun phrase from a string.
// For short strings, returns as-is. For longer ones, takes the last few words.
func lastNounPhrase(s string) string {
	s = strings.TrimRight(s, ".!?,;: ")
	words := strings.Fields(s)
	if len(words) == 0 {
		return ""
	}
	// Skip leading filler words.
	start := 0
	fillers := map[string]bool{"the": true, "a": true, "an": true, "our": true, "my": true, "this": true, "that": true, "we": true, "i": true, "it": true}
	for start < len(words) && fillers[strings.ToLower(words[start])] {
		start++
	}
	if start >= len(words) {
		// All filler, return last word.
		return words[len(words)-1]
	}
	words = words[start:]
	// Keep at most 4 words.
	if len(words) > 4 {
		words = words[len(words)-4:]
	}
	return strings.Join(words, " ")
}

// classify determines the type of an extracted item from a sentence.
// Returns the type and whether the sentence was classified.
func classify(sentence string) (string, bool) {
	lower := strings.ToLower(sentence)

	// Skip very short sentences (less than 3 non-space characters).
	trimmed := strings.Map(func(r rune) rune {
		if unicode.IsSpace(r) {
			return -1
		}
		return r
	}, sentence)
	if len(trimmed) < 3 {
		return "", false
	}

	if containsKeyword(lower, decisionKeywords) != "" {
		return "decision", true
	}
	if containsKeyword(lower, blockerKeywords) != "" {
		return "blocker", true
	}
	if containsKeyword(lower, progressKeywords) != "" {
		return "progress", true
	}
	// Check for fact before next_step since "is" is very common.
	// Only classify as fact if we can actually extract a triple.
	if ft := extractFact(sentence, lower); ft != nil {
		return "fact", true
	}
	if containsKeyword(lower, nextStepKeywords) != "" {
		return "next_step", true
	}
	return "", false
}

// ExtractFromText extracts decisions, facts, progress updates, blockers, and next steps
// from a block of conversation text using heuristic keyword matching.
func ExtractFromText(text string) []ExtractedItem {
	sentences := splitSentences(text)
	var items []ExtractedItem

	for _, sent := range sentences {
		itemType, ok := classify(sent)
		if !ok {
			continue
		}

		item := ExtractedItem{
			Content: sent,
			Type:    itemType,
		}

		if itemType == "fact" {
			lower := strings.ToLower(sent)
			item.Fact = extractFact(sent, lower)
		}

		items = append(items, item)
	}

	return items
}
