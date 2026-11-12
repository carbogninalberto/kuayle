package machine

import (
	"bytes"
	"fmt"
	"strings"
	"time"

	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/kuayle/kuayle-backend/pkg/sanitize"
)

const maxAgentOutputLine = 16 * 1024
const maxAgentOutputRecords = 4096

// maxAgentOutputEventLine is the longest line Kuayle buffers to cut it as a
// JSON event (see cutEventLine); a longer one is omitted whole.
const maxAgentOutputEventLine = 1 << 20

// Bound both bytes and rows. Records follow logical lines, never transport frames,
// so a manager recovering the same container reproduces identical sequence keys.
// A stored line never exceeds maxAgentOutputLine. A longer one that is a JSON
// event is redacted whole and then has its long values cut, so a tool call
// with a large output stays an event; the result is parsed from the uncut
// event, so a long final message is not lost. Any other oversized line is
// suppressed whole: retaining its prefix could leak a credential straddling
// the limit or fabricate a partial structured event.
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
	// result is what the run's result is parsed from: every line whole,
	// without Kuayle's own markers. After the storage limit nothing more is
	// stored, but lines keep reaching the parser through tail (the newest
	// maxAgentResultTail bytes of them), because a harness reports its final
	// message last. skip drops the rest of the line that was open at the
	// limit. incomplete records that the parser did not get some line.
	result     cappedBuffer
	tail       []string
	tailBytes  int
	skip       bool
	incomplete bool
	// lineBytes counts the bytes read of the logical line being read; cuts
	// is what Kuayle left out of the stored stream, by reason, and capped
	// the cut of the storage cap, which counts everything read after it.
	lineBytes int64
	cuts      map[string]*domain.OutputCut
	capped    *domain.OutputCut
}

// cut records one cut of the stream: reason, the bytes left out, the stored
// record it happened at and the limit that caused it.
func (s *agentOutputStream) cut(stream, reason string, bytes, line, limit int64) *domain.OutputCut {
	if s.cuts == nil {
		s.cuts = map[string]*domain.OutputCut{}
	}
	cut := s.cuts[reason]
	if cut == nil {
		cut = &domain.OutputCut{Stream: stream, Reason: reason, FirstLine: line, Limit: limit}
		s.cuts[reason] = cut
	}
	cut.Count++
	cut.Bytes += bytes
	return cut
}

// outputCutOrder is the order cuts are listed in.
var outputCutOrder = []string{domain.OutputCutLine, domain.OutputCutValues, domain.OutputCutRecords, domain.OutputCutBytes, domain.OutputCutTelemetry}

// Cuts lists what Kuayle left out of the run's stored output, standard
// output first: lines omitted, values cut inside events, and what came
// after a storage cap. Empty when everything is stored whole.
func (o *agentOutputBuffer) Cuts() []domain.OutputCut {
	cuts := []domain.OutputCut{}
	for _, stream := range []string{"stdout", "stderr"} {
		state := o.streams[stream]
		if state == nil {
			continue
		}
		for _, reason := range outputCutOrder {
			if cut := state.cuts[reason]; cut != nil {
				cuts = append(cuts, *cut)
			}
		}
	}
	return cuts
}

// maxAgentResultTail bounds the lines kept for the result parser after the
// storage limit.
const maxAgentResultTail = 1 << 20

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
		state = &agentOutputStream{text: newCappedBuffer(maxCapturedLogBytes), result: newCappedBuffer(maxCapturedLogBytes), docker: docker}
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
	for len(data) > 0 {
		if state.limited {
			state.capped.Bytes += int64(len(data))
			o.afterLimit(state, data)
			return nil
		}
		if state.sequence >= maxAgentOutputRecords || state.used >= maxCapturedLogBytes {
			reason, limit := domain.OutputCutBytes, int64(maxCapturedLogBytes)
			if state.sequence >= maxAgentOutputRecords {
				reason, limit = domain.OutputCutRecords, maxAgentOutputRecords
			}
			// A line that was open here is lost to storage and to the parser.
			if err := o.limit(stream, state, len(state.pending) > 0 || state.oversized, reason, limit); err != nil {
				return err
			}
			continue
		}
		size := len(data)
		newline := bytes.IndexByte(data, '\n')
		if newline >= 0 {
			size = newline + 1
		}
		size = min(size, maxCapturedLogBytes-state.used)
		state.used += size
		state.lineBytes += int64(size)
		if len(state.pending)+size > maxAgentOutputEventLine {
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
			if err := o.limit(stream, state, true, domain.OutputCutBytes, maxCapturedLogBytes); err != nil {
				return err
			}
		}
	}
	return nil
}

// limit ends storage for the stream. openLine: a line was being read, so the
// rest of it is skipped.
func (o *agentOutputBuffer) limit(stream string, state *agentOutputStream, openLine bool, reason string, limit int64) error {
	state.pending, state.oversized = nil, false
	state.limited = true
	// The marker is the next stored record; the open line is not stored.
	state.capped = state.cut(stream, reason, state.lineBytes, state.sequence+1, limit)
	state.lineBytes = 0
	state.skip, state.incomplete = openLine, state.incomplete || openLine
	return o.emit(stream, state, "[Output limit reached]\n", "", true)
}

