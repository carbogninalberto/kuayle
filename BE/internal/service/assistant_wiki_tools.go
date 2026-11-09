package service

import (
	"context"
	"errors"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/kuayle/kuayle-backend/internal/dto"
	"github.com/kuayle/kuayle-backend/pkg/sanitize"
)

// assistantWiki is the wiki service as the assistant uses it: the same
// methods as the HTTP routes. Every call applies the conversation creator's
// visibility of the wiki's team or project.
type assistantWiki interface {
	List(ctx context.Context, workspaceID uuid.UUID, scope domain.WikiScope) ([]domain.WikiPage, error)
	Get(ctx context.Context, workspaceID uuid.UUID, scope domain.WikiScope, pageID uuid.UUID) (*domain.WikiPage, error)
	Create(ctx context.Context, workspaceID uuid.UUID, scope domain.WikiScope, userID uuid.UUID, req dto.CreateWikiPageRequest) (*domain.WikiPage, error)
	Update(ctx context.Context, workspaceID uuid.UUID, scope domain.WikiScope, pageID, userID uuid.UUID, req dto.UpdateWikiPageRequest) (*domain.WikiPage, error)
	Delete(ctx context.Context, workspaceID uuid.UUID, scope domain.WikiScope, pageID uuid.UUID, userID uuid.UUID) error
	Revisions(ctx context.Context, workspaceID uuid.UUID, scope domain.WikiScope, pageID uuid.UUID) ([]domain.WikiRevision, error)
	AppendRegistry(ctx context.Context, workspaceID uuid.UUID, scope domain.WikiScope, userID uuid.UUID, text string) (*domain.WikiPage, error)
	Settings(ctx context.Context, workspaceID uuid.UUID, scope domain.WikiScope) (domain.WikiSettings, error)
	UpdateSettings(ctx context.Context, workspaceID uuid.UUID, scope domain.WikiScope, userID uuid.UUID, registry *bool, schedule *domain.WikiOverviewSchedule) (domain.WikiSettings, error)
	GenerateOverview(ctx context.Context, workspaceID uuid.UUID, scope domain.WikiScope, userID *uuid.UUID, start, end time.Time, trigger string) (*domain.WikiOverview, error)
	Tree(ctx context.Context, workspaceID uuid.UUID, scope domain.WikiScope) ([]domain.WikiTreeNode, error)
	Move(ctx context.Context, workspaceID uuid.UUID, scope domain.WikiScope, pageID, userID uuid.UUID, parent *uuid.UUID, index int) ([]domain.WikiTreeNode, error)
	CreateIn(ctx context.Context, workspaceID uuid.UUID, scope domain.WikiScope, userID uuid.UUID, req dto.CreateWikiPageRequest, kind string, parent *uuid.UUID) (*domain.WikiPage, error)
}

// assistantWikiPublisher is the publishing service as the HTTP route uses it.
type assistantWikiPublisher interface {
	State(ctx context.Context, workspaceID uuid.UUID, scope domain.WikiScope, pageID uuid.UUID) (*domain.WikiPublication, error)
	Publish(ctx context.Context, ws *domain.Workspace, role string, userID uuid.UUID, scope domain.WikiScope, pageID uuid.UUID, mode string, children, rotate bool) (*domain.WikiPublication, error)
}

type assistantWorkspaces interface {
	GetByID(ctx context.Context, id uuid.UUID) (*domain.Workspace, error)
}

// SetWikiPublishing enables publish_wiki_page and share_wiki_page.
func (t *AssistantTools) SetWikiPublishing(publish assistantWikiPublisher, workspaces assistantWorkspaces) {
	t.wikiPublish, t.wikiWorkspaces = publish, workspaces
}

type assistantProjects interface {
	ListByWorkspace(ctx context.Context, workspaceID uuid.UUID) ([]domain.Project, error)
}

// SetWiki enables the wiki tools.
func (t *AssistantTools) SetWiki(wiki assistantWiki, projects assistantProjects) {
	t.wiki, t.wikiProjects = wiki, projects
}

const wikiScopeParams = `"team_key":{"type":"string","maxLength":16,"description":"Key of the team whose wiki to use (see list_teams)."},"project":{"type":"string","maxLength":200,"description":"Name or ID of the project whose wiki to use, instead of team_key."}`

const (
	// assistantWikiReadContentRunes bounds a page's text in get_wiki_page.
	assistantWikiReadContentRunes = 12000
	assistantWikiWriteRunes       = 16000
)

// Destructive wiki actions; each always asks the user, whatever else the
// workspace lets the assistant do on its own.
const (
	wikiDeleteAction          = "delete_wiki_page"
	wikiRestoreAction         = "restore_wiki_page_version"
	wikiDisableRegistryAction = "disable_wiki_registry"
	// wikiShareAction makes a page public, replaces its public link or takes
	// a public page off the web: every change to what anonymous readers see.
	wikiShareAction = "share_wiki_page"
)

