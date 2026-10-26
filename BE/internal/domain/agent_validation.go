package domain

import (
	"errors"
	"strings"
	"unicode/utf8"
)

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
