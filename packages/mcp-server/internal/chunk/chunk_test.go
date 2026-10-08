package chunk

import (
	"fmt"
	"strings"
	"testing"
)

// words returns n distinct words starting at w<from>
func words(from, n int) string {
	ws := make([]string, n)
	for i := range ws {
		ws[i] = fmt.Sprintf("w%d", from+i)
	}
	return strings.Join(ws, " ")
}

func count(s string) int { return len(strings.Fields(s)) }

// checkCoverage asserts every passage fits MaxWords and that the passages,
// with overlaps removed, contain every word of text in order
func checkCoverage(t *testing.T, text string, passages []string) {
	t.Helper()
	all := strings.Fields(text)
	next := 0
	for i, p := range passages {
		ws := strings.Fields(p)
		if len(ws) > MaxWords {
			t.Errorf("passage %d has %d words, max %d", i, len(ws), MaxWords)
		}
		// Words already covered (the overlap) don't advance; new ones must
		// continue the text in order
		for _, w := range ws {
			if next < len(all) && w == all[next] {
				next++
			}
		}
	}
	if next != len(all) {
		t.Errorf("passages cover %d of %d words", next, len(all))
	}
}

func TestShortContentIsOnePassage(t *testing.T) {
	got := Split("  Evandro resolveu sozinho um bug de TEF.  ", nil)
	if len(got) != 1 || got[0] != "Evandro resolveu sozinho um bug de TEF." {
		t.Errorf("Split = %q", got)
	}
	if got := Split("   ", nil); len(got) != 0 {
		t.Errorf("Split of blank = %q, want none", got)
	}
}

func TestSentencesArePacked(t *testing.T) {
	// 10 sentences of 50 words: 4 fit in a passage, then overlap carries on
	var sb strings.Builder
	for i := 0; i < 10; i++ {
		sb.WriteString(words(i*50, 50) + ". ")
	}
	text := sb.String()
	got := Split(text, nil)
	checkCoverage(t, text, got)
	if len(got) < 3 {
		t.Fatalf("got %d passages, want at least 3", len(got))
	}
	for i, p := range got {
		if !strings.HasSuffix(p, ".") {
			t.Errorf("passage %d doesn't end at a sentence boundary: ...%q", i, p[len(p)-20:])
		}
	}
	// Each passage after the first starts with the end of the previous one
	for i := 1; i < len(got); i++ {
		prev := strings.Fields(got[i-1])
		first := strings.Fields(got[i])[0]
		if !strings.Contains(strings.Join(prev[len(prev)-OverlapWords:], " "), first) {
			t.Errorf("passage %d doesn't start with an overlap of passage %d", i, i-1)
		}
	}
}

func TestUnpunctuatedTranscriptUsesWindows(t *testing.T) {
	text := words(0, 1000) // an automatic transcript with no punctuation
	got := Split(text, nil)
	checkCoverage(t, text, got)
	// 200 words, then 160 new words per passage: ceil((1000-200)/160)+1 = 6
	if len(got) != 6 {
		t.Errorf("got %d passages, want 6", len(got))
	}
	for i := 1; i < len(got); i++ {
		prev := strings.Fields(got[i-1])
		cur := strings.Fields(got[i])
		if strings.Join(prev[len(prev)-OverlapWords:], " ") != strings.Join(cur[:OverlapWords], " ") {
			t.Errorf("passage %d doesn't overlap the previous one by %d words", i, OverlapWords)
		}
	}
	if last := got[len(got)-1]; !strings.HasSuffix(last, "w999") {
		t.Errorf("last passage doesn't end the text: ...%q", last[len(last)-10:])
	}
}

func TestLongSentenceAmongShortOnes(t *testing.T) {
	text := "Começamos pela pauta. " + words(0, 450) + ". Fechamos com os combinados."
	got := Split(text, nil)
	checkCoverage(t, text, got)
	if !strings.HasPrefix(got[0], "Começamos pela pauta.") {
		t.Errorf("first passage = %q...", got[0][:30])
	}
	if !strings.HasSuffix(got[len(got)-1], "Fechamos com os combinados.") {
		t.Errorf("last passage doesn't end the text")
	}
}

func TestHostSegments(t *testing.T) {
	long := words(0, 450)
	got := Split("ignored when segments are given", []string{" Pauta: entregas. ", "", long, "Combinados."})
	if got[0] != "Pauta: entregas." || got[len(got)-1] != "Combinados." {
		t.Errorf("segments not kept as passages: first %q, last %q", got[0], got[len(got)-1])
	}
	if n := len(got); n < 4 {
		t.Errorf("got %d passages, want the long segment split too", n)
	}
	// Blank segments fall back to the content
	if got := Split("conteúdo", []string{" ", ""}); len(got) != 1 || got[0] != "conteúdo" {
		t.Errorf("Split with blank segments = %q", got)
	}
}
