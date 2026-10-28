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
// Run the stop script under dash against a real process group.
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
