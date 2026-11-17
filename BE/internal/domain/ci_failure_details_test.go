package domain

import (
	"fmt"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/stretchr/testify/require"
)

// actionsPlaywrightLog is the shape of a real GitHub Actions job log of a
// failed Playwright step (KUA-201): timestamps on every line, ANSI colours,
// the step's header group, hundreds of passing tests, the failure report,
// then an `if: always()` upload step and the post-job cleanup, which also
// prints an error of its own after the step failed.
func actionsPlaywrightLog(passing int) string {
	var b strings.Builder
	line := func(at, text string) {
		b.WriteString("2026-11-16T09:" + at + ".1234567Z " + text + "\n")
	}
	line("12:01", "Current runner version: '2.329.0'")
	line("12:01", "##[group]Operating System")
	line("12:01", "Ubuntu")
	line("12:01", "24.04.3")
	line("12:01", "##[endgroup]")
	line("12:02", "##[group]Run actions/checkout@v5")
	line("12:02", "with:")
	line("12:02", "  repository: acme/kuayle")
	line("12:02", "##[endgroup]")
	line("12:03", "Syncing repository: acme/kuayle")
	line("12:04", "##[group]Run npm ci")
	line("12:04", "npm ci")
	line("12:04", "shell: /usr/bin/bash -e {0}")
	line("12:04", "##[endgroup]")
	line("12:20", "added 812 packages, and audited 813 packages in 15s")
	line("12:20", "found 0 vulnerabilities")
	line("12:21", "##[group]Run npx playwright test")
	line("12:21", "npx playwright test")
	line("12:21", "shell: /usr/bin/bash -e {0}")
	line("12:21", "env:")
	line("12:21", "  CI: true")
	line("12:21", "##[endgroup]")
	line("12:25", "")
	line("12:25", "Running 214 tests using 2 workers")
	for i := range passing {
		line("13:00", fmt.Sprintf("\x1b[32m  ✓\x1b[39m %3d [chromium] › tests/e2e/board.test.ts:%d:3 › board › passing case %d (1.2s)", i+1, i+10, i))
		line("13:00", fmt.Sprintf("[WebServer] GET /api/issues?page=%d 200 in 3ms", i))
		if i == passing/2 {
			line("14:02", "\x1b[31m  ✘\x1b[39m  87 [chromium] › tests/e2e/run-chart.test.ts:58:3 › run chart › zooms out to the whole run (30.0s)")
		}
	}
	line("14:40", "")
	line("14:40", "  1) [chromium] › tests/e2e/run-chart.test.ts:58:3 › run chart › zooms out to the whole run ──────────────────")
	line("14:40", "")
	line("14:40", "    Error: \x1b[2mexpect(\x1b[22m\x1b[31mlocator\x1b[39m\x1b[2m).\x1b[22mtoHaveAttribute\x1b[2m(\x1b[22m\x1b[32mexpected\x1b[39m\x1b[2m)\x1b[22m failed")
	line("14:40", "")
	line("14:40", "    Locator: locator('[data-testid=\"run-chart\"]')")
	line("14:40", "    Expected: \x1b[32m\"1\"\x1b[39m")
	line("14:40", "    Received: \x1b[31m\"2\"\x1b[39m")
	line("14:40", "    Timeout:  5000ms")
	line("14:40", "")
	line("14:40", "    > 61 |     await expect(chart).toHaveAttribute('data-zoom', '1');")
	line("14:40", "         |                         ^")
	line("14:40", "        at /home/runner/work/kuayle/kuayle/UI/tests/e2e/run-chart.test.ts:61:25")
	line("14:41", "")
	line("14:41", "  1 failed")
	line("14:41", "    [chromium] › tests/e2e/run-chart.test.ts:58:3 › run chart › zooms out to the whole run ───────────────────")
	line("14:41", "  2 flaky")
	line("14:41", "    [chromium] › tests/e2e/issue-page.test.ts:20:3 › issue page › scrolls the document ───────────────────────")
	line("14:41", "  211 passed (2.3m)")
	line("14:41", "##[error]Process completed with exit code 1.")
	line("14:42", "##[group]Run actions/upload-artifact@v4")
	line("14:42", "with:")
	line("14:42", "  name: playwright-report")
	line("14:42", "##[endgroup]")
	line("14:43", "With the provided path, there will be 37 files uploaded")
	line("14:44", "Artifact playwright-report has been successfully uploaded! Final size is 2451024 bytes.")
	line("14:45", "Post job cleanup.")
	line("14:45", "[command]/usr/bin/git version")
	line("14:45", "git version 2.51.0")
	line("14:45", "##[error]The process '/usr/bin/git' failed with exit code 128")
	line("14:46", "Cleaning up orphan processes")
	return b.String()
}

