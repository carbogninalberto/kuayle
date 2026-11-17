package domain

import (
	"fmt"
	"regexp"
	"strings"
	"time"
	"unicode"

	"github.com/kuayle/kuayle-backend/pkg/sanitize"
)

// A failed CI suite's summary (KUA-206) tells the agent, the reviewer and a
// person which checks failed and how: their names, failed steps,
// annotations and the end of the failed step's log. It is GitHub's text and
// untrusted; whoever shows it to an agent fences it.
const (
	// CIFailureSummaryBytes bounds the summary stored on a CI signal. With
	// its heading and fence it still fits one notebook finding.
	CIFailureSummaryBytes = 2500
	// CIFailureDetailsWait bounds how long a follow-up or a workflow's CI
	// step waits for Kuayle to read the failed checks after the webhook;
	// past it they go on with what the webhook read.
	CIFailureDetailsWait = 3 * time.Minute
)

// ciLogLineBytes bounds one log line in a summary.
const ciLogLineBytes = 300

var (
	// ciLogTimestamp is the time GitHub puts in front of every log line.
	ciLogTimestamp = regexp.MustCompile(`^\x{FEFF}?\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}(?:\.\d+)?Z ?`)
	// ciLogANSI are terminal colour and control sequences.
	ciLogANSI = regexp.MustCompile(`\x1b\[[0-9;?]*[ -/]*[@-~]|\x1b\][^\x07\x1b]*(?:\x07|\x1b\\)?|\x1b`)
	// ciFailureText is a line that reports a failure: an error, a failed
	// test, a panic or an assertion's expected and received values.
	ciFailureText = regexp.MustCompile(`(?i)(##\[error\]|\berror\b|\bfail(?:ed|ure|ures|ing|s)?\b|\bpanic\b|exception|traceback|assertion|\bexpected\b|\breceived\b|✘|✗|✖|×|✕|^\s*\d+\) )`)
	// ciPassedText is a line about a passing test, whatever its name says.
	ciPassedText = regexp.MustCompile(`^\s*(?:✓|✔|ok\s|PASS\b|--- PASS)`)
	// ciExitAnnotation is the annotation GitHub adds to every failed step;
	// the step's name already says it failed.
	ciExitAnnotation = regexp.MustCompile(`^(?:##\[error\])?Process completed with exit code \d+\.?$`)
)

// cleanCIText makes GitHub's text storable and readable: valid UTF-8,
// without NUL or other control characters but newlines and tabs.
func cleanCIText(text string) string {
	text = strings.ToValidUTF8(sanitize.StripNUL(text), "")
	return strings.Map(func(r rune) rune {
		if r == '\n' || r == '\t' {
			return r
		}
		if unicode.IsControl(r) {
			return -1
		}
		return r
	}, text)
}

// cleanCILogLine removes a log line's timestamp, colours and the
// box-drawing rule some reporters draw after a test's name.
func cleanCILogLine(line string) string {
	line = ciLogTimestamp.ReplaceAllString(strings.TrimSuffix(line, "\r"), "")
	line = cleanCIText(ciLogANSI.ReplaceAllString(line, ""))
	return strings.TrimRight(line, " \t─━═")
}

func ciStepStart(line string) bool { return strings.HasPrefix(line, "##[group]Run ") }

// ciStepBoundary starts the next step: a run step's header, a post step
// ("Post job cleanup.") or the runner's own clean-up.
func ciStepBoundary(line string) bool {
	return ciStepStart(line) || line == "Post job cleanup." || strings.HasPrefix(line, "Cleaning up orphan processes")
}

