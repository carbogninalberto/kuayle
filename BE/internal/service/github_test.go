package service

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/stretchr/testify/assert"
)

func TestGitHubServiceResolveIssueFromRefMatchesIdentifiersCaseInsensitive(t *testing.T) {
	ctx := context.Background()
	workspaceID := uuid.New()
	issue := &domain.Issue{
		ID:          uuid.New(),
		WorkspaceID: workspaceID,
		Identifier:  "KUA-1",
	}
	issueRepo := new(mockIssueRepo)
	issueRepo.On("GetByIdentifier", ctx, workspaceID, "KUA-1").Return(issue, nil).Once()

	svc := &GitHubService{issueRepo: issueRepo}

	result := svc.resolveIssueFromRef(ctx, workspaceID, "fix/kua-1-mobile-layout")

	assert.Equal(t, issue, result)
	issueRepo.AssertExpectations(t)
}
