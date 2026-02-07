package agent

import (
	"encoding/json"
	"fmt"
)

// ValidatorOutput represents the structured output from a validator agent.
type ValidatorOutput struct {
	Status string   `json:"status"` // "pass" or "fail"
	Notes  string   `json:"notes"`
	Issues []string `json:"issues,omitempty"`
}

// PlannerOutput represents the structured output from a planner agent.
type PlannerOutput struct {
	Tasks []PlannerTask `json:"tasks"`
}

// PlannerTask is a task as proposed by the planner.
type PlannerTask struct {
	ID            string   `json:"id"`
	Title         string   `json:"title"`
	Description   string   `json:"description"`
	Priority      int      `json:"priority"`
	CohesionGroup string   `json:"cohesion_group,omitempty"`
	Dependencies  []string `json:"dependencies"`
	FileLocks     []string `json:"file_locks"`
}

// ParseValidatorOutput parses structured validator output from JSON.
func ParseValidatorOutput(data []byte) (*ValidatorOutput, error) {
	var out ValidatorOutput
	if err := json.Unmarshal(data, &out); err != nil {
		return nil, fmt.Errorf("parse validator output: %w", err)
	}
	if out.Status != "pass" && out.Status != "fail" {
		return nil, fmt.Errorf("invalid validator status: %q (must be 'pass' or 'fail')", out.Status)
	}
	return &out, nil
}

// ParsePlannerOutput parses structured planner output from JSON.
func ParsePlannerOutput(data []byte) (*PlannerOutput, error) {
	var out PlannerOutput
	if err := json.Unmarshal(data, &out); err != nil {
		return nil, fmt.Errorf("parse planner output: %w", err)
	}
	if len(out.Tasks) == 0 {
		return nil, fmt.Errorf("planner produced no tasks")
	}
	return &out, nil
}
