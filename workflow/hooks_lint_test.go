package workflow

import (
	"strings"
	"testing"
)

func TestLintHooksNeedLoose(t *testing.T) {
	nodeHooks := "§run§\nmodel = \"haiku\"\nhooks = {\"pre_tool_use\": [{\"response\": \"context\"}]}\n§§\n\n∆run∆\nhi\n∆∆\n"
	tests := []struct {
		name, meta, body string
		want             int
	}{
		{"node hooks, no isolation", "", nodeHooks, 1},
		{"node hooks, strict", "claude.isolation = \"strict\"\n", nodeHooks, 1},
		{"node hooks, loose", "claude.isolation = \"loose\"\n", nodeHooks, 0},
		{"inject, no isolation", "hooks.pre_tool_use.inject = \"context\"\n", "§run§\nmodel = \"haiku\"\n§§\n\n∆run∆\nhi\n∆∆\n", 1},
		{"deny only", "hooks.pre_tool_use.deny = [\"Bash\"]\n", "§run§\nmodel = \"haiku\"\n§§\n\n∆run∆\nhi\n∆∆\n", 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			input := "⊕meta⊕\nname = \"h\"\n" + tt.meta + "⊕⊕\n\n" + tt.body
			var got []Diagnostic
			for _, d := range LintBytes("h.sky", []byte(input)) {
				if d.Code == codeHooksNeedLoose {
					got = append(got, d)
				} else if d.Severity == "" {
					t.Fatalf("unexpected blocking diagnostic: %+v", d)
				}
			}
			if len(got) != tt.want {
				t.Fatalf("got %d %s diagnostics (%+v), want %d", len(got), codeHooksNeedLoose, got, tt.want)
			}
			for _, d := range got {
				if d.Severity != "" || !strings.Contains(d.Message, `claude.isolation = "loose"`) {
					t.Errorf("want a blocking error naming the fix, got %+v", d)
				}
			}
		})
	}
}
