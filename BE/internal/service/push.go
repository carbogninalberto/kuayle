package service

import (
	"context"
	"crypto/ecdh"
	"encoding/base64"
	"errors"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	webpush "github.com/SherClockHolmes/webpush-go"
	"github.com/google/uuid"
	"github.com/kuayle/kuayle-backend/internal/config"
	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/kuayle/kuayle-backend/internal/dto"
	"github.com/kuayle/kuayle-backend/internal/repository"
	"github.com/kuayle/kuayle-backend/pkg/validate"
	log "github.com/sirupsen/logrus"
)

var (
	ErrPushUnavailable = errors.New("web push is not configured")
	ErrPushInvalid     = errors.New("invalid push subscription")
	ErrPushForbidden   = errors.New("push enrollment requires access to the workspace")
)

type pushStore interface {
	Register(context.Context, domain.PushSubscription, *uuid.UUID) (uuid.UUID, error)
	Delete(context.Context, uuid.UUID, uuid.UUID, *uuid.UUID) error
	ListByUser(context.Context, uuid.UUID) ([]domain.PushSubscription, error)
	RecordDelivery(context.Context, domain.PushSubscription, int) error
}

type pushWorkspaces interface {
	GetByID(context.Context, uuid.UUID) (*domain.Workspace, error)
	GetBySlug(context.Context, string) (*domain.Workspace, error)
	GetMember(context.Context, uuid.UUID, uuid.UUID) (*domain.WorkspaceMember, error)
}

type pushAccess struct {
	workspaces    pushWorkspaces
	requiresSSO   func(context.Context, uuid.UUID) (bool, error)
	authorizeChat func(context.Context, uuid.UUID, uuid.UUID) error
	notifications *repository.NotificationRepository
}

type PushService struct {
	publicKey string
	store     pushStore
	prefs     *PreferencesService
	access    pushAccess
	queue     chan pushJob
	send      func(context.Context, domain.PushSubscription, []byte) (int, error)
}

func NewPushService(cfg config.VAPIDConfig, store pushStore, prefs *PreferencesService, workspaces pushWorkspaces,
	requiresSSO func(context.Context, uuid.UUID) (bool, error), notifications *repository.NotificationRepository,
	authorizeChat func(context.Context, uuid.UUID, uuid.UUID) error) *PushService {
	s := &PushService{publicKey: cfg.PublicKey, store: store, prefs: prefs,
		access: pushAccess{workspaces, requiresSSO, authorizeChat, notifications}}
	if !cfg.Enabled() {
		return s
	}
	s.queue = make(chan pushJob, 128)
	dialer := &net.Dialer{Control: validate.PublicAddressControl}
	options := &webpush.Options{
		HTTPClient: &http.Client{
			Timeout: 4 * time.Second,
			// Direct HTTPS with public DNS results only. Redirects must never carry the VAPID token.
			Transport:     &http.Transport{DialContext: dialer.DialContext, IdleConnTimeout: time.Minute},
			CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
		},
		Subscriber: strings.TrimPrefix(cfg.Subject, "mailto:"), VAPIDPublicKey: cfg.PublicKey,
		VAPIDPrivateKey: cfg.PrivateKey, TTL: 300,
	}
	s.send = func(ctx context.Context, sub domain.PushSubscription, payload []byte) (int, error) {
		response, err := webpush.SendNotificationWithContext(ctx, payload, &webpush.Subscription{
			Endpoint: sub.Endpoint, Keys: webpush.Keys{P256dh: sub.P256DH, Auth: sub.Auth},
		}, options)
		if err != nil {
			return 0, err
		}
		defer response.Body.Close()
		return response.StatusCode, nil
	}
	return s
}

func (s *PushService) PublicKey() string { return s.publicKey }

func (s *PushService) Register(ctx context.Context, userID, ssoWorkspace uuid.UUID, userAgent string, req dto.RegisterPushSubscriptionRequest) (uuid.UUID, error) {
	if s.publicKey == "" {
		return uuid.Nil, ErrPushUnavailable
	}
	if !validPushSubscription(req) {
		return uuid.Nil, ErrPushInvalid
	}
	workspace, err := s.access.workspaces.GetBySlug(ctx, req.WorkspaceSlug)
	if err != nil {
		return uuid.Nil, err
	}
	if workspace == nil || (ssoWorkspace != uuid.Nil && ssoWorkspace != workspace.ID) {
		return uuid.Nil, ErrPushForbidden
	}
	member, err := s.access.workspaces.GetMember(ctx, workspace.ID, userID)
	if err != nil {
		return uuid.Nil, err
	}
	if member == nil {
		return uuid.Nil, ErrPushForbidden
	}
	if ssoWorkspace == uuid.Nil && member.Role != domain.RoleOwner {
		required, err := s.access.requiresSSO(ctx, workspace.ID)
		if err != nil {
			return uuid.Nil, err
		}
		if required {
			return uuid.Nil, ErrPushForbidden
		}
	}
	return s.store.Register(ctx, domain.PushSubscription{
		UserID: userID, WorkspaceID: workspace.ID, Endpoint: req.Endpoint, P256DH: req.P256DH, Auth: req.Auth,
		UserAgent: truncateRunes(userAgent, 512), SSOScoped: ssoWorkspace != uuid.Nil,
	}, req.ReplaceID)
}

func (s *PushService) Delete(ctx context.Context, userID, ssoWorkspace, id uuid.UUID) error {
	var scope *uuid.UUID
	if ssoWorkspace != uuid.Nil {
		scope = &ssoWorkspace
	}
	return s.store.Delete(ctx, userID, id, scope)
}

func validPushSubscription(req dto.RegisterPushSubscriptionRequest) bool {
	if len(req.Endpoint) > 2048 || req.WorkspaceSlug == "" || len(req.WorkspaceSlug) > 50 ||
		len(req.P256DH) > 88 || len(req.Auth) > 24 {
		return false
	}
	endpoint, err := url.Parse(req.Endpoint)
	if err != nil || endpoint.Scheme != "https" || endpoint.Hostname() == "" ||
		endpoint.User != nil || endpoint.Fragment != "" || (endpoint.Port() != "" && endpoint.Port() != "443") {
		return false
	}
	if ip := net.ParseIP(endpoint.Hostname()); ip != nil && !validate.IsPublicIP(ip) {
		return false
	}
	public, err := base64.RawURLEncoding.DecodeString(strings.TrimRight(req.P256DH, "="))
	if err != nil {
		return false
	}
	if _, err := ecdh.P256().NewPublicKey(public); err != nil {
		return false
	}
	auth, err := base64.RawURLEncoding.DecodeString(strings.TrimRight(req.Auth, "="))
	return err == nil && len(auth) == 16
}

func (s *PushService) deliver(ctx context.Context, sub domain.PushSubscription, payload []byte) {
	status, err := s.send(ctx, sub, payload)
	if ctx.Err() == nil && (err != nil || status >= 500) {
		status, _ = s.send(ctx, sub, payload)
	}
	if err := s.store.RecordDelivery(ctx, sub, status); err != nil && ctx.Err() == nil {
		log.WithError(err).Warn("Could not record web push delivery")
	}
}
