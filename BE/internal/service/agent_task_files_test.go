package service

import (
	"archive/tar"
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/kuayle/kuayle-backend/internal/repository"
	gh "github.com/kuayle/kuayle-backend/pkg/github"
	"github.com/stretchr/testify/require"
)

// taskFileSourceFake answers what the repository would, and records the ref.
type taskFileSourceFake struct {
	source *domain.TaskFileSource
	err    error
	refs   []string
}

func (f *taskFileSourceFake) TaskFileSource(_ context.Context, _, _ uuid.UUID, ref string) (*domain.TaskFileSource, error) {
	f.refs = append(f.refs, ref)
	if f.err != nil {
		return nil, f.err
	}
	copied := *f.source
	return &copied, nil
}

// fileScans completes every scan at once with what answer returns for it.
type fileScans struct {
	t        *testing.T
	answer   func(domain.WorktreeScanRequest) domain.WorktreeScanResult
	failure  string
	err      error
	requests []domain.WorktreeScanRequest
	targets  []domain.WorktreeScanTarget
}

func (f *fileScans) CreateWorktreeScan(_ context.Context, _, _ uuid.UUID, target domain.WorktreeScanTarget, req domain.WorktreeScanRequest) (*domain.WorktreeScan, error) {
	if err := repository.ValidateWorktreeScanRequest(req); err != nil {
		return nil, err
	}
	f.requests, f.targets = append(f.requests, req), append(f.targets, target)
	if f.err != nil {
		return nil, f.err
	}
	if f.failure != "" {
		return &domain.WorktreeScan{ID: uuid.New(), Error: &f.failure, CompletedAt: ptrTime(time.Now())}, nil
	}
	return completedWorktreeScan(f.t, f.answer(req)), nil
}

func (f *fileScans) GetWorktreeScan(context.Context, uuid.UUID, uuid.UUID) (*domain.WorktreeScan, error) {
	return nil, errors.New("scans complete at once in these tests")
}

func taskFilesContext() context.Context {
	return domain.WithAccess(context.Background(), domain.Access{WorkspaceID: uuid.New(), UserID: uuid.New()})
}

func publishedTaskFiles(t *testing.T, limits scanLimits, entries ...tarEntry) (*taskFiles, *taskFileSourceFake, *snapshotFake) {
	t.Helper()
	snapshots := &snapshotFake{archive: tarball(t, entries...)}
	source := &taskFileSourceFake{source: &domain.TaskFileSource{Repository: "acme/app", Head: scanSHA, Changed: []string{"src/app.go"}}}
	git := newGitFake()
	git.commit(scanSHA, filesOf(entries))
	return &taskFiles{sources: source, checkouts: newTestCheckouts(t, snapshots, limits), trees: git}, source, snapshots
}

func names(entries []domain.TaskFileEntry) []string {
	var out []string
	for _, entry := range entries {
		out = append(out, entry.Kind+" "+entry.Path)
	}
	return out
}

