package worktree

import (
	"testing"
)

func TestCheckDiskSpacePass(t *testing.T) {
	// Current directory should have plenty of space
	err := CheckDiskSpace("/tmp", 1) // 1 MB - should always pass
	if err != nil {
		t.Errorf("CheckDiskSpace: %v", err)
	}
}

func TestCheckDiskSpaceFail(t *testing.T) {
	// Request an absurd amount of space
	err := CheckDiskSpace("/tmp", 999999999)
	if err == nil {
		t.Error("expected error for insufficient disk space")
	}
}

func TestCheckDiskSpaceBadPath(t *testing.T) {
	err := CheckDiskSpace("/nonexistent/path/that/should/not/exist", 1)
	if err == nil {
		t.Error("expected error for bad path")
	}
}
