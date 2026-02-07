//go:build darwin

package agent

import (
	"syscall"
	"unsafe"
)

// getAvailableRAMMB returns available RAM in MB on macOS.
// Uses sysctlbyname to query hw.memsize (total) and vm.page_free_count * vm.pagesize (free).
func getAvailableRAMMB() int {
	// Get total memory
	totalMem, err := sysctlUint64("hw.memsize")
	if err != nil {
		return 0
	}

	// Get page size
	pageSize, err := sysctlUint64("hw.pagesize")
	if err != nil {
		pageSize = 4096 // default page size
	}

	// Get free page count
	freePages, err := sysctlUint64("vm.page_free_count")
	if err != nil {
		// Fallback: estimate 50% of total as available
		return int(totalMem / (2 * 1024 * 1024))
	}

	freeMB := int(freePages * pageSize / (1024 * 1024))
	if freeMB == 0 {
		// Fallback to fraction of total
		return int(totalMem / (4 * 1024 * 1024))
	}
	return freeMB
}

func sysctlUint64(name string) (uint64, error) {
	val, err := syscall.Sysctl(name)
	if err != nil {
		// Try as uint32 via SysctlUint32
		val32, err := syscall.SysctlUint32(name)
		if err != nil {
			return 0, err
		}
		return uint64(val32), nil
	}
	// syscall.Sysctl returns a string; for numeric values, interpret as bytes
	if len(val) >= 8 {
		return *(*uint64)(unsafe.Pointer(&[]byte(val)[0])), nil
	}
	if len(val) >= 4 {
		return uint64(*(*uint32)(unsafe.Pointer(&[]byte(val)[0]))), nil
	}
	return 0, syscall.EINVAL
}
