//go:build linux

package agent

import (
	"bufio"
	"os"
	"strconv"
	"strings"
)

// getAvailableRAMMB returns available RAM in MB on Linux.
// Parses /proc/meminfo for MemAvailable.
func getAvailableRAMMB() int {
	f, err := os.Open("/proc/meminfo")
	if err != nil {
		return 0
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "MemAvailable:") {
			fields := strings.Fields(line)
			if len(fields) < 2 {
				return 0
			}
			kb, err := strconv.Atoi(fields[1])
			if err != nil {
				return 0
			}
			return kb / 1024 // Convert KB to MB
		}
	}
	return 0
}
