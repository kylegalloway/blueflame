package agent

import (
	"encoding/json"
	"testing"
)

func TestParseValidatorOutputPass(t *testing.T) {
	data := []byte(`{"status":"pass","notes":"looks good","issues":[]}`)
	out, err := ParseValidatorOutput(data)
	if err != nil {
		t.Fatalf("ParseValidatorOutput: %v", err)
	}
	if out.Status != "pass" {
		t.Errorf("status = %q, want %q", out.Status, "pass")
	}
}

func TestParseValidatorOutputFail(t *testing.T) {
	data := []byte(`{"status":"fail","notes":"tests broken","issues":["test_auth failed","missing error handling"]}`)
	out, err := ParseValidatorOutput(data)
	if err != nil {
		t.Fatalf("ParseValidatorOutput: %v", err)
	}
	if out.Status != "fail" {
		t.Errorf("status = %q, want %q", out.Status, "fail")
	}
	if len(out.Issues) != 2 {
		t.Errorf("len(issues) = %d, want 2", len(out.Issues))
	}
}

func TestParseValidatorOutputInvalidStatus(t *testing.T) {
	data := []byte(`{"status":"maybe","notes":"not sure"}`)
	_, err := ParseValidatorOutput(data)
	if err == nil {
		t.Error("expected error for invalid status")
	}
}

func TestParseValidatorOutputInvalidJSON(t *testing.T) {
	data := []byte(`not json`)
	_, err := ParseValidatorOutput(data)
	if err == nil {
		t.Error("expected error for invalid JSON")
	}
}

func TestParsePlannerOutput(t *testing.T) {
	data := []byte(`{
		"tasks": [
			{
				"id": "task-001",
				"title": "Add JWT middleware",
				"description": "Create JWT validation",
				"priority": 1,
				"cohesion_group": "auth",
				"dependencies": [],
				"file_locks": ["pkg/middleware/"]
			}
		]
	}`)
	out, err := ParsePlannerOutput(data)
	if err != nil {
		t.Fatalf("ParsePlannerOutput: %v", err)
	}
	if len(out.Tasks) != 1 {
		t.Errorf("len(tasks) = %d, want 1", len(out.Tasks))
	}
	if out.Tasks[0].ID != "task-001" {
		t.Errorf("task.id = %q, want %q", out.Tasks[0].ID, "task-001")
	}
}

func TestParsePlannerOutputEmpty(t *testing.T) {
	data := []byte(`{"tasks":[]}`)
	_, err := ParsePlannerOutput(data)
	if err == nil {
		t.Error("expected error for empty tasks")
	}
}

func TestParsePlannerOutputInvalidJSON(t *testing.T) {
	data := []byte(`not json`)
	_, err := ParsePlannerOutput(data)
	if err == nil {
		t.Error("expected error for invalid JSON")
	}
}

func TestParsePlannerOutputFromClaudeEnvelope(t *testing.T) {
	// Simulate claude --output-format json wrapping the planner's response
	data := []byte(`{"result":"{\"tasks\":[{\"id\":\"task-001\",\"title\":\"Setup\",\"description\":\"Init project\",\"priority\":1,\"dependencies\":[],\"file_locks\":[\"main.go\"]}]}","cost_usd":0.01,"is_error":false,"duration_ms":5000,"num_turns":1}`)
	out, err := ParsePlannerOutput(data)
	if err != nil {
		t.Fatalf("ParsePlannerOutput from envelope: %v", err)
	}
	if len(out.Tasks) != 1 {
		t.Errorf("len(tasks) = %d, want 1", len(out.Tasks))
	}
	if out.Tasks[0].ID != "task-001" {
		t.Errorf("task.id = %q, want %q", out.Tasks[0].ID, "task-001")
	}
}

func TestParsePlannerOutputFromMarkdownCodeBlock(t *testing.T) {
	data := []byte("```json\n{\"tasks\":[{\"id\":\"task-001\",\"title\":\"Setup\",\"description\":\"Init project\",\"priority\":1,\"dependencies\":[],\"file_locks\":[\"main.go\"]}]}\n```")
	out, err := ParsePlannerOutput(data)
	if err != nil {
		t.Fatalf("ParsePlannerOutput from code block: %v", err)
	}
	if len(out.Tasks) != 1 {
		t.Errorf("len(tasks) = %d, want 1", len(out.Tasks))
	}
}

func TestParsePlannerOutputFromEnvelopeWithCodeBlock(t *testing.T) {
	// Claude envelope where the result field contains markdown-wrapped JSON
	inner := "```json\n{\"tasks\":[{\"id\":\"t1\",\"title\":\"Do thing\",\"description\":\"desc\",\"priority\":1,\"dependencies\":[],\"file_locks\":[]}]}\n```"
	data := []byte(`{"result":` + mustJSON(inner) + `,"cost_usd":0.02}`)
	out, err := ParsePlannerOutput(data)
	if err != nil {
		t.Fatalf("ParsePlannerOutput from envelope+code block: %v", err)
	}
	if len(out.Tasks) != 1 {
		t.Errorf("len(tasks) = %d, want 1", len(out.Tasks))
	}
}

