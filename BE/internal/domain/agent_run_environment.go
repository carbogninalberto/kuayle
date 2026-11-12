package domain

// AgentRunEnvironment is what a run ran on (KUA-137), recorded with the run
// when its container starts. The prompt by source is stored in its own table
// and the command line on the run (command_argv): the debug trace links
// them, this record does not copy them.
type AgentRunEnvironment struct {
	// Harness is the provider ID; HarnessCommand the binary the run started.
	// HarnessVersion is what that binary reported when the run started, and
	// HarnessVersionFrom where that reading came from.
	Harness            string `json:"harness"`
	HarnessCommand     string `json:"harness_command,omitempty"`
	HarnessVersion     string `json:"harness_version,omitempty"`
	HarnessVersionFrom string `json:"harness_version_from,omitempty"`
	// ImageRef is the image the run asked for, ImageID the local image it
	// resolved to and ImageDigest its repository digest, when it has one.
	ImageRef    string `json:"image_ref,omitempty"`
	ImageID     string `json:"image_id,omitempty"`
	ImageDigest string `json:"image_digest,omitempty"`
	Model       string `json:"model,omitempty"`
	Effort      string `json:"effort,omitempty"`
	// The container's limits as created.
	CPUMillis        int64 `json:"cpu_millis,omitempty"`
	MemoryBytes      int64 `json:"memory_bytes,omitempty"`
	PidsLimit        int64 `json:"pids_limit,omitempty"`
	TimeLimitSeconds int   `json:"time_limit_seconds,omitempty"`
	// PidsPeak is the most processes and threads seen in the container.
	PidsPeak int64 `json:"pids_peak,omitempty"`
	// ResumedSession is the native session the run continued, if any.
	ResumedSession   string `json:"resumed_session,omitempty"`
	WorktreeReadOnly bool   `json:"worktree_read_only,omitempty"`
	// WorktreeBefore and WorktreeAfter are the run's fingerprint of what the
	// agent was given and what it left, empty when it could not be taken.
	WorktreeBefore string `json:"worktree_before,omitempty"`
	WorktreeAfter  string `json:"worktree_after,omitempty"`
	ContainerID    string `json:"container_id,omitempty"`
}

// OutputCut reasons.
const (
	// OutputCutLine: a line over the stored line limit that was not one JSON
	// event, or still too large after its values were cut, was left out.
	OutputCutLine = "line_omitted"
	// OutputCutValues: long string values inside a JSON event were cut so
	// the event fits a stored line; the event is kept.
	OutputCutValues = "values_cut"
	// OutputCutRecords and OutputCutBytes: the stream reached its cap of
	// stored records or bytes; nothing after it is stored.
	OutputCutRecords = "record_cap"
	OutputCutBytes   = "byte_cap"
	// OutputCutSummary: the final message was longer than a summary.
	OutputCutSummary = "summary_truncated"
	// OutputCutDiff: the captured diff was truncated.
	OutputCutDiff = "diff_truncated"
	// OutputCutTelemetry: per-token progress events of the harness, which
	// carry no content, are never stored.
	OutputCutTelemetry = "telemetry_omitted"
)

// OutputCut is one kind of cut Kuayle made to a run's stored output: which
// stream, why, how many times, how many bytes were left out, and the first
// stored line it happened at (0 when it has none).
type OutputCut struct {
	Stream    string `json:"stream"`
	Reason    string `json:"reason"`
	Count     int    `json:"count"`
	Bytes     int64  `json:"bytes"`
	FirstLine int64  `json:"first_line,omitempty"`
	Limit     int64  `json:"limit,omitempty"`
}
