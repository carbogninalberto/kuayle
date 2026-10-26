package machine

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/moby/moby/api/pkg/stdcopy"
	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/client"
	"github.com/stretchr/testify/require"
	"sync/atomic"
)

func TestAgentOutputArrivesBeforeNativeProcessExits(t *testing.T) {
	release := make(chan struct{})
	var once sync.Once
	finish := func() { once.Do(func() { close(release) }) }
	defer finish()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/logs"):
			require.Equal(t, "1", r.URL.Query().Get("follow"))
			require.Equal(t, "1", r.URL.Query().Get("timestamps"))
			writeDockerFrame(w, stdcopy.Stdout, "2026-10-26T20:00:00.000000001Z working\n")
			w.(http.Flusher).Flush()
			select {
			case <-release:
			case <-r.Context().Done():
				return
			}
			writeDockerFrame(w, stdcopy.Stderr, "2026-10-26T20:00:01.000000001Z finished\n")
		case strings.HasSuffix(r.URL.Path, "/wait"):
			select {
			case <-release:
			case <-r.Context().Done():
				return
			}
			_, _ = w.Write([]byte(`{"StatusCode":0}`))
		case strings.HasSuffix(r.URL.Path, "/stop"):
			finish()
			w.WriteHeader(http.StatusNoContent)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	docker, err := client.New(client.WithHost(server.URL), client.WithAPIVersion("1.52"))
	require.NoError(t, err)
	defer docker.Close()
	runtime := &DockerRuntime{client: docker}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	arrived := make(chan string, 4)
	done := make(chan error, 1)
	go func() {
		result, err := runtime.resumeAgent(ctx, container.InspectResponse{ID: "test", State: &container.State{Running: true}}, false, nil, &AgentObserver{Output: func(stream string, data []byte) error { arrived <- stream + ":" + string(data); return nil }})
		if err == nil && (result.Stdout != "2026-10-26T20:00:00.000000001Z working\n" || result.Stderr != "2026-10-26T20:00:01.000000001Z finished\n") {
			err = errors.New("final output was not preserved")
		}
		done <- err
	}()
	select {
	case got := <-arrived:
		require.Equal(t, "stdout:2026-10-26T20:00:00.000000001Z working\n", got)
	case <-time.After(time.Second):
		finish()
		t.Fatal("no live output before process exit")
	}
	select {
	case <-done:
		t.Fatal("process unexpectedly exited")
	default:
	}
	finish()
	require.NoError(t, <-done)
	require.Equal(t, "stderr:2026-10-26T20:00:01.000000001Z finished\n", <-arrived)
}

func writeDockerFrame(w io.Writer, stream stdcopy.StdType, content string) {
	header := make([]byte, 8)
	header[0] = byte(stream)
	binary.BigEndian.PutUint32(header[4:], uint32(len(content)))
	_, _ = w.Write(append(header, []byte(content)...))
}

type capturedAgentLine struct {
	stream    string
	sequence  int64
	content   string
	truncated bool
}

