package workflow

import "fmt"

const codeThinkingRejected = "SKY-WF-115"

// lintThinkingModes flags thinking configs that current Claude models reject
// with HTTP 400 (SKY-WF-115, warning). The config is parsed but not yet
// forwarded to the CLI (#145), so this warns ahead of that wiring instead of
// failing parse for workflows that target older models:
//
//   - mode "enabled" (fixed budget_tokens): rejected by Fable 5.x, Opus 4.7+,
//     Sonnet 5.x and Haiku 5.5. Use mode "adaptive" and set effort.
//   - mode "disabled": rejected by Fable 5.x, Opus 5.5 and Sonnet 5.5, and by
//     Haiku 5.5 and Opus 5 above effort "high". Lower effort instead.
func lintThinkingModes(wf *Workflow) []Diagnostic {
	var diags []Diagnostic
	for i := range wf.Nodes {
		n := &wf.Nodes[i]
		if n.Thinking == nil {
			continue
		}
		var msg string
		switch n.Thinking.Mode {
		case "enabled":
			msg = `thinking.mode "enabled" (budget_tokens) is rejected by current Claude models (Fable 5.x, Opus 4.7+, Sonnet 5.x, Haiku 5.5); use mode "adaptive" and set effort`
		case "disabled":
			msg = `thinking.mode "disabled" is rejected by Claude Fable 5.x, Opus 5.5 and Sonnet 5.5, and by Haiku 5.5 and Opus 5 above effort "high"; lower effort instead`
		default:
			continue
		}
		diags = append(diags, Diagnostic{
			Code:     codeThinkingRejected,
			Severity: "warning",
			Message:  fmt.Sprintf("node %q: %s", n.ID, msg),
		})
	}
	return diags
}
