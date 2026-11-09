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

// assistantWiki is the wiki service as the assistant uses it. Every call
// applies the conversation creator's visibility of the wiki's team or project.
type assistantWiki interface {
	List(ctx context.Context, workspaceID uuid.UUID, scope domain.WikiScope) ([]domain.WikiPage, error)
	Get(ctx context.Context, workspaceID uuid.UUID, scope domain.WikiScope, pageID uuid.UUID) (*domain.WikiPage, error)
	Create(ctx context.Context, workspaceID uuid.UUID, scope domain.WikiScope, userID uuid.UUID, req dto.CreateWikiPageRequest) (*domain.WikiPage, error)
	Update(ctx context.Context, workspaceID uuid.UUID, scope domain.WikiScope, pageID, userID uuid.UUID, req dto.UpdateWikiPageRequest) (*domain.WikiPage, error)
	AppendRegistry(ctx context.Context, workspaceID uuid.UUID, scope domain.WikiScope, userID uuid.UUID, text string) (*domain.WikiPage, error)
}

type assistantProjects interface {
	ListByWorkspace(ctx context.Context, workspaceID uuid.UUID) ([]domain.Project, error)
}

// SetWiki enables the wiki tools.
func (t *AssistantTools) SetWiki(wiki assistantWiki, projects assistantProjects) {
	t.wiki, t.wikiProjects = wiki, projects
}

const wikiScopeParams = `"team_key":{"type":"string","maxLength":16,"description":"Key of the team whose wiki to use (see list_teams)."},"project":{"type":"string","maxLength":200,"description":"Name or ID of the project whose wiki to use, instead of team_key."}`

// assistantWikiReadContentRunes bounds a page's text in get_wiki_page.
const assistantWikiReadContentRunes = 12000

var assistantWikiToolCatalog = []assistantTool{
	{name: "list_wiki_pages", wiki: true, description: "List the pages of a team's or project's wiki the user can see, newest first, with their IDs and versions. Registry pages (the wiki's daily log) carry registry_date. Give team_key or project.", parameters: `{"type":"object","properties":{` + wikiScopeParams + `},"additionalProperties":false}`},
	{name: "get_wiki_page", wiki: true, description: "Read one wiki page as text, with its version (needed to update it). Give team_key or project and the page_id from list_wiki_pages.", parameters: `{"type":"object","properties":{` + wikiScopeParams + `,"page_id":{"type":"string","maxLength":36}},"required":["page_id"],"additionalProperties":false}`},
	{name: "create_wiki_page", wiki: true, write: true, description: "Create a page in a team's or project's wiki. content is Markdown or simple HTML. Only call this when the user asked to write to the wiki.", parameters: `{"type":"object","properties":{` + wikiScopeParams + `,"title":{"type":"string","minLength":1,"maxLength":200},"content":{"type":"string","maxLength":16000}},"required":["title"],"additionalProperties":false}`},
	{name: "update_wiki_page", wiki: true, write: true, description: "Change a wiki page: a new title, and content (Markdown or simple HTML) that replaces the page (mode \"replace\", needs the version from get_wiki_page) or is added at its end (mode \"append\"). A stale version fails because someone else changed the page: read it again. Only call this when the user asked to change the page.", parameters: `{"type":"object","properties":{` + wikiScopeParams + `,"page_id":{"type":"string","maxLength":36},"title":{"type":"string","minLength":1,"maxLength":200},"content":{"type":"string","maxLength":16000},"mode":{"type":"string","enum":["replace","append"]},"version":{"type":"integer","minimum":1}},"required":["page_id"],"additionalProperties":false}`},
	{name: "append_wiki_registry", wiki: true, write: true, description: "Add one short plain-text line to today's page of a wiki's registry, the team's or project's daily log of what happened (for example \"KUA-20 implemented, PR #161 opened\"). Kuayle adds the time and picks today's page. Only works when the wiki's registry is turned on; use it when the user asks to log something or after finishing work they asked you to record.", parameters: `{"type":"object","properties":{` + wikiScopeParams + `,"text":{"type":"string","minLength":1,"maxLength":2000}},"required":["text"],"additionalProperties":false}`},
}

// wikiToolScope resolves team_key or project among the teams and projects
// the user can see; anything else, hidden or missing, gives the same answer.
func (t *AssistantTools) wikiToolScope(ctx context.Context, ws uuid.UUID, teamKey, project string) (domain.WikiScope, error) {
	teamKey, project = strings.TrimSpace(teamKey), strings.TrimSpace(project)
	if (teamKey == "") == (project == "") {
		return domain.WikiScope{}, toolFailure("give either team_key or project")
	}
	if teamKey != "" {
		teams, err := t.teams.ListByWorkspace(ctx, ws)
		if err != nil {
			return domain.WikiScope{}, err
		}
		for _, team := range teams {
			if strings.EqualFold(team.Key, teamKey) {
				return domain.TeamWiki(team.ID), nil
			}
		}
		return domain.WikiScope{}, toolFailure("team %q is not available; call list_teams", truncateRunes(teamKey, 16))
	}
	projects, err := t.wikiProjects.ListByWorkspace(ctx, ws)
	if err != nil {
		return domain.WikiScope{}, err
	}
	for _, p := range projects {
		if p.ID.String() == strings.ToLower(project) || strings.EqualFold(p.Name, project) {
			return domain.ProjectWiki(p.ID), nil
		}
	}
	return domain.WikiScope{}, toolFailure("project %q is not available", truncateRunes(project, 64))
}

