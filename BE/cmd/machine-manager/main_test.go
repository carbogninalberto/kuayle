package main

import (
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
