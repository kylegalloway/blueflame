package ui

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/kylegalloway/blueflame/internal/state"
)

// PlanDecision represents the human's decision on a proposed plan.
type PlanDecision int

const (
	PlanApprove PlanDecision = iota
	PlanEdit
	PlanReplan
	PlanAbort
)

// ChangesetDecision represents the human's decision on a changeset.
type ChangesetDecision int

const (
	ChangesetApprove ChangesetDecision = iota
	ChangesetReject
	ChangesetSkip
)

// SessionDecision represents the human's decision on continuing a session.
type SessionDecision int

const (
	SessionContinue SessionDecision = iota
	SessionReplan
	SessionStop
)

// CrashRecoveryDecision represents the human's decision when a previous session is found.
type CrashRecoveryDecision int

const (
	RecoveryResume CrashRecoveryDecision = iota
	RecoveryFresh
)

// ValidatorFailureDecision represents the human's decision when a validator fails.
type ValidatorFailureDecision int

const (
	ValidatorManualReview ValidatorFailureDecision = iota
	ValidatorSkipTask
	ValidatorRetryTask
)

// ChangesetInfo describes a changeset for review.
type ChangesetInfo struct {
	Index         int
	Total         int
	CohesionGroup string
	Description   string
	FilesChanged  int
	LinesAdded    int
	LinesRemoved  int
	TaskIDs       []string
	Diff          string
	Deferred      bool
	DeferredNote  string
}

// SessionState describes the current session state for the continuation prompt.
type SessionState struct {
	WaveCycle     int
	Approved      int
	Requeued      int
	Blocked       int
	TotalCost     float64
	CostLimit     float64
	TokensUsed    int
	TokenLimit    int
	RequeuedTasks []string
}

// Prompter is the interface for human interaction.
type Prompter interface {
	PlanApproval(taskCount int, estimatedCost string) (PlanDecision, string)
	ChangesetReview(cs ChangesetInfo) (ChangesetDecision, string)
	SessionContinuation(state SessionState) SessionDecision
	ValidatorFailed(taskID string, err error) ValidatorFailureDecision
	CrashRecoveryPrompt(rs *state.OrchestratorState) CrashRecoveryDecision
	Warn(msg string)
	Info(msg string)
}

// TerminalPrompter implements Prompter using terminal I/O.
type TerminalPrompter struct {
	reader *bufio.Reader
	writer io.Writer
}

// NewTerminalPrompter creates a TerminalPrompter using stdin/stdout.
func NewTerminalPrompter() *TerminalPrompter {
	return &TerminalPrompter{
		reader: bufio.NewReader(os.Stdin),
		writer: os.Stdout,
	}
}

func (p *TerminalPrompter) PlanApproval(taskCount int, estimatedCost string) (PlanDecision, string) {
	fmt.Fprintf(p.writer, "\n(a)pprove / (e)dit tasks.yaml / (r)e-plan / (q)uit? ")
	line, _ := p.reader.ReadString('\n')
	switch strings.TrimSpace(strings.ToLower(line)) {
	case "a", "approve":
		return PlanApprove, ""
	case "e", "edit":
		return PlanEdit, ""
	case "r", "re-plan", "replan":
		fmt.Fprintf(p.writer, "What should change? ")
		feedback, _ := p.reader.ReadString('\n')
		return PlanReplan, strings.TrimSpace(feedback)
	case "q", "quit":
		return PlanAbort, ""
	default:
		return PlanAbort, ""
	}
}

func (p *TerminalPrompter) ChangesetReview(cs ChangesetInfo) (ChangesetDecision, string) {
	if cs.Deferred {
		fmt.Fprintf(p.writer, "\nChangeset %d/%d: [%s] %s\n  NOTE: %s\n",
			cs.Index, cs.Total, cs.CohesionGroup, cs.Description, cs.DeferredNote)
		return ChangesetSkip, ""
	}

	fmt.Fprintf(p.writer, "\nChangeset %d/%d: [%s] %s\n  [%d files changed, +%d, -%d]\n  Tasks: %s\n",
		cs.Index, cs.Total, cs.CohesionGroup, cs.Description,
		cs.FilesChanged, cs.LinesAdded, cs.LinesRemoved,
		strings.Join(cs.TaskIDs, ", "))
	fmt.Fprintf(p.writer, "  (a)pprove / (r)eject / (v)iew diff / (s)kip? ")

	line, _ := p.reader.ReadString('\n')
	switch strings.TrimSpace(strings.ToLower(line)) {
	case "a", "approve":
		return ChangesetApprove, ""
	case "r", "reject":
		fmt.Fprintf(p.writer, "  Rejection reason: ")
		reason, _ := p.reader.ReadString('\n')
		return ChangesetReject, strings.TrimSpace(reason)
	case "v", "view":
		fmt.Fprintln(p.writer, cs.Diff)
		// Re-prompt after viewing
		return p.ChangesetReview(cs)
	case "s", "skip":
		return ChangesetSkip, ""
	default:
		return ChangesetSkip, ""
	}
}

