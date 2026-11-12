package github

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// A failed check's annotations are one call and keep GitHub's own failure
// messages.
func TestFailedChecksReadTheirAnnotations(t *testing.T) {
	c := pullsClient(func(r *http.Request, _ map[string]any) (int, string) {
		require.Equal(t, "/repos/owner/repo/check-runs/7/annotations", r.URL.Path)
		return 200, `[{"annotation_level":"failure","message":"buildx failed with: ERROR: failed to solve","path":"Dockerfile","start_line":1,"end_line":2}]`
	})
	annotations, err := c.ListCheckRunAnnotations("secret", "owner", "repo", 7)
	require.NoError(t, err)
	require.Len(t, annotations, 1)
	require.Equal(t, "failure", annotations[0].Level)
	require.Contains(t, annotations[0].Message, "buildx failed")
}

// The job log endpoint redirects to a text file; Kuayle follows it and keeps
// only the end of what can be a very large log.
func TestTheJobLogKeepsItsEndAfterTheRedirect(t *testing.T) {
	log := strings.Repeat("noise\n", 40000) + "Could not resolve 'archive.ubuntu.com'\n"
	c := &Client{httpClient: &http.Client{Transport: commentTransport(func(r *http.Request) (*http.Response, error) {
		switch r.URL.Path {
		case "/repos/owner/repo/actions/jobs/42/logs":
			response := commentResponse(http.StatusFound, "")
			response.Header.Set("Location", "https://api.github.com/log")
			return response, nil
		case "/log":
			require.Equal(t, "token secret", r.Header.Get("Authorization"))
			return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(log)), Header: make(http.Header)}, nil
		}
		return commentResponse(http.StatusNotFound, "{}"), nil
	})}}
	tail, err := c.GetWorkflowJobLog(context.Background(), "secret", "owner", "repo", 42, 128)
	require.NoError(t, err)
	require.Len(t, tail, 128)
	require.Contains(t, tail, "Could not resolve 'archive.ubuntu.com'")
}