// A published task is listed at its head one directory at a time from the
// commit's git trees, with the task's changed files marked (a directory is
// changed when a file below it is), and its file contents are read from the
// GitHub archive of that commit: only what the commit tracks, less what its
// .gitignore names. A path must be one of the commit's files, so traversal,
// an ignored .env, links and directories are not found; a binary file carries
// no bytes and a file over the readable size none either. Listing downloads
// no archive.
func TestPublishedTaskFilesAreTheHeadCommitsTrackedFiles(t *testing.T) {
	files, source, snapshots := publishedTaskFiles(t, testScanLimits(),
		tarEntry{name: "repo-sha/", kind: tar.TypeDir},
		tarEntry{name: "repo-sha/.gitignore", body: ".env\n"},
		tarEntry{name: "repo-sha/.env", body: "LOCAL_ONLY=1\n"},
		tarEntry{name: "repo-sha/src/app.go", body: "package app\n\nfunc App() {}\n"},
		tarEntry{name: "repo-sha/src/lib/util.go", body: "package lib\n"},
		tarEntry{name: "repo-sha/image.png", body: "PNG\x00\x01"},
		tarEntry{name: "repo-sha/big.txt", body: strings.Repeat("x", 70<<10)},
		tarEntry{name: "repo-sha/link", kind: tar.TypeSymlink, link: "/etc/passwd"},
		tarEntry{name: "repo-sha/../escape.txt", body: "escape"},
	)
	ctx := taskFilesContext()
	ws, task := uuid.New(), uuid.New()

	root, err := files.list(ctx, ws, task, TaskFilesQuery{})
	require.NoError(t, err)
	require.Equal(t, scanSHA, root.Commit)
	require.Equal(t, "", root.Path)
	require.NotEmpty(t, root.Tree)
	require.False(t, root.Truncated)
	require.Equal(t, []string{"tree src", "blob .gitignore", "blob big.txt", "blob image.png"}, names(root.Entries))
	require.True(t, root.Entries[0].Changed, "src holds a changed file")
	require.Equal(t, "src", root.Entries[0].Name)
	require.Equal(t, int64(70<<10), *root.Entries[2].Size)
	require.NotEmpty(t, root.Entries[1].SHA)
	require.Nil(t, root.Entries[0].Size)
	require.Equal(t, []string{""}, source.refs)

	nested, err := files.list(ctx, ws, task, TaskFilesQuery{Path: "src/"})
	require.NoError(t, err)
	require.Equal(t, "src", nested.Path, "a trailing slash names the same directory")
	require.Equal(t, []string{"tree src/lib", "blob src/app.go"}, names(nested.Entries))
	require.False(t, nested.Entries[0].Changed)
	require.True(t, nested.Entries[1].Changed)
	require.Equal(t, "app.go", nested.Entries[1].Name)
	deep, err := files.list(ctx, ws, task, TaskFilesQuery{Path: "src/lib"})
	require.NoError(t, err)
	require.Equal(t, []string{"blob src/lib/util.go"}, names(deep.Entries))
	require.Zero(t, snapshots.calls.Load(), "no archive is downloaded to list")

	page, err := files.content(ctx, ws, task, "src/app.go", scanSHA, 0, 0)
	require.NoError(t, err)
	require.Equal(t, domain.TaskFileContent{Path: "src/app.go", Commit: scanSHA, Content: "package app\n\nfunc App() {}", Start: 1, End: 3, TotalLines: 3}, *page)

	for _, name := range []string{".env", "../escape.txt", "./src/app.go", "src/../src/app.go", "/src/app.go", "src", "link", ".git/config", "missing.go"} {
		_, err = files.content(ctx, ws, task, name, "", 1, 0)
		require.ErrorIs(t, err, ErrTaskFileNotFound, name)
	}
	binary, err := files.content(ctx, ws, task, "image.png", "", 1, 0)
	require.NoError(t, err)
	require.True(t, binary.Binary)
	require.Empty(t, binary.Content)
	large, err := files.content(ctx, ws, task, "big.txt", "", 1, 0)
	require.NoError(t, err)
	require.True(t, large.TooLarge)
	require.Empty(t, large.Content)
}

// A directory that is missing, a file where a directory is named, and
// every path that is not a clean relative directory are refused: the last
// before the task is even read, none by listing anything.
func TestTaskFilesListingRefusesMissingAndUncleanDirectories(t *testing.T) {
	files, source, _ := publishedTaskFiles(t, testScanLimits(),
		tarEntry{name: "repo-sha/src/app.go", body: "package app\n"},
		tarEntry{name: "repo-sha/README.md", body: "# app\n"},
	)
	ctx := taskFilesContext()
	ws, task := uuid.New(), uuid.New()
	for _, name := range []string{"missing", "src/missing", "README.md", "src/app.go", "src/app.go/deeper", "Src"} {
		_, err := files.list(ctx, ws, task, TaskFilesQuery{Path: name})
		require.ErrorIs(t, err, ErrTaskFileNotFound, name)
	}
	reads := len(source.refs)
	for _, name := range []string{"..", "../..", "src/../..", "../etc", "src/../src", "./src", "/src", "//src", "src//lib", ".git", "src/.git", "src\\lib", "src/\x00", "a\nb"} {
		_, err := files.list(ctx, ws, task, TaskFilesQuery{Path: name})
		require.ErrorIs(t, err, ErrTaskFilePath, name)
	}
	require.Len(t, source.refs, reads, "an unclean path is refused before the task is read")
	_, err := files.list(ctx, ws, task, TaskFilesQuery{Path: "src", Query: "app"})
	require.ErrorIs(t, err, repository.ErrConversationInvalid, "a search covers the whole commit")
	_, err = files.list(ctx, ws, task, TaskFilesQuery{Query: strings.Repeat("a", domain.TaskFileQueryMax+1)})
	require.ErrorIs(t, err, repository.ErrConversationInvalid)
}

