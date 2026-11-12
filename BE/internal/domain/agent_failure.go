package domain

import (
	"fmt"
	"regexp"
	"strings"
	"unicode/utf8"
)

// FailureClass says who or what made a run or a task fail (KUA-137), so
// failures can be told and counted apart.
type FailureClass string

const (
	// FailureAgent: the harness or the agent itself failed.
	FailureAgent FailureClass = "agent"
	// FailureProvider: the model provider refused or failed (a usage limit,
	// overload, a rejected sign-in).
	FailureProvider FailureClass = "provider"
	// FailurePlatform: Kuayle or its host stopped the run (out of memory, the
	// PID limit, the time limit, a cleanup or persistence failure).
	FailurePlatform FailureClass = "platform"
	// FailureCIInfrastructure: CI failed because of its runner or network.
	FailureCIInfrastructure FailureClass = "ci_infrastructure"
	// FailurePolicy: the workspace's policy or a workflow limit refused.
	FailurePolicy FailureClass = "policy"
	// FailurePerson: a person stopped it.
	FailurePerson FailureClass = "person"
	// FailureUnknown: the evidence does not decide. Never a guess.
	FailureUnknown FailureClass = "unknown"
)

// FailureClasses lists every class, in the order counts report them.
var FailureClasses = []FailureClass{FailureAgent, FailureProvider, FailurePlatform, FailureCIInfrastructure, FailurePolicy, FailurePerson, FailureUnknown}

// ValidFailureClass reports whether class is one of FailureClasses.
func ValidFailureClass(class string) bool {
	for _, known := range FailureClasses {
		if string(known) == class {
			return true
		}
	}
	return false
}

// FailureCauseMaxBytes bounds a stored cause.
const FailureCauseMaxBytes = 2000

// failureStderrLines and failureStderrBytes bound the stderr a cause quotes.
const (
	failureStderrLines = 12
	failureStderrBytes = 1200
)

// RunFailureEvidence is what the manager knows when a run did not succeed.
// Every text in it is already redacted.
type RunFailureEvidence struct {
	// Status is the run's final status: failed, timeout or cancelled.
	Status string
	// ExitCode is the harness process's exit code; nil or negative when the
	// process never reported one.
	ExitCode *int
	// The container's end state.
	OOMKilled        bool
	MemoryLimit      string // the memory limit explanation (MemoryLimitMessage)
	PidsLimit        int64
	PidsPeak         int64
	TimeLimitSeconds int
	// ExecutionError is the manager's own error for the run: the container
	// could not be created or followed, output could not be stored, cleanup
	// failed. The harness never writes it.
	ExecutionError string
	// UsageLimit is the provider's usage limit message, when detected.
	UsageLimit string
	// HarnessError is what the harness itself reported went wrong.
	HarnessError   string
	NoFinalMessage bool
	// Stderr is the end of the harness's standard error.
	Stderr string
	// CancelledBy says who cancelled a cancelled run: CancelledByPerson, or
	// CancelledByPolicy when access or the policy was withdrawn; empty when
	// Kuayle stopped it itself (its machine restarted or stopped).
	CancelledBy string
}

const (
	CancelledByPerson = "person"
	CancelledByPolicy = "policy"
)

// RunFailure is a run's failure as stored: the class, the rule that decided
// it, and the real cause.
type RunFailure struct {
	Class FailureClass
	Rule  string
	Cause string
}

// runFailureRule is one row of the classification table. The first rule that
// matches decides; its cause is the headline the evidence is appended to.
type runFailureRule struct {
	name  string
	class FailureClass
	match func(RunFailureEvidence) bool
	cause func(RunFailureEvidence) string
}

// Signatures the harness, its runtime or a shell print when the kernel
// refuses a new thread or process: the container's PID limit.
var pidLimitText = regexp.MustCompile(`(?i)(failed to create new os thread|pthread_create failed|cannot fork|fork: retry|unable to create (?:new )?(?:native )?thread|uv_thread_create|(?:fork|thread|clone|spawn|process)[^\n]{0,80}resource temporarily unavailable|resource temporarily unavailable[^\n]{0,80}(?:fork|thread|clone|spawn))`)

