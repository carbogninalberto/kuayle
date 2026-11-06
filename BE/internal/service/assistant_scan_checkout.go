package service

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	log "github.com/sirupsen/logrus"
)

// The repository scan tools read a temporary, read-only snapshot of one commit
// of a workspace repository. The API server downloads the commit's archive from
// GitHub with a read-only installation token and extracts it in Go under a
// per-process temporary directory, outside any working tree. No git binary or
// .git directory is involved, so the token never reaches the disk; it is held in
// memory for the download only. Checkouts are shared by the tool calls of one
// workspace for a short TTL and deleted when they expire and on shutdown.

// RepositorySnapshot is the archive of one commit.
type RepositorySnapshot struct {
	Commit  string
	Archive io.ReadCloser
}

// repositorySnapshots downloads the archive of a ref of a workspace repository.
type repositorySnapshots interface {
	RepositorySnapshot(ctx context.Context, ws uuid.UUID, repository, ref string) (*RepositorySnapshot, error)
}

type scanLimits struct {
	// archiveBytes bounds the compressed download, totalBytes the extracted
	// files and fileBytes one file (larger files are listed, not written).
	archiveBytes, totalBytes, fileBytes int64
	// files bounds the files of a checkout; the rest are not materialised.
	files int
	ttl   time.Duration
	// checkouts bounds the checkouts kept at once.
	checkouts int
}

var defaultScanLimits = scanLimits{
	archiveBytes: 100 << 20,
	totalBytes:   300 << 20,
	fileBytes:    2 << 20,
	files:        30000,
	ttl:          15 * time.Minute,
	checkouts:    8,
}

const (
	scanRootPrefix = "kuayle-repo-scan-"
	// scanDownloadTimeout bounds one download and extraction.
	scanDownloadTimeout = 2 * time.Minute
	binarySniffBytes    = 8000
)

// RepositoryCheckouts owns the temporary checkouts of this process.
type RepositoryCheckouts struct {
	source repositorySnapshots
	limits scanLimits
	parent string
	root   string
	now    func() time.Time

	mu     sync.Mutex
	items  map[string]*repositoryCheckout
	closed bool
}

type scanOmitted struct {
	// Ignored: matched a .gitignore. Links: symlinks and hard links, which are
	// never created. TooLarge: over the per-file cap (listed, not readable).
	// Unsafe: absolute, escaping or .git paths, or paths under a link.
	Ignored  int `json:"ignored,omitempty"`
	Links    int `json:"links,omitempty"`
	TooLarge int `json:"too_large,omitempty"`
	Unsafe   int `json:"unsafe,omitempty"`
}

type scanFile struct {
	Path    string `json:"path"`
	Size    int64  `json:"size"`
	Binary  bool   `json:"binary,omitempty"`
	Omitted string `json:"omitted,omitempty"`
}

type repositoryCheckout struct {
	ready      chan struct{}
	err        error
	key        string
	dir        string
	repository string
	ref        string
	commit     string
	files      []scanFile
	index      map[string]int
	omitted    scanOmitted
	// truncated: the file cap was reached and later files are missing.
	truncated bool
	bytes     int64
	expires   time.Time
	users     int
}

// NewRepositoryCheckouts keeps checkouts under the system temporary directory.
func NewRepositoryCheckouts(source repositorySnapshots) *RepositoryCheckouts {
	return newRepositoryCheckouts(source, os.TempDir(), defaultScanLimits)
}

func newRepositoryCheckouts(source repositorySnapshots, parent string, limits scanLimits) *RepositoryCheckouts {
	return &RepositoryCheckouts{source: source, limits: limits, parent: parent, now: time.Now, items: map[string]*repositoryCheckout{}}
}

// Run deletes expired checkouts until ctx ends, then deletes all of them. It
// first removes roots that crashed processes left behind.
func (c *RepositoryCheckouts) Run(ctx context.Context) {
	removeStaleScanRoots(c.parent, c.limits.ttl, c.now())
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			c.Close()
			return
		case <-ticker.C:
			c.Sweep()
		}
	}
}

// removeStaleScanRoots deletes scan roots not touched for longer than ttl; a
// live process's root is younger, since every checkout refreshes it.
func removeStaleScanRoots(parent string, ttl time.Duration, now time.Time) {
	entries, err := os.ReadDir(parent)
	if err != nil {
		return
	}
	for _, entry := range entries {
		if !entry.IsDir() || !strings.HasPrefix(entry.Name(), scanRootPrefix) {
			continue
		}
		info, err := entry.Info()
		if err != nil || now.Sub(info.ModTime()) <= ttl {
			continue
		}
		_ = os.RemoveAll(filepath.Join(parent, entry.Name()))
	}
}