// The listing of a directory stops at the workspace's limit and says so; a
// file is read a page at a time, by line range.
func TestPublishedTaskFilesTruncateTheDirectoryAndPageTheContent(t *testing.T) {
	var lines []string
	for i := 1; i <= domain.TaskFileLinesMax+5; i++ {
		lines = append(lines, "line "+strconv.Itoa(i))
	}
	files, source, _ := publishedTaskFiles(t, testScanLimits(),
		tarEntry{name: "repo-sha/a.txt", body: strings.Join(lines, "\n") + "\n"},
		tarEntry{name: "repo-sha/b.txt", body: "b\n"},
		tarEntry{name: "repo-sha/c.txt", body: "c\n"},
	)
	source.source.FilesMax = 2
	ctx := taskFilesContext()
	listed, err := files.list(ctx, uuid.New(), uuid.New(), TaskFilesQuery{})
	require.NoError(t, err)
	require.True(t, listed.Truncated)
	require.Equal(t, []string{"blob a.txt", "blob b.txt"}, names(listed.Entries))

	first, err := files.content(ctx, uuid.New(), uuid.New(), "a.txt", "", 1, 0)
	require.NoError(t, err)
	require.Equal(t, 1, first.Start)
	require.Equal(t, domain.TaskFileLinesMax, first.End)
	require.Equal(t, domain.TaskFileLinesMax+5, first.TotalLines)
	require.True(t, first.Truncated)
	next, err := files.content(ctx, uuid.New(), uuid.New(), "a.txt", "", first.End+1, 0)
	require.NoError(t, err)
	require.Equal(t, "line 5001\nline 5002\nline 5003\nline 5004\nline 5005", next.Content)
	require.False(t, next.Truncated)
	ranged, err := files.content(ctx, uuid.New(), uuid.New(), "a.txt", "", 2, 3)
	require.NoError(t, err)
	require.Equal(t, "line 2\nline 3", ranged.Content)
	for _, bad := range [][2]int{{-1, 0}, {5, 4}} {
		_, err = files.content(ctx, uuid.New(), uuid.New(), "a.txt", "", bad[0], bad[1])
		require.ErrorIs(t, err, repository.ErrConversationInvalid, bad)
	}
}

