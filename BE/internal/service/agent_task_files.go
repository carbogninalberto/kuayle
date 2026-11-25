package service

import (
	"context"
	"encoding/json"
	"errors"
	"path"
	"slices"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/kuayle/kuayle-backend/internal/repository"
	gh "github.com/kuayle/kuayle-backend/pkg/github"
	log "github.com/sirupsen/logrus"
)

// A task's files at a commit (KUA-280, KUA-319). The creator reads the files
// one commit of the task's branch tracks: by default its head, one directory
// at a time. A published task is listed through GitHub's trees API (a
// directory per request, cached by the immutable tree SHA) and its file
// contents are read from a temporary GitHub checkout of that commit
// (RepositoryCheckouts), downloaded only when a file is opened; before
// anything is published, and for an attempt's own commits, the commit is read
// on the task's machine with git (the tree and show worktree scans). Neither
// reads the worktree, so untracked and ignored files such as a .env are never
// listed or served, and a path is served only when the commit tracks it.

var (
	// ErrTaskFileRef: ref is not the task's head or a commit of its attempts.
	ErrTaskFileRef = errors.New("ref is not the task's head or a commit of its attempts")
	// ErrTaskFilePath: the directory asked for is not a clean relative path.
	ErrTaskFilePath = errors.New("path is not a clean relative directory")
	// ErrTaskFileNotFound: the commit tracks no file or directory at that path.
	ErrTaskFileNotFound = errors.New("the commit tracks nothing at that path")
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
	trees     repositoryTrees
	scans     worktreeScanStore
	wait      time.Duration
	// dirs holds the directories and search indexes read from GitHub by tree
	// SHA, commits the root tree SHA of each commit read.
	dirs    *treeCache[*cachedTree]
	commits *treeCache[string]
	once    sync.Once
}

// SetTaskFiles lets task creators read their task's files: published
// commits' contents from repository checkouts, the rest from worktree scans.
func (s *AgentTaskService) SetTaskFiles(checkouts *RepositoryCheckouts, scans worktreeScanStore) {
	s.taskFiles().checkouts, s.files.scans = checkouts, scans
}

// SetTaskFileTrees lets published commits be listed through GitHub's trees
// API, a directory at a time, instead of from a downloaded archive.
func (s *AgentTaskService) SetTaskFileTrees(trees repositoryTrees) {
	s.taskFiles().trees = trees
}

func (s *AgentTaskService) taskFiles() *taskFiles {
	if s.files == nil {
		s.files = &taskFiles{}
	}
	return s.files
}

