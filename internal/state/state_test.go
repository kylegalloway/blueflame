package state

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestSaveAndLoad(t *testing.T) {
	dir := t.TempDir()
	mgr := NewManager(dir)

	original := &OrchestratorState{
		SessionID:     "ses-001",
		WaveCycle:     2,
		Phase:         "development",
		SessionCost:   3.50,
		SessionTokens: 12000,
		StartTime:     time.Now().Truncate(time.Second),
	}

	if err := mgr.Save(original); err != nil {
		t.Fatalf("Save: %v", err)
	}

	loaded, err := mgr.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	if loaded.SessionID != original.SessionID {
		t.Errorf("SessionID = %q, want %q", loaded.SessionID, original.SessionID)
	}
	if loaded.WaveCycle != original.WaveCycle {
		t.Errorf("WaveCycle = %d, want %d", loaded.WaveCycle, original.WaveCycle)
	}
	if loaded.Phase != original.Phase {
		t.Errorf("Phase = %q, want %q", loaded.Phase, original.Phase)
	}
	if loaded.SessionCost != original.SessionCost {
		t.Errorf("SessionCost = %f, want %f", loaded.SessionCost, original.SessionCost)
	}
	if loaded.SessionTokens != original.SessionTokens {
		t.Errorf("SessionTokens = %d, want %d", loaded.SessionTokens, original.SessionTokens)
	}
	if loaded.LastSave.IsZero() {
		t.Error("LastSave should be set after Save")
	}
}

func TestLoadMissing(t *testing.T) {
	mgr := NewManager(t.TempDir())
	_, err := mgr.Load()
	if err == nil {
		t.Error("expected error for missing state file")
	}
}

func TestLoadCorrupted(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "state.json"), []byte("{invalid json"), 0o644)

	mgr := NewManager(dir)
	_, err := mgr.Load()
	if err == nil {
		t.Error("expected error for corrupted state file")
	}
}

func TestExists(t *testing.T) {
	dir := t.TempDir()
	mgr := NewManager(dir)

	if mgr.Exists() {
		t.Error("Exists should be false before Save")
	}

	mgr.Save(&OrchestratorState{SessionID: "test"})

	if !mgr.Exists() {
		t.Error("Exists should be true after Save")
	}
}

func TestRemove(t *testing.T) {
	dir := t.TempDir()
	mgr := NewManager(dir)
	mgr.Save(&OrchestratorState{SessionID: "test"})

	if err := mgr.Remove(); err != nil {
		t.Fatalf("Remove: %v", err)
	}

	if mgr.Exists() {
		t.Error("Exists should be false after Remove")
	}
}

func TestRemoveNonexistent(t *testing.T) {
	mgr := NewManager(t.TempDir())
	if err := mgr.Remove(); err != nil {
		t.Errorf("Remove nonexistent: %v", err)
	}
}

func TestAtomicSave(t *testing.T) {
	dir := t.TempDir()
	mgr := NewManager(dir)

	// Save multiple times; verify each is valid
	for i := 0; i < 5; i++ {
		err := mgr.Save(&OrchestratorState{
			SessionID: "test",
			WaveCycle: i + 1,
		})
		if err != nil {
			t.Fatalf("Save %d: %v", i, err)
		}

		loaded, err := mgr.Load()
		if err != nil {
			t.Fatalf("Load after Save %d: %v", i, err)
		}
		if loaded.WaveCycle != i+1 {
			t.Errorf("WaveCycle = %d, want %d", loaded.WaveCycle, i+1)
		}
	}
}
