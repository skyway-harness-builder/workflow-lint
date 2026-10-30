package workflow

import (
	"github.com/skyway-harness-builder/skyerr"
)

// WorkflowManifest is the machine-readable surface description of the .sky workflow language.
// Emitted by `sky describe --format json`.
type WorkflowManifest struct {
	SchemaVersion string            `json:"schema_version"`
	SkyVersion    string            `json:"sky_version"`
	Format        FormatManifest    `json:"format"`
	Nodes         NodesManifest     `json:"nodes"`
	Triggers      TriggersManifest  `json:"triggers"`
	WhenGrammar   WhenGrammar       `json:"when_grammar"`
	Templates     TemplatesManifest `json:"templates"`
	Params        ParamsManifest    `json:"params"`
	LintCodes     []skyerr.LintCode `json:"lint_codes"`
}

// FormatManifest describes the four Unicode delimiter pairs used in .sky files.
type FormatManifest struct {
	Delimiters []Delimiter `json:"delimiters"`
}

// Delimiter is one open/close pair used to mark a section in a .sky file.
type Delimiter struct {
	Name  string `json:"name"`
	Open  string `json:"open"`
	Close string `json:"close"`
	Note  string `json:"note,omitempty"`
}

// NodesManifest describes the full set of node kinds and their keys.
type NodesManifest struct {
	Kinds      []string                     `json:"kinds"`
	CommonKeys map[string]string            `json:"common_keys"`
	KindKeys   map[string]map[string]string `json:"kind_keys"`
}

// TriggersManifest describes the supported trigger types.
type TriggersManifest struct {
	Manual   ManualTriggerManifest   `json:"manual"`
	GitHub   GitHubTriggerManifest   `json:"github"`
	SkyEvent SkyEventTriggerManifest `json:"sky_event"`
	Schedule ScheduleTriggerManifest `json:"schedule"`
}

// ManualTriggerManifest describes the default (no trigger configured) invocation mode.
type ManualTriggerManifest struct {
	Description string `json:"description"`
}

// GitHubTriggerManifest describes the github trigger block.
type GitHubTriggerManifest struct {
	Keys       map[string]string `json:"keys"`
	ForkPolicy []string          `json:"fork_policy_values"`
}

// SkyEventTriggerManifest describes the sky_event trigger block.
type SkyEventTriggerManifest struct {
	Keys map[string]string `json:"keys"`
}

// ScheduleTriggerManifest describes the schedule trigger block.
type ScheduleTriggerManifest struct {
	Keys map[string]string `json:"keys"`
	Note string            `json:"note"`
}

// WhenGrammar describes the condition grammar for node.when fields.
type WhenGrammar struct {
	Rules  []string `json:"rules"`
	Source string   `json:"source"`
}

// TemplatesManifest describes {{var}} placeholder syntax and available filters.
type TemplatesManifest struct {
	PlaceholderRegex string   `json:"placeholder_regex"`
	Filters          []string `json:"filters"`
	OutputRefRegex   string   `json:"output_ref_regex"`
	Note             string   `json:"note"`
}

// ParamsManifest describes workflow input declarations under params.<name>.
type ParamsManifest struct {
	NameRegex       string   `json:"name_regex"`
	Types           []string `json:"types"`
	DeclarationKeys []string `json:"declaration_keys"`
	Note            string   `json:"note"`
}

// Manifest returns the complete workflow language manifest for the given binary version.
func Manifest(version string) WorkflowManifest {
	return WorkflowManifest{
		SchemaVersion: "1",
		SkyVersion:    version,
		Format:        FormatManifest{Delimiters: formatDelimiters()},
		Nodes: NodesManifest{
			Kinds:      nodeKinds(),
			CommonKeys: nodeCommonKeys(),
			KindKeys:   nodeKindKeys(),
		},
		Triggers:    triggersManifest(),
		WhenGrammar: whenGrammar(),
		Params: ParamsManifest{
			NameRegex:       paramNameRe.String(),
			Types:           []string{ParamTypeString, ParamTypeNumber, ParamTypeBoolean, ParamTypeEnum},
			DeclarationKeys: []string{"type", "required", "default", "description", "enum"},
			Note:            "Declare workflow inputs as params.<name>.type/required/default/description/enum in ⊕meta⊕. Undeclared {{var}} references are warning-only during rollout.",
		},
		Templates: TemplatesManifest{
			PlaceholderRegex: `\{\{\s*([\w.\-]+)(?:\s*\|\s*(\w+))?\s*\}\}`,
			Filters:          []string{"json", "urlencode"},
			OutputRefRegex:   `\$([\w-]+)\.output((?:\.[\w-]+)*)`,
			Note:             "{{var}} expansion available in prompt, http, and eval nodes; not in bash nodes (use $SKY_<VAR> env vars instead). {{var|json}} emits a quoted JSON value for safe embedding in JSON strings.",
		},
		LintCodes: workflowLintCodes(),
	}
}

