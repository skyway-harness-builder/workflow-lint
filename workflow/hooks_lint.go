package workflow

import "fmt"

const codeHooksNeedLoose = "SKY-WF-117"

// lintHooksNeedLoose flags node hooks and hooks.pre_tool_use.inject in a
// workflow that does not declare claude.isolation = "loose" (SKY-WF-117,
// error). Strict isolation runs claude with --bare, which skips SDK-registered
// hooks, and these have no CLI flag equivalent (deny lists do:
// --disallowed-tools), so skyway refuses such a run under strict. The
// daemon defaults to strict, so the workflow must opt into loose itself, the
// same rule SKY-WF-059 applies to permissions = "interactive".
func lintHooksNeedLoose(wf *Workflow) []Diagnostic {
	if wf.Claude.Isolation == "loose" {
		return nil
	}
	const fix = `; strict isolation (--bare) skips them, so set claude.isolation = "loose" in ⊕meta⊕`
	var diags []Diagnostic
	for i := range wf.Nodes {
		if len(wf.Nodes[i].Hooks) > 0 {
			diags = append(diags, Diagnostic{
				Code:    codeHooksNeedLoose,
				Message: fmt.Sprintf("node %q: hooks need loose isolation%s", wf.Nodes[i].ID, fix),
			})
		}
	}
	if wf.Hooks.PreToolUse != nil && wf.Hooks.PreToolUse.Inject != "" {
		diags = append(diags, Diagnostic{
			Code:    codeHooksNeedLoose,
			Message: "hooks.pre_tool_use.inject needs loose isolation" + fix,
		})
	}
	return diags
}