// TaskFiles lists one directory of the files a commit of the task tracks
// (the task's head by default), or searches their paths.
func (s *AgentTaskService) TaskFiles(ctx context.Context, ws, id uuid.UUID, q TaskFilesQuery) (*domain.TaskFiles, error) {
	if s.files == nil {
		return nil, ErrTaskFilesUnavailable
	}
	return s.files.list(ctx, ws, id, q)
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
	// changedDirs holds every directory above a changed file.
	changedDirs map[string]bool
	github      bool
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
	target := &taskFileTarget{changed: map[string]bool{}, changedDirs: map[string]bool{}, filesMax: source.FilesMax}
	if target.filesMax <= 0 || target.filesMax > domain.TaskFilesMax {
		target.filesMax = domain.TaskFilesMax
	}
	for _, name := range source.Changed {
		target.changed[name] = true
		for dir := path.Dir(name); dir != "." && dir != "/"; dir = path.Dir(dir) {
			target.changedDirs[dir] = true
		}
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

// TaskFilesQuery is what one listing reads: the commit (Ref, the task's
// head by default), one directory of it (Path, the root by default; one
// trailing slash is allowed) or, with Query, the files whose path contains
// that text.
type TaskFilesQuery struct {
	Ref, Path, Query string
}

// list reads one directory of the commit, or searches its paths. A published
// commit is read through GitHub's trees API, one directory per request and
// cached by tree SHA, so no archive is downloaded and nothing but the
// directory asked for is read; the machine reads the one directory with
// git ls-tree.
func (f *taskFiles) list(ctx context.Context, ws, id uuid.UUID, q TaskFilesQuery) (*domain.TaskFiles, error) {
	dir := strings.TrimSuffix(q.Path, "/")
	query := strings.TrimSpace(q.Query)
	if !domain.ValidTaskDirPath(dir) {
		return nil, ErrTaskFilePath
	}
	if len(query) > domain.TaskFileQueryMax || (query != "" && dir != "") {
		return nil, repository.ErrConversationInvalid
	}
	f.caches()
	target, err := f.target(ctx, ws, id, q.Ref)
	if err != nil {
		return nil, err
	}
	result := &domain.TaskFiles{Path: dir, Query: query, Entries: []domain.TaskFileEntry{}}
	if target.github {
		result.Commit = target.commit
		if err := f.listGitHub(ctx, ws, target, result); err != nil {
			return nil, err
		}
		return result, nil
	}
	request := domain.WorktreeScanRequest{Kind: domain.WorktreeScanTree, Ref: target.machineRef, Path: dir, Limit: target.filesMax}
	if query != "" {
		request.Pattern, request.Limit = query, domain.TaskFileSearchMax
	}
	scanned, err := f.scan(ctx, ws, id, target, request)
	if err != nil {
		return nil, err
	}
	if scanned.Missing {
		return nil, ErrTaskFileNotFound
	}
	result.Commit, result.Tree, result.Truncated, result.Matches = scanned.Commit, scanned.Tree, scanned.Truncated, scanned.FileMatches
	for _, entry := range scanned.Entries {
		result.Entries = append(result.Entries, target.entry(dir, entry))
	}
	for _, name := range scanned.Files {
		result.Entries = append(result.Entries, target.entry("", domain.TreeEntry{Name: path.Base(name), Kind: domain.TreeEntryBlob}, name))
	}
	return result, nil
}

// entry is a tree entry of directory dir as the listing shows it. A search
// result passes the file's whole path.
func (t *taskFileTarget) entry(dir string, entry domain.TreeEntry, whole ...string) domain.TaskFileEntry {
	name := path.Join(dir, entry.Name)
	if len(whole) > 0 {
		name = whole[0]
	}
	changed := t.changed[name]
	if entry.Kind == domain.TreeEntryTree {
		changed = t.changedDirs[name]
	}
	return domain.TaskFileEntry{TreeEntry: entry, Path: name, Changed: changed}
}

// listGitHub fills result from GitHub's trees. The commit's root tree is
// read by commit (cached, as a commit never changes either), then each
// directory of the path by its SHA, which the parent's entry names.
func (f *taskFiles) listGitHub(ctx context.Context, ws uuid.UUID, target *taskFileTarget, result *domain.TaskFiles) error {
	if f.trees == nil || target.repository == "" {
		return ErrTaskFilesUnavailable
	}
	root, err := f.commitTree(ctx, ws, target)
	if err != nil {
		return err
	}
	if result.Query != "" {
		return f.searchGitHub(ctx, ws, target, root, result)
	}
	sha := root
	if result.Path != "" {
		for _, segment := range strings.Split(result.Path, "/") {
			parent, err := f.directory(ctx, ws, target, sha)
			if err != nil {
				return err
			}
			i := slices.IndexFunc(parent.entries, func(e domain.TreeEntry) bool { return e.Name == segment && e.Kind == domain.TreeEntryTree })
			if i < 0 {
				return ErrTaskFileNotFound
			}
			sha = parent.entries[i].SHA
		}
	}
	tree, err := f.directory(ctx, ws, target, sha)
	if err != nil {
		return err
	}
	result.Tree, result.Truncated = sha, tree.truncated
	entries := tree.entries
	if len(entries) > target.filesMax {
		entries, result.Truncated = entries[:target.filesMax], true
	}
	for _, entry := range entries {
		result.Entries = append(result.Entries, target.entry(result.Path, entry))
	}
	return nil
}

// searchGitHub lists the files whose path contains the query, from the
// commit's recursive tree (cached by its root SHA like a directory).
func (f *taskFiles) searchGitHub(ctx context.Context, ws uuid.UUID, target *taskFileTarget, root string, result *domain.TaskFiles) error {
	index, err := f.index(ctx, ws, target, root)
	if err != nil {
		return err
	}
	needle := strings.ToLower(result.Query)
	result.Truncated = index.truncated
	for _, file := range index.entries {
		if !strings.Contains(strings.ToLower(file.Name), needle) {
			continue
		}
		result.Matches++
		if len(result.Entries) == domain.TaskFileSearchMax {
			result.Truncated = true
			continue
		}
		whole := file.Name
		file.Name = path.Base(whole)
		result.Entries = append(result.Entries, target.entry("", file, whole))
	}
	return nil
}

// cachedTree is a directory's entries (or, for a search index, every file's
// path and SHA) as read from one git tree; truncated: GitHub or the bound cut it.
type cachedTree struct {
	entries   []domain.TreeEntry
	truncated bool
}

func (f *taskFiles) cacheKey(kind string, ws uuid.UUID, target *taskFileTarget, sha string) string {
	return kind + "/" + ws.String() + "/" + target.repository + "/" + sha
}

// commitTree is the root tree SHA of the target commit.
func (f *taskFiles) commitTree(ctx context.Context, ws uuid.UUID, target *taskFileTarget) (string, error) {
	key := f.cacheKey("commit", ws, target, target.commit)
	if tree, ok := f.commits.get(key); ok {
		return tree, nil
	}
	tree, err := f.trees.RepositoryCommitTree(ctx, ws, target.repository, target.commit)
	if err != nil {
		return "", treeFailure(target, err)
	}
	f.commits.put(key, tree, 1)
	return tree, nil
}

// directory is the entries of the git tree sha: the cache's, or read from
// GitHub once. Only tracked regular files and directories are kept, named
// as a task file can be, directories first.
func (f *taskFiles) directory(ctx context.Context, ws uuid.UUID, target *taskFileTarget, sha string) (*cachedTree, error) {
	key := f.cacheKey("tree", ws, target, sha)
	if tree, ok := f.dirs.get(key); ok {
		return tree, nil
	}
	listing, err := f.trees.RepositoryTree(ctx, ws, target.repository, sha, false)
	if err != nil {
		return nil, treeFailure(target, err)
	}
	tree := &cachedTree{entries: []domain.TreeEntry{}, truncated: listing.Truncated}
	for _, node := range listing.Tree {
		if entry, ok := treeEntry(node, node.Path); ok && !strings.Contains(node.Path, "/") {
			tree.entries = append(tree.entries, entry)
		}
	}
	domain.SortTreeEntries(tree.entries)
	if len(tree.entries) > domain.TaskFilesMax {
		tree.entries, tree.truncated = tree.entries[:domain.TaskFilesMax], true
	}
	f.dirs.put(key, tree, len(tree.entries))
	return tree, nil
}

// index is every tracked file of the commit's root tree, by path, for a search.
func (f *taskFiles) index(ctx context.Context, ws uuid.UUID, target *taskFileTarget, root string) (*cachedTree, error) {
	key := f.cacheKey("index", ws, target, root)
	if tree, ok := f.dirs.get(key); ok {
		return tree, nil
	}
	listing, err := f.trees.RepositoryTree(ctx, ws, target.repository, root, true)
	if err != nil {
		return nil, treeFailure(target, err)
	}
	tree := &cachedTree{entries: []domain.TreeEntry{}, truncated: listing.Truncated}
	for _, node := range listing.Tree {
		if entry, ok := treeEntry(node, node.Path); ok && entry.Kind == domain.TreeEntryBlob {
			tree.entries = append(tree.entries, entry)
		}
	}
	sort.Slice(tree.entries, func(i, j int) bool { return tree.entries[i].Name < tree.entries[j].Name })
	if len(tree.entries) > domain.TaskFilesMax {
		tree.entries, tree.truncated = tree.entries[:domain.TaskFilesMax], true
	}
	f.dirs.put(key, tree, len(tree.entries))
	return tree, nil
}

// treeEntry keeps what a listing shows of a GitHub tree node: a directory,
// or a regular file (modes 100644 and 100755) whose path a task file can
// have. Links and submodules are not listed, as the machine's listing omits them.
func treeEntry(node gh.TreeNode, name string) (domain.TreeEntry, bool) {
	if !domain.ValidTaskFilePath(name) || !validObjectID(node.SHA) {
		return domain.TreeEntry{}, false
	}
	switch {
	case node.Type == "tree" && node.Mode == "040000":
		return domain.TreeEntry{Name: name, Kind: domain.TreeEntryTree, SHA: node.SHA}, true
	case node.Type == "blob" && (node.Mode == "100644" || node.Mode == "100755"):
		return domain.TreeEntry{Name: name, Kind: domain.TreeEntryBlob, SHA: node.SHA, Size: node.Size}, true
	}
	return domain.TreeEntry{}, false
}

func validObjectID(sha string) bool { return domain.ValidCommitID(sha) }

// treeFailure logs a failed tree read by its kind, never its text, and
// answers what a failed read of the files answers.
func treeFailure(target *taskFileTarget, err error) error {
	var api *gh.APIError
	reason := "the tree could not be read"
	switch {
	case errors.As(err, &api):
		reason = "GitHub answered " + strconv.Itoa(api.Status)
	case errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded):
		reason = "the request ended before the read"
	}
	log.WithFields(log.Fields{"repository": target.repository, "commit": target.commit, "reason": reason}).Warn("Task files tree read failed")
	return ErrTaskFilesFailed
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

// caches creates the tree caches on first use.
func (f *taskFiles) caches() {
	f.once.Do(func() {
		if f.dirs == nil {
			f.dirs = newTreeCache[*cachedTree](defaultTreeCacheLimits)
		}
		if f.commits == nil {
			f.commits = newTreeCache[string](defaultTreeCacheLimits)
		}
	})
}
