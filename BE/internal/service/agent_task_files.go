package service

import (
	"context"
	"encoding/json"
	"errors"
	"strconv"
	"time"

	"github.com/google/uuid"
	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/kuayle/kuayle-backend/internal/repository"
	gh "github.com/kuayle/kuayle-backend/pkg/github"
	log "github.com/sirupsen/logrus"
)

// A task's files at a commit (KUA-280). The creator reads the files one
// commit of the task's branch tracks: by default its head. A published task
// is read at a commit Kuayle published (or GitHub's head) from a temporary
// GitHub checkout of that commit (RepositoryCheckouts); before anything is
// published, and for an attempt's own commits, the commit is read on the
// task's machine with git (the tree and show worktree scans). Neither reads
// the worktree, so untracked and ignored files such as a .env are never
// listed or served, and a path is served only when the commit tracks it.

var (
	// ErrTaskFileRef: ref is not the task's head or a commit of its attempts.
	ErrTaskFileRef = errors.New("ref is not the task's head or a commit of its attempts")
	// ErrTaskFileNotFound: the commit tracks no file at that path.
	ErrTaskFileNotFound = errors.New("the commit tracks no file at that path")
	// ErrTaskFilesUnavailable: nothing is published and the task's checkout
	// is not on a running machine (or this server cannot read either).
	ErrTaskFilesUnavailable = errors.New("the task's files cannot be read now")
	// ErrTaskFilesBusy: the requester's scans already fill the machine.
	ErrTaskFilesBusy = errors.New("too many reads are running on the task's machine")
	// ErrTaskFilesFailed: GitHub or the machine could not read the commit.
	ErrTaskFilesFailed = errors.New("the files could not be read at this commit")
)

// taskFileSources reads, under the task's access rules, where its files are.
type taskFileSources interface {
	TaskFileSource(ctx context.Context, ws, id uuid.UUID, ref string) (*domain.TaskFileSource, error)
}

type taskFiles struct {
	sources   taskFileSources
	checkouts *RepositoryCheckouts
	scans     worktreeScanStore
	wait      time.Duration
}

// SetTaskFiles lets task creators read their task's files: published
// commits from repository checkouts, the rest from worktree scans.
func (s *AgentTaskService) SetTaskFiles(checkouts *RepositoryCheckouts, scans worktreeScanStore) {
	if s.files == nil {
		s.files = &taskFiles{}
	}
	s.files.checkouts, s.files.scans = checkouts, scans
}

// TaskFiles lists the files a commit of the task tracks (ref, or its head).
func (s *AgentTaskService) TaskFiles(ctx context.Context, ws, id uuid.UUID, ref string) (*domain.TaskFiles, error) {
	if s.files == nil {
		return nil, ErrTaskFilesUnavailable
	}
	return s.files.list(ctx, ws, id, ref)
}

// TaskFileContent returns lines start to end of one of those files.
func (s *AgentTaskService) TaskFileContent(ctx context.Context, ws, id uuid.UUID, name, ref string, start, end int) (*domain.TaskFileContent, error) {
	if s.files == nil {
		return nil, ErrTaskFilesUnavailable
	}
	return s.files.content(ctx, ws, id, name, ref, start, end)
}

// taskFileTarget is the commit to read: on GitHub (repository, commit) or on
// the machine (machineRef, which must resolve to expect when set).
type taskFileTarget struct {
	repository, commit string
	machineRef, expect string
	changed            map[string]bool
	github             bool
	// filesMax is how many files the workspace lets a listing hold.
	filesMax int
}

func (f *taskFiles) target(ctx context.Context, ws, id uuid.UUID, ref string) (*taskFileTarget, error) {
	if ref != "" && !domain.ValidCommitID(ref) {
		return nil, ErrTaskFileRef
	}
	if f.sources == nil {
		return nil, ErrTaskFilesUnavailable
	}
	source, err := f.sources.TaskFileSource(ctx, ws, id, ref)
	if err != nil {
		return nil, err
	}
	target := &taskFileTarget{changed: map[string]bool{}, filesMax: source.FilesMax}
	if target.filesMax <= 0 || target.filesMax > domain.TaskFilesMax {
		target.filesMax = domain.TaskFilesMax
	}
	for _, name := range source.Changed {
		target.changed[name] = true
	}
	switch {
	case source.Head != "" && (ref == "" || ref == source.Head || source.RefPublished):
		target.github, target.repository, target.commit = true, source.Repository, source.Head
		if ref != "" {
			target.commit = ref
		}
	case ref == "":
		target.machineRef = "HEAD"
	case source.RefRecorded:
		target.machineRef = ref
	case source.Head == "":
		// Before a publication the head is the task branch's tip.
		target.machineRef, target.expect = "HEAD", ref
	default:
		return nil, ErrTaskFileRef
	}
	return target, nil
}

func (f *taskFiles) list(ctx context.Context, ws, id uuid.UUID, ref string) (*domain.TaskFiles, error) {
	target, err := f.target(ctx, ws, id, ref)
	if err != nil {
		return nil, err
	}
	result := &domain.TaskFiles{Files: []domain.TaskFile{}}
	if target.github {
		checkout, release, err := f.checkout(ctx, ws, target)
		if err != nil {
			return nil, err
		}
		defer release()
		result.Commit, result.Truncated = checkout.commit, checkout.truncated
		for _, file := range checkout.files {
			if !domain.ValidTaskFilePath(file.Path) {
				continue
			}
			if len(result.Files) == target.filesMax {
				result.Truncated = true
				break
			}
			result.Files = append(result.Files, domain.TaskFile{Path: file.Path, Changed: target.changed[file.Path]})
		}
		return result, nil
	}
	scanned, err := f.scan(ctx, ws, id, target, domain.WorktreeScanRequest{Kind: domain.WorktreeScanTree, Ref: target.machineRef, Limit: target.filesMax})
	if err != nil {
		return nil, err
	}
	result.Commit, result.Truncated = scanned.Commit, scanned.Truncated
	for _, name := range scanned.Files {
		result.Files = append(result.Files, domain.TaskFile{Path: name, Changed: target.changed[name]})
	}
	return result, nil
}

