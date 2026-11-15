package validate

import "testing"

func TestSharedOIDCIssuer(t *testing.T) {
	for issuer, want := range map[string]bool{
		"https://accounts.google.com":                          true,
		"https://accounts.google.com/":                         true,
		"HTTPS://Accounts.Google.com":                          true,
		"https://login.microsoftonline.com/common/v2.0":        true,
		"https://login.microsoftonline.com/organizations/v2.0": true,
		"https://login.microsoftonline.com/consumers":          true,
		"https://login.microsoftonline.com/consumers/v2.0/":    true,
		"https://login.microsoftonline.com/0f1e2d3c/v2.0":      false,
		"https://keycloak.example.com/realms/kuayle":           false,
		"https://accounts.google.com.evil.example":             false,
		"": false,
	} {
		if got := SharedOIDCIssuer(issuer); got != want {
			t.Errorf("SharedOIDCIssuer(%q) = %v, want %v", issuer, got, want)
		}
	}
}
