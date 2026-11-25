package service

import (
	"archive/tar"
	"context"
	"errors"
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
	return &taskFiles{sources: source, checkouts: newTestCheckouts(t, snapshots, limits)}, source, snapshots
}

// A published task is read at its head from the GitHub archive of that
// commit: only what the commit tracks, less what its .gitignore names, with
// the task's changed files marked. A path must be one of the listing's, so
// traversal, an ignored .env, links and directories are not found; a binary
// file carries no bytes and a file over the readable size none either.
func TestPublishedTaskFilesAreTheHeadCommitsTrackedFiles(t *testing.T) {
	files, source, _ := publishedTaskFiles(t, testScanLimits(),
		tarEntry{name: "repo-sha/", kind: tar.TypeDir},
		tarEntry{name: "repo-sha/.gitignore", body: ".env\n"},
		tarEntry{name: "repo-sha/.env", body: "LOCAL_ONLY=1\n"},
		tarEntry{name: "repo-sha/src/app.go", body: "package app\n\nfunc App() {}\n"},
		tarEntry{name: "repo-sha/image.png", body: "PNG\x00\x01"},
		tarEntry{name: "repo-sha/big.txt", body: strings.Repeat("x", 70<<10)},
		tarEntry{name: "repo-sha/link", kind: tar.TypeSymlink, link: "/etc/passwd"},
		tarEntry{name: "repo-sha/../escape.txt", body: "escape"},
	)
	ctx := taskFilesContext()
	ws, task := uuid.New(), uuid.New()

	listed, err := files.list(ctx, ws, task, "")
	require.NoError(t, err)
	require.Equal(t, scanSHA, listed.Commit)
	require.False(t, listed.Truncated)
	require.Equal(t, []domain.TaskFile{{Path: ".gitignore"}, {Path: "big.txt"}, {Path: "image.png"}, {Path: "src/app.go", Changed: true}}, listed.Files)
	require.Equal(t, []string{""}, source.refs)

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

// The listing stops at its cap and says so; a file is read a page at a
// time, by line range.
func TestPublishedTaskFilesTruncateTheListingAndPageTheContent(t *testing.T) {
	limits := testScanLimits()
	limits.files = 2
	var lines []string
	for i := 1; i <= domain.TaskFileLinesMax+5; i++ {
		lines = append(lines, "line "+strconv.Itoa(i))
	}
	files, _, _ := publishedTaskFiles(t, limits,
		tarEntry{name: "repo-sha/a.txt", body: strings.Join(lines, "\n") + "\n"},
		tarEntry{name: "repo-sha/b.txt", body: "b\n"},
		tarEntry{name: "repo-sha/c.txt", body: "c\n"},
	)
	ctx := taskFilesContext()
	listed, err := files.list(ctx, uuid.New(), uuid.New(), "")
	require.NoError(t, err)
	require.True(t, listed.Truncated)
	require.Len(t, listed.Files, 2)

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
// else is refused before anything is downloaded or queued.
func TestTaskFilesReadOnlyTheHeadOrACommitOfTheTask(t *testing.T) {
	files, source, snapshots := publishedTaskFiles(t, testScanLimits(), tarEntry{name: "repo-sha/a.txt", body: "a\n"})
	scans := &fileScans{t: t, answer: func(req domain.WorktreeScanRequest) domain.WorktreeScanResult {
		return domain.WorktreeScanResult{Commit: req.Ref, Files: []string{"a.txt"}}
	}}
	files.scans = scans
	ctx := taskFilesContext()
	ws, task := uuid.New(), uuid.New()
	for _, ref := range []string{"main", "HEAD", "abc1234", strings.Repeat("A", 40), scanSHA + " "} {
		_, err := files.list(ctx, ws, task, ref)
		require.ErrorIs(t, err, ErrTaskFileRef, ref)
	}
	require.Empty(t, source.refs, "a malformed ref is refused before the task is read")

	other := strings.Repeat("e", 40)
	_, err := files.list(ctx, ws, task, other)
	require.ErrorIs(t, err, ErrTaskFileRef, "neither the head nor a commit of the task")
	require.Empty(t, scans.requests)
	require.Zero(t, snapshots.calls.Load())

	source.source.RefRecorded = true
	listed, err := files.list(ctx, ws, task, other)
	require.NoError(t, err)
	require.Equal(t, other, listed.Commit, "an attempt's own commit is read on the machine")
	require.Equal(t, []domain.WorktreeScanRequest{{Kind: domain.WorktreeScanTree, Ref: other}}, scans.requests)
	require.Equal(t, task, *scans.targets[0].TaskID)
	require.Zero(t, snapshots.calls.Load())

	// A commit Kuayle published before GitHub's head moved is read there.
	source.source.Head, source.source.RefPublished = strings.Repeat("9", 40), true
	listed, err = files.list(ctx, ws, task, scanSHA)
	require.NoError(t, err)
	require.Equal(t, scanSHA, listed.Commit)
	require.Equal(t, int32(1), snapshots.calls.Load())

	// The task's access rules come first: a task the caller may not read
	// is not found, and nothing is read for it.
	source.err = repository.ErrConversationNotFound
	_, err = files.list(ctx, ws, task, "")
	require.ErrorIs(t, err, repository.ErrConversationNotFound)
	_, err = files.content(ctx, ws, task, "a.txt", "", 1, 0)
	require.ErrorIs(t, err, repository.ErrConversationNotFound)
	require.Len(t, scans.requests, 1)
	require.Equal(t, int32(1), snapshots.calls.Load())
}

// Before anything is published, the task branch's commit is read on the
// task's machine with the tree and show scans; an unrecorded ref is read only
// while it is that commit. The machine's answers become the same results.
func TestUnpublishedTaskFilesAreReadOnTheMachine(t *testing.T) {
	head := strings.Repeat("c", 40)
	scans := &fileScans{t: t}
	source := &taskFileSourceFake{source: &domain.TaskFileSource{Repository: "acme/app", Changed: []string{"src/app.go"}}}
	files := &taskFiles{sources: source, scans: scans}
	ctx := taskFilesContext()
	ws, task := uuid.New(), uuid.New()

	scans.answer = func(req domain.WorktreeScanRequest) domain.WorktreeScanResult {
		if req.Kind == domain.WorktreeScanTree {
			return domain.WorktreeScanResult{Commit: head, Files: []string{"README.md", "src/app.go"}, Truncated: true}
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
	listed, err := files.list(ctx, ws, task, "")
	require.NoError(t, err)
	require.Equal(t, &domain.TaskFiles{Commit: head, Files: []domain.TaskFile{{Path: "README.md"}, {Path: "src/app.go", Changed: true}}, Truncated: true}, listed)
	require.Equal(t, domain.WorktreeScanRequest{Kind: domain.WorktreeScanTree, Ref: "HEAD"}, scans.requests[0])

	page, err := files.content(ctx, ws, task, "src/app.go", head, 0, 0)
	require.NoError(t, err)
	require.Equal(t, domain.TaskFileContent{Path: "src/app.go", Commit: head, Content: "package app", Start: 1, End: 1, TotalLines: 1}, *page)
	require.Equal(t, domain.WorktreeScanRequest{Kind: domain.WorktreeScanShow, Ref: "HEAD", Path: "src/app.go", StartLine: 1, EndLine: domain.TaskFileLinesMax}, scans.requests[1])
	binary, err := files.content(ctx, ws, task, "logo.png", "", 1, 0)
	require.NoError(t, err)
	require.True(t, binary.Binary)
	large, err := files.content(ctx, ws, task, "huge.sql", "", 1, 0)
	require.NoError(t, err)
	require.True(t, large.TooLarge)
	_, err = files.content(ctx, ws, task, ".env", "", 1, 0)
	require.ErrorIs(t, err, ErrTaskFileNotFound, "the machine reads the commit, which does not track it")
	requests := len(scans.requests)
	_, err = files.content(ctx, ws, task, "../.env", "", 1, 0)
	require.ErrorIs(t, err, ErrTaskFileNotFound)
	require.Len(t, scans.requests, requests, "an invalid path is refused before the machine is asked")

	// The branch moved on: the commit asked for is no longer the head.
	_, err = files.list(ctx, ws, task, strings.Repeat("d", 40))
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
		_, err = files.list(ctx, ws, task, "")
		require.ErrorIs(t, err, tc.want)
	}
	files.scans = nil
	_, err = files.list(ctx, ws, task, "")
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
