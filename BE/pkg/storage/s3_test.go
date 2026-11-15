package storage

import (
	"bytes"
	"context"
	"encoding/xml"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"
)

// fakeMultipartS3 answers the multipart calls of one bucket and records
// them in order.
type fakeMultipartS3 struct {
	mu        sync.Mutex
	calls     []string
	parts     map[int][]byte
	metadata  string
	completed []int
	failPart  int
}

func (f *fakeMultipartS3) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	q := r.URL.Query()
	switch {
	case r.Method == http.MethodPost && q.Has("uploads"):
		f.calls = append(f.calls, "create")
		f.metadata = r.Header.Get("X-Amz-Meta-Sha256")
		fmt.Fprint(w, `<InitiateMultipartUploadResult><Bucket>backups</Bucket><Key>k</Key><UploadId>up-1</UploadId></InitiateMultipartUploadResult>`)
	case r.Method == http.MethodPut && q.Get("uploadId") == "up-1":
		n, _ := strconv.Atoi(q.Get("partNumber"))
		f.calls = append(f.calls, "part "+strconv.Itoa(n))
		body, _ := io.ReadAll(r.Body)
		if n == f.failPart {
			w.WriteHeader(http.StatusForbidden)
			fmt.Fprint(w, `<Error><Code>AccessDenied</Code><Message>denied</Message></Error>`)
			return
		}
		f.parts[n] = body
		w.Header().Set("ETag", fmt.Sprintf(`"etag-%d"`, n))
	case r.Method == http.MethodPost && q.Get("uploadId") == "up-1":
		f.calls = append(f.calls, "complete")
		var req struct {
			Parts []struct {
				PartNumber int
				ETag       string
			} `xml:"Part"`
		}
		body, _ := io.ReadAll(r.Body)
		_ = xml.Unmarshal(body, &req)
		for _, part := range req.Parts {
			if part.ETag != fmt.Sprintf(`"etag-%d"`, part.PartNumber) {
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			f.completed = append(f.completed, part.PartNumber)
		}
		fmt.Fprint(w, `<CompleteMultipartUploadResult><Bucket>backups</Bucket><Key>k</Key><ETag>"final"</ETag></CompleteMultipartUploadResult>`)
	case r.Method == http.MethodDelete && q.Get("uploadId") == "up-1":
		f.calls = append(f.calls, "abort")
		w.WriteHeader(http.StatusNoContent)
	default:
		f.calls = append(f.calls, "unexpected "+r.Method+" "+r.URL.String())
		w.WriteHeader(http.StatusBadRequest)
	}
}

func newMultipartTestBackend(t *testing.T, fake *fakeMultipartS3) *S3Backend {
	t.Helper()
	server := httptest.NewServer(fake)
	t.Cleanup(server.Close)
	backend, err := NewS3Backend(S3Config{Endpoint: server.URL, Bucket: "backups", Region: "us-east-1", AccessKey: "a", SecretKey: "s"})
	require.NoError(t, err)
	return backend
}

func TestS3PutFileMultipartUploadsEveryPartInOrder(t *testing.T) {
	fake := &fakeMultipartS3{parts: map[int][]byte{}}
	backend := newMultipartTestBackend(t, fake)
	body := bytes.Repeat([]byte("0123456789"), 25) // 250 bytes: parts of 100, 100, 50

	err := backend.PutFileMultipart(context.Background(), "kuayle/b.tar.gz", bytes.NewReader(body), int64(len(body)), 100,
		"application/octet-stream", map[string]string{"sha256": "abc"})
	require.NoError(t, err)
	require.Equal(t, []string{"create", "part 1", "part 2", "part 3", "complete"}, fake.calls)
	require.Equal(t, "abc", fake.metadata)
	require.Equal(t, []int{1, 2, 3}, fake.completed)
	require.Equal(t, body, append(append(fake.parts[1], fake.parts[2]...), fake.parts[3]...))
	require.Len(t, fake.parts[3], 50)
}

func TestS3PutFileMultipartAbortsWhenAPartFails(t *testing.T) {
	fake := &fakeMultipartS3{parts: map[int][]byte{}, failPart: 2}
	backend := newMultipartTestBackend(t, fake)
	body := []byte(strings.Repeat("x", 300))

	err := backend.PutFileMultipart(context.Background(), "k", bytes.NewReader(body), int64(len(body)), 100, "application/octet-stream", nil)
	require.Error(t, err)
	require.Equal(t, "create", fake.calls[0])
	require.Equal(t, "abort", fake.calls[len(fake.calls)-1], "a failed upload is aborted so no parts linger")
	require.NotContains(t, fake.calls, "complete")
	require.NotContains(t, fake.calls, "part 3", "no part is sent after a failure")
}

func TestS3PutFileMultipartRejectsABadPartSize(t *testing.T) {
	fake := &fakeMultipartS3{parts: map[int][]byte{}}
	backend := newMultipartTestBackend(t, fake)
	err := backend.PutFileMultipart(context.Background(), "k", bytes.NewReader([]byte("x")), 1, 0, "", nil)
	require.Error(t, err)
	require.Empty(t, fake.calls)
}
