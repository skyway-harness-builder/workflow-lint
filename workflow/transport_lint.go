package workflow

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

const (
	codeCurlMissingFail    = "SKY-WF-111"
	codeCurlMissingMaxTime = "SKY-WF-112"
	codeRetryOnSink        = "SKY-WF-113"
	codePreferHTTPNode     = "SKY-WF-114"
)

var (
	sinkHeuristicPattern = regexp.MustCompile(`(?i)(send|post|create|notify|book|deliver|apply)`)
	curlWordPattern      = regexp.MustCompile(`\bcurl\b`)
	// curlCommandPattern matches curl where the shell runs it as a command: at
	// the start of a statement, after a separator (; & | ( { ` or $( ), after a
	// keyword such as if or !, or after a wrapper such as sudo or timeout N, with
	// optional VAR=value assignments in between. `command -v curl` and a curl
	// inside an echo string do not match.
	curlCommandPattern = regexp.MustCompile("(?:^|[;&|({`]|\\$\\(|(?:^|\\s)(?:if|then|elif|else|while|until|do|time|exec|!|sudo|nohup|xargs|command|env|timeout\\s+\\S+)\\s)\\s*(?:[A-Za-z_][A-Za-z0-9_]*=\\S*\\s+)*(?:\\S*/)?curl(?:\\s|$)")
)

// curlShortOptsWithArg are curl's single-letter options that take a value: in a
// bundle such as -sSo the rest of the bundle, or else the next word, is that
// value rather than more flags.
const curlShortOptsWithArg = "AbcCdDeEFHKmoPQrtTuUwxXyYz"

// ValidateTransportLint applies semantic bash-transport rules from issue #24:
//
//   - SKY-WF-111 (warning): curl without --fail or -f
//   - SKY-WF-112 (warning): curl without --max-time
//   - SKY-WF-113 (warning): retry on a sink-heuristic node
//   - SKY-WF-114 (info): bash body that is essentially a single curl → suggest http node
//
// wfPath resolves relative bash_file paths; pass "" to resolve against cwd.
func ValidateTransportLint(wf *Workflow, wfPath string) []Diagnostic {
	if wf == nil || len(wf.Nodes) == 0 {
		return nil
	}
	wfDir := filepath.Dir(wfPath)
	var diags []Diagnostic

	for i := range wf.Nodes {
		n := &wf.Nodes[i]
		diags = append(diags, lintRetryOnSink(n)...)
		if script, ok := resolveBashScript(n, wfDir); ok {
			diags = append(diags, lintCurlTransport(n.ID, script)...)
			diags = append(diags, lintPreferHTTPNode(n, script)...)
		}
	}
	return diags
}

func lintRetryOnSink(n *Node) []Diagnostic {
	if n.Retry == nil {
		return nil
	}
	if !looksLikeSinkNode(n) {
		return nil
	}
	return []Diagnostic{{
		Code:     codeRetryOnSink,
		Severity: "warning",
		Message: fmt.Sprintf(
			"node %q: retry is set on a sink-like node — retries belong on idempotent reads, not send/post/create/notify/book/deliver/apply operations",
			n.ID,
		),
	}}
}

func looksLikeSinkNode(n *Node) bool {
	candidates := []string{n.ID, n.Bash, n.BashFile, filepath.Base(n.BashFile)}
	for _, s := range candidates {
		if s != "" && sinkHeuristicPattern.MatchString(s) {
			return true
		}
	}
	return false
}

func lintCurlTransport(nodeID, script string) []Diagnostic {
	var diags []Diagnostic
	for _, call := range curlInvocations(script) {
		fail, maxTime := curlFlags(call.args)
		if !fail {
			diags = append(diags, Diagnostic{
				Code:     codeCurlMissingFail,
				Severity: "warning",
				Message:  fmt.Sprintf("node %q: curl without --fail (or -f): %s", nodeID, truncateCurlLine(call.stmt)),
			})
		}
		if !maxTime {
			diags = append(diags, Diagnostic{
				Code:     codeCurlMissingMaxTime,
				Severity: "warning",
				Message:  fmt.Sprintf("node %q: curl without --max-time: %s", nodeID, truncateCurlLine(call.stmt)),
			})
		}
	}
	return diags
}

// curlFlags reports whether curl's arguments turn on --fail and --max-time. It
// reads short options the way curl does, so -fsS, -sSf, -m 5, -m5 and -sm 5 all
// count, while -F (form data) and the value after -o or -H do not.
func curlFlags(args []string) (fail, maxTime bool) {
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case a == "--":
			return fail, maxTime
		case strings.HasPrefix(a, "--"):
			name, _, hasValue := strings.Cut(a[2:], "=")
			switch name {
			case "fail", "fail-with-body":
				fail = true
			case "max-time":
				maxTime = true
				if !hasValue {
					i++
				}
			}
		case len(a) > 1 && a[0] == '-':
			for j := 1; j < len(a); j++ {
				switch a[j] {
				case 'f':
					fail = true
				case 'm':
					maxTime = true
				}
				if strings.IndexByte(curlShortOptsWithArg, a[j]) >= 0 {
					if j == len(a)-1 {
						i++
					}
					break
				}
			}
		}
	}
	return fail, maxTime
}

