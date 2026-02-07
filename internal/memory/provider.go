package memory

import "time"

// SessionResult holds the results of a completed session for archival.
type SessionResult struct {
	ID             string
	AllTasks       []TaskSummary
	CompletedTasks []TaskSummary
	FailedTasks    []TaskSummary
	TotalCostUSD   float64
	TotalTokens    int
	Duration       time.Duration
	WaveCycles     int
}

// TaskSummary holds summary info about a task for archival.
type TaskSummary struct {
	ID            string
	Title         string
	ResultStatus  string
	ValidatorNotes string
	FilesChanged  []string
	CostUSD       float64
	TokensUsed    int
	FailureReason string
	RetryCount    int
}

// SessionContext holds prior session context loaded for the planner.
type SessionContext struct {
	PriorFailures []TaskSummary
	SessionCount  int
}

// Provider is the interface for persistent cross-session memory.
type Provider interface {
	Save(session SessionResult) error
	Load() (SessionContext, error)
}
