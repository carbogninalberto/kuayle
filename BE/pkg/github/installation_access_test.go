package github

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// fakeInstallation is one entry of GET /user/installations.
type fakeInstallation struct {
	ID         int64
	AccountID  int64
	Login      string
	TargetType string
}

// fakeGitHubUser is the account behind the OAuth grant: its identity and the
// organization memberships GitHub reports for it, keyed by organization login.
type fakeGitHubUser struct {
	ID          int64
	Login       string
	Memberships map[string]string
}

const (
	testUserID = int64(501)
	testOrgID  = int64(9001)
)

func userInstallation(id, accountID int64, login string) fakeInstallation {
	return fakeInstallation{ID: id, AccountID: accountID, Login: login, TargetType: "User"}
}

func orgInstallation(id, accountID int64, login string) fakeInstallation {
	return fakeInstallation{ID: id, AccountID: accountID, Login: login, TargetType: "Organization"}
}

func membershipBody(orgID int64, org, role, state string) string {
	body, _ := json.Marshal(map[string]any{
		"role": role, "state": state,
		"organization": map[string]any{"id": orgID, "login": org},
		"user":         map[string]any{"id": testUserID, "login": "octo"},
	})
	return string(body)
}

func installationAccessTransport(t *testing.T, user fakeGitHubUser, installationPages [][]fakeInstallation) http.RoundTripper {
	return commentTransport(func(r *http.Request) (*http.Response, error) {
		target := r.URL.Host + r.URL.Path
		if target == "github.com/login/oauth/access_token" {
			require.Equal(t, http.MethodPost, r.Method)
			require.NoError(t, r.ParseForm())
			require.Equal(t, "client-id", r.PostForm.Get("client_id"))
			require.Equal(t, "client-secret", r.PostForm.Get("client_secret"))
			if r.PostForm.Get("code") != "good-code" {
				return commentResponse(200, `{"error":"bad_verification_code"}`), nil
			}
			return commentResponse(200, `{"access_token":"user-token","token_type":"bearer"}`), nil
		}
		require.Equal(t, "api.github.com", r.URL.Host, "every lookup must go to the fixed GitHub API host")
		require.Equal(t, http.MethodGet, r.Method)
		require.Equal(t, "Bearer user-token", r.Header.Get("Authorization"))
		switch {
		case r.URL.Path == "/user/installations":
			page := 1
			if p := r.URL.Query().Get("page"); p == "2" {
				page = 2
			}
			items := []map[string]any{}
			if page <= len(installationPages) {
				for _, inst := range installationPages[page-1] {
					items = append(items, map[string]any{
						"id":          inst.ID,
						"target_type": inst.TargetType,
						"target_id":   inst.AccountID,
						"account":     map[string]any{"id": inst.AccountID, "login": inst.Login, "type": inst.TargetType},
					})
				}
			}
			body, _ := json.Marshal(map[string]any{"total_count": 0, "installations": items})
			return commentResponse(200, string(body)), nil
		case r.URL.Path == "/user":
			body, _ := json.Marshal(map[string]any{"id": user.ID, "login": user.Login, "type": "User"})
			return commentResponse(200, string(body)), nil
		case strings.HasPrefix(r.URL.Path, "/user/memberships/orgs/"):
			org := strings.TrimPrefix(r.URL.Path, "/user/memberships/orgs/")
			switch body := user.Memberships[org]; body {
			case "":
				return commentResponse(404, `{"message":"Not Found"}`), nil
			case "forbidden":
				return commentResponse(403, `{"message":"Resource not accessible by integration"}`), nil
			default:
				return commentResponse(200, body), nil
			}
		}
		t.Fatalf("unexpected request %s %s", r.Method, r.URL)
		return nil, nil
	})
}

// An installation_id in the setup redirect is caller-controlled. Only the
// installing user's OAuth grant proves which installation the flow created.
func TestUserControlsInstallation(t *testing.T) {
	full := make([]fakeInstallation, 100)
	for i := range full {
		full[i] = userInstallation(int64(1000+i), testUserID, "octo")
	}
	user := fakeGitHubUser{ID: testUserID, Login: "octo"}
	client := &http.Client{Transport: installationAccessTransport(t, user, [][]fakeInstallation{full, {userInstallation(77, testUserID, "octo")}})}
	ctx := context.Background()

	ok, err := UserControlsInstallation(ctx, client, "client-id", "client-secret", "good-code", 77)
	require.NoError(t, err)
	require.True(t, ok, "installations on later pages must be found")

	ok, err = UserControlsInstallation(ctx, client, "client-id", "client-secret", "good-code", 42)
	require.NoError(t, err)
	require.False(t, ok, "another account's installation must be refused")

	_, err = UserControlsInstallation(ctx, client, "client-id", "client-secret", "stolen-code", 77)
	require.Error(t, err)
}