// Close deletes every checkout and refuses new ones.
func (c *RepositoryCheckouts) Close() {
	c.mu.Lock()
	c.closed = true
	root := c.root
	c.items = map[string]*repositoryCheckout{}
	c.mu.Unlock()
	if root != "" {
		_ = os.RemoveAll(root)
	}
}

// Sweep deletes expired checkouts nobody is reading.
func (c *RepositoryCheckouts) Sweep() {
	now := c.now()
	var remove []string
	c.mu.Lock()
	for key, item := range c.items {
		if item.users == 0 && isClosed(item.ready) && !now.Before(item.expires) {
			delete(c.items, key)
			remove = append(remove, item.dir)
		}
	}
	c.mu.Unlock()
	for _, dir := range remove {
		if dir != "" {
			_ = os.RemoveAll(dir)
		}
	}
}

func isClosed(ch chan struct{}) bool {
	select {
	case <-ch:
		return true
	default:
		return false
	}
}

// Acquire returns the checkout of a ref of a workspace repository,
// downloading it once for concurrent callers. release must be called when
// reading is done; until then the checkout is not deleted.
func (c *RepositoryCheckouts) Acquire(ctx context.Context, ws uuid.UUID, repository, ref string) (*repositoryCheckout, func(), error) {
	key := ws.String() + "\x00" + strings.ToLower(repository) + "\x00" + ref
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return nil, nil, toolFailure("repository scans are shutting down; try again in a moment")
	}
	item, ok := c.items[key]
	if ok && isClosed(item.ready) && !c.now().Before(item.expires) && item.users == 0 {
		delete(c.items, key)
		go os.RemoveAll(item.dir)
		ok = false
	}
	owner := !ok
	if owner {
		item = &repositoryCheckout{ready: make(chan struct{}), key: key, repository: repository, ref: ref}
		c.items[key] = item
	}
	item.users++
	c.mu.Unlock()
	release := func() {
		c.mu.Lock()
		item.users--
		c.mu.Unlock()
	}
	if owner {
		err := c.materialise(ctx, ws, item)
		c.mu.Lock()
		item.err = err
		item.expires = c.now().Add(c.limits.ttl)
		if err != nil {
			// A failure is not cached: the next call tries again.
			if c.items[key] == item {
				delete(c.items, key)
			}
		}
		c.mu.Unlock()
		close(item.ready)
		if err == nil {
			c.evict()
		}
	} else {
		select {
		case <-item.ready:
		case <-ctx.Done():
			release()
			return nil, nil, toolFailure("the repository checkout did not finish in time")
		}
	}
	if item.err != nil {
		release()
		return nil, nil, item.err
	}
	return item, release, nil
}

// evict drops the oldest idle checkouts beyond the cap.
func (c *RepositoryCheckouts) evict() {
	c.mu.Lock()
	var idle []*repositoryCheckout
	for _, item := range c.items {
		if item.users == 0 && isClosed(item.ready) {
			idle = append(idle, item)
		}
	}
	excess := len(c.items) - c.limits.checkouts
	sort.Slice(idle, func(i, j int) bool { return idle[i].expires.Before(idle[j].expires) })
	var remove []string
	for i := 0; i < excess && i < len(idle); i++ {
		delete(c.items, idle[i].key)
		remove = append(remove, idle[i].dir)
	}
	c.mu.Unlock()
	for _, dir := range remove {
		_ = os.RemoveAll(dir)
	}
}

func (c *RepositoryCheckouts) ensureRoot() (string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.root == "" {
		root, err := os.MkdirTemp(c.parent, scanRootPrefix)
		if err != nil {
			return "", err
		}
		c.root = root
	}
	now := c.now()
	_ = os.Chtimes(c.root, now, now)
	return c.root, nil
}

var errScanTooLarge = errors.New("repository too large")

