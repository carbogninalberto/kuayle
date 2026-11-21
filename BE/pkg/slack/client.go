// Package slack calls the Slack Web API. Credentials are only sent to Slack
// in headers or form bodies. Provider responses and tokens never enter errors.
package slack

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

var ErrAPI = errors.New("Slack API request failed")
var ErrInvalidAuth = errors.New("Slack rejected the bot authorization")
var ErrRevoked = errors.New("Slack bot authorization is no longer valid")
var ErrRateLimited = errors.New("Slack rate limit reached")

const Scope = "channels:read,groups:read"

type Client struct {
	clientID, clientSecret, redirectURI string
	http                                *http.Client
}

func NewClient(id, secret, redirectURI string) *Client {
	return &Client{
		clientID: id, clientSecret: secret, redirectURI: redirectURI,
		http: &http.Client{
			Timeout:       10 * time.Second,
			CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
		},
	}
}

func (c *Client) InstallURL(state string) string {
	values := url.Values{"client_id": {c.clientID}, "redirect_uri": {c.redirectURI}, "scope": {Scope}, "state": {state}}
	return "https://slack.com/oauth/v2/authorize?" + values.Encode()
}

type Channel struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type oauthResult struct {
	Token        string                    `json:"access_token"`
	TokenType    string                    `json:"token_type"`
	RefreshToken string                    `json:"refresh_token"`
	Scope        string                    `json:"scope"`
	Team         struct{ ID, Name string } `json:"team"`
	User         struct{ ID string }       `json:"authed_user"`
}

type apiResult struct {
	oauthResult
	OK       bool      `json:"ok"`
	Error    string    `json:"error"`
	Channels []Channel `json:"channels"`
	Metadata struct {
		NextCursor string `json:"next_cursor"`
	} `json:"response_metadata"`
}

func (c *Client) call(ctx context.Context, method, token string, form url.Values) (apiResult, error) {
	var out apiResult
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://slack.com/api/"+method, strings.NewReader(form.Encode()))
	if err != nil {
		return out, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return out, ErrAPI
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusTooManyRequests {
		return out, ErrRateLimited
	}
	if resp.StatusCode != http.StatusOK || json.NewDecoder(io.LimitReader(resp.Body, 4<<20)).Decode(&out) != nil {
		return out, ErrAPI
	}
	if !out.OK {
		switch out.Error {
		case "invalid_auth":
			return out, ErrInvalidAuth
		case "account_inactive", "token_revoked":
			return out, ErrRevoked
		case "ratelimited":
			return out, ErrRateLimited
		}
		return out, ErrAPI
	}
	return out, nil
}

func (c *Client) Exchange(ctx context.Context, code string) (oauthResult, error) {
	out, err := c.call(ctx, "oauth.v2.access", "", url.Values{"code": {code}, "client_id": {c.clientID}, "client_secret": {c.clientSecret}, "redirect_uri": {c.redirectURI}})
	if err != nil {
		return oauthResult{}, err
	}
	if out.TokenType != "bot" || out.Token == "" || out.RefreshToken != "" || out.Team.ID == "" || out.User.ID == "" {
		return oauthResult{}, ErrAPI
	}
	return out.oauthResult, nil
}

func (c *Client) Channels(ctx context.Context, token string) ([]Channel, error) {
	channels := []Channel{}
	form := url.Values{"types": {"public_channel,private_channel"}, "exclude_archived": {"true"}, "limit": {"200"}}
	seen := map[string]bool{}
	for {
		out, err := c.call(ctx, "users.conversations", token, form)
		if err != nil {
			return nil, err
		}
		channels = append(channels, out.Channels...)
		cursor := out.Metadata.NextCursor
		if cursor == "" {
			return channels, nil
		}
		if seen[cursor] {
			return nil, ErrAPI
		}
		seen[cursor] = true
		form.Set("cursor", cursor)
	}
}

func (c *Client) Uninstall(ctx context.Context, token string) error {
	_, err := c.call(ctx, "apps.uninstall", token, url.Values{"client_id": {c.clientID}, "client_secret": {c.clientSecret}})
	return err
}

func (c *Client) CheckToken(ctx context.Context, token string) error {
	_, err := c.call(ctx, "auth.test", token, nil)
	return err
}
