package service

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/stretchr/testify/require"
)

const searchKey = "search-key-must-stay-secret"

func TestBraveSearchSendsTheKeyAndCleansResults(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, http.MethodGet, r.Method)
		require.Equal(t, "/res/v1/web/search", r.URL.Path)
		require.Equal(t, searchKey, r.Header.Get("X-Subscription-Token"))
		require.Equal(t, "go os.Root", r.URL.Query().Get("q"))
		require.Equal(t, "3", r.URL.Query().Get("count"))
		_, _ = w.Write([]byte(`{"web":{"results":[
			{"title":"The <strong>os</strong> package","url":"https://pkg.go.dev/os","description":"Root &amp; <strong>files</strong>"},
			{"title":"Bad","url":"javascript:alert(1)","description":"x"},
			{"title":"","url":"https://go.dev/blog/osroot","description":"Traversal-resistant file APIs"}
		]}}`))
	}))
	defer server.Close()
	provider := &BraveSearch{Endpoint: server.URL + "/res/v1/web/search", Client: server.Client()}
	results, err := provider.Search(context.Background(), searchKey, "go os.Root", 3)
	require.NoError(t, err)
	require.Equal(t, []WebSearchResult{
		{Title: "The os package", URL: "https://pkg.go.dev/os", Excerpt: "Root & files"},
		{Title: "https://go.dev/blog/osroot", URL: "https://go.dev/blog/osroot", Excerpt: "Traversal-resistant file APIs"},
	}, results, "only http(s) results are kept; markup is removed")
}

func TestTavilySearchPostsTheQuery(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, http.MethodPost, r.Method)
		require.Equal(t, "Bearer "+searchKey, r.Header.Get("Authorization"))
		var body map[string]any
		require.NoError(t, json.NewDecoder(r.Body).Decode(&body))
		require.Equal(t, "kuayle", body["query"])
		require.EqualValues(t, 2, body["max_results"])
		_, _ = w.Write([]byte(`{"results":[{"title":"Kuayle","url":"https://kuayle.com","content":"Issue tracking"}]}`))
	}))
	defer server.Close()
	provider := &TavilySearch{Endpoint: server.URL, Client: server.Client()}
	results, err := provider.Search(context.Background(), searchKey, "kuayle", 2)
	require.NoError(t, err)
	require.Equal(t, []WebSearchResult{{Title: "Kuayle", URL: "https://kuayle.com", Excerpt: "Issue tracking"}}, results)
}

func TestSearchFailuresAreClassified(t *testing.T) {
	for status, want := range map[int]error{
		http.StatusUnauthorized:        ErrWebSearchUnauthorized,
		http.StatusForbidden:           ErrWebSearchUnauthorized,
		http.StatusTooManyRequests:     ErrWebSearchRateLimited,
		http.StatusInternalServerError: ErrWebSearchUnavailable,
	} {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(status)
			_, _ = w.Write([]byte(`{"error":"key ` + searchKey + ` rejected"}`))
		}))
		for _, provider := range []WebSearchProvider{&BraveSearch{Endpoint: server.URL, Client: server.Client()}, &TavilySearch{Endpoint: server.URL, Client: server.Client()}} {
			_, err := provider.Search(context.Background(), searchKey, "q", 1)
			require.ErrorIs(t, err, want, "%s %d", provider.Name(), status)
			require.NotContains(t, err.Error(), searchKey, "provider bodies are never echoed")
		}
		server.Close()
	}
	slow := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-r.Context().Done():
		case <-time.After(2 * time.Second):
		}
	}))
	defer slow.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	_, err := (&BraveSearch{Endpoint: slow.URL, Client: slow.Client()}).Search(ctx, searchKey, "q", 1)
	require.ErrorIs(t, err, ErrWebSearchTimeout)
}

// researchFake is the workspace's research settings.
type researchFake struct {
	config ResearchConfig
	err    error
}

func (f researchFake) ResearchConfig(context.Context, uuid.UUID) (ResearchConfig, error) {
	return f.config, f.err
}

// providerFake answers every query with n results.
type providerFake struct {
	name    string
	results []WebSearchResult
	err     error
	queries []string
	counts  []int
}

func (p *providerFake) Name() string { return p.name }
func (p *providerFake) Search(_ context.Context, key, query string, count int) ([]WebSearchResult, error) {
	if key != searchKey {
		return nil, ErrWebSearchUnauthorized
	}
	p.queries, p.counts = append(p.queries, query), append(p.counts, count)
	if len(p.results) > count {
		return p.results[:count], p.err
	}
	return p.results, p.err
}

