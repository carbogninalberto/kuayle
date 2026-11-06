package systemdslice

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestOnlyMachineSlicesCanBeChanged(t *testing.T) {
	require.True(t, ValidName("kuayle-0123456789abcdef0123.slice"))
	for _, name := range []string{"system.slice", "kuayle.slice", "kuayle-.slice", "kuayle-ABC.slice", "kuayle-a-b.slice", "kuayle-abc.service", "kuayle-abc.slice/../system.slice"} {
		require.False(t, ValidName(name), name)
		require.Error(t, Run(context.Background(), []string{"apply", name, "1073741824", "0", "1000"}), name)
		require.Error(t, Run(context.Background(), []string{"remove", name}), name)
	}
}

func TestApplyRejectsLimitsBeforeContactingSystemd(t *testing.T) {
	for _, limits := range [][3]string{
		{"x", "0", "1000"}, {"1024", "0", "1000"}, {"1073741824", "-1", "1000"}, {"1073741824", "2147483648", "1000"},
		{"1073741824", "0", "0"}, {"1073741824", "0", "x"}, {"1073741824", "0", "2000000"},
	} {
		_, err := parseLimits(limits[0], limits[1], limits[2])
		require.Error(t, err, limits)
	}
	limits, err := parseLimits("17179869184", "8589934592", "16000")
	require.NoError(t, err)
	require.Equal(t, Limits{MemoryBytes: 16 << 30, SwapBytes: 8 << 30, CPUMillis: 16000}, limits)
	require.Error(t, Run(context.Background(), nil))
	require.Error(t, Run(context.Background(), []string{"apply", "kuayle-abc.slice", "17179869184", "0"}), "the CPU limit is required")
	require.Error(t, Run(context.Background(), []string{"reboot"}))
}
