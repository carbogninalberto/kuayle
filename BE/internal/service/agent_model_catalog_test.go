package service

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/kuayle/kuayle-backend/internal/agent"
	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/kuayle/kuayle-backend/internal/dto"
	cryptoutil "github.com/kuayle/kuayle-backend/pkg/crypto"
)

// goPlanListing is the shape of OpenCode's Go plan model list, reduced to the
// cases the catalog has to handle.
const goPlanListing = `{"object":"list","data":[
 {"id":"deepseek-v4.1-flash","name":"DeepSeek V4.1 Flash","type":"chat","context":128000,
  "capabilities":{"reasoning":{"supported":true,"effort":["low","high","max"]},"tools":true}},
 {"id":"glm-6","name":"GLM 6","type":"chat","capabilities":{"reasoning":{"supported":true,"effort":["none","high"]}}},
 {"id":"kimi-k3","name":"Kimi K3","type":"chat","capabilities":{"reasoning":{"supported":true,"effort":["minimal","low","medium","high","xhigh"]}}},
 {"id":"qwen-coder","name":"Qwen Coder","type":"chat","capabilities":{"reasoning":{"supported":false}}},
 {"id":"plain","type":"chat"},
 {"id":"odd-effort","name":"Odd","capabilities":{"reasoning":{"effort":"high"}}},
 {"id":"bad id","name":"Space"},
 {"id":"--flag","name":"Flag"},
 {"id":"","name":"Empty"},
 {"id":"embedder","name":"Embedder","type":"embedding"},
 {"id":"deepseek-v4.1-flash","name":"Duplicate","type":"chat"},
 {"id":"long-name","name":"` + "A\\u0000B\\n" + `CCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCC","type":"chat"}
]}`

// goPlanUpstream is a fake OpenCode Go model list. body and status can change
// between requests; requests counts the fetches the catalog made.
type goPlanUpstream struct {
	server    *httptest.Server
	mu        sync.Mutex
	body      string
	status    int
	requests  atomic.Int32
	userAgent atomic.Value
	auth      atomic.Value
	hold      chan struct{}
}

func newGoPlanUpstream(t *testing.T) *goPlanUpstream {
	t.Helper()
	up := &goPlanUpstream{body: goPlanListing, status: http.StatusOK}
	up.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		up.requests.Add(1)
		up.userAgent.Store(r.Header.Get("User-Agent"))
		up.auth.Store(r.Header.Get("Authorization"))
		up.mu.Lock()
		body, status, hold := up.body, up.status, up.hold
		up.mu.Unlock()
		if hold != nil {
			<-hold
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(up.server.Close)
	return up
}

func (u *goPlanUpstream) answer(status int, body string) {
	u.mu.Lock()
	defer u.mu.Unlock()
	u.status, u.body = status, body
}

// catalogClock is the catalog's clock in tests.
type catalogClock struct {
	mu  sync.Mutex
	now time.Time
}

func (c *catalogClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *catalogClock) advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = c.now.Add(d)
}

// catalogService reads its OpenCode Go list from the fake upstream, which is
// on loopback and so needs private-network discovery.
func catalogService(t *testing.T, store *devMachineStoreFake, up *goPlanUpstream) (*DevMachineService, *catalogClock) {
	t.Helper()
	svc := NewDevMachineService(store, agent.NewRegistry(agent.NewCodexProvider(""), agent.NewClaudeCodeProvider(""), agent.NewOpenCodeProvider(""), agent.NewQwenCodeProvider("")),
		true, "machines.example.test", cryptoutil.DeriveKey("test"), time.Minute, DevMachineImages{})
	svc.SetModelDiscovery(true)
	clock := &catalogClock{now: time.Date(2026, 11, 10, 9, 0, 0, 0, time.UTC)}
	svc.openCodeGo.now = clock.Now
	if up != nil {
		svc.openCodeGo.url = up.server.URL + "/inference/go/v1/models"
	}
	return svc, clock
}

func catalogIDs(catalog *dto.AgentModelCatalogResponse) []string {
	ids := []string{}
	for _, model := range catalog.Models {
		ids = append(ids, model.ID)
	}
	return ids
}

func catalogModel(t *testing.T, catalog *dto.AgentModelCatalogResponse, id string) dto.AgentCatalogModel {
	t.Helper()
	for _, model := range catalog.Models {
		if model.ID == id {
			return model
		}
	}
	t.Fatalf("the catalog has no model %s: %v", id, catalogIDs(catalog))
	return dto.AgentCatalogModel{}
}