// The excerpt is the failed step's own output: its failure lines and its
// end, without timestamps, colours, other steps or the post-job cleanup.
func TestCILogExcerptKeepsTheFailedStepsFailuresAndEnd(t *testing.T) {
	log := actionsPlaywrightLog(400)
	for name, step := range map[string]string{
		"the step as GitHub names a run step": "Run npx playwright test",
		// A step with its own name has no header that says so; the step that
		// printed the first error is the one that failed.
		"a step with its own name": "Playwright tests",
		"no step known":            "",
	} {
		t.Run(name, func(t *testing.T) {
			excerpt := CILogExcerpt(log, step, 1800)
			require.LessOrEqual(t, len(excerpt), 1800)
			require.True(t, utf8.ValidString(excerpt))
			// The failing test, its error and the step's verdict.
			require.Contains(t, excerpt, "✘  87 [chromium] › tests/e2e/run-chart.test.ts:58:3 › run chart › zooms out to the whole run")
			require.Contains(t, excerpt, "1) [chromium] › tests/e2e/run-chart.test.ts:58:3 › run chart › zooms out to the whole run")
			require.Contains(t, excerpt, "Error: expect(locator).toHaveAttribute(expected) failed")
			require.Contains(t, excerpt, "1 failed")
			require.Contains(t, excerpt, "211 passed (2.3m)")
			require.Contains(t, excerpt, "##[error]Process completed with exit code 1.")
			// Nothing the reader does not need.
			require.NotContains(t, excerpt, "\x1b", "colours are stripped")
			require.NotContains(t, excerpt, "2026-11-16T", "timestamps are stripped")
			require.NotContains(t, excerpt, "────", "box-drawing tails are trimmed")
			for _, other := range []string{"Post job cleanup", "git version", "failed with exit code 128", "Cleaning up orphan processes", "upload-artifact", "playwright-report", "found 0 vulnerabilities", "Syncing repository", "shell: /usr/bin/bash", "CI: true"} {
				require.NotContains(t, excerpt, other, "another step's or the cleanup's output")
			}
			require.NotContains(t, excerpt, "passing case", "passing tests never explain a failure")
			require.NotContains(t, excerpt, "page=3 ", "the step's other output gives way to its failures and its end")
		})
	}
}

// GitHub hands Kuayle only the end of a large log: a step whose header was
// cut off is still found from its error, and the excerpt stays bounded.
func TestCILogExcerptOfALogWhoseStartWasCut(t *testing.T) {
	log := actionsPlaywrightLog(400)
	cut := log[len(log)/2:]
	excerpt := CILogExcerpt(cut, "Run npx playwright test", 1200)
	require.LessOrEqual(t, len(excerpt), 1200)
	require.Contains(t, excerpt, "1 failed")
	require.Contains(t, excerpt, "Error: expect(locator).toHaveAttribute(expected) failed")
	require.NotContains(t, excerpt, "Post job cleanup")

	require.Empty(t, CILogExcerpt("", "Run tests", 1000))
	require.Empty(t, CILogExcerpt(log, "Run npx playwright test", 0))

	// A Go test failure in a step without Playwright's report.
	goLog := "2026-11-16T09:00:00.0000000Z ##[group]Run go test ./...\n" +
		"2026-11-16T09:00:00.0000000Z go test ./...\n" +
		"2026-11-16T09:00:00.0000000Z ##[endgroup]\n" +
		"2026-11-16T09:00:05.0000000Z ok  \tgithub.com/acme/app/pkg/a\t0.2s\n" +
		"2026-11-16T09:00:06.0000000Z --- FAIL: TestRetryBackoff (0.01s)\n" +
		"2026-11-16T09:00:06.0000000Z     retry_test.go:42: got 3 attempts, want 2\n" +
		"2026-11-16T09:00:06.0000000Z FAIL\n" +
		"2026-11-16T09:00:06.0000000Z FAIL\tgithub.com/acme/app/internal/retry\t0.3s\n" +
		"2026-11-16T09:00:07.0000000Z ##[error]Process completed with exit code 1.\n" +
		"2026-11-16T09:00:08.0000000Z Post job cleanup.\n"
	excerpt = CILogExcerpt(goLog, "Run go test ./...", 1000)
	require.Contains(t, excerpt, "--- FAIL: TestRetryBackoff (0.01s)")
	require.Contains(t, excerpt, "retry_test.go:42: got 3 attempts, want 2")
	require.NotContains(t, excerpt, "Post job cleanup")
}

