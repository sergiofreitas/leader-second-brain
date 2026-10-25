package mcpserver

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/second-brain/second-brain/packages/mcp-server/internal/config"
	"github.com/second-brain/second-brain/packages/mcp-server/internal/providers"
)

// PlaybookSections are the accepted values of the get_playbook "section"
// argument
var PlaybookSections = []string{"rites", "feedback", "assessment", "alerts"}

// HandleGetPlaybook processes a get_playbook tool call: the organization's
// method — how each rite (1:1, weekly, skip-level, feedback) is prepared,
// run and recorded, the feedback format, the assessment rubric and the alert
// thresholds — so skills follow it instead of a method of their own. A rite
// or a section narrows the answer.
func (s *Server) HandleGetPlaybook(ctx context.Context, args map[string]interface{}) (*ToolResult, error) {
	rite, _ := args["rite"].(string)
	section, _ := args["section"].(string)
	rite, section = strings.TrimSpace(rite), strings.ToLower(strings.TrimSpace(section))

	result := map[string]interface{}{"profile": s.cfg.Profile}
	if s.cfg.PlaybookSource != "" {
		result["playbook_file"] = s.cfg.PlaybookSource
	}
	if s.cfg.PlaybookProblem != "" {
		result["playbook_error"] = s.cfg.PlaybookProblem + " — using the profile's method until the file is fixed; tell the user"
	}

	if rite != "" {
		r, ok := s.cfg.Rites[rite]
		if !ok {
			return nil, fmt.Errorf("unknown rite %q (valid: %s)", rite, strings.Join(riteIDs(s.cfg.Rites), ", "))
		}
		result["rite"] = rite
		result["rites"] = map[string]config.Rite{rite: r}
		// What running the rite needs: feedback for a feedback, the
		// assessment when the rite discusses scores
		if rite == "feedback" {
			result["feedback"] = s.cfg.Feedback
		}
		result["assessment"] = s.cfg.Assessment
		return jsonResult(result)
	}

	switch section {
	case "":
		result["rites"] = s.cfg.Rites
		result["feedback"] = s.cfg.Feedback
		result["assessment"] = s.cfg.Assessment
		result["alerts"] = s.cfg.Alerts
		result["memory_types"] = providers.MemoryTypes
	case "rites":
		result["rites"] = s.cfg.Rites
	case "feedback":
		result["feedback"] = s.cfg.Feedback
	case "assessment":
		result["assessment"] = s.cfg.Assessment
	case "alerts":
		result["alerts"] = s.cfg.Alerts
	default:
		return nil, fmt.Errorf("invalid section %q (valid: %s)", section, strings.Join(PlaybookSections, ", "))
	}
	return jsonResult(result)
}

func riteIDs(rites map[string]config.Rite) []string {
	ids := make([]string, 0, len(rites))
	for id := range rites {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids
}

func jsonResult(v interface{}) (*ToolResult, error) {
	out, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return nil, err
	}
	return &ToolResult{Content: []ContentBlock{{Type: "text", Text: string(out)}}}, nil
}