// ciStepSection finds the failed step in a job log: the group GitHub opens
// with the step's name ("Run npm test"), or else the step that printed the
// first error; it ends where the next step, a post step or the clean-up
// starts. A log cut at its start still yields the step's end.
func ciStepSection(lines []string, failedStep string) []string {
	start := -1
	if name := strings.TrimSpace(failedStep); name != "" {
		for i := len(lines) - 1; i >= 0 && start < 0; i-- {
			if header, ok := strings.CutPrefix(lines[i], "##[group]"); ok && strings.EqualFold(strings.TrimSpace(header), name) {
				start = i
			}
		}
	}
	stepBefore := func(at int) int {
		for j := at; j >= 0; j-- {
			if ciStepStart(lines[j]) {
				return j
			}
		}
		return 0
	}
	if start < 0 {
		for i, line := range lines {
			if strings.HasPrefix(line, "##[error]") {
				start = stepBefore(i)
				break
			}
		}
	}
	if start < 0 {
		end := len(lines)
		for i, line := range lines {
			if ciStepBoundary(line) && !ciStepStart(line) {
				end = i
				break
			}
		}
		start = stepBefore(end - 1)
	}
	end := len(lines)
	for i := start + 1; i < len(lines); i++ {
		if ciStepBoundary(lines[i]) {
			end = i
			break
		}
	}
	return lines[start:end]
}

// ciStepBody drops the step's header group (its command, shell and
// environment), the markers of groups the step printed itself and the
// lines of tests that passed, which never explain a failure, and trims
// blank lines at either end.
func ciStepBody(section []string) []string {
	if len(section) > 0 && ciStepStart(section[0]) {
		section = section[1:]
		for i := 0; i < len(section) && i < 200; i++ {
			if section[i] == "##[endgroup]" {
				section = section[i+1:]
				break
			}
		}
	}
	body := make([]string, 0, len(section))
	for _, line := range section {
		if line == "##[endgroup]" || ciPassedText.MatchString(line) {
			continue
		}
		body = append(body, strings.TrimPrefix(line, "##[group]"))
	}
	for len(body) > 0 && strings.TrimSpace(body[0]) == "" {
		body = body[1:]
	}
	for len(body) > 0 && strings.TrimSpace(body[len(body)-1]) == "" {
		body = body[:len(body)-1]
	}
	return body
}

// ciTailStart returns where the end of the step that fits budget bytes
// starts, collapsing runs of blank lines.
func ciTailStart(lines []string, budget int) (int, []string) {
	var kept []string
	used := 0
	start := len(lines)
	for i := len(lines) - 1; i >= 0; i-- {
		line := TruncateBytes(lines[i], ciLogLineBytes)
		if strings.TrimSpace(line) == "" && len(kept) > 0 && strings.TrimSpace(kept[len(kept)-1]) == "" {
			start = i
			continue
		}
		if used+len(line)+1 > budget {
			break
		}
		kept = append(kept, line)
		used += len(line) + 1
		start = i
	}
	for i, j := 0, len(kept)-1; i < j; i, j = i+1, j-1 {
		kept[i], kept[j] = kept[j], kept[i]
	}
	return start, kept
}

// ciFailureLines picks the lines before the tail that report a failure,
// once each and in order, within budget bytes; last is the index after the
// last one picked.
func ciFailureLines(lines []string, budget int) (picked []string, last int) {
	seen := map[string]bool{}
	used := 0
	for i, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" || seen[line] || ciPassedText.MatchString(line) || !ciFailureText.MatchString(line) {
			continue
		}
		line = TruncateBytes(line, ciLogLineBytes)
		if used+len(line)+1 > budget {
			break
		}
		seen[line] = true
		picked = append(picked, line)
		used += len(line) + 1
		last = i + 1
	}
	return picked, last
}