// providerErrorText is wording model providers and their SDKs use for a
// failure on their side or of the account: overload, rate limits, server
// errors, rejected credentials, exhausted credit, an unreachable API.
var providerErrorText = regexp.MustCompile(`(?i)(overloaded|\b529\b|rate[ _-]?limit|too many requests|\b429\b|api ?error:? ?5\d\d|\b50[0234]\b[^\n]{0,40}(?:error|unavailable|gateway)|internal server error|service unavailable|bad gateway|authentication_error|invalid (?:x-)?api[ _-]?key|invalid_api_key|\b401\b[^\n]{0,40}unauthorized|insufficient_quota|credit balance is too low|billing|api connection error|connection error\.|model_not_found|upstream connect error)`)

// IsProviderError reports whether text reads as a model provider's failure.
func IsProviderError(text string) bool {
	return providerErrorText.MatchString(text)
}

// pidLimitReached: the container ran at its PID limit, or the output says the
// kernel refused a thread or process. A crash by signal while the container
// was at the limit is the limit: runtimes segfault when clone fails.
func pidLimitReached(e RunFailureEvidence) bool {
	if pidLimitText.MatchString(e.Stderr) || pidLimitText.MatchString(e.HarnessError) {
		return true
	}
	return e.PidsLimit > 0 && e.PidsPeak*100 >= e.PidsLimit*95 && exitSignal(e.ExitCode) != ""
}

func exited(code *int) bool { return code != nil && *code >= 0 }

// exitSignal names the signal of an exit code above 128, "" otherwise.
func exitSignal(code *int) string {
	if code == nil || *code <= 128 || *code > 192 {
		return ""
	}
	names := map[int]string{1: "SIGHUP", 2: "SIGINT", 3: "SIGQUIT", 4: "SIGILL", 6: "SIGABRT", 7: "SIGBUS", 8: "SIGFPE", 9: "SIGKILL", 11: "SIGSEGV", 13: "SIGPIPE", 15: "SIGTERM"}
	if name := names[*code-128]; name != "" {
		return name
	}
	return fmt.Sprintf("signal %d", *code-128)
}

