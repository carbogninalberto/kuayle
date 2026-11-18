package service

import (
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"os"
	"testing"

	"github.com/kuayle/kuayle-backend/internal/dto"
	"github.com/stretchr/testify/require"
	"golang.org/x/net/html"
)

// Run against the development realm imported by the sso Compose profile.
func TestSAMLKeycloak(t *testing.T) {
	base := os.Getenv("KUAYLE_TEST_KEYCLOAK_URL")
	if base == "" {
		t.Skip("requires the Keycloak dev realm")
	}
	rig := newSSOTestRig(t)
	rig.service.frontendURL = "http://localhost:5173"
	_, err := rig.service.SaveConfig(t.Context(), "acme", dto.SSOConfigRequest{Protocol: "saml", MetadataURL: base + "/realms/kuayle/protocol/saml/descriptor", Enabled: true, DefaultRole: "member"})
	require.NoError(t, err)
	target, bind, err := rig.service.Start(t.Context(), "acme", "")
	require.NoError(t, err)
	jar, err := cookiejar.New(nil)
	require.NoError(t, err)
	client := &http.Client{Jar: jar, Timeout: rig.service.httpClient.Timeout}
	login, err := client.Get(target)
	require.NoError(t, err)
	loginURL, fields := keycloakForm(t, login)
	fields.Set("username", "demo")
	fields.Set("password", "demo")
	result, err := client.PostForm(loginURL, fields)
	require.NoError(t, err)
	acs, proof := keycloakForm(t, result)
	require.Equal(t, rig.service.redirectURI("acme"), acs)
	user, _, _, _, err := rig.service.SAMLCallback(t.Context(), "acme", proof.Get("RelayState"), proof.Get("SAMLResponse"), bind)
	require.NoError(t, err)
	require.Equal(t, "demo@example.com", user.Email)
}

func keycloakForm(t *testing.T, resp *http.Response) (string, url.Values) {
	t.Helper()
	defer resp.Body.Close()
	require.Equal(t, http.StatusOK, resp.StatusCode)
	tokenizer := html.NewTokenizer(resp.Body)
	action := ""
	fields := url.Values{}
	for {
		kind := tokenizer.Next()
		if kind == html.ErrorToken {
			break
		}
		if kind != html.StartTagToken && kind != html.SelfClosingTagToken {
			continue
		}
		token := tokenizer.Token()
		attrs := map[string]string{}
		for _, attr := range token.Attr {
			attrs[attr.Key] = attr.Val
		}
		switch token.Data {
		case "form":
			action = attrs["action"]
		case "input":
			if attrs["name"] != "" {
				fields.Set(attrs["name"], attrs["value"])
			}
		}
	}
	require.NotEmpty(t, action)
	return action, fields
}
