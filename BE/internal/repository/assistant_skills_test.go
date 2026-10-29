package repository

import (
	"context"
	"os"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jmoiron/sqlx"
	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/stretchr/testify/require"
)

type skillFixture struct {
	db                                         *sqlx.DB
	ws, other                                  uuid.UUID
	owner, admin, member, teamMember, outsider uuid.UUID
	privateTeam, publicTeam                    uuid.UUID
}

func newSkillFixture(t *testing.T) skillFixture {
	t.Helper()
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		t.Skip("DATABASE_URL is not set")
	}
	db, err := Open(dsn)
	require.NoError(t, err)
	t.Cleanup(func() { db.Close() })
	f := skillFixture{db: db, ws: uuid.New(), other: uuid.New(), owner: uuid.New(), admin: uuid.New(), member: uuid.New(), teamMember: uuid.New(), outsider: uuid.New(), privateTeam: uuid.New(), publicTeam: uuid.New()}
	for _, id := range []uuid.UUID{f.owner, f.admin, f.member, f.teamMember, f.outsider} {
		_, err = db.Exec(`INSERT INTO users(id,email,name,password_hash) VALUES($1,$2,'Skills','hash')`, id, id.String()+"@test.invalid")
		require.NoError(t, err)
		t.Cleanup(func() { _, _ = db.Exec(`DELETE FROM users WHERE id=$1`, id) })
	}
	for _, id := range []uuid.UUID{f.ws, f.other} {
		_, err = db.Exec(`INSERT INTO workspaces(id,name,slug,owner_id) VALUES($1,'Skills',$2,$3)`, id, id.String(), f.owner)
		require.NoError(t, err)
		t.Cleanup(func() { _, _ = db.Exec(`DELETE FROM workspaces WHERE id=$1`, id) })
	}
	for id, role := range map[uuid.UUID]string{f.owner: "owner", f.admin: "admin", f.member: "member", f.teamMember: "member"} {
		_, err = db.Exec(`INSERT INTO workspace_members(workspace_id,user_id,role) VALUES($1,$2,$3)`, f.ws, id, role)
		require.NoError(t, err)
	}
	_, err = db.Exec(`INSERT INTO workspace_members(workspace_id,user_id,role) VALUES($1,$2,'admin')`, f.other, f.outsider)
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO teams(id,workspace_id,name,key,is_private) VALUES($1,$2,'Secret','SEC',true),($3,$2,'Open','OPN',false)`, f.privateTeam, f.ws, f.publicTeam)
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO team_members(team_id,user_id) VALUES($1,$2)`, f.privateTeam, f.teamMember)
	require.NoError(t, err)
	return f
}

func (f skillFixture) as(user, ws uuid.UUID) context.Context {
	return domain.WithAccess(context.Background(), domain.Access{WorkspaceID: ws, UserID: user})
}

func skillInput(name, status string, team *uuid.UUID) domain.AssistantSkillInput {
	return domain.AssistantSkillInput{TeamID: team, Name: name, Description: "When releasing", Body: "1. Tag\n2. Ship", Status: status, AuthorKind: "user"}
}

func skillNames(skills []domain.AssistantSkill) []string {
	names := []string{}
	for _, skill := range skills {
		names = append(names, skill.Name)
	}
	return names
}