func lintPreferHTTPNode(n *Node, script string) []Diagnostic {
	if n.HTTP != nil {
		return nil
	}
	if !isEssentiallySingleCurl(script) {
		return nil
	}
	return []Diagnostic{{
		Code:     codePreferHTTPNode,
		Severity: "info",
		Message: fmt.Sprintf(
			"node %q: bash body is essentially a single curl call — consider an http node (url, method, headers, body) with secrets allowlist and ${env:NAME} refs to avoid shell JSON quoting",
			n.ID,
		),
	}}
}

func resolveBashScript(n *Node, wfDir string) (string, bool) {
	if n.Bash != "" {
		return n.Bash, true
	}
	if n.BashFile == "" {
		return "", false
	}
	scriptPath := n.BashFile
	if !filepath.IsAbs(scriptPath) {
		scriptPath = filepath.Join(wfDir, scriptPath)
	}
	content, err := os.ReadFile(scriptPath)
	if err != nil {
		return "", false
	}
	return string(content), true
}

// curlCall is one curl command: the statement it is in, for messages, and the
// words after `curl` up to the end of that command.
type curlCall struct {
	stmt string
	args []string
}

// curlInvocations returns every curl command the script runs. Comment-only and
// echo/printf lines are skipped and backslash continuations joined first.
func curlInvocations(script string) []curlCall {
	var calls []curlCall
	for _, stmt := range bashStatements(script) {
		calls = append(calls, curlCalls(stmt)...)
	}
	return calls
}

// curlCalls returns the curl commands in one statement.
func curlCalls(stmt string) []curlCall {
	var calls []curlCall
	for _, loc := range curlCommandPattern.FindAllStringIndex(stmt, -1) {
		calls = append(calls, curlCall{stmt: stmt, args: commandWords(stmt[loc[1]:])})
	}
	return calls
}

// commandWords splits s into shell words up to the end of its first command: an
// unquoted ; | & ) or backtick. Quotes group a word and are dropped. An & right
// after < or > belongs to a redirection such as 2>&1.
func commandWords(s string) []string {
	var words []string
	var cur strings.Builder
	var quote byte
	flush := func() {
		if cur.Len() > 0 {
			words = append(words, cur.String())
			cur.Reset()
		}
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case quote != 0:
			if c == quote {
				quote = 0
			} else {
				cur.WriteByte(c)
			}
		case c == '\'' || c == '"':
			quote = c
		case c == ' ' || c == '\t':
			flush()
		case c == '&' && i > 0 && (s[i-1] == '>' || s[i-1] == '<'):
			cur.WriteByte(c)
		case c == ';' || c == '|' || c == '&' || c == ')' || c == '`':
			flush()
			return words
		default:
			cur.WriteByte(c)
		}
	}
	flush()
	return words
}

func bashStatements(script string) []string {
	joined := joinBackslashContinuations(script)
	var stmts []string
	for _, raw := range strings.Split(joined, "\n") {
		line := strings.TrimSpace(raw)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		// Skip obvious non-invocation mentions (conservative).
		if strings.HasPrefix(line, "echo ") || strings.HasPrefix(line, "printf ") {
			continue
		}
		stmts = append(stmts, line)
	}
	return stmts
}

func joinBackslashContinuations(script string) string {
	lines := strings.Split(script, "\n")
	var out []string
	var buf strings.Builder
	for _, raw := range lines {
		trimmed := strings.TrimSpace(raw)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			if buf.Len() > 0 {
				out = append(out, buf.String())
				buf.Reset()
			}
			out = append(out, raw)
			continue
		}
		// Strip inline comments for continuation decisions only.
		code := trimmed
		if idx := strings.Index(code, "#"); idx >= 0 {
			code = strings.TrimSpace(code[:idx])
		}
		if buf.Len() > 0 {
			buf.WriteByte(' ')
		}
		buf.WriteString(strings.TrimSpace(code))
		if strings.HasSuffix(code, `\`) {
			continue
		}
		out = append(out, buf.String())
		buf.Reset()
	}
	if buf.Len() > 0 {
		out = append(out, buf.String())
	}
	return strings.Join(out, "\n")
}

func isEssentiallySingleCurl(script string) bool {
	stmts := bashStatements(script)
	var curlCount int
	var otherExec int
	for _, stmt := range stmts {
		switch {
		case isBenignPreamble(stmt):
			continue
		case len(curlCalls(stmt)) > 0:
			curlCount += len(curlCalls(stmt))
		default:
			otherExec++
		}
	}
	return curlCount == 1 && otherExec == 0
}

func isBenignPreamble(stmt string) bool {
	switch {
	case strings.HasPrefix(stmt, "#!"):
		return true
	case strings.HasPrefix(stmt, "set "):
		return true
	case strings.HasPrefix(stmt, ":"):
		return true
	case strings.Contains(stmt, "=") && !curlWordPattern.MatchString(stmt):
		// Variable assignment without curl on the same line.
		return !strings.Contains(stmt, " $(curl") && !strings.Contains(stmt, "$(curl")
	default:
		return false
	}
}

func truncateCurlLine(line string) string {
	const max = 120
	line = strings.Join(strings.Fields(line), " ")
	if len(line) <= max {
		return line
	}
	return line[:max] + "..."
}

// isNonBlockingSeverity reports whether a diagnostic severity should not flip
// the lint exit code or block tier-dependent checks.
func isNonBlockingSeverity(severity string) bool {
	return severity == "warning" || severity == "info"
}
