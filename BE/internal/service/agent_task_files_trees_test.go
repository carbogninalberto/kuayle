package service

import (
	"context"
	"crypto/sha1"
	"encoding/hex"
	"errors"
	"fmt"
	"path"
	"sort"
	"strings"
	"sync"

	"github.com/google/uuid"
	gh "github.com/kuayle/kuayle-backend/pkg/github"
)

// gitFake is a content-addressed git repository behind GitHub's trees API:
// a commit's files become trees whose SHA is the hash of their entries, so a
// directory whose files did not change has the same SHA in every commit, as
// it has in git. It counts the reads it answers.
type gitFake struct {
	mu      sync.Mutex
	commits map[string]string
	trees   map[string]*gh.TreeListing
	// recursive: the root trees' flat listings.
	flat       map[string]*gh.TreeListing
	commitReqs []string
	treeReqs   []string
	// truncate marks every tree listing as cut by GitHub.
	truncate bool
	err      error
}

func newGitFake() *gitFake {
	return &gitFake{commits: map[string]string{}, trees: map[string]*gh.TreeListing{}, flat: map[string]*gh.TreeListing{}}
}

func objectSHA(parts ...string) string {
	sum := sha1.Sum([]byte(strings.Join(parts, "\x00")))
	return hex.EncodeToString(sum[:])
}

// commit records a commit tracking files (path -> content) and returns its
// root tree SHA. A path under a name ending in "@link" is a symlink and one
// ending in "@module" a submodule, listed by GitHub but never by Kuayle.
func (g *gitFake) commit(commit string, files map[string]string) string {
	g.mu.Lock()
	defer g.mu.Unlock()
	root := g.build("", files)
	g.commits[commit] = root
	var flat gh.TreeListing
	flat.SHA = root
	var names []string
	for name := range files {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		node, _ := fakeNode(strings.TrimSuffix(strings.TrimSuffix(name, "@link"), "@module"), name, files[name])
		flat.Tree = append(flat.Tree, node)
	}
	g.flat[root] = &flat
	return root
}

func fakeNode(name, original, content string) (gh.TreeNode, bool) {
	size := int64(len(content))
	switch {
	case strings.HasSuffix(original, "@link"):
		return gh.TreeNode{Path: name, Mode: "120000", Type: "blob", SHA: objectSHA("blob", content), Size: &size}, false
	case strings.HasSuffix(original, "@module"):
		return gh.TreeNode{Path: name, Mode: "160000", Type: "commit", SHA: objectSHA("commit", content)}, false
	}
	return gh.TreeNode{Path: name, Mode: "100644", Type: "blob", SHA: objectSHA("blob", content), Size: &size}, true
}

// build stores the tree of files (paths relative to it) and returns its SHA.
func (g *gitFake) build(_ string, files map[string]string) string {
	direct := map[string]string{}
	below := map[string]map[string]string{}
	for name, content := range files {
		dir, rest, nested := strings.Cut(name, "/")
		if !nested {
			direct[name] = content
			continue
		}
		if below[dir] == nil {
			below[dir] = map[string]string{}
		}
		below[dir][rest] = content
	}
	var nodes []gh.TreeNode
	for name, content := range direct {
		node, _ := fakeNode(strings.TrimSuffix(strings.TrimSuffix(name, "@link"), "@module"), name, content)
		nodes = append(nodes, node)
	}
	for dir, inner := range below {
		nodes = append(nodes, gh.TreeNode{Path: dir, Mode: "040000", Type: "tree", SHA: g.build(dir, inner)})
	}
	sort.Slice(nodes, func(i, j int) bool { return nodes[i].Path < nodes[j].Path })
	parts := []string{"tree"}
	for _, node := range nodes {
		parts = append(parts, node.Path, node.Mode, node.SHA)
	}
	sha := objectSHA(parts...)
	g.trees[sha] = &gh.TreeListing{SHA: sha, Tree: nodes}
	return sha
}

func (g *gitFake) RepositoryCommitTree(_ context.Context, _ uuid.UUID, _, commit string) (string, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.commitReqs = append(g.commitReqs, commit)
	if g.err != nil {
		return "", g.err
	}
	root, ok := g.commits[commit]
	if !ok {
		return "", &gh.APIError{Status: 404, Message: "Not Found"}
	}
	return root, nil
}

func (g *gitFake) RepositoryTree(_ context.Context, _ uuid.UUID, _, tree string, recursive bool) (*gh.TreeListing, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.treeReqs = append(g.treeReqs, fmt.Sprintf("%s recursive=%t", tree, recursive))
	if g.err != nil {
		return nil, g.err
	}
	source := g.trees[tree]
	if recursive {
		source = g.flat[tree]
	}
	if source == nil {
		return nil, &gh.APIError{Status: 404, Message: "Not Found"}
	}
	copied := *source
	copied.Truncated = g.truncate
	return &copied, nil
}

func (g *gitFake) reads() int {
	g.mu.Lock()
	defer g.mu.Unlock()
	return len(g.treeReqs)
}

func (g *gitFake) reset() {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.commitReqs, g.treeReqs = nil, nil
}

// filesOf is a flat map of a tarball's regular files, as the fake commit
// tracks them.
func filesOf(entries []tarEntry) map[string]string {
	files := map[string]string{}
	for _, entry := range entries {
		name := strings.TrimPrefix(entry.name, "repo-sha/")
		if entry.kind != 0 || strings.HasSuffix(name, ".env") || path.Clean(name) != name || strings.HasPrefix(name, "..") {
			continue
		}
		files[name] = entry.body
	}
	return files
}

var errGitHubDown = errors.New("connection reset by peer")
