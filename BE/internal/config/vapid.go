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
	if ValidVAPIDSubject(c.Subject) {
		return nil
	}
	// Existing deployments may also use a bare email address.
	if !strings.Contains(c.Subject, ":") && ValidVAPIDSubject("mailto:"+c.Subject) {
		return nil
	}
	return errors.New("VAPID_SUBJECT must be a contact email or HTTPS URL")
}

func ValidVAPIDSubject(value string) bool {
	if len(value) > 2048 {
		return false
	}
	if subject, err := url.ParseRequestURI(value); err == nil && subject.Scheme == "https" && subject.Hostname() != "" && subject.User == nil {
		return true
	}
	address, ok := strings.CutPrefix(value, "mailto:")
	if !ok {
		return false
	}
	contact, err := mail.ParseAddress(address)
	return err == nil && contact.Address == address
}
