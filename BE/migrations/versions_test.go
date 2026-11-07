package migrations_test

import (
	"os"
	"regexp"
	"strconv"
	"testing"

	"github.com/stretchr/testify/require"
)

// golang-migrate refuses to start when two files share a version, so two
// branches that each add the next number must not both reach main.
func TestMigrationVersionsAreUnique(t *testing.T) {
	entries, err := os.ReadDir(".")
	require.NoError(t, err)
	name := regexp.MustCompile(`^(\d+)_(.+)\.(up|down)\.sql$`)
	seen := map[string]string{}
	for _, entry := range entries {
		match := name.FindStringSubmatch(entry.Name())
		if match == nil {
			continue
		}
		// Versions are compared as numbers, as golang-migrate does.
		version, err := strconv.ParseUint(match[1], 10, 64)
		require.NoError(t, err, entry.Name())
		key := strconv.FormatUint(version, 10) + "." + match[3]
		if other, ok := seen[key]; ok {
			t.Errorf("migration version %d is used by both %s and %s", version, other, entry.Name())
			continue
		}
		seen[key] = entry.Name()
	}
	require.NotEmpty(t, seen)
}