// Only the head or a commit of the task's attempts is read: a published
// commit from GitHub, an attempt's own commit from the machine, anything
// else is refused before anything is read or queued.
func TestTaskFilesReadOnlyTheHeadOrACommitOfTheTask(t *testing.T) {
	files, source, snapshots := publishedTaskFiles(t, testScanLimits(), tarEntry{name: "repo-sha/a.txt", body: "a\n"})
	git := files.trees.(*gitFake)
	scans := &fileScans{t: t, answer: func(req domain.WorktreeScanRequest) domain.WorktreeScanResult {
		return domain.WorktreeScanResult{Commit: req.Ref, Tree: strings.Repeat("7", 40), Entries: []domain.TreeEntry{{Name: "a.txt", Kind: domain.TreeEntryBlob, SHA: strings.Repeat("8", 40)}}}
	}}
	files.scans = scans
	ctx := taskFilesContext()
	ws, task := uuid.New(), uuid.New()
	for _, ref := range []string{"main", "HEAD", "abc1234", strings.Repeat("A", 40), scanSHA + " "} {
		_, err := files.list(ctx, ws, task, TaskFilesQuery{Ref: ref})
		require.ErrorIs(t, err, ErrTaskFileRef, ref)
	}
	require.Empty(t, source.refs, "a malformed ref is refused before the task is read")

	other := strings.Repeat("e", 40)
	_, err := files.list(ctx, ws, task, TaskFilesQuery{Ref: other})
	require.ErrorIs(t, err, ErrTaskFileRef, "neither the head nor a commit of the task")
	require.Empty(t, scans.requests)
	require.Zero(t, git.reads())

	source.source.RefRecorded = true
	listed, err := files.list(ctx, ws, task, TaskFilesQuery{Ref: other})
	require.NoError(t, err)
	require.Equal(t, other, listed.Commit, "an attempt's own commit is read on the machine")
	require.Equal(t, []domain.WorktreeScanRequest{{Kind: domain.WorktreeScanTree, Ref: other, Limit: domain.TaskFilesMax}}, scans.requests)
	require.Equal(t, task, *scans.targets[0].TaskID)
	require.Zero(t, git.reads())

	// A commit Kuayle published before GitHub's head moved is read there.
	source.source.Head, source.source.RefPublished = strings.Repeat("9", 40), true
	listed, err = files.list(ctx, ws, task, TaskFilesQuery{Ref: scanSHA})
	require.NoError(t, err)
	require.Equal(t, scanSHA, listed.Commit)
	require.Equal(t, []string{"blob a.txt"}, names(listed.Entries))
	require.Positive(t, git.reads())
	require.Zero(t, snapshots.calls.Load())

	// The task's access rules come first: a task the caller may not read
	// is not found, and nothing is read for it.
	reads := git.reads()
	source.err = repository.ErrConversationNotFound
	_, err = files.list(ctx, ws, task, TaskFilesQuery{})
	require.ErrorIs(t, err, repository.ErrConversationNotFound)
	_, err = files.content(ctx, ws, task, "a.txt", "", 1, 0)
	require.ErrorIs(t, err, repository.ErrConversationNotFound)
	require.Len(t, scans.requests, 1)
	require.Equal(t, reads, git.reads())
	require.Zero(t, snapshots.calls.Load())
}

