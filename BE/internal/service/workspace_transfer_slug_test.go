package service

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

// /mcp is the MCP endpoint; an import must not create a workspace there
// either. The slug is refused before the archive or the database is read.
func TestWorkspaceImportRejectsReservedSlug(t *testing.T) {
	_, err := NewWorkspaceTransferService(nil, nil).Import(context.Background(), "", "Imported", "mcp", uuid.New())
	var transferErr *WorkspaceTransferError
	require.True(t, errors.As(err, &transferErr), "got %v", err)
	require.Equal(t, "VALIDATION_ERROR", transferErr.Code)
}
