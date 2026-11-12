package domain

import (
	"regexp"
	"strings"
)

// CI failure classes stored on a task's CI signal (KUA-137). A failed check
// is a test failure unless the evidence says otherwise.
const (
	CIFailureTest    = "test_failure"
	CIFailureBuild   = "build_failure"
	CIFailureRunner  = "runner_or_network"
	CIFailureTimeout = "timeout"
	CIFailureCancel  = "cancelled"
)

// CIFailureClasses lists the classes a CI signal may carry.
var CIFailureClasses = []string{CIFailureTest, CIFailureBuild, CIFailureRunner, CIFailureTimeout, CIFailureCancel}

// CIFailedCheck is one failed check of a commit as GitHub reports it: its
// name, conclusion, the step of its job that failed, the check's output (its
// title and summary, normally empty when a step failed), the failure
// annotations GitHub attached and the end of its Actions job log. Text is
// untrusted.
type CIFailedCheck struct {
	Name       string
	Conclusion string
	FailedStep string
	Text       string
	// Annotations are the failure messages of the check's annotations, read
	// with one call. A build that failed for the runner often leaves only an
	// annotation; the real reason is in the job log.
	Annotations []string
	// LogTail is the end of the Actions job's log, where the runner and the
	// network report what actually went wrong.
	LogTail string
}

// evidenceText is everything the check printed, for the signature table.
func (c CIFailedCheck) evidenceText() string {
	parts := make([]string, 0, 2+len(c.Annotations))
	if text := strings.TrimSpace(c.Text); text != "" {
		parts = append(parts, text)
	}
	parts = append(parts, c.Annotations...)
	if tail := strings.TrimSpace(c.LogTail); tail != "" {
		parts = append(parts, tail)
	}
	return strings.Join(parts, "\n")
}

// CIFailureEvidence is what Kuayle received about a failed check suite.
type CIFailureEvidence struct {
	// Conclusion is the suite's (or the only check's) conclusion.
	Conclusion string
	Checks     []CIFailedCheck
	// MoreChecks is set when more checks failed than Kuayle read. A runner
	// failure needs every failed check to agree, so an incomplete list is
	// never the runner's.
	MoreChecks bool
}

// CIFailure is the class with the rule and the evidence that decided it.
type CIFailure struct {
	Class    string
	Rule     string
	Evidence string
}

// ciRunnerSignature is one strong sign that the runner or the network failed,
// never the change. The names are part of the classification's evidence.
type ciRunnerSignature struct {
	name    string
	pattern *regexp.Regexp
}

// ciRunnerSignatures are the only signs that make a suite the runner's. Each
// names a real failure seen on GitHub Actions; ordinary test output that
// mentions a word like "timeout" or "connection refused" is not here.
var ciRunnerSignatures = []ciRunnerSignature{
	{"dns_resolution", regexp.MustCompile(`(?i)(could not resolve (?:host )?(?:'[^']+'|"[^"]+"|[a-z0-9._-]+)|temporary failure (?:in name resolution|resolving)|name or service not known|no such host|EAI_AGAIN|ENOTFOUND)`)},
	{"package_mirror_unreachable", regexp.MustCompile(`(?i)(failed to fetch[^\n]{0,200}(?:temporary failure|connection|network is unreachable|no route to host|\b404\b|\b5\d\d\b|\b429\b)|(?:archive|mirror|registry|pypi|npmjs|proxy\.golang)[^\n]{0,100}(?:unreachable|timed out|connection (?:refused|reset)))`)},
	{"registry_unavailable", regexp.MustCompile(`(?i)(error response from daemon:[^\n]{0,200}(?:toomanyrequests|\b(?:429|50[0-4])\b)|failed to (?:pull|download) (?:image|action)[^\n]{0,200}(?:toomanyrequests|\b(?:429|50[0-4])\b)|toomanyrequests|(?:npm|yarn|pnpm|pip|go|cargo|apt|apk|docker)[^\n]{0,120}\b(?:429|50[0-4])\b[^\n]{0,80}(?:too many requests|bad gateway|service unavailable|gateway time-?out))`)},
	{"download_interrupted", regexp.MustCompile(`(?i)((?:download(?:ing|ed)?|fetch(?:ing)?|pull(?:ing)?|resolv(?:ing|ed)?|read(?:ing)?|installing)[^\n]{0,120}(?:connection reset by peer|connection timed out|i/o timeout|tls handshake timeout|read: connection reset|ECONNRESET|ETIMEDOUT|ECONNREFUSED)|read: connection reset by peer|connection reset by peer[^\n]{0,120}(?:download|module|image|package)|(?:npm|yarn|pnpm|pip|go|cargo|apt|apk|docker)[^\n]{0,120}(?:connection timed out|connection reset by peer|i/o timeout|ECONNRESET|ETIMEDOUT))`)},
	{"no_space", regexp.MustCompile(`(?i)no space left on device`)},
	{"runner_shutdown", regexp.MustCompile(`(?i)(runner has received a shutdown signal|lost communication with the server|the runner (?:was )?(?:shut down|lost|stopped responding))`)},
	{"out_of_memory", regexp.MustCompile(`(?i)(javascript heap out of memory|fatal error: reached heap limit|allocation failed - javascript heap|runtime: out of memory|cannot allocate memory|oom[- ]?kill|killed process[^\n]{0,80}(?:memory|oom)|(?:\b137\b|signal: killed)[^\n]{0,80}(?:memory|oom))`)},
}

