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
		require.Error(t, Run(context.Background(), []string{"apply", name, "1073741824", "0"}), name)
		require.Error(t, Run(context.Background(), []string{"remove", name}), name)
	}
}

func TestApplyRejectsLimitsBeforeContactingSystemd(t *testing.T) {
	for _, limits := range [][2]string{{"x", "0"}, {"1024", "0"}, {"1073741824", "-1"}, {"1073741824", "2147483648"}} {
		_, _, err := parseLimits(limits[0], limits[1])
		require.Error(t, err, limits)
	}
	memory, swap, err := parseLimits("17179869184", "8589934592")
	require.NoError(t, err)
	require.Equal(t, uint64(16<<30), memory)
	require.Equal(t, uint64(8<<30), swap)
	require.Error(t, Run(context.Background(), nil))
	require.Error(t, Run(context.Background(), []string{"apply", "kuayle-abc.slice"}))
	require.Error(t, Run(context.Background(), []string{"reboot"}))
}
