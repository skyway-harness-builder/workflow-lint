package workflow

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"slices"
	"strconv"
	"strings"
	"time"

	cron "github.com/robfig/cron/v3"
	"github.com/skyway-harness-builder/skyerr"
)

// Diagnostic is a single linting finding from Lint.
type Diagnostic struct {
	File    string
	Line    int    // 0 means file-level
	Code    string // e.g. skyerr code string like "SKY-WF-001"
	Message string
	// Severity is "warning" or "info" for non-blocking findings (callers should
	// not flip the exit code) and "" (treated as error) for blocking findings.
	Severity string
}

// LintWithFixes parses and validates a single .sky file, returning diagnostics and
// machine-applicable fix proposals. error is only non-nil for filesystem errors.
func LintWithFixes(path string) ([]Diagnostic, []Fix, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, nil, err
	}
	diags := LintBytes(path, raw)
	fixes := ProposeFixes(path, raw, diags)
	return diags, fixes, nil
}

// Lint parses and validates a single .sky file, returning any diagnostics found.
// It runs: Parse → ValidateSchema → ValidateTemplates → ValidateBashTrust → ValidateSecrets → ValidateUltraKeywords → ValidateLearningsConfig → ValidateOutputFormatSchema.
// A parse failure short-circuits further checks.
// error is only non-nil for filesystem errors (cannot open file).
func Lint(path string) ([]Diagnostic, error) {
	diags, _, err := LintWithFixes(path)
	return diags, err
}

// LintWithRoots parses and validates a single .sky file, then runs tier-aware
// checks that require access to the workflow roots (e.g. SKY-WF-067 target
// resolution). Pass a zero Roots value to skip tier-dependent checks.
// error is only non-nil for filesystem errors.
func LintWithRoots(path string, roots Roots) ([]Diagnostic, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	diags := LintBytes(path, raw)
	if hasBlockingDiagnostics(diags) {
		// If base lint already failed, skip tier-dependent checks; the workflow
		// may not have parsed cleanly. Warning-only findings still allow cross-file checks.
		return diags, nil
	}

	wf, parseErr := Parse(bytes.NewReader(raw))
	if parseErr != nil {
		// Should not happen (LintBytes succeeded), but be safe.
		return diags, nil
	}

	for _, si := range validateInvoke(wf, roots) {
		diags = append(diags, Diagnostic{File: path, Code: string(si.Code), Message: FormatSchemaIssue(si)})
	}
	return diags, nil
}

func hasBlockingDiagnostics(diags []Diagnostic) bool {
	for _, d := range diags {
		if !isNonBlockingSeverity(d.Severity) {
			return true
		}
	}
	return false
}

