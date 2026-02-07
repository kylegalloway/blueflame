package memory

import (
	"encoding/json"
	"fmt"
	"os/exec"
)

// BeadsConfig holds Beads-specific configuration.
type BeadsConfig struct {
	Enabled             bool
	ArchiveAfterWave    bool
	IncludeFailureNotes bool
}

// BeadsProvider implements Provider using the beads CLI.
type BeadsProvider struct {
	config BeadsConfig
}

// NewBeadsProvider creates a BeadsProvider.
func NewBeadsProvider(cfg BeadsConfig) *BeadsProvider {
	return &BeadsProvider{config: cfg}
}

func (m *BeadsProvider) Save(session SessionResult) error {
	for _, task := range session.CompletedTasks {
		data := map[string]interface{}{
			"task_id":         task.ID,
			"title":           task.Title,
			"result":          task.ResultStatus,
			"validator_notes": task.ValidatorNotes,
			"files_changed":   task.FilesChanged,
			"cost_usd":        task.CostUSD,
		}
		jsonData, _ := json.Marshal(data)
		cmd := exec.Command("beads", "save", "--type", "task-result", "--data", string(jsonData))
		if err := cmd.Run(); err != nil {
			return fmt.Errorf("beads save task-result %s: %w", task.ID, err)
		}
	}

	if m.config.IncludeFailureNotes {
		for _, task := range session.FailedTasks {
			data := map[string]interface{}{
				"task_id":        task.ID,
				"title":          task.Title,
				"failure_reason": task.FailureReason,
				"retry_count":    task.RetryCount,
			}
			jsonData, _ := json.Marshal(data)
			cmd := exec.Command("beads", "save", "--type", "task-failure", "--data", string(jsonData))
			if err := cmd.Run(); err != nil {
				return fmt.Errorf("beads save task-failure %s: %w", task.ID, err)
			}
		}
	}

	summary := map[string]interface{}{
		"session_id":     session.ID,
		"total_tasks":    len(session.AllTasks),
		"completed":      len(session.CompletedTasks),
		"failed":         len(session.FailedTasks),
		"total_cost_usd": session.TotalCostUSD,
		"wave_cycles":    session.WaveCycles,
	}
	jsonData, _ := json.Marshal(summary)
	cmd := exec.Command("beads", "save", "--type", "session-summary", "--data", string(jsonData))
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("beads save session-summary: %w", err)
	}

	return nil
}

func (m *BeadsProvider) Load() (SessionContext, error) {
	output, err := exec.Command("beads", "load",
		"--type", "task-failure,session-summary",
		"--format", "json",
		"--limit", "20",
	).Output()
	if err != nil {
		// Graceful degradation: no beads data available
		return SessionContext{}, nil
	}

	var context SessionContext
	json.Unmarshal(output, &context)
	return context, nil
}
