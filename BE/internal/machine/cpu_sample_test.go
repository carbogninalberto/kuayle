package machine

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCPUPercentMeasuresBetweenSamples(t *testing.T) {
	previous := cpuSample{total: 1_000, system: 100_000, onlineCPUs: 4}
	// Two of four cores busy: half of the system time delta, times four CPUs.
	current := cpuSample{total: 1_000 + 50_000, system: 200_000, onlineCPUs: 4}
	require.InDelta(t, 200, cpuPercent(previous, current), 0.001)
}

func TestCPUPercentNeedsAUsablePreviousSample(t *testing.T) {
	current := cpuSample{total: 5_000, system: 200_000, onlineCPUs: 4}
	require.Zero(t, cpuPercent(cpuSample{}, current), "first sample")
	require.Zero(t, cpuPercent(cpuSample{total: 9_000, system: 100_000, onlineCPUs: 4}, current), "restarted container")
	require.Zero(t, cpuPercent(current, current), "no elapsed time")
}
