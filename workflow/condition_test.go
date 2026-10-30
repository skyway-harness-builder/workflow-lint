package workflow

import (
	"strings"
	"testing"

	"github.com/skyway-harness-builder/skyerr"
)

// Same table skyway's runner has carried for evaluateCondition, now that the
// grammar lives here.
func TestEvaluateCondition(t *testing.T) {
	t.Parallel()
	tests := []struct {
		cond    string
		want    bool
		wantErr bool
	}{
		{cond: "bug == 'bug'", want: true},
		{cond: "bug == 'feature'", want: false},
		{cond: "bug != 'feature'", want: true},
		{cond: "bug != 'bug'", want: false},
		{cond: `bug == "bug"`, want: true},
		{cond: `msg == "a==b"`, want: false},
		{cond: `a==b == "a==b"`, want: true},
		{cond: "", want: false},
		{cond: "false", want: false},
		{cond: "0", want: false},
		{cond: "true", want: true},
		{cond: "1", want: true},
		{cond: "  true  ", want: true},
		{cond: "truthy", wantErr: true},
		{cond: "bug == bug", wantErr: true},
		{cond: `bug == 'bug`, wantErr: true},
		{cond: `bug == "bug'`, wantErr: true},
		{cond: `bug 'bug'`, wantErr: true},
		{cond: `bug == 'it's'`, wantErr: true},
	}
	for _, tt := range tests {
		got, err := EvaluateCondition(tt.cond)
		if tt.wantErr {
			if err == nil {
				t.Errorf("EvaluateCondition(%q) = %v, want an error", tt.cond, got)
			}
			continue
		}
		if err != nil {
			t.Errorf("EvaluateCondition(%q) unexpected error: %v", tt.cond, err)
			continue
		}
		if got != tt.want {
			t.Errorf("EvaluateCondition(%q) = %v, want %v", tt.cond, got, tt.want)
		}
	}
}

func TestWhenProblem(t *testing.T) {
	t.Parallel()
	valid := []string{
		"$check.output == 'found'",
		"$check.output != 'missing'",
		`$check.output == "found"`,
		"$preflight.output.do_general == 'true'",
		"$fetch-pr.output.state.kind != 'closed'",
		"  $check.output == 'found'  ",
		"$check.output == 'a==b'",
		"$check.output == ''",
		"true", "false", "1", "0",
		"$gate.output",          // a lone reference: its output decides at run time
		"$gate.output.delivers", // same, with a field path
		"$a.output == $b.output == 'x'",
		"{{flag}}",                     // a lone placeholder: the variable decides at run time
		"{{label}} != 'ready-for-sky'", // sky-format.md's cancel-node example
		"$check.output == '{{expected}}'",
	}
	for _, w := range valid {
		if p := whenProblem(w); p != "" {
			t.Errorf("whenProblem(%q) = %q, want valid", w, p)
		}
	}
	invalid := map[string]string{
		"$check.output == found":        "right-hand side must be a quoted value",
		"$check.output != missing":      "right-hand side must be a quoted value",
		"$a.output == $b.output":        "right-hand side must be a quoted value",
		"$check.output == {{expected}}": "right-hand side must be a quoted value",
		"$check.output 'found'":         "quoted value must follow == or !=",
		"$check.output == 'found":       "right-hand side must be a quoted value",
		`$check.output == "found'`:      "right-hand side must be a quoted value",
		"$check.output == 'it's'":       "right-hand side must be a quoted value",
		"yes":                           "expected $node.output == 'value'",
		"$check.output and $b.output":   "expected $node.output == 'value'",
	}
	for w, want := range invalid {
		p := whenProblem(w)
		if p == "" {
			t.Errorf("whenProblem(%q) = valid, want a problem", w)
			continue
		}
		if !strings.Contains(p, want) {
			t.Errorf("whenProblem(%q) = %q, want it to contain %q", w, p, want)
		}
	}
}

// Every condition lint accepts must also parse at run time once its references
// resolve to a plain value, and every one it rejects must fail there. This pins
// lint and the runner to the same grammar.
func TestWhenProblem_MatchesRuntime(t *testing.T) {
	t.Parallel()
	for _, w := range []string{
		"$check.output == 'found'", "$check.output == found", "$check.output 'found'",
		"true", "yes", "$a.output == $b.output", "$a.output.f != 'x'",
		"{{label}} != 'ready'", "$check.output == {{expected}}",
	} {
		lintOK := whenProblem(w) == ""
		resolved := placeholderRe.ReplaceAllLiteralString(OutputRefRe.ReplaceAllLiteralString(w, "resolved-value"), "resolved-value")
		_, err := EvaluateCondition(resolved)
		if lintOK != (err == nil) {
			t.Errorf("%q: lint says ok=%v but the runtime grammar says err=%v", w, lintOK, err)
		}
	}
}

func TestLintBytes_WhenConditions(t *testing.T) {
	t.Parallel()
	const tmpl = "⊕meta⊕\nname = \"whenbug\"\ndescription = \"repro\"\ntrigger.manual = true\n⊕⊕\n\n" +
		"§check§\nbash = \"echo found\"\n§§\n\n" +
		"§act§\nbash = \"echo acting\"\ndepends_on = [\"check\"]\nwhen = \"%s\"\n§§\n"
	parse, ref := string(skyerr.ErrConditionParse), string(skyerr.ErrOutputRefInvalid)
	cases := []struct {
		when string
		want string // the one expected code, or "" for none
	}{
		{"$check.output == found", parse},
		{"$ghost.output == 'found'", ref},
		{"$ghost.output == $ghost.output == 'x'", ref}, // one per unknown node
		{"$check.output == 'found'", ""},
		{"{{label}} != 'ready'", ""},
		{"true", ""},
	}
	for _, tc := range cases {
		src := strings.Replace(tmpl, "%s", tc.when, 1)
		var got []Diagnostic
		for _, d := range LintBytes("whenbug.sky", []byte(src)) {
			if d.Code == parse || d.Code == ref {
				got = append(got, d)
			}
		}
		if tc.want == "" {
			if len(got) != 0 {
				t.Errorf("when %q: want no when diagnostics, got %+v", tc.when, got)
			}
			continue
		}
		if len(got) != 1 || got[0].Code != tc.want {
			t.Fatalf("when %q: want one %s, got %+v", tc.when, tc.want, got)
		}
		if got[0].Severity != "" {
			t.Errorf("%s severity = %q, want \"\" (blocking)", tc.want, got[0].Severity)
		}
		if !strings.Contains(got[0].Message, `node "act"`) {
			t.Errorf("message %q should name node \"act\"", got[0].Message)
		}
	}
}
