// Package chunk splits memory content into the passages that get embedded.
//
// Embedding models work best on passages of a few hundred tokens: a long
// transcript embedded whole averages away its subjects. Passages are built
// from whole sentences when possible, with a small overlap so an idea that
// spans a boundary is still found.
package chunk

import (
	"strings"
	"unicode"
)

const (
	// MaxWords is the target passage size (~260 tokens in Portuguese)
	MaxWords = 200
	// OverlapWords is how much of the previous passage a passage repeats
	OverlapWords = 40
)

// Split returns the passages of content. When segments are given (the host
// split the content by subject), each becomes a passage, and only segments
// longer than MaxWords are split further. Otherwise content is split by size.
func Split(content string, segments []string) []string {
	var passages []string
	if len(segments) > 0 {
		for _, s := range segments {
			passages = append(passages, bySize(s)...)
		}
	}
	if len(passages) == 0 {
		passages = bySize(content)
	}
	return passages
}

// bySize splits text into passages of at most MaxWords words, packing whole
// sentences and repeating up to OverlapWords words of the previous passage.
func bySize(text string) []string {
	text = strings.TrimSpace(text)
	if text == "" {
		return nil
	}
	if len(strings.Fields(text)) <= MaxWords {
		return []string{text}
	}

	var passages []string
	var current []string // words of the passage being built
	fresh := 0           // words in current that aren't overlap from the previous passage
	add := func(words []string) {
		current = append(current, words...)
		fresh += len(words)
	}
	flush := func() {
		if fresh > 0 {
			passages = append(passages, strings.Join(current, " "))
			current, fresh = overlapTail(current), 0
		}
	}
	for _, sentence := range sentences(text) {
		words := strings.Fields(sentence)
		// A sentence that doesn't fit next to the overlap is split into
		// windows (e.g. unpunctuated automatic transcripts)
		if len(words) > MaxWords-OverlapWords {
			flush()
			for len(words) > 0 {
				n := min(MaxWords-len(current), len(words))
				add(words[:n])
				words = words[n:]
				if len(current) >= MaxWords {
					flush()
				}
			}
			continue
		}
		if len(current)+len(words) > MaxWords {
			flush()
		}
		add(words)
	}
	flush()
	return passages
}

// overlapTail returns the last OverlapWords words of a passage, to start the
// next one
func overlapTail(words []string) []string {
	if len(words) <= OverlapWords {
		return append([]string(nil), words...)
	}
	return append([]string(nil), words[len(words)-OverlapWords:]...)
}

// sentences splits text after ., !, ?, … (followed by a space) and at line
// breaks. Text without punctuation comes back as a single sentence.
func sentences(text string) []string {
	var out []string
	start := 0
	runes := []rune(text)
	for i, r := range runes {
		end := false
		switch {
		case r == '\n':
			end = true
		case strings.ContainsRune(".!?…", r):
			end = i+1 == len(runes) || unicode.IsSpace(runes[i+1])
		}
		if end {
			if s := strings.TrimSpace(string(runes[start : i+1])); s != "" {
				out = append(out, s)
			}
			start = i + 1
		}
	}
	if s := strings.TrimSpace(string(runes[start:])); s != "" {
		out = append(out, s)
	}
	return out
}
