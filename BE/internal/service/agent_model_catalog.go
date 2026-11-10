package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/kuayle/kuayle-backend/internal/dto"
)

// The model catalog is what the model field of a profile or task suggests for
// a coding agent. OpenCode's comes from the public model list of its Go plan;
// Codex and Claude Code suggest what the workspace already configured. The
// field stays free text everywhere: the catalog helps, it never limits what
// can be typed.

const (
	// openCodeGoModelsURL is OpenCode's public Go plan model list. It needs no
	// key. The address is fixed here: no request or setting can change it.
	openCodeGoModelsURL = "https://opencode.ai/inference/go/v1/models"
	// openCodeGoPrefix is how OpenCode addresses a Go plan model.
	openCodeGoPrefix = "opencode-go/"
	// catalogUserAgent names the client as OpenCode asks, with nothing about
	// the deployment or its users.
	catalogUserAgent = "kuayle"

	openCodeGoCatalogTTL = time.Hour
	// openCodeGoCatalogRetry is the least time between two fetches, whether a
	// user asked to refresh or the last fetch failed.
	openCodeGoCatalogRetry   = 30 * time.Second
	openCodeGoFetchTimeout   = 10 * time.Second
	openCodeGoSaveWait       = 3 * time.Second
	maxCatalogBodyBytes      = 2 << 20
	maxCatalogModelNameRunes = 80

	catalogSourceOpenCodeGo = "opencode-go"
	catalogSourceWorkspace  = "workspace"
)

var errCatalogFetch = errors.New("the model list could not be read")

// openCodeGoCatalog keeps the last good Go plan list in memory, per process.
// One fetch runs at a time, on its own deadline, so a caller that gives up
// does not cancel it for the others.
type openCodeGoCatalog struct {
	url      string
	now      func() time.Time
	saveWait time.Duration

	mu          sync.Mutex
	models      []dto.AgentCatalogModel
	fetchedAt   time.Time
	attemptedAt time.Time
	failed      bool
	inflight    chan struct{}
}

func newOpenCodeGoCatalog() *openCodeGoCatalog {
	return &openCodeGoCatalog{url: openCodeGoModelsURL, now: time.Now, saveWait: openCodeGoSaveWait}
}

// openCodeGoList is the catalog as one caller sees it.
type openCodeGoList struct {
	models    []dto.AgentCatalogModel
	fetchedAt time.Time
	// stale: the list is older than its TTL or the last fetch failed.
	stale bool
	// available is false while no list was ever read.
	available bool
}

func (c *openCodeGoCatalog) listLocked() openCodeGoList {
	if c.fetchedAt.IsZero() {
		return openCodeGoList{}
	}
	return openCodeGoList{models: c.models, fetchedAt: c.fetchedAt, available: true,
		stale: c.failed || c.now().Sub(c.fetchedAt) >= openCodeGoCatalogTTL}
}

// cached is the list in memory. It never fetches.
func (c *openCodeGoCatalog) cached() openCodeGoList {
	if c == nil {
		return openCodeGoList{}
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.listLocked()
}

// load returns the list, fetching it first when there is none, it is past its
// TTL or force asks for a refresh. Fetches are at least openCodeGoCatalogRetry
// apart; in between, what is in memory is returned. The caller waits for a
// fetch at most wait, and no longer than its own context.
func (c *openCodeGoCatalog) load(ctx context.Context, client *http.Client, force bool, wait time.Duration) openCodeGoList {
	if c == nil {
		return openCodeGoList{}
	}
	c.mu.Lock()
	now := c.now()
	due := force || c.fetchedAt.IsZero() || c.failed || now.Sub(c.fetchedAt) >= openCodeGoCatalogTTL
	if due && c.inflight == nil && (c.attemptedAt.IsZero() || now.Sub(c.attemptedAt) >= openCodeGoCatalogRetry) {
		c.inflight, c.attemptedAt = make(chan struct{}), now
		go c.fetch(client, c.inflight)
	}
	pending := c.inflight
	c.mu.Unlock()
	if due && pending != nil {
		timer := time.NewTimer(wait)
		defer timer.Stop()
		select {
		case <-pending:
		case <-timer.C:
		case <-ctx.Done():
		}
	}
	return c.cached()
}

func (c *openCodeGoCatalog) fetch(client *http.Client, done chan struct{}) {
	ctx, cancel := context.WithTimeout(context.Background(), openCodeGoFetchTimeout)
	defer cancel()
	models, err := fetchOpenCodeGoModels(ctx, client, c.url)
	c.mu.Lock()
	if err == nil {
		c.models, c.fetchedAt = models, c.now()
	}
	c.failed, c.inflight = err != nil, nil
	c.mu.Unlock()
	close(done)
}

// fetchOpenCodeGoModels reads the list with the model discovery client, so
// its rules apply: public addresses only unless the operator allows private
// networks, and no redirects. Everything in the answer is untrusted.
func fetchOpenCodeGoModels(ctx context.Context, client *http.Client, url string) ([]dto.AgentCatalogModel, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, errCatalogFetch
	}
	request.Header.Set("User-Agent", catalogUserAgent)
	request.Header.Set("Accept", "application/json")
	response, err := client.Do(request)
	if err != nil {
		return nil, errCatalogFetch
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return nil, errCatalogFetch
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, maxCatalogBodyBytes+1))
	if err != nil || len(body) > maxCatalogBodyBytes {
		return nil, errCatalogFetch
	}
	models := parseOpenCodeGoModels(body)
	if len(models) == 0 {
		// An empty list would refuse every model on save; keep the last one.
		return nil, errCatalogFetch
	}
	return models, nil
}