// runnerSignature names the first strong runner or network sign in text.
func runnerSignature(text string) (name, found string) {
	for _, signature := range ciRunnerSignatures {
		if match := signature.pattern.FindString(text); match != "" {
			return signature.name, match
		}
	}
	return "", ""
}

// ciTimeoutText is the job's own time limit, GitHub's verdict.
var ciTimeoutText = regexp.MustCompile(`(?i)(exceeded the maximum execution time|has timed out after|job was cancell?ed because it exceeded)`)

// ciBuildText is the code not building.
var ciBuildText = regexp.MustCompile(`(?i)(compilation (?:failed|error)|build failed|failed to compile|cannot find (?:module|package)|syntax ?error|error TS\d+|undefined: |does not compile|linker command failed|error\[E\d+\])`)

// ciRunnerStep is a step of the runner's own work, never the change's.
var ciRunnerStep = regexp.MustCompile(`(?i)^(set up job|set up runner|initialize containers|complete runner|pull [^\n]*image|run actions/checkout[^\n]*|checkout( code| repository)?|post [^\n]*)$`)

// ciTestStep names a step that runs the change's own tests or checks. Its
// output can contain any word a network failure also uses, so a signature in
// a test step never makes the suite the runner's.
var ciTestStep = regexp.MustCompile(`(?i)^(run |running )?(unit |integration |e2e |end-to-end |go |npm |yarn |pnpm |pytest |jest |vitest |playwright |cargo |make )?(test|tests|testing|spec|specs|lint|format|check|checks|typecheck)\b`)

var ciBuildName = regexp.MustCompile(`(?i)\b(build|compile|typecheck|type-check|bundle|package|docker|image)\b`)

func isTestStep(step string) bool {
	return ciTestStep.MatchString(strings.TrimSpace(step))
}

// ciFailureRule is one row of the CI table; the first that matches decides.
type ciFailureRule struct {
	name  string
	class string
	match func(CIFailureEvidence) (string, bool)
}

func anyCheck(e CIFailureEvidence, match func(CIFailedCheck) (string, bool)) (string, bool) {
	for _, check := range e.Checks {
		if evidence, ok := match(check); ok {
			return evidence, true
		}
	}
	return "", false
}

func checkLabel(check CIFailedCheck, what string) string {
	label := check.Name
	if check.FailedStep != "" {
		label += ", step " + check.FailedStep
	}
	if label == "" {
		return what
	}
	return label + ": " + what
}

// allChecksAreTheRunners reports the first strong signature only when every
// failed check has one and no failed check runs the change's tests.
func allChecksAreTheRunners(e CIFailureEvidence) (string, bool) {
	if e.MoreChecks || len(e.Checks) == 0 {
		return "", false
	}
	first := ""
	for _, check := range e.Checks {
		if isTestStep(check.FailedStep) {
			return "", false
		}
		name, found := runnerSignature(check.evidenceText())
		if found == "" {
			return "", false
		}
		if first == "" {
			first = checkLabel(check, name+": "+found)
		}
	}
	return first, true
}