// Before anything is published, the task branch's commit is read on the
// task's machine with the tree and show scans, one directory per scan; an
// unrecorded ref is read only while it is that commit. The machine's answers
// become the same results.
func TestUnpublishedTaskFilesAreReadOnTheMachine(t *testing.T) {
	head, tree := strings.Repeat("c", 40), strings.Repeat("d", 40)
	size := int64(12)
	scans := &fileScans{t: t}
	source := &taskFileSourceFake{source: &domain.TaskFileSource{Repository: "acme/app", Changed: []string{"src/app.go"}}}
	files := &taskFiles{sources: source, scans: scans}
	ctx := taskFilesContext()
	ws, task := uuid.New(), uuid.New()

	scans.answer = func(req domain.WorktreeScanRequest) domain.WorktreeScanResult {
		if req.Kind == domain.WorktreeScanTree {
			switch {
			case req.Pattern != "":
				return domain.WorktreeScanResult{Commit: head, Files: []string{"src/app.go"}, FileMatches: 3, Truncated: true}
			case req.Path == "":
				return domain.WorktreeScanResult{Commit: head, Tree: tree, Truncated: true, Entries: []domain.TreeEntry{
					{Name: "src", Kind: domain.TreeEntryTree, SHA: strings.Repeat("1", 40)},
					{Name: "README.md", Kind: domain.TreeEntryBlob, SHA: strings.Repeat("2", 40), Size: &size}}}
			case req.Path == "src":
				return domain.WorktreeScanResult{Commit: head, Tree: strings.Repeat("1", 40), Entries: []domain.TreeEntry{{Name: "app.go", Kind: domain.TreeEntryBlob, SHA: strings.Repeat("3", 40)}}}
			}
			return domain.WorktreeScanResult{Commit: head, Missing: true}
		}
		switch req.Path {
		case "src/app.go":
			return domain.WorktreeScanResult{Commit: head, Content: "package app", StartLine: req.StartLine, EndLine: 1, TotalLines: 1}
		case "logo.png":
			return domain.WorktreeScanResult{Commit: head, Binary: true}
		case "huge.sql":
			return domain.WorktreeScanResult{Commit: head, TooLarge: true}
		}
		return domain.WorktreeScanResult{Commit: head, Missing: true}
	}
	listed, err := files.list(ctx, ws, task, TaskFilesQuery{})
	require.NoError(t, err)
	require.Equal(t, head, listed.Commit)
	require.Equal(t, tree, listed.Tree)
	require.True(t, listed.Truncated)
	require.Equal(t, []string{"tree src", "blob README.md"}, names(listed.Entries))
	require.True(t, listed.Entries[0].Changed)
	require.Equal(t, domain.WorktreeScanRequest{Kind: domain.WorktreeScanTree, Ref: "HEAD", Limit: domain.TaskFilesMax}, scans.requests[0])

	nested, err := files.list(ctx, ws, task, TaskFilesQuery{Path: "src/"})
	require.NoError(t, err)
	require.Equal(t, []string{"blob src/app.go"}, names(nested.Entries))
	require.True(t, nested.Entries[0].Changed)
	require.Equal(t, domain.WorktreeScanRequest{Kind: domain.WorktreeScanTree, Ref: "HEAD", Path: "src", Limit: domain.TaskFilesMax}, scans.requests[1])
	_, err = files.list(ctx, ws, task, TaskFilesQuery{Path: "nope"})
	require.ErrorIs(t, err, ErrTaskFileNotFound, "the commit has no such directory")
	requests := len(scans.requests)
	_, err = files.list(ctx, ws, task, TaskFilesQuery{Path: "../.."})
	require.ErrorIs(t, err, ErrTaskFilePath)
	require.Len(t, scans.requests, requests, "a traversal is refused before the machine is asked")

	found, err := files.list(ctx, ws, task, TaskFilesQuery{Query: " APP "})
	require.NoError(t, err)
	require.Equal(t, []string{"blob src/app.go"}, names(found.Entries))
	require.Equal(t, "app.go", found.Entries[0].Name)
	require.Equal(t, 3, found.Matches)
	require.True(t, found.Truncated)
	require.Equal(t, domain.WorktreeScanRequest{Kind: domain.WorktreeScanTree, Ref: "HEAD", Pattern: "APP", Limit: domain.TaskFileSearchMax}, scans.requests[len(scans.requests)-1])

	page, err := files.content(ctx, ws, task, "src/app.go", head, 0, 0)
	require.NoError(t, err)
	require.Equal(t, domain.TaskFileContent{Path: "src/app.go", Commit: head, Content: "package app", Start: 1, End: 1, TotalLines: 1}, *page)
	require.Equal(t, domain.WorktreeScanRequest{Kind: domain.WorktreeScanShow, Ref: "HEAD", Path: "src/app.go", StartLine: 1, EndLine: domain.TaskFileLinesMax}, scans.requests[len(scans.requests)-1])
	binary, err := files.content(ctx, ws, task, "logo.png", "", 1, 0)
	require.NoError(t, err)
	require.True(t, binary.Binary)
	large, err := files.content(ctx, ws, task, "huge.sql", "", 1, 0)
	require.NoError(t, err)
	require.True(t, large.TooLarge)
	_, err = files.content(ctx, ws, task, ".env", "", 1, 0)
	require.ErrorIs(t, err, ErrTaskFileNotFound, "the machine reads the commit, which does not track it")
	requests = len(scans.requests)
	_, err = files.content(ctx, ws, task, "../.env", "", 1, 0)
	require.ErrorIs(t, err, ErrTaskFileNotFound)
	require.Len(t, scans.requests, requests, "an invalid path is refused before the machine is asked")

	// The branch moved on: the commit asked for is no longer the head.
	_, err = files.list(ctx, ws, task, TaskFilesQuery{Ref: strings.Repeat("d", 40)})
	require.ErrorIs(t, err, ErrTaskFileRef)

	for _, tc := range []struct {
		err     error
		failure string
		want    error
	}{
		{err: repository.ErrWorktreeUnavailable, want: ErrTaskFilesUnavailable},
		{err: repository.ErrWorktreeScanBusy, want: ErrTaskFilesBusy},
		{err: repository.ErrConversationNotFound, want: repository.ErrConversationNotFound},
		{failure: "The scan could not finish", want: ErrTaskFilesFailed},
	} {
		scans.err, scans.failure = tc.err, tc.failure
		_, err = files.list(ctx, ws, task, TaskFilesQuery{})
		require.ErrorIs(t, err, tc.want)
	}
	files.scans = nil
	_, err = files.list(ctx, ws, task, TaskFilesQuery{})
	require.ErrorIs(t, err, ErrTaskFilesUnavailable, "no machine scans on this server")
}

