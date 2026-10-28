package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"image"
	"image/png"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
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
	"github.com/kuayle/kuayle-backend/pkg/storage"
	"github.com/labstack/echo/v4"
	"github.com/stretchr/testify/require"
)

// Conversation images use the authorized upload flow end to end: only the
// conversation's reader can upload, read or send them; content is sniffed and
// bounded; a resent request keeps its images without duplicating them.
func TestConversationAttachmentsHTTP(t *testing.T) {
	databaseURL := os.Getenv("DATABASE_URL")
	if databaseURL == "" {
		t.Skip("DATABASE_URL is not set")
	}
	db, err := sqlx.Connect("pgx", databaseURL)
	require.NoError(t, err)
	defer db.Close()
	user, other, guest, ws := uuid.New(), uuid.New(), uuid.New(), uuid.New()
	for _, id := range []uuid.UUID{user, other, guest} {
		_, err = db.Exec(`INSERT INTO users(id,email,name,password_hash) VALUES($1,$2,'Chat images','hash')`, id, id.String()+"@test.invalid")
		require.NoError(t, err)
		defer db.Exec(`DELETE FROM users WHERE id=$1`, id)
	}
	_, err = db.Exec(`INSERT INTO workspaces(id,name,slug,owner_id) VALUES($1,'Chat images',$2,$3)`, ws, ws.String(), user)
	require.NoError(t, err)
	defer db.Exec(`DELETE FROM workspaces WHERE id=$1`, ws)
	for id, role := range map[uuid.UUID]string{user: "owner", other: "member", guest: "guest"} {
		_, err = db.Exec(`INSERT INTO workspace_members(workspace_id,user_id,role) VALUES($1,$2,$3)`, ws, id, role)
		require.NoError(t, err)
	}
	key := crypto.DeriveKey("conversation-attachments-test")
	encrypted, err := crypto.Encrypt("test-credential", key)
	require.NoError(t, err)
	aiRepo := repository.NewAISettingsRepository(db)
	require.NoError(t, aiRepo.Upsert(context.Background(), &domain.AISettings{WorkspaceID: ws, Provider: domain.AIProviderOpenAICompatible, BaseURL: "https://model.example.test", Model: "test-model", APIKeyEncrypted: &encrypted}))
	repo := repository.NewConversationRepository(db)
	svc := service.NewConversationService(repo, service.NewAISettingsService(aiRepo, nil, nil, key))
	dir := t.TempDir()
	backend, err := storage.NewLocalBackend(dir, "/uploads")
	require.NoError(t, err)
	svc.SetAttachments(service.NewImageStore(backend), repository.NewAssetRepository(db), nil, repository.NewWorkspaceRepository(db))
	tokens := repository.NewPersonalAccessTokenRepository(db)
	secret := "conversation-attachments-secret-at-least-32"
	middleware := testMiddleware(mw.Auth(secret, tokens))
	middleware.workspaceMembership = mw.WorkspaceMembership(repository.NewWorkspaceRepository(db))
	e := echo.New()
	registerRoutes(e, &appHandlers{conversation: handler.NewConversationHandler(svc, "https://kuayle.test")}, middleware)
	server := httptest.NewServer(e)
	defer server.Close()
	tokenFor := func(id uuid.UUID) string {
		token, e := jwtpkg.GenerateAccessToken(id, secret)
		require.NoError(t, e)
		return token
	}
	owner := tokenFor(user)
	root := server.URL + "/api/workspaces/" + ws.String()
	do := func(method, path, contentType string, body io.Reader, auth string) *http.Response {
		t.Helper()
		req, e := http.NewRequest(method, root+path, body)
		require.NoError(t, e)
		req.Header.Set("Authorization", "Bearer "+auth)
		if contentType != "" {
			req.Header.Set("Content-Type", contentType)
		}
		res, e := http.DefaultClient.Do(req)
		require.NoError(t, e)
		t.Cleanup(func() { res.Body.Close() })
		return res
	}
	upload := func(path, filename string, data []byte, auth string) *http.Response {
		t.Helper()
		var body bytes.Buffer
		form := multipart.NewWriter(&body)
		part, e := form.CreateFormFile("file", filename)
		require.NoError(t, e)
		_, _ = part.Write(data)
		require.NoError(t, form.Close())
		return do("POST", path, form.FormDataContentType(), &body, auth)
	}
	var pngBuf bytes.Buffer
	require.NoError(t, png.Encode(&pngBuf, image.NewRGBA(image.Rect(0, 0, 12, 8))))
	picture := pngBuf.Bytes()

	var conversation domain.Conversation
	res := do("POST", "/conversations", "application/json", strings.NewReader(`{"title":"Images"}`), owner)
	require.Equal(t, 201, res.StatusCode)
	require.NoError(t, json.NewDecoder(res.Body).Decode(&conversation))
	path := "/conversations/" + conversation.ID.String()

	// Only the conversation's reader may upload.
	require.Equal(t, 404, upload(path+"/attachments", "a.png", picture, tokenFor(other)).StatusCode)
	require.Equal(t, 404, upload(path+"/attachments", "a.png", picture, tokenFor(guest)).StatusCode)
	raw, hash, prefix, err := domain.GenerateToken()
	require.NoError(t, err)
	require.NoError(t, tokens.Create(context.Background(), &domain.PersonalAccessToken{ID: uuid.New(), UserID: user, Name: "Images denied", Scopes: []string{"issues:read", "assets:read"}, TokenHash: hash, TokenPrefix: prefix}))
	require.Equal(t, 403, upload(path+"/attachments", "a.png", picture, raw).StatusCode)
	// Content decides: markup named .png and oversized bodies are refused.
	require.Equal(t, 415, upload(path+"/attachments", "evil.png", []byte("<html><script>alert(1)</script></html>"), owner).StatusCode)
	require.Equal(t, 413, upload(path+"/attachments", "big.png", append(append([]byte{}, picture...), make([]byte, domain.ConversationAttachmentMaxBytes)...), owner).StatusCode)
	entries, err := os.ReadDir(dir)
	require.NoError(t, err)
	require.Empty(t, entries, "refused uploads are not stored")

	ids := []uuid.UUID{}
	for i := range domain.ConversationAttachmentsPerMessage + 1 {
		res = upload(path+"/attachments", fmt.Sprintf("shot-%d.png", i), picture, owner)
		require.Equal(t, 201, res.StatusCode)
		var attachment domain.ConversationAttachment
		require.NoError(t, json.NewDecoder(res.Body).Decode(&attachment))
		require.Equal(t, "image/png", attachment.ContentType)
		require.Equal(t, [2]int{12, 8}, [2]int{attachment.Width, attachment.Height})
		ids = append(ids, attachment.ID)
	}
	// Served only to the reader, as an image that cannot be sniffed as markup.
	res = do("GET", path+"/attachments/"+ids[0].String(), "", nil, owner)
	require.Equal(t, 200, res.StatusCode)
	require.Equal(t, "image/png", res.Header.Get("Content-Type"))
	require.Equal(t, "nosniff", res.Header.Get("X-Content-Type-Options"))
	require.Contains(t, res.Header.Get("Content-Disposition"), "inline")
	require.Contains(t, res.Header.Get("Content-Security-Policy"), "sandbox")
	served, _ := io.ReadAll(res.Body)
	require.Equal(t, picture, served)
	require.Equal(t, 404, do("GET", path+"/attachments/"+ids[0].String(), "", nil, tokenFor(other)).StatusCode)
	require.Equal(t, 403, do("GET", path+"/attachments/"+ids[0].String(), "", nil, raw).StatusCode)

	send := func(request uuid.UUID, content string, attachments []uuid.UUID) *http.Response {
		body, _ := json.Marshal(map[string]any{"request_id": request, "content": content, "attachment_ids": attachments})
		return do("POST", path+"/messages", "application/json", bytes.NewReader(body), owner)
	}
	require.Equal(t, 400, send(uuid.New(), "too many", ids).StatusCode, "at most six images per message")
	request := uuid.New()
	res = send(request, "", ids[:2])
	require.Equal(t, 202, res.StatusCode, "an image alone is a message")
	var reply domain.ConversationMessage
	require.NoError(t, json.NewDecoder(res.Body).Decode(&reply))
	res = send(request, "", ids[:2])
	require.Equal(t, 202, res.StatusCode)
	var replayed domain.ConversationMessage
	require.NoError(t, json.NewDecoder(res.Body).Decode(&replayed))
	require.Equal(t, reply.ID, replayed.ID, "a resent request is the same turn")
	require.Equal(t, 409, send(request, "", ids[2:3]).StatusCode, "the same request with other images conflicts")

	var history domain.ConversationHistory
	res = do("GET", path, "", nil, owner)
	require.Equal(t, 200, res.StatusCode)
	require.NoError(t, json.NewDecoder(res.Body).Decode(&history))
	require.Len(t, history.Messages, 2)
	require.Len(t, history.Messages[0].Attachments, 2)
	require.Equal(t, ids[0], history.Messages[0].Attachments[0].ID)
	require.Equal(t, ids[1], history.Messages[0].Attachments[1].ID)
	var bound int
	require.NoError(t, db.Get(&bound, `SELECT count(*) FROM conversation_attachments WHERE message_id IS NOT NULL AND conversation_id=$1`, conversation.ID))
	require.Equal(t, 2, bound, "no duplicated attachments")
	var events []domain.ConversationEvent
	res = do("GET", path+"/events", "", nil, owner)
	require.NoError(t, json.NewDecoder(res.Body).Decode(&events))
	var created domain.ConversationMessage
	require.NoError(t, json.Unmarshal(events[0].Payload, &created))
	require.Len(t, created.Attachments, 2, "the realtime stream carries the images")

	// Sent images cannot be removed or re-sent; pending ones can be removed.
	require.Equal(t, 404, do("DELETE", path+"/attachments/"+ids[0].String(), "", nil, owner).StatusCode)
	require.Equal(t, 204, do("DELETE", path+"/attachments/"+ids[6].String(), "", nil, owner).StatusCode)
	require.Equal(t, 404, do("GET", path+"/attachments/"+ids[6].String(), "", nil, owner).StatusCode)

	// Deleting the conversation queues every stored object for removal.
	_, err = db.Exec(`UPDATE conversation_messages SET status='completed' WHERE conversation_id=$1`, conversation.ID)
	require.NoError(t, err)
	require.Equal(t, 204, do("DELETE", path, "", nil, owner).StatusCode)
	var garbage int
	require.NoError(t, db.Get(&garbage, `SELECT count(*) FROM conversation_attachment_garbage WHERE storage_key LIKE 'conversation-%'`))
	require.GreaterOrEqual(t, garbage, 7)
}
