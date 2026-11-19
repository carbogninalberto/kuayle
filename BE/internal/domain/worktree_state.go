package domain

import "regexp"

var worktreeFingerprint = regexp.MustCompile(`^([0-9a-f]{40,64}) ([0-9a-f]{40,64}) [0-9a-f]{40,64}$`)

// WorktreeContent reads the commit and non-ignored tree from the machine's
// fingerprint. The third hash records ignored paths for planning steps.
func WorktreeContent(state string) (head, tree string, ok bool) {
	parts := worktreeFingerprint.FindStringSubmatch(state)
	if parts == nil {
		return "", "", false
	}
	return parts[1], parts[2], true
}