// A failed download is logged by its kind, never by its text: an archive
// URL can carry GitHub's download token.
func TestTaskFilesCheckoutFailureNeverLogsTheErrorText(t *testing.T) {
	leaked := errors.New(`downloading repository archive: Get "https://codeload.github.com/acme/app/legacy.tar.gz/x?sig=placeholder": EOF`)
	require.Equal(t, "the archive could not be downloaded", checkoutFailure(leaked))
	require.Equal(t, "GitHub answered 404", checkoutFailure(&gh.APIError{Status: 404, Message: "Not Found"}))
	require.Equal(t, "the repository is too large", checkoutFailure(toolFailure("the repository is too large")))
	require.NotContains(t, checkoutFailure(context.DeadlineExceeded), "codeload")
}

// A directory listing holds as many entries as the workspace allows (its Dev
// Machine policy, 30,000 by default): GitHub's listing of a directory is cut
// there, and the machine is asked for that many, beyond what a scan result
// held before. A search is capped by its own, smaller bound and counts every match.
func TestTaskFilesListAsManyEntriesAsTheWorkspaceAllows(t *testing.T) {
	files, source, _ := publishedTaskFiles(t, testScanLimits(),
		tarEntry{name: "repo-sha/a.txt", body: "a\n"}, tarEntry{name: "repo-sha/b.txt", body: "b\n"},
		tarEntry{name: "repo-sha/c.txt", body: "c\n"}, tarEntry{name: "repo-sha/d.txt", body: "d\n"},
	)
	ctx := taskFilesContext()
	source.source.FilesMax = 3
	listed, err := files.list(ctx, uuid.New(), uuid.New(), TaskFilesQuery{})
	require.NoError(t, err)
	require.Equal(t, []string{"blob a.txt", "blob b.txt", "blob c.txt"}, names(listed.Entries))
	require.True(t, listed.Truncated)
	source.source.FilesMax = 4
	listed, err = files.list(ctx, uuid.New(), uuid.New(), TaskFilesQuery{})
	require.NoError(t, err)
	require.Len(t, listed.Entries, 4)
	require.False(t, listed.Truncated)

	many := make([]domain.TreeEntry, 2500)
	for i := range many {
		many[i] = domain.TreeEntry{Name: fmt.Sprintf("file-%04d.go", i), Kind: domain.TreeEntryBlob, SHA: strings.Repeat("a", 40)}
	}
	scans := &fileScans{t: t, answer: func(req domain.WorktreeScanRequest) domain.WorktreeScanResult {
		return domain.WorktreeScanResult{Commit: strings.Repeat("c", 40), Entries: many[:min(len(many), req.Limit)], Truncated: req.Limit < len(many)}
	}}
	machine := &taskFiles{sources: &taskFileSourceFake{source: &domain.TaskFileSource{Repository: "acme/app", FilesMax: 2500}}, scans: scans}
	listed, err = machine.list(ctx, uuid.New(), uuid.New(), TaskFilesQuery{Path: "src/a-typical-directory"})
	require.NoError(t, err)
	require.Equal(t, 2500, scans.requests[0].Limit)
	require.Len(t, listed.Entries, 2500, "more entries than a 64 KiB scan result held")
	require.False(t, listed.Truncated)
	machine.sources.(*taskFileSourceFake).source.FilesMax = 1000
	listed, err = machine.list(ctx, uuid.New(), uuid.New(), TaskFilesQuery{})
	require.NoError(t, err)
	require.Equal(t, 1000, scans.requests[1].Limit)
	require.Len(t, listed.Entries, 1000)
	require.True(t, listed.Truncated)

	// GitHub cutting its own listing is a truncated listing too.
	files.trees.(*gitFake).truncate = true
	files.dirs = newTreeCache[*cachedTree](defaultTreeCacheLimits)
	listed, err = files.list(ctx, uuid.New(), uuid.New(), TaskFilesQuery{})
	require.NoError(t, err)
	require.True(t, listed.Truncated)
}

