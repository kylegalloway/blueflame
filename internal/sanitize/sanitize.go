package sanitize

import "strings"

// delimiters that must not appear in user-provided task content.
var dangerousDelimiters = []string{
	"<task-description>",
	"</task-description>",
	"<prior-context>",
	"</prior-context>",
	"<rejection-feedback>",
	"</rejection-feedback>",
	"<diff>",
	"</diff>",
}

// TaskContent sanitizes task description content to prevent prompt injection.
// It strips any XML-like delimiters that the prompt templates use to separate
// trusted instructions from untrusted data.
func TaskContent(content string) string {
	result := content
	for _, delim := range dangerousDelimiters {
		result = strings.ReplaceAll(result, delim, "")
	}
	return result
}
