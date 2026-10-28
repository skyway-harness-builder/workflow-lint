package workflow

import (
	"strings"
	"testing"
)

func TestLintThinkingModes(t *testing.T) {
	tests := []struct {
		name     string
		thinking string
		wantWarn string // substring of the SKY-WF-115 message; "" means no warning
	}{
		{"enabled", `thinking = {"mode": "enabled", "budget_tokens": 4000}`, `"enabled" (budget_tokens) is rejected`},
		{"disabled", `thinking = {"mode": "disabled"}`, `"disabled" is rejected`},
		{"adaptive", `thinking = {"mode": "adaptive"}`, ""},
		{"unset", ``, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			input := "⊕meta⊕\nname = \"think\"\n⊕⊕\n\n§run§\nmodel = \"sonnet\"\n" + tt.thinking + "\n§§\n\n∆run∆\nhi\n∆∆\n"
			diags := LintBytes("think.sky", []byte(input))

			var got []Diagnostic
			for _, d := range diags {
				if d.Code == codeThinkingRejected {
					got = append(got, d)
				} else if d.Severity == "" {
					t.Fatalf("unexpected blocking diagnostic: %+v", d)
				}
			}
			if tt.wantWarn == "" {
				if len(got) != 0 {
					t.Fatalf("got %+v, want no %s", got, codeThinkingRejected)
				}
				return
			}
			if len(got) != 1 || got[0].Severity != "warning" || !strings.Contains(got[0].Message, tt.wantWarn) {
				t.Fatalf("got %+v, want one %s warning containing %q", got, codeThinkingRejected, tt.wantWarn)
			}
		})
	}
}
