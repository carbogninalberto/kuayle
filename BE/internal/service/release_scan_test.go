package service

import (
	"strings"
	"testing"

	gh "github.com/kuayle/kuayle-backend/pkg/github"
	"github.com/stretchr/testify/require"
)

// Fake credentials are assembled at run time so this file itself never
// contains a value a secret scanner would flag.
var (
	fakeGitHubToken = "gh" + "p_" + strings.Repeat("A1b2", 9)
	fakeAWSKey      = "AK" + "IA" + strings.Repeat("Q", 16)
	fakeKeyHeader   = "-----BEGIN " + "OPENSSH PRIVATE" + " KEY-----"
	fakeCredURL     = "postgres://kuayle:" + "Zq8" + "vWx41@db.example.com/app"
)

func TestReleaseScanFindsCredentialsOnAddedLinesOnly(t *testing.T) {
	files := []gh.CompareFile{
		{Filename: "BE/config.go", Status: "modified", Patch: "@@ -10,3 +10,4 @@ func load() {\n context\n-old := \"" + fakeGitHubToken + "\"\n+token := \"" + fakeGitHubToken + "\"\n+fine := 1\n"},
		{Filename: "deploy/key.txt", Status: "added", Patch: "@@ -0,0 +1,2 @@\n+" + fakeKeyHeader + "\n+abc\n"},
		{Filename: "scripts/aws.sh", Status: "modified", Patch: "@@ -1 +1,2 @@\n x\n+export KEY=" + fakeAWSKey + "\n"},
		{Filename: "docs/db.md", Status: "modified", Patch: "@@ -1 +1 @@\n+connect with " + fakeCredURL + "\n"},
		// Removing a secret is what a fix looks like, so removed files and
		// removed lines are never findings.
		{Filename: "old/secret.go", Status: "removed", Patch: "@@ -1 +0,0 @@\n-" + fakeGitHubToken + "\n"},
		{Filename: "README.md", Status: "modified", Patch: "@@ -1 +1 @@\n+Use postgres://user:" + "${DB_PASSWORD}" + "@host/db or postgres://<user>:" + "<password>" + "@host\n"},
	}
	findings, unscanned := scanReleaseFiles(files)
	require.Empty(t, unscanned)
	require.Equal(t, []secretFinding{
		{File: "BE/config.go", Line: 11, Kind: "GitHub token"},
		{File: "deploy/key.txt", Line: 1, Kind: "private key"},
		{File: "scripts/aws.sh", Line: 2, Kind: "AWS access key ID"},
		{File: "docs/db.md", Line: 1, Kind: "URL with embedded credentials"},
	}, findings)
	reason := secretRefusal("the release range", findings)
	require.Contains(t, reason, "BE/config.go line 11 (GitHub token)")
	require.Contains(t, reason, "deploy/key.txt line 1 (private key)")
	for _, value := range []string{fakeGitHubToken, fakeAWSKey, fakeKeyHeader, "Zq8vWx41"} {
		require.NotContains(t, reason, value, "a refusal names the file and type, never the value")
	}
}

func TestReleaseScanFlagsSensitiveFilesAndReportsUnscannedOnes(t *testing.T) {
	files := []gh.CompareFile{
		{Filename: "deploy/.env", Status: "added"},
		{Filename: "certs/server.pem", Status: "modified"},
		{Filename: ".env.example", Status: "added", Patch: "@@ -0,0 +1 @@\n+DB_PASSWORD=\n"},
		{Filename: "UI/static/logo.png", Status: "added"},
		{Filename: "renamed.go", Status: "renamed"},
	}
	findings, unscanned := scanReleaseFiles(files)
	require.Equal(t, []secretFinding{
		{File: "deploy/.env", Kind: "sensitive file (.env)"},
		{File: "certs/server.pem", Kind: "sensitive file (server.pem)"},
	}, findings)
	require.Equal(t, []string{"UI/static/logo.png"}, unscanned, "a changed file without a diff cannot be scanned; a pure rename has no new content")
	require.Contains(t, secretRefusal("the release range", findings), "deploy/.env (sensitive file (.env))")
}

func TestReleaseNotesCheckRefusesSecretsPrivateHostsAndLocalPaths(t *testing.T) {
	ok := "### Features\n- **Releases from chat (KUA-88).** See https://github.com/acme/app/pull/7 and docs at https://kuayle.com.\n- Version 1.2.3 is a version, and 8.8.8.8 is a public address."
	require.NoError(t, checkReleaseNotes(ok))

	for notes, want := range map[string]string{
		"- token " + fakeGitHubToken:                      "line 1 (GitHub token)",
		"ok\n- deployed to 10.0.12.5 behind the VPN":      "line 2 (private address)",
		"- see http://192.168.1.20:8080/admin":            "line 1 (private address)",
		"- reach the API at api.prod.internal":            "line 1 (private host name)",
		"- logs are in /Users/alice/work/kuayle/logs":     "line 1 (local path)",
		"- run from /home/deploy/kuayle":                  "line 1 (local path)",
		"- our build box ci-runner.corp has the cache":    "line 1 (private host name)",
		"- loopback 127.0.0.1 is used by the healthcheck": "line 1 (private address)",
	} {
		err := checkReleaseNotes(notes)
		require.Error(t, err, notes)
		require.Contains(t, toolErrorText(err), want, notes)
		require.Contains(t, toolErrorText(err), "release notes", notes)
		require.NotContains(t, toolErrorText(err), fakeGitHubToken)
		require.NotContains(t, toolErrorText(err), "10.0.12.5")
	}
	require.ErrorContains(t, checkReleaseNotes("   "), "empty")
	require.ErrorContains(t, checkReleaseNotes(strings.Repeat("a", maxReleaseNotesRunes+1)), "at most")
}