func formatDelimiters() []Delimiter {
	return []Delimiter{
		{Name: "meta", Open: "⊕meta⊕", Close: "⊕⊕", Note: "workflow metadata section; required, exactly one per file"},
		{Name: "node", Open: "§<id>§", Close: "§§", Note: "DAG node config section; id is the node identifier"},
		{Name: "node_prompt", Open: "∆<id>∆", Close: "∆∆", Note: "node prompt body; pairs with the §<id>§ section by matching id; Claude receives this verbatim"},
		{Name: "doc", Open: "※※", Close: "※※", Note: "doc/comment block; content is discarded by the parser"},
	}
}

func nodeCommonKeys() map[string]string {
	return map[string]string{
		"id":             "string: node identifier; unique within the workflow",
		"depends_on":     "[]string: node IDs this node waits for before executing",
		"when":           "string: condition expression; node is skipped if false",
		"model":          "string: Claude model alias (sonnet, opus, haiku, fable) or full ID (claude-sonnet-5-5)",
		"effort":         "string: Claude effort level",
		"isolation":      "string: worktree | worktree-run",
		"keep_branch":    "bool: retain the worktree branch after cleanup",
		"max_budget_usd": "number: per-node USD spend cap",
		"max_turns":      "integer: max Claude reasoning turns (>=1)",
		"trigger_rule":   "string: all_done (default) | all_success | one_success | one_failure",
		"chain_from":     "string: node ID whose Claude session to resume",
		"system_prompt":  "string: per-node system prompt override",
		"fallback_model": "string: model used when primary is overloaded",
		"mcp_config":     "string: path to per-node mcp.json override",
		"allowed_tools":  "[]string: tool allowlist merged with RunOpts base",
		"denied_tools":   "[]string: tools removed via --disallowed-tools (\"*\" removes all)",
		"permissions":    "string: interactive enables --permission-prompt-tool",
		"context":        "string: fresh | shared",
		"thinking":       "object: extended thinking config (mode, budget_tokens)",
		"sandbox":        "object: filesystem.allow is []string of relative paths",
		"skills":         "[]string: skill names from .claude/skills/<name>/SKILL.md",
		"learnings":      "object: compound-knowledge injection config (exclude, only, max_bytes)",
		"hooks":          "object: per-node hook registrations (map[event][]NodeHookEntry)",
		"retry":          "object: retry config (max_attempts, delay_ms, on_error)",
		"output_format":  "object: JSON schema for structured output",
		"emit":           "object|string: sky event to emit on successful completion",
		"safety":         "string: requires_permission suppresses SKY-WF-063",
		"runner":         "string: claude (default) or skylence: which CLI executes this node",
	}
}

