package machine

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/client"
	"github.com/stretchr/testify/require"
)

func TestSessionPurgeHelperIsIsolatedAndTargetsOnlyTheTask(t *testing.T) {
	volume := "workspace"
	machine := &domain.DevMachine{ID: uuid.New(), WorkspaceID: uuid.New(), RoutingKey: "fixture", WorkspaceVolumeName: &volume}
	task := uuid.New()
	var config container.Config
	var host container.HostConfig
	var started, removed int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.Contains(r.URL.Path, "/volumes/"):
			_, _ = w.Write([]byte(`{"Name":"workspace"}`))
		case strings.Contains(r.URL.Path, "/images/"):
			_, _ = w.Write([]byte(`{"Id":"image","Config":{}}`))
		case strings.HasSuffix(r.URL.Path, "/json"):
			http.NotFound(w, r)
		case strings.HasSuffix(r.URL.Path, "/containers/create"):
			var input struct {
				container.Config
				HostConfig container.HostConfig
			}
			require.NoError(t, json.NewDecoder(r.Body).Decode(&input))
			config, host = input.Config, input.HostConfig
			_, _ = w.Write([]byte(`{"Id":"purge"}`))
		case strings.HasSuffix(r.URL.Path, "/start"):
			started++
			w.WriteHeader(204)
		case strings.HasSuffix(r.URL.Path, "/wait"):
			_, _ = w.Write([]byte(`{"StatusCode":0}`))
		case r.Method == http.MethodDelete:
			removed++
			w.WriteHeader(204)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	docker, err := client.New(client.WithHost(server.URL), client.WithAPIVersion("1.52"))
	require.NoError(t, err)
	defer docker.Close()
	require.NoError(t, (&DockerRuntime{client: docker}).PurgeTaskSession(context.Background(), machine, "developer:fixture", task))
	require.Equal(t, []string{"rm"}, config.Entrypoint)
	require.Equal(t, []string{"-rf", "--", "/workspace/.kuayle/agent-sessions/" + task.String()}, config.Cmd)
	require.Equal(t, "1000:1000", config.User)
	require.Equal(t, container.NetworkMode("none"), host.NetworkMode)
	require.True(t, host.ReadonlyRootfs)
	require.Equal(t, []string{"ALL"}, host.CapDrop)
	require.Empty(t, host.CapAdd)
	require.Len(t, host.Mounts, 1)
	require.Equal(t, "workspace", host.Mounts[0].Source)
	require.Equal(t, 1, started)
	require.GreaterOrEqual(t, removed, 1, "the helper is removed afterwards")
}