func TestOpenCodeCatalogListsTheGoPlanModelsWithKuayleEfforts(t *testing.T) {
	up := newGoPlanUpstream(t)
	svc, clock := catalogService(t, &devMachineStoreFake{}, up)

	catalog, err := svc.AgentModelCatalog(context.Background(), uuid.New(), "opencode", false)
	require.NoError(t, err)
	require.Equal(t, "opencode", catalog.Harness)
	require.Equal(t, "opencode-go", catalog.Source)
	require.False(t, catalog.Stale)
	require.False(t, catalog.Unavailable)
	require.Equal(t, clock.Now(), *catalog.UpdatedAt)
	// Invalid, empty, duplicate and non-chat entries are left out.
	require.Equal(t, []string{"opencode-go/deepseek-v4.1-flash", "opencode-go/glm-6", "opencode-go/kimi-k3", "opencode-go/long-name",
		"opencode-go/odd-effort", "opencode-go/plain", "opencode-go/qwen-coder"}, catalogIDs(catalog))

	flash := catalogModel(t, catalog, "opencode-go/deepseek-v4.1-flash")
	require.Equal(t, "DeepSeek V4.1 Flash", flash.Name)
	require.Equal(t, []string{"off", "low", "high", "max"}, flash.Efforts, "off (the model's default) is always allowed")
	require.Equal(t, []string{"off", "high"}, catalogModel(t, catalog, "opencode-go/glm-6").Efforts, "upstream none is Kuayle's off")
	require.Equal(t, []string{"off", "low", "medium", "high", "xhigh"}, catalogModel(t, catalog, "opencode-go/kimi-k3").Efforts, "minimal has no Kuayle level")
	// No selectable effort upstream: not known, any level.
	require.Empty(t, catalogModel(t, catalog, "opencode-go/qwen-coder").Efforts)
	require.Empty(t, catalogModel(t, catalog, "opencode-go/odd-effort").Efforts)
	plain := catalogModel(t, catalog, "opencode-go/plain")
	require.Empty(t, plain.Efforts)
	require.Equal(t, "plain", plain.Name, "a model without a name shows its id")
	// Names are untrusted text: no control characters, bounded length.
	long := catalogModel(t, catalog, "opencode-go/long-name").Name
	require.True(t, strings.HasPrefix(long, "ABCCC"), long)
	require.Len(t, []rune(long), maxCatalogModelNameRunes)

	// The list is public: no credential is sent, and the client names itself
	// without a version, host or user.
	require.Equal(t, "kuayle", up.userAgent.Load())
	require.Empty(t, up.auth.Load())
}

func TestOpenCodeCatalogIsCachedAndConcurrentRequestsFetchOnce(t *testing.T) {
	up := newGoPlanUpstream(t)
	svc, clock := catalogService(t, &devMachineStoreFake{}, up)
	hold := make(chan struct{})
	up.mu.Lock()
	up.hold = hold
	up.mu.Unlock()

	var wg sync.WaitGroup
	results := make([]*dto.AgentModelCatalogResponse, 8)
	for i := range results {
		wg.Add(1)
		go func() {
			defer wg.Done()
			results[i], _ = svc.AgentModelCatalog(context.Background(), uuid.New(), "opencode", false)
		}()
	}
	require.Eventually(t, func() bool { return up.requests.Load() == 1 }, 5*time.Second, 5*time.Millisecond)
	// Give every caller time to arrive while the one fetch is held.
	time.Sleep(50 * time.Millisecond)
	close(hold)
	wg.Wait()
	for _, result := range results {
		require.NotNil(t, result)
		require.Len(t, result.Models, 7)
	}
	require.EqualValues(t, 1, up.requests.Load(), "concurrent requests share one fetch")

	up.mu.Lock()
	up.hold = nil
	up.mu.Unlock()
	clock.advance(openCodeGoCatalogTTL - time.Minute)
	_, err := svc.AgentModelCatalog(context.Background(), uuid.New(), "opencode", false)
	require.NoError(t, err)
	require.EqualValues(t, 1, up.requests.Load(), "a fresh list is served from memory")

	clock.advance(2 * time.Minute)
	_, err = svc.AgentModelCatalog(context.Background(), uuid.New(), "opencode", false)
	require.NoError(t, err)
	require.EqualValues(t, 2, up.requests.Load(), "the list is fetched again after its TTL")
}

