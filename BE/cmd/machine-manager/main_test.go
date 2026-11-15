package main

import (
	"os"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/kuayle/kuayle-backend/internal/machine"
	"github.com/stretchr/testify/require"
)

func TestShutdownDrain(t *testing.T) {
	for _, scenario := range []struct {
		value string
		want  time.Duration
	}{
		{"", machine.DefaultShutdownDrain},
		{"0", 0},
		{"300", 300 * time.Second},
		{"-5", machine.DefaultShutdownDrain},
		{"abc", machine.DefaultShutdownDrain},
	} {
		t.Run(scenario.value, func(t *testing.T) {
			t.Setenv("DEV_MACHINE_SHUTDOWN_DRAIN_SECONDS", scenario.value)
			require.Equal(t, scenario.want, shutdownDrain())
		})
	}
}

// cleanupAfterDrain is what a stopping manager still needs after the drain:
// the operations it stops clean up (a check's compose services for up to a
// minute) and hand themselves over before Docker kills the process.
const cleanupAfterDrain = time.Minute

// The Compose stop grace period leaves the cleanups after the drain room to
// finish, and the Compose default drain is the manager's own.
func TestComposeStopGraceLeavesRoomAfterTheDrain(t *testing.T) {
	grace := regexp.MustCompile(`(?m)^\s+stop_grace_period:\s*(\d+)s\s*$`)
	drain := regexp.MustCompile(`DEV_MACHINE_SHUTDOWN_DRAIN_SECONDS:\s*\$\{DEV_MACHINE_SHUTDOWN_DRAIN_SECONDS:-(\d+)\}`)
	for _, path := range []string{"../../../docker-compose.yml", "../../../selfhosting/docker-compose.yml"} {
		t.Run(path, func(t *testing.T) {
			data, err := os.ReadFile(path)
			require.NoError(t, err)
			service := composeService(string(data), "machine-manager")
			require.NotEmpty(t, service, "machine-manager service")
			graceMatch, drainMatch := grace.FindStringSubmatch(service), drain.FindStringSubmatch(service)
			require.NotNil(t, graceMatch, "stop_grace_period")
			require.NotNil(t, drainMatch, "DEV_MACHINE_SHUTDOWN_DRAIN_SECONDS default")
			graceSeconds, _ := strconv.Atoi(graceMatch[1])
			drainSeconds, _ := strconv.Atoi(drainMatch[1])
			require.Equal(t, machine.DefaultShutdownDrain, time.Duration(drainSeconds)*time.Second, "the Compose default is the manager's")
			require.GreaterOrEqual(t, time.Duration(graceSeconds-drainSeconds)*time.Second, cleanupAfterDrain, "stop_grace_period must outlast the drain by the cleanups")
		})
	}
}

// composeService is the text of one top-level Compose service, up to the next
// service at the same indentation.
func composeService(compose, name string) string {
	var lines []string
	inside := false
	for _, line := range strings.Split(compose, "\n") {
		top := strings.HasPrefix(line, "  ") && len(line) > 2 && line[2] != ' ' && line[2] != '#'
		if top || (len(line) > 0 && line[0] != ' ' && line[0] != '#') {
			if inside {
				break
			}
			inside = line == "  "+name+":"
		}
		if inside {
			lines = append(lines, line)
		}
	}
	return strings.Join(lines, "\n")
}
