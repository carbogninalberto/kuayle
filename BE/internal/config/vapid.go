package config

import (
	"bytes"
	"crypto/ecdh"
	"encoding/base64"
	"errors"
	"net/mail"
	"net/url"
	"strings"
)

type VAPIDConfig struct {
	PublicKey  string `envconfig:"VAPID_PUBLIC_KEY"`
	PrivateKey string `envconfig:"VAPID_PRIVATE_KEY"`
	Subject    string `envconfig:"VAPID_SUBJECT"`
}

func (c VAPIDConfig) Enabled() bool { return c.PublicKey != "" }

func (c VAPIDConfig) validate() error {
	if c.PublicKey == "" && c.PrivateKey == "" && c.Subject == "" {
		return nil
	}
	private, err := base64.RawURLEncoding.DecodeString(c.PrivateKey)
	if err != nil {
		return errors.New("invalid VAPID_PRIVATE_KEY")
	}
	key, err := ecdh.P256().NewPrivateKey(private)
	if err != nil {
		return errors.New("invalid VAPID_PRIVATE_KEY")
	}
	public, err := base64.RawURLEncoding.DecodeString(c.PublicKey)
	if err != nil || !bytes.Equal(key.PublicKey().Bytes(), public) {
		return errors.New("VAPID_PUBLIC_KEY must match VAPID_PRIVATE_KEY")
	}
	if subject, err := url.Parse(c.Subject); err == nil && subject.Scheme == "https" && subject.Hostname() != "" && subject.User == nil {
		return nil
	}
	address := strings.TrimPrefix(c.Subject, "mailto:")
	if contact, err := mail.ParseAddress(address); err == nil && contact.Address == address {
		return nil
	}
	return errors.New("VAPID_SUBJECT must be a contact email or HTTPS URL")
}