// A search answers the files whose path contains the text, whatever its
// case, up to its bound, and counts them all; it reads the commit's tree
// once and answers the next refinement from the cache.
func TestPublishedTaskFilesSearchPathsWithABound(t *testing.T) {
	tracked := map[string]string{"src/Search.ts": "s", "src/lib/search_test.ts": "t", "README.md": "r", "docs/link@link": "x", "vendor/mod@module": "m"}
	for i := range domain.TaskFileSearchMax + 20 {
		tracked[fmt.Sprintf("gen/item-%04d.txt", i)] = strconv.Itoa(i)
	}
	git := newGitFake()
	root := git.commit(scanSHA, tracked)
	files := &taskFiles{sources: &taskFileSourceFake{source: &domain.TaskFileSource{Repository: "acme/app", Head: scanSHA, Changed: []string{"src/Search.ts"}}}, trees: git}
	ctx := taskFilesContext()
	ws, task := uuid.New(), uuid.New()

	found, err := files.list(ctx, ws, task, TaskFilesQuery{Query: "SEARCH"})
	require.NoError(t, err)
	require.Equal(t, []string{"blob src/Search.ts", "blob src/lib/search_test.ts"}, names(found.Entries))
	require.Equal(t, "Search.ts", found.Entries[0].Name)
	require.True(t, found.Entries[0].Changed)
	require.Equal(t, 2, found.Matches)
	require.False(t, found.Truncated)
	require.Equal(t, []string{root + " recursive=true"}, git.treeReqs)

	capped, err := files.list(ctx, ws, task, TaskFilesQuery{Query: "item-"})
	require.NoError(t, err)
	require.Len(t, capped.Entries, domain.TaskFileSearchMax)
	require.Equal(t, domain.TaskFileSearchMax+20, capped.Matches)
	require.True(t, capped.Truncated)
	none, err := files.list(ctx, ws, task, TaskFilesQuery{Query: "link"})
	require.NoError(t, err)
	require.Empty(t, none.Entries, "a symlink and a submodule are not files")
	require.Len(t, git.treeReqs, 1, "refined searches are answered from the cached tree")
}