func TestOpenCodeCatalogServesTheLastGoodListWhenTheUpstreamFails(t *testing.T) {
	up := newGoPlanUpstream(t)
	svc, clock := catalogService(t, &devMachineStoreFake{}, up)
	first, err := svc.AgentModelCatalog(context.Background(), uuid.New(), "opencode", false)
	require.NoError(t, err)
	fetched := *first.UpdatedAt

	for name, answer := range map[string]struct {
		status int
		body   string
	}{
		"server error":    {http.StatusBadGateway, `{}`},
		"redirect":        {http.StatusFound, ``},
		"not json":        {http.StatusOK, `<html>blocked</html>`},
		"no valid models": {http.StatusOK, `{"data":[{"id":"bad id"}]}`},
		"oversized body":  {http.StatusOK, `{"data":[{"id":"big","name":"` + strings.Repeat("x", maxCatalogBodyBytes) + `"}]}`},
	} {
		t.Run(name, func(t *testing.T) {
			up.answer(answer.status, answer.body)
			clock.advance(openCodeGoCatalogTTL + time.Minute)
			before := up.requests.Load()
			stale, err := svc.AgentModelCatalog(context.Background(), uuid.New(), "opencode", false)
			require.NoError(t, err)
			require.Equal(t, before+1, up.requests.Load())
			require.True(t, stale.Stale)
			require.False(t, stale.Unavailable)
			require.Len(t, stale.Models, 7, "the last good list is kept")
			require.Equal(t, fetched, *stale.UpdatedAt)
		})
	}

	// A failed fetch is not repeated on every request.
	before := up.requests.Load()
	_, err = svc.AgentModelCatalog(context.Background(), uuid.New(), "opencode", false)
	require.NoError(t, err)
	require.Equal(t, before, up.requests.Load())

	// It recovers once the upstream answers again.
	up.answer(http.StatusOK, goPlanListing)
	clock.advance(openCodeGoCatalogRetry + time.Second)
	fresh, err := svc.AgentModelCatalog(context.Background(), uuid.New(), "opencode", false)
	require.NoError(t, err)
	require.False(t, fresh.Stale)
	require.Equal(t, clock.Now(), *fresh.UpdatedAt)
}

func TestOpenCodeCatalogIsUnavailableWithoutAnyGoodList(t *testing.T) {
	up := newGoPlanUpstream(t)
	up.answer(http.StatusServiceUnavailable, ``)
	svc, _ := catalogService(t, &devMachineStoreFake{}, up)

	catalog, err := svc.AgentModelCatalog(context.Background(), uuid.New(), "opencode", false)
	require.NoError(t, err, "an unreachable upstream never fails the request")
	require.True(t, catalog.Unavailable)
	require.False(t, catalog.Stale)
	require.NotNil(t, catalog.Models)
	require.Empty(t, catalog.Models)
	require.Nil(t, catalog.UpdatedAt)
}

// The list is fetched with the model discovery client: unless the operator
// allows private networks it dials public addresses only, so an upstream that
// resolves to loopback is never read.
func TestOpenCodeCatalogDialsPublicAddressesOnly(t *testing.T) {
	up := newGoPlanUpstream(t)
	svc, _ := catalogService(t, &devMachineStoreFake{}, up)
	svc.SetModelDiscovery(false)

	catalog, err := svc.AgentModelCatalog(context.Background(), uuid.New(), "opencode", false)
	require.NoError(t, err)
	require.True(t, catalog.Unavailable)
	require.Zero(t, up.requests.Load())
}

func TestOpenCodeCatalogRefreshBypassesTheTTLButIsRateLimited(t *testing.T) {
	up := newGoPlanUpstream(t)
	svc, clock := catalogService(t, &devMachineStoreFake{}, up)
	ctx := context.Background()
	_, err := svc.AgentModelCatalog(ctx, uuid.New(), "opencode", false)
	require.NoError(t, err)
	require.EqualValues(t, 1, up.requests.Load())

	for range 5 {
		catalog, err := svc.AgentModelCatalog(ctx, uuid.New(), "opencode", true)
		require.NoError(t, err)
		require.Len(t, catalog.Models, 7)
	}
	require.EqualValues(t, 1, up.requests.Load(), "a refresh right after a fetch is served from memory")

	clock.advance(openCodeGoCatalogRetry + time.Second)
	up.answer(http.StatusOK, `{"data":[{"id":"only-one","name":"Only One","type":"chat"}]}`)
	refreshed, err := svc.AgentModelCatalog(ctx, uuid.New(), "opencode", true)
	require.NoError(t, err)
	require.EqualValues(t, 2, up.requests.Load(), "a refresh fetches although the list is within its TTL")
	require.Equal(t, []string{"opencode-go/only-one"}, catalogIDs(refreshed))

	_, err = svc.AgentModelCatalog(ctx, uuid.New(), "opencode", true)
	require.NoError(t, err)
	require.EqualValues(t, 2, up.requests.Load())
}