var assistantWikiToolCatalog = []assistantTool{
	{name: "list_wiki_pages", wiki: true, description: "List the pages of a team's or project's wiki the user can see, newest first, with their IDs and versions. Registry pages (the wiki's daily log) carry registry_date. Give team_key or project.", parameters: `{"type":"object","properties":{` + wikiScopeParams + `},"additionalProperties":false}`},
	{name: "get_wiki_page", wiki: true, description: "Read one wiki page as text, with its headings and its version (needed to change it). Give team_key or project and the page_id from list_wiki_pages.", parameters: `{"type":"object","properties":{` + wikiScopeParams + `,"page_id":{"type":"string","maxLength":36}},"required":["page_id"],"additionalProperties":false}`},
	{name: "list_wiki_page_versions", wiki: true, description: "List a wiki page's recorded changes, newest first: version_id, version, who and when, and the title before and after. Use a version_id with restore_wiki_page_version.", parameters: `{"type":"object","properties":{` + wikiScopeParams + `,"page_id":{"type":"string","maxLength":36}},"required":["page_id"],"additionalProperties":false}`},
	{name: "create_wiki_page", wiki: true, write: true, description: "Create a page in a team's or project's wiki. content is Markdown or simple HTML. kind \"folder\" creates a folder (a page that groups others); parent_id places it inside a page or folder from list_wiki_pages, last among its siblings. Only call this when the user asked to write to the wiki.", parameters: `{"type":"object","properties":{` + wikiScopeParams + `,"title":{"type":"string","minLength":1,"maxLength":200},"content":{"type":"string","maxLength":16000},"kind":{"type":"string","enum":["page","folder"]},"parent_id":{"type":"string","maxLength":36}},"required":["title"],"additionalProperties":false}`},
	{name: "move_wiki_page", wiki: true, write: true, description: "Move a wiki page or folder: parent_id is the page or folder to put it in (null or omitted for the top level) and index its position among its new siblings, 0 first. Pages nest at most 6 levels, never inside themselves, and only within the same wiki. Only call this when the user asked to reorganise the wiki.", parameters: `{"type":"object","properties":{` + wikiScopeParams + `,"page_id":{"type":"string","maxLength":36},"parent_id":{"type":["string","null"],"maxLength":36},"index":{"type":"integer","minimum":0}},"required":["page_id"],"additionalProperties":false}`},
	{name: "publish_wiki_page", wiki: true, write: true, publish: true, description: "Read or set how a wiki page is published inside the workspace. mode \"status\" reads it; \"internal\" gives members who can see the wiki a clean read-only reader; \"off\" stops that. children includes the pages nested under it. Making a page public, replacing its public link or taking a public page off the web is share_wiki_page. Only change it when the user asked.", parameters: `{"type":"object","properties":{` + wikiScopeParams + `,"page_id":{"type":"string","maxLength":36},"mode":{"type":"string","enum":["status","off","internal"]},"children":{"type":"boolean"}},"required":["page_id","mode"],"additionalProperties":false}`},
	{name: wikiShareAction, wiki: true, approval: true, publish: true, description: "Ask the user to approve a change to what anyone on the web can read: mode \"public\" publishes the page (children also its nested pages) at a link anyone can open without signing in; rotate replaces a public page's link so the old one stops working; mode \"internal\" or \"off\" takes a public page off the web. Nothing changes until the user approves in Kuayle. Only call this when the user asked.", parameters: `{"type":"object","properties":{` + wikiScopeParams + `,"page_id":{"type":"string","maxLength":36},"mode":{"type":"string","enum":["public","internal","off"]},"children":{"type":"boolean"},"rotate":{"type":"boolean"}},"required":["page_id","mode"],"additionalProperties":false}`},
	{name: "update_wiki_page", wiki: true, write: true, description: "Change a wiki page. title renames it. content (Markdown or simple HTML) is applied by mode: \"replace\" replaces the whole page, \"append\" adds it at the end, \"replace_section\" replaces what is under heading (up to the next heading of its level), \"insert_after_heading\" inserts it right after heading. version is the version from get_wiki_page; it is required for replace and recommended otherwise. If the page changed since, the call fails as a conflict: read the page again and redo the change, never retry blindly. Only call this when the user asked to change the page.", parameters: `{"type":"object","properties":{` + wikiScopeParams + `,"page_id":{"type":"string","maxLength":36},"title":{"type":"string","minLength":1,"maxLength":200},"content":{"type":"string","maxLength":16000},"mode":{"type":"string","enum":["replace","append","replace_section","insert_after_heading"]},"heading":{"type":"string","maxLength":200},"version":{"type":"integer","minimum":1}},"required":["page_id"],"additionalProperties":false}`},
	{name: wikiDeleteAction, wiki: true, approval: true, description: "Ask the user to approve deleting a wiki page. Nothing is deleted until the user approves in Kuayle. Only call this when the user asked to delete the page.", parameters: `{"type":"object","properties":{` + wikiScopeParams + `,"page_id":{"type":"string","maxLength":36}},"required":["page_id"],"additionalProperties":false}`},
	{name: wikiRestoreAction, wiki: true, approval: true, description: "Ask the user to approve restoring a wiki page to a recorded version: the state after that change (state \"after\", the default) or before it (\"before\"). It replaces the page's current title and content once the user approves, and fails as a conflict if the page changed after you asked. Only call this when the user asked to restore.", parameters: `{"type":"object","properties":{` + wikiScopeParams + `,"page_id":{"type":"string","maxLength":36},"version_id":{"type":"string","maxLength":36},"state":{"type":"string","enum":["after","before"]}},"required":["page_id","version_id"],"additionalProperties":false}`},
	{name: "append_wiki_registry", wiki: true, write: true, description: "Add one short plain-text line to today's page of a wiki's registry, the team's or project's daily log of what happened (for example \"KUA-20 implemented, PR #161 opened\"). Kuayle adds the time and picks today's page. Only works when the wiki's registry is turned on; use it when the user asks to log something or after finishing work they asked you to record.", parameters: `{"type":"object","properties":{` + wikiScopeParams + `,"text":{"type":"string","minLength":1,"maxLength":2000}},"required":["text"],"additionalProperties":false}`},
	{name: "manage_wiki_registry", wiki: true, write: true, description: "Manage a wiki's registry. action \"status\" reads whether it is on and its overview schedule; \"enable\" turns it on; \"set_overview_schedule\" saves when AI overviews are generated (frequency daily or weekly, weekday 0=Sunday..6, time HH:MM UTC, period_days 1..31, enabled); \"generate_overview\" summarises period_start..period_end (YYYY-MM-DD, at most 31 days) now with the workspace's AI settings. Turning the registry off is disable_wiki_registry. Only change settings when the user asked.", parameters: `{"type":"object","properties":{` + wikiScopeParams + `,"action":{"type":"string","enum":["status","enable","set_overview_schedule","generate_overview"]},"enabled":{"type":"boolean"},"frequency":{"type":"string","enum":["daily","weekly"]},"weekday":{"type":"integer","minimum":0,"maximum":6},"time":{"type":"string","maxLength":5},"period_days":{"type":"integer","minimum":1,"maximum":31},"period_start":{"type":"string","maxLength":10},"period_end":{"type":"string","maxLength":10}},"required":["action"],"additionalProperties":false}`},
	{name: wikiDisableRegistryAction, wiki: true, approval: true, description: "Ask the user to approve turning a wiki's registry off; agents and the assistant then cannot log to it. Existing registry pages stay. Only call this when the user asked.", parameters: `{"type":"object","properties":{` + wikiScopeParams + `},"additionalProperties":false}`},
}

