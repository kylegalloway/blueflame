package agent

import "testing"

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