// The stored summary names every failed check (bounded), its failed step,
// its annotations and its log excerpt, and fits the signal's column.
func TestCIFailureSummaryNamesChecksStepsTestsAndErrors(t *testing.T) {
	checks := []CIFailedCheck{
		{
			Name: "UI browser tests", Conclusion: "failure", FailedStep: "Run npx playwright test",
			Annotations:       []string{"Process completed with exit code 1.", "Error: expect(locator).toHaveAttribute(expected) failed"},
			AnnotationDetails: []string{"Process completed with exit code 1.", "UI/tests/e2e/run-chart.test.ts:61: [chromium] › run-chart.test.ts:58:3 › run chart › zooms out to the whole run: Error: expect(locator).toHaveAttribute(expected) failed"},
			LogTail:           actionsPlaywrightLog(400),
		},
		{Name: "Lint", Conclusion: "failure", Text: "eslint found 2 problems\nsrc/a.ts:3 no-unused-vars"},
	}
	summary := CIFailureSummary(checks, true)
	require.LessOrEqual(t, len(summary), CIFailureSummaryBytes)
	require.True(t, utf8.ValidString(summary))
	require.Contains(t, summary, `"UI browser tests" concluded failure in step "Run npx playwright test"`)
	require.Contains(t, summary, "run-chart.test.ts:58:3 › run chart › zooms out to the whole run")
	require.Contains(t, summary, "UI/tests/e2e/run-chart.test.ts:61")
	require.Contains(t, summary, "Error: expect(locator).toHaveAttribute(expected) failed")
	require.NotContains(t, summary, "Process completed with exit code 1.\n-", "GitHub's generic exit annotation is left out")
	require.Contains(t, summary, `"Lint" concluded failure`)
	require.Contains(t, summary, "eslint found 2 problems")
	require.Contains(t, summary, "More checks failed than Kuayle read")
	require.NotContains(t, summary, "Post job cleanup")

	// Many failed checks still fit, and each one is named.
	many := make([]CIFailedCheck, 0, 10)
	for i := range 10 {
		many = append(many, CIFailedCheck{Name: fmt.Sprintf("job %d", i), Conclusion: "failure", FailedStep: "Run npx playwright test", LogTail: actionsPlaywrightLog(200)})
	}
	summary = CIFailureSummary(many, false)
	require.LessOrEqual(t, len(summary), CIFailureSummaryBytes)
	for i := range 10 {
		require.Contains(t, summary, fmt.Sprintf(`"job %d"`, i))
	}
	require.Empty(t, CIFailureSummary(nil, false))

	// GitHub's text never breaks the column: no NUL, valid UTF-8.
	dirty := CIFailureSummary([]CIFailedCheck{{Name: "x\x00y", Conclusion: "failure", Text: "bad \xff\x00 bytes\x07"}}, false)
	require.NotContains(t, dirty, "\x00")
	require.NotContains(t, dirty, "\x07")
	require.True(t, utf8.ValidString(dirty))
}