// wikiToolScopeInput addresses a team wiki by key or a project wiki by name or ID.
type wikiToolScopeInput struct {
	TeamKey string `json:"team_key"`
	Project string `json:"project"`
}

// wikiToolScope resolves team_key or project among the teams and projects
// the user can see; anything else, hidden or missing, gives the same answer.
// It also returns the name shown on approval cards.
func (t *AssistantTools) wikiToolScope(ctx context.Context, ws uuid.UUID, input wikiToolScopeInput) (domain.WikiScope, string, error) {
	teamKey, project := strings.TrimSpace(input.TeamKey), strings.TrimSpace(input.Project)
	if (teamKey == "") == (project == "") {
		return domain.WikiScope{}, "", toolFailure("give either team_key or project")
	}
	if teamKey != "" {
		teams, err := t.teams.ListByWorkspace(ctx, ws)
		if err != nil {
			return domain.WikiScope{}, "", err
		}
		for _, team := range teams {
			if strings.EqualFold(team.Key, teamKey) {
				return domain.TeamWiki(team.ID), team.Key, nil
			}
		}
		return domain.WikiScope{}, "", toolFailure("team %q is not available; call list_teams", truncateRunes(teamKey, 16))
	}
	projects, err := t.wikiProjects.ListByWorkspace(ctx, ws)
	if err != nil {
		return domain.WikiScope{}, "", err
	}
	for _, p := range projects {
		if p.ID.String() == strings.ToLower(project) || strings.EqualFold(p.Name, project) {
			return domain.ProjectWiki(p.ID), p.Name, nil
		}
	}
	return domain.WikiScope{}, "", toolFailure("project %q is not available", truncateRunes(project, 64))
}

