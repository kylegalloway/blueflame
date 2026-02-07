package agent

import (
	"testing"

	"github.com/kylegalloway/blueflame/internal/config"
)

func TestEffectiveConcurrencyNotAdaptive(t *testing.T) {
	cfg := &config.ConcurrencyConfig{
		Development: 4,
		Adaptive:    false,
	}
	got := EffectiveConcurrency(cfg)
	if got != 4 {
		t.Errorf("EffectiveConcurrency = %d, want 4", got)
	}
}

func TestEffectiveConcurrencyAdaptive(t *testing.T) {
	cfg := &config.ConcurrencyConfig{
		Development:            4,
		Adaptive:               true,
		AdaptiveMinRAMPerAgentMB: 512,
	}

	got := EffectiveConcurrency(cfg)
	// We can't predict the exact value, but it should be >= 1 and <= 4
	if got < 1 || got > 4 {
		t.Errorf("EffectiveConcurrency = %d, want 1-4", got)
	}
}

func TestEffectiveConcurrencyMinimum(t *testing.T) {
	cfg := &config.ConcurrencyConfig{
		Development: 0,
		Adaptive:    false,
	}
	got := EffectiveConcurrency(cfg)
	if got != 1 {
		t.Errorf("EffectiveConcurrency = %d, want 1 (minimum)", got)
	}
}

func TestGetAvailableRAMMB(t *testing.T) {
	ram := getAvailableRAMMB()
	// Should return some positive value on any real machine
	if ram <= 0 {
		t.Errorf("getAvailableRAMMB = %d, want > 0", ram)
	}
	t.Logf("Available RAM: %d MB", ram)
}
