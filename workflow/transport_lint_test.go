package workflow

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func writeTransportFixture(t *testing.T, name, sky string, scripts map[string]string) (dir, path string) {
	t.Helper()
	dir = t.TempDir()
	path = filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(sky), 0o600); err != nil {
		t.Fatalf("write sky: %v", err)
	}
	for rel, content := range scripts {
		full := filepath.Join(dir, rel)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatalf("mkdir: %v", err)
		}
		if err := os.WriteFile(full, []byte(content), 0o600); err != nil {
			t.Fatalf("write script %s: %v", rel, err)
		}
	}
	return dir, path
}

func diagByCode(diags []Diagnostic, code string) []Diagnostic {
	var out []Diagnostic
	for _, d := range diags {
		if d.Code == code {
			out = append(out, d)
		}
	}
	return out
}

func TestValidateTransportLint_CurlMissingFail(t *testing.T) {
	_, path := writeTransportFixture(t, "wf.sky", `⊕meta⊕
name = "curl-fail"
trigger.manual = true
⊕⊕

§run§
bash = "curl -sS https://example.com"
§§
`, nil)

	diags, err := Lint(path)
	if err != nil {
		t.Fatalf("Lint: %v", err)
	}
	got := diagByCode(diags, codeCurlMissingFail)
	if len(got) != 1 {
		t.Fatalf("want 1 %s, got %+v", codeCurlMissingFail, diags)
	}
	if got[0].Severity != "warning" {
		t.Errorf("severity = %q, want warning", got[0].Severity)
	}
}

func TestValidateTransportLint_CurlMissingMaxTime(t *testing.T) {
	_, path := writeTransportFixture(t, "wf.sky", `⊕meta⊕
name = "curl-maxtime"
trigger.manual = true
⊕⊕

§run§
bash = "curl -sS --fail https://example.com"
§§
`, nil)

	diags, err := Lint(path)
	if err != nil {
		t.Fatalf("Lint: %v", err)
	}
	got := diagByCode(diags, codeCurlMissingMaxTime)
	if len(got) != 1 {
		t.Fatalf("want 1 %s, got %+v", codeCurlMissingMaxTime, diags)
	}
	if got[0].Severity != "warning" {
		t.Errorf("severity = %q, want warning", got[0].Severity)
	}
}

func TestValidateTransportLint_CurlFlagsOnContinuedLine(t *testing.T) {
	script := `#!/usr/bin/env bash
set -euo pipefail
curl -sS \
  --fail \
  --max-time 30 \
  -X POST "https://example.com/send"
`
	_, path := writeTransportFixture(t, "wf.sky", `⊕meta⊕
name = "curl-ok"
trigger.manual = true
⊕⊕

§send§
bash_file = "./send.sh"
§§
`, map[string]string{"send.sh": script})

	diags, err := Lint(path)
	if err != nil {
		t.Fatalf("Lint: %v", err)
	}
	for _, code := range []string{codeCurlMissingFail, codeCurlMissingMaxTime} {
		if hasCode(diags, code) {
			t.Errorf("compliant continued curl should not emit %s, got %+v", code, diags)
		}
	}
}

func TestValidateTransportLint_RetryOnSink(t *testing.T) {
	_, path := writeTransportFixture(t, "wf.sky", `⊕meta⊕
name = "retry-sink"
trigger.manual = true
⊕⊕

§send§
bash = "echo noop"
retry = {"max_attempts": 3}
§§
`, nil)

	diags, err := Lint(path)
	if err != nil {
		t.Fatalf("Lint: %v", err)
	}
	got := diagByCode(diags, codeRetryOnSink)
	if len(got) != 1 {
		t.Fatalf("want 1 %s, got %+v", codeRetryOnSink, diags)
	}
	if got[0].Severity != "warning" {
		t.Errorf("severity = %q, want warning", got[0].Severity)
	}
	if !strings.Contains(got[0].Message, "idempotent reads") {
		t.Errorf("message = %q, want idempotent reads guidance", got[0].Message)
	}
}

func TestValidateTransportLint_RetryOnReadOK(t *testing.T) {
	_, path := writeTransportFixture(t, "wf.sky", `⊕meta⊕
name = "retry-read"
trigger.manual = true
⊕⊕

§pull§
bash_file = "./pull.sh"
retry = {"max_attempts": 3}
§§
`, map[string]string{"pull.sh": `curl -sS --fail --max-time 30 https://example.com`})

	diags, err := Lint(path)
	if err != nil {
		t.Fatalf("Lint: %v", err)
	}
	if hasCode(diags, codeRetryOnSink) {
		t.Errorf("retry on idempotent read should not warn, got %+v", diags)
	}
}

func TestValidateTransportLint_PreferHTTPNode(t *testing.T) {
	_, path := writeTransportFixture(t, "wf.sky", `⊕meta⊕
name = "single-curl"
trigger.manual = true
⊕⊕

§post§
bash = "curl -sS --fail --max-time 30 -X POST https://example.com/hook -d '{}'"
§§
`, nil)

	diags, err := Lint(path)
	if err != nil {
		t.Fatalf("Lint: %v", err)
	}
	got := diagByCode(diags, codePreferHTTPNode)
	if len(got) != 1 {
		t.Fatalf("want 1 %s, got %+v", codePreferHTTPNode, diags)
	}
	if got[0].Severity != "info" {
		t.Errorf("severity = %q, want info", got[0].Severity)
	}
	if !strings.Contains(got[0].Message, "http node") {
		t.Errorf("message = %q, want http node suggestion", got[0].Message)
	}
}