func wikiToolFailure(err error, action string) error {
	var invalid *WikiValidationError
	switch {
	case errors.As(err, &invalid):
		return toolFailure("%s", invalid.Error())
	case errors.Is(err, ErrWikiNotFound):
		return toolFailure("wiki page not found")
	case errors.Is(err, ErrWikiConflict):
		return toolFailure("conflict: the page changed since you read it; call get_wiki_page and redo the change on the current text")
	case errors.Is(err, ErrWikiRegistryDisabled):
		return toolFailure("the registry of this wiki is turned off; manage_wiki_registry with action enable turns it on")
	case errors.Is(err, ErrNoRegistryEntries):
		return toolFailure("nothing was logged in the registry for this period")
	case errors.Is(err, ErrAISettingsNotConfigured), errors.Is(err, ErrAIProviderRequestFailed):
		return toolFailure("%s", WikiOverviewErrorText(err))
	}
	return toolFailure("the wiki could not be %s", action)
}

func wikiPageSummary(page domain.WikiPage) map[string]any {
	summary := map[string]any{"page_id": page.ID.String(), "title": page.Title, "version": page.Version, "updated_at": page.UpdatedAt.UTC().Format(time.RFC3339)}
	if page.RegistryDate != nil {
		summary["registry_date"] = page.RegistryDate.UTC().Format(time.DateOnly)
	}
	return summary
}

func wikiSettingsSummary(settings domain.WikiSettings) map[string]any {
	s := settings.WikiOverviewSchedule
	result := map[string]any{
		"registry_enabled": settings.RegistryEnabled,
		"overview_schedule": map[string]any{
			"enabled": s.Enabled, "frequency": s.Frequency, "weekday": s.Weekday, "time": s.Time, "period_days": s.PeriodDays, "timezone": WikiOverviewTimezone.String(),
		},
	}
	if settings.NextRunAt != nil {
		result["overview_next_run_at"] = settings.NextRunAt.UTC().Format(time.RFC3339)
	}
	if settings.LastError != nil {
		result["overview_last_error"] = *settings.LastError
	}
	return result
}

func parseWikiPageID(raw string) (uuid.UUID, error) {
	id, err := uuid.Parse(strings.TrimSpace(raw))
	if err != nil {
		return uuid.Nil, toolFailure("wiki page not found")
	}
	return id, nil
}

