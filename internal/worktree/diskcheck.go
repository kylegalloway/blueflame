package worktree

import (
	"fmt"
	"syscall"
)

// MinDiskSpaceMB is the minimum free disk space required before creating a worktree.
const MinDiskSpaceMB = 500

// CheckDiskSpace checks if the given path has at least minMB megabytes of free space.
func CheckDiskSpace(path string, minMB int) error {
	var stat syscall.Statfs_t
	if err := syscall.Statfs(path, &stat); err != nil {
		return fmt.Errorf("statfs %s: %w", path, err)
	}

	availableBytes := stat.Bavail * uint64(stat.Bsize)
	availableMB := availableBytes / (1024 * 1024)

	if int(availableMB) < minMB {
		return fmt.Errorf("insufficient disk space: %d MB available, %d MB required at %s",
			availableMB, minMB, path)
	}
	return nil
}
