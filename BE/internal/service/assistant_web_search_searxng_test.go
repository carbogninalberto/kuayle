package service

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/kuayle/kuayle-backend/pkg/validate"
	"github.com/stretchr/testify/require"
)

func searxngFor(t *testing.T, base string) *SearXNGSearch {
	t.Helper()
	parsed, err := validate.OperatorBaseURL(base)
	require.NoError(t, err)
	return NewSearXNGSearch(parsed)
}

func TestSearXNGSearchAsksForJSONAndCleansResults(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, http.MethodGet, r.Method)
		require.Equal(t, "/searxng/search", r.URL.Path, "the search path is under the configured base")
		query := r.URL.Query()
		require.Equal(t, "go os.Root", query.Get("q"))
		require.Equal(t, "json", query.Get("format"))
		require.Equal(t, "1", query.Get("safesearch"))
		require.Empty(t, r.Header.Get("Authorization"), "SearXNG needs no key")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"query":"go os.Root","number_of_results":0,"results":[
			{"title":"The <b>os</b> package","url":"https://pkg.go.dev/os","content":"Root &amp; <b>files</b>","engine":"duckduckgo"},
			{"title":"Script","url":"javascript:alert(1)","content":"x"},
			{"title":"Mail","url":"mailto:a@example.com","content":"x"},
			{"title":"","url":"http://go.dev/blog/osroot","content":"` + strings.Repeat("long ", 300) + `"}
		],"answers":[],"infoboxes":[],"suggestions":["x"],"unresponsive_engines":[]}`))
	}))
	defer server.Close()
	results, err := searxngFor(t, server.URL+"/searxng/").Search(context.Background(), "", "go os.Root", 5)
	require.NoError(t, err)
	require.Len(t, results, 2, "only http(s) results are kept")
	require.Equal(t, WebSearchResult{Title: "The os package", URL: "https://pkg.go.dev/os", Excerpt: "Root & files"}, results[0])
	require.Equal(t, "http://go.dev/blog/osroot", results[1].Title, "an empty title falls back to the URL")
	require.LessOrEqual(t, len([]rune(results[1].Excerpt)), webSearchExcerptRunes+1, "excerpts are capped")
}

func TestSearXNGFailuresAreClassified(t *testing.T) {
	for status, want := range map[int]error{
		http.StatusForbidden:           ErrWebSearchFormatDisabled,
		http.StatusTooManyRequests:     ErrWebSearchRateLimited,
		http.StatusInternalServerError: ErrWebSearchUnavailable,
		http.StatusBadGateway:          ErrWebSearchUnavailable,
	} {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(status)
			_, _ = w.Write([]byte(`<html>Forbidden</html>`))
		}))
		_, err := searxngFor(t, server.URL).Search(context.Background(), "", "q", 3)
		require.ErrorIs(t, err, want, "%d", status)
		server.Close()
	}
	invalid := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`<!doctype html><title>SearXNG</title>`))
	}))
	defer invalid.Close()
	_, err := searxngFor(t, invalid.URL).Search(context.Background(), "", "q", 3)
	require.ErrorIs(t, err, ErrWebSearchUnavailable, "an HTML page is not results")

	slow := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-r.Context().Done():
		case <-time.After(2 * time.Second):
		}
	}))
	defer slow.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	_, err = searxngFor(t, slow.URL).Search(ctx, "", "q", 1)
	require.ErrorIs(t, err, ErrWebSearchTimeout)

	closed := httptest.NewServer(http.NotFoundHandler())
	closedURL := closed.URL
	closed.Close()
	_, err = searxngFor(t, closedURL).Search(context.Background(), "", "q", 1)
	require.ErrorIs(t, err, ErrWebSearchUnavailable, "an unreachable instance")
}

// The configured SearXNG host is the only private address its client dials;
// any other private address is still refused, and redirects are not followed.
func TestSearXNGClientDialsOnlyTheConfiguredPrivateHost(t *testing.T) {
	var otherHits atomic.Int32
	other := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		otherHits.Add(1)
		_, _ = w.Write([]byte(`{"results":[{"title":"internal","url":"https://internal.example","content":"secret"}]}`))
	}))
	defer other.Close()
	configured := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("q") == "redirect" {
			http.Redirect(w, r, other.URL+"/search?format=json&q=redirect", http.StatusFound)
			return
		}
		_, _ = w.Write([]byte(`{"results":[{"title":"ok","url":"https://ok.example","content":"fine"}]}`))
	}))
	defer configured.Close()
	provider := searxngFor(t, configured.URL)

	results, err := provider.Search(context.Background(), "", "hello", 3)
	require.NoError(t, err, "the configured loopback host is allowed")
	require.Equal(t, "https://ok.example", results[0].URL)

	_, err = provider.Search(context.Background(), "", "redirect", 3)
	require.ErrorIs(t, err, ErrWebSearchUnavailable, "a redirect is an error, not followed")
	require.Zero(t, otherHits.Load())

	// The same client refuses any other private address, including another
	// port on the configured host.
	for _, target := range []string{other.URL, "http://127.0.0.2:" + mustPort(t, configured.URL), "http://169.254.169.254/latest/meta-data/"} {
		request, err := http.NewRequest(http.MethodGet, target, nil)
		require.NoError(t, err)
		response, err := provider.Client.Do(request)
		if response != nil {
			_, _ = io.Copy(io.Discard, response.Body)
			response.Body.Close()
		}
		require.ErrorIs(t, err, validate.ErrNonPublicAddress, target)
	}
	require.Zero(t, otherHits.Load())

	// A client without an allowed host is the public-only one.
	_, err = (&SearXNGSearch{Base: provider.Base, Client: newSearchClient()}).Search(context.Background(), "", "hello", 3)
	require.ErrorIs(t, err, ErrWebSearchUnavailable)
}

func mustPort(t *testing.T, raw string) string {
	parsed, err := url.Parse(raw)
	require.NoError(t, err)
	return parsed.Port()
}

func TestWebSearchToolUsesSearXNGWithoutAKey(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var results []string
		for i := range 12 {
			results = append(results, fmt.Sprintf(`{"title":"R%d","url":"https://example.com/%d","content":"%s"}`, i, i, strings.Repeat("text ", 200)))
		}
		_, _ = w.Write([]byte(`{"results":[` + strings.Join(results, ",") + `]}`))
	}))
	defer server.Close()
	tools, ws, member := searchTools(ResearchConfig{SearchProvider: "searxng"}, &providerFake{name: "brave"})
	tools.SetWebSearch(researchFake{config: ResearchConfig{SearchProvider: "searxng"}}, searxngFor(t, server.URL))
	result, _, err := tools.run(context.Background(), ws, member, "web_search", `{"query":"kuayle","count":4}`)
	require.NoError(t, err)
	data, err := json.Marshal(result)
	require.NoError(t, err)
	var decoded struct {
		Provider string            `json:"provider"`
		Results  []WebSearchResult `json:"results"`
	}
	require.NoError(t, json.Unmarshal(data, &decoded))
	require.Equal(t, "searxng", decoded.Provider)
	require.Len(t, decoded.Results, 4, "SearXNG has no count parameter; the tool caps the list")
	require.LessOrEqual(t, len(data), webSearchResultBytes+1024)
}

func TestWebSearchToolExplainsAFormatDisabledSearXNG(t *testing.T) {
	provider := &providerFake{name: "searxng", err: ErrWebSearchFormatDisabled}
	tools, ws, member := searchTools(ResearchConfig{SearchProvider: "searxng"}, provider)
	tools.SetWebSearch(researchFake{config: ResearchConfig{SearchProvider: "searxng"}}, keylessFake{provider})
	_, _, err := tools.run(context.Background(), ws, member, "web_search", `{"query":"x"}`)
	require.Error(t, err)
	text := toolErrorText(err)
	require.Contains(t, text, "search.formats")
	require.Contains(t, text, "json")
	require.Contains(t, text, "do not invent")

	// A provider the instance no longer offers is not configured.
	tools.SetWebSearch(researchFake{config: ResearchConfig{SearchProvider: "searxng"}}, &providerFake{name: "brave"})
	_, _, err = tools.run(context.Background(), ws, member, "web_search", `{"query":"x"}`)
	require.Contains(t, toolErrorText(err), "not configured")
}

// keylessFake is a provider that needs no API key.
type keylessFake struct{ *providerFake }

func (keylessFake) Keyless() bool { return true }
func (k keylessFake) Search(ctx context.Context, _ string, query string, count int) ([]WebSearchResult, error) {
	return k.providerFake.Search(ctx, searchKey, query, count)
}