// LintBytes validates raw .sky file content and returns diagnostics.
// path is used only to populate the File field in each Diagnostic.
// Pass a synthetic path (e.g. "workflow.sky") when content comes from a string.
func LintBytes(path string, raw []byte) []Diagnostic { //nolint:funlen // sequential validation pipeline, splitting adds no clarity
	wf, parseErr := Parse(bytes.NewReader(raw))
	if parseErr != nil {
		d := Diagnostic{File: path}
		var se *skyerr.Error
		if errors.As(parseErr, &se) {
			d.Code = string(se.Code)
			msg := se.Message
			if se.Cause != nil {
				msg += ": " + se.Cause.Error()
			}
			d.Line, d.Message = lineFromErr(msg)
		} else {
			d.Code = string(skyerr.ErrWorkflowParse)
			d.Line, d.Message = lineFromErr(parseErr.Error())
		}
		return []Diagnostic{d}
	}

	schemaIssues, schemaErr := ValidateSchema(wf)
	if schemaErr != nil {
		var se *skyerr.Error
		d := Diagnostic{File: path}
		if errors.As(schemaErr, &se) {
			d.Code = string(se.Code)
			d.Message = se.Message
			if se.Cause != nil {
				d.Message += ": " + se.Cause.Error()
			}
		} else {
			d.Code = string(skyerr.ErrWorkflowSchema)
			d.Message = schemaErr.Error()
		}
		return []Diagnostic{d}
	}
	if len(schemaIssues) > 0 {
		var diags []Diagnostic
		for _, si := range schemaIssues {
			code := string(skyerr.ErrWorkflowSchema)
			if si.Code != "" {
				code = string(si.Code)
			}
			diags = append(diags, Diagnostic{File: path, Code: code, Message: FormatSchemaIssue(si)})
		}
		return diags
	}

	if templateIssues := ValidateTemplates(wf); len(templateIssues) > 0 {
		var diags []Diagnostic
		for _, ti := range templateIssues {
			diags = append(diags, Diagnostic{
				File:    path,
				Code:    string(skyerr.ErrTemplateUnsafeSplice),
				Message: FormatSchemaIssue(ti),
			})
		}
		return diags
	}

	var diags []Diagnostic
	if trustIssues := ValidateBashTrust(wf); len(trustIssues) > 0 {
		for _, ti := range trustIssues {
			diags = append(diags, Diagnostic{
				File:    path,
				Code:    string(skyerr.ErrBashTrustUnsafe),
				Message: FormatSchemaIssue(ti),
			})
		}
	}
	for _, si := range ValidateSecrets(wf) {
		code := string(si.Code)
		if code == "" {
			code = string(skyerr.ErrSecretUndeclared)
		}
		diags = append(diags, Diagnostic{File: path, Code: code, Message: FormatSchemaIssue(si)})
	}
	for _, si := range ValidateUltraKeywords(wf) {
		diags = append(diags, Diagnostic{File: path, Code: string(si.Code), Message: FormatSchemaIssue(si)})
	}
	for _, si := range ValidateLearningsConfig(wf) {
		diags = append(diags, Diagnostic{File: path, Code: string(si.Code), Message: FormatSchemaIssue(si)})
	}
	for _, si := range ValidateOutputFormatSchema(wf) {
		diags = append(diags, Diagnostic{File: path, Code: string(si.Code), Message: FormatSchemaIssue(si)})
	}
	for _, si := range ValidateSafetyClass(wf) {
		diags = append(diags, Diagnostic{File: path, Code: string(si.Code), Message: FormatSchemaIssue(si)})
	}
	for _, si := range validateInvokeBasic(wf) {
		diags = append(diags, Diagnostic{File: path, Code: string(si.Code), Message: FormatSchemaIssue(si)})
	}
	for _, d := range ValidateTemplateParams(wf) {
		diags = append(diags, Diagnostic{File: path, Code: d.Code, Message: d.Message, Severity: d.Severity})
	}
	if gh := wf.Trigger.GitHub; gh != nil && gh.CheckRun != nil {
		validConclusions := map[string]bool{
			"success": true, "failure": true, "neutral": true, "cancelled": true,
			"timed_out": true, "action_required": true, "skipped": true, "stale": true,
		}
		if c := gh.CheckRun.Conclusion; c != "" && !validConclusions[c] {
			diags = append(diags, Diagnostic{
				File:    path,
				Line:    0,
				Code:    string(skyerr.ErrCheckRunConclusionInvalid),
				Message: fmt.Sprintf("workflow %q: trigger.github.check_run.conclusion %q is not a valid GitHub conclusion value", wf.Name, c),
			})
		}
		if !slices.Contains(gh.Events, "check_run.completed") {
			diags = append(diags, Diagnostic{
				File:    path,
				Line:    0,
				Code:    string(skyerr.ErrCheckRunMissingEvent),
				Message: fmt.Sprintf("workflow %q: trigger.github.check_run is set but trigger.github.events does not include \"check_run.completed\"", wf.Name),
			})
		}
	}
	for _, si := range validateSourceTriggers(wf) {
		diags = append(diags, Diagnostic{File: path, Code: string(si.Code), Message: FormatSchemaIssue(si)})
	}
	for _, d := range validateScheduleTrigger(wf) {
		diags = append(diags, Diagnostic{File: path, Code: d.Code, Message: d.Message})
	}
	for _, d := range ValidateBashFile(wf, path) {
		diags = append(diags, Diagnostic{File: path, Code: d.Code, Message: d.Message, Severity: d.Severity})
	}
	for _, d := range ValidateUI(wf, path) {
		diags = append(diags, Diagnostic{File: path, Code: d.Code, Message: d.Message, Severity: d.Severity})
	}
	for _, d := range lintDocBlocks(wf) {
		diags = append(diags, Diagnostic{File: path, Code: d.Code, Message: d.Message, Severity: d.Severity, Line: d.Line})
	}
	for _, d := range ValidateTransportLint(wf, path) {
		diags = append(diags, Diagnostic{File: path, Code: d.Code, Message: d.Message, Severity: d.Severity})
	}
	for _, d := range lintThinkingModes(wf) {
		diags = append(diags, Diagnostic{File: path, Code: d.Code, Message: d.Message, Severity: d.Severity})
	}
	for _, d := range lintHooksNeedLoose(wf) {
		diags = append(diags, Diagnostic{File: path, Code: d.Code, Message: d.Message, Severity: d.Severity})
	}
	return diags
}

