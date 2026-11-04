package service

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/kuayle/kuayle-backend/internal/dto"
)

// SystemUpdater talks to the updater sidecar. Settings → Version → Upgrade and
// the assistant's upgrade_instance tool both go through it, so they run the
// same updater with the same token.
type SystemUpdater struct {
	url, token string
	client     *http.Client
}

func NewSystemUpdater(url, token string) *SystemUpdater {
	return &SystemUpdater{
		url:    strings.TrimRight(strings.TrimSpace(url), "/"),
		token:  strings.TrimSpace(token),
		client: &http.Client{Timeout: 10 * time.Second},
	}
}

// Configured reports whether an updater URL and token are set.
func (u *SystemUpdater) Configured() bool {
	return u != nil && u.url != "" && u.token != ""
}

// Status reads the updater's run state. The second result is the HTTP status.
func (u *SystemUpdater) Status(ctx context.Context) (dto.SystemUpdateStatusResponse, int, error) {
	var status dto.SystemUpdateStatusResponse
	code, err := u.call(ctx, http.MethodGet, "/status", &status)
	return status, code, err
}

// Start asks the updater to begin an update run.
func (u *SystemUpdater) Start(ctx context.Context) (dto.SystemUpdateStartResponse, int, error) {
	var result dto.SystemUpdateStartResponse
	code, err := u.call(ctx, http.MethodPost, "/update", &result)
	return result, code, err
}

func (u *SystemUpdater) call(ctx context.Context, method, path string, target any) (int, error) {
	req, err := http.NewRequestWithContext(ctx, method, u.url+path, nil)
	if err != nil {
		return 0, err
	}
	req.Header.Set("Authorization", "Bearer "+u.token)
	res, err := u.client.Do(req)
	if err != nil {
		return 0, err
	}
	defer res.Body.Close()
	if target != nil {
		_ = json.NewDecoder(io.LimitReader(res.Body, 64*1024)).Decode(target)
	}
	return res.StatusCode, nil
}
