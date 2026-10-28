package workflow

import (
	"strings"
	"testing"
)

func TestValidateSchema_Valid(t *testing.T) {
	wf := &Workflow{
		Name: "ok",
		Trigger: Trigger{
			GitHub: &GitHubTrigger{Events: []string{"issues.labeled"}},
		},
		Nodes: []Node{
			{ID: "plan", Command: "plan", Model: "opus"},
			{ID: "apply", Command: "apply", DependsOn: []string{"plan"}, Model: "sonnet"},
		},
	}
	issues, err := ValidateSchema(wf)
	if err != nil {
		t.Fatalf("ValidateSchema returned setup error: %v", err)
	}
	if len(issues) != 0 {
		t.Fatalf("expected no schema issues, got %d: %+v", len(issues), issues)
	}
}

func TestValidateSchema_InvalidModel(t *testing.T) {
	wf := &Workflow{
		Name: "bad-model",
		Trigger: Trigger{
			GitHub: &GitHubTrigger{Events: []string{"issues.labeled"}},
		},
		Nodes: []Node{
			{ID: "first", Command: "plan"},
			{ID: "second", Command: "apply", Model: "gpt-4"},
		},
	}
	issues, err := ValidateSchema(wf)
	if err != nil {
		t.Fatalf("ValidateSchema returned setup error: %v", err)
	}
	if len(issues) == 0 {
		t.Fatal("expected at least one schema issue for invalid model, got none")
	}
	// The model accepts an alias enum or a full-ID pattern, so an invalid value
	// yields one issue per branch; at least one must name the allowed values.
	var found, mentioned bool
	for _, si := range issues {
		if si.NodeID != "second" {
			continue
		}
		found = true
		if !strings.Contains(si.Pointer, "/nodes/1/model") {
			t.Errorf("pointer %q should reference /nodes/1/model", si.Pointer)
		}
		msg := FormatSchemaIssue(si)
		if !strings.HasPrefix(msg, "node second:") {
			t.Errorf("formatted issue %q should start with 'node second:'", msg)
		}
		lower := strings.ToLower(si.Message)
		if strings.Contains(lower, "sonnet") || strings.Contains(lower, "enum") || strings.Contains(lower, "gpt-4") {
			mentioned = true
		}
	}
	if !found {
		t.Fatalf("expected issue attributed to node 'second', got %+v", issues)
	}
	if !mentioned {
		t.Errorf("no issue for node 'second' mentions the allowed models or the invalid value: %+v", issues)
	}
}

func TestValidateSchema_MissingHTTPURL(t *testing.T) {
	// http.url is schema-required; parser also rejects it but schema must catch it too.
	wf := &Workflow{
		Name:    "missing-url",
		Trigger: Trigger{GitHub: &GitHubTrigger{Events: []string{"a"}}},
		Nodes: []Node{
			{ID: "notify", HTTP: &HTTPConfig{Method: "POST"}},
		},
	}
	issues, err := ValidateSchema(wf)
	if err != nil {
		t.Fatalf("setup error: %v", err)
	}
	if len(issues) == 0 {
		t.Fatal("expected schema issue for missing http.url")
	}
	var found bool
	for _, si := range issues {
		if si.NodeID == "notify" && strings.Contains(si.Pointer, "/nodes/0/http") {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected issue on node 'notify' http config, got %+v", issues)
	}
}
