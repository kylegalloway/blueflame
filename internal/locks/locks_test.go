package locks

import (
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"syscall"
	"testing"
)

func TestAcquireAndRelease(t *testing.T) {
	dir := t.TempDir()
	mgr := NewManager(filepath.Join(dir, "locks"))

	err := mgr.Acquire("worker-1", []string{"pkg/middleware/", "internal/auth/"})
	if err != nil {
		t.Fatalf("Acquire: %v", err)
	}

	if !mgr.IsHeld("pkg/middleware/") {
		t.Error("pkg/middleware/ should be held")
	}
	if !mgr.IsHeld("internal/auth/") {
		t.Error("internal/auth/ should be held")
	}

	held := mgr.HeldPaths()
	if len(held) != 2 {
		t.Errorf("len(HeldPaths) = %d, want 2", len(held))
	}

	mgr.Release("worker-1")

	if mgr.IsHeld("pkg/middleware/") {
		t.Error("pkg/middleware/ should not be held after release")
	}
}

func TestConflictDetection(t *testing.T) {
	dir := t.TempDir()
	lockDir := filepath.Join(dir, "locks")
	mgr1 := NewManager(lockDir)
	mgr2 := NewManager(lockDir)

	// First manager acquires
	err := mgr1.Acquire("worker-1", []string{"pkg/middleware/"})
	if err != nil {
		t.Fatalf("Acquire 1: %v", err)
	}

	// Second manager should fail on same path
	err = mgr2.Acquire("worker-2", []string{"pkg/middleware/"})
	if err == nil {
		t.Error("expected conflict error")
	}

	// Release and retry
	mgr1.Release("worker-1")

	err = mgr2.Acquire("worker-2", []string{"pkg/middleware/"})
	if err != nil {
		t.Fatalf("Acquire after release: %v", err)
	}
	mgr2.Release("worker-2")
}

func TestAllOrNothingAcquire(t *testing.T) {
	dir := t.TempDir()
	lockDir := filepath.Join(dir, "locks")
	mgr1 := NewManager(lockDir)
	mgr2 := NewManager(lockDir)

	// First manager holds one path
	err := mgr1.Acquire("worker-1", []string{"pkg/b/"})
	if err != nil {
		t.Fatalf("Acquire 1: %v", err)
	}

	// Second manager tries to acquire two paths, one conflicts
	err = mgr2.Acquire("worker-2", []string{"pkg/a/", "pkg/b/"})
	if err == nil {
		t.Error("expected conflict error")
	}

	// pkg/a/ should NOT be held since the acquire rolled back
	if mgr2.IsHeld("pkg/a/") {
		t.Error("pkg/a/ should not be held after rollback")
	}

	mgr1.Release("worker-1")
}

func TestHasConflict(t *testing.T) {
	dir := t.TempDir()
	mgr := NewManager(filepath.Join(dir, "locks"))

	mgr.Acquire("worker-1", []string{"pkg/a/"})

	if !mgr.HasConflict([]string{"pkg/a/"}) {
		t.Error("expected conflict with pkg/a/")
	}
	if mgr.HasConflict([]string{"pkg/b/"}) {
		t.Error("unexpected conflict with pkg/b/")
	}

	mgr.Release("worker-1")
}

func TestCleanStale(t *testing.T) {
	dir := t.TempDir()
	lockDir := filepath.Join(dir, "locks")
	os.MkdirAll(lockDir, 0o755)

	// Create a "stale" lock file (no flock held)
	stalePath := filepath.Join(lockDir, "pkg__old__.lock")
	f, _ := os.Create(stalePath)
	f.WriteString("dead-agent 99999 2026-01-01T00:00:00Z\n")
	f.Close()

	mgr := NewManager(lockDir)
	err := mgr.CleanStale()
	if err != nil {
		t.Fatalf("CleanStale: %v", err)
	}

	// Stale lock should be removed
	if _, err := os.Stat(stalePath); !os.IsNotExist(err) {
		t.Error("stale lock file should have been removed")
	}
}

