package machine

import (
	"os"
	"os/exec"
	"strconv"
	"syscall"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

// The developer container's /bin/sh is dash, whose kill builtin rejects "--".
// Run the stop script (which ends a preview an earlier release started in the
// developer container) under dash against a real process group.
func TestStopPreviewScriptStopsTheProcessGroupUnderDash(t *testing.T) {
	if _, err := os.Stat("/bin/dash"); err != nil {
		t.Skip("dash is not installed")
	}
	app := exec.Command("sleep", "300")
	app.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	require.NoError(t, app.Start())
	exited := make(chan struct{})
	go func() { _ = app.Wait(); close(exited) }()
	t.Cleanup(func() { _ = syscall.Kill(-app.Process.Pid, syscall.SIGKILL) })

	task := uuid.New().String()
	pidfile := "/tmp/kuayle-preview-" + task + ".pid"
	require.NoError(t, os.WriteFile(pidfile, []byte(strconv.Itoa(app.Process.Pid)), 0o600))
	t.Cleanup(func() { _ = os.Remove(pidfile) })

	out, err := exec.Command("/bin/dash", "-c", stopPreviewScript, "kuayle-preview-stop", task).CombinedOutput()
	require.NoError(t, err, string(out))
	select {
	case <-exited:
	case <-time.After(10 * time.Second):
		t.Fatal("the preview process group is still running")
	}
	_, err = os.Stat(pidfile)
	require.True(t, os.IsNotExist(err), "the pidfile is removed")
}

// A preview container's command line runs the argv as a child, passes its exit
// code on, and ends it at once when the container is stopped (SIGTERM to the
// runner), also when the command installs no handler. Run it under dash.
func TestPreviewRunnerPassesTheExitCodeAndStopsTheCommandOnTerm(t *testing.T) {
	if _, err := os.Stat("/bin/dash"); err != nil {
		t.Skip("dash is not installed")
	}
	failed := exec.Command("/bin/dash", "-c", previewRunner, "kuayle-preview", "sh", "-c", "exit 7")
	err := failed.Run()
	var exit *exec.ExitError
	require.ErrorAs(t, err, &exit)
	require.Equal(t, 7, exit.ExitCode(), "a command that fails at once is reported with its code")

	marker := "kuayle-preview-runner-" + uuid.NewString()
	runner := exec.Command("/bin/dash", "-c", previewRunner, "kuayle-preview", "sh", "-c", "exec sleep 300 # "+marker)
	runner.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	require.NoError(t, runner.Start())
	t.Cleanup(func() { _ = syscall.Kill(-runner.Process.Pid, syscall.SIGKILL) })
	exited := make(chan error, 1)
	go func() { exited <- runner.Wait() }()
	time.Sleep(500 * time.Millisecond)
	require.NoError(t, runner.Process.Signal(syscall.SIGTERM))
	select {
	case err := <-exited:
		require.ErrorAs(t, err, &exit)
		require.Equal(t, 128+int(syscall.SIGTERM), exit.ExitCode())
	case <-time.After(10 * time.Second):
		t.Fatal("the runner kept running after SIGTERM")
	}
	require.ErrorIs(t, syscall.Kill(-runner.Process.Pid, 0), syscall.ESRCH, "the command ended with the runner")
}