// parseOpenCodeGoModels keeps the chat models whose prefixed ID is a valid
// model name, at most maxDiscoveredModels of them, sorted by ID.
func parseOpenCodeGoModels(body []byte) []dto.AgentCatalogModel {
	var listing struct {
		Data []struct {
			ID           string          `json:"id"`
			Name         json.RawMessage `json:"name"`
			Type         json.RawMessage `json:"type"`
			Capabilities json.RawMessage `json:"capabilities"`
		} `json:"data"`
	}
	if json.Unmarshal(body, &listing) != nil {
		return nil
	}
	models, seen := []dto.AgentCatalogModel{}, map[string]bool{}
	for _, item := range listing.Data {
		id := openCodeGoPrefix + item.ID
		var kind, name string
		_ = json.Unmarshal(item.Type, &kind)
		_ = json.Unmarshal(item.Name, &name)
		// The bare ID must be a model name too: the prefix alone would let an
		// ID that starts like a flag through.
		if seen[id] || !modelNamePattern.MatchString(item.ID) || !modelNamePattern.MatchString(id) || (kind != "" && kind != "chat") {
			continue
		}
		seen[id] = true
		// A shape other than the expected one means the efforts are not known.
		var capabilities struct {
			Reasoning struct {
				Effort []string `json:"effort"`
			} `json:"reasoning"`
		}
		_ = json.Unmarshal(item.Capabilities, &capabilities)
		models = append(models, dto.AgentCatalogModel{ID: id, Name: catalogModelName(name, item.ID), Efforts: kuayleEfforts(capabilities.Reasoning.Effort)})
		if len(models) == maxDiscoveredModels {
			break
		}
	}
	sort.Slice(models, func(i, j int) bool { return models[i].ID < models[j].ID })
	return models
}

// kuayleEfforts maps a model's upstream effort values onto Kuayle's levels:
// the ones Kuayle has, with "none" as off; values without a level (minimal)
// are dropped. Off, which passes no variant and so runs the model's default,
// is always allowed. Empty means the model's efforts are not known.
func kuayleEfforts(upstream []string) []string {
	allowed := map[string]bool{}
	for _, value := range upstream {
		if value == "none" {
			value = "off"
		}
		if containsString(modelThinkingLevels, value) {
			allowed[value] = true
		}
	}
	efforts := []string{}
	if len(allowed) == 0 {
		return efforts
	}
	allowed["off"] = true
	for _, level := range modelThinkingLevels {
		if allowed[level] {
			efforts = append(efforts, level)
		}
	}
	return efforts
}

// catalogModelName is a display name from untrusted text: one line without
// control characters, length-capped, and the model's ID when nothing is left.
func catalogModelName(name, fallback string) string {
	name = strings.TrimSpace(strings.Map(func(r rune) rune {
		if unicode.IsControl(r) || r == utf8.RuneError {
			return -1
		}
		return r
	}, name))
	if runes := []rune(name); len(runes) > maxCatalogModelNameRunes {
		name = strings.TrimSpace(string(runes[:maxCatalogModelNameRunes]))
	}
	if name == "" {
		return fallback
	}
	return name
}

