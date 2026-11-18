package domain

import (
	"path"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
)

// The assistant runs short commands (run_command, run_command_write) on a
// Dev Machine its user owns, after the user approves each one. With no such
// machine it can propose a small temporary machine (start_temporary_machine)
// that is permanently deleted after TemporaryMachineTTL.

// DevMachineOpRunCommand runs one approved assistant command in a fresh,
// credential-free check container of the machine.
const DevMachineOpRunCommand DevMachineOperationAction = "run_command"

const (
	// CommandRunTimeout is the hard limit of a command: the container is
	// killed once it passes.
	CommandRunTimeout = 60 * time.Second
	// CommandMaxBytes bounds a command's text (also a schema CHECK).
	CommandMaxBytes = 4000
	// CommandOutputBytes is the stored tail of a run's output (also a schema
	// CHECK); the model sees a shorter tail.
	CommandOutputBytes = 64 << 10
	// CommandFolderRoot holds one folder per conversation for the files a
	// write run creates.
	CommandFolderRoot = "/workspace/kuayle-assistant"
	// TemporaryMachineTTL is the fixed lifetime of a temporary machine, from
	// its creation; it is never extended.
	TemporaryMachineTTL = 30 * time.Minute
	// DefaultMaxTemporaryMachines is the workspace quota of live temporary
	// machines unless the policy sets one from 0 (off) to
	// MaxTemporaryMachinesLimit.
	DefaultMaxTemporaryMachines = 2
	MaxTemporaryMachinesLimit   = 10
)

// TemporaryMachineResources is the tiny headless preset of a temporary
// machine. It satisfies DevMachineCustomSize: the smallest CPU and memory
// options (1 GiB per vCPU) and the smallest disk.
var TemporaryMachineResources = TaskMachineResources{CPUMillis: 1000, MemoryMB: 2048, DiskGB: 20}

// CommandFolder is the working folder of a conversation's write runs.
func CommandFolder(conversation uuid.UUID) string {
	return CommandFolderRoot + "/" + conversation.String()
}

// CommandWorkingDirectory resolves a working directory relative to
// /workspace. It refuses absolute paths outside /workspace, parent
// references and control characters; "" is /workspace.
func CommandWorkingDirectory(relative string) (string, bool) {
	relative = strings.TrimSpace(relative)
	if relative == "" || relative == "." || relative == "/workspace" {
		return "/workspace", true
	}
	if len(relative) > 500 || !utf8.ValidString(relative) {
		return "", false
	}
	for _, r := range relative {
		if r < 0x20 || r == 0x7f {
			return "", false
		}
	}
	if trimmed, ok := strings.CutPrefix(relative, "/workspace/"); ok {
		relative = trimmed
	} else if strings.HasPrefix(relative, "/") {
		return "", false
	}
	// Mirrors the CHECK (working_directory NOT LIKE '%..%') of
	// assistant_command_runs: any ".." anywhere, not only a whole segment.
	if strings.Contains(relative, "..") {
		return "", false
	}
	return path.Clean("/workspace/" + relative), true
}

// CommandRun is one approved command on a machine and its result. The
// machine's name and size are kept with the run so the transcript still
// names them after a temporary machine is gone.
type CommandRun struct {
	ID                uuid.UUID  `json:"id" db:"id"`
	WorkspaceID       uuid.UUID  `json:"workspace_id" db:"workspace_id"`
	MachineID         *uuid.UUID `json:"machine_id" db:"machine_id"`
	MachineName       string     `json:"machine" db:"machine_name"`
	CPUMillis         int        `json:"cpu_millis" db:"cpu_millis"`
	MemoryMB          int        `json:"memory_mb" db:"memory_mb"`
	DiskGB            int        `json:"disk_gb" db:"disk_gb"`
	Temporary         bool       `json:"temporary" db:"temporary"`
	Reused            bool       `json:"reused" db:"reused"`
	RequestedByUserID *uuid.UUID `json:"requested_by_user_id" db:"requested_by_user_id"`
	ConversationID    *uuid.UUID `json:"conversation_id" db:"conversation_id"`
	ToolCallID        *uuid.UUID `json:"tool_call_id" db:"tool_call_id"`
	ApprovalID        *uuid.UUID `json:"approval_id" db:"approval_id"`
	Command           string     `json:"command" db:"command"`
	Write             bool       `json:"write" db:"write"`
	// Migration marks a run started by run_migration; ActionName names the
	// admin-configured migration action it ran ("" for an ordinary command).
	Migration        bool       `json:"migration" db:"migration"`
	ActionName       string     `json:"action,omitempty" db:"action_name"`
	WorkingDirectory string     `json:"working_directory" db:"working_directory"`
	Status           string     `json:"status" db:"status"`
	ExitCode         *int       `json:"exit_code" db:"exit_code"`
	Output           string     `json:"output" db:"output"`
	OutputTruncated  bool       `json:"output_truncated" db:"output_truncated"`
	Error            *string    `json:"error" db:"error"`
	CreatedAt        time.Time  `json:"created_at" db:"created_at"`
	StartedAt        *time.Time `json:"started_at" db:"started_at"`
	FinishedAt       *time.Time `json:"finished_at" db:"finished_at"`
	DurationMS       *int64     `json:"duration_ms" db:"duration_ms"`
	// ExpiresAt is the temporary machine's deletion time, when it has one.
	ExpiresAt *time.Time `json:"expires_at,omitempty" db:"expires_at"`
}

// CommandMachine is the machine an approval card names: which one, its size,
// whether it is temporary (and until when) and whether the assistant reused
// one of the user's machines instead of starting one.
type CommandMachine struct {
	MachineID uuid.UUID  `json:"machine_id"`
	Machine   string     `json:"machine"`
	CPUMillis int        `json:"cpu_millis"`
	MemoryMB  int        `json:"memory_mb"`
	DiskGB    int        `json:"disk_gb"`
	Temporary bool       `json:"temporary"`
	Reused    bool       `json:"reused"`
	ExpiresAt *time.Time `json:"expires_at,omitempty"`
}

// CommandMachineOf describes a machine for a card or a result.
func CommandMachineOf(machine *DevMachine, reused bool) CommandMachine {
	return CommandMachine{MachineID: machine.ID, Machine: machine.Name, CPUMillis: machine.CPUMillis, MemoryMB: machine.MemoryMB, DiskGB: machine.DiskGB,
		Temporary: machine.TemporaryExpiresAt != nil, Reused: reused, ExpiresAt: machine.TemporaryExpiresAt}
}

// CommandRunPayload is the approval of run_command or run_command_write: the
// exact command, where it runs and on which machine.
type CommandRunPayload struct {
	CommandMachine
	Command          string `json:"command"`
	Write            bool   `json:"write"`
	WorkingDirectory string `json:"working_directory"`
	TimeoutSeconds   int    `json:"timeout_seconds"`
}

// TemporaryMachinePayload is the approval of start_temporary_machine: the
// preset, its lifetime and the workspace quota when it was requested.
type TemporaryMachinePayload struct {
	CPUMillis  int    `json:"cpu_millis"`
	MemoryMB   int    `json:"memory_mb"`
	DiskGB     int    `json:"disk_gb"`
	Profile    string `json:"profile"`
	TTLMinutes int    `json:"ttl_minutes"`
	QuotaUsed  int    `json:"quota_used"`
	QuotaLimit int    `json:"quota_limit"`
	// WorkingDirectory is the conversation's folder on the machine.
	WorkingDirectory string `json:"working_directory"`
	Credentials      bool   `json:"credentials"`
}
