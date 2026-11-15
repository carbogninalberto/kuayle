package domain

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestTemporaryMachinePresetIsAValidCustomSize(t *testing.T) {
	preset := TemporaryMachineResources
	require.NoError(t, DevMachineCustomSize(preset.CPUMillis, preset.MemoryMB, preset.DiskGB), "the preset needs no lower size floors")
	require.LessOrEqual(t, preset.CPUMillis, 2000)
	require.LessOrEqual(t, preset.MemoryMB, 2048)
}

func TestCommandWorkingDirectoryStaysInTheWorkspace(t *testing.T) {
	for input, want := range map[string]string{
		"":                       "/workspace",
		".":                      "/workspace",
		"tasks/eng-1":            "/workspace/tasks/eng-1",
		"/workspace/tasks/eng-1": "/workspace/tasks/eng-1",
		"tasks//eng-1/":          "/workspace/tasks/eng-1",
	} {
		got, ok := CommandWorkingDirectory(input)
		require.True(t, ok, input)
		require.Equal(t, want, got, input)
	}
	for _, input := range []string{"../etc", "tasks/../../etc", "/etc", "/workspacex", "tasks/\nx", "/workspace/.."} {
		_, ok := CommandWorkingDirectory(input)
		require.False(t, ok, input)
	}
}
