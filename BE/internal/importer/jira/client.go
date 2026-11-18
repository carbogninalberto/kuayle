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
	ErrAttachmentTooLarge     = errors.New("attachment exceeds the size limit")
	ErrInvalidBaseURL         = errors.New("invalid Jira base URL")
	ErrAttachmentHostMismatch = errors.New("attachment URL is outside the configured Jira host")
	ErrRateLimited            = errors.New("jira kept rate limiting the import")
)

// StatusError is a non-2xx Jira response. It carries only the status code:
// response bodies may contain source data and are never surfaced.
type StatusError struct{ Status int }

func (e *StatusError) Error() string {
	return fmt.Sprintf("jira request failed with status %d", e.Status)
}

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

// parseBaseURL validates a Jira base URL and returns its parsed form. It is
// shared by NewClient and SiteFromBaseURL so the site key cannot drift from
// the host the client actually talks to.
func parseBaseURL(raw string) (*url.URL, error) {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || parsed.Host == "" || (parsed.Scheme != "https" && parsed.Scheme != "http") {
		return nil, ErrInvalidBaseURL
	}
	// Credentials travel in the job's encrypted credentials, never in the URL.
	if parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
		return nil, ErrInvalidBaseURL
	}
	return parsed, nil
}

// DescribeError turns a Jira client error into a message that is safe to show
// the admin: it names the cause (credentials, address, availability) without
// response bodies, request URLs or credentials. It returns "" for an error it
// does not recognise; the caller then shows a generic message.
func DescribeError(err error) string {
	var status *StatusError
	var netErr net.Error
	switch {
	case err == nil:
		return ""
	case errors.As(err, &status):
		switch status.Status {
		case http.StatusUnauthorized, http.StatusForbidden:
			return fmt.Sprintf("Jira rejected the credentials (HTTP %d). Check the email and API token and that the account can browse the projects.", status.Status)
		case http.StatusNotFound:
			return "Jira returned HTTP 404. Check the base URL and whether this is a Cloud or Server/Data Center site."
		case http.StatusBadRequest:
			return "Jira rejected the request (HTTP 400). Check the JQL filter."
		default:
			return fmt.Sprintf("Jira returned HTTP %d.", status.Status)
		}
	case errors.Is(err, ErrInvalidBaseURL):
		return "The Jira base URL is invalid. Use the https address of the site, without credentials or a query."
	case errors.Is(err, validate.ErrNonPublicAddress):
		return "The Jira address is on a private network, which this server does not allow."
	case errors.Is(err, ErrRateLimited):
		return "Jira kept rate limiting the request. Try again later."
	case errors.Is(err, context.DeadlineExceeded), errors.As(err, &netErr):
		return "Could not reach the Jira site. Check the base URL."
	default:
		var syntax *json.SyntaxError
		var typeErr *json.UnmarshalTypeError
		if errors.As(err, &syntax) || errors.As(err, &typeErr) {
			return "Jira returned an unexpected response. Check the base URL and the Cloud or Server/Data Center choice."
		}
		return ""
	}
}

// SiteFromBaseURL returns the lower-cased host of a Jira base URL. It is the
// same value Client.Site() returns and is used server-side as part of the
// workspace-scoped idempotency key, so two Cloud sites never collide.
func SiteFromBaseURL(raw string) (string, error) {
	parsed, err := parseBaseURL(raw)
	if err != nil {
		return "", err
	}
	return strings.ToLower(parsed.Host), nil
}

func NewClient(cfg Config) (*Client, error) {
	parsed, err := parseBaseURL(cfg.BaseURL)
	if err != nil {
		return nil, err
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

// do sends a request and retries 429/503 with Retry-After and backoff. The
// caller decides whether to attach credentials with auth before calling it.
// It never logs or returns response bodies, which may contain source data.
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
		resp, err := c.http.Do(clone)
		if err != nil {
			return nil, err
		}
		if resp.StatusCode != http.StatusTooManyRequests && resp.StatusCode != http.StatusServiceUnavailable {
			if resp.StatusCode < 200 || resp.StatusCode >= 300 {
				status := resp.StatusCode
				resp.Body.Close()
				return nil, &StatusError{Status: status}
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
			return nil, fmt.Errorf("%w after %d attempts", ErrRateLimited, attempts)
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
	c.auth(req)
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
	c.auth(req)
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
	parsed, err := url.Parse(attachment.URL)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return nil, fmt.Errorf("%w: invalid attachment URL", ErrAttachmentHostMismatch)
	}
	base, err := url.Parse(c.baseURL)
	if err != nil {
		return nil, err
	}
	// Credentials must never leave the configured Jira host. Jira content URLs
	// are served from the same scheme and host as the REST API, so a foreign
	// host is refused rather than fetched without auth.
	if !strings.EqualFold(parsed.Scheme, base.Scheme) || !strings.EqualFold(parsed.Host, base.Host) {
		return nil, fmt.Errorf("%w: %s", ErrAttachmentHostMismatch, attachment.Filename)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, attachment.URL, nil)
	if err != nil {
		return nil, err
	}
	c.auth(req)
	resp, err := c.do(ctx, req)
	if err != nil {
		return nil, err
	}
	return resp.Body, nil
}
