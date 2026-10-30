package workflow

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/skyway-harness-builder/skyerr"
)

var ultraKeywordRe = regexp.MustCompile(`(?i)\b(ultraplan|ultrareview|ultrathink)\b`)

// ValidateUltraKeywords returns a SKY-WF-061 SchemaIssue for each prompt location
// that contains the bare keyword "ultraplan", "ultrareview" or "ultrathink".
// skyway runs nodes in stream-json mode, not 'claude -p', so Claude Code's keyword
// detection stays active: a keyword in a prompt fires ("ultrathink" injects a
// reason-thoroughly nudge) whether or not the author meant it to.
func ValidateUltraKeywords(wf *Workflow) []SchemaIssue {
	if wf == nil {
		return nil
	}
	var issues []SchemaIssue
	for _, n := range wf.Nodes {
		id := n.ID
		if n.Script == nil { // script bodies are stored in Node.Prompt — skip to avoid false positives
			if iss, ok := scanUltra(n.Prompt, id, "/nodes/"+id+"/prompt"); ok {
				issues = append(issues, iss)
			}
		}
		if iss, ok := scanUltra(n.SystemPrompt, id, "/nodes/"+id+"/system_prompt"); ok {
			issues = append(issues, iss)
		}
	}
	for i, s := range wf.Steps {
		if iss, ok := scanUltra(s.Prompt, "", fmt.Sprintf("/steps/%d/prompt", i)); ok {
			issues = append(issues, iss)
		}
	}
	return issues
}

func scanUltra(s, nodeID, pointer string) (SchemaIssue, bool) {
	if s == "" {
		return SchemaIssue{}, false
	}
	matches := ultraKeywordRe.FindAllString(s, -1)
	if len(matches) == 0 {
		return SchemaIssue{}, false
	}
	seen := make(map[string]bool)
	var unique []string
	for _, m := range matches {
		lo := strings.ToLower(m)
		if !seen[lo] {
			seen[lo] = true
			unique = append(unique, lo)
		}
	}
	kw := "'" + unique[0] + "'"
	if n := len(unique); n > 1 {
		kw = "'" + strings.Join(unique[:n-1], "', '") + "' and '" + unique[n-1] + "'"
	}
	return SchemaIssue{
		NodeID:  nodeID,
		Pointer: pointer,
		Message: fmt.Sprintf("bare keyword %s is a Claude Code magic keyword, and it stays active under 'skyway run' (stream-json, not 'claude -p'); state the intent explicitly and set reasoning with the node's effort or thinking config", kw),
		Code:    skyerr.ErrPromptUltraKeywordNoOp,
	}, true
}
