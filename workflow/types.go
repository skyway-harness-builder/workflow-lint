package workflow

import "encoding/json"

// DocBlock represents a ※※ or ※<id>※ doc/comment block in a .sky file.
// StepID is empty for loose (※※) blocks; non-empty blocks are associated with the named node.
type DocBlock struct {
	StepID string
	Body   string
	Line   int
}

// ChangelogEntry is one entry in a workflow's ui changelog (from the
// `changelog` frontmatter list in the workflow's ui markdown file).
type ChangelogEntry struct {
	Version string `json:"version,omitempty"`
	Date    string `json:"date,omitempty"`
	Note    string `json:"note"`
}

// UIDoc is the resolved content of one ui markdown file for one locale.
// Short/Long come from optional YAML frontmatter (short_description / long_description);
// Body is the markdown after the frontmatter. Populated by the host (daemon), not by Parse.
// Changelog is populated from the `changelog` frontmatter list in workflow-level ui files;
// node-level ui files leave it empty.
type UIDoc struct {
	Short     string           `json:"short,omitempty"`
	Long      string           `json:"long,omitempty"`
	Body      string           `json:"body"`
	Changelog []ChangelogEntry `json:"changelog,omitempty"`
}

// Workflow supports both sequential steps and DAG nodes.
// If Nodes is set, DAG execution is used. Otherwise, Steps runs sequentially.
type Workflow struct {
	Name          string               `json:"name"`
	Source        SourceTier           `json:"source,omitempty"` // set by loader; identifies which tier this came from
	Raw           string               `json:"-" yaml:"-"`       // verbatim source bytes; populated by Parse
	Description   string               `json:"description,omitempty"`
	LibrarySource *string              `json:"_library_source,omitempty"` // repo slug injected at install time (e.g. "owner/repo")
	LibraryRef    *string              `json:"_library_ref,omitempty"`    // resolved tag injected at install time
	UI            string               `json:"ui,omitempty"`              // path (relative to the .sky file's dir) to a markdown file rendered into the OUTER workflow card; ignored by execution. Same resolution as the per-node ui key.
	UIDoc         map[string]UIDoc     `json:"_ui,omitempty"`             // host-populated at load time: locale -> rendered workflow card content; never parsed from .sky source.
	OutputStyle   string               `json:"output_style,omitempty"`    // "terse" appends a response-compression directive to every Claude prompt
	MaxBudget     float64              `json:"max_budget_usd,omitempty"`  // aggregate USD cap across all nodes in one run
	Claude        WorkflowClaudeOpts   `json:"claude,omitempty"`          // per-workflow Claude subprocess options
	MCPServers    map[string]any       `json:"mcp_servers,omitempty"`     // per-workflow MCP servers; merged with managed mcp.json per run
	Secrets       []string             `json:"secrets,omitempty"`         // env var names reachable via ${env:NAME} in mcp_servers and http nodes
	Params        map[string]ParamSpec `json:"params,omitempty"`          // declared workflow inputs, parsed from params.<name>.* keys in ⊕meta⊕
	Hooks         WorkflowHooks        `json:"hooks,omitempty"`           // per-workflow hook registrations
	Learnings     *LearningsConfig     `json:"learnings,omitempty"`       // workflow-level learnings config; overridden per-node
	RunDoc        bool                 `json:"run_doc,omitempty"`         // generate a shared scratchpad skeleton at run start
	Trigger       Trigger              `json:"trigger"`
	Steps         []Step               `json:"steps,omitempty"` // Legacy sequential mode
	Nodes         []Node               `json:"nodes,omitempty"` // DAG mode
	Docs          []DocBlock           `json:"-"`               // doc/comment blocks; populated by Parse; not used for execution
}

// ParamSpec declares one typed workflow input under params.<name> in ⊕meta⊕.
type ParamSpec struct {
	Type        string   `json:"type"`
	Required    bool     `json:"required,omitempty"`
	Default     any      `json:"default,omitempty"`
	Description string   `json:"description,omitempty"`
	Enum        []string `json:"enum,omitempty"`
}