func collectAgentOutput(secrets map[string]string) (*agentOutputBuffer, *[]capturedAgentLine) {
	records := new([]capturedAgentLine)
	output := newAgentOutputBuffer(secrets, func(stream string, sequence int64, source string, content string, truncated bool) error {
		*records = append(*records, capturedAgentLine{stream, sequence, content, truncated})
		return nil
	})
	return output, records
}
func TestAgentOutputRedactsBeforePersistenceAcrossFrames(t *testing.T) {
	output, records := collectAgentOutput(map[string]string{"TOKEN": "test-secret-123", "KEY": "line-one\nline-two", "JSON": "quote\"slash\\value"})
	for _, part := range []string{"first test-sec", "ret-123\nline-one\nline-", "two\nquote\\\"slash\\\\value\nlast"} {
		require.NoError(t, output.Write("stdout", []byte(part)))
	}
	require.Len(t, *records, 4, "complete lines must persist while execution is running")
	require.NoError(t, output.Finish())
	require.Equal(t, "first [REDACTED]\n[REDACTED]\n[REDACTED]\n[REDACTED]\nlast", output.Text("stdout"))
	for i, line := range *records {
		require.Equal(t, int64(i+1), line.sequence)
		require.NotContains(t, line.content, "secret-123")
	}
}
func TestAgentOutputReplayHasStableRecordsAndBounds(t *testing.T) {
	input := "first\n" + strings.Repeat("x", maxAgentOutputLine+20) + "\nlast\n"
	one, a := collectAgentOutput(nil)
	split, b := collectAgentOutput(nil)
	require.NoError(t, one.Write("stdout", []byte(input)))
	require.NoError(t, one.Finish())
	for start := 0; start < len(input); start += 7 {
		require.NoError(t, split.Write("stdout", []byte(input[start:min(start+7, len(input))])))
	}
	require.NoError(t, split.Finish())
	require.Equal(t, *a, *b)
	require.Len(t, *a, 3)
	require.True(t, (*a)[1].truncated)
	require.NotContains(t, (*a)[1].content, "xxx")
	bounded, lines := collectAgentOutput(nil)
	for range maxAgentOutputRecords + 100 {
		require.NoError(t, bounded.Write("stdout", []byte("a\n")))
	}
	require.NoError(t, bounded.Finish())
	require.Len(t, *lines, maxAgentOutputRecords+1)
	require.True(t, (*lines)[len(*lines)-1].truncated)
}
func TestAgentOutputPersistenceFailureAndTruncationDoNotLeakTail(t *testing.T) {
	failure := errors.New("store unavailable")
	output := newAgentOutputBuffer(nil, func(string, int64, string, string, bool) error { return failure })
	require.ErrorIs(t, output.Write("stdout", []byte("work\n")), failure)
	bounded, lines := collectAgentOutput(map[string]string{"TOKEN": "tail-secret"})
	// Put a partial secret at the capture boundary. It must not be flushed as text.
	require.NoError(t, bounded.Write("stdout", []byte(strings.Repeat("x", maxCapturedLogBytes-5)+"tail-secret\n")))
	require.NoError(t, bounded.Finish())
	for _, line := range *lines {
		require.NotContains(t, line.content, "tail-")
		require.True(t, line.truncated)
	}
}

func TestAgentOutputFailureStopsNativeProcess(t *testing.T) {
	for _, scenario := range []string{"persistence_failure", "cancelled"} {
		t.Run(scenario, func(t *testing.T) {
			stopped := make(chan struct{}, 1)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch {
				case strings.HasSuffix(r.URL.Path, "/logs"):
					writeDockerFrame(w, stdcopy.Stdout, "working\n")
					w.(http.Flusher).Flush()
					<-r.Context().Done()
				case strings.HasSuffix(r.URL.Path, "/wait"):
					<-r.Context().Done()
				case strings.HasSuffix(r.URL.Path, "/stop"):
					stopped <- struct{}{}
					w.WriteHeader(http.StatusNoContent)
				default:
					http.NotFound(w, r)
				}
			}))
			defer server.Close()
			docker, err := client.New(client.WithHost(server.URL), client.WithAPIVersion("1.52"))
			require.NoError(t, err)
			defer docker.Close()
			runtime := &DockerRuntime{client: docker}
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			failure := errors.New("persistence unavailable")
			started := time.Now()
			_, err = runtime.resumeAgent(ctx, container.InspectResponse{ID: "test", State: &container.State{Running: true}}, false, nil, &AgentObserver{Output: func(string, []byte) error {
				if scenario == "cancelled" {
					cancel()
					return nil
				}
				return failure
			}})
			require.Less(t, time.Since(started), time.Second, "stop does not wait for the process timeout")
			if scenario == "cancelled" {
				require.ErrorIs(t, err, context.Canceled)
			} else {
				require.ErrorIs(t, err, failure)
			}
			select {
			case <-stopped:
			default:
				t.Fatal("native process was not stopped")
			}
		})
	}
}

