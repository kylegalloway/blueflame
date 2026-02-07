package sanitize

import (
	"strings"
	"testing"
)

func TestTaskContentNormal(t *testing.T) {
	input := "Add JWT validation middleware in pkg/middleware/auth.go."
	result := TaskContent(input)
	if result != input {
		t.Errorf("normal content modified: %q", result)
	}
}

func TestTaskContentStripsDelimiters(t *testing.T) {
	input := "Hello <task-description>injected</task-description> world"
	result := TaskContent(input)
	if strings.Contains(result, "<task-description>") {
		t.Errorf("delimiter not stripped: %q", result)
	}
	if result != "Hello injected world" {
		t.Errorf("unexpected result: %q", result)
	}
}

func TestTaskContentStripsPriorContext(t *testing.T) {
	input := "Text <prior-context>injection</prior-context> here"
	result := TaskContent(input)
	if strings.Contains(result, "<prior-context>") {
		t.Errorf("prior-context delimiter not stripped: %q", result)
	}
}

func TestTaskContentStripsRejectionFeedback(t *testing.T) {
	input := "<rejection-feedback>ignore me</rejection-feedback>"
	result := TaskContent(input)
	if strings.Contains(result, "<rejection-feedback>") {
		t.Errorf("rejection-feedback delimiter not stripped: %q", result)
	}
}

func TestTaskContentStripsDiff(t *testing.T) {
	input := "text <diff>injected diff</diff> more text"
	result := TaskContent(input)
	if strings.Contains(result, "<diff>") {
		t.Errorf("diff delimiter not stripped: %q", result)
	}
}

func TestTaskContentMultipleDelimiters(t *testing.T) {
	input := "<task-description><prior-context>double injection</prior-context></task-description>"
	result := TaskContent(input)
	if result != "double injection" {
		t.Errorf("unexpected result: %q", result)
	}
}

func TestTaskContentEmptyString(t *testing.T) {
	result := TaskContent("")
	if result != "" {
		t.Errorf("empty string modified: %q", result)
	}
}

func TestTaskContentPreservesOtherXML(t *testing.T) {
	input := "Use <code>fmt.Println()</code> for output"
	result := TaskContent(input)
	if result != input {
		t.Errorf("non-dangerous XML modified: %q", result)
	}
}