func searchTools(config ResearchConfig, provider *providerFake) (*AssistantTools, uuid.UUID, uuid.UUID) {
	ws, member := uuid.New(), uuid.New()
	tools := NewAssistantTools(prIssues{}, nil, prMembers{member: domain.RoleMember}, nil)
	tools.SetWebSearch(researchFake{config: config}, provider)
	return tools, ws, member
}

func TestWebSearchToolCitesURLsWithinItsBudget(t *testing.T) {
	provider := &providerFake{name: "brave"}
	for i := range 10 {
		provider.results = append(provider.results, WebSearchResult{Title: fmt.Sprintf("Result %d", i), URL: fmt.Sprintf("https://example.com/%d", i), Excerpt: strings.Repeat("long excerpt ", 400)})
	}
	tools, ws, member := searchTools(ResearchConfig{SearchProvider: "brave", SearchAPIKey: searchKey}, provider)
	result, _, err := tools.run(context.Background(), ws, member, "web_search", `{"query":"  kuayle scans  ","count":50}`)
	require.NoError(t, err)
	require.Equal(t, []string{"kuayle scans"}, provider.queries)
	require.Equal(t, []int{maxWebSearchResults}, provider.counts, "count is capped")
	data, err := json.Marshal(result)
	require.NoError(t, err)
	require.LessOrEqual(t, len(data), webSearchResultBytes+1024)
	require.NotContains(t, string(data), searchKey)
	var decoded struct {
		Query    string            `json:"query"`
		Provider string            `json:"provider"`
		Results  []WebSearchResult `json:"results"`
		Note     string            `json:"note"`
	}
	require.NoError(t, json.Unmarshal(data, &decoded))
	require.Equal(t, "kuayle scans", decoded.Query)
	require.Equal(t, "brave", decoded.Provider)
	require.NotEmpty(t, decoded.Results)
	for _, r := range decoded.Results {
		require.LessOrEqual(t, len([]rune(r.Excerpt)), webSearchExcerptRunes+1)
		require.True(t, strings.HasPrefix(r.URL, "https://"))
	}
	require.Contains(t, decoded.Note, "Cite")
}

func TestWebSearchFailuresTellTheModelNotToInventSources(t *testing.T) {
	for name, tc := range map[string]struct {
		config ResearchConfig
		err    error
		want   string
	}{
		"no key":       {config: ResearchConfig{SearchProvider: "brave"}, want: "not configured"},
		"rate limited": {config: ResearchConfig{SearchProvider: "brave", SearchAPIKey: searchKey}, err: ErrWebSearchRateLimited, want: "rate"},
		"timeout":      {config: ResearchConfig{SearchProvider: "brave", SearchAPIKey: searchKey}, err: ErrWebSearchTimeout, want: "in time"},
		"bad key":      {config: ResearchConfig{SearchProvider: "brave", SearchAPIKey: "wrong"}, want: "rejected the search API key"},
		"unknown":      {config: ResearchConfig{SearchProvider: "other", SearchAPIKey: searchKey}, want: "not configured"},
	} {
		provider := &providerFake{name: "brave", err: tc.err, results: []WebSearchResult{{Title: "x", URL: "https://x"}}}
		tools, ws, member := searchTools(tc.config, provider)
		_, _, err := tools.run(context.Background(), ws, member, "web_search", `{"query":"x"}`)
		require.Error(t, err, name)
		text := toolErrorText(err)
		require.Contains(t, text, tc.want, name)
		require.Contains(t, text, "do not invent", name)
		require.NotContains(t, text, searchKey)
	}
	tools, ws, member := searchTools(ResearchConfig{SearchProvider: "brave", SearchAPIKey: searchKey}, &providerFake{name: "brave"})
	_, _, err := tools.run(context.Background(), ws, member, "web_search", `{"query":""}`)
	require.Error(t, err)
	result, _, err := tools.run(context.Background(), ws, member, "web_search", `{"query":"nothing"}`)
	require.NoError(t, err)
	require.Empty(t, result.(map[string]any)["results"])
}

func TestWebSearchIsOfferedOnlyOnceWired(t *testing.T) {
	tool, ok := lookupAssistantTool("web_search")
	require.True(t, ok)
	require.True(t, tool.webSearch)
	require.False(t, tool.write || tool.approval)
	require.False(t, (&AssistantTools{}).available(tool))
	tools, _, _ := searchTools(ResearchConfig{}, &providerFake{name: "brave"})
	require.True(t, tools.available(tool))
}