func TestCleanStaleDoesNotRemoveActive(t *testing.T) {
	dir := t.TempDir()
	lockDir := filepath.Join(dir, "locks")

	mgr := NewManager(lockDir)
	mgr.Acquire("worker-1", []string{"active/"})

	// Create a separate manager to run CleanStale
	mgr2 := NewManager(lockDir)
	err := mgr2.CleanStale()
	if err != nil {
		t.Fatalf("CleanStale: %v", err)
	}

	// Active lock should still be held
	if !mgr.IsHeld("active/") {
		t.Error("active lock should still be held")
	}

	mgr.Release("worker-1")
}

func TestLockFilePath(t *testing.T) {
	mgr := NewManager("/locks")

	tests := []struct {
		input    string
		expected string
	}{
		{"pkg/middleware/", "/locks/pkg__middleware__.lock"},
		{"internal/auth/handler.go", "/locks/internal__auth__handler.go.lock"},
	}
	for _, tt := range tests {
		got := mgr.lockFilePath(tt.input)
		if got != tt.expected {
			t.Errorf("lockFilePath(%q) = %q, want %q", tt.input, got, tt.expected)
		}
	}
}

func TestConcurrentAcquisition(t *testing.T) {
	dir := t.TempDir()
	lockDir := filepath.Join(dir, "locks")

	var successes atomic.Int32
	var wg sync.WaitGroup

	// 10 goroutines compete for the same lock
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			mgr := NewManager(lockDir)
			err := mgr.Acquire("worker-"+string(rune('A'+id)), []string{"contested/"})
			if err == nil {
				successes.Add(1)
				// Hold briefly then release
				mgr.Release("worker-" + string(rune('A'+id)))
			}
		}(i)
	}

	wg.Wait()

	// At least one should have succeeded
	if successes.Load() == 0 {
		t.Error("expected at least one successful acquisition")
	}
}

func TestCleanStaleNonexistentDir(t *testing.T) {
	mgr := NewManager("/nonexistent/locks")
	err := mgr.CleanStale()
	if err != nil {
		t.Fatalf("CleanStale on nonexistent dir: %v", err)
	}
}

func TestReleaseAllEmpty(t *testing.T) {
	mgr := NewManager(t.TempDir())
	// Should not panic
	mgr.ReleaseAll()
}

func TestLockFileMetadata(t *testing.T) {
	dir := t.TempDir()
	lockDir := filepath.Join(dir, "locks")
	mgr := NewManager(lockDir)

	mgr.Acquire("worker-meta", []string{"test/"})

	// Read the lock file to verify metadata
	lockPath := mgr.lockFilePath("test/")
	data, err := os.ReadFile(lockPath)
	if err != nil {
		t.Fatalf("read lock file: %v", err)
	}

	content := string(data)
	if len(content) == 0 {
		t.Error("lock file should contain metadata")
	}

	// Should contain agent ID and PID
	if !containsString(content, "worker-meta") {
		t.Errorf("lock metadata should contain agent ID, got: %q", content)
	}

	mgr.Release("worker-meta")
}

func containsString(s, substr string) bool {
	return len(s) >= len(substr) && searchString(s, substr)
}

func searchString(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

// Verify flock is actually providing OS-level enforcement
func TestFlockActuallyWorks(t *testing.T) {
	dir := t.TempDir()
	lockPath := filepath.Join(dir, "test.lock")

	f1, _ := os.OpenFile(lockPath, os.O_CREATE|os.O_RDWR, 0o644)
	defer f1.Close()

	// Acquire exclusive lock
	err := syscall.Flock(int(f1.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
	if err != nil {
		t.Fatalf("first flock: %v", err)
	}

	// Second attempt should fail
	f2, _ := os.OpenFile(lockPath, os.O_CREATE|os.O_RDWR, 0o644)
	defer f2.Close()

	err = syscall.Flock(int(f2.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
	if err == nil {
		t.Error("expected flock conflict")
	}

	// Release first lock
	syscall.Flock(int(f1.Fd()), syscall.LOCK_UN)

	// Now second attempt should succeed
	err = syscall.Flock(int(f2.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
	if err != nil {
		t.Fatalf("second flock after release: %v", err)
	}
	syscall.Flock(int(f2.Fd()), syscall.LOCK_UN)
}
