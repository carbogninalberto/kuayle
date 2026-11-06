package validate

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestOperatorBaseURL(t *testing.T) {
	for raw, want := range map[string]string{
		"http://searxng:8080":                  "http://searxng:8080",
		" http://SearXNG:8080/ ":               "http://searxng:8080",
		"https://search.example.com/searxng/":  "https://search.example.com/searxng",
		"https://10.0.0.5":                     "https://10.0.0.5",
		"http://[fd00::5]:8888/a-b_c.d~e/":     "http://[fd00::5]:8888/a-b_c.d~e",
		"HTTP://searxng:8080":                  "http://searxng:8080",
		"https://search.example.com/v1/search": "https://search.example.com/v1/search",
	} {
		parsed, err := OperatorBaseURL(raw)
		require.NoError(t, err, raw)
		require.Equal(t, want, parsed.String(), raw)
	}
	for _, raw := range []string{
		"",
		"searxng:8080",
		"ftp://searxng",
		"file:///etc/passwd",
		"http://",
		"http://:8080",
		"http://user:pass@searxng:8080",
		"http://user@searxng",
		"http://searxng:8080?format=json",
		"http://searxng:8080/?",
		"http://searxng:8080#x",
		"http://searxng:8080/../admin",
		"http://searxng:8080/a/./b",
		"http://searxng:8080/a//b",
		"http://searxng:8080/%2e%2e/admin",
		"http://searxng:8080/a%2fb",
		"http://searxng:8080/a b",
		"http://searxng:0",
		"http://searxng:65536",
		"http://searxng:http",
		"http://searxng\\@evil.example",
		"//searxng:8080",
	} {
		_, err := OperatorBaseURL(raw)
		require.Error(t, err, raw)
	}
}
