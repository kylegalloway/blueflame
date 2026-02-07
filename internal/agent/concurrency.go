package agent

import (
	"log"

	"github.com/kylegalloway/blueflame/internal/config"
)

// EffectiveConcurrency returns the actual concurrency to use, potentially
// reduced from the configured value based on available system RAM.
func EffectiveConcurrency(cfg *config.ConcurrencyConfig) int {
	configured := cfg.Development
	if configured < 1 {
		configured = 1
	}

	if !cfg.Adaptive {
		return configured
	}

	minRAMPerAgent := cfg.AdaptiveMinRAMPerAgentMB
	if minRAMPerAgent <= 0 {
		minRAMPerAgent = 512 // default: 512 MB per agent
	}

	availableRAM := getAvailableRAMMB()
	if availableRAM <= 0 {
		log.Printf("Could not determine available RAM; using configured concurrency %d", configured)
		return configured
	}

	maxByRAM := availableRAM / minRAMPerAgent
	if maxByRAM < 1 {
		maxByRAM = 1
	}

	if maxByRAM < configured {
		log.Printf("Reducing concurrency from %d to %d due to available RAM (%d MB)",
			configured, maxByRAM, availableRAM)
		return maxByRAM
	}

	return configured
}
