# workflow-lint

Standalone open-source linter for [Skylence](https://skylence.be) `.sky` workflow files.

## Install

Download a prebuilt binary from [GitHub Releases](https://github.com/skyway-harness-builder/workflow-lint/releases), or build from source:

```sh
go install github.com/skyway-harness-builder/workflow-lint@latest
```

## Usage

```sh
workflow-lint [--format text|json|sarif] [--repo-root <path>] <file|glob>...
```

**Exit codes:** `0` = no problems (or warnings only), `1` = blocking problems found, `2` = tool error.

### `bash_file` nodes

A bash node may carry its script in a separate file via `bash_file = "./script.sh"`
(relative to the `.sky` file) instead of an inline `bash = "..."`. The two are
mutually exclusive. Related lint codes:

- `SKY-WF-099` — both `bash` and `bash_file` set on the same node (blocking; also
  rejected at parse time as `SKY-WF-024`).
- `SKY-WF-100` — `bash_file` references a script that does not exist (blocking).
- `SKY-WF-101` — bash syntax / shellcheck findings in the referenced script
  (warning, non-blocking; does not affect the exit code). shellcheck is used when
  it is on `PATH`; its absence is silent.

### Bash transport (curl / retry)

Semantic rules for outbound HTTP in bash nodes (inline `bash` or resolved
`bash_file` script body):

- `SKY-WF-111` — `curl` without `--fail` or `-f` (warning).
- `SKY-WF-112` — `curl` without `--max-time` (warning).
- `SKY-WF-113` — `retry` on a sink-like node id/script (`send`, `post`,
  `create`, `notify`, `book`, `deliver`, `apply`) — retries belong on
  idempotent reads (warning).
- `SKY-WF-114` — bash body is essentially a single `curl` call; suggests using
  an `http` node instead (info).

### Thinking config

- `SKY-WF-115` — `thinking.mode = "enabled"` (fixed `budget_tokens`) or
  `"disabled"` on a node. Current Claude models reject these with HTTP 400:
  `enabled` on Fable 5.x, Opus 4.7+, Sonnet 5.x and Haiku 5.5; `disabled` on
  Fable 5.x, Opus 5.5 and Sonnet 5.5. Use `thinking = {"mode": "adaptive"}`
  and set `effort` (warning; the field is not forwarded to the CLI yet).

### Code allocation

`SKY-WF-*` codes here are allocated in sequence; the next free one is
`SKY-WF-116`. `SKY-WF-200`–`SKY-WF-299` are reserved for checks `skyway lint`
runs itself (its concurrency checks are `SKY-WF-201`–`203`), so do not
allocate in that block. Earlier, both sides used `SKY-WF-111`–`113` for
different checks.

### Declared workflow params

Workflows may declare typed run inputs in `⊕meta⊕` using `params.<name>.*` keys:

```sky
params.title.type = "string"
params.title.required = true
params.priority.type = "enum"
params.priority.enum = ["low", "high"]
params.priority.default = "low"
```

Supported types are `string`, `number`, `boolean`, and `enum`. Defaults are
validated against the declared type. `{{var}}` references that are neither
declared params nor known trigger-injected vars emit `SKY-WF-108` as a warning
during rollout. Invoke nodes are cross-checked against target workflow params
when lint runs with workflow roots.

### Examples

```sh
# Lint all workflows in the current directory
workflow-lint workflows/*.sky

# JSON output (single flat array across all files)
workflow-lint --format json workflows/*.sky

# SARIF for GitHub Code Scanning
workflow-lint --format sarif --repo-root . workflows/*.sky
```

## GitHub Action

Use [skyway-harness-builder/lint-action](https://github.com/skyway-harness-builder/lint-action) to run this linter in CI.

## License

Apache-2.0. See [LICENSE](LICENSE).
