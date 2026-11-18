package github

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type commentTransport func(*http.Request) (*http.Response, error)

func (f commentTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func commentResponse(code int, body string) *http.Response {
	return &http.Response{StatusCode: code, Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header)}
}

func TestCommentReconcilesPaginatedAppMarker(t *testing.T) {
	const marker = "<!-- kuayle:pr-issue:test -->"
	for _, own := range []bool{true, false} {
		t.Run(map[bool]string{true: "own app", false: "spoofed marker"}[own], func(t *testing.T) {
			gets, posts := 0, 0
			c := &Client{appID: 42, httpClient: &http.Client{Transport: commentTransport(func(r *http.Request) (*http.Response, error) {
				require.Equal(t, "Bearer secret", r.Header.Get("Authorization"))
				require.Equal(t, "/repos/owner/repo/issues/7/comments", r.URL.Path)
				if r.Method == http.MethodGet {
					gets++
					if gets == 1 {
						items := make([]map[string]any, 100)
						for i := range items {
							items[i] = map[string]any{"body": "unrelated"}
						}
						b, _ := json.Marshal(items)
						return commentResponse(200, string(b)), nil
					}
					require.Equal(t, "2", r.URL.Query().Get("page"))
					app := 43
					if own {
						app = 42
					}
					b, _ := json.Marshal([]map[string]any{{"body": marker + "\r\n\r\nold body", "user": map[string]any{"type": "Bot"}, "performed_via_github_app": map[string]any{"id": app}}})
					return commentResponse(200, string(b)), nil
				}
				posts++
				var body map[string]string
				require.NoError(t, json.NewDecoder(r.Body).Decode(&body))
				require.Equal(t, marker+"\n\nLinked issue", body["body"])
				return commentResponse(201, `{}`), nil
			})}}
			require.NoError(t, c.EnsurePullRequestComment(context.Background(), "secret", "owner/repo", 7, marker, "Linked issue"))
			require.Equal(t, 2, gets)
			if own {
				require.Zero(t, posts)
			} else {
				require.Equal(t, 1, posts)
			}
		})
	}
}

func TestCommentRecoversLostPostResponse(t *testing.T) {
	const marker = "<!-- kuayle:pr-issue:test -->"
	posts := 0
	c := &Client{appID: 42, httpClient: &http.Client{Transport: commentTransport(func(r *http.Request) (*http.Response, error) {
		if r.Method == http.MethodPost {
			posts++
			return nil, errors.New("lost response secret")
		}
		if posts == 0 {
			return commentResponse(200, `[]`), nil
		}
		b, _ := json.Marshal([]map[string]any{{"body": marker + "\n\nlink", "user": map[string]any{"type": "Bot"}, "performed_via_github_app": map[string]any{"id": 42}}})
		return commentResponse(200, string(b)), nil
	})}}
	err := c.EnsurePullRequestComment(context.Background(), "secret", "owner/repo", 7, marker, "link")
	require.Error(t, err)
	require.NotContains(t, err.Error(), "secret")
	require.NoError(t, c.EnsurePullRequestComment(context.Background(), "secret", "owner/repo", 7, marker, "link"))
	require.Equal(t, 1, posts)
}

func TestCommentFailuresNeverBlindlyPostOrExposeContent(t *testing.T) {
	for _, status := range []int{401, 403, 404, 429, 500} {
		for _, method := range []string{http.MethodGet, http.MethodPost} {
			posts := 0
			c := &Client{appID: 42, httpClient: &http.Client{Transport: commentTransport(func(r *http.Request) (*http.Response, error) {
				if r.Method == http.MethodPost {
					posts++
				}
				if r.Method == method {
					return commentResponse(status, "secret private content"), nil
				}
				return commentResponse(200, `[]`), nil
			})}}
			err := c.EnsurePullRequestComment(context.Background(), "secret", "owner/repo", 7, "marker", "link")
			require.Error(t, err)
			require.NotContains(t, err.Error(), "secret")
			if method == http.MethodGet {
				require.Zero(t, posts)
			} else {
				require.Equal(t, 1, posts)
			}
		}
	}
}

func TestCommentDoesNotTrustSameAppHumanAuthor(t *testing.T) {
	posts := 0
	c := &Client{appID: 42, httpClient: &http.Client{Transport: commentTransport(func(r *http.Request) (*http.Response, error) {
		if r.Method == http.MethodGet {
			return commentResponse(200, `[{"body":"marker\n\nlink","user":{"type":"User"},"performed_via_github_app":{"id":42}}]`), nil
		}
		posts++
		return commentResponse(201, `{}`), nil
	})}}
	require.NoError(t, c.EnsurePullRequestComment(context.Background(), "secret", "owner/repo", 7, "marker", "link"))
	require.Equal(t, 1, posts)
}

func TestInstallationTokenHonorsCommentDeadline(t *testing.T) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)
	c := &Client{appID: 42, privateKey: key, httpClient: &http.Client{Transport: commentTransport(func(r *http.Request) (*http.Response, error) {
		<-r.Context().Done()
		return nil, r.Context().Err()
	})}}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	_, _, err = c.GetInstallationTokenContext(ctx, 123)
	require.ErrorIs(t, err, context.DeadlineExceeded)
}

