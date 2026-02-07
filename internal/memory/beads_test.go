package memory

import "testing"

func TestNoopProvider(t *testing.T) {
	p := &NoopProvider{}

	err := p.Save(SessionResult{ID: "test"})
	if err != nil {
		t.Errorf("Save: %v", err)
	}

	ctx, err := p.Load()
	if err != nil {
		t.Errorf("Load: %v", err)
	}
	if ctx.SessionCount != 0 {
		t.Errorf("SessionCount = %d, want 0", ctx.SessionCount)
	}
}

func TestProviderInterface(t *testing.T) {
	var _ Provider = &NoopProvider{}
	var _ Provider = &BeadsProvider{}
}