// runWikiTool executes a wiki tool for the conversation creator, whose role
// run has already checked for wiki:read (and wiki:write for writes).
func (t *AssistantTools) runWikiTool(ctx context.Context, ws, user uuid.UUID, name, arguments string) (any, *uuid.UUID, error) {
	switch name {
	case "list_wiki_pages":
		var input wikiToolScopeInput
		if err := decodeToolInput(arguments, &input); err != nil {
			return nil, nil, err
		}
		scope, _, err := t.wikiToolScope(ctx, ws, input)
		if err != nil {
			return nil, nil, err
		}
		pages, err := t.wiki.List(ctx, ws, scope)
		if err != nil {
			return nil, nil, wikiToolFailure(err, "listed")
		}
		// Where each page sits: the same tree the wiki shows. A store
		// without a tree lists the pages flat.
		placed := map[uuid.UUID]domain.WikiTreeNode{}
		if nodes, err := t.wiki.Tree(ctx, ws, scope); err == nil {
			for _, node := range nodes {
				placed[node.ID] = node
			}
		}
		result := make([]map[string]any, 0, len(pages))
		for _, page := range pages {
			summary := wikiPageSummary(page)
			if node, ok := placed[page.ID]; ok && page.RegistryDate == nil {
				summary["kind"], summary["position"] = node.Kind, node.Position
				if node.ParentID != nil {
					summary["parent_id"] = node.ParentID.String()
				}
			}
			result = append(result, summary)
		}
		return map[string]any{"pages": result}, nil, nil
	case "get_wiki_page", "list_wiki_page_versions":
		var input struct {
			wikiToolScopeInput
			PageID string `json:"page_id"`
		}
		if err := decodeToolInput(arguments, &input); err != nil {
			return nil, nil, err
		}
		scope, _, err := t.wikiToolScope(ctx, ws, input.wikiToolScopeInput)
		if err != nil {
			return nil, nil, err
		}
		pageID, err := parseWikiPageID(input.PageID)
		if err != nil {
			return nil, nil, err
		}
		if name == "list_wiki_page_versions" {
			revisions, err := t.wiki.Revisions(ctx, ws, scope, pageID)
			if err != nil {
				return nil, nil, wikiToolFailure(err, "read")
			}
			result := make([]map[string]any, 0, len(revisions))
			for _, revision := range revisions {
				entry := map[string]any{"version_id": revision.ID.String(), "version": revision.Version, "at": revision.UpdatedAt.UTC().Format(time.RFC3339), "title_before": revision.OldTitle, "title_after": revision.NewTitle, "content_changed": revision.OldContent != revision.NewContent}
				if revision.EditedByName != nil {
					entry["by"] = *revision.EditedByName
				}
				result = append(result, entry)
			}
			return map[string]any{"versions": result}, nil, nil
		}
		page, err := t.wiki.Get(ctx, ws, scope, pageID)
		if err != nil {
			return nil, nil, wikiToolFailure(err, "read")
		}
		result := wikiPageSummary(*page)
		result["headings"] = wikiHeadings(page.Content)
		result["content"] = truncateRunes(sanitize.StripHTML(page.Content), assistantWikiReadContentRunes)
		return result, nil, nil
	case "create_wiki_page":
		var input struct {
			wikiToolScopeInput
			Title    string `json:"title"`
			Content  string `json:"content"`
			Kind     string `json:"kind"`
			ParentID string `json:"parent_id"`
		}
		if err := decodeToolInput(arguments, &input); err != nil {
			return nil, nil, err
		}
		if utf8.RuneCountInString(input.Content) > assistantWikiWriteRunes {
			return nil, nil, toolFailure("content must be at most 16000 characters")
		}
		scope, _, err := t.wikiToolScope(ctx, ws, input.wikiToolScopeInput)
		if err != nil {
			return nil, nil, err
		}
		req := dto.CreateWikiPageRequest{Title: input.Title, Content: input.Content}
		var page *domain.WikiPage
		if input.Kind == "" && input.ParentID == "" {
			page, err = t.wiki.Create(ctx, ws, scope, user, req)
		} else {
			var parent *uuid.UUID
			if input.ParentID != "" {
				id, perr := parseWikiPageID(input.ParentID)
				if perr != nil {
					return nil, nil, perr
				}
				parent = &id
			}
			page, err = t.wiki.CreateIn(ctx, ws, scope, user, req, input.Kind, parent)
		}
		if err != nil {
			return nil, nil, wikiToolFailure(err, "created")
		}
		summary := wikiPageSummary(*page)
		if input.Kind != "" {
			summary["kind"] = input.Kind
		}
		return summary, nil, nil
	case "move_wiki_page":
		var input struct {
			wikiToolScopeInput
			PageID   string  `json:"page_id"`
			ParentID *string `json:"parent_id"`
			Index    *int    `json:"index"`
		}
		if err := decodeToolInput(arguments, &input); err != nil {
			return nil, nil, err
		}
		scope, _, err := t.wikiToolScope(ctx, ws, input.wikiToolScopeInput)
		if err != nil {
			return nil, nil, err
		}
		pageID, err := parseWikiPageID(input.PageID)
		if err != nil {
			return nil, nil, err
		}
		var parent *uuid.UUID
		if input.ParentID != nil && *input.ParentID != "" {
			id, err := parseWikiPageID(*input.ParentID)
			if err != nil {
				return nil, nil, err
			}
			parent = &id
		}
		index := 1 << 30 // last
		if input.Index != nil {
			index = *input.Index
		}
		nodes, err := t.wiki.Move(ctx, ws, scope, pageID, user, parent, index)
		if err != nil {
			return nil, nil, wikiToolFailure(err, "moved")
		}
		for _, node := range nodes {
			if node.ID == pageID {
				moved := map[string]any{"page_id": pageID.String(), "position": node.Position}
				if node.ParentID != nil {
					moved["parent_id"] = node.ParentID.String()
				}
				return moved, nil, nil
			}
		}
		return map[string]any{"page_id": pageID.String()}, nil, nil
	case "publish_wiki_page":
		return t.publishWikiPage(ctx, ws, user, arguments)
	case "update_wiki_page":
		return t.updateWikiPage(ctx, ws, user, arguments)
	case "append_wiki_registry":
		var input struct {
			wikiToolScopeInput
			Text string `json:"text"`
		}
		if err := decodeToolInput(arguments, &input); err != nil {
			return nil, nil, err
		}
		scope, _, err := t.wikiToolScope(ctx, ws, input.wikiToolScopeInput)
		if err != nil {
			return nil, nil, err
		}
		page, err := t.wiki.AppendRegistry(ctx, ws, scope, user, input.Text)
		if err != nil {
			return nil, nil, wikiToolFailure(err, "updated")
		}
		return wikiPageSummary(*page), nil, nil
	case "manage_wiki_registry":
		return t.manageWikiRegistry(ctx, ws, user, arguments)
	}
	return nil, nil, toolFailure("unknown tool")
}