// Directories are cached by git tree SHA, which names their content, so a
// directory that did not change between two commits is read once: the second
// commit's listing reads only the trees whose SHA is new. The cache is the
// workspace's and repository's own, and a failed read caches nothing.
func TestTaskFilesDirectoriesAreCachedByTreeSHAAcrossCommits(t *testing.T) {
	second := strings.Repeat("2", 40)
	git := newGitFake()
	shared := map[string]string{"lib/a.go": "package lib\n", "lib/b.go": "package lib\n"}
	firstFiles := map[string]string{"main.go": "v1", "README.md": "r"}
	secondFiles := map[string]string{"main.go": "v2", "README.md": "r"}
	for name, content := range shared {
		firstFiles[name], secondFiles[name] = content, content
	}
	rootA := git.commit(scanSHA, firstFiles)
	rootB := git.commit(second, secondFiles)
	require.NotEqual(t, rootA, rootB)
	source := &taskFileSourceFake{source: &domain.TaskFileSource{Repository: "acme/app", Head: scanSHA}}
	files := &taskFiles{sources: source, trees: git}
	ctx := taskFilesContext()
	ws, task := uuid.New(), uuid.New()

	listed, err := files.list(ctx, ws, task, TaskFilesQuery{Path: "lib"})
	require.NoError(t, err)
	require.Equal(t, []string{"blob lib/a.go", "blob lib/b.go"}, names(listed.Entries))
	libSHA := listed.Tree
	require.Equal(t, []string{rootA + " recursive=false", libSHA + " recursive=false"}, git.treeReqs, "the root, then the directory")

	// Listing again reads nothing: the commit and its trees are cached.
	git.reset()
	_, err = files.list(ctx, ws, task, TaskFilesQuery{Path: "lib"})
	require.NoError(t, err)
	require.Empty(t, git.treeReqs)
	require.Empty(t, git.commitReqs)

	// A new head with the same lib directory: only its new root is read.
	source.source.Head = second
	listed, err = files.list(ctx, ws, task, TaskFilesQuery{Path: "lib"})
	require.NoError(t, err)
	require.Equal(t, second, listed.Commit)
	require.Equal(t, libSHA, listed.Tree, "an unchanged directory keeps its tree SHA")
	require.Equal(t, []string{second}, git.commitReqs)
	require.Equal(t, []string{rootB + " recursive=false"}, git.treeReqs, "lib is reused; only the new root is read")

	// Another task of the same repository shares what was read.
	git.reset()
	_, err = files.list(ctx, ws, uuid.New(), TaskFilesQuery{Path: "lib"})
	require.NoError(t, err)
	require.Empty(t, git.treeReqs)
	// Another workspace, or another repository, never reads this one's cache.
	_, err = files.list(ctx, uuid.New(), task, TaskFilesQuery{Path: "lib"})
	require.NoError(t, err)
	require.Len(t, git.treeReqs, 2)
	git.reset()
	source.source.Repository = "acme/other"
	_, err = files.list(ctx, ws, task, TaskFilesQuery{Path: "lib"})
	require.NoError(t, err)
	require.Len(t, git.treeReqs, 2)

	// A failed read answers a failure and caches nothing.
	fresh := &taskFiles{sources: source, trees: git}
	git.reset()
	git.err = errGitHubDown
	_, err = fresh.list(ctx, ws, task, TaskFilesQuery{})
	require.ErrorIs(t, err, ErrTaskFilesFailed)
	git.err = nil
	listed, err = fresh.list(ctx, ws, task, TaskFilesQuery{})
	require.NoError(t, err)
	require.Len(t, listed.Entries, 3)
	_, err = (&taskFiles{sources: source}).list(ctx, ws, task, TaskFilesQuery{})
	require.ErrorIs(t, err, ErrTaskFilesUnavailable, "no GitHub trees on this server")
}

// The cache holds what it is allowed and no more: it drops the least
// recently used entries beyond its item count and its total weight, and an
// entry nobody read for its TTL; reading an entry keeps it.
func TestTreeCacheIsBoundedByCountWeightAndIdleTime(t *testing.T) {
	clock := time.Date(2026, 11, 25, 10, 0, 0, 0, time.UTC)
	cache := newTreeCache[string](treeCacheLimits{items: 3, weight: 10, ttl: time.Hour})
	cache.now = func() time.Time { return clock }

	cache.put("a", "A", 1)
	cache.put("b", "B", 1)
	cache.put("c", "C", 1)
	_, ok := cache.get("a") // a is now the most recently used
	require.True(t, ok)
	cache.put("d", "D", 1)
	_, ok = cache.get("b")
	require.False(t, ok, "the least recently used entry made room")
	for _, key := range []string{"a", "c", "d"} {
		_, ok = cache.get(key)
		require.True(t, ok, key)
	}

	cache.put("heavy", "H", 8)
	items, weight := cache.size()
	require.LessOrEqual(t, weight, 10)
	require.Equal(t, 10, weight, "the heavy entry pushed out the oldest one until the weight fit")
	require.Equal(t, 3, items)
	_, ok = cache.get("a")
	require.False(t, ok)
	cache.put("huge", "X", 11)
	_, ok = cache.get("huge")
	require.False(t, ok, "an entry heavier than the whole cache is not kept")

	clock = clock.Add(59 * time.Minute)
	_, ok = cache.get("heavy") // read: stays for another hour
	require.True(t, ok)
	clock = clock.Add(2 * time.Minute)
	_, ok = cache.get("heavy")
	require.True(t, ok, "reading renewed it")
	items, weight = cache.size()
	require.Equal(t, 1, items, "the entry that was not read for an hour expired")
	require.Equal(t, 8, weight)
	clock = clock.Add(time.Hour)
	_, ok = cache.get("heavy")
	require.False(t, ok)
	items, weight = cache.size()
	require.Zero(t, items)
	require.Zero(t, weight)
}
