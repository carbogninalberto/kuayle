package domain

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestDevMachineSizeOptions(t *testing.T) {
	options := DevMachineOptionsForWorkspace(100)

	require.Equal(t, []int{1000, 2000, 4000, 6000, 8000, 12000, 16000}, options.CPUMillis)
	require.Equal(t, []int{2048, 4096, 8192, 12288, 16384, 24576, 32768, 49152, 65536}, options.MemoryMB)
	// Disk is offered in 2 GiB steps from 20 GiB up to the workspace limit.
	require.Len(t, options.DiskGB, 41)
	require.Equal(t, []int{20, 22, 24}, options.DiskGB[:3])
	require.Equal(t, 100, options.DiskGB[len(options.DiskGB)-1])
	for index := 1; index < len(options.DiskGB); index++ {
		require.Equal(t, 2, options.DiskGB[index]-options.DiskGB[index-1])
	}

	require.Equal(t, []int{20, 22, 24, 26}, DevMachineOptionsForWorkspace(27).DiskGB, "an odd limit offers the even sizes below it")
	require.Empty(t, DevMachineOptionsForWorkspace(19).DiskGB)
	all := DevMachineOptionsForWorkspace(4096).DiskGB
	require.Len(t, all, 1015)
	require.Equal(t, 2048, all[len(all)-1], "never above the supported maximum")
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
		{name: "disk in 2 GiB steps", cpuMillis: 2000, memoryMB: 4096, diskGB: 62},
		{name: "odd disk", cpuMillis: 2000, memoryMB: 4096, diskGB: 25, errMessage: "disk_gb 25 is not a supported option"},
		{name: "disk below minimum", cpuMillis: 2000, memoryMB: 4096, diskGB: 18, errMessage: "disk_gb 18 is not a supported option"},
		{name: "disk above maximum", cpuMillis: 2000, memoryMB: 4096, diskGB: 2050, errMessage: "disk_gb 2050 is not a supported option"},
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

func TestTaskMachineResourcesDefaultToEightGiB(t *testing.T) {
	require.Equal(t, TaskMachineResources{CPUMillis: 4000, MemoryMB: 8192, DiskGB: 20}, DefaultTaskMachineResources)
	// A policy that has none stored yet (for example a fake) falls back per field.
	policy := &DevMachineWorkspacePolicy{TaskMachineMemoryMB: 16384}
	require.Equal(t, TaskMachineResources{CPUMillis: 4000, MemoryMB: 16384, DiskGB: 20}, policy.TaskMachine())
	policy = &DevMachineWorkspacePolicy{TaskMachineCPUMillis: 8000, TaskMachineMemoryMB: 32768, TaskMachineDiskGB: 64}
	require.Equal(t, TaskMachineResources{CPUMillis: 8000, MemoryMB: 32768, DiskGB: 64}, policy.TaskMachine())
}

func TestValidateTaskMachineResources(t *testing.T) {
	for _, test := range []struct {
		name       string
		resources  TaskMachineResources
		maxDiskGB  int
		errMessage string
	}{
		{name: "default", resources: DefaultTaskMachineResources, maxDiskGB: 100},
		{name: "large", resources: TaskMachineResources{CPUMillis: 16000, MemoryMB: 65536, DiskGB: 2048}, maxDiskGB: 2048},
		{name: "unsupported memory", resources: TaskMachineResources{CPUMillis: 4000, MemoryMB: 10000, DiskGB: 20}, maxDiskGB: 100, errMessage: "memory_mb 10000 is not a supported option"},
		{name: "one GiB per vCPU", resources: TaskMachineResources{CPUMillis: 8000, MemoryMB: 4096, DiskGB: 20}, maxDiskGB: 100, errMessage: "memory_mb 4096 must be at least 8192 for cpu_millis 8000 (1 GiB per vCPU)"},
		{name: "above workspace disk limit", resources: TaskMachineResources{CPUMillis: 4000, MemoryMB: 8192, DiskGB: 120}, maxDiskGB: 100, errMessage: "task machine disk_gb 120 exceeds the workspace limit of 100 GB"},
	} {
		t.Run(test.name, func(t *testing.T) {
			err := ValidateTaskMachineResources(test.resources, test.maxDiskGB)
			if test.errMessage == "" {
				require.NoError(t, err)
				return
			}
			require.EqualError(t, err, test.errMessage)
		})
	}
}

func TestValidDevMachineDiskGB(t *testing.T) {
	for value, valid := range map[int]bool{20: true, 42: true, 2048: true, 18: false, 41: false, 2050: false} {
		require.Equal(t, valid, ValidDevMachineDiskGB(value), value)
	}
}