// publishWikiPage reads or sets a page's publication inside the workspace.
// Anything anonymous readers would notice (a public link appearing,
// changing or disappearing) goes through share_wiki_page and its approval.
func (t *AssistantTools) publishWikiPage(ctx context.Context, ws, user uuid.UUID, arguments string) (any, *uuid.UUID, error) {
	var input struct {
		wikiToolScopeInput
		PageID   string `json:"page_id"`
		Mode     string `json:"mode"`
		Children *bool  `json:"children"`
	}
	if err := decodeToolInput(arguments, &input); err != nil {
		return nil, nil, err
	}
	scope, _, err := t.wikiToolScope(ctx, ws, input.wikiToolScopeInput)
	if err != nil {
		return nil, nil, err
	}
	pageID, err := parseWikiPageID(input.PageID)
	if err != nil {
		return nil, nil, err
	}
	current, err := t.wikiPublish.State(ctx, ws, scope, pageID)
	if err != nil {
		return nil, nil, wikiToolFailure(err, "read")
	}
	if input.Mode == "status" {
		return wikiPublicationSummary(pageID, current), nil, nil
	}
	if input.Mode != domain.WikiPublishOff && input.Mode != domain.WikiPublishInternal {
		return nil, nil, toolFailure("mode must be status, off or internal; public links go through share_wiki_page")
	}
	if current.Mode == domain.WikiPublishPublic {
		return nil, nil, toolFailure("this page is public; taking it off the web needs the user's approval through share_wiki_page")
	}
	children := current.Children
	if input.Children != nil {
		children = *input.Children
	}
	workspace, member, err := t.wikiPublisherContext(ctx, ws, user)
	if err != nil {
		return nil, nil, err
	}
	state, err := t.wikiPublish.Publish(ctx, workspace, member.Role, user, scope, pageID, input.Mode, children, false)
	if err != nil {
		return nil, nil, wikiPublishFailure(err)
	}
	return wikiPublicationSummary(pageID, state), nil, nil
}

func (t *AssistantTools) wikiPublisherContext(ctx context.Context, ws, user uuid.UUID) (*domain.Workspace, *domain.WorkspaceMember, error) {
	member, err := t.members.GetMember(ctx, ws, user)
	if err != nil || member == nil || member.Role == domain.RoleGuest {
		return nil, nil, toolFailure("you no longer have access to this workspace")
	}
	workspace, err := t.wikiWorkspaces.GetByID(ctx, ws)
	if err != nil || workspace == nil {
		return nil, nil, toolFailure("you no longer have access to this workspace")
	}
	return workspace, member, nil
}

func wikiPublishFailure(err error) error {
	switch {
	case errors.Is(err, ErrWikiPublishForbidden):
		return toolFailure("your role cannot share pages publicly in this workspace")
	case errors.Is(err, ErrWikiPublishPrivate):
		return toolFailure("pages of private teams cannot be public")
	}
	return wikiToolFailure(err, "published")
}

func wikiPublicationSummary(pageID uuid.UUID, state *domain.WikiPublication) map[string]any {
	// The link itself stays out of the transcript; the user copies it in the wiki.
	return map[string]any{"page_id": pageID.String(), "mode": state.Mode, "children": state.Children, "has_public_link": state.Token != nil}
}

func (t *AssistantTools) updateWikiPage(ctx context.Context, ws, user uuid.UUID, arguments string) (any, *uuid.UUID, error) {
	var input struct {
		wikiToolScopeInput
		PageID  string  `json:"page_id"`
		Title   *string `json:"title"`
		Content *string `json:"content"`
		Mode    string  `json:"mode"`
		Heading string  `json:"heading"`
		Version int     `json:"version"`
	}
	if err := decodeToolInput(arguments, &input); err != nil {
		return nil, nil, err
	}
	if input.Content != nil && utf8.RuneCountInString(*input.Content) > assistantWikiWriteRunes {
		return nil, nil, toolFailure("content must be at most 16000 characters")
	}
	scope, _, err := t.wikiToolScope(ctx, ws, input.wikiToolScopeInput)
	if err != nil {
		return nil, nil, err
	}
	pageID, err := parseWikiPageID(input.PageID)
	if err != nil {
		return nil, nil, err
	}
	request := dto.UpdateWikiPageRequest{Title: input.Title, Version: input.Version}
	mode := input.Mode
	if mode == "" {
		mode = "replace"
	}
	if input.Content == nil {
		if input.Title == nil {
			return nil, nil, toolFailure("give a new title or content")
		}
		mode = "rename"
	}
	switch mode {
	case "rename":
	case "replace":
		if input.Version < 1 {
			return nil, nil, toolFailure("version is required to replace a page; read it with get_wiki_page")
		}
		request.Content = input.Content
	case "append", "replace_section", "insert_after_heading":
		if strings.TrimSpace(*input.Content) == "" && mode != "replace_section" {
			return nil, nil, toolFailure("content to add is required")
		}
		page, err := t.wiki.Get(ctx, ws, scope, pageID)
		if err != nil {
			return nil, nil, wikiToolFailure(err, "updated")
		}
		if request.Version < 1 {
			request.Version = page.Version
		} else if request.Version != page.Version {
			// The edit would be computed on text the model has not read.
			return nil, nil, wikiToolFailure(ErrWikiConflict, "updated")
		}
		// Convert the addition on its own so Markdown is not read as part of
		// the page's HTML.
		addition := sanitize.SanitizeEditorContent(*input.Content)
		var changed string
		ok := true
		switch mode {
		case "append":
			changed = page.Content + addition
		case "replace_section":
			changed, ok = replaceWikiSection(page.Content, input.Heading, addition)
		case "insert_after_heading":
			changed, ok = insertAfterWikiHeading(page.Content, input.Heading, addition)
		}
		if !ok {
			return nil, nil, toolFailure("heading %q is not on the page; get_wiki_page lists its headings", truncateRunes(input.Heading, 200))
		}
		request.Content = &changed
	default:
		return nil, nil, toolFailure("unknown mode")
	}
	if request.Version < 1 {
		page, err := t.wiki.Get(ctx, ws, scope, pageID)
		if err != nil {
			return nil, nil, wikiToolFailure(err, "updated")
		}
		request.Version = page.Version
	}
	page, err := t.wiki.Update(ctx, ws, scope, pageID, user, request)
	if err != nil {
		return nil, nil, wikiToolFailure(err, "updated")
	}
	return wikiPageSummary(*page), nil, nil
}