// CILogExcerpt is what explains a failed step in an Actions job log, within
// limit bytes: the step's lines that report failures (failed tests, errors)
// and the end of the step's output, where its verdict is. Timestamps and
// colours are removed; other steps and the post-job clean-up are left out.
// failedStep is the step's name from GitHub's jobs API; a step with its own
// name is found by its first error instead. The text is GitHub's and
// untrusted.
func CILogExcerpt(log, failedStep string, limit int) string {
	// Below a few dozen bytes no line of a log says anything.
	if limit < 32 || strings.TrimSpace(log) == "" {
		return ""
	}
	raw := strings.Split(log, "\n")
	lines := make([]string, 0, len(raw))
	for _, line := range raw {
		lines = append(lines, cleanCILogLine(line))
	}
	body := ciStepBody(ciStepSection(lines, failedStep))
	if len(body) == 0 {
		return ""
	}
	const (
		whole        = "Log of the step:\n"
		failuresHead = "Lines that report failures:\n"
		tailHead     = "End of the step's log:\n"
	)
	if start, kept := ciTailStart(body, limit-len(whole)); start == 0 {
		return TruncateBytes(whole+strings.Join(kept, "\n"), limit)
	}
	// The end of the step gets most of the room; failures printed before it
	// get the rest, and the end takes back what they leave.
	tailBudget := limit * 3 / 5
	start, _ := ciTailStart(body, tailBudget-len(tailHead))
	failures, last := ciFailureLines(body[:start], limit-tailBudget-len(failuresHead)-len("…\n"))
	if len(failures) == 0 {
		_, kept := ciTailStart(body, limit-len(tailHead))
		return TruncateBytes(tailHead+strings.Join(kept, "\n"), limit)
	}
	head := failuresHead + strings.Join(failures, "\n") + "\n"
	start, kept := ciTailStart(body, limit-len(head)-len("…\n")-len(tailHead))
	if start < last {
		start, kept = ciTailStart(body[last:], limit-len(head)-len("…\n")-len(tailHead))
		start += last
	}
	gap := ""
	if start > last {
		gap = "…\n"
	}
	return TruncateBytes(head+gap+tailHead+strings.Join(kept, "\n"), limit)
}

// ciSummaryLine keeps one bounded line of GitHub's text.
func ciSummaryLine(text string, limit int) string {
	return TruncateBytes(strings.Join(strings.Fields(cleanCIText(text)), " "), limit)
}

// ciCheckSummary describes one failed check within budget bytes.
func ciCheckSummary(check CIFailedCheck, budget int) string {
	budget = max(budget, 64)
	conclusion := ciSummaryLine(check.Conclusion, 32)
	if conclusion == "" {
		conclusion = "failure"
	}
	var b strings.Builder
	fmt.Fprintf(&b, "%q concluded %s", ciSummaryLine(check.Name, 120), conclusion)
	if step := ciSummaryLine(check.FailedStep, 120); step != "" {
		fmt.Fprintf(&b, " in step %q", step)
	}
	b.WriteString("\n")
	if text := strings.TrimSpace(cleanCIText(check.Text)); text != "" {
		b.WriteString("Output:\n" + TruncateBytes(text, min(400, max(budget/4, 80))) + "\n")
	}
	annotations := check.AnnotationDetails
	if len(annotations) == 0 {
		annotations = check.Annotations
	}
	listed := 0
	for _, annotation := range annotations {
		annotation = ciSummaryLine(annotation, ciLogLineBytes)
		if annotation == "" || ciExitAnnotation.MatchString(annotation) || listed >= 5 || b.Len()+len(annotation)+16 > budget/2 {
			continue
		}
		if listed == 0 {
			b.WriteString("Annotations:\n")
		}
		b.WriteString("- " + annotation + "\n")
		listed++
	}
	if excerpt := CILogExcerpt(check.LogTail, check.FailedStep, budget-b.Len()); excerpt != "" {
		b.WriteString(excerpt + "\n")
	}
	return TruncateBytes(strings.TrimRight(b.String(), "\n"), budget)
}

// CIFailureSummary describes a failed suite's checks for the agent, the
// reviewer and a person, within CIFailureSummaryBytes: for each check its
// name, conclusion and failed step, the check's own output, its
// annotations and an excerpt of its job log. more says that more checks
// failed than were read. The text is GitHub's and untrusted.
func CIFailureSummary(checks []CIFailedCheck, more bool) string {
	if len(checks) == 0 && !more {
		return ""
	}
	footer := ""
	if more {
		footer = "More checks failed than Kuayle read; GitHub lists them all."
	}
	budget := CIFailureSummaryBytes - len(footer) - 2
	parts := make([]string, 0, len(checks)+1)
	used := 0
	for i, check := range checks {
		share := (budget - used) / (len(checks) - i)
		part := ciCheckSummary(check, share-2)
		parts = append(parts, part)
		used += len(part) + 2
	}
	if footer != "" {
		parts = append(parts, footer)
	}
	return TruncateBytes(strings.Join(parts, "\n\n"), CIFailureSummaryBytes)
}