// GitHub lists an installation to every user who can reach one repository it
// covers. Binding it hands the workspace the installation's tokens, so only
// the account that owns the installation, or an admin of the organization
// that does, may bind it.
func TestUserControlsInstallationRequiresOwnerOrOrganizationAdmin(t *testing.T) {
	const installationID = 77
	ctx := context.Background()
	check := func(t *testing.T, user fakeGitHubUser, inst fakeInstallation) (bool, error) {
		t.Helper()
		client := &http.Client{Transport: installationAccessTransport(t, user, [][]fakeInstallation{{inst}})}
		return UserControlsInstallation(ctx, client, "client-id", "client-secret", "good-code", installationID)
	}
	member := func(memberships map[string]string) fakeGitHubUser {
		return fakeGitHubUser{ID: testUserID, Login: "octo", Memberships: memberships}
	}
	acme := orgInstallation(installationID, testOrgID, "acme")

	t.Run("organization member who is not an admin is refused", func(t *testing.T) {
		ok, err := check(t, member(map[string]string{"acme": membershipBody(testOrgID, "acme", "member", "active")}), acme)
		require.False(t, ok)
		require.NoError(t, err)
	})
	t.Run("organization admin is accepted", func(t *testing.T) {
		ok, err := check(t, member(map[string]string{"acme": membershipBody(testOrgID, "acme", "admin", "active")}), acme)
		require.NoError(t, err)
		require.True(t, ok)
	})
	t.Run("pending admin invitation is refused", func(t *testing.T) {
		ok, err := check(t, member(map[string]string{"acme": membershipBody(testOrgID, "acme", "admin", "pending")}), acme)
		require.False(t, ok)
		require.NoError(t, err)
	})
	t.Run("billing manager is refused", func(t *testing.T) {
		ok, _ := check(t, member(map[string]string{"acme": membershipBody(testOrgID, "acme", "billing_manager", "active")}), acme)
		require.False(t, ok)
	})
	t.Run("outside collaborator without a membership is refused", func(t *testing.T) {
		ok, _ := check(t, member(nil), acme)
		require.False(t, ok)
	})
	t.Run("a forbidden membership lookup fails closed", func(t *testing.T) {
		ok, err := check(t, member(map[string]string{"acme": "forbidden"}), acme)
		require.False(t, ok)
		require.Error(t, err)
	})
	t.Run("admin of another organization with the same login text is refused", func(t *testing.T) {
		// The login was renamed and reused; the numeric id is what identifies the account.
		ok, _ := check(t, member(map[string]string{"acme": membershipBody(testOrgID+1, "acme", "admin", "active")}), acme)
		require.False(t, ok)
	})
	t.Run("user installation bound by its own account is accepted", func(t *testing.T) {
		ok, err := check(t, member(nil), userInstallation(installationID, testUserID, "octo"))
		require.NoError(t, err)
		require.True(t, ok)
	})
	t.Run("user installation bound by another user is refused", func(t *testing.T) {
		// A collaborator on one of the owner's repositories sees the installation.
		ok, err := check(t, member(nil), userInstallation(installationID, testUserID+1, "someone-else"))
		require.NoError(t, err)
		require.False(t, ok)
	})
	t.Run("user installation with a matching login but another id is refused", func(t *testing.T) {
		ok, _ := check(t, member(nil), userInstallation(installationID, testUserID+1, "octo"))
		require.False(t, ok)
	})
	t.Run("installation without an account id is refused", func(t *testing.T) {
		ok, _ := check(t, member(nil), userInstallation(installationID, 0, "octo"))
		require.False(t, ok)
	})
	t.Run("enterprise installation is refused", func(t *testing.T) {
		ok, _ := check(t, member(nil), fakeInstallation{ID: installationID, AccountID: 7, Login: "big-co", TargetType: "Enterprise"})
		require.False(t, ok)
	})
}