// Only current owners and admins write skills; names are unique per scope
// ignoring case; each workspace has its own.
func TestAssistantSkillWritesAreForAdmins(t *testing.T) {
	f := newSkillFixture(t)
	repo := NewAssistantSkillRepository(f.db)
	admin := f.as(f.admin, f.ws)

	skill, err := repo.Create(admin, f.ws, f.admin, skillInput("How we release", "active", nil))
	require.NoError(t, err)
	require.Equal(t, "active", skill.Status)
	require.Equal(t, 1, skill.CurrentVersion)
	require.Equal(t, 1, skill.LatestVersion)
	require.Nil(t, skill.PendingVersion)
	require.Equal(t, "1. Tag\n2. Ship", skill.Body)
	require.Equal(t, f.admin, *skill.CreatedByUserID)

	for _, user := range []uuid.UUID{f.member, f.teamMember, f.outsider} {
		ctx := f.as(user, f.ws)
		_, err = repo.Create(ctx, f.ws, user, skillInput("Other", "active", nil))
		require.ErrorIs(t, err, ErrAssistantSkillDenied)
		_, err = repo.Update(ctx, f.ws, skill.ID, user, skillInput("Changed", "", nil))
		require.ErrorIs(t, err, ErrAssistantSkillDenied)
		_, err = repo.SetEnabled(ctx, f.ws, skill.ID, user, false)
		require.ErrorIs(t, err, ErrAssistantSkillDenied)
		_, err = repo.Activate(ctx, f.ws, skill.ID, user, nil)
		require.ErrorIs(t, err, ErrAssistantSkillDenied)
		_, err = repo.Restore(ctx, f.ws, skill.ID, user, 1)
		require.ErrorIs(t, err, ErrAssistantSkillDenied)
		require.ErrorIs(t, repo.Delete(ctx, f.ws, skill.ID, user), ErrAssistantSkillDenied)
		_, _, err = repo.Propose(ctx, f.ws, user, skillInput("Draft", "", nil))
		require.ErrorIs(t, err, ErrAssistantSkillDenied)
	}

	_, err = repo.Create(admin, f.ws, f.admin, skillInput("how WE release", "active", nil))
	require.ErrorIs(t, err, ErrAssistantSkillNameTaken, "names are unique ignoring case")
	team, err := repo.Create(admin, f.ws, f.admin, skillInput("How we release", "active", &f.publicTeam))
	require.NoError(t, err, "a team may have its own skill with the same name")
	require.Equal(t, "OPN", *team.TeamKey)
	_, err = repo.Create(admin, f.ws, f.admin, skillInput("Elsewhere", "active", &uuid.Nil))
	require.ErrorIs(t, err, ErrAssistantSkillNotFound)

	// An admin of another workspace cannot reach this one's skills or teams.
	outside := f.as(f.outsider, f.other)
	_, err = repo.Get(outside, f.ws, skill.ID)
	require.ErrorIs(t, err, ErrAssistantSkillNotFound)
	_, err = repo.Create(outside, f.other, f.outsider, skillInput("Cross", "active", &f.publicTeam))
	require.ErrorIs(t, err, ErrAssistantSkillNotFound, "teams of another workspace are refused")
	_, err = repo.Update(outside, f.other, skill.ID, f.outsider, skillInput("Changed", "", nil))
	require.ErrorIs(t, err, ErrAssistantSkillNotFound)
	list, err := repo.List(outside, f.other)
	require.NoError(t, err)
	require.Empty(t, list)

	// A demoted admin loses writing in the transaction that writes.
	_, err = f.db.Exec(`UPDATE workspace_members SET role='member' WHERE workspace_id=$1 AND user_id=$2`, f.ws, f.admin)
	require.NoError(t, err)
	_, err = repo.SetEnabled(admin, f.ws, skill.ID, f.admin, false)
	require.ErrorIs(t, err, ErrAssistantSkillDenied)

	owner := f.as(f.owner, f.ws)
	require.NoError(t, repo.Delete(owner, f.ws, skill.ID, f.owner))
	_, err = repo.Get(owner, f.ws, skill.ID)
	require.ErrorIs(t, err, ErrAssistantSkillNotFound)
	var versions int
	require.NoError(t, f.db.Get(&versions, `SELECT count(*) FROM assistant_skill_versions WHERE skill_id=$1`, skill.ID))
	require.Zero(t, versions, "versions go with their skill")
}

// Edits are versions: a draft edit of an active skill waits for activation,
// restoring copies an old version forward, and the assistant only drafts.
func TestAssistantSkillVersionsDraftsAndRestore(t *testing.T) {
	f := newSkillFixture(t)
	repo := NewAssistantSkillRepository(f.db)
	ctx := f.as(f.owner, f.ws)

	skill, err := repo.Create(ctx, f.ws, f.owner, skillInput("Release", "active", nil))
	require.NoError(t, err)
	edit := skillInput("Release", "draft", nil)
	edit.Body = "1. Tag\n2. Ship\n3. Announce"
	drafted, err := repo.Update(ctx, f.ws, skill.ID, f.owner, edit)
	require.NoError(t, err)
	require.Equal(t, "active", drafted.Status)
	require.Equal(t, 1, drafted.CurrentVersion)
	require.Equal(t, "1. Tag\n2. Ship", drafted.Body, "the active content is unchanged")
	require.Equal(t, 2, *drafted.PendingVersion)

	activated, err := repo.Activate(ctx, f.ws, skill.ID, f.owner, nil)
	require.NoError(t, err)
	require.Equal(t, 2, activated.CurrentVersion)
	require.Equal(t, "1. Tag\n2. Ship\n3. Announce", activated.Body)
	require.Nil(t, activated.PendingVersion)
	_, err = repo.Activate(ctx, f.ws, skill.ID, f.owner, nil)
	require.ErrorIs(t, err, ErrAssistantSkillNotDraft)
	one := 1
	_, err = repo.Activate(ctx, f.ws, skill.ID, f.owner, &one)
	require.ErrorIs(t, err, ErrAssistantSkillNotDraft, "only drafts are activated; old versions are restored")

	restored, err := repo.Restore(ctx, f.ws, skill.ID, f.owner, 1)
	require.NoError(t, err)
	require.Equal(t, 3, restored.CurrentVersion)
	require.Equal(t, "1. Tag\n2. Ship", restored.Body)
	_, err = repo.Restore(ctx, f.ws, skill.ID, f.owner, 9)
	require.ErrorIs(t, err, ErrAssistantSkillNotFound)

	// The assistant's edit of an active skill is a new draft version.
	proposal := skillInput("release", "", nil)
	proposal.Body = "Assistant edit"
	proposed, version, err := repo.Propose(ctx, f.ws, f.owner, proposal)
	require.NoError(t, err)
	require.Equal(t, skill.ID, proposed.ID, "the name finds the existing skill")
	require.Equal(t, 4, version)
	require.Equal(t, "active", proposed.Status)
	require.Equal(t, "1. Tag\n2. Ship", proposed.Body)
	require.Equal(t, 4, *proposed.PendingVersion)

	// A new proposal is a draft skill that is never listed for the assistant.
	fresh, version, err := repo.Propose(ctx, f.ws, f.owner, skillInput("Onboarding", "active", nil))
	require.NoError(t, err)
	require.Equal(t, 1, version)
	require.Equal(t, "draft", fresh.Status)
	require.Equal(t, "assistant", fresh.AuthorKind)
	listed, err := repo.ForAssistant(ctx, f.ws, nil, 50)
	require.NoError(t, err)
	require.Equal(t, []string{"Release"}, skillNames(listed))
	_, err = repo.Activate(ctx, f.ws, fresh.ID, f.owner, nil)
	require.NoError(t, err)

	versions, err := repo.Versions(ctx, f.ws, skill.ID)
	require.NoError(t, err)
	require.Len(t, versions, 4)
	require.Equal(t, 4, versions[0].Version)
	require.Equal(t, "draft", versions[0].Status)
	require.Equal(t, "assistant", versions[0].AuthorKind)
	require.Equal(t, "active", versions[1].Status)
	require.Equal(t, "user", versions[1].AuthorKind)

	// Disabled skills stay listed in settings but not for the assistant.
	_, err = repo.SetEnabled(ctx, f.ws, skill.ID, f.owner, false)
	require.NoError(t, err)
	listed, err = repo.ForAssistant(ctx, f.ws, nil, 50)
	require.NoError(t, err)
	require.Equal(t, []string{"Onboarding"}, skillNames(listed))
	all, err := repo.List(ctx, f.ws)
	require.NoError(t, err)
	require.Len(t, all, 2)
	require.Empty(t, all[0].Body, "lists leave bodies out")

	large := skillInput("Large", "active", nil)
	large.Body = strings.Repeat("x", 65537)
	_, err = repo.Create(ctx, f.ws, f.owner, large)
	require.Error(t, err, "the body is capped at 64 KiB in the database too")
}