func wikiToolFailure(err error, action string) error {
	var invalid *WikiValidationError
	switch {
	case errors.As(err, &invalid):
		return toolFailure("%s", invalid.Error())
	case errors.Is(err, ErrWikiNotFound):
		return toolFailure("wiki page not found")
	case errors.Is(err, ErrWikiConflict):
		return toolFailure("the page changed since you read it; call get_wiki_page and try again")
	case errors.Is(err, ErrWikiRegistryDisabled):
		return toolFailure("the registry of this wiki is turned off; someone who can edit the wiki can turn it on in the wiki")
	}
	return toolFailure("the wiki page could not be %s", action)
}

func wikiPageSummary(page domain.WikiPage) map[string]any {
	summary := map[string]any{"page_id": page.ID.String(), "title": page.Title, "version": page.Version, "updated_at": page.UpdatedAt.UTC().Format(time.RFC3339)}
	if page.RegistryDate != nil {
		summary["registry_date"] = page.RegistryDate.UTC().Format(time.DateOnly)
	}
	return summary
}

// runWikiTool executes a wiki tool for the conversation creator, whose role
// run has already checked for wiki:read (and wiki:write for writes).
func (t *AssistantTools) runWikiTool(ctx context.Context, ws, user uuid.UUID, name, arguments string) (any, *uuid.UUID, error) {
	var input struct {
		TeamKey string  `json:"team_key"`
		Project string  `json:"project"`
		PageID  string  `json:"page_id"`
		Title   *string `json:"title"`
		Content *string `json:"content"`
		Mode    string  `json:"mode"`
		Version int     `json:"version"`
		Text    string  `json:"text"`
	}
	if err := decodeToolInput(arguments, &input); err != nil {
		return nil, nil, err
	}
	// Each tool accepts only its own fields.
	allowed := map[string]bool{
		"list_wiki_pages":      input.PageID == "" && input.Title == nil && input.Content == nil && input.Mode == "" && input.Version == 0 && input.Text == "",
		"get_wiki_page":        input.Title == nil && input.Content == nil && input.Mode == "" && input.Version == 0 && input.Text == "",
		"create_wiki_page":     input.PageID == "" && input.Mode == "" && input.Version == 0 && input.Text == "",
		"update_wiki_page":     input.Text == "",
		"append_wiki_registry": input.PageID == "" && input.Title == nil && input.Content == nil && input.Mode == "" && input.Version == 0,
	}
	if !allowed[name] {
		return nil, nil, toolFailure("invalid arguments")
	}
	if input.Content != nil && utf8.RuneCountInString(*input.Content) > 16000 {
		return nil, nil, toolFailure("content must be at most 16000 characters")
	}
	scope, err := t.wikiToolScope(ctx, ws, input.TeamKey, input.Project)
	if err != nil {
		return nil, nil, err
	}
	var pageID uuid.UUID
	if name == "get_wiki_page" || name == "update_wiki_page" {
		if pageID, err = uuid.Parse(strings.TrimSpace(input.PageID)); err != nil {
			return nil, nil, toolFailure("wiki page not found")
		}
	}
	switch name {
	case "list_wiki_pages":
		pages, err := t.wiki.List(ctx, ws, scope)
		if err != nil {
			return nil, nil, wikiToolFailure(err, "listed")
		}
		result := make([]map[string]any, 0, len(pages))
		for _, page := range pages {
			result = append(result, wikiPageSummary(page))
		}
		return map[string]any{"pages": result}, nil, nil
	case "get_wiki_page":
		page, err := t.wiki.Get(ctx, ws, scope, pageID)
		if err != nil {
			return nil, nil, wikiToolFailure(err, "read")
		}
		result := wikiPageSummary(*page)
		result["content"] = truncateRunes(sanitize.StripHTML(page.Content), assistantWikiReadContentRunes)
		return result, nil, nil
	case "create_wiki_page":
		if input.Title == nil {
			return nil, nil, toolFailure("title is required")
		}
		request := dto.CreateWikiPageRequest{Title: *input.Title}
		if input.Content != nil {
			request.Content = *input.Content
		}
		page, err := t.wiki.Create(ctx, ws, scope, user, request)
		if err != nil {
			return nil, nil, wikiToolFailure(err, "created")
		}
		return wikiPageSummary(*page), nil, nil
	case "update_wiki_page":
		request := dto.UpdateWikiPageRequest{Title: input.Title, Version: input.Version}
		switch input.Mode {
		case "", "replace":
			if input.Content != nil {
				request.Content = input.Content
			}
			if input.Version < 1 {
				return nil, nil, toolFailure("version is required; read it with get_wiki_page")
			}
		case "append":
			if input.Content == nil || strings.TrimSpace(*input.Content) == "" {
				return nil, nil, toolFailure("content to append is required")
			}
			page, err := t.wiki.Get(ctx, ws, scope, pageID)
			if err != nil {
				return nil, nil, wikiToolFailure(err, "updated")
			}
			if request.Version < 1 {
				request.Version = page.Version
			}
			// Convert the addition on its own so Markdown is not read as
			// part of the page's HTML.
			combined := page.Content + sanitize.SanitizeEditorContent(*input.Content)
			request.Content = &combined
		default:
			return nil, nil, toolFailure("mode must be replace or append")
		}
		page, err := t.wiki.Update(ctx, ws, scope, pageID, user, request)
		if err != nil {
			return nil, nil, wikiToolFailure(err, "updated")
		}
		return wikiPageSummary(*page), nil, nil
	case "append_wiki_registry":
		page, err := t.wiki.AppendRegistry(ctx, ws, scope, user, input.Text)
		if err != nil {
			return nil, nil, wikiToolFailure(err, "updated")
		}
		return wikiPageSummary(*page), nil, nil
	}
	return nil, nil, toolFailure("unknown tool")
}