// AgentModelCatalog lists the models a profile or task can choose for a
// coding agent. It never fails because an upstream is unreachable: OpenCode's
// list is then the last good one marked stale, or empty and unavailable.
// refresh asks for a new fetch, at most every openCodeGoCatalogRetry.
// Callers check that the user may read the workspace's agent settings.
func (s *DevMachineService) AgentModelCatalog(ctx context.Context, workspaceID uuid.UUID, harness string, refresh bool) (*dto.AgentModelCatalogResponse, error) {
	catalog := &dto.AgentModelCatalogResponse{Harness: harness, Models: []dto.AgentCatalogModel{}}
	switch {
	case harness == "opencode":
		list := s.openCodeGo.load(ctx, s.modelHTTPClient(), refresh, openCodeGoFetchTimeout)
		catalog.Source, catalog.Stale, catalog.Unavailable = catalogSourceOpenCodeGo, list.stale, !list.available
		if list.available {
			catalog.Models = append(catalog.Models, list.models...)
			fetched := list.fetchedAt
			catalog.UpdatedAt = &fetched
		}
	case harnessRunsOnModelServer(harness):
		models, err := s.workspaceModels(ctx, workspaceID, harness)
		if err != nil {
			return nil, err
		}
		for _, model := range models {
			catalog.Models = append(catalog.Models, dto.AgentCatalogModel{ID: model, Name: model, Efforts: []string{}})
		}
		if len(models) > 0 {
			catalog.Source = catalogSourceWorkspace
		}
	}
	return catalog, nil
}

// workspaceModels are the models the workspace already knows for a harness
// that runs on model servers, as a profile (which names no server) can use
// them. When the default server speaks the harness's protocol it runs the
// profile and refuses a model outside its list, so that list is the answer.
// Otherwise the harness runs on its own sign-in, which takes any model: the
// models of every server that speaks its protocol and the workspace's default
// model for the agent are all suggestions.
func (s *DevMachineService) workspaceModels(ctx context.Context, workspaceID uuid.UUID, harness string) ([]string, error) {
	servers, err := s.store.ListModelServers(ctx, workspaceID)
	if err != nil {
		return nil, err
	}
	models := []string{}
	add := func(values ...string) {
		for _, value := range values {
			if value != "" && modelNamePattern.MatchString(value) && !containsString(models, value) && len(models) < maxDiscoveredModels {
				models = append(models, value)
			}
		}
	}
	for _, server := range servers {
		if server.IsDefault && server.Serves(harness) {
			if listed := server.ModelList(); len(listed) > 0 {
				add(listed...)
				return models, nil
			}
		}
	}
	for _, server := range servers {
		if server.Serves(harness) {
			add(server.ModelList()...)
			add(server.DefaultModel)
		}
	}
	defaults, err := s.store.ListAgentModelDefaults(ctx, workspaceID)
	if err != nil {
		return nil, err
	}
	for _, value := range defaults {
		if value.Provider == harness {
			add(value.Model)
		}
	}
	return models, nil
}

// validateCatalogModel checks a model and effort a user is saving against the
// catalog. Only OpenCode's Go plan models can be judged: another OpenCode
// provider, another coding agent, or no list in memory accept as before, so a
// save never depends on the upstream being reachable. It waits briefly for a
// first fetch, and looks once for a model added since the last one before
// refusing it. Task starts and workflow steps do not call it: a stored
// profile keeps running if the list later drops its model.
func (s *DevMachineService) validateCatalogModel(ctx context.Context, provider, model, thinking string) error {
	if provider != "opencode" || !strings.HasPrefix(model, openCodeGoPrefix) {
		return nil
	}
	list := s.openCodeGo.load(ctx, s.modelHTTPClient(), false, s.openCodeGo.wait())
	if !list.available {
		return nil
	}
	entry, known := findCatalogModel(list.models, model)
	if !known {
		list = s.openCodeGo.load(ctx, s.modelHTTPClient(), true, s.openCodeGo.wait())
		if entry, known = findCatalogModel(list.models, model); !known {
			return &AgentModelError{fmt.Sprintf("OpenCode Go does not offer the model %q", model)}
		}
	}
	if thinking != "" && thinking != "off" && len(entry.Efforts) > 0 && !containsString(entry.Efforts, thinking) {
		return &AgentModelError{fmt.Sprintf("%s allows the effort levels %s", model, strings.Join(entry.Efforts, ", "))}
	}
	return nil
}

func (c *openCodeGoCatalog) wait() time.Duration {
	if c == nil {
		return 0
	}
	return c.saveWait
}

func findCatalogModel(models []dto.AgentCatalogModel, id string) (dto.AgentCatalogModel, bool) {
	for _, model := range models {
		if model.ID == id {
			return model, true
		}
	}
	return dto.AgentCatalogModel{}, false
}
