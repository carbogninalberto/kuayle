package service

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/stretchr/testify/assert"
)

func TestTeamService_Delete(t *testing.T) {
	repo := new(mockTeamRepo)
	svc := NewTeamService(repo, new(mockTeamStatusRepo))
	ctx := context.Background()
	workspaceID := uuid.New()
	teamID := uuid.New()

	repo.On("GetByID", ctx, teamID).Return(&domain.Team{ID: teamID, WorkspaceID: workspaceID}, nil)
	repo.On("Delete", ctx, teamID).Return(nil)

	err := svc.Delete(ctx, workspaceID, teamID)

	assert.NoError(t, err)
	repo.AssertExpectations(t)
}

func TestTeamService_Delete_NotFoundWhenWorkspaceDiffers(t *testing.T) {
	repo := new(mockTeamRepo)
	svc := NewTeamService(repo, new(mockTeamStatusRepo))
	ctx := context.Background()
	teamID := uuid.New()

	repo.On("GetByID", ctx, teamID).Return(&domain.Team{ID: teamID, WorkspaceID: uuid.New()}, nil)

	err := svc.Delete(ctx, uuid.New(), teamID)

	assert.ErrorIs(t, err, ErrTeamNotFound)
	repo.AssertNotCalled(t, "Delete", ctx, teamID)
}

func TestTeamService_Leave_RemovesMembership(t *testing.T) {
	repo := new(mockTeamRepo)
	svc := NewTeamService(repo, new(mockTeamStatusRepo))
	ctx := context.Background()
	workspaceID := uuid.New()
	teamID := uuid.New()
	userID := uuid.New()
	otherUserID := uuid.New()

	repo.On("GetByID", ctx, teamID).Return(&domain.Team{ID: teamID, WorkspaceID: workspaceID}, nil)
	repo.On("GetMember", ctx, teamID, userID).Return(&domain.TeamMember{TeamID: teamID, UserID: userID}, nil)
	repo.On("ListMembers", ctx, teamID).Return([]domain.TeamMember{
		{TeamID: teamID, UserID: userID},
		{TeamID: teamID, UserID: otherUserID},
	}, nil)
	repo.On("RemoveMember", ctx, teamID, userID).Return(nil)

	deleted, err := svc.Leave(ctx, workspaceID, teamID, userID, domain.RoleMember)

	assert.NoError(t, err)
	assert.False(t, deleted)
	repo.AssertExpectations(t)
}

func TestTeamService_Leave_DeletesWhenLastMember(t *testing.T) {
	repo := new(mockTeamRepo)
	svc := NewTeamService(repo, new(mockTeamStatusRepo))
	ctx := context.Background()
	workspaceID := uuid.New()
	teamID := uuid.New()
	userID := uuid.New()

	repo.On("GetByID", ctx, teamID).Return(&domain.Team{ID: teamID, WorkspaceID: workspaceID}, nil)
	repo.On("GetMember", ctx, teamID, userID).Return(&domain.TeamMember{TeamID: teamID, UserID: userID}, nil)
	repo.On("ListMembers", ctx, teamID).Return([]domain.TeamMember{{TeamID: teamID, UserID: userID}}, nil)
	repo.On("Delete", ctx, teamID).Return(nil)

	deleted, err := svc.Leave(ctx, workspaceID, teamID, userID, domain.RoleMember)

	assert.NoError(t, err)
	assert.True(t, deleted)
	repo.AssertExpectations(t)
}

func TestTeamService_Leave_DeletesWhenWorkspaceOwner(t *testing.T) {
	repo := new(mockTeamRepo)
	svc := NewTeamService(repo, new(mockTeamStatusRepo))
	ctx := context.Background()
	workspaceID := uuid.New()
	teamID := uuid.New()
	userID := uuid.New()

	repo.On("GetByID", ctx, teamID).Return(&domain.Team{ID: teamID, WorkspaceID: workspaceID}, nil)
	repo.On("Delete", ctx, teamID).Return(nil)

	deleted, err := svc.Leave(ctx, workspaceID, teamID, userID, domain.RoleOwner)

	assert.NoError(t, err)
	assert.True(t, deleted)
	repo.AssertNotCalled(t, "GetMember", ctx, teamID, userID)
	repo.AssertExpectations(t)
}

func TestTeamService_Leave_NotMember(t *testing.T) {
	repo := new(mockTeamRepo)
	svc := NewTeamService(repo, new(mockTeamStatusRepo))
	ctx := context.Background()
	workspaceID := uuid.New()
	teamID := uuid.New()
	userID := uuid.New()

	repo.On("GetByID", ctx, teamID).Return(&domain.Team{ID: teamID, WorkspaceID: workspaceID}, nil)
	repo.On("GetMember", ctx, teamID, userID).Return(nil, nil)

	deleted, err := svc.Leave(ctx, workspaceID, teamID, userID, domain.RoleMember)

	assert.ErrorIs(t, err, ErrTeamMemberNotFound)
	assert.False(t, deleted)
	repo.AssertExpectations(t)
}
