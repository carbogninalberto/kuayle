package service

import (
	"context"
	"crypto/hmac"
	"database/sql"
	"encoding/json"
	"errors"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/kuayle/kuayle-backend/internal/config"
	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/kuayle/kuayle-backend/internal/repository"
	"github.com/kuayle/kuayle-backend/pkg/crypto"
	"github.com/kuayle/kuayle-backend/pkg/slack"
	log "github.com/sirupsen/logrus"
)

var ErrSlackDisabled = errors.New("Slack app credentials are not configured")
var ErrSlackState = errors.New("Slack authorization expired or does not match this session")
var ErrSlackInput = errors.New("invalid Slack request")
var ErrSlackSignature = errors.New("invalid Slack signature")

type SlackService struct {
	repo   *repository.SlackRepository
	app    config.SlackConfig
	client *slack.Client
	key    []byte
}

func NewSlackService(repo *repository.SlackRepository, app config.SlackConfig, key []byte, frontend string) *SlackService {
	return &SlackService{repo, app, slack.NewClient(app.ClientID, app.ClientSecret, strings.TrimRight(frontend, "/")+"/integrations/slack/callback"), key}
}

type SlackStatus struct {
	Configured bool                  `json:"configured"`
	Install    *domain.SlackInstall  `json:"install"`
	Bindings   []domain.SlackBinding `json:"bindings"`
}

func (s *SlackService) Status(ctx context.Context, ws uuid.UUID) (SlackStatus, error) {
	status := SlackStatus{Configured: s.app.Configured(), Bindings: []domain.SlackBinding{}}
	install, err := s.repo.Get(ctx, ws)
	if errors.Is(err, sql.ErrNoRows) {
		return status, nil
	}
	if err != nil {
		return status, err
	}
	status.Install = &install
	status.Bindings, err = s.repo.Bindings(ctx, ws)
	return status, err
}

func (s *SlackService) Start(ctx context.Context, ws, user uuid.UUID) (string, error) {
	if !s.app.Configured() {
		return "", ErrSlackDisabled
	}
	_, err := s.repo.Get(ctx, ws)
	if err == nil {
		return "", repository.ErrSlackConnected
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return "", err
	}
	state := uuid.New()
	if err = s.repo.BeginInstall(ctx, ws, user, state); err != nil {
		return "", err
	}
	return s.client.InstallURL(state.String()), nil
}

func (s *SlackService) Connect(ctx context.Context, ws, user, state uuid.UUID, code string) error {
	if !s.app.Configured() {
		return ErrSlackDisabled
	}
	if err := s.repo.ConsumeState(ctx, ws, user, state); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ErrSlackState
		}
		return err
	}
	auth, err := s.client.Exchange(ctx, code)
	if err != nil {
		return err
	}
	ciphertext, err := crypto.Encrypt(auth.Token, s.key)
	if err != nil {
		return err
	}
	return s.repo.Install(ctx, domain.SlackInstall{ID: uuid.New(), WorkspaceID: ws, TeamID: auth.Team.ID, Name: auth.Team.Name, Token: ciphertext, Scope: auth.Scope}, user, auth.User.ID)
}

func (s *SlackService) bot(ctx context.Context, ws uuid.UUID) (domain.SlackInstall, string, error) {
	install, err := s.repo.Get(ctx, ws)
	if err != nil {
		return install, "", err
	}
	token, err := crypto.Decrypt(install.Token, s.key)
	return install, token, err
}

func (s *SlackService) Disconnect(ctx context.Context, ws uuid.UUID) error {
	if !s.app.Configured() {
		return ErrSlackDisabled
	}
	i, token, err := s.bot(ctx, ws)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	err = s.client.Uninstall(ctx, token)
	if err != nil && !errors.Is(err, slack.ErrRevoked) {
		return err
	}
	return s.repo.Remove(ctx, i.ID)
}

func (s *SlackService) Channels(ctx context.Context, ws uuid.UUID) ([]slack.Channel, error) {
	_, token, err := s.bot(ctx, ws)
	if err != nil {
		return nil, err
	}
	return s.client.Channels(ctx, token)
}

func (s *SlackService) SetBinding(ctx context.Context, ws, user uuid.UUID, b domain.SlackBinding) error {
	if (b.TeamID == nil) == (b.ProjectID == nil) {
		return ErrSlackInput
	}
	i, token, err := s.bot(ctx, ws)
	if err != nil {
		return err
	}
	if b.ChannelID != "" {
		channels, err := s.client.Channels(ctx, token)
		if err != nil {
			return err
		}
		for _, ch := range channels {
			if ch.ID == b.ChannelID {
				return s.repo.SetBinding(ctx, i, user, b)
			}
		}
		return ErrSlackInput
	}
	return s.repo.SetBinding(ctx, i, user, b)
}

func (s *SlackService) Event(ctx context.Context, ts, sig string, body []byte) (string, error) {
	sec, err := strconv.ParseInt(ts, 10, 64)
	now := time.Now().Unix()
	if err != nil || sec < now-300 || sec > now+300 || s.app.SigningSecret == "" {
		return "", ErrSlackSignature
	}
	signed := append([]byte("v0:"+ts+":"), body...)
	expected := "v0=" + crypto.HMACSha256(signed, s.app.SigningSecret)
	if !hmac.Equal([]byte(expected), []byte(sig)) {
		return "", ErrSlackSignature
	}
	var event struct {
		Type, Challenge string
		TeamID          string `json:"team_id"`
		EventID         string `json:"event_id"`
		Event           struct{ Type string }
	}
	if json.Unmarshal(body, &event) != nil || event.Type == "" {
		return "", ErrSlackInput
	}
	switch event.Type {
	case "url_verification":
		if event.Challenge == "" {
			return "", ErrSlackInput
		}
		return event.Challenge, nil
	case "event_callback":
		if event.TeamID == "" || event.EventID == "" || event.Event.Type == "" {
			return "", ErrSlackInput
		}
		if event.Event.Type == "app_uninstalled" {
			receiptCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
			defer cancel()
			return "", s.repo.EnqueueUninstall(receiptCtx, event.TeamID, event.EventID)
		}
	}
	return "", nil
}

func (s *SlackService) Run(ctx context.Context) {
	if !s.app.Configured() {
		return
	}
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := s.processUninstall(ctx); err != nil && !errors.Is(err, sql.ErrNoRows) && ctx.Err() == nil {
				log.WithError(err).Warn("Slack uninstall check failed")
			}
		}
	}
}

func (s *SlackService) processUninstall(ctx context.Context) error {
	e, err := s.repo.ClaimUninstall(ctx)
	if err != nil {
		return err
	}
	token, err := crypto.Decrypt(e.Install.Token, s.key)
	if err != nil {
		return err
	}
	// Slack can redeliver an old uninstall after a new install for the same
	// team. Verify that this exact installation has lost its bot authorization.
	err = s.client.CheckToken(ctx, token)
	revoked := errors.Is(err, slack.ErrRevoked) || errors.Is(err, slack.ErrInvalidAuth)
	if err != nil && !revoked {
		return err
	}
	return s.repo.FinishUninstall(ctx, e, revoked)
}
