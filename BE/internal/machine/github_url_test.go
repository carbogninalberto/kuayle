package machine

import "testing"

func TestSafeGitHubPullRequestURL(t *testing.T) {
	tests := []struct {
		name       string
		value      string
		repository string
		want       bool
	}{
		{name: "valid", value: "https://github.com/carbogninalberto/kuayle/pull/46", repository: "carbogninalberto/kuayle", want: true},
		{name: "case insensitive repository", value: "https://github.com/Carbogninalberto/Kuayle/pull/46", repository: "carbogninalberto/kuayle", want: true},
		{name: "http", value: "http://github.com/carbogninalberto/kuayle/pull/46", repository: "carbogninalberto/kuayle"},
		{name: "lookalike host", value: "https://github.com.example.com/carbogninalberto/kuayle/pull/46", repository: "carbogninalberto/kuayle"},
		{name: "userinfo", value: "https://github.com@evil.example/carbogninalberto/kuayle/pull/46", repository: "carbogninalberto/kuayle"},
		{name: "other repository", value: "https://github.com/attacker/kuayle/pull/46", repository: "carbogninalberto/kuayle"},
		{name: "repository prefix", value: "https://github.com/carbogninalberto/kuayle-malicious/pull/46", repository: "carbogninalberto/kuayle"},
		{name: "non pull request", value: "https://github.com/carbogninalberto/kuayle/issues/46", repository: "carbogninalberto/kuayle"},
		{name: "invalid pull request number", value: "https://github.com/carbogninalberto/kuayle/pull/zero", repository: "carbogninalberto/kuayle"},
		{name: "pull request subpage", value: "https://github.com/carbogninalberto/kuayle/pull/46/files", repository: "carbogninalberto/kuayle"},
		{name: "query", value: "https://github.com/carbogninalberto/kuayle/pull/46?redirect=https://evil.example", repository: "carbogninalberto/kuayle"},
		{name: "malformed repository", value: "https://github.com/carbogninalberto/kuayle/pull/46", repository: "carbogninalberto/kuayle/extra"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := safeGitHubPullRequestURL(tt.value, tt.repository)
			if ok != tt.want {
				t.Fatalf("safeGitHubPullRequestURL() ok = %v, want %v", ok, tt.want)
			}
			if tt.want && got != tt.value {
				t.Fatalf("safeGitHubPullRequestURL() = %q, want %q", got, tt.value)
			}
			if !tt.want && got != "" {
				t.Fatalf("safeGitHubPullRequestURL() returned rejected URL %q", got)
			}
		})
	}
}
