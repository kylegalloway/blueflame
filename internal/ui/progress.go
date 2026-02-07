package ui

import (
	"fmt"
	"strings"
	"time"
)

// ProgressState holds the current state for progress display.
type ProgressState struct {
	Phase         string
	WaveCycle     int
	RunningAgents int
	TotalTasks    int
	Completed     int
	Failed        int
	Blocked       int
	Pending       int
	SessionCost   float64
	SessionTokens int
	StartTime     time.Time
}

// CostSummary holds the final cost report for a session.
type CostSummary struct {
	SessionID     string
	TotalCost     float64
	TotalTokens   int
	WaveCycles    int
	TasksCompleted int
	TasksFailed    int
	TasksMerged    int
	Duration       time.Duration
	CostLimit      float64
	TokenLimit     int
}

// FormatProgress returns a single-line progress string for display during waves.
func FormatProgress(ps ProgressState) string {
	elapsed := time.Since(ps.StartTime).Truncate(time.Second)
	return fmt.Sprintf("[%s] Wave %d | %d running | %d/%d done | %d failed | $%.2f | %v elapsed",
		ps.Phase, ps.WaveCycle, ps.RunningAgents, ps.Completed, ps.TotalTasks,
		ps.Failed, ps.SessionCost, elapsed)
}

// FormatCostSummary returns a multi-line cost summary for end-of-session display.
func FormatCostSummary(cs CostSummary) string {
	var b strings.Builder
	b.WriteString("\n=== Session Summary ===\n")
	b.WriteString(fmt.Sprintf("Session:    %s\n", cs.SessionID))
	b.WriteString(fmt.Sprintf("Duration:   %v\n", cs.Duration.Truncate(time.Second)))
	b.WriteString(fmt.Sprintf("Waves:      %d\n", cs.WaveCycles))
	b.WriteString("\nTasks:\n")
	b.WriteString(fmt.Sprintf("  Completed: %d\n", cs.TasksCompleted))
	b.WriteString(fmt.Sprintf("  Merged:    %d\n", cs.TasksMerged))
	b.WriteString(fmt.Sprintf("  Failed:    %d\n", cs.TasksFailed))
	b.WriteString("\nCost:\n")
	b.WriteString(fmt.Sprintf("  Total:     $%.4f\n", cs.TotalCost))
	if cs.CostLimit > 0 {
		pct := (cs.TotalCost / cs.CostLimit) * 100
		b.WriteString(fmt.Sprintf("  Limit:     $%.2f (%.1f%% used)\n", cs.CostLimit, pct))
	}
	b.WriteString(fmt.Sprintf("  Tokens:    %d\n", cs.TotalTokens))
	if cs.TokenLimit > 0 {
		pct := (float64(cs.TotalTokens) / float64(cs.TokenLimit)) * 100
		b.WriteString(fmt.Sprintf("  Limit:     %d (%.1f%% used)\n", cs.TokenLimit, pct))
	}
	b.WriteString("=======================\n")
	return b.String()
}
