package state

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// OrchestratorState holds the persistent orchestrator state for crash recovery.
type OrchestratorState struct {
	SessionID    string    `json:"session_id"`
	WaveCycle    int       `json:"wave_cycle"`
	Phase        string    `json:"phase"` // "planning", "development", "validation", "merge"
	SessionCost  float64   `json:"session_cost_usd"`
	SessionTokens int     `json:"session_tokens"`
	StartTime    time.Time `json:"start_time"`
	LastSave     time.Time `json:"last_save"`
}

// Manager handles crash recovery state persistence.
type Manager struct {
	path string
}

// NewManager creates a state Manager.
func NewManager(blueflameDir string) *Manager {
	return &Manager{
		path: filepath.Join(blueflameDir, "state.json"),
	}
}

// Save persists the orchestrator state atomically.
func (m *Manager) Save(state *OrchestratorState) error {
	state.LastSave = time.Now()

	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal state: %w", err)
	}

	dir := filepath.Dir(m.path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create state dir: %w", err)
	}

	tmpFile, err := os.CreateTemp(dir, "state-*.json.tmp")
	if err != nil {
		return fmt.Errorf("create temp file: %w", err)
	}
	tmpPath := tmpFile.Name()

	if _, err := tmpFile.Write(data); err != nil {
		tmpFile.Close()
		os.Remove(tmpPath)
		return fmt.Errorf("write temp file: %w", err)
	}
	if err := tmpFile.Close(); err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("close temp file: %w", err)
	}

	if err := os.Rename(tmpPath, m.path); err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("rename temp file: %w", err)
	}

	return nil
}

// Load reads the persisted orchestrator state.
func (m *Manager) Load() (*OrchestratorState, error) {
	data, err := os.ReadFile(m.path)
	if err != nil {
		return nil, fmt.Errorf("read state: %w", err)
	}

	var state OrchestratorState
	if err := json.Unmarshal(data, &state); err != nil {
		return nil, fmt.Errorf("parse state: %w", err)
	}

	return &state, nil
}

// Exists returns true if a recovery state file exists.
func (m *Manager) Exists() bool {
	_, err := os.Stat(m.path)
	return err == nil
}

// Remove deletes the state file (after successful session completion).
func (m *Manager) Remove() error {
	if err := os.Remove(m.path); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("remove state: %w", err)
	}
	return nil
}
