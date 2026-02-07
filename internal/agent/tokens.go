package agent

import (
	"encoding/json"
	"fmt"
	"strings"
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
	extracted := extractResultJSON(data)
	var out ValidatorOutput
	if err := json.Unmarshal(extracted, &out); err != nil {
		return nil, fmt.Errorf("parse validator output: %w", err)
	}
	if out.Status != "pass" && out.Status != "fail" {
		return nil, fmt.Errorf("invalid validator status: %q (must be 'pass' or 'fail')", out.Status)
	}
	return &out, nil
}

// ParsePlannerOutput parses structured planner output from JSON.
func ParsePlannerOutput(data []byte) (*PlannerOutput, error) {
	extracted := extractResultJSON(data)
	var out PlannerOutput
	if err := json.Unmarshal(extracted, &out); err != nil {
		return nil, fmt.Errorf("parse planner output: %w", err)
	}
	if len(out.Tasks) == 0 {
		return nil, fmt.Errorf("planner produced no tasks")
	}
	return &out, nil
}

// extractResultJSON extracts the inner result from Claude's --output-format json envelope.
// When claude is invoked with --output-format json, stdout is a JSON object like:
//
//	{"type": "result", "result": "...", "total_cost_usd": 0.5, ...}
//
// The "result" field contains the agent's actual text response. If that response is JSON
// (possibly wrapped in markdown code blocks), this function extracts it.
// If data is not a Claude envelope, it is returned as-is (with code block stripping).
func extractResultJSON(data []byte) []byte {
	// Try to extract from Claude output envelope
	var envelope struct {
		Result string `json:"result"`
	}
	if json.Unmarshal(data, &envelope) == nil && envelope.Result != "" {
		data = []byte(envelope.Result)
	}

	// Strip markdown code blocks if present (e.g. ```json\n...\n```)
	stripped := stripMarkdownCodeBlock(data)
	if json.Valid(stripped) {
		return stripped
	}

	// The response might contain a JSON code block embedded in natural language text.
	// Look for ```json ... ``` within the content.
	if found := extractEmbeddedCodeBlock(data); found != nil {
		return found
	}

	// Last resort: find the first { ... } that parses as valid JSON.
	if found := extractFirstJSONObject(data); found != nil {
		return found
	}

	return stripped
}

func stripMarkdownCodeBlock(data []byte) []byte {
	s := strings.TrimSpace(string(data))
	if !strings.HasPrefix(s, "```") {
		return data
	}
	// Skip the opening ``` line (e.g. "```json")
	if idx := strings.Index(s, "\n"); idx >= 0 {
		s = s[idx+1:]
	}
	// Strip trailing ```
	if idx := strings.LastIndex(s, "```"); idx >= 0 {
		s = s[:idx]
	}
	return []byte(strings.TrimSpace(s))
}

// extractEmbeddedCodeBlock finds a ```json ... ``` block embedded in text.
func extractEmbeddedCodeBlock(data []byte) []byte {
	s := string(data)
	// Look for ```json or ``` followed by JSON
	markers := []string{"```json\n", "```json\r\n", "```\n", "```\r\n"}
	for _, marker := range markers {
		start := strings.Index(s, marker)
		if start < 0 {
			continue
		}
		inner := s[start+len(marker):]
		end := strings.Index(inner, "```")
		if end < 0 {
			continue
		}
		candidate := []byte(strings.TrimSpace(inner[:end]))
		if json.Valid(candidate) {
			return candidate
		}
	}
	return nil
}

// extractFirstJSONObject finds the first top-level { ... } that parses as valid JSON.
func extractFirstJSONObject(data []byte) []byte {
	s := string(data)
	for i := 0; i < len(s); i++ {
		if s[i] != '{' {
			continue
		}
		// Find matching closing brace by counting depth.
		depth := 0
		inString := false
		escaped := false
		for j := i; j < len(s); j++ {
			if escaped {
				escaped = false
				continue
			}
			ch := s[j]
			if ch == '\\' && inString {
				escaped = true
				continue
			}
			if ch == '"' {
				inString = !inString
				continue
			}
			if inString {
				continue
			}
			if ch == '{' {
				depth++
			} else if ch == '}' {
				depth--
				if depth == 0 {
					candidate := []byte(s[i : j+1])
					if json.Valid(candidate) {
						return candidate
					}
					break
				}
			}
		}
	}
	return nil
}