func (t *AssistantTools) manageWikiRegistry(ctx context.Context, ws, user uuid.UUID, arguments string) (any, *uuid.UUID, error) {
	var input struct {
		wikiToolScopeInput
		Action      string `json:"action"`
		Enabled     *bool  `json:"enabled"`
		Frequency   string `json:"frequency"`
		Weekday     *int   `json:"weekday"`
		Time        string `json:"time"`
		PeriodDays  int    `json:"period_days"`
		PeriodStart string `json:"period_start"`
		PeriodEnd   string `json:"period_end"`
	}
	if err := decodeToolInput(arguments, &input); err != nil {
		return nil, nil, err
	}
	scope, _, err := t.wikiToolScope(ctx, ws, input.wikiToolScopeInput)
	if err != nil {
		return nil, nil, err
	}
	switch input.Action {
	case "status":
		settings, err := t.wiki.Settings(ctx, ws, scope)
		if err != nil {
			return nil, nil, wikiToolFailure(err, "read")
		}
		return wikiSettingsSummary(settings), nil, nil
	case "enable":
		on := true
		settings, err := t.wiki.UpdateSettings(ctx, ws, scope, user, &on, nil)
		if err != nil {
			return nil, nil, wikiToolFailure(err, "updated")
		}
		return wikiSettingsSummary(settings), nil, nil
	case "set_overview_schedule":
		// Unset fields keep the saved schedule's values.
		current, err := t.wiki.Settings(ctx, ws, scope)
		if err != nil {
			return nil, nil, wikiToolFailure(err, "updated")
		}
		schedule := current.WikiOverviewSchedule
		if input.Enabled != nil {
			schedule.Enabled = *input.Enabled
		}
		if input.Frequency != "" {
			schedule.Frequency = input.Frequency
		}
		if input.Weekday != nil {
			schedule.Weekday = *input.Weekday
		}
		if input.Time != "" {
			schedule.Time = input.Time
		}
		if input.PeriodDays != 0 {
			schedule.PeriodDays = input.PeriodDays
		}
		settings, err := t.wiki.UpdateSettings(ctx, ws, scope, user, nil, &schedule)
		if err != nil {
			return nil, nil, wikiToolFailure(err, "updated")
		}
		return wikiSettingsSummary(settings), nil, nil
	case "generate_overview":
		start, startErr := time.Parse(time.DateOnly, input.PeriodStart)
		end, endErr := time.Parse(time.DateOnly, input.PeriodEnd)
		if startErr != nil || endErr != nil {
			return nil, nil, toolFailure("period_start and period_end must be dates (YYYY-MM-DD)")
		}
		overview, err := t.wiki.GenerateOverview(ctx, ws, scope, &user, start, end, domain.WikiOverviewTriggerManual)
		if err != nil {
			return nil, nil, wikiToolFailure(err, "summarised")
		}
		return map[string]any{"overview_id": overview.ID.String(), "period_start": input.PeriodStart, "period_end": input.PeriodEnd, "content": truncateRunes(sanitize.StripHTML(overview.Content), 4000)}, nil, nil
	}
	return nil, nil, toolFailure("unknown action")
}

