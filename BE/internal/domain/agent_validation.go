package domain

import (
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
