package workflow

import (
	"strings"
	"testing"

	"github.com/skyway-harness-builder/skyerr"
)

func TestValidateUltraKeywords(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name      string
		wf        *Workflow
		wantCodes []skyerr.Code
		wantNone  bool
	}{
		{
			name:     "nil workflow",
			wf:       nil,
			wantNone: true,
		},
		{
			name: "WF-061 ultraplan in node.prompt lowercase",
			wf: &Workflow{
				Name:  "test",
				Nodes: []Node{{ID: "n1", Prompt: "ultraplan migrate the auth service to JWTs"}},
			},
			wantCodes: []skyerr.Code{skyerr.ErrPromptUltraKeywordNoOp},
		},
		{
			name: "WF-061 ultraplan uppercase",
			wf: &Workflow{
				Name:  "test",
				Nodes: []Node{{ID: "n1", Prompt: "ULTRAPLAN the database schema"}},
			},
			wantCodes: []skyerr.Code{skyerr.ErrPromptUltraKeywordNoOp},
		},
		{
			name: "WF-061 ultraplan mixed case",
			wf: &Workflow{
				Name:  "test",
				Nodes: []Node{{ID: "n1", Prompt: "UltraPlan the API design"}},
			},
			wantCodes: []skyerr.Code{skyerr.ErrPromptUltraKeywordNoOp},
		},
		{
			name: "WF-061 ultrareview in node.prompt",
			wf: &Workflow{
				Name:  "test",
				Nodes: []Node{{ID: "n1", Prompt: "ultrareview the pending changes"}},
			},
			wantCodes: []skyerr.Code{skyerr.ErrPromptUltraKeywordNoOp},
		},
		{
			name: "WF-061 both keywords in same prompt — one issue",
			wf: &Workflow{
				Name:  "test",
				Nodes: []Node{{ID: "n1", Prompt: "ultraplan then ultrareview the result"}},
			},
			wantCodes: []skyerr.Code{skyerr.ErrPromptUltraKeywordNoOp},
		},
		{
			name: "WF-061 ultrathink in node.prompt",
			wf: &Workflow{
				Name:  "test",
				Nodes: []Node{{ID: "n1", Prompt: "Ultrathink about the edge cases first"}},
			},
			wantCodes: []skyerr.Code{skyerr.ErrPromptUltraKeywordNoOp},
		},
		{
			name: "no match — ultrathinking",
			wf: &Workflow{
				Name:  "test",
				Nodes: []Node{{ID: "n1", Prompt: "no ultrathinking needed"}},
			},
			wantNone: true,
		},
		{
			name: "WF-061 ultraplan in system_prompt",
			wf: &Workflow{
				Name:  "test",
				Nodes: []Node{{ID: "n1", Prompt: "do something", SystemPrompt: "ultraplan each step"}},
			},
			wantCodes: []skyerr.Code{skyerr.ErrPromptUltraKeywordNoOp},
		},
		{
			name: "WF-061 ultraplan in step.prompt (legacy format)",
			wf: &Workflow{
				Name:  "test",
				Steps: []Step{{Name: "s1", Prompt: "ultraplan the migration"}},
			},
			wantCodes: []skyerr.Code{skyerr.ErrPromptUltraKeywordNoOp},
		},
		{
			name: "no match — embedded substring myultraplanner",
			wf: &Workflow{
				Name:  "test",
				Nodes: []Node{{ID: "n1", Prompt: "run myultraplanner and check results"}},
			},
			wantNone: true,
		},
		{
			name: "no match — script body contains ultraplan (skip script nodes)",
			wf: &Workflow{
				Name: "test",
				Nodes: []Node{{
					ID:     "n1",
					Prompt: "const keyword = 'ultraplan'; console.log(keyword);",
					Script: &ScriptConfig{Runtime: "bun"},
				}},
			},
			wantNone: true,
		},
		{
			name: "no match — wait.prompt contains ultraplan (human-facing, not sent to claude)",
			wf: &Workflow{
				Name:  "test",
				Nodes: []Node{{ID: "n1", Wait: &WaitConfig{Prompt: "ultraplan approved?"}}},
			},
			wantNone: true,
		},
		{
			name: "no match — approval.prompt contains ultraplan",
			wf: &Workflow{
				Name:  "test",
				Nodes: []Node{{ID: "n1", Approval: &ApprovalConfig{Prompt: "ultraplan the approach before approving"}}},
			},
			wantNone: true,
		},
		{
			name: "no match — plain prompt with no keywords",
			wf: &Workflow{
				Name:  "test",
				Nodes: []Node{{ID: "n1", Prompt: "migrate the auth service to JWTs"}},
			},
			wantNone: true,
		},
		{
			name: "multiple nodes — only one has keyword",
			wf: &Workflow{
				Name: "test",
				Nodes: []Node{
					{ID: "n1", Prompt: "analyse the codebase"},
					{ID: "n2", Prompt: "ultraplan the refactor"},
					{ID: "n3", Prompt: "write a summary"},
				},
			},
			wantCodes: []skyerr.Code{skyerr.ErrPromptUltraKeywordNoOp},
		},
		{
			name: "multiple nodes — both have keywords — two issues",
			wf: &Workflow{
				Name: "test",
				Nodes: []Node{
					{ID: "n1", Prompt: "ultraplan the migration"},
					{ID: "n2", Prompt: "ultrareview the result"},
				},
			},
			wantCodes: []skyerr.Code{skyerr.ErrPromptUltraKeywordNoOp, skyerr.ErrPromptUltraKeywordNoOp},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			issues := ValidateUltraKeywords(tc.wf)
			if tc.wantNone {
				if len(issues) != 0 {
					t.Errorf("expected no issues, got %d: %v", len(issues), issues)
				}
				return
			}
			if len(issues) != len(tc.wantCodes) {
				t.Fatalf("issue count=%d want %d; issues=%v", len(issues), len(tc.wantCodes), issues)
			}
			got := make(map[skyerr.Code]int, len(issues))
			for _, iss := range issues {
				got[iss.Code]++
			}
			for _, code := range tc.wantCodes {
				if got[code] == 0 {
					t.Errorf("expected code %q not found in issues: %v", code, issues)
				} else {
					got[code]--
				}
			}
		})
	}
}

// The message names every keyword found and no longer claims the keywords are
// inert: under skyway's stream-json invocation they fire.
func TestScanUltra_Message(t *testing.T) {
	t.Parallel()
	iss, ok := scanUltra("ultrathink, then ultraplan, then ULTRAREVIEW and ultrathink again", "n1", "/nodes/n1/prompt")
	if !ok {
		t.Fatal("want an issue")
	}
	if want := "'ultrathink', 'ultraplan' and 'ultrareview'"; !strings.Contains(iss.Message, want) {
		t.Errorf("message %q should list %s", iss.Message, want)
	}
	for _, stale := range []string{"no effect", "non-interactive"} {
		if strings.Contains(iss.Message, stale) {
			t.Errorf("message %q still says %q", iss.Message, stale)
		}
	}
	if !strings.Contains(iss.Message, "stays active under 'skyway run'") {
		t.Errorf("message %q should say the keyword stays active", iss.Message)
	}
}
