package main

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"

	"github.com/google/uuid"
	"github.com/jmoiron/sqlx"
	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/kuayle/kuayle-backend/internal/handler"
	mw "github.com/kuayle/kuayle-backend/internal/middleware"
	"github.com/kuayle/kuayle-backend/internal/repository"
	"github.com/kuayle/kuayle-backend/internal/service"
	"github.com/kuayle/kuayle-backend/pkg/crypto"
	jwtpkg "github.com/kuayle/kuayle-backend/pkg/jwt"
	"github.com/labstack/echo/v4"
	"github.com/stretchr/testify/require"
)

// Voice settings are owner-only, the status is for members, and only a
// conversation's reader can transcribe into it; personal access tokens are
// refused. Audio never reaches the voice service for refused requests.
func TestVoiceHTTP(t *testing.T) {
	databaseURL := os.Getenv("DATABASE_URL")
	if databaseURL == "" {
		t.Skip("DATABASE_URL is not set")
	}
	db, err := sqlx.Connect("pgx", databaseURL)
	require.NoError(t, err)
	defer db.Close()
	owner, member, guest, ws := uuid.New(), uuid.New(), uuid.New(), uuid.New()
	for _, id := range []uuid.UUID{owner, member, guest} {
		_, err = db.Exec(`INSERT INTO users(id,email,name,password_hash) VALUES($1,$2,'Voice','hash')`, id, id.String()+"@test.invalid")
		require.NoError(t, err)
		defer db.Exec(`DELETE FROM users WHERE id=$1`, id)
	}
	_, err = db.Exec(`INSERT INTO workspaces(id,name,slug,owner_id) VALUES($1,'Voice',$2,$3)`, ws, ws.String(), owner)
	require.NoError(t, err)
	defer db.Exec(`DELETE FROM workspaces WHERE id=$1`, ws)
	for id, role := range map[uuid.UUID]string{owner: "owner", member: "member", guest: "guest"} {
		_, err = db.Exec(`INSERT INTO workspace_members(workspace_id,user_id,role) VALUES($1,$2,$3)`, ws, id, role)
		require.NoError(t, err)
	}

	// A fake voice service speaking the internal protocol.
	const internalToken = "voice-http-internal-token-0123456789abcdef"
	var mu sync.Mutex
	transcribed := 0
	voice := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		if r.Header.Get("Authorization") != "Bearer "+internalToken {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		switch {
		case r.URL.Path == "/v1/status" || strings.HasPrefix(r.URL.Path, "/v1/models/"):
			_, _ = io.WriteString(w, `{"models":[{"id":"faster-whisper-small","state":"ready","downloaded_bytes":5,"total_bytes":5}]}`)
		case r.URL.Path == "/v1/transcribe":
			transcribed++
			_, _ = io.WriteString(w, `{"text":"create an issue for the login bug"}`)
		}
	}))
	defer voice.Close()
	client, err := service.NewVoiceClient(voice.URL, internalToken)
	require.NoError(t, err)
	key := crypto.DeriveKey("voice-http-test")
	aiRepo := repository.NewAISettingsRepository(db)
	encrypted, err := crypto.Encrypt("test-credential", key)
	require.NoError(t, err)
	require.NoError(t, aiRepo.Upsert(context.Background(), &domain.AISettings{WorkspaceID: ws, Provider: domain.AIProviderOpenAICompatible, BaseURL: "https://model.example.test", Model: "test-model", APIKeyEncrypted: &encrypted}))
	conversations := repository.NewConversationRepository(db)
	workspaces := repository.NewWorkspaceRepository(db)
	conversationSvc := service.NewConversationService(conversations, service.NewAISettingsService(aiRepo, nil, nil, key))
	voiceSvc := service.NewVoiceService(repository.NewVoiceRepository(db), workspaces, conversations, client)

	tokens := repository.NewPersonalAccessTokenRepository(db)
	secret := "voice-http-test-secret-at-least-32-chars"
	middleware := testMiddleware(mw.Auth(secret, tokens))
	middleware.workspaceMembership = mw.WorkspaceMembership(workspaces)
	e := echo.New()
	registerRoutes(e, &appHandlers{conversation: handler.NewConversationHandler(conversationSvc, "https://kuayle.test"), voice: handler.NewVoiceHandler(voiceSvc)}, middleware)
	server := httptest.NewServer(e)
	defer server.Close()
	tokenFor := func(id uuid.UUID) string {
		token, e := jwtpkg.GenerateAccessToken(id, secret)
		require.NoError(t, e)
		return token
	}
	raw, hash, prefix, err := domain.GenerateToken()
	require.NoError(t, err)
	require.NoError(t, tokens.Create(context.Background(), &domain.PersonalAccessToken{ID: uuid.New(), UserID: owner, Name: "Voice denied", Scopes: []string{"issues:read", "workspaces:read"}, TokenHash: hash, TokenPrefix: prefix}))
	root := server.URL + "/api/workspaces/" + ws.String()
	do := func(method, path, contentType string, body io.Reader, auth string) (int, map[string]any) {
		t.Helper()
		req, e := http.NewRequest(method, root+path, body)
		require.NoError(t, e)
		req.Header.Set("Authorization", "Bearer "+auth)
		if contentType != "" {
			req.Header.Set("Content-Type", contentType)
		}
		res, e := http.DefaultClient.Do(req)
		require.NoError(t, e)
		defer res.Body.Close()
		var payload map[string]any
		_ = json.NewDecoder(res.Body).Decode(&payload)
		return res.StatusCode, payload
	}

	// Status: members see whether voice works; guests and tokens do not.
	status, payload := do("GET", "/voice", "", nil, tokenFor(owner))
	require.Equal(t, 200, status)
	require.Equal(t, "off", payload["model"])
	require.NotEmpty(t, payload["models"])
	status, payload = do("GET", "/voice", "", nil, tokenFor(member))
	require.Equal(t, 200, status)
	require.Equal(t, false, payload["owner"])
	require.NotContains(t, payload, "total_bytes")
	require.Equal(t, 403, first(do("GET", "/voice", "", nil, tokenFor(guest))))
	require.Equal(t, 403, first(do("GET", "/voice", "", nil, raw)))

	// Choosing the model is owner-only and session-only.
	choose := `{"model":"faster-whisper-small"}`
	require.Equal(t, 403, first(do("PUT", "/voice", "application/json", strings.NewReader(choose), tokenFor(member))))
	require.Equal(t, 403, first(do("PUT", "/voice", "application/json", strings.NewReader(choose), raw)))
	require.Equal(t, 400, first(do("PUT", "/voice", "application/json", strings.NewReader(`{"model":"whisper-huge"}`), tokenFor(owner))))

	var conversation domain.Conversation
	req, _ := http.NewRequest("POST", root+"/conversations", strings.NewReader(`{"title":"Voice"}`))
	req.Header.Set("Authorization", "Bearer "+tokenFor(owner))
	req.Header.Set("Content-Type", "application/json")
	res, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	require.Equal(t, 201, res.StatusCode)
	require.NoError(t, json.NewDecoder(res.Body).Decode(&conversation))
	res.Body.Close()
	path := "/conversations/" + conversation.ID.String() + "/transcriptions"
	webm := "\x1a\x45\xdf\xa3 recorded audio"

	// Off: the composer is told, nothing is sent.
	status, payload = do("POST", path, "audio/webm", strings.NewReader(webm), tokenFor(owner))
	require.Equal(t, 409, status)
	require.Equal(t, "VOICE_OFF", payload["error"].(map[string]any)["code"])

	status, payload = do("PUT", "/voice", "application/json", strings.NewReader(choose), tokenFor(owner))
	require.Equal(t, 200, status)
	require.Equal(t, "faster-whisper-small", payload["model"])
	require.Equal(t, true, payload["ready"])

	// Only the conversation's reader may transcribe into it.
	require.Equal(t, 404, first(do("POST", path, "audio/webm", strings.NewReader(webm), tokenFor(member))))
	require.Equal(t, 404, first(do("POST", path, "audio/webm", strings.NewReader(webm), tokenFor(guest))))
	require.Equal(t, 403, first(do("POST", path, "audio/webm", strings.NewReader(webm), raw)))
	require.Equal(t, 404, first(do("POST", "/conversations/"+uuid.NewString()+"/transcriptions", "audio/webm", strings.NewReader(webm), tokenFor(owner))))
	require.Equal(t, 415, first(do("POST", path, "text/html", strings.NewReader("<html>"), tokenFor(owner))))
	require.Equal(t, 413, first(do("POST", path, "audio/webm", bytes.NewReader(make([]byte, domain.VoiceMaxBytes+1)), tokenFor(owner))))
	require.Zero(t, transcribed, "refused requests never reach the voice service")

	status, payload = do("POST", path, "audio/webm;codecs=opus", strings.NewReader(webm), tokenFor(owner))
	require.Equal(t, 200, status)
	require.Equal(t, "create an issue for the login bug", payload["text"])
	require.Equal(t, 1, transcribed)
	var messages int
	require.NoError(t, db.Get(&messages, `SELECT count(*) FROM conversation_messages WHERE conversation_id=$1`, conversation.ID))
	require.Zero(t, messages, "the transcript only fills the composer")
}

func first(status int, _ map[string]any) int { return status }
