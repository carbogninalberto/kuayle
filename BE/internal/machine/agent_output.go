package machine

import (
	"bytes"
	"fmt"
	"github.com/kuayle/kuayle-backend/pkg/sanitize"
	"strings"
	"time"
)

const maxAgentOutputLine = 16 * 1024
const maxAgentOutputRecords = 4096

// Bound both bytes and rows. Records follow logical lines, never transport frames,
// so a manager recovering the same container reproduces identical sequence keys.
// Oversized lines are suppressed whole: retaining their prefix could leak a
// credential straddling the limit or fabricate a partial structured event.
type agentOutputBuffer struct {
	streams map[string]*agentOutputStream
	redact  *strings.Replacer
	persist func(string, int64, string, string, bool) error
}
type agentOutputStream struct {
	docker    bool
	origin    []byte
	source    string
	pending   []byte
	oversized bool
	used      int
	sequence  int64
	limited   bool
	text      cappedBuffer
}

func newAgentOutputBuffer(secrets map[string]string, persist func(string, int64, string, string, bool) error) *agentOutputBuffer {
	values := make([]string, 0, len(secrets))
	for _, value := range secrets {
		values = append(values, value)
	}
	return &agentOutputBuffer{streams: map[string]*agentOutputStream{}, redact: sanitize.SecretReplacer(values), persist: persist}
}
func (o *agentOutputBuffer) Write(stream string, data []byte) error {
	return o.write(stream, data, false)
}
func (o *agentOutputBuffer) WriteDocker(stream string, data []byte) error {
	return o.write(stream, data, true)
}
func (o *agentOutputBuffer) write(stream string, data []byte, docker bool) error {
	if stream != "stdout" && stream != "stderr" {
		return fmt.Errorf("unsupported agent output stream")
	}
	state := o.streams[stream]
	if state == nil {
		state = &agentOutputStream{text: newCappedBuffer(maxCapturedLogBytes), docker: docker}
		o.streams[stream] = state
	}
	if state.docker != docker {
		return fmt.Errorf("agent log source format changed")
	}
	if docker && state.source == "" {
		for _, b := range data {
			state.origin = append(state.origin, b)
			if b == ' ' {
				if _, err := time.Parse(time.RFC3339Nano, string(state.origin[:len(state.origin)-1])); err != nil {
					return fmt.Errorf("invalid native log timestamp")
				}
				state.source = string(state.origin[:len(state.origin)-1])
				state.origin = nil
				break
			}
			if len(state.origin) > 48 {
				return fmt.Errorf("missing native log timestamp")
			}
		}
	}
	for len(data) > 0 && !state.limited {
		if state.sequence >= maxAgentOutputRecords || state.used >= maxCapturedLogBytes {
			state.pending = nil
			state.limited = true
			return o.emit(stream, state, "[Output limit reached]\n", true)
		}
		size := len(data)
		newline := bytes.IndexByte(data, '\n')
		if newline >= 0 {
			size = newline + 1
		}
		size = min(size, maxCapturedLogBytes-state.used)
		state.used += size
		if len(state.pending)+size > maxAgentOutputLine {
			state.pending = nil
			state.oversized = true
		}
		if !state.oversized {
			state.pending = append(state.pending, data[:size]...)
		}
		complete := data[size-1] == '\n'
		data = data[size:]
		if complete {
			if err := o.flush(stream, state); err != nil {
				return err
			}
		} else if state.used == maxCapturedLogBytes {
			state.pending = nil
			state.limited = true
			return o.emit(stream, state, "[Output limit reached]\n", true)
		}
	}
	return nil
}
func (o *agentOutputBuffer) flush(stream string, state *agentOutputStream) error {
	content := string(state.pending)
	truncated := state.oversized
	state.pending = nil
	state.oversized = false
	// Claude Code streams a telemetry event every token or two; storing them
	// would exhaust the record budget and hide the agent's real output.
	if !truncated && isTokenTelemetry(content) {
		state.used -= len(content)
		return nil
	}
	if truncated {
		content = "[Oversized output line omitted]\n"
	} else {
		if state.docker {
			stamp, body, ok := strings.Cut(content, " ")
			if !ok || len(stamp) > 48 {
				return fmt.Errorf("missing native log timestamp")
			}
			if _, err := time.Parse(time.RFC3339Nano, stamp); err != nil {
				return fmt.Errorf("invalid native log timestamp")
			}
			content = body
		}
		// Sanitize only after matching raw secret bytes. PostgreSQL text rejects NUL.
		content = strings.ReplaceAll(strings.ToValidUTF8(o.redact.Replace(content), "�"), "\x00", "�")
	}
	return o.emit(stream, state, content, truncated)
}
func (o *agentOutputBuffer) emit(stream string, state *agentOutputStream, content string, truncated bool) error {
	state.sequence++
	if err := o.persist(stream, state.sequence, state.source, content, truncated); err != nil {
		return err
	}
	_, _ = state.text.Write([]byte(content))
	return nil
}
func (o *agentOutputBuffer) Finish() error {
	for _, stream := range []string{"stdout", "stderr"} {
		state := o.streams[stream]
		if state != nil && !state.limited && (len(state.pending) > 0 || state.oversized) {
			if err := o.flush(stream, state); err != nil {
				return err
			}
		}
	}
	return nil
}
func (o *agentOutputBuffer) Text(stream string) string {
	if s := o.streams[stream]; s != nil {
		return s.text.String()
	}
	return ""
}

// isTokenTelemetry matches native per-token progress events, which carry no
// information for reviewers.
func isTokenTelemetry(line string) bool {
	return strings.Contains(line, `"type":"system"`) && strings.Contains(line, `"subtype":"thinking_tokens"`) && len(line) < 512
}
