package domain

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestDevMachineSizeOptions(t *testing.T) {
	options := DevMachineOptionsForWorkspace(100)

	require.Equal(t, []int{1000, 2000, 4000, 6000, 8000, 12000, 16000}, options.CPUMillis)
	require.Equal(t, []int{2048, 4096, 8192, 12288, 16384, 24576, 32768, 49152, 65536}, options.MemoryMB)
	require.Equal(t, []int{20, 30, 40, 50, 75, 100}, options.DiskGB)

	require.Equal(t, []int{20, 30, 40}, DevMachineOptionsForWorkspace(49).DiskGB)
	require.Empty(t, DevMachineOptionsForWorkspace(19).DiskGB)
	require.Equal(t, []int{20, 30, 40, 50, 75, 100, 150, 200, 300, 500, 750, 1000, 1500, 2048}, DevMachineOptionsForWorkspace(2048).DiskGB)
}

func TestDevMachineCustomSizeValidation(t *testing.T) {
	for _, test := range []struct {
		name       string
		cpuMillis  int
		memoryMB   int
		diskGB     int
		errMessage string
	}{
		{name: "minimum valid", cpuMillis: 1000, memoryMB: 2048, diskGB: 20},
		{name: "one GiB per vCPU boundary", cpuMillis: 4000, memoryMB: 4096, diskGB: 100},
		{name: "largest valid", cpuMillis: 16000, memoryMB: 65536, diskGB: 2048},
		{name: "unsupported cpu", cpuMillis: 1500, memoryMB: 4096, diskGB: 20, errMessage: "cpu_millis 1500 is not a supported option"},
		{name: "unsupported memory", cpuMillis: 2000, memoryMB: 3000, diskGB: 20, errMessage: "memory_mb 3000 is not a supported option"},
		{name: "unsupported disk", cpuMillis: 2000, memoryMB: 4096, diskGB: 25, errMessage: "disk_gb 25 is not a supported option"},
		{name: "less than one GiB per vCPU", cpuMillis: 4000, memoryMB: 2048, diskGB: 20, errMessage: "memory_mb 2048 must be at least 4096 for cpu_millis 4000 (1 GiB per vCPU)"},
	} {
		t.Run(test.name, func(t *testing.T) {
			err := DevMachineCustomSize(test.cpuMillis, test.memoryMB, test.diskGB)
			if test.errMessage == "" {
				require.NoError(t, err)
				return
			}
			require.EqualError(t, err, test.errMessage)
		})
	}
}

func TestDevMachinePresetSizesRemainUnchanged(t *testing.T) {
	expected := map[string][4]int{
		"small":  {2000, 4096, 20, 120},
		"medium": {4000, 8192, 50, 240},
		"large":  {8000, 16384, 100, 480},
	}
	for size, values := range expected {
		cpuMillis, memoryMB, diskGB, maxRuntime, ok := DevMachineSize(size)
		require.True(t, ok, size)
		require.Equal(t, values, [4]int{cpuMillis, memoryMB, diskGB, maxRuntime}, size)
	}
}
