// Package jira implements the Jira Cloud and Jira Server/Data Center REST
// adapters. It produces the neutral model in the parent importer package and
// makes no assumptions about Kuayle entities.
package jira

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/kuayle/kuayle-backend/internal/importer"
	"github.com/kuayle/kuayle-backend/pkg/validate"
)

var (
	ErrAttachmentTooLarge = errors.New("attachment exceeds the size limit")
	ErrInvalidBaseURL     = errors.New("invalid Jira base URL")
)

// Config configures one Jira source. Cloud uses an email + API token with
// Basic auth and the v3 REST API; Server/DC uses a personal access token with
// Bearer auth and the v2 REST API.
type Config struct {
	BaseURL             string
	Email               string
	Token               string
	BearerToken         string
	Cloud               bool
	AllowPrivateNetwork bool
	MaxAttachmentSize   int64
}

// Client is a Jira REST client. It never follows redirects and, unless private
// networks are explicitly allowed, dials only public addresses.
type Client struct {
	baseURL   string
	site      string
	version   string
	cloud     bool
	email     string
	token     string
	bearer    string
	http      *http.Client
	maxAttach int64
	jql       string

	// resolved lazily by field discovery
	sprintField string
	epicField   string
}

func NewClient(cfg Config) (*Client, error) {
	parsed, err := url.Parse(strings.TrimSpace(cfg.BaseURL))
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return nil, ErrInvalidBaseURL
	}
	if parsed.Scheme != "https" && !cfg.AllowPrivateNetwork {
		return nil, fmt.Errorf("%w: https is required unless private networks are allowed", ErrInvalidBaseURL)
	}
	base := strings.TrimRight(cfg.BaseURL, "/")
	c := &Client{
		baseURL:   base,
		site:      strings.ToLower(parsed.Host),
		version:   "2",
		email:     cfg.Email,
		token:     cfg.Token,
		bearer:    cfg.BearerToken,
		maxAttach: cfg.MaxAttachmentSize,
	}
	if cfg.Cloud {
		c.version = "3"
		c.cloud = true
	}
	c.http = newHTTPClient(cfg.AllowPrivateNetwork)
	return c, nil
}

// WithHTTPClient replaces the underlying HTTP client. It is used by tests with
// an httptest server and keeps credentials out of error messages.
func (c *Client) WithHTTPClient(client *http.Client) *Client {
	c.http = client
	return c
}

func newHTTPClient(allowPrivate bool) *http.Client {
	dialer := &net.Dialer{Timeout: 15 * time.Second, KeepAlive: 30 * time.Second}
	if !allowPrivate {
		dialer.Control = validate.PublicAddressControl
	}
	return &http.Client{
		Timeout: 60 * time.Second,
		Transport: &http.Transport{
			DialContext:           dialer.DialContext,
			TLSHandshakeTimeout:   10 * time.Second,
			ResponseHeaderTimeout: 20 * time.Second,
			TLSClientConfig:       &tls.Config{MinVersion: tls.VersionTLS12},
		},
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
}

func (c *Client) Site() string { return c.site }

func (c *Client) apiPath(path string) string {
	return c.baseURL + "/rest/api/" + c.version + path
}

func (c *Client) auth(req *http.Request) {
	if c.bearer != "" {
		req.Header.Set("Authorization", "Bearer "+c.bearer)
		return
	}
	req.SetBasicAuth(c.email, c.token)
}

// do sends a request and retries 429/503 with Retry-After and backoff. It
// never logs or returns response bodies, which may contain source data.
func (c *Client) do(ctx context.Context, req *http.Request) (*http.Response, error) {
	const attempts = 4
	backoff := 500 * time.Millisecond
	for i := 0; i < attempts; i++ {
		clone := req.Clone(ctx)
		if req.Body != nil && req.GetBody != nil {
			body, err := req.GetBody()
			if err != nil {
				return nil, err
			}
			clone.Body = body
		}
		c.auth(clone)
		resp, err := c.http.Do(clone)
		if err != nil {
			return nil, err
		}
		if resp.StatusCode != http.StatusTooManyRequests && resp.StatusCode != http.StatusServiceUnavailable {
			if resp.StatusCode < 200 || resp.StatusCode >= 300 {
				status := resp.StatusCode
				resp.Body.Close()
				return nil, fmt.Errorf("jira request failed with status %d", status)
			}
			return resp, nil
		}
		delay := backoff
		if raw := resp.Header.Get("Retry-After"); raw != "" {
			if secs, err := strconv.Atoi(raw); err == nil && secs > 0 {
				delay = time.Duration(secs) * time.Second
			}
		}
		resp.Body.Close()
		if i == attempts-1 {
			return nil, fmt.Errorf("jira rate limited after %d attempts", attempts)
		}
		timer := time.NewTimer(delay)
		select {
		case <-ctx.Done():
			timer.Stop()
			return nil, ctx.Err()
		case <-timer.C:
		}
		backoff *= 2
	}
	return nil, errors.New("jira request failed")
}

func (c *Client) getJSON(ctx context.Context, fullURL string, target any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, fullURL, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/json")
	resp, err := c.do(ctx, req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	return json.NewDecoder(resp.Body).Decode(target)
}

func (c *Client) postJSON(ctx context.Context, fullURL string, body any, target any) error {
	payload, err := json.Marshal(body)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, fullURL, bytes.NewReader(payload))
	if err != nil {
		return err
	}
	req.GetBody = func() (io.ReadCloser, error) { return io.NopCloser(bytes.NewReader(payload)), nil }
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	resp, err := c.do(ctx, req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	return json.NewDecoder(resp.Body).Decode(target)
}

// OpenAttachment downloads an attachment's bytes through the authenticated
// client. Oversized attachments are rejected before any body is read so the
// caller can skip them without failing the whole job.
func (c *Client) OpenAttachment(ctx context.Context, attachment importer.SourceAttachment) (io.ReadCloser, error) {
	if c.maxAttach > 0 && attachment.Size > c.maxAttach {
		return nil, fmt.Errorf("%w: %s", ErrAttachmentTooLarge, attachment.Filename)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, attachment.URL, nil)
	if err != nil {
		return nil, err
	}
	resp, err := c.do(ctx, req)
	if err != nil {
		return nil, err
	}
	return resp.Body, nil
}
