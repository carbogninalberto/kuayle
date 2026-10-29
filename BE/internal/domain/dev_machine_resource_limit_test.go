package domain

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestMemoryLimitMessageNamesTheContainerAndItsLimit(t *testing.T) {
	// Production: the agent container of a 4 GiB task machine had 35%.
	limit := int64(4096) * 1024 * 1024 * 35 / 100
	message := MemoryLimitMessage("agent", limit, true)
	require.Equal(t, "Out of memory: the agent container reached its 1.4 GiB memory limit. Resize the machine or raise the task machine memory.", message)
	require.True(t, IsResourceLimitReason(message))

	require.Equal(t, "Out of memory: the terminal container reached its 2 GiB memory limit. Resize the machine.",
		MemoryLimitMessage("terminal", 2*1024*1024*1024, false))
	require.Equal(t, "Out of memory: the Docker daemon container reached its 512 MiB memory limit. Resize the machine.",
		MemoryLimitMessage("docker", 512*1024*1024, false))
	require.Equal(t, "Out of memory: the check container reached its memory limit. Resize the machine.",
		MemoryLimitMessage("validation", 0, false))
}

func TestDiskLimitMessageIsAResourceLimitReason(t *testing.T) {
	message := DiskLimitMessage(40)
	require.Equal(t, "Disk full: the workspace reached its 40 GB disk limit, so Kuayle stopped the machine. Resize the disk to continue.", message)
	require.True(t, IsResourceLimitReason(message))
	require.False(t, IsResourceLimitReason("Native coding attempt failed; stopping execution before retry"))
	require.False(t, IsResourceLimitReason(""))
}