func TestDockerOutputTracksRetainedPrefixAndRemovesTimestamps(t *testing.T) {
	var sources, contents []string
	output := newAgentOutputBuffer(map[string]string{"TOKEN": "fixture-secret"}, func(_ string, _ int64, source, content string, _ bool) error {
		sources = append(sources, source)
		contents = append(contents, content)
		return nil
	})
	raw := "2026-10-26T20:00:00.000000001Z first fixture-secret\n2026-10-26T20:00:01.000000001Z second\n"
	for i := 0; i < len(raw); i += 3 {
		require.NoError(t, output.WriteDocker("stdout", []byte(raw[i:min(i+3, len(raw))])))
	}
	require.NoError(t, output.Finish())
	require.Equal(t, []string{"first [REDACTED]\n", "second\n"}, contents)
	require.Equal(t, []string{"2026-10-26T20:00:00.000000001Z", "2026-10-26T20:00:00.000000001Z"}, sources)
	invalid := newAgentOutputBuffer(nil, func(string, int64, string, string, bool) error { t.Fatal("invalid source persisted"); return nil })
	require.Error(t, invalid.WriteDocker("stdout", []byte("untrusted prefix\n")))
}

func TestAgentStartObserverRunsAfterStartAndStopsOnRejection(t *testing.T) {
	for _, scenario := range []string{"created", "paused", "running", "exited", "start_failed", "rejected"} {
		t.Run(scenario, func(t *testing.T) {
			var mu sync.Mutex
			var calls []string
			record := func(call string) { mu.Lock(); defer mu.Unlock(); calls = append(calls, call) }
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch {
				case strings.HasSuffix(r.URL.Path, "/exec"):
					var body struct{ AttachStdin bool }
					if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
						http.Error(w, "bad request", 400)
						return
					}
					id := "clear"
					if body.AttachStdin {
						id = "ready"
					}
					_ = json.NewEncoder(w).Encode(map[string]string{"Id": id})
				case strings.Contains(r.URL.Path, "/exec/") && strings.HasSuffix(r.URL.Path, "/start"):
					_, _ = io.Copy(io.Discard, r.Body)
					conn, rw, err := w.(http.Hijacker).Hijack()
					if err != nil {
						return
					}
					defer conn.Close()
					_, _ = rw.WriteString("HTTP/1.1 101 UPGRADED\r\nContent-Type: application/vnd.docker.raw-stream\r\nConnection: Upgrade\r\nUpgrade: tcp\r\n\r\n")
					_ = rw.Flush()
					if strings.Contains(r.URL.Path, "/ready/") {
						_, _ = io.Copy(io.Discard, conn)
					}
				case strings.Contains(r.URL.Path, "/exec/") && strings.HasSuffix(r.URL.Path, "/json"):
					_, _ = w.Write([]byte(`{"ExitCode":0}`))
				case strings.HasSuffix(r.URL.Path, "/start"), strings.HasSuffix(r.URL.Path, "/unpause"):
					record("start")
					if scenario == "start_failed" {
						http.Error(w, "start failed", http.StatusInternalServerError)
						return
					}
					w.WriteHeader(http.StatusNoContent)
				case strings.HasSuffix(r.URL.Path, "/stop"):
					record("stop")
					w.WriteHeader(http.StatusNoContent)
				case strings.HasSuffix(r.URL.Path, "/logs"):
					record("logs")
				case strings.HasSuffix(r.URL.Path, "/wait"):
					_, _ = w.Write([]byte(`{"StatusCode":0}`))
				default:
					http.NotFound(w, r)
				}
			}))
			defer server.Close()
			docker, err := client.New(client.WithHost("tcp://"+strings.TrimPrefix(server.URL, "http://")), client.WithAPIVersion("1.52"))
			require.NoError(t, err)
			defer docker.Close()
			runtime := &DockerRuntime{client: docker}
			state := &container.State{Status: "created"}
			if scenario == "paused" {
				state.Paused = true
			}
			if scenario == "running" || scenario == "rejected" {
				state.Running = true
			}
			if scenario == "exited" {
				state.Status = "exited"
			}
			rejected := errors.New("start no longer authorized")
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			_, err = runtime.resumeAgent(ctx, container.InspectResponse{ID: "native", State: state}, false, nil, &AgentObserver{Started: func(id string) error {
				require.Equal(t, "native", id)
				record("observer")
				if scenario == "rejected" {
					return rejected
				}
				return nil
			}})
			mu.Lock()
			got := append([]string(nil), calls...)
			mu.Unlock()
			switch scenario {
			case "start_failed":
				require.Error(t, err)
				require.Equal(t, []string{"start"}, got)
			case "rejected":
				require.ErrorIs(t, err, rejected)
				require.Equal(t, []string{"observer", "stop"}, got)
			case "created", "paused":
				require.NoError(t, err)
				require.Equal(t, []string{"start", "observer", "logs"}, got)
			default:
				require.NoError(t, err)
				require.Equal(t, []string{"observer", "logs"}, got)
			}
		})
	}
}