func TestUpsertPatchUpdatesMarkerCommentInPlace(t *testing.T) {
	const marker = "<!-- kuayle:pr-issue:test -->"
	const newBody = "<details>\n<p>new linkback</p>\n</details>"
	patches, posts := 0, 0
	c := &Client{appID: 42, httpClient: &http.Client{Transport: commentTransport(func(r *http.Request) (*http.Response, error) {
		if r.Method == http.MethodGet {
			require.Equal(t, "/repos/owner/repo/issues/7/comments", r.URL.Path)
			b, _ := json.Marshal([]map[string]any{{"id": 555, "body": marker + "\r\n\r\nLinked Kuayle issue: [KUA-60](https://kuayle.test/acme/issue/KUA-60)", "user": map[string]any{"type": "Bot"}, "performed_via_github_app": map[string]any{"id": 42}}})
			return commentResponse(200, string(b)), nil
		}
		if r.Method == http.MethodPatch {
			patches++
			require.Equal(t, "/repos/owner/repo/issues/7/comments/555", r.URL.Path)
			var body map[string]string
			require.NoError(t, json.NewDecoder(r.Body).Decode(&body))
			require.Equal(t, marker+"\n\n"+newBody, body["body"])
			return commentResponse(200, `{}`), nil
		}
		posts++
		return commentResponse(201, `{}`), nil
	})}}
	require.NoError(t, c.UpsertPullRequestComment(context.Background(), "secret", "owner/repo", 7, marker, newBody))
	require.Equal(t, 1, patches)
	require.Zero(t, posts)
}

func TestUpsertLeavesIdenticalBodyAlone(t *testing.T) {
	const marker = "<!-- kuayle:pr-issue:test -->"
	const body = "<details>\n<p>linkback</p>\n</details>"
	writes := 0
	c := &Client{appID: 42, httpClient: &http.Client{Transport: commentTransport(func(r *http.Request) (*http.Response, error) {
		if r.Method == http.MethodGet {
			b, _ := json.Marshal([]map[string]any{{"id": 555, "body": marker + "\r\n\r\n" + body, "user": map[string]any{"type": "Bot"}, "performed_via_github_app": map[string]any{"id": 42}}})
			return commentResponse(200, string(b)), nil
		}
		writes++
		return commentResponse(201, `{}`), nil
	})}}
	require.NoError(t, c.UpsertPullRequestComment(context.Background(), "secret", "owner/repo", 7, marker, body))
	require.Zero(t, writes)
}

func TestUpsertPostsWhenMarkerAbsent(t *testing.T) {
	const marker = "<!-- kuayle:pr-issue:test -->"
	patches, posts := 0, 0
	c := &Client{appID: 42, httpClient: &http.Client{Transport: commentTransport(func(r *http.Request) (*http.Response, error) {
		if r.Method == http.MethodGet {
			return commentResponse(200, `[{"id":1,"body":"unrelated","user":{"type":"Bot"},"performed_via_github_app":{"id":42}}]`), nil
		}
		if r.Method == http.MethodPatch {
			patches++
			return commentResponse(200, `{}`), nil
		}
		posts++
		return commentResponse(201, `{}`), nil
	})}}
	require.NoError(t, c.UpsertPullRequestComment(context.Background(), "secret", "owner/repo", 7, marker, "body"))
	require.Zero(t, patches)
	require.Equal(t, 1, posts)
}

func TestUpsertIgnoresSpoofedMarker(t *testing.T) {
	for name, existing := range map[string]map[string]any{
		"other app":  {"body": "<!-- kuayle:pr-issue:test -->\n\nspoofed", "user": map[string]any{"type": "Bot"}, "performed_via_github_app": map[string]any{"id": 43}},
		"human user": {"body": "<!-- kuayle:pr-issue:test -->\n\nspoofed", "user": map[string]any{"type": "User"}, "performed_via_github_app": map[string]any{"id": 42}},
	} {
		t.Run(name, func(t *testing.T) {
			patches, posts := 0, 0
			c := &Client{appID: 42, httpClient: &http.Client{Transport: commentTransport(func(r *http.Request) (*http.Response, error) {
				if r.Method == http.MethodGet {
					b, _ := json.Marshal([]map[string]any{existing})
					return commentResponse(200, string(b)), nil
				}
				if r.Method == http.MethodPatch {
					patches++
					return commentResponse(200, `{}`), nil
				}
				posts++
				return commentResponse(201, `{}`), nil
			})}}
			require.NoError(t, c.UpsertPullRequestComment(context.Background(), "secret", "owner/repo", 7, "<!-- kuayle:pr-issue:test -->", "body"))
			require.Zero(t, patches)
			require.Equal(t, 1, posts)
		})
	}
}

func TestUpsertPatchFailureDoesNotPostOrExposeContent(t *testing.T) {
	for _, status := range []int{401, 403, 404, 422, 429, 500} {
		patches, posts := 0, 0
		c := &Client{appID: 42, httpClient: &http.Client{Transport: commentTransport(func(r *http.Request) (*http.Response, error) {
			if r.Method == http.MethodGet {
				b, _ := json.Marshal([]map[string]any{{"id": 555, "body": "marker\r\n\r\nold", "user": map[string]any{"type": "Bot"}, "performed_via_github_app": map[string]any{"id": 42}}})
				return commentResponse(200, string(b)), nil
			}
			if r.Method == http.MethodPatch {
				patches++
				return commentResponse(status, "secret private content"), nil
			}
			posts++
			return commentResponse(201, `{}`), nil
		})}}
		err := c.UpsertPullRequestComment(context.Background(), "secret", "owner/repo", 7, "marker", "body")
		require.Error(t, err)
		require.NotContains(t, err.Error(), "secret")
		require.Equal(t, 1, patches)
		require.Zero(t, posts)
	}
}