// afterLimit keeps reading lines for the result parser once storage stopped.
// Nothing is persisted and no error stops the run.
func (o *agentOutputBuffer) afterLimit(state *agentOutputStream, data []byte) {
	for len(data) > 0 {
		size := len(data)
		newline := bytes.IndexByte(data, '\n')
		if newline >= 0 {
			size = newline + 1
		}
		if !state.skip && !state.oversized {
			if len(state.pending)+size > maxAgentOutputEventLine {
				state.pending, state.oversized = nil, true
			} else {
				state.pending = append(state.pending, data[:size]...)
			}
		}
		data = data[size:]
		if newline >= 0 {
			o.flushAfterLimit(state)
		}
	}
}
func (o *agentOutputBuffer) flushAfterLimit(state *agentOutputStream) {
	content := string(state.pending)
	lost := state.skip || state.oversized
	state.pending, state.skip, state.oversized = nil, false, false
	if !lost && isTokenTelemetry(content) {
		return
	}
	if !lost {
		_, whole, ok, err := o.line(state, content)
		if lost = err != nil || !ok; !lost {
			state.tail = append(state.tail, whole)
			state.tailBytes += len(whole)
		}
	}
	for state.tailBytes > maxAgentResultTail && len(state.tail) > 1 {
		state.tailBytes -= len(state.tail[0])
		state.tail = state.tail[1:]
		lost = true
	}
	state.incomplete = state.incomplete || lost
}

// line turns a raw logical line into what is stored and what the result is
// parsed from (the same, unless the stored line had to be cut). ok is false
// for a line over the limit that cannot be cut.
func (o *agentOutputBuffer) line(state *agentOutputStream, content string) (stored, whole string, ok bool, err error) {
	if state.docker {
		stamp, body, found := strings.Cut(content, " ")
		if !found || len(stamp) > 48 {
			return "", "", false, fmt.Errorf("missing native log timestamp")
		}
		if _, err := time.Parse(time.RFC3339Nano, stamp); err != nil {
			return "", "", false, fmt.Errorf("invalid native log timestamp")
		}
		content = body
	}
	// The limit applies to the line itself, not to Docker's timestamp.
	long := len(content) > maxAgentOutputLine
	// Sanitize only after matching raw secret bytes. PostgreSQL text rejects NUL.
	content = strings.ReplaceAll(strings.ToValidUTF8(o.redact.Replace(content), "\uFFFD"), "\x00", "\uFFFD")
	if !long {
		return content, content, true, nil
	}
	stored, whole, ok = cutEventLine(content, o.redact)
	return stored, whole, ok, nil
}

func (o *agentOutputBuffer) flush(stream string, state *agentOutputStream) error {
	content := string(state.pending)
	truncated := state.oversized
	read := state.lineBytes
	state.pending = nil
	state.oversized = false
	state.lineBytes = 0
	// Claude Code streams a telemetry event every token or two; storing them
	// would exhaust the record budget and hide the agent's real output.
	if !truncated && isTokenTelemetry(content) {
		state.used -= len(content)
		state.cut(stream, domain.OutputCutTelemetry, read, 0, 0)
		return nil
	}
	whole := ""
	if !truncated {
		var ok bool
		var err error
		if content, whole, ok, err = o.line(state, content); err != nil {
			return err
		}
		truncated = !ok
		if ok && len(whole) > len(content) {
			state.cut(stream, domain.OutputCutValues, int64(len(whole)-len(content)), state.sequence+1, maxAgentOutputLine)
		}
	}
	if truncated {
		// The parser never sees this line; it may have been the final message.
		content, whole, state.incomplete = "[Oversized output line omitted]\n", "", true
		state.cut(stream, domain.OutputCutLine, read, state.sequence+1, maxAgentOutputLine)
	}
	return o.emit(stream, state, content, whole, truncated)
}

// emit stores a record. result is the line as the result parser gets it:
// empty for Kuayle's own markers, which are not the agent's output.
func (o *agentOutputBuffer) emit(stream string, state *agentOutputStream, content, result string, truncated bool) error {
	state.sequence++
	if err := o.persist(stream, state.sequence, state.source, content, truncated); err != nil {
		return err
	}
	_, _ = state.text.Write([]byte(content))
	_, _ = state.result.Write([]byte(result))
	return nil
}
func (o *agentOutputBuffer) Finish() error {
	for _, stream := range []string{"stdout", "stderr"} {
		state := o.streams[stream]
		if state == nil || (len(state.pending) == 0 && !state.oversized && !state.skip) {
			continue
		}
		if state.limited {
			o.flushAfterLimit(state)
		} else if err := o.flush(stream, state); err != nil {
			return err
		}
	}
	return nil
}

// ResultText is the stream as the run's result is parsed from it: whole
// redacted lines, without the markers Kuayle stores in their place, followed
// by the lines that came after storage stopped.
func (o *agentOutputBuffer) ResultText(stream string) string {
	s := o.streams[stream]
	if s == nil {
		return ""
	}
	return s.result.String() + strings.Join(s.tail, "")
}

// Incomplete reports that the result parser did not get every line of the
// stream: one was too long to keep, was open when storage stopped, or was
// pushed out of the tail kept after it.
func (o *agentOutputBuffer) Incomplete(stream string) bool {
	s := o.streams[stream]
	return s != nil && s.incomplete
}

// Text is the stream as stored, markers included.
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