func (c *RepositoryCheckouts) materialise(parent context.Context, ws uuid.UUID, item *repositoryCheckout) error {
	ctx, cancel := context.WithTimeout(parent, scanDownloadTimeout)
	defer cancel()
	root, err := c.ensureRoot()
	if err != nil {
		return err
	}
	snapshot, err := c.source.RepositorySnapshot(ctx, ws, item.repository, item.ref)
	if err != nil {
		return err
	}
	defer snapshot.Archive.Close()
	dir, err := os.MkdirTemp(root, "checkout-")
	if err != nil {
		return err
	}
	item.dir, item.commit = dir, snapshot.Commit
	if err = c.extract(ctx, item, snapshot.Archive); err != nil {
		_ = os.RemoveAll(dir)
		item.dir = ""
		if errors.Is(err, errScanTooLarge) {
			return toolFailure("%s at %s is larger than the scan limit (%d MB archive, %d MB of files); scan a smaller repository or read single files with the pull request tools", item.repository, truncateRunes(item.ref, 60), c.limits.archiveBytes>>20, c.limits.totalBytes>>20)
		}
		if ctx.Err() != nil {
			return toolFailure("downloading %s took too long; try again or scan a smaller repository", item.repository)
		}
		log.WithError(err).WithField("repository", item.repository).Warn("Repository scan checkout failed")
		return toolFailure("the archive of %s could not be read", item.repository)
	}
	log.WithFields(log.Fields{"workspace_id": ws, "repository": item.repository, "commit": item.commit, "files": len(item.files), "bytes": item.bytes}).Info("Repository scan checkout created")
	return nil
}

// countingReader fails once more than limit bytes were read.
type countingReader struct {
	r     io.Reader
	n     int64
	limit int64
}

func (c *countingReader) Read(p []byte) (int, error) {
	n, err := c.r.Read(p)
	c.n += int64(n)
	if c.n > c.limit {
		return n, errScanTooLarge
	}
	return n, err
}

