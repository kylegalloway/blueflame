package ui

import (
	"strings"
	"testing"
	"time"
)

func TestFormatProgress(t *testing.T) {
	ps := ProgressState{
		Phase:         "development",
		WaveCycle:     2,
		RunningAgents: 3,
		TotalTasks:    10,
		Completed:     4,
		Failed:        1,
		SessionCost:   2.50,
		StartTime:     time.Now().Add(-5 * time.Minute),
	}

	got := FormatProgress(ps)
	if !strings.Contains(got, "development") {
		t.Errorf("missing phase: %s", got)
	}
	if !strings.Contains(got, "Wave 2") {
		t.Errorf("missing wave cycle: %s", got)
	}
	if !strings.Contains(got, "3 running") {
		t.Errorf("missing running count: %s", got)
	}
	if !strings.Contains(got, "4/10 done") {
		t.Errorf("missing done count: %s", got)
	}
	if !strings.Contains(got, "$2.50") {
		t.Errorf("missing cost: %s", got)
	}
}

func TestFormatCostSummary(t *testing.T) {
	cs := CostSummary{
		SessionID:      "ses-test",
		TotalCost:      5.25,
		TotalTokens:    50000,
		WaveCycles:     3,
		TasksCompleted: 8,
		TasksFailed:    1,
		TasksMerged:    7,
		Duration:       15 * time.Minute,
		CostLimit:      20.0,
		TokenLimit:     200000,
	}

	got := FormatCostSummary(cs)
	checks := []string{
		"ses-test",
		"15m0s",
		"Waves:      3",
		"Completed: 8",
		"Merged:    7",
		"Failed:    1",
		"$5.25",
		"$20.00",
		"50000",
		"200000",
	}
	for _, want := range checks {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in:\n%s", want, got)
		}
	}
}

func TestFormatCostSummaryNoLimits(t *testing.T) {
	cs := CostSummary{
		SessionID:  "ses-test",
		TotalCost:  1.0,
		Duration:   5 * time.Minute,
	}

	got := FormatCostSummary(cs)
	// Should not contain "used" since no limits set
	if strings.Contains(got, "used") {
		t.Errorf("should not show limit percentage: %s", got)
	}
}