// validateSourceTriggers checks that sentry, linear, jira, and gitlab triggers have at least
// one event listed (SKY-WF-095).
func validateSourceTriggers(wf *Workflow) []SchemaIssue {
	var issues []SchemaIssue
	if wf.Trigger.Sentry != nil && len(wf.Trigger.Sentry.Events) == 0 {
		issues = append(issues, SchemaIssue{
			Message: "trigger.sentry.events must not be empty",
			Code:    skyerr.ErrSourceTriggerEventsEmpty,
		})
	}
	if wf.Trigger.Linear != nil && len(wf.Trigger.Linear.Events) == 0 {
		issues = append(issues, SchemaIssue{
			Message: "trigger.linear.events must not be empty",
			Code:    skyerr.ErrSourceTriggerEventsEmpty,
		})
	}
	if wf.Trigger.Jira != nil && len(wf.Trigger.Jira.Events) == 0 {
		issues = append(issues, SchemaIssue{
			Message: "trigger.jira.events must not be empty",
			Code:    skyerr.ErrSourceTriggerEventsEmpty,
		})
	}
	if wf.Trigger.GitLab != nil && len(wf.Trigger.GitLab.Events) == 0 {
		issues = append(issues, SchemaIssue{
			Message: "trigger.gitlab.events must not be empty",
			Code:    skyerr.ErrSourceTriggerEventsEmpty,
		})
	}
	return issues
}

// validateScheduleTrigger validates the schedule trigger block (SKY-WF-096..098).
func validateScheduleTrigger(wf *Workflow) []Diagnostic {
	s := wf.Trigger.Schedule
	if s == nil {
		return nil
	}
	var diags []Diagnostic
	if s.Cron == "" {
		diags = append(diags, Diagnostic{
			Line:    0,
			Code:    string(skyerr.ErrScheduleCronRequired),
			Message: fmt.Sprintf("workflow %q: trigger.schedule.cron is required", wf.Name),
		})
		return diags // no point parsing empty string
	}
	p := cron.NewParser(cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow)
	if _, err := p.Parse(s.Cron); err != nil {
		diags = append(diags, Diagnostic{
			Line:    0,
			Code:    string(skyerr.ErrScheduleCronInvalid),
			Message: fmt.Sprintf("workflow %q: trigger.schedule.cron %q is invalid: %v", wf.Name, s.Cron, err),
		})
	}
	if s.Timezone != "" {
		// Reject "Local" explicitly — it resolves to the OS zone, which is
		// untrusted and non-deterministic across machines. Require an explicit
		// IANA name so DST transitions (spring-forward gaps, fall-back overlaps)
		// are handled predictably by time.LoadLocation.
		if strings.EqualFold(s.Timezone, "local") {
			diags = append(diags, Diagnostic{
				Line:    0,
				Code:    string(skyerr.ErrScheduleTimezoneInvalid),
				Message: fmt.Sprintf("workflow %q: trigger.schedule.timezone %q is not allowed; use an explicit IANA name (e.g. \"UTC\", \"Europe/Brussels\")", wf.Name, s.Timezone),
			})
		} else if _, err := time.LoadLocation(s.Timezone); err != nil {
			diags = append(diags, Diagnostic{
				Line:    0,
				Code:    string(skyerr.ErrScheduleTimezoneInvalid),
				Message: fmt.Sprintf("workflow %q: trigger.schedule.timezone %q is not a valid IANA location", wf.Name, s.Timezone),
			})
		}
	}
	return diags
}