func TestOpenCodeCatalogKeepsAtMostTheDiscoveryLimit(t *testing.T) {
	var body strings.Builder
	body.WriteString(`{"data":[`)
	for i := range maxDiscoveredModels + 20 {
		if i > 0 {
			body.WriteString(",")
		}
		fmt.Fprintf(&body, `{"id":"model-%04d","type":"chat"}`, i)
	}
	body.WriteString(`]}`)
	up := newGoPlanUpstream(t)
	up.answer(http.StatusOK, body.String())
	svc, _ := catalogService(t, &devMachineStoreFake{}, up)

	catalog, err := svc.AgentModelCatalog(context.Background(), uuid.New(), "opencode", false)
	require.NoError(t, err)
	require.Len(t, catalog.Models, maxDiscoveredModels)
}

// Claude Code and Codex suggest what the workspace already knows: the models a
// save without a model server would accept. No upstream is asked.
func TestModelServerHarnessCatalogListsWorkspaceModels(t *testing.T) {
	ws := uuid.New()
	up := newGoPlanUpstream(t)
	anthropicOnly := domain.AgentModelServer{ID: uuid.New(), WorkspaceID: ws, Name: "Anthropic gateway",
		AnthropicBaseURL: dmStrPtr("https://gateway.example.test"), Models: []byte(`["claude-opus-5-5","claude-haiku-5"]`), DefaultModel: "claude-opus-5-5", Thinking: "high"}
	store := &devMachineStoreFake{
		modelServers: []domain.AgentModelServer{anthropicOnly},
		agentModelDefaults: []domain.AgentModelDefault{
			{WorkspaceID: ws, Provider: "codex", Model: "gpt-5.5", Thinking: "high"},
			{WorkspaceID: ws, Provider: "claude-code", Model: "claude-sonnet-5"},
		},
	}
	svc, _ := catalogService(t, store, up)
	ctx := context.Background()

	// No default server serves the harness: it runs on its own sign-in, which
	// takes any model, so every known model is a suggestion.
	claude, err := svc.AgentModelCatalog(ctx, ws, "claude-code", true)
	require.NoError(t, err)
	require.Equal(t, "workspace", claude.Source)
	require.False(t, claude.Unavailable)
	require.Equal(t, []string{"claude-opus-5-5", "claude-haiku-5", "claude-sonnet-5"}, catalogIDs(claude))
	require.Equal(t, "claude-opus-5-5", claude.Models[0].Name)
	require.Empty(t, claude.Models[0].Efforts)

	codex, err := svc.AgentModelCatalog(ctx, ws, "codex", false)
	require.NoError(t, err)
	require.Equal(t, []string{"gpt-5.5"}, catalogIDs(codex), "a server that does not speak the harness's protocol adds nothing")

	// A default server that serves the harness runs every profile of it, and
	// refuses a model it does not list: only its models are suggested.
	store.modelServers = append(store.modelServers, deepSeekModelServer(ws))
	codex, err = svc.AgentModelCatalog(ctx, ws, "codex", false)
	require.NoError(t, err)
	require.Equal(t, []string{"deepseek-flash", "deepseek-v4-pro"}, catalogIDs(codex))
	for _, id := range catalogIDs(codex) {
		require.NoError(t, svc.validateAgentModel(ctx, ws, "codex", nil, id, ""), id)
	}

	for _, harness := range []string{"qwen-code", "unknown", ""} {
		empty, err := svc.AgentModelCatalog(ctx, ws, harness, false)
		require.NoError(t, err)
		require.NotNil(t, empty.Models)
		require.Empty(t, empty.Models, harness)
		require.False(t, empty.Unavailable)
	}
	require.Zero(t, up.requests.Load(), "only OpenCode's list is fetched")
}

