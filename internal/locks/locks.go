package locks

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"
)

// Manager handles advisory file locking via flock.
type Manager struct {
	lockDir    string
	held       map[string]*os.File  // normalized path -> open file handle
	agentPaths map[string][]string  // agentID -> list of held paths
	mu         sync.Mutex
}

// NewManager creates a lock Manager.
func NewManager(lockDir string) *Manager {
	return &Manager{
		lockDir:    lockDir,
		held:       make(map[string]*os.File),
		agentPaths: make(map[string][]string),
	}
}

// lockFilePath converts a project-relative path to a lock file path.
// Path separators are replaced with double underscores.
func (m *Manager) lockFilePath(path string) string {
	normalized := strings.ReplaceAll(path, string(filepath.Separator), "__")
	return filepath.Join(m.lockDir, normalized+".lock")
}

// Acquire acquires exclusive locks on the given paths for an agent.
// Fails immediately if any lock is already held (non-blocking flock).
// All-or-nothing: if any lock fails, all previously acquired locks are released.
func (m *Manager) Acquire(agentID string, paths []string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if err := os.MkdirAll(m.lockDir, 0o755); err != nil {
		return fmt.Errorf("create lock dir: %w", err)
	}

	var acquired []*lockEntry
	for _, path := range paths {
		lockPath := m.lockFilePath(path)

		f, err := os.OpenFile(lockPath, os.O_CREATE|os.O_RDWR, 0o644)
		if err != nil {
			m.rollback(acquired)
			return fmt.Errorf("open lock file %s: %w", lockPath, err)
		}

		// Non-blocking exclusive lock
		err = syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
		if err != nil {
			f.Close()
			m.rollback(acquired)
			return fmt.Errorf("lock conflict on %q: %w", path, err)
		}

		// Write lock metadata
		f.Truncate(0)
		f.Seek(0, 0)
		fmt.Fprintf(f, "%s %d %s\n", agentID, os.Getpid(), time.Now().Format(time.RFC3339))

		acquired = append(acquired, &lockEntry{path: path, file: f})
		m.held[path] = f
	}

	m.agentPaths[agentID] = append(m.agentPaths[agentID], paths...)
	return nil
}

// Release releases locks held by a specific agent.
func (m *Manager) Release(agentID string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	paths, ok := m.agentPaths[agentID]
	if !ok {
		return
	}

	for _, path := range paths {
		if f, exists := m.held[path]; exists {
			syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
			f.Close()
			os.Remove(m.lockFilePath(path))
			delete(m.held, path)
		}
	}
	delete(m.agentPaths, agentID)
}

// ReleaseAll releases all held locks.
func (m *Manager) ReleaseAll() {
	m.mu.Lock()
	defer m.mu.Unlock()

	for path, f := range m.held {
		syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
		f.Close()
		os.Remove(m.lockFilePath(path))
		delete(m.held, path)
	}
	m.agentPaths = make(map[string][]string)
}

// IsHeld returns true if the given path is currently locked.
func (m *Manager) IsHeld(path string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	_, ok := m.held[path]
	return ok
}

// HeldPaths returns a list of all currently held lock paths.
func (m *Manager) HeldPaths() []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	paths := make([]string, 0, len(m.held))
	for p := range m.held {
		paths = append(paths, p)
	}
	return paths
}

// HasConflict checks if any of the given paths would conflict with currently held locks.
func (m *Manager) HasConflict(paths []string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, p := range paths {
		if _, ok := m.held[p]; ok {
			return true
		}
	}
	return false
}

// CleanStale removes lock files whose holder process is no longer alive.
func (m *Manager) CleanStale() error {
	entries, err := os.ReadDir(m.lockDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("read lock dir: %w", err)
	}

	for _, entry := range entries {
		if !strings.HasSuffix(entry.Name(), ".lock") {
			continue
		}
		lockPath := filepath.Join(m.lockDir, entry.Name())

		// Try to acquire the lock; if successful, it's stale
		f, err := os.OpenFile(lockPath, os.O_RDWR, 0o644)
		if err != nil {
			continue
		}
		err = syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
		if err == nil {
			// Lock acquired -> it was stale, remove it
			syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
			f.Close()
			os.Remove(lockPath)
		} else {
			f.Close()
		}
	}
	return nil
}

type lockEntry struct {
	path string
	file *os.File
}

func (m *Manager) rollback(entries []*lockEntry) {
	for _, e := range entries {
		syscall.Flock(int(e.file.Fd()), syscall.LOCK_UN)
		e.file.Close()
		os.Remove(m.lockFilePath(e.path))
		delete(m.held, e.path)
	}
}