func (p *TerminalPrompter) SessionContinuation(state SessionState) SessionDecision {
	fmt.Fprintf(p.writer, "\nWave cycle %d complete.\n", state.WaveCycle)
	fmt.Fprintf(p.writer, "  Approved: %d changeset(s)\n", state.Approved)
	fmt.Fprintf(p.writer, "  Re-queued: %d task(s)", state.Requeued)
	if len(state.RequeuedTasks) > 0 {
		fmt.Fprintf(p.writer, " (%s)", strings.Join(state.RequeuedTasks, ", "))
	}
	fmt.Fprintln(p.writer)
	fmt.Fprintf(p.writer, "  Blocked: %d task(s)\n", state.Blocked)

	if state.CostLimit > 0 {
		fmt.Fprintf(p.writer, "  Session budget: $%.2f / $%.2f USD limit\n",
			state.TotalCost, state.CostLimit)
	} else if state.TokenLimit > 0 {
		fmt.Fprintf(p.writer, "  Session budget: %d / %d token limit\n",
			state.TokensUsed, state.TokenLimit)
	}

	fmt.Fprintf(p.writer, "\n  (c)ontinue / (r)e-plan / (s)top? ")
	line, _ := p.reader.ReadString('\n')
	switch strings.TrimSpace(strings.ToLower(line)) {
	case "c", "continue":
		return SessionContinue
	case "r", "re-plan", "replan":
		return SessionReplan
	case "s", "stop":
		return SessionStop
	default:
		return SessionStop
	}
}

func (p *TerminalPrompter) ValidatorFailed(taskID string, err error) ValidatorFailureDecision {
	fmt.Fprintf(p.writer, "\nValidator failed for %s: %v\n", taskID, err)
	fmt.Fprintf(p.writer, "  (m)anual review / (s)kip task / (r)etry? ")
	line, _ := p.reader.ReadString('\n')
	switch strings.TrimSpace(strings.ToLower(line)) {
	case "m", "manual":
		return ValidatorManualReview
	case "s", "skip":
		return ValidatorSkipTask
	case "r", "retry":
		return ValidatorRetryTask
	default:
		return ValidatorSkipTask
	}
}

func (p *TerminalPrompter) CrashRecoveryPrompt(rs *state.OrchestratorState) CrashRecoveryDecision {
	fmt.Fprintf(p.writer, "\nPrevious session found: %s\n", rs.SessionID)
	fmt.Fprintf(p.writer, "  Wave cycle: %d, phase: %s\n", rs.WaveCycle, rs.Phase)
	fmt.Fprintf(p.writer, "  Cost so far: $%.2f (%d tokens)\n", rs.SessionCost, rs.SessionTokens)
	fmt.Fprintf(p.writer, "\n(r)esume / (f)resh? ")
	line, _ := p.reader.ReadString('\n')
	switch strings.TrimSpace(strings.ToLower(line)) {
	case "r", "resume":
		return RecoveryResume
	default:
		return RecoveryFresh
	}
}

func (p *TerminalPrompter) Warn(msg string) {
	fmt.Fprintf(p.writer, "WARNING: %s\n", msg)
}

func (p *TerminalPrompter) Info(msg string) {
	fmt.Fprintf(p.writer, "%s\n", msg)
}

// ScriptedPrompter implements Prompter with predetermined decisions for testing.
type ScriptedPrompter struct {
	PlanDecisions      []PlanDecision
	ChangesetDecisions []ChangesetDecision
	SessionDecisions   []SessionDecision
	ValidatorDecisions []ValidatorFailureDecision
	RecoveryDecisions  []CrashRecoveryDecision
	RejectionReasons   []string
	ReplanFeedback     []string
	Messages           []string

	planIdx      int
	changesetIdx int
	sessionIdx   int
	validatorIdx int
	recoveryIdx  int
}

