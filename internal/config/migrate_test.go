package config

import (
	"testing"
)

func TestMigrateV1(t *testing.T) {
	raw := []byte(`
schema_version: 1
project:
  name: "test"
  repo: "/tmp"
`)
	cfg, err := Migrate(raw)
	if err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	if cfg.SchemaVersion != 1 {
		t.Errorf("schema_version = %d, want 1", cfg.SchemaVersion)
	}
	if cfg.Project.Name != "test" {
		t.Errorf("project.name = %q, want %q", cfg.Project.Name, "test")
	}
}

func TestMigrateNoVersion(t *testing.T) {
	raw := []byte(`
project:
  name: "test"
  repo: "/tmp"
`)
	cfg, err := Migrate(raw)
	if err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	if cfg.SchemaVersion != 1 {
		t.Errorf("schema_version = %d, want 1 (default for missing version)", cfg.SchemaVersion)
	}
}

func TestMigrateUnsupportedVersion(t *testing.T) {
	raw := []byte(`
schema_version: 99
project:
  name: "test"
`)
	_, err := Migrate(raw)
	if err == nil {
		t.Fatal("expected error for unsupported schema version")
	}
}

func TestMigrateInvalidYAML(t *testing.T) {
	raw := []byte(`{{{invalid yaml}}}`)
	_, err := Migrate(raw)
	if err == nil {
		t.Fatal("expected error for invalid YAML")
	}
}