// WorkflowHooks holds per-lifecycle-event hook configurations for a workflow.
type WorkflowHooks struct {
	PreToolUse *WorkflowHook `json:"pre_tool_use,omitempty"`
}

// WorkflowHook is a single hook configuration.
type WorkflowHook struct {
	// Inject is text injected as additionalContext on every matching tool call.
	Inject string `json:"inject,omitempty"`
	// Deny is a list of tool names to block. Use "*" to deny all tools.
	Deny []string `json:"deny,omitempty"`
}

// WorkflowClaudeOpts holds per-workflow overrides for Claude subprocess isolation.
// Set via ⊕meta⊕ key claude.isolation = "loose".
type WorkflowClaudeOpts struct {
	Isolation string `json:"isolation,omitempty"` // "strict" or "loose"
}

func (w *Workflow) IsDAG() bool {
	return len(w.Nodes) > 0
}

type Trigger struct {
	GitHub   *GitHubTrigger   `json:"github,omitempty"`
	SkyEvent *SkyEventTrigger `json:"sky_event,omitempty"`
	Sentry   *SourceTrigger   `json:"sentry,omitempty"`
	Linear   *SourceTrigger   `json:"linear,omitempty"`
	Jira     *SourceTrigger   `json:"jira,omitempty"`
	GitLab   *SourceTrigger   `json:"gitlab,omitempty"`
	Schedule *ScheduleTrigger `json:"schedule,omitempty"`
}

// ScheduleTrigger fires the workflow on a time-based cron schedule.
type ScheduleTrigger struct {
	Cron     string `json:"cron"`               // standard 5-field cron expression
	Timezone string `json:"timezone,omitempty"` // IANA timezone name; defaults to UTC
}

// SourceTrigger is a generic event-list trigger used by third-party sources
// (e.g. Sentry, Linear, Jira, GitLab) that do not require GitHub-specific fields.
type SourceTrigger struct {
	Events []string `json:"events"`
}

// SkyEventTrigger fires the workflow when a sky event with a matching name is emitted.
type SkyEventTrigger struct {
	Event string `json:"event"`
}

// EmitSpec describes the sky event to emit on successful node/step completion.
// Accepts both a plain string (emit name only) and an object with name + payload.
type EmitSpec struct {
	Name    string            `json:"name"`
	Payload map[string]string `json:"payload,omitempty"`
}

func (e *EmitSpec) UnmarshalJSON(data []byte) error {
	var s string
	if err := json.Unmarshal(data, &s); err == nil {
		e.Name = s
		return nil
	}
	type emitAlias EmitSpec
	var alias emitAlias
	if err := json.Unmarshal(data, &alias); err != nil {
		return err
	}
	*e = EmitSpec(alias)
	return nil
}

type GitHubTrigger struct {
	Events         []string        `json:"events"`
	Label          string          `json:"label,omitempty"`
	ForkPolicy     string          `json:"fork_policy,omitempty"`     // "deny" (default), "allow", "trusted-only"
	TrustedAuthors []string        `json:"trusted_authors,omitempty"` // used when fork_policy=trusted-only
	CheckRun       *CheckRunFilter `json:"check_run,omitempty"`
}

// CheckRunFilter constrains check_run events by conclusion and/or check name.
// Both fields are optional; an empty field matches any value.
type CheckRunFilter struct {
	Conclusion string `json:"conclusion,omitempty"` // "success", "failure", "neutral", etc.
	Name       string `json:"name,omitempty"`       // e.g. "CI", "lint"
}

// Step is the legacy sequential format.
type Step struct {
	Name       string    `json:"name"`
	Prompt     string    `json:"prompt,omitempty"`
	Model      string    `json:"model,omitempty"`
	Mode       string    `json:"mode,omitempty"`
	Advisor    bool      `json:"advisor,omitempty"`
	Isolation  string    `json:"isolation,omitempty"`
	KeepBranch bool      `json:"keep_branch,omitempty"`
	Effort     string    `json:"effort,omitempty"`
	MaxBudget  float64   `json:"max_budget_usd,omitempty"`
	MaxTurns   int       `json:"max_turns,omitempty"`
	Emit       *EmitSpec `json:"emit,omitempty"`
}