func (p *ScriptedPrompter) PlanApproval(taskCount int, estimatedCost string) (PlanDecision, string) {
	if p.planIdx < len(p.PlanDecisions) {
		d := p.PlanDecisions[p.planIdx]
		var feedback string
		if d == PlanReplan && p.planIdx < len(p.ReplanFeedback) {
			feedback = p.ReplanFeedback[p.planIdx]
		}
		p.planIdx++
		return d, feedback
	}
	return PlanAbort, ""
}

func (p *ScriptedPrompter) ChangesetReview(cs ChangesetInfo) (ChangesetDecision, string) {
	if cs.Deferred {
		return ChangesetSkip, ""
	}
	if p.changesetIdx < len(p.ChangesetDecisions) {
		d := p.ChangesetDecisions[p.changesetIdx]
		var reason string
		if d == ChangesetReject && p.changesetIdx < len(p.RejectionReasons) {
			reason = p.RejectionReasons[p.changesetIdx]
		}
		p.changesetIdx++
		return d, reason
	}
	return ChangesetApprove, ""
}

func (p *ScriptedPrompter) SessionContinuation(state SessionState) SessionDecision {
	if p.sessionIdx < len(p.SessionDecisions) {
		d := p.SessionDecisions[p.sessionIdx]
		p.sessionIdx++
		return d
	}
	return SessionStop
}

func (p *ScriptedPrompter) ValidatorFailed(taskID string, err error) ValidatorFailureDecision {
	if p.validatorIdx < len(p.ValidatorDecisions) {
		d := p.ValidatorDecisions[p.validatorIdx]
		p.validatorIdx++
		return d
	}
	return ValidatorSkipTask
}

func (p *ScriptedPrompter) CrashRecoveryPrompt(rs *state.OrchestratorState) CrashRecoveryDecision {
	if p.recoveryIdx < len(p.RecoveryDecisions) {
		d := p.RecoveryDecisions[p.recoveryIdx]
		p.recoveryIdx++
		return d
	}
	return RecoveryFresh
}

// NewScriptedPrompterFromFile creates a ScriptedPrompter by reading decisions from a file.
// File format: one decision per line (approve/reject/continue/stop/etc.)
func NewScriptedPrompterFromFile(path string) *ScriptedPrompter {
	data, err := os.ReadFile(path)
	if err != nil {
		return &ScriptedPrompter{}
	}

	p := &ScriptedPrompter{}
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		switch strings.ToLower(line) {
		case "approve", "plan-approve":
			p.PlanDecisions = append(p.PlanDecisions, PlanApprove)
		case "plan-edit":
			p.PlanDecisions = append(p.PlanDecisions, PlanEdit)
		case "plan-replan":
			p.PlanDecisions = append(p.PlanDecisions, PlanReplan)
		case "plan-abort":
			p.PlanDecisions = append(p.PlanDecisions, PlanAbort)
		case "changeset-approve":
			p.ChangesetDecisions = append(p.ChangesetDecisions, ChangesetApprove)
		case "changeset-reject":
			p.ChangesetDecisions = append(p.ChangesetDecisions, ChangesetReject)
		case "changeset-skip":
			p.ChangesetDecisions = append(p.ChangesetDecisions, ChangesetSkip)
		case "continue":
			p.SessionDecisions = append(p.SessionDecisions, SessionContinue)
		case "stop":
			p.SessionDecisions = append(p.SessionDecisions, SessionStop)
		case "replan":
			p.SessionDecisions = append(p.SessionDecisions, SessionReplan)
		case "recovery-resume":
			p.RecoveryDecisions = append(p.RecoveryDecisions, RecoveryResume)
		case "recovery-fresh":
			p.RecoveryDecisions = append(p.RecoveryDecisions, RecoveryFresh)
		}
	}
	return p
}

func (p *ScriptedPrompter) Warn(msg string) {
	p.Messages = append(p.Messages, "WARN: "+msg)
	fmt.Fprintf(os.Stderr, "WARNING: %s\n", msg)
}

func (p *ScriptedPrompter) Info(msg string) {
	p.Messages = append(p.Messages, msg)
	fmt.Fprintf(os.Stderr, "%s\n", msg)
}
