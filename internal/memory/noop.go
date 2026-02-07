package memory

// NoopProvider implements Provider as a no-op (when beads is disabled).
type NoopProvider struct{}

func (m *NoopProvider) Save(session SessionResult) error {
	return nil
}

func (m *NoopProvider) Load() (SessionContext, error) {
	return SessionContext{}, nil
}
