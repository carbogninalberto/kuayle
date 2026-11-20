package machine

import (
	"context"
	"encoding/binary"
	"io"
	"net"
	"strings"
	"testing"

	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/moby/moby/client"
	"github.com/stretchr/testify/require"
)

type logExecClient struct {
	*dockerClientFake
	target         string
	exec           client.ExecCreateOptions
	stdout, stderr string
	exit           int
}

func (f *logExecClient) ExecCreate(_ context.Context, id string, options client.ExecCreateOptions) (client.ExecCreateResult, error) {
	f.target, f.exec = id, options
	return client.ExecCreateResult{ID: "read"}, nil
}
func (f *logExecClient) ExecAttach(context.Context, string, client.ExecAttachOptions) (client.ExecAttachResult, error) {
	conn, server := net.Pipe()
	go func() {
		defer server.Close()
		for i, text := range []string{f.stdout, f.stderr} {
			header := [8]byte{byte(i + 1)}
			binary.BigEndian.PutUint32(header[4:], uint32(len(text)))
			server.Write(header[:])
			io.WriteString(server, text)
		}
	}()
	return client.ExecAttachResult{HijackedResponse: client.NewHijackedResponse(conn, "")}, nil
}
func (f *logExecClient) ExecInspect(context.Context, string, client.ExecInspectOptions) (client.ExecInspectResult, error) {
	return client.ExecInspectResult{ExitCode: f.exit}, nil
}

func TestMachineLogsUseOnlyTheMachineDaemon(t *testing.T) {
	id := "private-daemon"
	services := []domain.DevMachineService{{ServiceType: "docker", Status: "running", ContainerID: &id}}
	client := &logExecClient{stdout: "newest\nolder\noldest\n"}
	runtime := &DockerRuntime{client: client}
	read := func(kind domain.WorktreeScanKind, container string) (*domain.WorktreeScanResult, error) {
		return runtime.ScanWorktree(context.Background(), &domain.DevMachine{}, services, WorktreeScanJob{Request: domain.WorktreeScanRequest{Kind: kind, Container: container, Limit: 2}})
	}
	got, err := read(domain.MachineScanContainers, "")
	require.NoError(t, err)
	require.Equal(t, id, client.target)
	require.Equal(t, "1000:1000", client.exec.User)
	require.Equal(t, []string{"ps", "--filter", "status=running", "--last", "3", "--format", "{{.ID}}\t{{.Names}}\t{{.Image}}\t{{.Status}}"}, client.exec.Cmd[8:])
	require.Equal(t, "newest\nolder", got.Content)
	require.True(t, got.Truncated)

	client.stdout, client.stderr = strings.Repeat("x", refreshOutputCapture)+"sensitive suffix\n", "last stderr line\n"
	got, err = read(domain.MachineScanContainerLogs, "example-1")
	require.NoError(t, err)
	require.Equal(t, []string{"logs", "--tail", "3", "--", "example-1"}, client.exec.Cmd[8:])
	require.Equal(t, "last stderr line\n", got.Content)
	require.True(t, got.Truncated)
	client.exit = 1
	got, err = read(domain.MachineScanContainerLogs, "example-1")
	require.Nil(t, got)
	require.Error(t, err)
	require.NotContains(t, err.Error(), "sensitive suffix")
	client.target = ""
	_, err = read(domain.MachineScanContainerLogs, "--follow")
	require.Error(t, err)
	require.Empty(t, client.target)
	services = nil
	_, err = read(domain.MachineScanContainers, "")
	require.ErrorContains(t, err, "no running Docker service")
	require.Empty(t, client.target)
}
