package agent

import "testing"

func TestMatchesAnyGlob(t *testing.T) {
	tests := []struct {
		path     string
		patterns []string
		want     bool
	}{
		{"src/main.go", []string{"src/**"}, true},   // ** matches via filepath.Match on base
		{"src/main.go", []string{"src/*.go"}, true},
		{".env", []string{".env*"}, true},
		{".env.local", []string{".env*"}, true},
		{"pkg/auth/handler.go", []string{"*.secret"}, false},
		{"credentials.secret", []string{"*.secret"}, true},
		{"foo/bar.go", []string{}, false},
	}

	for _, tt := range tests {
		got := matchesAnyGlob(tt.path, tt.patterns)
		if got != tt.want {
			t.Errorf("matchesAnyGlob(%q, %v) = %v, want %v", tt.path, tt.patterns, got, tt.want)
		}
	}
}

func TestWithinFileLocks(t *testing.T) {
	tests := []struct {
		path      string
		fileLocks []string
		want      bool
	}{
		{"pkg/middleware/auth.go", []string{"pkg/middleware/"}, true},
		{"pkg/middleware/nested/deep.go", []string{"pkg/middleware/"}, true},
		{"pkg/other/file.go", []string{"pkg/middleware/"}, false},
		{"internal/auth/handler.go", []string{"internal/auth/"}, true},
		{"internal/auth/handler.go", []string{"internal/other/"}, false},
		{"exact_file.go", []string{"exact_file.go"}, true},
		{"other_file.go", []string{"exact_file.go"}, false},
	}

	for _, tt := range tests {
		got := withinFileLocks(tt.path, tt.fileLocks)
		if got != tt.want {
			t.Errorf("withinFileLocks(%q, %v) = %v, want %v", tt.path, tt.fileLocks, got, tt.want)
		}
	}
}

func TestPostCheckResultViolations(t *testing.T) {
	result := &PostCheckResult{Pass: true}
	if !result.Pass {
		t.Error("initial state should be pass")
	}

	result.AddViolation("blocked_path_modified", ".env")
	if result.Pass {
		t.Error("should be fail after adding violation")
	}
	if len(result.Violations) != 1 {
		t.Errorf("len(violations) = %d, want 1", len(result.Violations))
	}
	if result.Violations[0].Type != "blocked_path_modified" {
		t.Errorf("type = %q", result.Violations[0].Type)
	}
}
