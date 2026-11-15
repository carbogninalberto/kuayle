package sanitize

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"strings"
)

// StripNUL removes NUL characters, which PostgreSQL text and jsonb reject.
func StripNUL(s string) string { return strings.ReplaceAll(s, "\x00", "") }

// JSONWithoutNUL returns raw JSON with NUL removed from every string and
// object key, so PostgreSQL jsonb accepts it (it rejects the \u0000 escape,
// SQLSTATE 22P05). JSON without that escape is returned unchanged; otherwise
// the value is re-encoded with sorted keys and numbers kept exactly.
func JSONWithoutNUL(raw []byte) ([]byte, error) {
	if !bytes.Contains(raw, []byte(`\u0000`)) {
		return raw, nil
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	var value any
	if err := decoder.Decode(&value); err != nil {
		return nil, err
	}
	if _, err := decoder.Token(); !errors.Is(err, io.EOF) {
		return nil, errors.New("sanitize: trailing data after JSON value")
	}
	var out bytes.Buffer
	encoder := json.NewEncoder(&out)
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(withoutNUL(value)); err != nil {
		return nil, err
	}
	return bytes.TrimSuffix(out.Bytes(), []byte("\n")), nil
}

func withoutNUL(value any) any {
	switch typed := value.(type) {
	case string:
		return StripNUL(typed)
	case map[string]any:
		cleaned := make(map[string]any, len(typed))
		for key, item := range typed {
			cleaned[StripNUL(key)] = withoutNUL(item)
		}
		return cleaned
	case []any:
		for index, item := range typed {
			typed[index] = withoutNUL(item)
		}
		return typed
	default:
		return value
	}
}
