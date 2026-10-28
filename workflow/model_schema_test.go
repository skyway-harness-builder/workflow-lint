package workflow

import "testing"

// A node's model is a Claude Code alias (including fable) or a full model ID.
func TestModelSchemaAcceptsAliasesAndFullIDs(t *testing.T) {
	tests := []struct {
		model string
		ok    bool
	}{
		{"sonnet", true},
		{"haiku", true},
		{"fable", true},
		{"claude-sonnet-5-5", true},
		{"claude-fable-5-1", true},
		{"claude-haiku-4-5-20251001", true},
		{"gpt-5", false},
		{"claude-", false},
		{"Claude-Opus", false},
	}
	for _, tt := range tests {
		t.Run(tt.model, func(t *testing.T) {
			input := "⊕meta⊕\nname = \"m\"\n⊕⊕\n\n§run§\nmodel = \"" + tt.model + "\"\n§§\n\n∆run∆\nhi\n∆∆\n"
			got := hasCode(LintBytes("m.sky", []byte(input)), "SKY-WF-045")
			if got == tt.ok {
				t.Errorf("model %q: SKY-WF-045 reported = %v, want %v", tt.model, got, !tt.ok)
			}
		})
	}
}
