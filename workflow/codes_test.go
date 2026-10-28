package workflow

import (
	"testing"

	"github.com/skyway-harness-builder/skyerr"
)

// Codes defined in this repo must not reuse a number skyerr already registers
// for another check. CodeParamInvalid once shared SKY-WF-107 with
// skyerr.ErrForeachChainInvalid, and workflowLintCodes silently dropped it.
func TestLocalCodesDoNotCollideWithSkyerr(t *testing.T) {
	registered := make(map[string]string, len(skyerr.LintCodes))
	for _, c := range skyerr.LintCodes {
		registered[c.Code] = c.Description
	}
	local := []string{
		string(CodeParamInvalid), string(CodeTemplateUndeclared),
		string(CodeInvokeParamMissing), string(CodeInvokeParamType),
		string(CodeRunnerInvalid),
		codeCurlMissingFail, codeCurlMissingMaxTime, codeRetryOnSink, codePreferHTTPNode,
		codeThinkingRejected,
	}
	seen := map[string]bool{}
	for _, code := range local {
		if desc, ok := registered[code]; ok {
			t.Errorf("%s is defined here and also registered in skyerr as %q", code, desc)
		}
		if seen[code] {
			t.Errorf("%s is defined twice in this repo", code)
		}
		seen[code] = true
	}
}
