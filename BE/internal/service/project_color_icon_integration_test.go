package service

import (
	"testing"

	"github.com/kuayle/kuayle-backend/internal/dto"
	"github.com/kuayle/kuayle-backend/internal/repository"
	"github.com/stretchr/testify/require"
)

func projStr(s string) *string { return &s }

// A project's colour and icon are saved on create, change on PATCH, clear on an
// empty string, and survive an update that does not mention them.
func TestProjectColorAndIconPostgres(t *testing.T) {
	f := newToolFixture(t)
	svc := NewProjectService(repository.NewProjectRepository(f.db))

	created, err := svc.Create(f.ctx, f.ws, dto.CreateProjectRequest{Name: "Launch", Color: projStr("#3b82f6"), Icon: projStr("Rocket")})
	require.NoError(t, err)
	require.Equal(t, "#3b82f6", *created.Color)
	require.Equal(t, "Rocket", *created.Icon)

	plain, err := svc.Create(f.ctx, f.ws, dto.CreateProjectRequest{Name: "Plain"})
	require.NoError(t, err)
	require.Nil(t, plain.Color)
	require.Nil(t, plain.Icon)

	renamed, err := svc.Update(f.ctx, f.ws, created.ID, dto.UpdateProjectRequest{Name: projStr("Launch 2")})
	require.NoError(t, err)
	require.Equal(t, "#3b82f6", *renamed.Color, "an update without colour keeps it")

	changed, err := svc.Update(f.ctx, f.ws, created.ID, dto.UpdateProjectRequest{Color: projStr("#ef4444"), Icon: projStr("emoji:🚀")})
	require.NoError(t, err)
	got, err := svc.GetByID(f.ctx, created.ID)
	require.NoError(t, err)
	require.Equal(t, "#ef4444", *got.Color)
	require.Equal(t, "emoji:🚀", *got.Icon)
	require.Equal(t, *changed.Icon, *got.Icon)

	_, err = svc.Update(f.ctx, f.ws, created.ID, dto.UpdateProjectRequest{Color: projStr(""), Icon: projStr("")})
	require.NoError(t, err)
	got, err = svc.GetByID(f.ctx, created.ID)
	require.NoError(t, err)
	require.Nil(t, got.Color)
	require.Nil(t, got.Icon)

	list, err := svc.ListByWorkspace(f.ctx, f.ws)
	require.NoError(t, err)
	require.Len(t, list, 2)
}
