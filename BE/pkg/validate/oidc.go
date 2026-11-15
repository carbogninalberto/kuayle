package validate

import "strings"

// sharedOIDCIssuers are identity providers that sign in accounts of any
// organisation. Without an allowed-domain list they would admit anyone.
var sharedOIDCIssuers = map[string]bool{
	"https://accounts.google.com":                     true,
	"https://login.microsoftonline.com/common":        true,
	"https://login.microsoftonline.com/organizations": true,
	"https://login.microsoftonline.com/consumers":     true,
}

// SharedOIDCIssuer reports whether issuer is a multi-tenant identity provider
// (Google, Microsoft Entra common/organizations/consumers). The comparison
// ignores case, a trailing slash and a trailing /v2.0.
func SharedOIDCIssuer(issuer string) bool {
	normalized := strings.TrimRight(strings.ToLower(strings.TrimSpace(issuer)), "/")
	normalized = strings.TrimRight(strings.TrimSuffix(normalized, "/v2.0"), "/")
	return sharedOIDCIssuers[normalized]
}
