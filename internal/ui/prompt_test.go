package ui

import (
	"os"
	"testing"

	"github.com/kylegalloway/blueflame/internal/state"
)

func TestScriptedPrompterPlan(t *testing.T) {
	p := &ScriptedPrompter{
		PlanDecisions: []PlanDecision{PlanApprove, PlanReplan, PlanAbort},
	}

	if d, _ := p.PlanApproval(3, "$1.50"); d != PlanApprove {
		t.Errorf("first = %d, want PlanApprove", d)
	}
	if d, _ := p.PlanApproval(3, "$1.50"); d != PlanReplan {
		t.Errorf("second = %d, want PlanReplan", d)
	}
	if d, _ := p.PlanApproval(3, "$1.50"); d != PlanAbort {
		t.Errorf("third = %d, want PlanAbort", d)
	}
	// Exhausted -> default to abort
	if d, _ := p.PlanApproval(3, "$1.50"); d != PlanAbort {
		t.Errorf("exhausted = %d, want PlanAbort", d)
	}
}

func TestScriptedPrompterChangeset(t *testing.T) {
	p := &ScriptedPrompter{
		ChangesetDecisions: []ChangesetDecision{ChangesetApprove, ChangesetReject},
		RejectionReasons:   []string{"", "bad docs"},
	}

	cs := ChangesetInfo{Index: 1, Total: 2, CohesionGroup: "auth"}

	d, reason := p.ChangesetReview(cs)
	if d != ChangesetApprove {
		t.Errorf("first = %d, want ChangesetApprove", d)
	}
	if reason != "" {
		t.Errorf("reason = %q, want empty", reason)
	}

	d, reason = p.ChangesetReview(cs)
	if d != ChangesetReject {
		t.Errorf("second = %d, want ChangesetReject", d)
	}
	if reason != "bad docs" {
		t.Errorf("reason = %q, want %q", reason, "bad docs")
	}
}

func TestScriptedPrompterDeferredChangeset(t *testing.T) {
	p := &ScriptedPrompter{
		ChangesetDecisions: []ChangesetDecision{ChangesetApprove},
	}

	cs := ChangesetInfo{Deferred: true, DeferredNote: "depends on rejected group"}
	d, _ := p.ChangesetReview(cs)
	if d != ChangesetSkip {
		t.Errorf("deferred = %d, want ChangesetSkip", d)
	}
}

func TestScriptedPrompterSession(t *testing.T) {
	p := &ScriptedPrompter{
		SessionDecisions: []SessionDecision{SessionContinue, SessionStop},
	}

	state := SessionState{WaveCycle: 1}
	if d := p.SessionContinuation(state); d != SessionContinue {
		t.Errorf("first = %d, want SessionContinue", d)
	}
	if d := p.SessionContinuation(state); d != SessionStop {
		t.Errorf("second = %d, want SessionStop", d)
	}
}

func TestScriptedPrompterMessages(t *testing.T) {
	p := &ScriptedPrompter{}
	p.Warn("test warning")
	p.Info("test info")

	if len(p.Messages) != 2 {
		t.Fatalf("len(Messages) = %d, want 2", len(p.Messages))
	}
	if p.Messages[0] != "WARN: test warning" {
		t.Errorf("first message = %q", p.Messages[0])
	}
}

func TestPrompterInterface(t *testing.T) {
	var _ Prompter = &TerminalPrompter{}
	var _ Prompter = &ScriptedPrompter{}
}

func TestScriptedPrompterFromFile(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/decisions.txt"
	content := `# comment
approve
changeset-approve
changeset-reject
continue
stop
`
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	p := NewScriptedPrompterFromFile(path)

	if len(p.PlanDecisions) != 1 || p.PlanDecisions[0] != PlanApprove {
		t.Errorf("PlanDecisions = %v, want [PlanApprove]", p.PlanDecisions)
	}
	if len(p.ChangesetDecisions) != 2 {
		t.Errorf("ChangesetDecisions len = %d, want 2", len(p.ChangesetDecisions))
	}
	if len(p.SessionDecisions) != 2 {
		t.Errorf("SessionDecisions len = %d, want 2", len(p.SessionDecisions))
	}
}

func TestScriptedPrompterFromFileMissing(t *testing.T) {
	p := NewScriptedPrompterFromFile("/nonexistent/path")
	if len(p.PlanDecisions) != 0 {
		t.Errorf("expected empty decisions for missing file")
	}
}

func TestScriptedPrompterCrashRecovery(t *testing.T) {
	p := &ScriptedPrompter{
		RecoveryDecisions: []CrashRecoveryDecision{RecoveryResume, RecoveryFresh},
	}

	rs := &state.OrchestratorState{SessionID: "ses-test", WaveCycle: 2}

	if d := p.CrashRecoveryPrompt(rs); d != RecoveryResume {
		t.Errorf("first = %d, want RecoveryResume", d)
	}
	if d := p.CrashRecoveryPrompt(rs); d != RecoveryFresh {
		t.Errorf("second = %d, want RecoveryFresh", d)
	}
	// Exhausted -> default to RecoveryFresh
	if d := p.CrashRecoveryPrompt(rs); d != RecoveryFresh {
		t.Errorf("exhausted = %d, want RecoveryFresh", d)
	}
}

func TestScriptedPrompterFromFileRecovery(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/decisions.txt"
	content := `recovery-resume
recovery-fresh
approve
`
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	p := NewScriptedPrompterFromFile(path)

	if len(p.RecoveryDecisions) != 2 {
		t.Fatalf("RecoveryDecisions len = %d, want 2", len(p.RecoveryDecisions))
	}
	if p.RecoveryDecisions[0] != RecoveryResume {
		t.Errorf("first recovery = %d, want RecoveryResume", p.RecoveryDecisions[0])
	}
	if p.RecoveryDecisions[1] != RecoveryFresh {
		t.Errorf("second recovery = %d, want RecoveryFresh", p.RecoveryDecisions[1])
	}
	if len(p.PlanDecisions) != 1 {
		t.Errorf("PlanDecisions len = %d, want 1", len(p.PlanDecisions))
	}
}
