package service

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/kuayle/kuayle-backend/internal/dto"
	"github.com/kuayle/kuayle-backend/pkg/validate"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
)

func TestTeamService_Delete(t *testing.T) {
	repo := new(mockTeamRepo)
	svc := NewTeamService(repo, new(mockTeamStatusRepo))
	ctx := context.Background()
	workspaceID := uuid.New()
	teamID := uuid.New()

	repo.On("GetByID", ctx, teamID).Return(&domain.Team{ID: teamID, WorkspaceID: workspaceID}, nil)
	repo.On("Delete", ctx, teamID).Return(nil, nil)

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
	repo.On("Delete", ctx, teamID).Return(nil, nil)

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
	repo.On("Delete", ctx, teamID).Return(nil, nil)

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

func TestPrivateTeamLeaveNeverDeletesContent(t *testing.T) {
	for _, role := range []string{domain.RoleOwner, domain.RoleAdmin, domain.RoleMember, domain.RoleGuest} {
		t.Run(role, func(t *testing.T) {
			repo := new(mockTeamRepo)
			svc := NewTeamService(repo, new(mockTeamStatusRepo))
			ctx := context.Background()
			workspace, team, user := uuid.New(), uuid.New(), uuid.New()
			repo.On("GetByID", ctx, team).Return(&domain.Team{ID: team, WorkspaceID: workspace, IsPrivate: true}, nil)
			repo.On("GetMember", ctx, team, user).Return(&domain.TeamMember{TeamID: team, UserID: user}, nil)
			repo.On("RemoveMember", ctx, team, user).Return(nil)
			deleted, err := svc.Leave(ctx, workspace, team, user, role)
			assert.NoError(t, err)
			assert.False(t, deleted)
			repo.AssertNotCalled(t, "Delete", ctx, team)
			repo.AssertExpectations(t)
		})
	}
}

type rekeyTeamRepo struct {
	*mockTeamRepo
	newKey string
}

func (r *rekeyTeamRepo) UpdateWithKey(_ context.Context, team *domain.Team, newKey string) error {
	r.newKey = newKey
	team.Key = newKey
	return nil
}

func TestTeamService_Update_ChangesKeyThroughTheAtomicPath(t *testing.T) {
	base := new(mockTeamRepo)
	repo := &rekeyTeamRepo{mockTeamRepo: base}
	svc := NewTeamService(repo, new(mockTeamStatusRepo))
	ctx := context.Background()
	teamID := uuid.New()
	base.On("GetByID", ctx, teamID).Return(&domain.Team{ID: teamID, WorkspaceID: uuid.New(), Key: "ENG"}, nil)
	key := "PLT"

	team, err := svc.Update(ctx, teamID, dto.UpdateTeamRequest{Key: &key})

	assert.NoError(t, err)
	assert.Equal(t, "PLT", team.Key)
	assert.Equal(t, "PLT", repo.newKey)
	base.AssertNotCalled(t, "Update", ctx, team)
}

func TestTeamService_Update_SameKeyIsNotARename(t *testing.T) {
	repo := new(mockTeamRepo)
	svc := NewTeamService(repo, new(mockTeamStatusRepo))
	ctx := context.Background()
	teamID := uuid.New()
	repo.On("GetByID", ctx, teamID).Return(&domain.Team{ID: teamID, WorkspaceID: uuid.New(), Key: "ENG"}, nil)
	repo.On("Update", ctx, mock.AnythingOfType("*domain.Team")).Return(nil)
	key := "ENG"

	_, err := svc.Update(ctx, teamID, dto.UpdateTeamRequest{Key: &key})

	assert.NoError(t, err)
	repo.AssertExpectations(t)
}

func TestUpdateTeamRequest_KeyRules(t *testing.T) {
	for key, valid := range map[string]bool{"PLT": true, "A": true, "ABCDEFGHIJ": true, "plt": false, "PL1": false, "": false, "ABCDEFGHIJK": false, "A-B": false} {
		k := key
		err := validate.Struct(&dto.UpdateTeamRequest{Key: &k})
		assert.Equal(t, valid, err == nil, "key %q", key)
	}
	assert.NoError(t, validate.Struct(&dto.UpdateTeamRequest{}))
}