func (f *taskFiles) content(ctx context.Context, ws, id uuid.UUID, name, ref string, start, end int) (*domain.TaskFileContent, error) {
	if start == 0 {
		start = 1
	}
	if start < 1 || end < 0 || (end > 0 && end < start) {
		return nil, repository.ErrConversationInvalid
	}
	if limit := start + domain.TaskFileLinesMax - 1; end == 0 || end > limit {
		end = limit
	}
	target, err := f.target(ctx, ws, id, ref)
	if err != nil {
		return nil, err
	}
	if !domain.ValidTaskFilePath(name) {
		return nil, ErrTaskFileNotFound
	}
	result := &domain.TaskFileContent{Path: name, Start: start}
	if target.github {
		checkout, release, err := f.checkout(ctx, ws, target)
		if err != nil {
			return nil, err
		}
		defer release()
		result.Commit = checkout.commit
		// The checkout's index is the listing: ignored, linked and escaping
		// paths are not in it, and the read never follows a link out.
		file := checkout.file(name)
		if file == nil || file.Path != name {
			return nil, ErrTaskFileNotFound
		}
		if file.Omitted != "" {
			result.TooLarge = true
			return result, nil
		}
		if file.Binary {
			result.Binary = true
			return result, nil
		}
		data, err := checkout.read(name)
		if err != nil {
			log.WithError(err).WithField("task_id", id).Warn("Task file read failed")
			return nil, ErrTaskFilesFailed
		}
		result.Content, result.End, result.TotalLines, result.Truncated = domain.TaskFilePage(data, start, end, domain.TaskFileBytesMax)
		return result, nil
	}
	scanned, err := f.scan(ctx, ws, id, target, domain.WorktreeScanRequest{Kind: domain.WorktreeScanShow, Ref: target.machineRef, Path: name, StartLine: start, EndLine: end})
	if err != nil {
		return nil, err
	}
	if scanned.Missing {
		return nil, ErrTaskFileNotFound
	}
	result.Commit, result.Content, result.End, result.TotalLines = scanned.Commit, scanned.Content, scanned.EndLine, scanned.TotalLines
	result.Truncated, result.Binary, result.TooLarge = scanned.Truncated, scanned.Binary, scanned.TooLarge
	return result, nil
}

// checkout acquires the GitHub checkout of the target commit; release must be
// called when reading is done.
func (f *taskFiles) checkout(ctx context.Context, ws uuid.UUID, target *taskFileTarget) (*repositoryCheckout, func(), error) {
	if f.checkouts == nil || target.repository == "" {
		return nil, nil, ErrTaskFilesUnavailable
	}
	checkout, release, err := f.checkouts.Acquire(ctx, ws, target.repository, target.commit)
	if err != nil {
		log.WithFields(log.Fields{"repository": target.repository, "commit": target.commit, "reason": checkoutFailure(err)}).Warn("Task files checkout failed")
		return nil, nil, ErrTaskFilesFailed
	}
	if checkout.commit != target.commit {
		release()
		return nil, nil, ErrTaskFilesFailed
	}
	return checkout, release, nil
}

// checkoutFailure is what is logged of a failed checkout: a user-safe
// message or GitHub's status, never the error's text, whose archive URL can
// carry a download token.
func checkoutFailure(err error) string {
	var safe *errAssistantTool
	var api *gh.APIError
	switch {
	case errors.As(err, &safe):
		return safe.message
	case errors.As(err, &api):
		return "GitHub answered " + strconv.Itoa(api.Status)
	case errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded):
		return "the request ended before the download"
	}
	return "the archive could not be downloaded"
}

// scan runs one tree or show scan on the task's machine as the requester and
// waits for its result.
func (f *taskFiles) scan(ctx context.Context, ws, id uuid.UUID, target *taskFileTarget, req domain.WorktreeScanRequest) (*domain.WorktreeScanResult, error) {
	if f.scans == nil {
		return nil, ErrTaskFilesUnavailable
	}
	access, _ := domain.AccessFromContext(ctx)
	scan, err := f.scans.CreateWorktreeScan(ctx, ws, access.UserID, domain.WorktreeScanTarget{TaskID: &id}, req)
	switch {
	case errors.Is(err, repository.ErrWorktreeUnavailable):
		return nil, ErrTaskFilesUnavailable
	case errors.Is(err, repository.ErrWorktreeScanBusy):
		return nil, ErrTaskFilesBusy
	case err != nil:
		return nil, err
	case scan == nil:
		return nil, ErrTaskFilesFailed
	}
	scan, err = awaitWorktreeScan(ctx, f.scans, ws, scan, f.wait)
	if errors.Is(err, errWorktreeScanWait) {
		return nil, ErrTaskFilesFailed
	}
	if err != nil {
		return nil, err
	}
	var result domain.WorktreeScanResult
	if scan.Error != nil || json.Unmarshal(scan.Result, &result) != nil {
		return nil, ErrTaskFilesFailed
	}
	if target.expect != "" && result.Commit != target.expect {
		return nil, ErrTaskFileRef
	}
	return &result, nil
}