// Node is a DAG node with dependencies and conditional execution.
type Node struct {
	ID        string   `json:"id"`
	DependsOn []string `json:"depends_on,omitempty"`
	When      string   `json:"when,omitempty"` // condition: "$node-id.output.field == 'value'"

	// Node type: exactly one execution kind may be set (command may also carry a prompt block).
	// Loop is a modifier wrapping the body (command/bash/prompt); it co-exists with the body kind.
	Command     string           `json:"command,omitempty"`      // reference to .sky/commands/<name>.md
	Prompt      string           `json:"prompt,omitempty"`       // inline prompt (also supplement for command nodes)
	Bash        string           `json:"bash,omitempty"`         // shell command
	BashFile    string           `json:"bash_file,omitempty"`    // path to a shell script (relative to the .sky file); mutually exclusive with bash
	UI          string           `json:"ui,omitempty"`           // path (relative to the .sky file's dir) to a markdown file rendered into the node card by the dashboard UI; ignored by DAG execution. Extensionless = locale set (<ui>.<lang>.md); a .md value = single file (locale "default"). Mirrors bash_file.
	UIDocs      map[string]UIDoc `json:"_ui,omitempty"`          // host-populated at load time: locale -> rendered card content; never parsed from .sky source (like _library_source).
	HTTP        *HTTPConfig      `json:"http,omitempty"`         // outbound HTTP call
	Eval        *EvalConfig      `json:"eval,omitempty"`         // assertion on a prior node's output
	Loop        *LoopConfig      `json:"loop,omitempty"`         // repeat body until condition passes
	Foreach     *ForeachConfig   `json:"foreach,omitempty"`      // fan the body out over items resolved at run time
	Wait        *WaitConfig      `json:"wait,omitempty"`         // pause for human approval or webhook
	Cancel      *CancelConfig    `json:"cancel,omitempty"`       // abort the run immediately
	Script      *ScriptConfig    `json:"script,omitempty"`       // run inline TypeScript or Python
	Approval    *ApprovalConfig  `json:"approval,omitempty"`     // structured approval gate (extends wait)
	Invoke      *InvokeConfig    `json:"invoke,omitempty"`       // synchronously call another .sky workflow
	AcquireLock *LockConfig      `json:"acquire_lock,omitempty"` // acquire a named distributed lock
	ReleaseLock *LockConfig      `json:"release_lock,omitempty"` // release a named distributed lock
	Spawn       *SpawnConfig     `json:"spawn,omitempty"`        // run N workers in parallel and collect outputs
	Council     *CouncilConfig   `json:"council,omitempty"`      // fan-out N read-only advisory members then synthesize
	Review      *ReviewConfig    `json:"review,omitempty"`       // read-only code review against a base branch

	// Execution options
	Model         string  `json:"model,omitempty"`
	Effort        string  `json:"effort,omitempty"`
	Isolation     string  `json:"isolation,omitempty"`
	Runner        string  `json:"runner,omitempty"` // "claude" (default, absent) or "skylence": which CLI executes this node
	KeepBranch    bool    `json:"keep_branch,omitempty"`
	MaxBudget     float64 `json:"max_budget_usd,omitempty"`
	MaxTurns      int     `json:"max_turns,omitempty"`
	TriggerRule   string  `json:"trigger_rule,omitempty"`   // all_done (default), one_success, one_failure
	ChainFrom     string  `json:"chain_from,omitempty"`     // resume the Claude session from this dep node's session_id
	SystemPrompt  string  `json:"system_prompt,omitempty"`  // per-node system prompt override
	FallbackModel string  `json:"fallback_model,omitempty"` // model to use when primary is overloaded
	MCPConfig     string  `json:"mcp_config,omitempty"`     // path to per-node mcp.json override

	// Tool control (#141)
	AllowedTools []string `json:"allowed_tools,omitempty"` // allowlist; merged with RunOpts base
	DeniedTools  []string `json:"denied_tools,omitempty"`  // removed via --disallowed-tools; "*" removes all

	// Permissions (#191)
	Permissions string `json:"permissions,omitempty"` // "interactive" enables --permission-prompt-tool

	// Session context (#143)
	Context string `json:"context,omitempty"` // "fresh" | "shared"

	// Thinking (#145 — stored for future CLI support; not wired to flags yet)
	Thinking *ThinkingConfig `json:"thinking,omitempty"`

	// Sandbox (#146 — filesystem.allow only; maps to --add-dir)
	Sandbox *SandboxConfig `json:"sandbox,omitempty"`

	// Skills (#138)
	Skills []string `json:"skills,omitempty"` // skill names resolved from .claude/skills/<name>/SKILL.md

	// Links lists codebase names whose local paths are injected via --add-dir.
	// Resolved at run time against the codebase registry (GetCodebaseByName).
	Links []string `json:"links,omitempty"`

	// Learnings — compound knowledge injected from .sky/learnings/<category>/*.md
	Learnings *LearningsConfig `json:"learnings,omitempty"`

	// Per-node hooks (#140)
	Hooks NodeHooks `json:"hooks,omitempty"` // map[event][]NodeHookEntry

	// Retry (#142)
	Retry *RetryConfig `json:"retry,omitempty"`

	// Output
	OutputFormat map[string]any `json:"output_format,omitempty"` // JSON schema for structured output

	// Chaining
	Emit *EmitSpec `json:"emit,omitempty"` // emit a sky event on successful completion

	// Safety classification override — "requires_permission" suppresses SKY-WF-063.
	Safety string `json:"safety,omitempty"`
}