// runFailureRules is the classification table, most decisive evidence first.
// What the platform did to the run comes before what the provider or the
// harness said: a run killed at its memory limit often also prints an error.
var runFailureRules = []runFailureRule{
	{"cancelled_by_person", FailurePerson,
		func(e RunFailureEvidence) bool { return e.Status == "cancelled" && e.CancelledBy == CancelledByPerson },
		func(RunFailureEvidence) string { return "A person stopped the run" }},
	{"cancelled_by_policy", FailurePolicy,
		func(e RunFailureEvidence) bool { return e.Status == "cancelled" && e.CancelledBy == CancelledByPolicy },
		func(RunFailureEvidence) string {
			return "The run was stopped because access to the issue or the workspace policy no longer allows it"
		}},
	{"cancelled_by_platform", FailurePlatform,
		func(e RunFailureEvidence) bool { return e.Status == "cancelled" },
		func(RunFailureEvidence) string {
			return "Kuayle stopped the run: its machine was restarted, stopped or replaced"
		}},
	{"out_of_memory", FailurePlatform,
		func(e RunFailureEvidence) bool { return e.OOMKilled || e.MemoryLimit != "" },
		func(e RunFailureEvidence) string {
			if e.MemoryLimit != "" {
				return e.MemoryLimit
			}
			return "Out of memory: the agent container reached its memory limit"
		}},
	{"pid_limit", FailurePlatform, pidLimitReached,
		func(e RunFailureEvidence) string {
			if e.PidsLimit > 0 {
				return fmt.Sprintf("The agent container reached its limit of %d processes and threads (PID limit), so the harness could not start another and crashed", e.PidsLimit)
			}
			return "The agent container reached its limit of processes and threads (PID limit), so the harness could not start another"
		}},
	{"time_limit", FailurePlatform,
		func(e RunFailureEvidence) bool { return e.Status == "timeout" },
		func(e RunFailureEvidence) string {
			if e.TimeLimitSeconds > 0 {
				return fmt.Sprintf("Kuayle stopped the run at its time limit of %d minutes", e.TimeLimitSeconds/60)
			}
			return "Kuayle stopped the run at its time limit"
		}},
	{"platform_error", FailurePlatform,
		func(e RunFailureEvidence) bool { return e.ExecutionError != "" },
		func(e RunFailureEvidence) string { return platformErrorCause(e.ExecutionError) }},
	{"usage_limit", FailureProvider,
		func(e RunFailureEvidence) bool { return e.UsageLimit != "" },
		func(e RunFailureEvidence) string { return "The provider's usage limit was reached: " + e.UsageLimit }},
	{"sign_in_required", FailureProvider,
		func(e RunFailureEvidence) bool { return strings.HasPrefix(e.HarnessError, "sign_in_required") },
		func(e RunFailureEvidence) string { return "The provider needs a sign-in: " + e.HarnessError }},
	{"provider_error", FailureProvider,
		func(e RunFailureEvidence) bool { return IsProviderError(e.HarnessError) },
		func(e RunFailureEvidence) string { return "The provider reported an error: " + e.HarnessError }},
	{"no_final_message", FailureAgent,
		func(e RunFailureEvidence) bool { return e.NoFinalMessage },
		func(e RunFailureEvidence) string { return NoFinalMessageReason(e.HarnessError) }},
	{"harness_error", FailureAgent,
		func(e RunFailureEvidence) bool { return e.HarnessError != "" },
		func(e RunFailureEvidence) string { return "The harness reported an error: " + e.HarnessError }},
	{"provider_error_output", FailureProvider,
		func(e RunFailureEvidence) bool { return IsProviderError(e.Stderr) },
		func(RunFailureEvidence) string { return "The harness stopped on a provider error" }},
	{"killed", FailureUnknown,
		func(e RunFailureEvidence) bool { return exitSignal(e.ExitCode) != "" },
		func(e RunFailureEvidence) string {
			return "The harness process was ended by " + exitSignal(e.ExitCode) + " without an out-of-memory event, and nothing recorded says what sent it"
		}},
	{"harness_exit_with_output", FailureAgent,
		func(e RunFailureEvidence) bool {
			return exited(e.ExitCode) && *e.ExitCode != 0 && strings.TrimSpace(e.Stderr) != ""
		},
		func(RunFailureEvidence) string { return "The harness exited with an error" }},
	{"harness_exit_silent", FailureUnknown,
		func(e RunFailureEvidence) bool { return exited(e.ExitCode) && *e.ExitCode != 0 },
		func(RunFailureEvidence) string {
			return "The harness exited without reporting an error or writing to standard error"
		}},
}

// platformErrorCause names the manager's own failures in plain words and
// keeps the manager's message.
func platformErrorCause(message string) string {
	for prefix, what := range map[string]string{
		"agent_output_persistence_failed":  "Kuayle could not store the agent's output and stopped the run",
		"agent_session_persistence_failed": "Kuayle could not record the agent's session and stopped the run",
		"agent_timeout_cleanup_failed":     "Kuayle could not clean up the run after its time limit",
		"agent_recovery_cleanup_failed":    "Kuayle could not clean up the run it was recovering",
		"agent_recovery_credentials":       "Kuayle could not recover the run's original credentials",
		"checkout_not_ready":               "The run's checkout was not ready",
	} {
		if strings.HasPrefix(message, prefix) {
			return what + ": " + message
		}
	}
	return "Kuayle could not run the agent: " + message
}