// ciFailureRules: a cancellation and a time limit are GitHub's own verdicts
// and come first; then the runner or network, by GitHub's conclusion, by the
// step that failed or by the strong signatures in every failed check's
// annotations and log tail; then a build; otherwise a test failure.
var ciFailureRules = []ciFailureRule{
	{"conclusion_cancelled", CIFailureCancel, func(e CIFailureEvidence) (string, bool) {
		if e.Conclusion == "cancelled" {
			return "the check suite concluded cancelled", true
		}
		return "", false
	}},
	{"conclusion_timed_out", CIFailureTimeout, func(e CIFailureEvidence) (string, bool) {
		if e.Conclusion == "timed_out" {
			return "the check suite concluded timed_out", true
		}
		return anyCheck(e, func(c CIFailedCheck) (string, bool) {
			if c.Conclusion == "timed_out" {
				return checkLabel(c, "concluded timed_out"), true
			}
			if found := ciTimeoutText.FindString(c.evidenceText()); found != "" {
				return checkLabel(c, found), true
			}
			return "", false
		})
	}},
	{"conclusion_startup_failure", CIFailureRunner, func(e CIFailureEvidence) (string, bool) {
		if e.Conclusion == "startup_failure" {
			return "the check suite could not start (startup_failure)", true
		}
		return anyCheck(e, func(c CIFailedCheck) (string, bool) {
			return checkLabel(c, "could not start (startup_failure)"), c.Conclusion == "startup_failure"
		})
	}},
	{"runner_step", CIFailureRunner, func(e CIFailureEvidence) (string, bool) {
		return anyCheck(e, func(c CIFailedCheck) (string, bool) {
			return checkLabel(c, "the runner's own step failed"), ciRunnerStep.MatchString(strings.TrimSpace(c.FailedStep))
		})
	}},
	{"runner_output", CIFailureRunner, allChecksAreTheRunners},
	{"build_output", CIFailureBuild, func(e CIFailureEvidence) (string, bool) {
		return anyCheck(e, func(c CIFailedCheck) (string, bool) {
			found := ciBuildText.FindString(c.evidenceText())
			return checkLabel(c, found), found != ""
		})
	}},
	{"build_step", CIFailureBuild, func(e CIFailureEvidence) (string, bool) {
		return anyCheck(e, func(c CIFailedCheck) (string, bool) {
			if ciBuildName.MatchString(c.FailedStep) {
				return checkLabel(c, "a build step failed"), true
			}
			// Without the failed step, the check's own name is all there is.
			return checkLabel(c, "a build check failed"), c.FailedStep == "" && ciBuildName.MatchString(c.Name) && !strings.Contains(strings.ToLower(c.Name), "test")
		})
	}},
}

// ClassifyCIFailure classifies a failed check suite. Every failed check has
// to agree before the failure is the runner's: one check that failed on a
// test keeps it a test failure, since the agent has something to fix. A
// truncated list never decides the runner.
func ClassifyCIFailure(e CIFailureEvidence) CIFailure {
	for _, rule := range ciFailureRules {
		if rule.class == CIFailureRunner && e.MoreChecks && rule.name != "conclusion_startup_failure" {
			continue
		}
		evidence, ok := rule.match(e)
		if !ok {
			continue
		}
		if rule.class == CIFailureRunner && len(e.Checks) > 1 && rule.name != "runner_output" {
			for _, check := range e.Checks {
				if single := ClassifyCIFailure(CIFailureEvidence{Checks: []CIFailedCheck{check}}); single.Class != CIFailureRunner {
					return CIFailure{Class: single.Class, Rule: single.Rule, Evidence: TruncateBytes(single.Evidence, 500)}
				}
			}
		}
		// A build beside a failing test is still the change's, but the test
		// is what a person should read.
		if rule.class == CIFailureBuild && len(e.Checks) > 1 {
			for _, check := range e.Checks {
				if single := ClassifyCIFailure(CIFailureEvidence{Checks: []CIFailedCheck{check}}); single.Class == CIFailureTest {
					return CIFailure{Class: single.Class, Rule: single.Rule, Evidence: TruncateBytes(single.Evidence, 500)}
				}
			}
		}
		return CIFailure{Class: rule.class, Rule: rule.name, Evidence: TruncateBytes(evidence, 500)}
	}
	return CIFailure{Class: CIFailureTest, Rule: "default_test_failure", Evidence: "nothing received says the failure is not the change's"}
}