func TestValidateTransportLint_DunningSendRegression(t *testing.T) {
	// Mirrors workflows-finance/dunning-sequence/scripts/send-dunning.sh (compliant).
	script := `#!/usr/bin/env bash
set -euo pipefail
base="${EMAIL_API_BASE:?set EMAIL_API_BASE}"
: "${EMAIL_API_TOKEN:?EMAIL_API_TOKEN not set}"
body='{"to":"a@b.c"}'
curl -sS --fail --max-time 30 -X POST \
  -H "Authorization: Bearer ${EMAIL_API_TOKEN}" \
  -H 'Content-Type: application/json' \
  -d "$body" \
  "${base}/send"
`
	sky := `⊕meta⊕
name = "dunning-send"
trigger.manual = true
⊕⊕

§send§
bash_file = "./scripts/send-dunning.sh"
§§
`
	_, path := writeTransportFixture(t, "wf.sky", sky, map[string]string{"scripts/send-dunning.sh": script})

	diags, err := Lint(path)
	if err != nil {
		t.Fatalf("Lint: %v", err)
	}
	for _, code := range []string{codeCurlMissingFail, codeCurlMissingMaxTime, codeRetryOnSink} {
		if hasCode(diags, code) {
			t.Errorf("dunning send regression should not emit %s, got %+v", code, diags)
		}
	}
}

func TestValidateTransportLint_SkipsCommentEchoCurl(t *testing.T) {
	_, path := writeTransportFixture(t, "wf.sky", `⊕meta⊕
name = "no-false-positive"
trigger.manual = true
⊕⊕

§run§
bash = "echo 'use curl --fail here'"
§§
`, nil)

	diags, err := Lint(path)
	if err != nil {
		t.Fatalf("Lint: %v", err)
	}
	for _, code := range []string{codeCurlMissingFail, codeCurlMissingMaxTime, codePreferHTTPNode} {
		if hasCode(diags, code) {
			t.Errorf("echo mention should not trigger %s, got %+v", code, diags)
		}
	}
}

func TestIsNonBlockingSeverity(t *testing.T) {
	if !isNonBlockingSeverity("warning") || !isNonBlockingSeverity("info") {
		t.Fatal("warning and info must be non-blocking")
	}
	if isNonBlockingSeverity("") || isNonBlockingSeverity("error") {
		t.Fatal("empty/error severities must be blocking")
	}
}

// Each script is one node's bash; want lists (fail, max-time) per curl command
// found, in order. The first three lines come from essential-workflows'
// check-sky-environment and private-workflows' publish-framework-depindexes,
// which used to warn.
func TestCurlInvocations(t *testing.T) {
	t.Parallel()
	type call struct{ fail, maxTime bool }
	cases := []struct {
		script string
		want   []call
	}{
		{`if curl -fsS -m 5 "$BASE/health" >/dev/null 2>&1; then`, []call{{true, true}}},
		{`if ! curl -fsSL "$PUBLIC_INDEX_URL" -o "$CURRENT_INDEX"; then`, []call{{true, false}}},
		{`command -v curl >/dev/null 2>&1 || { echo 'FRAMEWORK_MERGE_FAILED: curl not found' >&2; exit 1; }`, nil},
		{`which curl`, nil},
		{`echo "curl -sS x"`, nil},
		{`curl -sSf --max-time 5 https://x`, []call{{true, true}}},
		{`curl -sS --max-time=5 https://x`, []call{{false, true}}},
		{`curl -sm 5 https://x`, []call{{false, true}}},
		{`curl -m5 https://x`, []call{{false, true}}},
		{`curl --fail-with-body https://x`, []call{{true, false}}},
		{`curl -F x=y https://x`, []call{{false, false}}},
		{`curl -sSo -f https://x`, []call{{false, false}}},
		{`curl -sS "https://x?a=1&b=2" -d '-f'`, []call{{false, false}}},
		{`resp=$(curl -fsS --max-time 10 "$U") && curl -sS "$V" | jq .`, []call{{true, true}, {false, false}}},
		{`out="$(curl -fsS -m 3 x)"`, []call{{true, true}}},
		{`TOKEN=abc curl -fsS -m 3 x`, []call{{true, true}}},
		{`timeout 10 curl -fsS -m 3 x`, []call{{true, true}}},
		{`/usr/bin/curl -fsS -m 3 x`, []call{{true, true}}},
	}
	for _, tc := range cases {
		var got []call
		for _, c := range curlInvocations(tc.script) {
			fail, maxTime := curlFlags(c.args)
			got = append(got, call{fail, maxTime})
		}
		if !reflect.DeepEqual(got, tc.want) {
			t.Errorf("%s\n  got  %+v\n  want %+v", tc.script, got, tc.want)
		}
	}
}