// CancelConfig marks a node that aborts the entire run immediately when reached.
// An empty struct is valid — no configuration is required.
type CancelConfig struct {
	Reason string `json:"reason,omitempty"` // optional message recorded in the run error
}

// ScriptConfig runs inline TypeScript (bun) or Python (uv). The script body comes
// from the ∆ block (stored in Node.Prompt). Deps are installed before execution.
type ScriptConfig struct {
	Runtime string   `json:"runtime"`           // "bun" | "uv"
	Timeout int      `json:"timeout,omitempty"` // ms; default 30000
	Deps    []string `json:"deps,omitempty"`    // packages installed before execution
}

// ApprovalConfig extends WaitConfig with structured approval semantics.
type ApprovalConfig struct {
	Prompt          string         `json:"prompt,omitempty"`           // message shown to approver
	Channel         string         `json:"channel,omitempty"`          // "manual" (default) or "webhook"
	Timeout         string         `json:"timeout,omitempty"`          // duration string e.g. "24h"
	Approvers       []string       `json:"approvers,omitempty"`        // informational
	CaptureResponse bool           `json:"capture_response,omitempty"` // include approver feedback in output
	OnReject        *RejectOptions `json:"on_reject,omitempty"`        // behaviour on rejection
}

// RejectOptions controls what happens when an approval is rejected.
type RejectOptions struct {
	Prompt      string `json:"prompt,omitempty"`       // message included in output when rejected
	PromptNode  string `json:"prompt_node,omitempty"`  // node ID to re-execute on rejection; must be a direct depends_on dep
	MaxAttempts int    `json:"max_attempts,omitempty"` // 0–10; rejections tolerated before failing (0 = fail on first rejection)
}

// ThinkingConfig configures extended thinking for a prompt node.
// Stored in the schema for future CLI flag support; not yet wired.
// Only "adaptive" works on every current Claude model (thinking depth is set
// with effort); "enabled" and "disabled" draw a SKY-WF-115 lint warning.
type ThinkingConfig struct {
	Mode         string `json:"mode"`                    // "adaptive" | "enabled" | "disabled"
	BudgetTokens int    `json:"budget_tokens,omitempty"` // required when mode="enabled"
}

// NodeHookEntry is a single per-node hook rule.
type NodeHookEntry struct {
	Matcher  string `json:"matcher,omitempty"` // regex matched against tool name; empty = match all
	Response string `json:"response"`          // "allow", "deny", or inject text
	Timeout  int    `json:"timeout,omitempty"` // seconds; default 30
}

