package domain

import (
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func treeNode(parent *uuid.UUID, position int) WikiTreeNode {
	return WikiTreeNode{ID: uuid.New(), ParentID: parent, Position: position, Kind: WikiKindPage}
}

func positions(changes []WikiTreeNode) map[uuid.UUID]int {
	out := map[uuid.UUID]int{}
	for _, node := range changes {
		out[node.ID] = node.Position
	}
	return out
}

func TestPlanWikiMoveReordersAndNests(t *testing.T) {
	folder := treeNode(nil, 0)
	folder.Kind = WikiKindFolder
	a, b := treeNode(&folder.ID, 0), treeNode(&folder.ID, 1)
	top := treeNode(nil, 1)
	nodes := []WikiTreeNode{folder, a, b, top}

	// Into the folder, between a and b: b shifts down, the top level closes up.
	changes, err := PlanWikiMove(nodes, top.ID, &folder.ID, 1)
	require.NoError(t, err)
	require.Equal(t, map[uuid.UUID]int{top.ID: 1, b.ID: 2}, positions(changes))
	for _, node := range changes {
		require.Equal(t, folder.ID, *node.ParentID)
	}

	// Out to the top level, first: the old siblings renumber.
	changes, err = PlanWikiMove(nodes, a.ID, nil, 0)
	require.NoError(t, err)
	require.Equal(t, map[uuid.UUID]int{a.ID: 0, folder.ID: 1, top.ID: 2, b.ID: 0}, positions(changes))

	// Same place: nothing changes. Index past the end means last.
	changes, err = PlanWikiMove(nodes, b.ID, &folder.ID, 1)
	require.NoError(t, err)
	require.Empty(t, changes)
	changes, err = PlanWikiMove(nodes, a.ID, &folder.ID, 99)
	require.NoError(t, err)
	require.Equal(t, map[uuid.UUID]int{b.ID: 0, a.ID: 1}, positions(changes))
}

func TestPlanWikiMoveRejectsCyclesDepthAndOtherScopes(t *testing.T) {
	root := treeNode(nil, 0)
	child := treeNode(&root.ID, 0)
	grandchild := treeNode(&child.ID, 0)
	nodes := []WikiTreeNode{root, child, grandchild}

	_, err := PlanWikiMove(nodes, root.ID, &grandchild.ID, 0)
	require.ErrorIs(t, err, ErrWikiMoveCycle)
	_, err = PlanWikiMove(nodes, root.ID, &root.ID, 0)
	require.ErrorIs(t, err, ErrWikiMoveCycle)

	elsewhere := uuid.New() // a page of another scope is simply not in nodes
	_, err = PlanWikiMove(nodes, child.ID, &elsewhere, 0)
	require.ErrorIs(t, err, ErrWikiMoveNotFound)
	_, err = PlanWikiMove(nodes, elsewhere, nil, 0)
	require.ErrorIs(t, err, ErrWikiMoveNotFound)

	// A chain of six is full: nothing more fits at its bottom, and a
	// two-level subtree cannot go under the fifth.
	chain := []WikiTreeNode{treeNode(nil, 0)}
	for i := 1; i < WikiMaxDepth; i++ {
		chain = append(chain, treeNode(&chain[i-1].ID, 0))
	}
	loose := treeNode(nil, 1)
	pair := treeNode(nil, 2)
	pairChild := treeNode(&pair.ID, 0)
	all := append(chain, loose, pair, pairChild)
	_, err = PlanWikiMove(all, loose.ID, &chain[WikiMaxDepth-1].ID, 0)
	require.ErrorIs(t, err, ErrWikiMoveTooDeep)
	_, err = PlanWikiMove(all, loose.ID, &chain[WikiMaxDepth-2].ID, 0)
	require.NoError(t, err)
	_, err = PlanWikiMove(all, pair.ID, &chain[WikiMaxDepth-2].ID, 0)
	require.ErrorIs(t, err, ErrWikiMoveTooDeep)

	registry := treeNode(nil, 3)
	registry.Registry = true
	_, err = PlanWikiMove(append(all, registry), registry.ID, nil, 0)
	require.ErrorIs(t, err, ErrWikiMoveNotInTree)
	_, err = PlanWikiMove(append(all, registry), loose.ID, &registry.ID, 0)
	require.ErrorIs(t, err, ErrWikiMoveNotInTree)
}
