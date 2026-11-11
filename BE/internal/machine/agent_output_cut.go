package machine

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"unicode/utf8"
)

// cutEventLine shortens an oversized line that is one JSON object (a harness
// event) to fit maxAgentOutputLine by cutting its long string values, the
// longest first: a tool call keeps its name, command and status and loses the
// middle of its output. Decoded values are redacted before they are cut, so a
// secret the raw line spelled with other escapes is neither stored nor left
// as a fragment at a cut. It returns the line to store and the whole redacted
// event, which the run's result is parsed from; ok is false when the line is
// not such an object or cannot be made to fit.
func cutEventLine(line string, redact *strings.Replacer) (stored, whole string, ok bool) {
	body := strings.TrimSpace(line)
	if !strings.HasPrefix(body, "{") {
		return "", "", false
	}
	decoder := json.NewDecoder(strings.NewReader(body))
	// Timestamps and IDs keep their digits.
	decoder.UseNumber()
	var event map[string]any
	if err := decoder.Decode(&event); err != nil {
		return "", "", false
	}
	if _, err := decoder.Token(); err != io.EOF {
		return "", "", false
	}
	event = mapEventStrings(event, func(value string) string { return redact.Replace(value) }).(map[string]any)
	whole, ok = encodeEvent(event)
	if !ok {
		return "", "", false
	}
	for limit := maxAgentOutputLine / 2; limit >= 256; limit /= 2 {
		cut := mapEventValues(event, func(value string) string { return cutEventValue(value, limit) })
		if stored, ok = encodeEvent(cut); ok && len(stored) <= maxAgentOutputLine {
			return stored, whole, true
		}
	}
	return "", "", false
}

// mapEventStrings applies change to every string of a decoded event, keys
// included; mapEventValues leaves the keys.
func mapEventStrings(value any, change func(string) string) any {
	return mapEvent(value, change, true)
}

func mapEventValues(value any, change func(string) string) any {
	return mapEvent(value, change, false)
}

func mapEvent(value any, change func(string) string, keys bool) any {
	switch typed := value.(type) {
	case string:
		return change(typed)
	case []any:
		items := make([]any, len(typed))
		for i, item := range typed {
			items[i] = mapEvent(item, change, keys)
		}
		return items
	case map[string]any:
		fields := make(map[string]any, len(typed))
		for key, field := range typed {
			if keys {
				key = change(key)
			}
			fields[key] = mapEvent(field, change, keys)
		}
		return fields
	}
	return value
}

// cutEventValue keeps the start and the end of a value longer than limit
// bytes (the end of a command's output is where it says how it went) and
// says how much was left out.
func cutEventValue(value string, limit int) string {
	if len(value) <= limit {
		return value
	}
	head, tail := limit*3/4, limit/4
	for head > 0 && !utf8.RuneStart(value[head]) {
		head--
	}
	start := len(value) - tail
	for start < len(value) && !utf8.RuneStart(value[start]) {
		start++
	}
	return fmt.Sprintf("%s\n… [%d bytes cut by Kuayle] …\n%s", value[:head], start-head, value[start:])
}

func encodeEvent(event any) (string, bool) {
	var buffer bytes.Buffer
	encoder := json.NewEncoder(&buffer)
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(event); err != nil {
		return "", false
	}
	// Encode ends the line.
	return buffer.String(), true
}