// NodeHooks is a map from hook event name to hook entries.
type NodeHooks map[string][]NodeHookEntry

// RetryConfig controls automatic retry behaviour for a node.
type RetryConfig struct {
	MaxAttempts int    `json:"max_attempts"`       // 1–5
	DelayMS     int    `json:"delay_ms,omitempty"` // base delay; default 2000
	OnError     string `json:"on_error,omitempty"` // "transient" (default) | "all"
}

// SandboxConfig holds per-node sandbox settings. Only filesystem.allow is
// enforced today (maps to --add-dir). Network, deny, and other Archon fields
// have no Claude CLI counterpart and are omitted intentionally.
type SandboxConfig struct {
	Filesystem FilesystemConfig `json:"filesystem"`
}

// FilesystemConfig is the filesystem sub-section of SandboxConfig.
type FilesystemConfig struct {
	Allow []string `json:"allow,omitempty"` // relative paths; passed as --add-dir
}

// LearningsConfig controls how the compound-knowledge learnings store is injected
// into prompt and command nodes. Applied at workflow level (floor) or per-node (override).
type LearningsConfig struct {
	// Exclude lists category names to skip during injection. Ignored when Only is set.
	Exclude []string `json:"exclude,omitempty"`
	// Only, when non-empty, restricts injection to these categories only (overrides Exclude).
	Only []string `json:"only,omitempty"`
	// MaxBytes caps the total byte length of injected content. 0 = default (32 KiB);
	// -1 = no cap. Values outside [-1, 1048576] are rejected by the linter.
	MaxBytes int `json:"max_bytes,omitempty"`
}

// HTTPConfig is the config for an http node.
type HTTPConfig struct {
	URL          string            `json:"url"`
	Method       string            `json:"method,omitempty"` // default GET
	Headers      map[string]string `json:"headers,omitempty"`
	Body         string            `json:"body,omitempty"`          // templated with {{vars}} and $node.output
	ExpectStatus int               `json:"expect_status,omitempty"` // 0 = any 2xx
	TimeoutS     int               `json:"timeout_s,omitempty"`     // default 30
}

// EvalConfig is the config for an eval (assertion) node. Exactly one assertion field must be set.
type EvalConfig struct {
	Source   string `json:"source"`             // $node.output reference to evaluate
	Contains string `json:"contains,omitempty"` // source must contain this substring
	Matches  string `json:"matches,omitempty"`  // source must match this regex
	Equals   string `json:"equals,omitempty"`   // source must equal this string exactly
}

// LoopConfig is the config for a loop node. The loop body is the node's own command/bash/prompt.
type LoopConfig struct {
	Until LoopCondition `json:"until"`
	Max   int           `json:"max,omitempty"` // default 10
	// IdleTimeoutMs aborts the current iteration if the bash body produces no
	// stdout for this many milliseconds. Bash bodies only — prompt/command
	// bodies stream tokens continuously and have no observable idleness.
	IdleTimeoutMs int `json:"idle_timeout_ms,omitempty"`
}

// LoopCondition is the condition checked after each iteration. Exactly one field must be set.
// Bash exit 0 = pass (stop looping). Eval assertion pass = stop looping.
type LoopCondition struct {
	Bash string      `json:"bash,omitempty"`
	Eval *EvalConfig `json:"eval,omitempty"`
}

// ForeachConfig fans a node body out over a list of items resolved at run
// time. Items is either a []string literal or a string containing a
// $node.output[.field] / {{var}} reference that resolves to a JSON array
// (fallback: non-empty lines). MaxConcurrency bounds parallel items;
// 0 means the default of 1 (sequential).
type ForeachConfig struct {
	Items          any `json:"items"`
	MaxConcurrency int `json:"max_concurrency,omitempty"`
}

// WaitConfig pauses the run until a resume signal arrives or timeout expires.
type WaitConfig struct {
	Prompt    string   `json:"prompt,omitempty"`    // message shown to approver
	Channel   string   `json:"channel,omitempty"`   // webhook | manual (default: manual)
	Timeout   string   `json:"timeout,omitempty"`   // duration string e.g. "24h"; empty = no timeout
	Approvers []string `json:"approvers,omitempty"` // informational; not enforced in Phase 2
}