// safeArchivePath maps an archive entry to a checkout path, or "" when the
// entry must not be materialised. GitHub archives hold one top directory,
// which is dropped.
func safeArchivePath(name string) (string, bool) {
	if name == "" || strings.ContainsRune(name, 0) || strings.HasPrefix(name, "/") || strings.Contains(name, `\`) || filepath.IsAbs(name) {
		return "", false
	}
	_, rest, ok := strings.Cut(name, "/")
	if !ok {
		return "", true // the top directory itself
	}
	rest = strings.TrimSuffix(rest, "/")
	if rest == "" {
		return "", true
	}
	for _, segment := range strings.Split(rest, "/") {
		if segment == ".." || segment == ".git" {
			return "", false
		}
	}
	clean := path.Clean(rest)
	if clean == "." || clean == ".." || strings.HasPrefix(clean, "../") || strings.HasPrefix(clean, "/") {
		return "", false
	}
	return clean, true
}

func (c *RepositoryCheckouts) extract(ctx context.Context, item *repositoryCheckout, archive io.Reader) error {
	compressed := &countingReader{r: archive, limit: c.limits.archiveBytes}
	gz, err := gzip.NewReader(compressed)
	if err != nil {
		if errors.Is(err, errScanTooLarge) {
			return err
		}
		return fmt.Errorf("opening archive: %w", err)
	}
	defer gz.Close()
	dirRoot, err := os.OpenRoot(item.dir)
	if err != nil {
		return err
	}
	defer dirRoot.Close()
	reader := tar.NewReader(gz)
	var links []string
	ignoreFiles := map[string]string{}
	var files []scanFile
	underLink := func(name string) bool {
		for _, link := range links {
			if name == link || strings.HasPrefix(name, link+"/") {
				return true
			}
		}
		return false
	}
	for {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		header, err := reader.Next()
		// Buffered readers may decode past the cap before reporting it.
		if compressed.lastErr() != nil {
			return errScanTooLarge
		}
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			if errors.Is(err, errScanTooLarge) || errors.Is(compressed.lastErr(), errScanTooLarge) {
				return errScanTooLarge
			}
			return fmt.Errorf("reading archive: %w", err)
		}
		switch header.Typeflag {
		case tar.TypeXGlobalHeader, tar.TypeXHeader, tar.TypeGNULongName, tar.TypeGNULongLink:
			continue
		}
		name, ok := safeArchivePath(header.Name)
		if !ok {
			item.omitted.Unsafe++
			continue
		}
		if name == "" {
			continue
		}
		if underLink(name) {
			item.omitted.Unsafe++
			continue
		}
		switch header.Typeflag {
		case tar.TypeDir:
			continue // directories are created for the files inside them
		case tar.TypeSymlink, tar.TypeLink:
			links = append(links, name)
			item.omitted.Links++
			continue
		case tar.TypeReg:
		default:
			item.omitted.Unsafe++
			continue
		}
		if len(files) >= c.limits.files {
			item.truncated = true
			break
		}
		file := scanFile{Path: name, Size: header.Size}
		if header.Size > c.limits.fileBytes || header.Size < 0 {
			file.Omitted = "too_large"
			item.omitted.TooLarge++
			files = append(files, file)
			continue
		}
		if item.bytes+header.Size > c.limits.totalBytes {
			return errScanTooLarge
		}
		if dir := path.Dir(name); dir != "." {
			if err := dirRoot.MkdirAll(dir, 0o700); err != nil {
				item.omitted.Unsafe++
				continue
			}
		}
		out, err := dirRoot.OpenFile(name, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
		if err != nil {
			item.omitted.Unsafe++
			continue
		}
		var head bytes.Buffer
		written, err := io.Copy(io.MultiWriter(out, &prefixWriter{buf: &head, limit: binarySniffBytes}), io.LimitReader(reader, header.Size))
		closeErr := out.Close()
		if err != nil {
			if errors.Is(err, errScanTooLarge) {
				return err
			}
			return fmt.Errorf("extracting %s: %w", name, err)
		}
		if closeErr != nil {
			return closeErr
		}
		item.bytes += written
		file.Binary = bytes.IndexByte(head.Bytes(), 0) >= 0
		if path.Base(name) == ".gitignore" && !file.Binary && written <= 256<<10 {
			if data, err := dirRoot.ReadFile(name); err == nil {
				base := path.Dir(name)
				if base == "." {
					base = ""
				}
				ignoreFiles[base] = string(data)
			}
		}
		files = append(files, file)
	}
	if compressed.lastErr() != nil {
		return errScanTooLarge
	}
	ignore := newGitignore(ignoreFiles)
	kept := files[:0]
	for _, file := range files {
		if ignore.ignored(file.Path) {
			item.omitted.Ignored++
			if file.Omitted == "" {
				_ = dirRoot.Remove(file.Path)
			} else {
				item.omitted.TooLarge--
			}
			continue
		}
		kept = append(kept, file)
	}
	sort.Slice(kept, func(i, j int) bool { return kept[i].Path < kept[j].Path })
	item.files = kept
	item.index = make(map[string]int, len(kept))
	for i, file := range kept {
		item.index[file.Path] = i
	}
	return nil
}

func (c *countingReader) lastErr() error {
	if c.n > c.limit {
		return errScanTooLarge
	}
	return nil
}

// prefixWriter keeps the first limit bytes written to it.
type prefixWriter struct {
	buf   *bytes.Buffer
	limit int
}

func (w *prefixWriter) Write(p []byte) (int, error) {
	if room := w.limit - w.buf.Len(); room > 0 {
		if len(p) < room {
			room = len(p)
		}
		w.buf.Write(p[:room])
	}
	return len(p), nil
}

// paths lists the checkout's files.
func (co *repositoryCheckout) paths() []string {
	paths := make([]string, len(co.files))
	for i, file := range co.files {
		paths[i] = file.Path
	}
	return paths
}

// cleanScanPath normalises a path given by the model; "" means invalid.
func cleanScanPath(name string) string {
	name = strings.TrimSpace(name)
	if name == "" || strings.ContainsRune(name, 0) || strings.Contains(name, `\`) || strings.HasPrefix(name, "/") {
		return ""
	}
	for _, segment := range strings.Split(name, "/") {
		if segment == ".." || segment == ".git" {
			return ""
		}
	}
	clean := path.Clean(name)
	if clean == "." {
		return ""
	}
	return clean
}

// file returns a listed file, or nil.
func (co *repositoryCheckout) file(name string) *scanFile {
	clean := cleanScanPath(name)
	if clean == "" {
		return nil
	}
	i, ok := co.index[clean]
	if !ok {
		return nil
	}
	return &co.files[i]
}

// read returns a listed, readable file. The path must be in the checkout's
// index (so ignored, omitted and escaping paths are refused), and it is opened
// through os.Root, which never follows a link out of the checkout.
func (co *repositoryCheckout) read(name string) ([]byte, error) {
	file := co.file(name)
	if file == nil {
		return nil, toolFailure("%s is not a file of this checkout; list the files with repo_list_files or repo_glob", truncateRunes(name, 200))
	}
	if file.Omitted != "" {
		return nil, toolFailure("%s is larger than the scan's per-file limit", file.Path)
	}
	root, err := os.OpenRoot(co.dir)
	if err != nil {
		return nil, err
	}
	defer root.Close()
	info, err := root.Lstat(file.Path)
	if err != nil || !info.Mode().IsRegular() {
		return nil, toolFailure("%s cannot be read", file.Path)
	}
	handle, err := root.Open(file.Path)
	if err != nil {
		return nil, toolFailure("%s cannot be read", file.Path)
	}
	defer handle.Close()
	data, err := io.ReadAll(io.LimitReader(handle, file.Size+1))
	if err != nil {
		return nil, err
	}
	return data, nil
}
