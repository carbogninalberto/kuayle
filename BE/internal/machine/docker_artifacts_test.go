package machine

import (
	"archive/tar"
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/moby/moby/client"
	"github.com/stretchr/testify/require"
)

func TestValidationArtifactsKeepOnlyBoundedReportFiles(t *testing.T) {
	archive := func(entries ...tar.Header) []byte {
		var buffer bytes.Buffer
		writer := tar.NewWriter(&buffer)
		for _, entry := range entries {
			header := entry
			if header.Typeflag == tar.TypeReg {
				header.Size = int64(len(header.Linkname))
			}
			content := header.Linkname
			if header.Typeflag == tar.TypeReg {
				header.Linkname = ""
			}
			require.NoError(t, writer.WriteHeader(&header))
			if header.Typeflag == tar.TypeReg {
				_, _ = writer.Write([]byte(content))
			}
		}
		require.NoError(t, writer.Close())
		return buffer.Bytes()
	}
	// Linkname carries file content for regular entries in this fixture.
	report := archive(
		tar.Header{Name: "playwright-report/index.html", Typeflag: tar.TypeReg, Linkname: "<html>report</html>"},
		tar.Header{Name: "playwright-report/data/shot.png", Typeflag: tar.TypeReg, Linkname: "PNG"},
		tar.Header{Name: "playwright-report/app.js", Typeflag: tar.TypeReg, Linkname: "alert(1)"},
		tar.Header{Name: "playwright-report/secrets.png", Typeflag: tar.TypeSymlink, Linkname: "/run/kuayle-secrets/GITHUB_TOKEN"},
		tar.Header{Name: "playwright-report/../../etc/passwd.txt", Typeflag: tar.TypeReg, Linkname: "root"},
		tar.Header{Name: "elsewhere/notes.txt", Typeflag: tar.TypeReg, Linkname: "outside"},
	)
	var requested []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "/archive") {
			http.NotFound(w, r)
			return
		}
		source := r.URL.Query().Get("path")
		requested = append(requested, source)
		if strings.HasSuffix(source, "/test-results") {
			http.Error(w, `{"message":"No such container:path"}`, http.StatusNotFound)
			return
		}
		w.Header().Set("X-Docker-Container-Path-Stat", "eyJuYW1lIjoicGxheXdyaWdodC1yZXBvcnQiLCJzaXplIjowLCJtb2RlIjoyMTQ3NDg0MDAwfQ==")
		_, _ = w.Write(report)
	}))
	defer server.Close()
	docker, err := client.New(client.WithHost(server.URL), client.WithAPIVersion("1.52"))
	require.NoError(t, err)
	defer docker.Close()
	machine := &domain.DevMachine{RoutingKey: "fixture"}
	run := &domain.DevMachineAgentRun{ID: uuid.New()}
	artifacts, err := (&DockerRuntime{client: docker}).CollectValidationArtifacts(context.Background(), machine, run, nil, &domain.DevMachineCheckout{WorkspacePath: "/workspace/tasks/rec-1"})
	require.NoError(t, err)
	require.Equal(t, []string{"/workspace/tasks/rec-1/playwright-report", "/workspace/tasks/rec-1/test-results"}, requested)
	paths := map[string]string{}
	for _, artifact := range artifacts {
		paths[artifact.Path] = artifact.ContentType
	}
	require.Equal(t, map[string]string{"playwright-report/index.html": "text/html", "playwright-report/data/shot.png": "image/png"}, paths,
		"scripts, links, traversal and files outside report directories are dropped")
}