// validateInvokeBasic runs invoke-specific checks that do not require tier access:
// SKY-WF-064 (dynamic target), SKY-WF-065 (invoke inside loop), SKY-WF-066 (self-invoke).
func validateInvokeBasic(wf *Workflow) []SchemaIssue {
	if len(wf.Nodes) == 0 {
		return nil
	}
	var issues []SchemaIssue
	for i := range wf.Nodes {
		n := &wf.Nodes[i]
		if n.Invoke == nil {
			continue
		}
		// SKY-WF-064: dynamic target.
		if strings.Contains(n.Invoke.Target, "{{") {
			issues = append(issues, SchemaIssue{
				NodeID:  n.ID,
				Message: fmt.Sprintf("node %q: invoke.target %q contains a template expression; use a literal workflow name", n.ID, n.Invoke.Target),
				Code:    skyerr.ErrInvokeDynamicTarget,
			})
		}
		// SKY-WF-066: self-invoke.
		if n.Invoke.Target == wf.Name {
			issues = append(issues, SchemaIssue{
				NodeID:  n.ID,
				Message: fmt.Sprintf("node %q: invoke.target %q matches the current workflow name (self-invoke not allowed)", n.ID, n.Invoke.Target),
				Code:    skyerr.ErrInvokeSelfTarget,
			})
		}
	}

	// SKY-WF-065: invoke node inside a loop body. In v1 the loop body kind is
	// always the node's own bash/prompt/command; a separate invoke field on the
	// same node is blocked by the multi-kind parser check. The scenario we guard
	// against here is a future where loop could reference an external node ID as
	// its body. For now emit the diagnostic when a node carries both loop and
	// invoke (belt-and-suspenders alongside the parser check).
	for i := range wf.Nodes {
		n := &wf.Nodes[i]
		if n.Loop != nil && n.Invoke != nil {
			issues = append(issues, SchemaIssue{
				NodeID:  n.ID,
				Message: fmt.Sprintf("node %q: invoke node inside a loop body is not supported in v1", n.ID),
				Code:    skyerr.ErrInvokeInsideLoop,
			})
		}
	}
	return issues
}

// validateInvoke runs all invoke checks including SKY-WF-067 (target resolution),
// which requires roots. Called from LintWithRoots only.
func validateInvoke(wf *Workflow, roots Roots) []SchemaIssue {
	basic := validateInvokeBasic(wf)
	if roots == (Roots{}) {
		return basic
	}

	var issues []SchemaIssue
	issues = append(issues, basic...)

	for i := range wf.Nodes {
		n := &wf.Nodes[i]
		if n.Invoke == nil || strings.Contains(n.Invoke.Target, "{{") {
			continue // skip dynamic targets (already reported by basic check)
		}
		if n.Invoke.Target == wf.Name {
			continue // skip self (already reported)
		}
		target, err := Resolve(n.Invoke.Target, roots)
		if err != nil {
			issues = append(issues, SchemaIssue{
				NodeID:  n.ID,
				Message: fmt.Sprintf("node %q: invoke.target %q not found in any workflow tier", n.ID, n.Invoke.Target),
				Code:    skyerr.ErrInvokeTargetNotFound,
			})
			continue
		}
		issues = append(issues, validateInvokeParams(n, target)...)
	}
	return issues
}

// lineFromErr extracts a line number from a skyerr message of the form "line N: ...".
// Returns 0 if not found.
func lineFromErr(msg string) (line int, message string) {
	const prefix = "line "
	if len(msg) <= len(prefix) || msg[:len(prefix)] != prefix {
		return 0, msg
	}
	rest := msg[len(prefix):]
	i := 0
	for i < len(rest) && rest[i] >= '0' && rest[i] <= '9' {
		i++
	}
	if i == 0 || i >= len(rest) || rest[i] != ':' {
		return 0, msg
	}
	n, err := strconv.Atoi(rest[:i])
	if err != nil {
		return 0, msg
	}
	remaining := rest[i+1:]
	if remaining != "" && remaining[0] == ' ' {
		remaining = remaining[1:]
	}
	return n, remaining
}
