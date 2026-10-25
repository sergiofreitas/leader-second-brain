package mcpserver

import (
	"fmt"
	"strings"

	"github.com/second-brain/second-brain/packages/mcp-server/internal/config"
	"github.com/second-brain/second-brain/packages/mcp-server/internal/providers"
)

// normalizeAssessment validates a structured assessment against the
// configured method: who it is about, the criteria (by id or name, when the
// playbook lists them, then replaced by their ids) and the scale.
func (s *Server) normalizeAssessment(ex *providers.EntityExtraction) error {
	a := ex.Assessment
	if a == nil {
		return nil
	}
	method := s.cfg.Assessment
	a.Name, a.Cycle = strings.TrimSpace(a.Name), strings.TrimSpace(a.Cycle)
	if ex.AboutPerson == "" {
		return fmt.Errorf("assessment: about_person is required (who was assessed)")
	}
	if len(a.Scores) == 0 {
		return fmt.Errorf("assessment: scores are required, one per criterion")
	}
	seen := map[string]bool{}
	for i := range a.Scores {
		sc := &a.Scores[i]
		sc.Criterion, sc.Evidence = strings.TrimSpace(sc.Criterion), strings.TrimSpace(sc.Evidence)
		if sc.Criterion == "" {
			return fmt.Errorf("assessment score %d: criterion is required", i+1)
		}
		if len(method.Criteria) > 0 {
			c, ok := findCriterion(method.Criteria, sc.Criterion)
			if !ok {
				return fmt.Errorf("assessment score %d: unknown criterion %q (valid: %s; see get_playbook)", i+1, sc.Criterion, criterionIDs(method.Criteria))
			}
			sc.Criterion = c.ID
		}
		if id := config.CriterionID(sc.Criterion); seen[id] {
			return fmt.Errorf("assessment score %d: criterion %q is scored twice", i+1, sc.Criterion)
		} else {
			seen[id] = true
		}
		if sc.Leader == nil && sc.Self == nil {
			return fmt.Errorf("assessment score %d (%s): pass leader, self or both", i+1, sc.Criterion)
		}
		for who, v := range map[string]*int{"leader": sc.Leader, "self": sc.Self} {
			if v != nil && (*v < method.ScaleMin || *v > method.ScaleMax) {
				return fmt.Errorf("assessment score %d (%s): %s score %d is outside the scale %d-%d", i+1, sc.Criterion, who, *v, method.ScaleMin, method.ScaleMax)
			}
		}
	}
	if ex.MemoryType == "observation" {
		ex.MemoryType = "assessment" // the default type, for a memory that is an assessment
	}
	return nil
}

// assessmentProps are the props of an Assessment node: the scores by
// criterion, the averages and the divergences of the configured threshold
// or more between the leader's and the person's own score
func (s *Server) assessmentProps(a *providers.ExtractedAssessment) map[string]interface{} {
	method := s.cfg.Assessment
	name := a.Name
	if name == "" {
		name = method.Name
	}
	scores := make([]interface{}, 0, len(a.Scores))
	divergences := []interface{}{}
	var leaderSum, selfSum, leaderN, selfN int
	for _, sc := range a.Scores {
		entry := map[string]interface{}{"criterion_id": config.CriterionID(sc.Criterion), "criterion": sc.Criterion}
		if c, ok := findCriterion(method.Criteria, sc.Criterion); ok {
			entry["criterion_id"] = c.ID
			entry["criterion"] = c.Name
			if c.Group != "" {
				entry["group"] = c.Group
			}
		}
		if sc.Leader != nil {
			entry["leader"] = *sc.Leader
			leaderSum, leaderN = leaderSum+*sc.Leader, leaderN+1
		}
		if sc.Self != nil {
			entry["self"] = *sc.Self
			selfSum, selfN = selfSum+*sc.Self, selfN+1
		}
		if sc.Evidence != "" {
			entry["evidence"] = sc.Evidence
		}
		scores = append(scores, entry)
		if sc.Leader != nil && sc.Self != nil {
			if gap := *sc.Leader - *sc.Self; gap >= method.DivergenceThreshold || -gap >= method.DivergenceThreshold {
				divergences = append(divergences, map[string]interface{}{
					"criterion": entry["criterion"], "leader": *sc.Leader, "self": *sc.Self, "gap": gap,
				})
			}
		}
	}
	props := map[string]interface{}{
		"scale":       fmt.Sprintf("%d-%d", method.ScaleMin, method.ScaleMax),
		"visibility":  method.Visibility,
		"scores":      scores,
		"divergences": divergences,
	}
	if name != "" {
		props["name"] = name
	}
	if a.Cycle != "" {
		props["cycle"] = a.Cycle
	}
	averages := map[string]interface{}{}
	if leaderN > 0 {
		averages["leader"] = round1(float64(leaderSum) / float64(leaderN))
	}
	if selfN > 0 {
		averages["self"] = round1(float64(selfSum) / float64(selfN))
	}
	props["averages"] = averages
	return props
}

// findCriterion finds a criterion by id or by name, ignoring case and accents
func findCriterion(criteria []config.Criterion, key string) (config.Criterion, bool) {
	id := config.CriterionID(key)
	for _, c := range criteria {
		if c.ID == key || c.ID == id || config.CriterionID(c.Name) == id {
			return c, true
		}
	}
	return config.Criterion{}, false
}

func criterionIDs(criteria []config.Criterion) string {
	ids := make([]string, len(criteria))
	for i, c := range criteria {
		ids[i] = c.ID
	}
	return strings.Join(ids, ", ")
}

func round1(x float64) float64 {
	return float64(int(x*10+0.5)) / 10
}
