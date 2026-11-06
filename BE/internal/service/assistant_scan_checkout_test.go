package service

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

const scanSHA = "89abcdef0123456789abcdef0123456789abcdef"

type tarEntry struct {
	name, body, link string
	kind             byte
	size             int64 // overrides len(body) for size-cap tests
}

func tarball(t *testing.T, entries ...tarEntry) []byte {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	require.NoError(t, tw.WriteHeader(&tar.Header{Name: "pax_global_header", Typeflag: tar.TypeXGlobalHeader, PAXRecords: map[string]string{"comment": scanSHA}}))
	for _, entry := range entries {
		kind := entry.kind
		if kind == 0 {
			kind = tar.TypeReg
		}
		header := &tar.Header{Name: entry.name, Typeflag: kind, Mode: 0o644, Linkname: entry.link}
		if kind == tar.TypeReg {
			header.Size = int64(len(entry.body))
		}
		if kind == tar.TypeDir {
			header.Mode = 0o755
		}
		require.NoError(t, tw.WriteHeader(header))
		if kind == tar.TypeReg {
			_, err := tw.Write([]byte(entry.body))
			require.NoError(t, err)
		}
	}
	require.NoError(t, tw.Close())
	require.NoError(t, gz.Close())
	return buf.Bytes()
}

// snapshotFake serves one archive and records what it was asked for.
type snapshotFake struct {
	archive []byte
	err     error
	calls   atomic.Int32
	gate    chan struct{}
}

