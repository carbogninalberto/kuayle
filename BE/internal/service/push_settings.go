package service

import (
	"context"
	"encoding/json"
	"errors"
	"strings"

	webpush "github.com/SherClockHolmes/webpush-go"
	"github.com/google/uuid"
	"github.com/kuayle/kuayle-backend/internal/config"
	"github.com/kuayle/kuayle-backend/internal/dto"
)

const pushSettingKey = "web_push"

var (
	ErrPushReadOnly = errors.New("Web Push is configured by the environment")
	ErrPushConfirm  = errors.New("confirm key regeneration: every device must enable push again")
	ErrPushSubject  = errors.New("enter a mailto: address or HTTPS contact URL")
	ErrPushNoKeys   = errors.New("generate a key pair before enabling Web Push")
)

type instanceSettings interface {
	Get(context.Context, string) (string, error)
	Update(context.Context, string, bool, func(string) (string, error)) error
}

// The whole configuration is one encrypted setting so readers always see a matching pair.
type pushConfiguration struct {
	PublicKey  string `json:"public_key"`
	PrivateKey string `json:"private_key"`
	Subject    string `json:"subject"`
	Enabled    bool   `json:"enabled"`
}

func (p pushConfiguration) vapid() config.VAPIDConfig {
	return config.VAPIDConfig{PublicKey: p.PublicKey, PrivateKey: p.PrivateKey, Subject: p.Subject}
}

func decodePushSetting(raw string) (pushConfiguration, error) {
	var p pushConfiguration
	if raw == "" {
		return p, nil
	}
	err := json.Unmarshal([]byte(raw), &p)
	return p, err
}

func (s *PushService) effective(ctx context.Context) (pushConfiguration, error) {
	if s.env.Enabled() {
		return pushConfiguration{PublicKey: s.env.PublicKey, PrivateKey: s.env.PrivateKey, Subject: s.env.Subject, Enabled: true}, nil
	}
	raw, err := s.settings.Get(ctx, pushSettingKey)
	if err != nil {
		return pushConfiguration{}, err
	}
	return decodePushSetting(raw)
}

func (s *PushService) status(p pushConfiguration, devices int) dto.PushSettings {
	source := "off"
	if s.env.Enabled() {
		source = "environment"
	} else if p.PublicKey != "" || p.Subject != "" {
		source = "app"
	}
	return dto.PushSettings{
		Source: source, ReadOnly: s.env.Enabled(), PublicKey: p.PublicKey,
		Subject: p.Subject, Enabled: p.Enabled, SubscribedDevices: devices,
	}
}

func (s *PushService) Settings(ctx context.Context, user uuid.UUID) (dto.PushSettings, error) {
	p, err := s.effective(ctx)
	if err != nil {
		return dto.PushSettings{}, err
	}
	devices, err := s.store.CountByUser(ctx, user)
	if err != nil {
		return dto.PushSettings{}, err
	}
	return s.status(p, devices), nil
}

func (s *PushService) changeSettings(ctx context.Context, user uuid.UUID, change func(*pushConfiguration) error) (dto.PushSettings, error) {
	if s.env.Enabled() {
		return dto.PushSettings{}, ErrPushReadOnly
	}
	devices, err := s.store.CountByUser(ctx, user)
	if err != nil {
		return dto.PushSettings{}, err
	}
	var updated pushConfiguration
	err = s.settings.Update(ctx, pushSettingKey, true, func(raw string) (string, error) {
		p, err := decodePushSetting(raw)
		if err != nil {
			return "", err
		}
		if err := change(&p); err != nil {
			return "", err
		}
		updated = p
		data, err := json.Marshal(p)
		return string(data), err
	})
	if err != nil {
		return dto.PushSettings{}, err
	}
	return s.status(updated, devices), nil
}

func (s *PushService) GenerateKeys(ctx context.Context, user uuid.UUID, confirm bool) (dto.PushSettings, error) {
	var rotated bool
	status, err := s.changeSettings(ctx, user, func(p *pushConfiguration) error {
		rotated = p.PublicKey != ""
		if rotated && !confirm {
			return ErrPushConfirm
		}
		private, public, err := webpush.GenerateVAPIDKeys()
		if err != nil {
			return err
		}
		p.PrivateKey, p.PublicKey = private, public
		return nil
	})
	status.RequiresReenrollment = rotated && err == nil
	return status, err
}

func (s *PushService) UpdateSettings(ctx context.Context, user uuid.UUID, subject string, enabled bool) (dto.PushSettings, error) {
	return s.changeSettings(ctx, user, func(p *pushConfiguration) error {
		subject = strings.TrimSpace(subject)
		if (enabled || subject != "") && !config.ValidVAPIDSubject(subject) {
			return ErrPushSubject
		}
		if enabled && p.PublicKey == "" {
			return ErrPushNoKeys
		}
		p.Subject, p.Enabled = subject, enabled
		return nil
	})
}

func (s *PushService) Test(ctx context.Context, user uuid.UUID) (dto.PushTestResult, error) {
	var result dto.PushTestResult
	p, err := s.effective(ctx)
	if err != nil {
		return result, err
	}
	if !p.Enabled {
		return result, ErrPushUnavailable
	}
	subs, err := s.store.ListByUser(ctx, user)
	if err != nil {
		return result, err
	}
	payload := []byte(`{"title":"Kuayle","body":"Test notification","tag":"kuayle-push-test","data":{"url":"/"}}`)
	for _, sub := range subs {
		if err := ctx.Err(); err != nil {
			return result, err
		}
		if s.deliver(ctx, p.vapid(), sub, payload) {
			result.Sent++
		} else {
			result.Failed++
		}
	}
	return result, nil
}