// ClassifyRunFailure applies the table to a run that did not succeed. The
// cause is the rule's headline followed by the evidence: the exit code and
// signal, and the end of standard error. When no rule matches (no exit code,
// no error, nothing printed) the class is unknown and the cause says so.
func ClassifyRunFailure(e RunFailureEvidence) RunFailure {
	failure := RunFailure{Class: FailureUnknown, Rule: "no_evidence", Cause: "The run ended as " + e.Status + " and nothing recorded says why"}
	for _, rule := range runFailureRules {
		if rule.match(e) {
			failure = RunFailure{Class: rule.class, Rule: rule.name, Cause: rule.cause(e)}
			break
		}
	}
	var evidence []string
	if exited(e.ExitCode) && (*e.ExitCode != 0 || e.Status != "cancelled") {
		code := fmt.Sprintf("exit code %d", *e.ExitCode)
		if signal := exitSignal(e.ExitCode); signal != "" {
			code += ", " + signal
		}
		evidence = append(evidence, code)
	}
	failure.Cause = strings.TrimRight(strings.TrimSpace(failure.Cause), ".")
	if len(evidence) > 0 {
		failure.Cause += " (" + strings.Join(evidence, "; ") + ")"
	}
	if stderr := TailLines(e.Stderr, failureStderrLines, failureStderrBytes); stderr != "" && !strings.Contains(failure.Cause, stderr) {
		failure.Cause += ". Standard error ended with:\n" + stderr
	}
	failure.Cause = TruncateBytes(failure.Cause, FailureCauseMaxBytes)
	return failure
}

// TailLines keeps the last lines of text that are not blank, at most
// maxLines of them and maxBytes long, cut at a rune boundary.
func TailLines(text string, maxLines, maxBytes int) string {
	var kept []string
	lines := strings.Split(strings.ReplaceAll(text, "\r", ""), "\n")
	for i := len(lines) - 1; i >= 0 && len(kept) < maxLines; i-- {
		if line := strings.TrimRight(lines[i], " \t"); strings.TrimSpace(line) != "" {
			kept = append([]string{line}, kept...)
		}
	}
	tail := strings.Join(kept, "\n")
	if len(tail) > maxBytes {
		tail = tail[len(tail)-maxBytes:]
		for len(tail) > 0 && !utf8.RuneStart(tail[0]) {
			tail = tail[1:]
		}
		tail = "…" + tail
	}
	return tail
}

// TruncateBytes cuts text to at most limit bytes at a rune boundary, ending
// with an ellipsis when it cut.
func TruncateBytes(text string, limit int) string {
	if len(text) <= limit {
		return text
	}
	cut := limit - len("…")
	for cut > 0 && !utf8.RuneStart(text[cut]) {
		cut--
	}
	return text[:cut] + "…"
}

// TaskFailure is why a task fails: the class and the sentence people read.
type TaskFailure struct {
	Class  FailureClass
	Reason string
}

// FailureClassLabel is the class in words, for summaries.
func FailureClassLabel(class FailureClass) string {
	switch class {
	case FailureAgent:
		return "Agent"
	case FailureProvider:
		return "Provider"
	case FailurePlatform:
		return "Platform"
	case FailureCIInfrastructure:
		return "CI infrastructure"
	case FailurePolicy:
		return "Policy"
	case FailurePerson:
		return "Person"
	}
	return "Unknown"
}

// WorkflowOutcomeFailureClass is the class of a task whose workflow ends as
// failed after outcome, when nothing more specific is known: failed checks
// and reviews are the agent's work, a publication Kuayle could not make is
// the platform's, a wait that timed out or a limit is the workflow's policy.
func WorkflowOutcomeFailureClass(outcome string) FailureClass {
	switch outcome {
	case WorkflowAgentFailed, WorkflowChecksFailed, WorkflowReviewChangesRequested:
		return FailureAgent
	case WorkflowPublishFailed:
		return FailurePlatform
	case WorkflowTimeout, WorkflowChecksSkipped:
		return FailurePolicy
	case WorkflowApprovalDenied:
		return FailurePerson
	}
	return FailureUnknown
}
