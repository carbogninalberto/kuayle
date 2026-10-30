// Package mail sends transactional email over SMTP.
package mail

import (
	"fmt"
	"net/smtp"
	"strings"
)

type Config struct {
	Host     string `envconfig:"SMTP_HOST"`
	Port     int    `envconfig:"SMTP_PORT" default:"587"`
	Username string `envconfig:"SMTP_USERNAME"`
	Password string `envconfig:"SMTP_PASSWORD"`
	From     string `envconfig:"SMTP_FROM"`
}

func (c Config) Enabled() bool { return c.Host != "" && c.From != "" }

type Sender interface {
	Send(to, subject, body string) error
}

type SMTPSender struct{ cfg Config }

func NewSMTPSender(cfg Config) *SMTPSender { return &SMTPSender{cfg: cfg} }

func (s *SMTPSender) Send(to, subject, body string) error {
	for _, v := range []string{to, subject, s.cfg.From} {
		if strings.ContainsAny(v, "\r\n") {
			return fmt.Errorf("invalid header value")
		}
	}
	var auth smtp.Auth
	if s.cfg.Username != "" {
		auth = smtp.PlainAuth("", s.cfg.Username, s.cfg.Password, s.cfg.Host)
	}
	msg := "From: " + s.cfg.From + "\r\nTo: " + to + "\r\nSubject: " + subject +
		"\r\nMIME-Version: 1.0\r\nContent-Type: text/plain; charset=UTF-8\r\n\r\n" + body
	return smtp.SendMail(fmt.Sprintf("%s:%d", s.cfg.Host, s.cfg.Port), auth, s.cfg.From, []string{to}, []byte(msg))
}