func TestAgentAuthorizationAfterPreparationPreventsStart(t *testing.T) {
	var cancelled atomic.Bool
	var starts, removes atomic.Int32
	denied := errors.New("task cancelled during container preparation")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.Contains(r.URL.Path, "/images/"):
			_, _ = w.Write([]byte(`{"Id":"image","Config":{}}`))
		case strings.HasSuffix(r.URL.Path, "/containers/create"):
			// Cancellation happens after the manager's pre-dispatch check, while
			// Docker is still allocating the container, before any process starts.
			cancelled.Store(true)
			_, _ = w.Write([]byte(`{"Id":"native"}`))
		case strings.HasSuffix(r.URL.Path, "/start"):
			starts.Add(1)
			http.Error(w, "must not start", 500)
		case r.Method == http.MethodDelete:
			removes.Add(1)
			w.WriteHeader(204)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	docker, err := client.New(client.WithHost(server.URL), client.WithAPIVersion("1.52"))
	require.NoError(t, err)
	defer docker.Close()
	runtime := &DockerRuntime{client: docker}
	machine := &domain.DevMachine{ID: uuid.New(), WorkspaceID: uuid.New(), RoutingKey: "fixture", DockerNetworkName: ptr("network"), WorkspaceVolumeName: ptr("volume")}
	run := &domain.DevMachineAgentRun{ID: uuid.New(), CommandArgv: []byte(`["codex"]`)}
	_, err = runtime.RunAgent(context.Background(), machine, run, &domain.DevMachineAgentProvider{ImageRef: "fixture"}, nil, nil, &AgentObserver{Authorize: func() error {
		if cancelled.Load() {
			return denied
		}
		return nil
	}})
	require.ErrorIs(t, err, denied)
	require.Zero(t, starts.Load())
	require.Equal(t, int32(1), removes.Load())
}

func TestAgentRecoveryAuthorizationPreventsResume(t *testing.T) {
	for _, state := range []container.State{{Status: "created"}, {Status: "paused", Paused: true}, {Status: "running", Running: true}} {
		t.Run(string(state.Status), func(t *testing.T) {
			var starts, stops atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if strings.HasSuffix(r.URL.Path, "/stop") {
					stops.Add(1)
					w.WriteHeader(204)
					return
				}
				if strings.HasSuffix(r.URL.Path, "/start") || strings.HasSuffix(r.URL.Path, "/unpause") {
					starts.Add(1)
				}
				http.Error(w, "unexpected request", 500)
			}))
			defer server.Close()
			docker, err := client.New(client.WithHost(server.URL), client.WithAPIVersion("1.52"))
			require.NoError(t, err)
			defer docker.Close()
			runtime := &DockerRuntime{client: docker}
			denied := errors.New("task revoked")
			_, err = runtime.resumeAgent(context.Background(), container.InspectResponse{ID: "native", State: &state}, false, nil, &AgentObserver{Authorize: func() error { return denied }})
			require.ErrorIs(t, err, denied)
			require.Zero(t, starts.Load())
			require.Equal(t, int32(1), stops.Load())
		})
	}
}
