package workflow

import (
	"fmt"
	"strings"
)

const codeWhenInvalid = "SKY-WF-119"

// EvaluateCondition parses a "when" condition against a strict grammar and
// returns the evaluation result plus any parse error. skyway's runner calls it
// on the condition after resolving $node.output references, and lint calls it
// through lintWhenConditions, so both enforce the same grammar.
//
// Grammar (whitespace around tokens tolerated):
//
//	CONDITION  := COMPARISON | TRUTHY
//	COMPARISON := LHS OP QUOTED
//	OP         := "==" | "!="
//	QUOTED     := '...' | "..." (inner quote character of the same kind not allowed)
//	TRUTHY     := "" | "0" | "1" | "true" | "false"
//
// LHS is captured as-is after TrimSpace. Malformed input returns an error; the
// runner fails a node whose condition does not parse.
func EvaluateCondition(condition string) (bool, error) {
	condition = strings.TrimSpace(condition)

	// Comparison shape: scan for a trailing quoted literal. If one is present,
	// the text preceding it (after trimming) must end in "==" or "!=". Everything
	// before the operator is the LHS. This order (RHS first) avoids mis-splitting
	// on operator substrings that appear inside LHS or RHS values after
	// placeholder substitution.
	if right, rightStart, ok := extractTrailingQuoted(condition); ok {
		prefix := strings.TrimSpace(condition[:rightStart])
		switch {
		case strings.HasSuffix(prefix, "=="):
			left := strings.TrimSpace(strings.TrimSuffix(prefix, "=="))
			return left == right, nil
		case strings.HasSuffix(prefix, "!="):
			left := strings.TrimSpace(strings.TrimSuffix(prefix, "!="))
			return left != right, nil
		}
		return false, fmt.Errorf("condition %q: quoted right-hand side must follow == or !=", condition)
	}

	switch condition {
	case "", "0", "false":
		return false, nil
	case "1", "true":
		return true, nil
	}
	return false, fmt.Errorf("condition %q: must be \"lhs == 'value'\", \"lhs != 'value'\", or a boolean literal (true/false/0/1/empty)", condition)
}

// extractTrailingQuoted returns the inner text of a well-formed quoted string at
// the end of s, the index where that quoted literal starts, and whether one was
// found. The opening quote must match the closing quote; the inner text may not
// contain the quote character (no escape syntax).
func extractTrailingQuoted(s string) (inner string, start int, ok bool) {
	if len(s) < 2 {
		return "", 0, false
	}
	closing := s[len(s)-1]
	if closing != '\'' && closing != '"' {
		return "", 0, false
	}
	// Find the matching opening quote — the first char in s that equals `closing`,
	// scanning from the left. If any additional `closing` char appears between
	// that opening quote and the final char, the literal is ambiguous — reject.
	openIdx := strings.IndexByte(s[:len(s)-1], closing)
	if openIdx < 0 {
		return "", 0, false
	}
	innerText := s[openIdx+1 : len(s)-1]
	if strings.IndexByte(innerText, closing) >= 0 {
		return "", 0, false
	}
	return innerText, openIdx, true
}

// whenProbe stands in for every $node.output reference when lint checks a
// condition's shape. It has no quotes, operators or spaces, so it cannot change
// how the expression parses; only the run-time output can.
const whenProbe = "x"

// whenProblem returns why a node's when condition can never evaluate, or "" if
// it can. It runs the runtime grammar (EvaluateCondition) on the condition with
// every output reference replaced by whenProbe. A condition that is a single
// output reference is accepted: its output decides at run time.
func whenProblem(when string) string {
	trimmed := strings.TrimSpace(when)
	if loc := OutputRefRe.FindStringIndex(trimmed); loc != nil && loc[0] == 0 && loc[1] == len(trimmed) {
		return ""
	}
	probe := OutputRefRe.ReplaceAllLiteralString(trimmed, whenProbe)
	if _, err := EvaluateCondition(probe); err == nil {
		return ""
	}
	if _, _, ok := extractTrailingQuoted(probe); ok {
		return "a quoted value must follow == or !="
	}
	if strings.Contains(probe, "==") || strings.Contains(probe, "!=") {
		return "the right-hand side must be a quoted value, such as 'done'"
	}
	return "expected $node.output == 'value', $node.output != 'value', or true, false, 1, 0 or empty"
}

// lintWhenConditions reports SKY-WF-119 (error) for each node whose when
// condition can never evaluate. skyway fails such a node at run time with
// "malformed when-condition", so lint catches it first.
func lintWhenConditions(wf *Workflow) []Diagnostic {
	var diags []Diagnostic
	for i := range wf.Nodes {
		n := &wf.Nodes[i]
		if n.When == "" {
			continue
		}
		if problem := whenProblem(n.When); problem != "" {
			diags = append(diags, Diagnostic{
				Code:    codeWhenInvalid,
				Message: fmt.Sprintf("node %q: when %q can never evaluate: %s; skyway fails the node at run time", n.ID, n.When, problem),
			})
		}
	}
	return diags
}
