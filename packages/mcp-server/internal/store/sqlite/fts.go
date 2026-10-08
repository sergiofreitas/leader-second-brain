package sqlite

import (
	"strings"
	"unicode"
)

// ============================================================
// FTS5 keyword search
//
// User text is never passed to MATCH as is: FTS5 has its own syntax
// ("1:1" is a column filter, a quote or a parenthesis is a syntax error).
// Queries are rebuilt from words, each quoted, joined with OR and ranked by
// BM25, so memories matching more (and rarer) terms come first.
//
// FTS5 doesn't stem Portuguese: "microgestão" doesn't match
// "microgerenciando". The MCP host fills that gap by sending extra terms —
// synonyms, inflections, prefixes ("microger*") and related expressions.
// ============================================================

// stopwords are Portuguese and English words too common to search for
var stopwords = map[string]bool{}

func init() {
	for _, w := range strings.Fields(`
		a o as os um uma uns umas e é de do da dos das em no na nos nas
		por pelo pela pelos pelas para pra com sem sobre que se ao aos à às
		ele ela eles elas eu tu você vocês nós me te lhe isso isto esse essa
		este esta aquele aquela seu sua seus suas meu minha nosso nossa
		mais muito como quando onde qual quais quem já não sim ou mas também
		foi ser está estão tem têm há
		the of and or to in on at for with is are was be it this that an`) {
		stopwords[w] = true
	}
}

// words splits text into lowercase words (letters and digits)
func words(text string) []string {
	return strings.FieldsFunc(strings.ToLower(text), func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r)
	})
}

// BuildFTSQuery returns an FTS5 query matching any of the significant words
// of query and any of terms. A term ending in * matches by prefix; a term of
// several words matches as a phrase. Returns "" when nothing is searchable.
func BuildFTSQuery(query string, terms []string) string {
	var parts []string
	seen := map[string]bool{}
	add := func(part string) {
		if !seen[part] {
			seen[part] = true
			parts = append(parts, part)
		}
	}
	for _, w := range words(query) {
		if !stopwords[w] {
			add(`"` + w + `"`)
		}
	}
	for _, term := range terms {
		term = strings.TrimSpace(term)
		prefix := strings.HasSuffix(term, "*")
		ws := words(term)
		if len(ws) == 0 || (len(ws) == 1 && stopwords[ws[0]] && !prefix) {
			continue
		}
		part := `"` + strings.Join(ws, " ") + `"`
		if prefix {
			part += "*"
		}
		add(part)
	}
	return strings.Join(parts, " OR ")
}

// SearchFTS searches memories by keyword: the significant words of query,
// plus extra terms (synonyms, inflections, prefixes like "deleg*"). Results
// come best first, with a snippet around the match instead of the content.
func (s *Store) SearchFTS(query string, terms []string, limit int) ([]map[string]interface{}, error) {
	match := BuildFTSQuery(query, terms)
	if match == "" || limit <= 0 {
		return nil, nil
	}
	rows, err := s.q.Query(
		`SELECT m.id, m.type, COALESCE(m.about_person, ''), m.created_at,
			snippet(memories_fts, 0, '<mark>', '</mark>', '...', 32) AS snippet,
			bm25(memories_fts) AS rank
		 FROM memories_fts
		 JOIN memories m ON m.rowid = memories_fts.rowid
		 WHERE memories_fts MATCH ?
		 ORDER BY rank
		 LIMIT ?`,
		match, limit,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var results []map[string]interface{}
	for rows.Next() {
		var id, memType, aboutPerson, createdAt, snippet string
		var rank float64
		if err := rows.Scan(&id, &memType, &aboutPerson, &createdAt, &snippet, &rank); err != nil {
			return nil, err
		}
		results = append(results, map[string]interface{}{
			"memory_id": id, "type": memType,
			"about_person": aboutPerson, "created_at": createdAt,
			"snippet": snippet, "rank": rank,
		})
	}
	return results, rows.Err()
}
