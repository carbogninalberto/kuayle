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
// name, conclusion, the step of its job that failed and what it printed
// (the check's output title and summary, its annotations). Text is untrusted.
type CIFailedCheck struct {
	Name       string
	Conclusion string
	FailedStep string
	Text       string
}

// CIFailureEvidence is what Kuayle received about a failed check suite.
type CIFailureEvidence struct {
	// Conclusion is the suite's (or the only check's) conclusion.
	Conclusion string
	Checks     []CIFailedCheck
}

// CIFailure is the class with the rule and the evidence that decided it.
type CIFailure struct {
	Class    string
	Rule     string
	Evidence string
}

var (
	// The runner, its host or the network failed: nothing the change did.
	ciRunnerText = regexp.MustCompile(`(?i)(runner has received a shutdown signal|lost communication with the server|runner (?:is |was )?(?:offline|lost|unavailable)|hosted runner (?:lost|encountered)|no space left on device|temporary failure (?:in name resolution|resolving)|could not resolve host|failed to fetch https?://|unable to (?:connect|access) [^\n]{0,80}(?:mirror|archive|registry|github\.com)|connection (?:timed out|reset by peer|refused)|tls handshake timeout|i/o timeout|\b(?:ETIMEDOUT|ECONNRESET|EAI_AGAIN|ENOTFOUND|ECONNREFUSED)\b|toomanyrequests|\b50[234]\b[^\n]{0,40}(?:bad gateway|service unavailable|gateway time-?out)|out of memory|\boom[- ]?kill|exit(?:ed with)? code 137|signal: killed|the operation was canceled by the runner|failed to (?:pull|download) (?:image|action)|error response from daemon)`)
	// The job ran past its time limit.
	ciTimeoutText = regexp.MustCompile(`(?i)(exceeded the maximum execution time|has timed out after|job was cancell?ed because it exceeded)`)
	// The code did not build.
	ciBuildText = regexp.MustCompile(`(?i)(compilation (?:failed|error)|build failed|failed to compile|cannot find (?:module|package)|syntax ?error|error TS\d+|undefined: |does not compile|linker command failed|error\[E\d+\])`)
	// Step and check names that are the runner's own work, not the change's.
	ciRunnerStep = regexp.MustCompile(`(?i)^(set up job|set up runner|initialize containers|complete runner|pull [^\n]*image|run actions/checkout[^\n]*|checkout( code| repository)?|post [^\n]*)$`)
	ciBuildName  = regexp.MustCompile(`(?i)\b(build|compile|typecheck|type-check|bundle|package|docker|image)\b`)
)

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

// ciFailureRules: a cancellation and a time limit are GitHub's own verdicts
// and come first; then the runner or network, by GitHub's conclusion, by the
// step that failed or by what was printed; then a build; otherwise a test
// failure, as before this classification existed.
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
			if found := ciTimeoutText.FindString(c.Text); found != "" {
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
	{"runner_output", CIFailureRunner, func(e CIFailureEvidence) (string, bool) {
		return anyCheck(e, func(c CIFailedCheck) (string, bool) {
			found := ciRunnerText.FindString(c.Text)
			return checkLabel(c, found), found != ""
		})
	}},
	{"build_output", CIFailureBuild, func(e CIFailureEvidence) (string, bool) {
		return anyCheck(e, func(c CIFailedCheck) (string, bool) {
			found := ciBuildText.FindString(c.Text)
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
// test keeps it a test failure, since the agent has something to fix.
func ClassifyCIFailure(e CIFailureEvidence) CIFailure {
	for _, rule := range ciFailureRules {
		evidence, ok := rule.match(e)
		if !ok {
			continue
		}
		if rule.class == CIFailureRunner && len(e.Checks) > 1 {
			for _, check := range e.Checks {
				if single := ClassifyCIFailure(CIFailureEvidence{Checks: []CIFailedCheck{check}}); single.Class != CIFailureRunner {
					return CIFailure{Class: single.Class, Rule: single.Rule, Evidence: TruncateBytes(single.Evidence, 500)}
				}
			}
		}
		return CIFailure{Class: rule.class, Rule: rule.name, Evidence: TruncateBytes(evidence, 500)}
	}
	return CIFailure{Class: CIFailureTest, Rule: "default_test_failure", Evidence: "nothing received says the failure is not the change's"}
}

// CIFailureStartsFollowUp reports whether a CI failure of this class is the
// agent's to fix. A runner or network failure and a cancellation are not: the
// agent cannot fix a runner, and nobody asked it to fix a cancelled run.
func CIFailureStartsFollowUp(class string) bool {
	return class != CIFailureRunner && class != CIFailureCancel
}