// Skills of a private team are visible only to its members and workspace
// admins, in settings and to the assistant; a team chat lists only its team.
func TestAssistantSkillsFollowPrivateTeamVisibility(t *testing.T) {
	f := newSkillFixture(t)
	repo := NewAssistantSkillRepository(f.db)
	admin := f.as(f.admin, f.ws)
	_, err := repo.Create(admin, f.ws, f.admin, skillInput("Everyone", "active", nil))
	require.NoError(t, err)
	secret, err := repo.Create(admin, f.ws, f.admin, skillInput("Secret release", "active", &f.privateTeam))
	require.NoError(t, err)
	_, err = repo.Create(admin, f.ws, f.admin, skillInput("Open release", "active", &f.publicTeam))
	require.NoError(t, err)

	member, teamMember := f.as(f.member, f.ws), f.as(f.teamMember, f.ws)
	listed, err := repo.List(member, f.ws)
	require.NoError(t, err)
	require.Equal(t, []string{"Everyone", "Open release"}, skillNames(listed))
	_, err = repo.Get(member, f.ws, secret.ID)
	require.ErrorIs(t, err, ErrAssistantSkillNotFound)
	_, err = repo.Versions(member, f.ws, secret.ID)
	require.ErrorIs(t, err, ErrAssistantSkillNotFound)
	listed, err = repo.ForAssistant(member, f.ws, nil, 50)
	require.NoError(t, err)
	require.Equal(t, []string{"Open release", "Everyone"}, skillNames(listed), "team skills first")
	loaded, err := repo.LoadForAssistant(member, f.ws, nil, "secret RELEASE", "")
	require.NoError(t, err)
	require.Empty(t, loaded)

	listed, err = repo.List(teamMember, f.ws)
	require.NoError(t, err)
	require.Equal(t, []string{"Everyone", "Open release", "Secret release"}, skillNames(listed))
	loaded, err = repo.LoadForAssistant(teamMember, f.ws, nil, "secret RELEASE", "sec")
	require.NoError(t, err)
	require.Len(t, loaded, 1)
	require.Equal(t, "1. Tag\n2. Ship", loaded[0].Body)

	// A chat scoped to the private team lists its skills and workspace ones only.
	listed, err = repo.ForAssistant(teamMember, f.ws, &f.privateTeam, 50)
	require.NoError(t, err)
	require.Equal(t, []string{"Secret release", "Everyone"}, skillNames(listed))
	loaded, err = repo.LoadForAssistant(teamMember, f.ws, &f.privateTeam, "Open release", "")
	require.NoError(t, err)
	require.Empty(t, loaded, "another team's skill is not loaded in a team chat")

	// Leaving the team hides its skills at once.
	_, err = f.db.Exec(`DELETE FROM team_members WHERE team_id=$1 AND user_id=$2`, f.privateTeam, f.teamMember)
	require.NoError(t, err)
	_, err = repo.Get(teamMember, f.ws, secret.ID)
	require.ErrorIs(t, err, ErrAssistantSkillNotFound)
}
