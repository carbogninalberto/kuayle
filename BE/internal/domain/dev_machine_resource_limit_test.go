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
	require.False(t, IsResourceLimitReason("Native coding iteration failed; stopping execution before retry"))
	require.False(t, IsResourceLimitReason(""))
}

// Production: a 16 GiB machine reported its Docker daemon container's 6.4 GiB
// share. Machines whose containers share the machine's memory name the
// machine's limit and the container whose process was stopped.
func TestMachineMemoryLimitMessageNamesTheMachineLimit(t *testing.T) {
	message := MachineMemoryLimitMessage("docker", 16<<30, false)
	require.Equal(t, "Out of memory: the machine reached its 16 GiB memory limit and a process in the Docker daemon container was stopped. Resize the machine.", message)
	require.True(t, IsResourceLimitReason(message))
	require.Equal(t, "Out of memory: the machine reached its memory limit and a process in the agent container was stopped. Resize the machine or raise the task machine memory.",
		MachineMemoryLimitMessage("agent", 0, true))
}