// A profile save checks an OpenCode Go model and its effort against the list;
// everything the list cannot judge is accepted as before.
func TestProfileModelIsCheckedAgainstTheOpenCodeGoCatalog(t *testing.T) {
	up := newGoPlanUpstream(t)
	svc, _ := catalogService(t, &devMachineStoreFake{}, up)
	ctx := context.Background()

	for _, tc := range []struct {
		name, provider, model, effort, reason string
	}{
		{name: "known model", provider: "opencode", model: "opencode-go/deepseek-v4.1-flash"},
		{name: "known model and allowed effort", provider: "opencode", model: "opencode-go/deepseek-v4.1-flash", effort: "max"},
		{name: "off is always allowed", provider: "opencode", model: "opencode-go/kimi-k3", effort: "off"},
		{name: "unknown model", provider: "opencode", model: "opencode-go/deepseek-v9", reason: `OpenCode Go does not offer the model "opencode-go/deepseek-v9"`},
		{name: "effort the model does not allow", provider: "opencode", model: "opencode-go/deepseek-v4.1-flash", effort: "medium",
			reason: "opencode-go/deepseek-v4.1-flash allows the effort levels off, low, high, max"},
		{name: "model without known efforts takes any level", provider: "opencode", model: "opencode-go/qwen-coder", effort: "xhigh"},
		{name: "another opencode provider", provider: "opencode", model: "anthropic/claude-sonnet-5", effort: "max"},
		{name: "another opencode provider, nested", provider: "opencode", model: "openrouter/opencode-go/deepseek-v9"},
		{name: "no model", provider: "opencode", effort: "medium"},
		{name: "another harness", provider: "codex", model: "opencode-go/deepseek-v9"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := svc.validateCatalogModel(ctx, tc.provider, tc.model, tc.effort)
			if tc.reason == "" {
				require.NoError(t, err)
				return
			}
			var refused *AgentModelError
			require.ErrorAs(t, err, &refused)
			require.Equal(t, tc.reason, refused.Reason)
		})
	}
}

func TestProfileModelCheckNeverDependsOnTheUpstream(t *testing.T) {
	up := newGoPlanUpstream(t)
	up.answer(http.StatusServiceUnavailable, ``)
	svc, clock := catalogService(t, &devMachineStoreFake{}, up)
	ctx := context.Background()

	// No list at all: saving is not blocked.
	require.NoError(t, svc.validateCatalogModel(ctx, "opencode", "opencode-go/anything", "max"))

	// A slow upstream is waited for only briefly.
	hold := make(chan struct{})
	defer close(hold)
	up.mu.Lock()
	up.hold = hold
	up.mu.Unlock()
	svc.openCodeGo.saveWait = 20 * time.Millisecond
	clock.advance(openCodeGoCatalogRetry + time.Second)
	started := time.Now()
	require.NoError(t, svc.validateCatalogModel(ctx, "opencode", "opencode-go/anything", "max"))
	require.Less(t, time.Since(started), 3*time.Second)
}

func TestProfileModelCheckUsesAStaleListAndLearnsNewModels(t *testing.T) {
	up := newGoPlanUpstream(t)
	svc, clock := catalogService(t, &devMachineStoreFake{}, up)
	ctx := context.Background()
	require.NoError(t, svc.validateCatalogModel(ctx, "opencode", "opencode-go/glm-6", "high"))

	// The upstream goes away: the last good list still decides.
	up.answer(http.StatusBadGateway, ``)
	clock.advance(openCodeGoCatalogTTL + time.Minute)
	var refused *AgentModelError
	require.ErrorAs(t, svc.validateCatalogModel(ctx, "opencode", "opencode-go/deepseek-v9", ""), &refused)
	require.NoError(t, svc.validateCatalogModel(ctx, "opencode", "opencode-go/glm-6", "high"))

	// A model added upstream since the last fetch is looked up before it is
	// refused, within the refresh rate limit.
	up.answer(http.StatusOK, `{"data":[{"id":"deepseek-v9","type":"chat"}]}`)
	clock.advance(openCodeGoCatalogRetry + time.Second)
	require.NoError(t, svc.validateCatalogModel(ctx, "opencode", "opencode-go/deepseek-v9", ""))
}

// Starting a task never waits for the upstream and never refuses a model the
// list does not have: a stored profile keeps running if the list drops it.
func TestTaskStartIgnoresTheCatalog(t *testing.T) {
	up := newGoPlanUpstream(t)
	svc, _ := catalogService(t, &devMachineStoreFake{}, up)
	ctx := context.Background()
	_, err := svc.AgentModelCatalog(ctx, uuid.New(), "opencode", false)
	require.NoError(t, err)
	fetches := up.requests.Load()

	require.NoError(t, svc.validateAgentModel(ctx, uuid.New(), "opencode", nil, "opencode-go/dropped-model", "medium"))
	require.Equal(t, fetches, up.requests.Load())

	cold, _ := catalogService(t, &devMachineStoreFake{}, up)
	require.NoError(t, cold.validateAgentModel(ctx, uuid.New(), "opencode", nil, "opencode-go/deepseek-v4.1-flash", "medium"))
	require.Equal(t, fetches, up.requests.Load(), "a task start does not fetch the list")
}
