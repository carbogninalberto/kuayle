package domain

import (
	"database/sql/driver"
	"encoding/json"
	"errors"
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

// ValidateTestCommand bounds argv for preview and manual check commands.
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
