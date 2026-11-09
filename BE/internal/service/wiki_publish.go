package service

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/kuayle/kuayle-backend/internal/repository"
)

// wikiPublishRepo is the publishing part of the wiki repository.
type wikiPublishRepo interface {
	Get(ctx context.Context, workspaceID uuid.UUID, scope domain.WikiScope, pageID uuid.UUID) (*domain.WikiPage, error)
	SetPublish(ctx context.Context, workspaceID uuid.UUID, scope domain.WikiScope, pageID uuid.UUID, mode string, children bool) error
	PublishState(ctx context.Context, workspaceID uuid.UUID, scope domain.WikiScope, pageID uuid.UUID) (*domain.WikiPublication, error)
	DeleteWikiPageLinks(ctx context.Context, workspaceID uuid.UUID, scope domain.WikiScope, pageID uuid.UUID) error
	PublishedPages(ctx context.Context, workspaceID, rootID uuid.UUID) ([]repository.PublicWikiPage, error)
	PublicPageReachable(ctx context.Context, workspaceID, pageID uuid.UUID) (bool, error)
}

var (
	// ErrWikiPublishForbidden: the workspace lets fewer roles share publicly.
	ErrWikiPublishForbidden = errors.New("your role cannot share pages publicly in this workspace")
	// ErrWikiPublishPrivate: pages of private teams cannot be public.
	ErrWikiPublishPrivate = errors.New("pages of private teams cannot be published publicly")
)

// WikiPublishService publishes wiki pages read-only: internally to members
// who can already see the wiki, or publicly through an ordinary shared link
// of scope wiki_page, so public pages use the share tokens, revocation, rate
// limits and privacy rules of issue shares.
type WikiPublishService struct {
	repo            wikiPublishRepo
	links           repository.SharedLinkRepo
	workspaces      repository.WorkspaceRepo
	assetSigningKey string
}

func NewWikiPublishService(repo wikiPublishRepo, links repository.SharedLinkRepo, workspaces repository.WorkspaceRepo, jwtKey string) *WikiPublishService {
	// Public asset links share the issue shares' signing key, so the existing
	// public asset route validates them.
	return &WikiPublishService{repo: repo, links: links, workspaces: workspaces, assetSigningKey: publicAssetKey(jwtKey)}
}

// State reads a visible page's publication.
func (s *WikiPublishService) State(ctx context.Context, workspaceID uuid.UUID, scope domain.WikiScope, pageID uuid.UUID) (*domain.WikiPublication, error) {
	state, err := s.repo.PublishState(ctx, workspaceID, scope, pageID)
	return state, wikiError(err)
}

// Publish sets a page's publication. Going public needs the workspace's
// share role and a team that is not private; leaving public deletes the
// link, so its token stops working at once. rotate replaces a public link
// with a new token.
func (s *WikiPublishService) Publish(ctx context.Context, ws *domain.Workspace, role string, userID uuid.UUID, scope domain.WikiScope, pageID uuid.UUID, mode string, children, rotate bool) (*domain.WikiPublication, error) {
	if mode != domain.WikiPublishOff && mode != domain.WikiPublishInternal && mode != domain.WikiPublishPublic {
		return nil, &WikiValidationError{Field: "mode", Message: "must be off, internal or public"}
	}
	before, err := s.repo.PublishState(ctx, ws.ID, scope, pageID)
	if err != nil {
		return nil, wikiError(err)
	}
	if mode == domain.WikiPublishPublic && !CanCreateSharedLink(role, ws.ShareLinkMinRole) {
		return nil, ErrWikiPublishForbidden
	}
	if mode != domain.WikiPublishPublic || rotate {
		if err := s.repo.DeleteWikiPageLinks(ctx, ws.ID, scope, pageID); err != nil {
			return nil, err
		}
	}
	if err := s.repo.SetPublish(ctx, ws.ID, scope, pageID, mode, children); err != nil {
		return nil, wikiError(err)
	}
	if mode == domain.WikiPublishPublic {
		state, err := s.repo.PublishState(ctx, ws.ID, scope, pageID)
		if err != nil {
			return nil, wikiError(err)
		}
		if state.LinkID == nil {
			if err := s.createLink(ctx, ws.ID, userID, pageID); err != nil {
				// Restore the previous publication; a page that cannot have a
				// public link is never left marked public.
				_ = s.repo.SetPublish(ctx, ws.ID, scope, pageID, before.Mode, before.Children)
				return nil, err
			}
		}
	}
	state, err := s.repo.PublishState(ctx, ws.ID, scope, pageID)
	return state, wikiError(err)
}

func (s *WikiPublishService) createLink(ctx context.Context, workspaceID, userID, pageID uuid.UUID) error {
	token, err := generateToken()
	if err != nil {
		return fmt.Errorf("failed to generate token")
	}
	id := pageID
	link := &domain.SharedLink{ID: uuid.New(), Token: token, WorkspaceID: workspaceID, CreatedBy: userID, Scope: "wiki_page",
		ScopeID: &id, Filters: []byte(`{}`), IsActive: true}
	// The insert's visibility predicate requires the page to be public and
	// readable anonymously, so a private team's page is refused here.
	err = s.links.Create(ctx, link)
	if err != nil && (errors.Is(err, repository.ErrPrivateWorkspaceOperation) || errors.Is(err, sql.ErrNoRows)) {
		return ErrWikiPublishPrivate
	}
	return err
}

// PublicWiki is what a public link shows: the published page, and its
// nested pages when it publishes them. Content asset URLs are signed for
// one hour and bound to the page they appear in.
type PublicWiki struct {
	WorkspaceName string
	RootID        uuid.UUID
	Pages         []repository.PublicWikiPage
}

// ResolvePublic returns the pages a public token publishes, or not found
// when the token is unknown, revoked, expired or the page is no longer public.
func (s *WikiPublishService) ResolvePublic(ctx context.Context, token string) (*PublicWiki, error) {
	ctx = domain.PublicContext(ctx)
	link, err := s.links.GetByToken(ctx, token)
	if err != nil || link == nil || !link.IsActive || link.Scope != "wiki_page" || link.ScopeID == nil ||
		(link.ExpiresAt != nil && link.ExpiresAt.Before(time.Now())) {
		return nil, ErrWikiNotFound
	}
	ws, err := s.workspaces.GetByID(ctx, link.WorkspaceID)
	if err != nil || ws == nil {
		return nil, ErrWikiNotFound
	}
	pages, err := s.repo.PublishedPages(ctx, link.WorkspaceID, *link.ScopeID)
	if err != nil {
		return nil, err
	}
	if len(pages) == 0 {
		return nil, ErrWikiNotFound
	}
	for i := range pages {
		pages[i].Content = signPublicAssetURLs(pages[i].Content, s.assetSigningKey, link.WorkspaceID, pages[i].ID)
	}
	return &PublicWiki{WorkspaceName: ws.Name, RootID: *link.ScopeID, Pages: pages}, nil
}

// PublicPageReachable lets the public asset route serve a file signed for a
// wiki page only while that page is still publicly published.
func (s *WikiPublishService) PublicPageReachable(ctx context.Context, workspaceID, pageID uuid.UUID) bool {
	ok, err := s.repo.PublicPageReachable(domain.PublicContext(ctx), workspaceID, pageID)
	return err == nil && ok
}

// publicAssetKey derives the signing key of public asset links from the
// server's JWT signing key, as SharedLinkService does.
func publicAssetKey(jwtSigningKey string) string { return jwtSigningKey + ":prompt-assets" }