func nodeKindKeys() map[string]map[string]string {
	return map[string]map[string]string{
		"prompt":  {"prompt": "string: inline prompt text sent to Claude"},
		"command": {"command": "string: reference to .sky/commands/<name>.md", "prompt": "string: optional inline supplement appended to the command body"},
		"bash":    {"bash": "string: shell command to execute"},
		"script": {
			"script":         "object: script config",
			"script.runtime": "string: required; bun | uv",
			"script.timeout": "number: timeout in ms; default 30000",
			"script.deps":    "[]string: packages installed before execution",
		},
		"http": {
			"http":               "object: HTTP call config",
			"http.url":           "string: required; supports {{var}} expansion",
			"http.method":        "string: HTTP method; default GET",
			"http.headers":       "map[string]string: request headers",
			"http.body":          "string: request body; use {{var|json}} inside JSON strings",
			"http.expect_status": "integer: expected status code; 0 = any 2xx",
			"http.timeout_s":     "integer: request timeout in seconds; default 30",
		},
		"eval": {
			"eval":          "object: assertion config",
			"eval.source":   "string: required; $node.output reference to evaluate",
			"eval.contains": "string: source must contain this substring",
			"eval.matches":  "string: source must match this regex",
			"eval.equals":   "string: source must equal this string exactly",
		},
		"loop": {
			"loop":                 "object: loop config (wraps the node body kind)",
			"loop.until":           "object: condition checked after each iteration",
			"loop.until.bash":      "string: bash command; exit 0 stops the loop",
			"loop.until.eval":      "object: eval assertion; pass stops the loop",
			"loop.max":             "integer: max iterations; default 10; hard cap 100",
			"loop.idle_timeout_ms": "integer: bash bodies only; abort iteration if no stdout for N ms",
		},
		"wait": {
			"wait":           "object: pause config",
			"wait.prompt":    "string: message shown to approver",
			"wait.channel":   "string: manual (default) | webhook",
			"wait.timeout":   "string: duration string e.g. 24h; empty = no timeout",
			"wait.approvers": "[]string: informational; not enforced",
		},
		"approval": {
			"approval":                        "object: structured approval gate (extends wait)",
			"approval.prompt":                 "string: message shown to approver",
			"approval.channel":                "string: manual (default) | webhook",
			"approval.timeout":                "string: duration string e.g. 24h",
			"approval.approvers":              "[]string: informational",
			"approval.capture_response":       "bool: include approver feedback in output",
			"approval.on_reject":              "object: behaviour on rejection",
			"approval.on_reject.prompt":       "string: message included when rejected",
			"approval.on_reject.prompt_node":  "string: node ID to re-execute on rejection",
			"approval.on_reject.max_attempts": "integer: 0-10; rejections tolerated before failing",
		},
		"cancel": {
			"cancel":        "object: aborts the entire run when reached",
			"cancel.reason": "string: optional message recorded in the run error",
		},
	}
}

func triggersManifest() TriggersManifest {
	return TriggersManifest{
		Manual: ManualTriggerManifest{Description: "no trigger block set; workflow is invoked manually via sky run"},
		GitHub: GitHubTriggerManifest{
			Keys: map[string]string{
				"trigger.github.events":          "[]string: GitHub event types to match (e.g. issues, pull_request)",
				"trigger.github.label":           "string: if set, event must carry this label",
				"trigger.github.fork_policy":     "string: deny (default) | allow | trusted-only",
				"trigger.github.trusted_authors": "[]string: used when fork_policy=trusted-only",
			},
			ForkPolicy: []string{"deny", "allow", "trusted-only"},
		},
		SkyEvent: SkyEventTriggerManifest{
			Keys: map[string]string{
				"trigger.sky_event.event": "string: exact event name to match; emitted by another workflow's emit node",
			},
		},
		Schedule: ScheduleTriggerManifest{
			Keys: map[string]string{
				"cron":     "Standard 5-field cron expression (min hour dom mon dow). Example: \"0 9 * * 1-5\" fires at 09:00 Mon–Fri.",
				"timezone": "IANA timezone name (e.g. \"America/New_York\"). Defaults to UTC.",
			},
			Note: "Fires the workflow on a time-based schedule. The daemon must be running for schedules to execute.",
		},
	}
}

func whenGrammar() WhenGrammar {
	return WhenGrammar{
		Rules: []string{
			"lhs == 'literal'",
			"lhs != 'literal'",
			"boolean literal: true | false | 1 | 0 | (empty string = false)",
		},
		Source: "workflow.EvaluateCondition (skyway's runner calls the same function; lint checks it as SKY-WF-031)",
	}
}

// nodeKinds returns the complete list of node kind strings in the same order as NodeType().
func nodeKinds() []string {
	return []string{
		"cancel",
		"script",
		"approval",
		"loop",
		"wait",
		"command",
		"bash",
		"http",
		"eval",
		"prompt",
	}
}
