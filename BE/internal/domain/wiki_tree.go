package domain

import (
	"errors"
	"sort"

	"github.com/google/uuid"
)

// Wiki page kinds: a folder groups pages in the tree; it is still a page.
const (
	WikiKindPage   = "page"
	WikiKindFolder = "folder"
)

// WikiMaxDepth is how deep pages may nest; the top level is depth 1.
const WikiMaxDepth = 6

var (
	ErrWikiMoveCycle     = errors.New("a page cannot move into itself or a page inside it")
	ErrWikiMoveTooDeep   = errors.New("pages can nest at most 6 levels deep")
	ErrWikiMoveNotInTree = errors.New("registry pages are not part of the page tree")
)

// WikiTreeNode is one page's place in its scope's tree.
type WikiTreeNode struct {
	ID       uuid.UUID  `db:"id"`
	ParentID *uuid.UUID `db:"parent_id"`
	Position int        `db:"position"`
	Kind     string     `db:"kind"`
	Registry bool       `db:"registry"`
}

func sameParent(a, b *uuid.UUID) bool {
	return (a == nil && b == nil) || (a != nil && b != nil && *a == *b)
}

// PlanWikiMove places page id under parent (nil for the top level) at index
// among its new siblings, counted without the page itself. nodes must be the
// whole tree of one scope; a parent missing from it is outside the scope.
// It returns every node whose parent or position changes. Unknown pages
// report ErrWikiMoveNotFound so callers answer like any invisible page.
func PlanWikiMove(nodes []WikiTreeNode, id uuid.UUID, parent *uuid.UUID, index int) ([]WikiTreeNode, error) {
	byID := make(map[uuid.UUID]WikiTreeNode, len(nodes))
	for _, node := range nodes {
		byID[node.ID] = node
	}
	moving, ok := byID[id]
	if !ok {
		return nil, ErrWikiMoveNotFound
	}
	if moving.Registry {
		return nil, ErrWikiMoveNotInTree
	}
	depth := 0
	if parent != nil {
		target, ok := byID[*parent]
		if !ok {
			return nil, ErrWikiMoveNotFound
		}
		if target.Registry {
			return nil, ErrWikiMoveNotInTree
		}
		// Walk up from the new parent: meeting the page means a cycle.
		for at, steps := &target, 0; at != nil; steps++ {
			if at.ID == id {
				return nil, ErrWikiMoveCycle
			}
			depth++
			if at.ParentID == nil || steps > len(nodes) {
				break
			}
			next, ok := byID[*at.ParentID]
			if !ok {
				break
			}
			at = &next
		}
	}
	if depth+wikiSubtreeHeight(nodes, id, 0) > WikiMaxDepth {
		return nil, ErrWikiMoveTooDeep
	}

	siblings := func(of *uuid.UUID) []WikiTreeNode {
		list := []WikiTreeNode{}
		for _, node := range nodes {
			if node.ID != id && !node.Registry && sameParent(node.ParentID, of) {
				list = append(list, node)
			}
		}
		sort.SliceStable(list, func(i, j int) bool {
			if list[i].Position != list[j].Position {
				return list[i].Position < list[j].Position
			}
			return list[i].ID.String() < list[j].ID.String()
		})
		return list
	}
	target := siblings(parent)
	if index < 0 {
		index = 0
	}
	if index > len(target) {
		index = len(target)
	}
	moving.ParentID = parent
	target = append(target[:index], append([]WikiTreeNode{moving}, target[index:]...)...)

	changed := map[uuid.UUID]WikiTreeNode{}
	renumber := func(list []WikiTreeNode) {
		for position, node := range list {
			original := byID[node.ID]
			if original.Position != position || !sameParent(original.ParentID, node.ParentID) {
				node.Position = position
				changed[node.ID] = node
			}
		}
	}
	renumber(target)
	if !sameParent(byID[id].ParentID, parent) {
		renumber(siblings(byID[id].ParentID))
	}
	out := make([]WikiTreeNode, 0, len(changed))
	for _, node := range changed {
		out = append(out, node)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID.String() < out[j].ID.String() })
	return out, nil
}

// ErrWikiMoveNotFound: the page or the new parent is not in the scope.
var ErrWikiMoveNotFound = errors.New("wiki page not found")

func wikiSubtreeHeight(nodes []WikiTreeNode, id uuid.UUID, guard int) int {
	if guard > len(nodes) {
		return 1
	}
	height := 0
	for _, node := range nodes {
		if node.ParentID != nil && *node.ParentID == id {
			if h := wikiSubtreeHeight(nodes, node.ID, guard+1); h > height {
				height = h
			}
		}
	}
	return height + 1
}

// Wiki publication modes.
const (
	WikiPublishOff      = "off"
	WikiPublishInternal = "internal"
	WikiPublishPublic   = "public"
)

// WikiPublication is how a page is published and, when public, its link.
type WikiPublication struct {
	Mode     string     `db:"mode"`
	Children bool       `db:"children"`
	LinkID   *uuid.UUID `db:"link_id"`
	Token    *string    `db:"token"`
}
