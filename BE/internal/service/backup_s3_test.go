package service

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestBackupPartSizeStaysWithinTheS3PartLimit(t *testing.T) {
	const mib = int64(1) << 20
	require.Equal(t, 64*mib, backupPartSize(100*mib), "small archives use 64 MiB parts")
	huge := int64(2) << 40 // 2 TiB
	part := backupPartSize(huge)
	require.Greater(t, part, 64*mib)
	require.LessOrEqual(t, (huge+part-1)/part, int64(9000), "large archives stay under the 10,000 part limit")
}

// s3Recorder is a minimal S3 endpoint that accepts single and multipart
// uploads and records which kind each request was.
type s3Recorder struct {
	mu       sync.Mutex
	requests []string
	received int64
}

func (s *s3Recorder) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()
	q := r.URL.Query()
	n, _ := io.Copy(io.Discard, r.Body)
	switch {
	case r.Method == http.MethodPost && q.Has("uploads"):
		s.requests = append(s.requests, "create")
		_, _ = io.WriteString(w, `<InitiateMultipartUploadResult><UploadId>u</UploadId></InitiateMultipartUploadResult>`)
	case r.Method == http.MethodPut && q.Has("partNumber"):
		s.requests = append(s.requests, "part")
		s.received += n
		w.Header().Set("ETag", `"p"`)
	case r.Method == http.MethodPost && q.Has("uploadId"):
		s.requests = append(s.requests, "complete")
		_, _ = io.WriteString(w, `<CompleteMultipartUploadResult><ETag>"f"</ETag></CompleteMultipartUploadResult>`)
	case r.Method == http.MethodPut:
		s.requests = append(s.requests, "put")
		s.received += n
	default:
		s.requests = append(s.requests, "other")
		w.WriteHeader(http.StatusBadRequest)
	}
}

func TestBackupLargeArchivesUseMultipartUpload(t *testing.T) {
	recorder := &s3Recorder{}
	server := httptest.NewServer(recorder)
	t.Cleanup(server.Close)
	store, err := newBackupObjectStore(BackupS3Destination{Endpoint: server.URL, Bucket: "b", Region: "us-east-1", AccessKey: "a", SecretKey: "s"}, true)
	require.NoError(t, err)

	threshold, part := backupMultipartThreshold, backupMinPartSize
	t.Cleanup(func() { backupMultipartThreshold, backupMinPartSize = threshold, part })
	backupMultipartThreshold, backupMinPartSize = 1000, 400

	dir := t.TempDir()
	small := filepath.Join(dir, "small")
	require.NoError(t, os.WriteFile(small, []byte(strings.Repeat("s", 1000)), 0o600))
	require.NoError(t, store.PutFile(context.Background(), "small", small, "sum"))
	require.Equal(t, []string{"put"}, recorder.requests, "an archive up to the threshold is one PUT")

	recorder.requests, recorder.received = nil, 0
	large := filepath.Join(dir, "large")
	require.NoError(t, os.WriteFile(large, []byte(strings.Repeat("l", 1001)), 0o600))
	require.NoError(t, store.PutFile(context.Background(), "large", large, "sum"))
	require.Equal(t, []string{"create", "part", "part", "part", "complete"}, recorder.requests)
	require.EqualValues(t, 1001, recorder.received)
}