// prepareWikiApproval validates a destructive wiki action for approval and
// binds it to what the user will see: the wiki, the page and, for a restore,
// the recorded version and the page version it applies to.
func (t *AssistantTools) prepareWikiApproval(ctx context.Context, ws, user uuid.UUID, action, arguments string) (domain.WikiApprovalPayload, error) {
	member, err := t.members.GetMember(ctx, ws, user)
	if err != nil || member == nil || member.Role == domain.RoleGuest {
		return domain.WikiApprovalPayload{}, toolFailure("you no longer have access to this workspace")
	}
	if !domain.HasPermission(member.Role, domain.PermWikiWrite) {
		return domain.WikiApprovalPayload{}, toolFailure("your role cannot edit wikis")
	}
	if t.wiki == nil || t.wikiProjects == nil {
		return domain.WikiApprovalPayload{}, toolFailure("unknown tool")
	}
	var input struct {
		wikiToolScopeInput
		PageID    string `json:"page_id"`
		VersionID string `json:"version_id"`
		State     string `json:"state"`
		Mode      string `json:"mode"`
		Children  *bool  `json:"children"`
		Rotate    bool   `json:"rotate"`
	}
	if err := decodeToolInput(arguments, &input); err != nil {
		return domain.WikiApprovalPayload{}, err
	}
	if action != wikiShareAction && (input.Mode != "" || input.Children != nil || input.Rotate) {
		return domain.WikiApprovalPayload{}, toolFailure("invalid arguments")
	}
	if action == wikiDisableRegistryAction && (input.PageID != "" || input.VersionID != "" || input.State != "") ||
		action == wikiDeleteAction && (input.VersionID != "" || input.State != "") {
		return domain.WikiApprovalPayload{}, toolFailure("invalid arguments")
	}
	scope, name, err := t.wikiToolScope(ctx, ws, input.wikiToolScopeInput)
	if err != nil {
		return domain.WikiApprovalPayload{}, err
	}
	payload := domain.WikiApprovalPayload{Wiki: scope.Kind, ScopeID: scope.ID, ScopeName: name}
	if action == wikiDisableRegistryAction {
		settings, err := t.wiki.Settings(ctx, ws, scope)
		if err != nil {
			return payload, wikiToolFailure(err, "read")
		}
		if !settings.RegistryEnabled {
			return payload, toolFailure("the registry of this wiki is already off")
		}
		return payload, nil
	}
	pageID, err := parseWikiPageID(input.PageID)
	if err != nil {
		return payload, err
	}
	page, err := t.wiki.Get(ctx, ws, scope, pageID)
	if err != nil {
		return payload, wikiToolFailure(err, "read")
	}
	payload.PageID, payload.Title, payload.Version = &page.ID, page.Title, page.Version
	if action == wikiShareAction {
		return t.prepareWikiShare(ctx, ws, user, scope, payload, input.Mode, input.Children, input.Rotate)
	}
	if action == wikiRestoreAction {
		versionID, err := uuid.Parse(strings.TrimSpace(input.VersionID))
		if err != nil {
			return payload, toolFailure("version not found; call list_wiki_page_versions")
		}
		state := input.State
		if state == "" {
			state = "after"
		}
		if state != "after" && state != "before" {
			return payload, toolFailure("state must be after or before")
		}
		revisions, err := t.wiki.Revisions(ctx, ws, scope, pageID)
		if err != nil {
			return payload, wikiToolFailure(err, "read")
		}
		for _, revision := range revisions {
			if revision.ID == versionID {
				title := revision.NewTitle
				if state == "before" {
					title = revision.OldTitle
				}
				at := revision.UpdatedAt
				payload.VersionID, payload.RestoreState, payload.RestoreTitle, payload.VersionAt = &revision.ID, state, title, &at
				return payload, nil
			}
		}
		return payload, toolFailure("version not found; call list_wiki_page_versions")
	}
	return payload, nil
}

// prepareWikiShare validates a change to what anonymous readers see before
// it is offered for approval: the page is visible, the user's role may share
// publicly when the page is or becomes public, and the change actually
// involves a public link (other changes are publish_wiki_page's).
func (t *AssistantTools) prepareWikiShare(ctx context.Context, ws, user uuid.UUID, scope domain.WikiScope, payload domain.WikiApprovalPayload, mode string, children *bool, rotate bool) (domain.WikiApprovalPayload, error) {
	if t.wikiPublish == nil || t.wikiWorkspaces == nil {
		return payload, toolFailure("unknown tool")
	}
	if mode != domain.WikiPublishPublic && mode != domain.WikiPublishInternal && mode != domain.WikiPublishOff {
		return payload, toolFailure("mode must be public, internal or off")
	}
	current, err := t.wikiPublish.State(ctx, ws, scope, *payload.PageID)
	if err != nil {
		return payload, wikiToolFailure(err, "read")
	}
	if rotate && (mode != domain.WikiPublishPublic || current.Mode != domain.WikiPublishPublic) {
		return payload, toolFailure("only a public page's link can be replaced; keep mode public")
	}
	if mode != domain.WikiPublishPublic && current.Mode != domain.WikiPublishPublic {
		return payload, toolFailure("this page is not public; use publish_wiki_page for internal publishing")
	}
	include := current.Children
	if children != nil {
		include = *children
	}
	if mode == domain.WikiPublishPublic && current.Mode == domain.WikiPublishPublic && !rotate && include == current.Children {
		return payload, toolFailure("this page is already public that way")
	}
	workspace, member, err := t.wikiPublisherContext(ctx, ws, user)
	if err != nil {
		return payload, err
	}
	if mode == domain.WikiPublishPublic && !CanCreateSharedLink(member.Role, workspace.ShareLinkMinRole) {
		return payload, toolFailure("your role cannot share pages publicly in this workspace")
	}
	payload.PublishMode, payload.PublishChildren, payload.Rotate, payload.PublishedFrom = mode, include, rotate, current.Mode
	return payload, nil
}
