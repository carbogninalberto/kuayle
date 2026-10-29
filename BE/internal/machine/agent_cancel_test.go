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

func TestCancelAgentRemovesEveryOwnedContainer(t *testing.T) {
	for _, firstStatus := range []int{http.StatusNoContent, http.StatusNotFound, http.StatusInternalServerError} {
		t.Run(http.StatusText(firstStatus), func(t *testing.T) {
			run := &domain.DevMachineAgentRun{ID: uuid.New(), MachineID: uuid.New(), WorkspaceID: uuid.New()}
			labels := machineLabels(&domain.DevMachine{ID: run.MachineID, WorkspaceID: run.WorkspaceID})
			labels["com.kuayle.agent-run-id"] = run.ID.String()
			var removed, removedVolumes []string
			inbox := machineLabels(&domain.DevMachine{ID: run.MachineID, WorkspaceID: run.WorkspaceID})
			inbox["com.kuayle.agent-run-id"], inbox["com.kuayle.service"] = run.ID.String(), liveInboxService
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch {
				case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/containers/json"):
					require.Equal(t, "1", r.URL.Query().Get("all"))
					_ = json.NewEncoder(w).Encode([]container.Summary{{ID: "coding", Labels: labels}, {ID: "validation", Labels: labels}, {ID: "other", Labels: map[string]string{"com.kuayle.agent-run-id": uuid.NewString()}}})
				case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/networks"):
					_, _ = w.Write([]byte(`[]`))
				case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/volumes"):
					require.Contains(t, r.URL.Query().Get("filters"), "com.kuayle.agent-run-id="+run.ID.String())
					_ = json.NewEncoder(w).Encode(map[string]any{"Volumes": []map[string]any{{"Name": "inbox", "Labels": inbox}}})
				case r.Method == http.MethodDelete && strings.Contains(r.URL.Path, "/volumes/"):
					removedVolumes = append(removedVolumes, r.URL.Path[strings.LastIndex(r.URL.Path, "/")+1:])
					w.WriteHeader(http.StatusNoContent)
				case r.Method == http.MethodDelete:
					require.Equal(t, "1", r.URL.Query().Get("force"))
					require.Equal(t, "1", r.URL.Query().Get("v"))
					id := r.URL.Path[strings.LastIndex(r.URL.Path, "/")+1:]
					removed = append(removed, id)
					if id == "coding" {
						w.WriteHeader(firstStatus)
					} else {
						w.WriteHeader(http.StatusNoContent)
					}
				default:
					http.NotFound(w, r)
				}
			}))
			defer server.Close()
			docker, err := client.New(client.WithHost(server.URL), client.WithAPIVersion("1.52"))
			require.NoError(t, err)
			defer docker.Close()
			err = (&DockerRuntime{client: docker}).CancelAgent(context.Background(), run)
			if firstStatus == http.StatusInternalServerError {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
			require.Equal(t, []string{"coding", "validation"}, removed)
			if firstStatus != http.StatusInternalServerError {
				require.Equal(t, []string{"inbox"}, removedVolumes, "the run's inbox goes with its containers")
			}
		})
	}
}

func TestCancelAgentRejectsForeignOwnershipAndContinuesCleanup(t *testing.T) {
	for _, field := range []string{"com.kuayle.managed", "com.kuayle.machine-id", "com.kuayle.workspace-id"} {
		for _, value := range []string{"", "other"} {
			t.Run(field+"/"+value, func(t *testing.T) {
				run := &domain.DevMachineAgentRun{ID: uuid.New(), MachineID: uuid.New(), WorkspaceID: uuid.New()}
				labels := machineLabels(&domain.DevMachine{ID: run.MachineID, WorkspaceID: run.WorkspaceID})
				labels["com.kuayle.agent-run-id"] = run.ID.String()
				foreign := make(map[string]string, len(labels))
				for key, val := range labels {
					foreign[key] = val
				}
				foreign[field] = value
				var removed []string
				server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					switch {
					case strings.HasSuffix(r.URL.Path, "/containers/json"):
						_ = json.NewEncoder(w).Encode([]container.Summary{{ID: "foreign", Labels: foreign}, {ID: "owned", Labels: labels}})
					case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/networks"):
						_, _ = w.Write([]byte(`[]`))
					case r.Method == http.MethodDelete:
						removed = append(removed, r.URL.Path[strings.LastIndex(r.URL.Path, "/")+1:])
						w.WriteHeader(http.StatusNoContent)
					default:
						http.NotFound(w, r)
					}
				}))
				defer server.Close()
				docker, err := client.New(client.WithHost(server.URL), client.WithAPIVersion("1.52"))
				require.NoError(t, err)
				defer docker.Close()
				err = (&DockerRuntime{client: docker}).CancelAgent(context.Background(), run)
				require.Error(t, err)
				require.Equal(t, []string{"owned"}, removed)
			})
		}
	}
}
