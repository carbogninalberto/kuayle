package service

import (
	"encoding/json"
	"testing"

	"github.com/google/uuid"
	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/stretchr/testify/require"
)

func TestSCIMQueryParsesProviderFiltersAndPagination(t *testing.T) {
	query, err := ParseSCIMQuery("UserName EQ \"quoted\\\"@example.test\"", "-1", "200", false)
	require.NoError(t, err)
	require.Equal(t, domain.SCIMQuery{Attribute: "username", Value: "quoted\"@example.test", Start: 1, Count: 100}, query)
	query, err = ParseSCIMQuery("emails[type eq \"work\"].value eq \"member@example.test\"", "2", "0", false)
	require.NoError(t, err)
	require.Equal(t, domain.SCIMQuery{Attribute: "workemail", Value: "member@example.test", Start: 2, Count: 0}, query)
	query, err = ParseSCIMQuery("displayName eq \"Platform\"", "", "", true)
	require.NoError(t, err)
	require.Equal(t, domain.SCIMQuery{Attribute: "displayname", Value: "Platform", Start: 1, Count: 100}, query)

	for _, filter := range []string{"userName sw \"p\"", "displayName eq \"p\"", "userName eq \"p\" or userName eq \"q\""} {
		_, err := ParseSCIMQuery(filter, "", "", false)
		var failure *SCIMError
		require.ErrorAs(t, err, &failure)
		require.Equal(t, "invalidFilter", failure.Type)
	}
	_, err = ParseSCIMQuery("", "invalid", "", false)
	require.Error(t, err)
}

func TestSCIMUserPatchPreservesUnmentionedAttributes(t *testing.T) {
	user := domain.SCIMUser{
		SCIMUserProfile: domain.SCIMUserProfile{
			UserName: "member@example.test",
			Name:     domain.SCIMName{GivenName: "Sam", FamilyName: "Example"},
			Emails:   []domain.SCIMEmail{{Value: "home@example.test", Type: "home"}},
		},
		Active: true,
	}
	body := json.RawMessage("{\"schemas\":[\"" + SCIMMessage + "PatchOp\"],\"Operations\":[" +
		"{\"op\":\"Replace\",\"path\":\"active\",\"value\":\"False\"}," +
		"{\"op\":\"Replace\",\"value\":{\"displayName\":\"New display name\"}}," +
		"{\"op\":\"Replace\",\"path\":\"name.givenName\",\"value\":\"Taylor\"}," +
		"{\"op\":\"Add\",\"path\":\"emails[type eq \\\"work\\\"].value\",\"value\":\"office@example.test\"}]}")
	require.NoError(t, patchSCIMUser(&user, body))
	require.False(t, user.Active)
	require.Equal(t, "member@example.test", user.UserName)
	require.Equal(t, "New display name", user.DisplayName)
	require.Equal(t, domain.SCIMName{GivenName: "Taylor", FamilyName: "Example"}, user.Name)
	require.Equal(t, []domain.SCIMEmail{
		{Value: "home@example.test", Type: "home"},
		{Value: "office@example.test", Type: "work"},
	}, user.Emails)
}

func TestSCIMGroupPatchRemovesOnlyRequestedMembers(t *testing.T) {
	filtered, explicit, retained := uuid.New(), uuid.New(), uuid.New()
	group := domain.SCIMGroup{
		ID: uuid.New(), DisplayName: "Original",
		Members: []domain.SCIMGroupMember{{Value: filtered}, {Value: explicit}, {Value: retained}},
	}
	body := json.RawMessage("{\"schemas\":[\"" + SCIMMessage + "PatchOp\"],\"Operations\":[" +
		"{\"op\":\"replace\",\"value\":{\"displayName\":\"Platform\",\"id\":\"" + group.ID.String() + "\"}}," +
		"{\"op\":\"remove\",\"path\":\"members[value eq \\\"" + filtered.String() + "\\\"]\"}," +
		"{\"op\":\"remove\",\"path\":\"members\",\"value\":[{\"value\":\"" + explicit.String() + "\"}]}]}")
	require.NoError(t, patchSCIMGroup(&group, body))
	require.Equal(t, "Platform", group.DisplayName)
	require.Equal(t, []domain.SCIMGroupMember{{Value: retained}}, group.Members)
}
