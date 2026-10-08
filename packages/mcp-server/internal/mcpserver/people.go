package mcpserver

import (
	"database/sql"
	"errors"
	"fmt"
	"strings"

	"github.com/second-brain/second-brain/packages/mcp-server/internal/store/sqlite"
)

// errAmbiguousPerson: a partial name matches several people
var errAmbiguousPerson = errors.New("ambiguous name")

// resolvePerson finds the person a lookup tool (recall, get_team_context,
// rename_person) is asked about: by full name ignoring case and accents, or
// by part of it ("Natiele" for "Natiele Bastião da Silva") when only one
// person matches. The error says why no one was chosen: not found (it wraps
// sql.ErrNoRows) or several matches, listed so the host can ask which one.
func resolvePerson(st *sqlite.Store, name string) (id, storedName string, err error) {
	id, storedName, err = st.FindPerson(name)
	if !errors.Is(err, sql.ErrNoRows) {
		return id, storedName, err
	}
	matches, err := st.MatchPeople(name)
	if err != nil {
		return "", "", err
	}
	switch len(matches) {
	case 0:
		return "", "", fmt.Errorf("person %q not found (see list_people): %w", name, sql.ErrNoRows)
	case 1:
		return matches[0].ID, matches[0].Name, nil
	}
	return "", "", fmt.Errorf("%q matches several people: %s; ask which one and use the full name (%w)", name, quotedNames(matches), errAmbiguousPerson)
}

// lookupMiss turns a resolvePerson error into the tool's answer: a text the
// host reads when no one or several people match, an error otherwise
func lookupMiss(err error) (*ToolResult, error) {
	if errors.Is(err, sql.ErrNoRows) || errors.Is(err, errAmbiguousPerson) {
		return &ToolResult{Content: []ContentBlock{{Type: "text", Text: err.Error()}}}, nil
	}
	return nil, err
}

// quotedNames lists people's names, quoted
func quotedNames(people []sqlite.Person) string {
	names := make([]string, len(people))
	for i, p := range people {
		names[i] = fmt.Sprintf("%q", p.Name)
	}
	return strings.Join(names, ", ")
}