// NodeType returns what kind of node this is.
func (n *Node) NodeType() string {
	switch {
	case n.Cancel != nil:
		return "cancel"
	case n.Script != nil:
		return "script"
	case n.Approval != nil:
		return "approval"
	case n.Loop != nil:
		return "loop"
	case n.Foreach != nil:
		return "foreach"
	case n.Wait != nil:
		return "wait"
	case n.Invoke != nil:
		return "invoke"
	case n.AcquireLock != nil:
		return "acquire_lock"
	case n.ReleaseLock != nil:
		return "release_lock"
	case n.Review != nil:
		return "review"
	case n.Council != nil:
		return "council"
	case n.Spawn != nil:
		return "spawn"
	case n.Command != "":
		return "command"
	case n.Bash != "" || n.BashFile != "":
		return "bash"
	case n.HTTP != nil:
		return "http"
	case n.Eval != nil:
		return "eval"
	default:
		return "prompt"
	}
}

// InvokeConfig is the config for an invoke node. It synchronously calls another
// .sky workflow as a child run and returns its leaf output to the parent DAG.
type InvokeConfig struct {
	Target string            `json:"target"`         // workflow name to call; must be a literal (no template expressions)
	Vars   map[string]string `json:"vars,omitempty"` // input variables passed to the child workflow
}

// LockConfig is shared by acquire_lock and release_lock nodes.
// Key is the lock name. TTL is optional on acquire_lock (default 10m, max 1h)
// and ignored on release_lock.
type LockConfig struct {
	Key string `json:"key"`
	TTL string `json:"ttl,omitempty"` // e.g. "10m"; parsed as time.Duration
}

// SpawnConfig configures a spawn node that runs N worker agents in parallel.
type SpawnConfig struct {
	Workers        []SpawnWorker   `json:"workers"`
	MaxWait        string          `json:"max_wait,omitempty"`
	OnIdle         string          `json:"on_idle,omitempty"`
	ContinuePrompt string          `json:"continue_prompt,omitempty"`
	Boundary       *BoundaryConfig `json:"boundary,omitempty"`
}

// BoundaryConfig constrains what files the spawn step may edit.
// Enforcement requires git; if git is unavailable the hard check is skipped with a warning.
// Globs use path.Match semantics (single-segment wildcard only; ** is rejected at lint time).
type BoundaryConfig struct {
	ReadOnly    bool     `json:"read_only,omitempty"`
	Own         []string `json:"own,omitempty"`
	MustNotEdit []string `json:"must_not_edit,omitempty"`
}

// SpawnWorker is a single parallel worker within a spawn node.
type SpawnWorker struct {
	ID     string `json:"id"`
	Prompt string `json:"prompt"`
	Model  string `json:"model,omitempty"`
}

// CouncilConfig fans out N read-only advisory members in parallel, then runs a
// synthesis turn that combines their outputs into one recommendation.
// Members are always read-only: the boundary is enforced via P2's git-diff check
// and the B5 worktree PreToolUse hook. No boundary field is exposed — it is always
// set to ReadOnly:true.
type CouncilConfig struct {
	Members      []SpawnWorker `json:"members"`
	Synthesis    string        `json:"synthesis"`
	MaxWait      string        `json:"max_wait,omitempty"`
	MaxBudgetUSD float64       `json:"max_budget_usd,omitempty"`
}

// ReviewConfig configures a built-in read-only code-review node.
// It runs git diff against Base, loads relevant CLAUDE.md/AGENTS.md files scoped
// to the changed paths, and passes everything to Claude with a high-signal review prompt.
// The node is always read-only (Write/Edit/Bash denied via DeniedTools).
type ReviewConfig struct {
	Base   string   `json:"base,omitempty"`   // base ref for diff (default: "main")
	Target string   `json:"target,omitempty"` // PR number or branch name; "" means working tree vs Base
	Paths  []string `json:"paths,omitempty"`  // optional path filter; defaults to all changed files
}
