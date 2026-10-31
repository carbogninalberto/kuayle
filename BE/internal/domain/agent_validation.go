package domain

import (
	"bytes"
	"database/sql/driver"
	"encoding/json"
	"errors"
	"io"
	"regexp"
	"strings"
	"unicode/utf8"
)

var composePath = regexp.MustCompile(`^[A-Za-z0-9._-]+(/[A-Za-z0-9._-]+)*\.ya?ml$`)

// ValidateComposePath accepts a repository-relative .yml/.yaml path without
// dot segments; the database enforces the same rule.
func ValidateComposePath(path string) error {
	if len(path) > 255 || !composePath.MatchString(path) {
		return errors.New("invalid compose file path")
	}
	for _, segment := range strings.Split(path, "/") {
		if segment == "." || segment == ".." {
			return errors.New("invalid compose file path")
		}
	}
	return nil
}

// Commands use argv rather than a host shell expression. Nil means inherit;
// an explicit empty array means no configured check.
func ValidateTestCommand(command []string) error {
	if len(command) > 64 {
		return errors.New("verification command has too many arguments")
	}
	total := 0
	for i, arg := range command {
		total += len(arg)
		if len(arg) > 8192 || total > 32768 || strings.ContainsRune(arg, 0) || !utf8.ValidString(arg) || (i == 0 && strings.TrimSpace(arg) == "") {
			return errors.New("invalid verification command")
		}
	}
	return nil
}

// An implement attempt chooses its own tests by writing
// AgentValidationFile at its worktree root; Kuayle then reruns them in the
// credential-free check container when no check is configured. The file is
// agent output, so it is parsed strictly and bounded.
const (
	AgentValidationFile            = ".kuayle/validation.json"
	AgentValidationFileMaxBytes    = 65536
	AgentValidationMaxCommands     = 8
	AgentValidationSummaryMaxRunes = 2000
)

// Sources of an independent check's commands.
const (
	CheckSourceConfigured = "configured"
	CheckSourceAgent      = "agent"
)

// AgentDeclaredValidation is what an attempt's validation file declared, as
// recorded on the attempt: either Commands (and an optional Summary) or the
// Error that made Kuayle ignore the file.
type AgentDeclaredValidation struct {
	Commands [][]string `json:"commands,omitempty"`
	Summary  string     `json:"summary,omitempty"`
	Error    string     `json:"error,omitempty"`
}

// ParseAgentValidationFile accepts exactly {"commands": [[argv...], ...],
// "summary": "..."}: one to AgentValidationMaxCommands non-empty argv arrays
// that pass ValidateTestCommand, and a summary of at most
// AgentValidationSummaryMaxRunes characters. Anything else is rejected as a
// whole with a fixed reason that never echoes the file.
func ParseAgentValidationFile(data []byte, oversize bool) AgentDeclaredValidation {
	reject := func(reason string) AgentDeclaredValidation { return AgentDeclaredValidation{Error: reason} }
	if oversize || len(data) > AgentValidationFileMaxBytes {
		return reject("the file is larger than 64 KiB")
	}
	var file struct {
		Commands *[]json.RawMessage `json:"commands"`
		Summary  string             `json:"summary"`
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&file); err != nil {
		return reject("the file is not a JSON object with only \"commands\" and \"summary\"")
	}
	if _, err := decoder.Token(); !errors.Is(err, io.EOF) {
		return reject("the file has content after its JSON object")
	}
	if file.Commands == nil || len(*file.Commands) == 0 {
		return reject("\"commands\" must list at least one command")
	}
	if len(*file.Commands) > AgentValidationMaxCommands {
		return reject("\"commands\" lists more than 8 commands")
	}
	commands := make([][]string, 0, len(*file.Commands))
	for _, raw := range *file.Commands {
		var argv []string
		if json.Unmarshal(raw, &argv) != nil || len(argv) == 0 || ValidateTestCommand(argv) != nil {
			return reject("every command must be a non-empty array of argument strings within the check command limits")
		}
		commands = append(commands, argv)
	}
	summary := strings.TrimSpace(file.Summary)
	if !utf8.ValidString(summary) || strings.ContainsRune(summary, 0) || utf8.RuneCountInString(summary) > AgentValidationSummaryMaxRunes {
		return reject("\"summary\" must be text of at most 2000 characters")
	}
	return AgentDeclaredValidation{Commands: commands, Summary: summary}
}

// CheckCommandResult is the exit code of one command of a check, in the
// order of the step's commands.
type CheckCommandResult struct {
	ExitCode int `json:"exit_code"`
}

// CheckCommands are the argv arrays a check step runs in order: its recorded
// commands, or its single command for steps that predate them.
func (s DevMachineAgentRunStep) CheckCommands() [][]string {
	var commands [][]string
	if len(s.Commands) > 0 && json.Unmarshal(s.Commands, &commands) == nil && len(commands) > 0 {
		return commands
	}
	var argv []string
	if json.Unmarshal(s.CommandArgv, &argv) == nil && len(argv) > 0 {
		return [][]string{argv}
	}
	return nil
}

// CheckCommandRun is one command of a check with its exit code.
type CheckCommandRun struct {
	Argv     []string
	ExitCode int
}

// CheckRuns pairs the step's commands with their recorded results; a step
// without per-command results reports its single command's exit code.
func (s DevMachineAgentRunStep) CheckRuns() []CheckCommandRun {
	commands := s.CheckCommands()
	var results []CheckCommandResult
	if len(s.Commands) > 0 {
		_ = json.Unmarshal(s.CommandResults, &results)
	} else if s.ExitCode != nil && len(commands) == 1 {
		results = []CheckCommandResult{{ExitCode: *s.ExitCode}}
	}
	runs := make([]CheckCommandRun, 0, len(results))
	for i, result := range results {
		if i < len(commands) {
			runs = append(runs, CheckCommandRun{Argv: commands[i], ExitCode: result.ExitCode})
		}
	}
	return runs
}

// FailedCommand is the first command of a finished check that exited non-zero.
func (s DevMachineAgentRunStep) FailedCommand() ([]string, int, bool) {
	for _, run := range s.CheckRuns() {
		if run.ExitCode != 0 {
			return run.Argv, run.ExitCode, true
		}
	}
	return nil, 0, false
}

// NullJSON is a nullable jsonb column: NULL scans as empty and an empty value
// is written and encoded as null.
type NullJSON json.RawMessage

func (j *NullJSON) Scan(value any) error {
	switch v := value.(type) {
	case nil:
		*j = nil
	case []byte:
		*j = append((*j)[:0], v...)
	case string:
		*j = NullJSON(v)
	default:
		return errors.New("unsupported JSON value")
	}
	return nil
}

func (j NullJSON) Value() (driver.Value, error) {
	if len(j) == 0 {
		return nil, nil
	}
	return []byte(j), nil
}

func (j NullJSON) MarshalJSON() ([]byte, error) {
	if len(j) == 0 {
		return []byte("null"), nil
	}
	return j, nil
}