func (f *snapshotFake) RepositorySnapshot(ctx context.Context, _ uuid.UUID, repository, ref string) (*RepositorySnapshot, error) {
	f.calls.Add(1)
	if f.gate != nil {
		select {
		case <-f.gate:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	if f.err != nil {
		return nil, f.err
	}
	return &RepositorySnapshot{Commit: scanSHA, Archive: io.NopCloser(bytes.NewReader(f.archive))}, nil
}

func incompressible(t *testing.T, n int) string {
	t.Helper()
	data := make([]byte, n)
	_, err := rand.Read(data)
	require.NoError(t, err)
	return hex.EncodeToString(data)
}

func newTestCheckouts(t *testing.T, source repositorySnapshots, limits scanLimits) *RepositoryCheckouts {
	t.Helper()
	parent := t.TempDir()
	c := newRepositoryCheckouts(source, parent, limits)
	t.Cleanup(c.Close)
	return c
}

func testScanLimits() scanLimits {
	return scanLimits{archiveBytes: 1 << 20, totalBytes: 1 << 20, fileBytes: 64 << 10, files: 100, ttl: time.Minute, checkouts: 4}
}

func TestCheckoutRejectsTraversalAndNeverCreatesLinks(t *testing.T) {
	outside := t.TempDir()
	archive := tarball(t,
		tarEntry{name: "acme-app-89abcde/", kind: tar.TypeDir},
		tarEntry{name: "acme-app-89abcde/main.go", body: "package main\n"},
		tarEntry{name: "acme-app-89abcde/../escape.txt", body: "outside"},
		tarEntry{name: "acme-app-89abcde/a/../../escape2.txt", body: "outside"},
		tarEntry{name: "/etc/kuayle-scan-test", body: "absolute"},
		tarEntry{name: "acme-app-89abcde/link", kind: tar.TypeSymlink, link: outside},
		tarEntry{name: "acme-app-89abcde/up", kind: tar.TypeSymlink, link: "../../.."},
		tarEntry{name: "acme-app-89abcde/hard", kind: tar.TypeLink, link: "acme-app-89abcde/main.go"},
		tarEntry{name: "acme-app-89abcde/.git/config", body: "[credential]"},
		tarEntry{name: "acme-app-89abcde/link/inside.txt", body: "through the link"},
	)
	c := newTestCheckouts(t, &snapshotFake{archive: archive}, testScanLimits())
	co, release, err := c.Acquire(context.Background(), uuid.New(), "acme/app", "main")
	require.NoError(t, err)
	defer release()

	require.Equal(t, scanSHA, co.commit)
	paths := co.paths()
	require.Contains(t, paths, "main.go")
	require.NotContains(t, paths, ".git/config", "a .git directory is never materialised")
	for _, path := range paths {
		require.False(t, strings.HasPrefix(path, "..") || strings.HasPrefix(path, "/"), path)
	}
	require.Equal(t, 3, co.omitted.Links, "symlinks and hard links are counted, never created")
	entries, err := os.ReadDir(outside)
	require.NoError(t, err)
	require.Empty(t, entries, "nothing was written through a link")
	_, err = os.Stat(filepath.Join(filepath.Dir(co.dir), "escape.txt"))
	require.True(t, os.IsNotExist(err))
	err = filepath.Walk(co.dir, func(path string, info os.FileInfo, err error) error {
		require.NoError(t, err)
		require.Zero(t, info.Mode()&os.ModeSymlink, "no symlink on disk: %s", path)
		return nil
	})
	require.NoError(t, err)

	_, err = co.read("link/inside.txt")
	require.Error(t, err)
	for _, bad := range []string{"../escape.txt", "/etc/passwd", "a/../../x", ".git/config", "main.go/../../x"} {
		_, err := co.read(bad)
		require.Error(t, err, bad)
	}
	data, err := co.read("./main.go")
	require.NoError(t, err)
	require.Equal(t, "package main\n", string(data))
}

func TestCheckoutReadRefusesASymlinkPlantedAfterExtraction(t *testing.T) {
	archive := tarball(t, tarEntry{name: "r/secret.txt", body: "x"})
	c := newTestCheckouts(t, &snapshotFake{archive: archive}, testScanLimits())
	co, release, err := c.Acquire(context.Background(), uuid.New(), "acme/app", "main")
	require.NoError(t, err)
	defer release()
	target := filepath.Join(t.TempDir(), "host-secret")
	require.NoError(t, os.WriteFile(target, []byte("host"), 0o600))
	require.NoError(t, os.Remove(filepath.Join(co.dir, "secret.txt")))
	require.NoError(t, os.Symlink(target, filepath.Join(co.dir, "secret.txt")))
	_, err = co.read("secret.txt")
	require.Error(t, err, "reads go through os.Root and never follow a link out of the checkout")
}

func TestCheckoutCapsSizesAndMarksBinaries(t *testing.T) {
	limits := testScanLimits()
	limits.fileBytes = 10
	archive := tarball(t,
		tarEntry{name: "r/small.txt", body: "hello"},
		tarEntry{name: "r/big.txt", body: strings.Repeat("x", 11)},
		tarEntry{name: "r/image.png", body: "\x89PNG\x00\x01"},
	)
	c := newTestCheckouts(t, &snapshotFake{archive: archive}, limits)
	co, release, err := c.Acquire(context.Background(), uuid.New(), "acme/app", "main")
	require.NoError(t, err)
	defer release()
	require.Equal(t, 1, co.omitted.TooLarge)
	big := co.file("big.txt")
	require.NotNil(t, big, "a file over the cap is listed")
	require.Equal(t, "too_large", big.Omitted)
	_, err = co.read("big.txt")
	require.Error(t, err)
	require.True(t, co.file("image.png").Binary)
	_, err = os.Stat(filepath.Join(co.dir, "big.txt"))
	require.True(t, os.IsNotExist(err), "an oversized file is not written")

	limits = testScanLimits()
	limits.totalBytes = 8
	c = newTestCheckouts(t, &snapshotFake{archive: tarball(t, tarEntry{name: "r/a", body: "12345"}, tarEntry{name: "r/b", body: "67890"})}, limits)
	_, _, err = c.Acquire(context.Background(), uuid.New(), "acme/app", "main")
	require.Error(t, err)
	require.Contains(t, toolErrorText(err), "larger than")

	limits = testScanLimits()
	limits.archiveBytes = 64
	c = newTestCheckouts(t, &snapshotFake{archive: tarball(t, tarEntry{name: "r/a", body: incompressible(t, 600)})}, limits)
	_, _, err = c.Acquire(context.Background(), uuid.New(), "acme/app", "main")
	require.Error(t, err)
	require.Contains(t, toolErrorText(err), "larger than")
	entries, _ := os.ReadDir(c.root)
	require.Empty(t, entries, "a refused checkout leaves nothing behind")

	limits = testScanLimits()
	limits.files = 2
	c = newTestCheckouts(t, &snapshotFake{archive: tarball(t, tarEntry{name: "r/a", body: "1"}, tarEntry{name: "r/b", body: "2"}, tarEntry{name: "r/c", body: "3"})}, limits)
	co, release, err = c.Acquire(context.Background(), uuid.New(), "acme/app", "main")
	require.NoError(t, err)
	defer release()
	require.True(t, co.truncated)
	require.Len(t, co.paths(), 2)
}

func TestCheckoutRespectsGitignore(t *testing.T) {
	archive := tarball(t,
		tarEntry{name: "r/.gitignore", body: "# build output\n*.log\n/dist/\nnode_modules\n!keep.log\n"},
		tarEntry{name: "r/app.log", body: "x"},
		tarEntry{name: "r/keep.log", body: "x"},
		tarEntry{name: "r/dist/bundle.js", body: "x"},
		tarEntry{name: "r/src/dist/real.go", body: "x"},
		tarEntry{name: "r/web/node_modules/lib/index.js", body: "x"},
		tarEntry{name: "r/web/.gitignore", body: "generated/\n"},
		tarEntry{name: "r/web/generated/out.ts", body: "x"},
		tarEntry{name: "r/generated/kept.ts", body: "x"},
		tarEntry{name: "r/src/main.go", body: "x"},
	)
	c := newTestCheckouts(t, &snapshotFake{archive: archive}, testScanLimits())
	co, release, err := c.Acquire(context.Background(), uuid.New(), "acme/app", "main")
	require.NoError(t, err)
	defer release()
	require.ElementsMatch(t, []string{".gitignore", "keep.log", "src/dist/real.go", "web/.gitignore", "generated/kept.ts", "src/main.go"}, co.paths())
	require.Equal(t, 4, co.omitted.Ignored)
	_, err = co.read("app.log")
	require.Error(t, err, "an ignored file cannot be read")
}

func TestCheckoutsAreSharedExpireAndAreRemovedOnClose(t *testing.T) {
	source := &snapshotFake{archive: tarball(t, tarEntry{name: "r/a.go", body: "package a"}), gate: make(chan struct{})}
	c := newTestCheckouts(t, source, testScanLimits())
	now := time.Now()
	c.now = func() time.Time { return now }
	ws := uuid.New()
	var wg sync.WaitGroup
	results := make([]*repositoryCheckout, 4)
	for i := range results {
		wg.Go(func() {
			co, release, err := c.Acquire(context.Background(), ws, "acme/app", "main")
			require.NoError(t, err)
			results[i] = co
			release()
		})
	}
	time.Sleep(50 * time.Millisecond)
	close(source.gate)
	wg.Wait()
	require.EqualValues(t, 1, source.calls.Load(), "concurrent scans share one download")
	dir := results[0].dir
	for _, co := range results {
		require.Same(t, results[0], co)
	}

	other, release, err := c.Acquire(context.Background(), uuid.New(), "acme/app", "main")
	require.NoError(t, err)
	release()
	require.NotEqual(t, dir, other.dir, "workspaces never share a checkout")

	now = now.Add(2 * time.Minute)
	c.Sweep()
	_, err = os.Stat(dir)
	require.True(t, os.IsNotExist(err), "an expired checkout is deleted")
	_, release, err = c.Acquire(context.Background(), ws, "acme/app", "main")
	require.NoError(t, err)
	release()
	require.EqualValues(t, 3, source.calls.Load(), "an expired checkout is downloaded again")

	root := c.root
	c.Close()
	_, err = os.Stat(root)
	require.True(t, os.IsNotExist(err), "shutdown removes every checkout")
	_, _, err = c.Acquire(context.Background(), ws, "acme/app", "main")
	require.Error(t, err, "a closed scanner refuses new checkouts")
}

func TestCheckoutInUseSurvivesSweepAndFailuresAreNotCached(t *testing.T) {
	source := &snapshotFake{archive: tarball(t, tarEntry{name: "r/a.go", body: "package a"})}
	c := newTestCheckouts(t, source, testScanLimits())
	now := time.Now()
	c.now = func() time.Time { return now }
	co, release, err := c.Acquire(context.Background(), uuid.New(), "acme/app", "main")
	require.NoError(t, err)
	now = now.Add(time.Hour)
	c.Sweep()
	_, err = co.read("a.go")
	require.NoError(t, err, "a checkout being read is not deleted under the reader")
	release()
	c.Sweep()
	_, err = os.Stat(co.dir)
	require.True(t, os.IsNotExist(err))

	failing := &snapshotFake{err: errors.New("GitHub down")}
	c = newTestCheckouts(t, failing, testScanLimits())
	ws := uuid.New()
	_, _, err = c.Acquire(context.Background(), ws, "acme/app", "main")
	require.Error(t, err)
	_, _, err = c.Acquire(context.Background(), ws, "acme/app", "main")
	require.Error(t, err)
	require.EqualValues(t, 2, failing.calls.Load(), "a failed download is retried, not cached")
}

func TestStaleCheckoutRootsFromEarlierProcessesAreRemoved(t *testing.T) {
	parent := t.TempDir()
	stale := filepath.Join(parent, scanRootPrefix+"old")
	fresh := filepath.Join(parent, scanRootPrefix+"new")
	unrelated := filepath.Join(parent, "something-else")
	for _, dir := range []string{stale, fresh, unrelated} {
		require.NoError(t, os.MkdirAll(dir, 0o700))
	}
	old := time.Now().Add(-2 * time.Hour)
	require.NoError(t, os.Chtimes(stale, old, old))
	removeStaleScanRoots(parent, time.Hour, time.Now())
	_, err := os.Stat(stale)
	require.True(t, os.IsNotExist(err))
	_, err = os.Stat(fresh)
	require.NoError(t, err, "another live process's checkouts are kept")
	_, err = os.Stat(unrelated)
	require.NoError(t, err)
}

func TestGlobMatching(t *testing.T) {
	for _, tc := range []struct {
		pattern, path string
		want          bool
	}{
		{"**/*.go", "main.go", true},
		{"**/*.go", "BE/internal/x.go", true},
		{"BE/**/*_test.go", "BE/internal/service/a_test.go", true},
		{"BE/**/*_test.go", "UI/a_test.go", false},
		{"*.go", "BE/x.go", false},
		{"src/**", "src/a/b.ts", true},
		{"src/*.ts", "src/a/b.ts", false},
		{"**", "anything/at/all", true},
		{"[ab].txt", "a.txt", true},
	} {
		require.Equal(t, tc.want, matchGlob(tc.pattern, tc.path), "%s ~ %s", tc.pattern, tc.path)
	}
}
