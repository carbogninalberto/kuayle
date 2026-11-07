package machine

import (
	"context"
	"net"
	"testing"
	"time"

	"github.com/moby/moby/client"
	"github.com/stretchr/testify/require"
)

// The exec never answers, like a daemon stuck under heavy disk load.
type stalledExecClientFake struct {
	*dockerClientFake
	server net.Conn
}

func (f *stalledExecClientFake) ExecCreate(context.Context, string, client.ExecCreateOptions) (client.ExecCreateResult, error) {
	return client.ExecCreateResult{ID: "exec"}, nil
}

func (f *stalledExecClientFake) ExecAttach(context.Context, string, client.ExecAttachOptions) (client.ExecAttachResult, error) {
	conn, server := net.Pipe()
	f.server = server
	return client.ExecAttachResult{HijackedResponse: client.NewHijackedResponse(conn, "")}, nil
}

func TestExecOutputStopsWhenItsContextEnds(t *testing.T) {
	fake := &stalledExecClientFake{dockerClientFake: &dockerClientFake{}}
	runtime := &DockerRuntime{client: fake}
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	done := make(chan error, 1)
	go func() {
		_, err := runtime.execOutput(ctx, "collector", []string{"du", "-sb", "/workspace"})
		done <- err
	}()
	select {
	case err := <-done:
		require.Error(t, err)
	case <-time.After(5 * time.Second):
		t.Fatal("a stalled exec held the caller past its deadline")
	}
	_ = fake.server.Close()
}
