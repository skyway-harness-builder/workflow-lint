package workflow

import (
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/skyway-harness-builder/skyerr"
)

const (
	CodeParamInvalid       skyerr.Code = "SKY-WF-116"
	CodeTemplateUndeclared skyerr.Code = "SKY-WF-108"
	CodeInvokeParamMissing skyerr.Code = "SKY-WF-109"
	CodeInvokeParamType    skyerr.Code = "SKY-WF-110"
)

const (
	ParamTypeString  = "string"
	ParamTypeNumber  = "number"
	ParamTypeBoolean = "boolean"
	ParamTypeEnum    = "enum"
)

var paramNameRe = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_-]*(?:\.[A-Za-z_][A-Za-z0-9_-]*)*$`)

// ValidateParams checks workflow-level params declarations parsed from ⊕meta⊕.
func ValidateParams(wf *Workflow) error {
	for name, p := range wf.Params {
		if !paramNameRe.MatchString(name) {
			return skyerr.New(CodeParamInvalid, fmt.Sprintf("workflow %q: params.%s has invalid name (must match %s)", wf.Name, name, paramNameRe.String()))
		}
		if p.Type == "" {
			return skyerr.New(CodeParamInvalid, fmt.Sprintf("workflow %q: params.%s.type is required", wf.Name, name))
		}
		if !validParamType(p.Type) {
			return skyerr.New(CodeParamInvalid, fmt.Sprintf("workflow %q: params.%s.type %q invalid (valid: string, number, boolean, enum)", wf.Name, name, p.Type))
		}
		if p.Type == ParamTypeEnum && len(p.Enum) == 0 {
			return skyerr.New(CodeParamInvalid, fmt.Sprintf("workflow %q: params.%s.enum must list at least one value", wf.Name, name))
		}
		if p.Type != ParamTypeEnum && len(p.Enum) > 0 {
			return skyerr.New(CodeParamInvalid, fmt.Sprintf("workflow %q: params.%s.enum is only valid when type = \"enum\"", wf.Name, name))
		}
		for _, v := range p.Enum {
			if v == "" {
				return skyerr.New(CodeParamInvalid, fmt.Sprintf("workflow %q: params.%s.enum must not contain empty values", wf.Name, name))
			}
		}
		if p.Default != nil {
			if err := validateParamValue(name, p, p.Default); err != nil {
				return skyerr.New(CodeParamInvalid, fmt.Sprintf("workflow %q: %v", wf.Name, err))
			}
		}
	}
	return nil
}

func validParamType(t string) bool {
	switch t {
	case ParamTypeString, ParamTypeNumber, ParamTypeBoolean, ParamTypeEnum:
		return true
	default:
		return false
	}
}

func validateParamValue(name string, p ParamSpec, value any) error {
	switch p.Type {
	case ParamTypeString:
		if _, ok := value.(string); !ok {
			return fmt.Errorf("params.%s.default must be a string", name)
		}
	case ParamTypeNumber:
		switch value.(type) {
		case float64, int, int64, jsonNumberLike:
			// parseAssignBody produces float64; tests may build Workflow directly with ints.
		default:
			return fmt.Errorf("params.%s.default must be a number", name)
		}
	case ParamTypeBoolean:
		if _, ok := value.(bool); !ok {
			return fmt.Errorf("params.%s.default must be a boolean", name)
		}
	case ParamTypeEnum:
		s, ok := value.(string)
		if !ok {
			return fmt.Errorf("params.%s.default must be one of enum values", name)
		}
		if !stringInSlice(s, p.Enum) {
			return fmt.Errorf("params.%s.default %q is not in enum", name, s)
		}
	}
	return nil
}

type jsonNumberLike interface{ String() string }

// ValidateTemplateParams warns when {{var}} references are not declared params or known trigger vars.
func ValidateTemplateParams(wf *Workflow) []Diagnostic {
	declared := map[string]bool{}
	for name := range wf.Params {
		declared[name] = true
	}
	allowed := triggerVarAllowlist(wf)
	foreachAllowed := foreachVarAllowlist(wf)
	seen := map[string]bool{}
	var out []Diagnostic
	for _, ref := range collectTemplateRefs(wf) {
		if declared[ref] || allowed(ref) || foreachAllowed(ref) || seen[ref] {
			continue
		}
		seen[ref] = true
		out = append(out, Diagnostic{
			Code:     string(CodeTemplateUndeclared),
			Severity: "warning",
			Message:  fmt.Sprintf("{{%s}} is not declared in params and is not a known trigger-injected var", ref),
		})
	}
	return out
}

// foreachVarAllowlist allows the item/item_index/item_total variables the
// runner injects into a foreach body when the workflow has any foreach node.
// Without this, {{item}} in a foreach body is flagged SKY-WF-108 even though it
// resolves at run time.
func foreachVarAllowlist(wf *Workflow) func(string) bool {
	hasForeach := false
	for _, n := range wf.Nodes {
		if n.Foreach != nil {
			hasForeach = true
			break
		}
	}
	if !hasForeach {
		return func(string) bool { return false }
	}
	vars := map[string]bool{"item": true, "item_index": true, "item_total": true}
	return func(name string) bool { return vars[name] }
}

func collectTemplateRefs(wf *Workflow) []string {
	var refs []string
	add := func(text string) {
		for _, m := range placeholderRe.FindAllStringSubmatch(text, -1) {
			if len(m) > 1 && m[1] != "" {
				refs = append(refs, m[1])
			}
		}
	}
	for _, s := range wf.Steps {
		add(s.Prompt)
	}
	for _, n := range wf.Nodes {
		add(n.Prompt)
		if n.HTTP != nil {
			add(n.HTTP.URL)
			add(n.HTTP.Body)
			for _, v := range n.HTTP.Headers {
				add(v)
			}
		}
		if n.Wait != nil {
			add(n.Wait.Prompt)
		}
		if n.Approval != nil {
			add(n.Approval.Prompt)
			if n.Approval.OnReject != nil {
				add(n.Approval.OnReject.Prompt)
			}
		}
	}
	return refs
}

func triggerVarAllowlist(wf *Workflow) func(string) bool {
	exact := map[string]bool{
		"action": true,
	}
	prefixes := map[string]bool{}
	if wf.Trigger.GitHub != nil {
		for _, p := range []string{"repo", "repository", "issue", "pull_request", "sender", "comment", "label", "ref", "branch", "sha", "release", "workflow", "check_run"} {
			prefixes[p] = true
		}
	}
	if wf.Trigger.SkyEvent != nil {
		for _, p := range []string{"event", "payload"} {
			prefixes[p] = true
		}
	}
	if wf.Trigger.Schedule != nil {
		for _, p := range []string{"schedule", "now", "date", "time"} {
			prefixes[p] = true
		}
	}
	return func(name string) bool {
		if exact[name] {
			return true
		}
		head, _, _ := strings.Cut(name, ".")
		return prefixes[head]
	}
}

func validateInvokeParams(n *Node, target *Workflow) []SchemaIssue {
	if len(target.Params) == 0 {
		return nil
	}
	provided := n.Invoke.Vars
	var issues []SchemaIssue
	keys := make([]string, 0, len(provided))
	for k := range provided {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		p, ok := target.Params[k]
		if !ok {
			issues = append(issues, SchemaIssue{NodeID: n.ID, Code: CodeInvokeParamMissing, Message: fmt.Sprintf("node %q: invoke.vars.%s is not declared by target workflow %q", n.ID, k, target.Name)})
			continue
		}
		if err := validateInvokeStringValue(k, p, provided[k]); err != nil {
			issues = append(issues, SchemaIssue{NodeID: n.ID, Code: CodeInvokeParamType, Message: fmt.Sprintf("node %q: invoke.vars.%s for target %q: %v", n.ID, k, target.Name, err)})
		}
	}
	paramNames := make([]string, 0, len(target.Params))
	for name := range target.Params {
		paramNames = append(paramNames, name)
	}
	sort.Strings(paramNames)
	for _, name := range paramNames {
		p := target.Params[name]
		if p.Required && p.Default == nil {
			if _, ok := provided[name]; !ok {
				issues = append(issues, SchemaIssue{NodeID: n.ID, Code: CodeInvokeParamMissing, Message: fmt.Sprintf("node %q: invoke target %q requires params.%s", n.ID, target.Name, name)})
			}
		}
	}
	return issues
}

func validateInvokeStringValue(name string, p ParamSpec, raw string) error {
	switch p.Type {
	case ParamTypeString:
		return nil
	case ParamTypeNumber:
		if _, err := strconv.ParseFloat(raw, 64); err != nil {
			return fmt.Errorf("value %q is not a number", raw)
		}
	case ParamTypeBoolean:
		if _, err := strconv.ParseBool(raw); err != nil {
			return fmt.Errorf("value %q is not a boolean", raw)
		}
	case ParamTypeEnum:
		if !stringInSlice(raw, p.Enum) {
			return fmt.Errorf("value %q is not in enum for params.%s", raw, name)
		}
	}
	return nil
}

func stringInSlice(s string, list []string) bool {
	for _, v := range list {
		if s == v {
			return true
		}
	}
	return false
}

func workflowLintCodes() []skyerr.LintCode {
	codes := append([]skyerr.LintCode{}, skyerr.LintCodes...)
	seen := make(map[string]bool, len(codes)+4)
	for _, c := range codes {
		seen[c.Code] = true
	}
	for _, c := range []skyerr.LintCode{
		{Code: string(CodeParamInvalid), Description: "params declaration is invalid (name/type/default/enum mismatch)"},
		{Code: string(CodeTemplateUndeclared), Description: "{{var}} reference is neither declared in params nor trigger-injected"},
		{Code: string(CodeInvokeParamMissing), Description: "invoke.vars do not satisfy target workflow params"},
		{Code: string(CodeInvokeParamType), Description: "invoke.vars value does not match target workflow param type"},
		{Code: string(CodeRunnerInvalid), Description: "node.runner has an unsupported value (valid: claude, skylence)"},
	} {
		if !seen[c.Code] {
			codes = append(codes, c)
		}
	}
	return codes
}
