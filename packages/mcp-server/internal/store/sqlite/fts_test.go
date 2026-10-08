package sqlite

import (
	"testing"
)

func TestBuildFTSQuery(t *testing.T) {
	cases := []struct {
		query string
		terms []string
		want  string
	}{
		{"como anda a delegação dele?", nil, `"anda" OR "delegação" OR "dele"`},
		{"1:1 com o Sérgio", nil, `"1" OR "sérgio"`},
		{`feedback "construtivo" (urgente) NOT`, nil, `"feedback" OR "construtivo" OR "urgente" OR "not"`},
		{"microgestão", []string{"microger*", " não delega ", "deleg*", "microgestão", "de", ""},
			`"microgestão" OR "microger"* OR "não delega" OR "deleg"*`},
		{"o que é isso?", nil, ""},
		{"", []string{"a*"}, `"a"*`},
	}
	for _, c := range cases {
		if got := BuildFTSQuery(c.query, c.terms); got != c.want {
			t.Errorf("BuildFTSQuery(%q, %q) = %s, want %s", c.query, c.terms, got, c.want)
		}
	}
}

func TestSearchFTS(t *testing.T) {
	s, err := New(t.TempDir() + "/fts.db")
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer s.Close()
	memories := map[string]string{
		"m_micro": "O Sérgio anda microgerenciando as tarefas técnicas do time.",
		"m_deleg": "Ele não delega as revisões de código; centraliza tudo.",
		"m_1on1":  "Na 1:1 combinamos revisar o PDI (trimestral).",
		"m_other": "O Evandro vai tirar férias em dezembro.",
	}
	for id, content := range memories {
		if err := s.InsertMemory(id, "observation", content, "text", "test", "", "", 1); err != nil {
			t.Fatalf("insert: %v", err)
		}
	}
	ids := func(results []map[string]interface{}) map[string]bool {
		out := map[string]bool{}
		for _, r := range results {
			out[r["memory_id"].(string)] = true
		}
		return out
	}

	// The word alone doesn't match the inflected form; the host's terms do
	if got, _ := s.SearchFTS("microgestão", nil, 10); len(got) != 0 {
		t.Errorf("microgestão alone = %v, want no match (no stemming)", ids(got))
	}
	got, err := s.SearchFTS("microgestão", []string{"microger*", "não delega", "centraliza"}, 10)
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if found := ids(got); len(found) != 2 || !found["m_micro"] || !found["m_deleg"] {
		t.Errorf("microgestão with terms = %v, want m_micro and m_deleg", found)
	}
	if _, ok := got[0]["content"]; ok {
		t.Error("keyword results include the whole content")
	}
	if snippet, _ := got[0]["snippet"].(string); snippet == "" {
		t.Error("keyword result without a snippet")
	}

	// Accents don't matter (FTS5's unicode61 tokenizer folds them)
	if found := ids(must(s.SearchFTS("revisoes", nil, 10))); !found["m_deleg"] {
		t.Errorf("revisoes = %v, want m_deleg", found)
	}
	// FTS5 syntax in user text is searched literally instead of failing
	for _, q := range []string{"1:1", `"PDI`, "(trimestral", "PDI AND", "NEAR(x y)", "*"} {
		if _, err := s.SearchFTS(q, nil, 10); err != nil {
			t.Errorf("SearchFTS(%q) failed: %v", q, err)
		}
	}
	if found := ids(must(s.SearchFTS("1:1", nil, 10))); !found["m_1on1"] {
		t.Errorf("1:1 = %v, want m_1on1", found)
	}
}

func must(results []map[string]interface{}, err error) []map[string]interface{} {
	if err != nil {
		panic(err)
	}
	return results
}