func TestParseValidatorOutputFromClaudeEnvelope(t *testing.T) {
	data := []byte(`{"result":"{\"status\":\"pass\",\"notes\":\"all good\"}","cost_usd":0.005}`)
	out, err := ParseValidatorOutput(data)
	if err != nil {
		t.Fatalf("ParseValidatorOutput from envelope: %v", err)
	}
	if out.Status != "pass" {
		t.Errorf("status = %q, want %q", out.Status, "pass")
	}
}

func TestExtractResultJSON_DirectJSON(t *testing.T) {
	data := []byte(`{"tasks":[]}`)
	got := string(extractResultJSON(data))
	if got != `{"tasks":[]}` {
		t.Errorf("extractResultJSON = %q, want %q", got, `{"tasks":[]}`)
	}
}

func TestParsePlannerOutputFromNewCLIEnvelope(t *testing.T) {
	// New claude CLI format with type, total_cost_usd, usage, etc.
	inner := `{"tasks":[{"id":"task-001","title":"Setup","description":"Init project","priority":1,"dependencies":[],"file_locks":["main.go"]}]}`
	data := []byte(`{"type":"result","subtype":"success","is_error":false,"result":` + mustJSON(inner) + `,"total_cost_usd":0.05,"duration_ms":3000,"num_turns":1,"usage":{"input_tokens":100,"output_tokens":200},"session_id":"abc123"}`)
	out, err := ParsePlannerOutput(data)
	if err != nil {
		t.Fatalf("ParsePlannerOutput from new CLI envelope: %v", err)
	}
	if len(out.Tasks) != 1 {
		t.Errorf("len(tasks) = %d, want 1", len(out.Tasks))
	}
	if out.Tasks[0].ID != "task-001" {
		t.Errorf("task.id = %q, want %q", out.Tasks[0].ID, "task-001")
	}
}

func TestParsePlannerOutputFromNewCLIEnvelopeWithCodeBlock(t *testing.T) {
	// New CLI format where result contains markdown code block
	inner := "```json\n{\"tasks\":[{\"id\":\"t1\",\"title\":\"Do thing\",\"description\":\"desc\",\"priority\":1,\"dependencies\":[],\"file_locks\":[]}]}\n```"
	data := []byte(`{"type":"result","subtype":"success","is_error":false,"result":` + mustJSON(inner) + `,"total_cost_usd":0.02}`)
	out, err := ParsePlannerOutput(data)
	if err != nil {
		t.Fatalf("ParsePlannerOutput from new CLI envelope+code block: %v", err)
	}
	if len(out.Tasks) != 1 {
		t.Errorf("len(tasks) = %d, want 1", len(out.Tasks))
	}
}

func TestParsePlannerOutputFromTextWithEmbeddedCodeBlock(t *testing.T) {
	// Planner returns natural language with embedded JSON code block
	text := "Based on the requirements, here is the task breakdown:\n\n```json\n{\"tasks\":[{\"id\":\"task-001\",\"title\":\"Setup\",\"description\":\"Init project\",\"priority\":1,\"dependencies\":[],\"file_locks\":[\"main.go\"]}]}\n```\n\nThis plan covers all the requirements."
	data := []byte(`{"type":"result","result":` + mustJSON(text) + `,"total_cost_usd":0.03}`)
	out, err := ParsePlannerOutput(data)
	if err != nil {
		t.Fatalf("ParsePlannerOutput from text with embedded code block: %v", err)
	}
	if len(out.Tasks) != 1 {
		t.Errorf("len(tasks) = %d, want 1", len(out.Tasks))
	}
}

func TestParsePlannerOutputFromTextWithEmbeddedJSON(t *testing.T) {
	// Planner returns natural language with raw JSON (no code block)
	text := "Based on the requirements, here is the plan:\n\n{\"tasks\":[{\"id\":\"task-001\",\"title\":\"Setup\",\"description\":\"Init project\",\"priority\":1,\"dependencies\":[],\"file_locks\":[\"main.go\"]}]}\n\nLet me know if you need changes."
	data := []byte(`{"type":"result","result":` + mustJSON(text) + `,"total_cost_usd":0.03}`)
	out, err := ParsePlannerOutput(data)
	if err != nil {
		t.Fatalf("ParsePlannerOutput from text with embedded JSON: %v", err)
	}
	if len(out.Tasks) != 1 {
		t.Errorf("len(tasks) = %d, want 1", len(out.Tasks))
	}
}

func TestExtractEmbeddedCodeBlock(t *testing.T) {
	input := []byte("Here is the result:\n\n```json\n{\"key\": \"value\"}\n```\n\nDone.")
	got := extractEmbeddedCodeBlock(input)
	if got == nil {
		t.Fatal("extractEmbeddedCodeBlock returned nil")
	}
	if string(got) != `{"key": "value"}` {
		t.Errorf("got %q, want %q", string(got), `{"key": "value"}`)
	}
}

func TestExtractFirstJSONObject(t *testing.T) {
	input := []byte("Some text before {\"key\": \"value\"} and after")
	got := extractFirstJSONObject(input)
	if got == nil {
		t.Fatal("extractFirstJSONObject returned nil")
	}
	if string(got) != `{"key": "value"}` {
		t.Errorf("got %q, want %q", string(got), `{"key": "value"}`)
	}
}

func TestExtractFirstJSONObject_NoJSON(t *testing.T) {
	input := []byte("No JSON here at all")
	got := extractFirstJSONObject(input)
	if got != nil {
		t.Errorf("expected nil, got %q", string(got))
	}
}

func mustJSON(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}
