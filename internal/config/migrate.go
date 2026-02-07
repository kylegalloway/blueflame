package config

import (
	"fmt"

	"gopkg.in/yaml.v3"
)

const maxSupportedSchemaVersion = 1

// Migrate parses raw YAML bytes, handling schema version migration.
func Migrate(raw []byte) (*Config, error) {
	var base struct {
		SchemaVersion int `yaml:"schema_version"`
	}
	if err := yaml.Unmarshal(raw, &base); err != nil {
		return nil, fmt.Errorf("parse schema_version: %w", err)
	}

	switch {
	case base.SchemaVersion == 0 || base.SchemaVersion == 1:
		return parseV1(raw)
	default:
		return nil, fmt.Errorf("unsupported schema_version %d (max supported: %d)",
			base.SchemaVersion, maxSupportedSchemaVersion)
	}
}

func parseV1(raw []byte) (*Config, error) {
	var cfg Config
	if err := yaml.Unmarshal(raw, &cfg); err != nil {
		return nil, fmt.Errorf("parse config: %w", err)
	}
	cfg.SchemaVersion = 1
	return &cfg, nil
}
