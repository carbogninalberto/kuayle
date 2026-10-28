package service

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"html"
	"io"
	"path/filepath"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/kuayle/kuayle-backend/internal/dto"
	"github.com/kuayle/kuayle-backend/internal/repository"
	"github.com/kuayle/kuayle-backend/pkg/imageinfo"
	"github.com/kuayle/kuayle-backend/pkg/storage"
	log "github.com/sirupsen/logrus"
)

var (
	ErrAttachmentTooLarge    = errors.New("image is larger than 10 MB")
	ErrAttachmentUnsupported = errors.New("only PNG, JPEG, WebP and GIF images are supported")
	ErrAttachmentsDisabled   = errors.New("image attachments are not available")
)

// Bounds for images sent to a vision model: only the newest few, each and in
// total small enough for an ordinary Chat Completions request.
const (
	visionMaxImages     = 4
	visionMaxImageBytes = 4 << 20
	visionMaxTotalBytes = 10 << 20
	// Never-sent uploads are removed after a day.
	pendingAttachmentAge = 24 * time.Hour
)

// ImageStore keeps conversation images in Kuayle's configured storage
// backend after validating them by content.
type ImageStore struct {
	backend storage.Backend
}

func NewImageStore(backend storage.Backend) *ImageStore {
	return &ImageStore{backend: backend}
}

// readImage reads at most the attachment limit plus one byte.
func readImage(r io.Reader) ([]byte, error) {
	data, err := io.ReadAll(io.LimitReader(r, domain.ConversationAttachmentMaxBytes+1))
	if err != nil {
		return nil, err
	}
	if len(data) > domain.ConversationAttachmentMaxBytes {
		return nil, ErrAttachmentTooLarge
	}
	return data, nil
}

// Save validates and stores one image. The returned key must be recorded or
// deleted by the caller.
func (s *ImageStore) Save(ctx context.Context, filename string, data []byte) (domain.NewConversationAttachment, error) {
	if len(data) > domain.ConversationAttachmentMaxBytes {
		return domain.NewConversationAttachment{}, ErrAttachmentTooLarge
	}
	info, err := imageinfo.Inspect(data)
	if err != nil {
		return domain.NewConversationAttachment{}, ErrAttachmentUnsupported
	}
	ext := domain.ConversationAttachmentTypes[info.ContentType]
	key := "conversation-" + uuid.NewString() + ext
	if _, err = s.backend.Put(ctx, key, bytes.NewReader(data), info.ContentType); err != nil {
		return domain.NewConversationAttachment{}, err
	}
	return domain.NewConversationAttachment{
		StorageKey: key, Filename: attachmentFilename(filename, ext), ContentType: info.ContentType,
		Size: int64(len(data)), Width: info.Width, Height: info.Height,
	}, nil
}

func (s *ImageStore) Delete(ctx context.Context, keys ...string) {
	for _, key := range keys {
		if err := s.backend.Delete(ctx, key); err != nil {
			log.WithField("component", "conversation-attachments").Warn("stored image cleanup failed")
		}
	}
}

// Open returns at most limit bytes of a stored object.
func (s *ImageStore) Read(ctx context.Context, key string, limit int64) ([]byte, error) {
	rc, err := s.backend.Get(ctx, key)
	if err != nil {
		return nil, err
	}
	defer rc.Close()
	data, err := io.ReadAll(io.LimitReader(rc, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > limit {
		return nil, ErrAttachmentTooLarge
	}
	return data, nil
}

// attachmentFilename keeps a printable base name with the content's extension.
func attachmentFilename(name, ext string) string {
	name = filepath.Base(strings.ReplaceAll(name, "\\", "/"))
	name = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) || r == '"' || r == '/' {
			return -1
		}
		return r
	}, name)
	name = strings.TrimSpace(strings.TrimSuffix(name, filepath.Ext(name)))
	if name == "" || name == "." {
		name = "image"
	}
	for len(name) > 120 || !utf8.ValidString(name) {
		_, size := utf8.DecodeLastRuneInString(name)
		name = name[:len(name)-max(size, 1)]
	}
	return name + ext
}

// SetAttachments enables image uploads, vision context and filing images on
// issues. Without it conversations are text-only.
func (s *ConversationService) SetAttachments(images *ImageStore, assets repository.AssetRepo, comments attachmentCommenter, workspaces attachmentWorkspaces) {
	s.images, s.assets, s.comments, s.workspaces = images, assets, comments, workspaces
}

type attachmentCommenter interface {
	Create(ctx context.Context, issueID, userID uuid.UUID, req dto.CreateCommentRequest) (*domain.Comment, error)
}

type attachmentWorkspaces interface {
	GetByID(ctx context.Context, id uuid.UUID) (*domain.Workspace, error)
}

// Upload validates, stores and records one pending image for the composer.
func (s *ConversationService) Upload(ctx context.Context, ws, id uuid.UUID, filename string, r io.Reader) (*domain.ConversationAttachment, error) {
	if s.images == nil {
		return nil, ErrAttachmentsDisabled
	}
	// Authorize before reading or storing anything.
	if err := s.Repo.Authorize(ctx, ws, id); err != nil {
		return nil, err
	}
	data, err := readImage(r)
	if err != nil {
		return nil, err
	}
	stored, err := s.images.Save(ctx, filename, data)
	if err != nil {
		return nil, err
	}
	attachment, err := s.Repo.CreateAttachment(ctx, ws, id, stored)
	if err != nil {
		s.images.Delete(context.WithoutCancel(ctx), stored.StorageKey)
		return nil, err
	}
	return attachment, nil
}

// OpenAttachment authorizes and opens one image for streaming.
func (s *ConversationService) OpenAttachment(ctx context.Context, ws, id, attachment uuid.UUID) (*domain.ConversationAttachment, io.ReadCloser, error) {
	if s.images == nil {
		return nil, nil, repository.ErrConversationNotFound
	}
	a, err := s.Repo.Attachment(ctx, ws, id, attachment)
	if err != nil {
		return nil, nil, err
	}
	rc, err := s.images.backend.Get(ctx, a.StorageKey)
	if err != nil {
		return nil, nil, repository.ErrConversationNotFound
	}
	return a, rc, nil
}

// SendMessage is Send with images uploaded to this conversation.
func (s *ConversationService) SendMessage(ctx context.Context, ws, id, request uuid.UUID, content string, attachments []uuid.UUID) (*domain.ConversationMessage, error) {
	if len(attachments) > 0 && s.images == nil {
		return nil, repository.ErrConversationInvalid
	}
	if err := s.Repo.Authorize(ctx, ws, id); err != nil {
		return nil, err
	}
	settings, err := s.configuration(ctx, ws)
	if err != nil {
		return nil, err
	}
	return s.Repo.Send(ctx, ws, id, request, content, settings.Provider, settings.Model, attachments...)
}

// sweepAttachments expires never-sent uploads and removes the stored objects
// of deleted attachments.
func (s *ConversationService) sweepAttachments(ctx context.Context) {
	if s.images == nil {
		return
	}
	if _, err := s.Repo.ExpirePendingAttachments(ctx, pendingAttachmentAge, 100); err != nil {
		log.Warn("Expiring pending conversation images failed")
		return
	}
	keys, err := s.Repo.AttachmentGarbage(ctx, 100)
	if err != nil {
		log.Warn("Listing deleted conversation images failed")
		return
	}
	for _, key := range keys {
		if err := s.images.backend.Delete(ctx, key); err != nil {
			continue
		}
		_ = s.Repo.ForgetAttachmentGarbage(ctx, key)
	}
}

// attachmentNote describes images in text, so a model without vision (or an
// image beyond the vision bounds) can still refer to, file or forward them.
func attachmentNote(attachments []domain.ConversationAttachment) string {
	var b strings.Builder
	for i, a := range attachments {
		if i > 0 {
			b.WriteByte('\n')
		}
		fmt.Fprintf(&b, "[image attached: %s, %dx%d, id=%s]", a.Filename, a.Width, a.Height, a.ID)
	}
	return b.String()
}

// modelMessages turns context messages into provider messages. With vision
// on, the newest images within the bounds are added as image_url parts.
func (s *ConversationService) modelMessages(ctx context.Context, history []domain.ConversationMessage, vision bool) []chatCompletionMessage {
	images := map[uuid.UUID]string{}
	if vision && s.images != nil {
		total := 0
		for i := len(history) - 1; i >= 0 && len(images) < visionMaxImages; i-- {
			for _, a := range history[i].Attachments {
				if len(images) >= visionMaxImages || a.Size > visionMaxImageBytes || total+int(a.Size) > visionMaxTotalBytes {
					continue
				}
				data, err := s.images.Read(ctx, a.StorageKey, visionMaxImageBytes)
				if err != nil {
					continue
				}
				total += len(data)
				images[a.ID] = "data:" + a.ContentType + ";base64," + base64.StdEncoding.EncodeToString(data)
			}
		}
	}
	messages := make([]chatCompletionMessage, 0, len(history))
	for _, m := range history {
		message := chatCompletionMessage{Role: m.Role, Content: m.Content}
		if m.Role == "user" && len(m.Attachments) > 0 {
			message.Content = strings.TrimSpace(m.Content + "\n\n" + attachmentNote(m.Attachments))
			parts := []chatContentPart{{Type: "text", Text: message.Content}}
			for _, a := range m.Attachments {
				if url, ok := images[a.ID]; ok {
					parts = append(parts, chatContentPart{Type: "image_url", ImageURL: &chatImageURL{URL: url}})
				}
			}
			if len(parts) > 1 {
				message.Parts = parts
			}
		}
		messages = append(messages, message)
	}
	return messages
}

const attachImagesToolName = "attach_images_to_issue"

var attachImagesTool = assistantTool{
	name: attachImagesToolName, write: true,
	description: "File images the user attached in this conversation on an issue, as a comment with the images, so issue readers and coding agents get them. Images are named in messages as [image attached: name, WxH, id=...]. Omit attachment_ids to file the newest images (at most 6). Only call this when the user asked to add or file the images, or asked to create an issue from them.",
	parameters:  `{"type":"object","properties":{"identifier":{"type":"string","maxLength":32},"attachment_ids":{"type":"array","maxItems":6,"items":{"type":"string","maxLength":36}}},"required":["identifier"],"additionalProperties":false}`,
}

// attachImagesToIssue copies conversation images into issue assets scoped to
// the issue's team and posts them as one comment by the user. Images already
// filed on that issue are not filed again.
func (s *ConversationService) attachImagesToIssue(ctx context.Context, a domain.ConversationAttempt, arguments string) (any, *uuid.UUID, error) {
	var input struct {
		Identifier    string   `json:"identifier"`
		AttachmentIDs []string `json:"attachment_ids"`
	}
	if err := decodeToolInput(arguments, &input); err != nil {
		return nil, nil, err
	}
	if s.images == nil || s.tools == nil || s.assets == nil || s.comments == nil || s.workspaces == nil {
		return nil, nil, toolFailure("image attachments are not available")
	}
	ws, user := a.Conversation.WorkspaceID, a.Conversation.CreatorID
	member, err := s.tools.members.GetMember(ctx, ws, user)
	if err != nil || member == nil || member.Role == domain.RoleGuest || !domain.HasPermission(member.Role, domain.PermIssueUpdate) {
		return nil, nil, toolFailure("your role cannot update issues")
	}
	ids := make([]uuid.UUID, 0, len(input.AttachmentIDs))
	for _, raw := range input.AttachmentIDs {
		id, err := uuid.Parse(strings.TrimSpace(raw))
		if err != nil {
			return nil, nil, toolFailure("unknown image %q", raw)
		}
		ids = append(ids, id)
	}
	issue, err := s.tools.issues.GetByIdentifier(ctx, ws, strings.ToUpper(strings.TrimSpace(input.Identifier)))
	if err != nil {
		return nil, nil, err
	}
	if issue == nil {
		return nil, nil, toolFailure("issue %s was not found", input.Identifier)
	}
	images, err := s.Repo.SentAttachments(ctx, ws, a.Conversation.ID, ids, domain.ConversationAttachmentsPerMessage)
	if err != nil {
		return nil, nil, err
	}
	if len(ids) > 0 && len(images) != len(ids) {
		return nil, nil, toolFailure("some images are not in this conversation")
	}
	if len(images) == 0 {
		return nil, nil, toolFailure("this conversation has no images")
	}
	workspace, err := s.workspaces.GetByID(ctx, ws)
	if err != nil || workspace == nil {
		return nil, nil, toolFailure("you no longer have access to this workspace")
	}
	var body strings.Builder
	filed, already := 0, 0
	for _, image := range images {
		existing, err := s.Repo.IssueAttachmentAsset(ctx, ws, a.Conversation.ID, image.ID, issue.ID)
		if err != nil {
			return nil, nil, err
		}
		if existing != nil {
			already++
			continue
		}
		asset, err := s.copyToIssueAsset(ctx, ws, user, issue.TeamID, image)
		if err != nil {
			return nil, nil, err
		}
		if err = s.Repo.LinkAttachmentIssue(ctx, ws, a.Conversation.ID, image.ID, issue.ID, asset); err != nil {
			return nil, nil, err
		}
		fmt.Fprintf(&body, `<p><img src="/api/workspaces/%s/assets/%s" alt="%s"></p>`, html.EscapeString(workspace.Slug), asset, html.EscapeString(image.Filename))
		filed++
	}
	if filed > 0 {
		if _, err = s.comments.Create(ctx, issue.ID, user, dto.CreateCommentRequest{Body: "<p>Images from the assistant conversation:</p>" + body.String()}); err != nil {
			return nil, nil, err
		}
	}
	return map[string]any{"identifier": issue.Identifier, "filed": filed, "already_filed": already}, &issue.ID, nil
}

func (s *ConversationService) copyToIssueAsset(ctx context.Context, ws, user, team uuid.UUID, image domain.ConversationAttachment) (uuid.UUID, error) {
	data, err := s.images.Read(ctx, image.StorageKey, domain.ConversationAttachmentMaxBytes)
	if err != nil {
		return uuid.Nil, toolFailure("image %s is unavailable", image.Filename)
	}
	key := uuid.NewString() + domain.ConversationAttachmentTypes[image.ContentType]
	if _, err = s.images.backend.Put(ctx, key, bytes.NewReader(data), image.ContentType); err != nil {
		return uuid.Nil, err
	}
	asset := &domain.Asset{ID: uuid.New(), WorkspaceID: ws, TeamID: &team, StorageKey: key, Filename: image.Filename, ContentType: image.ContentType, Size: int64(len(data)), UploadedBy: user}
	if err = s.assets.Create(ctx, asset); err != nil {
		s.images.Delete(context.WithoutCancel(ctx), key)
		return uuid.Nil, err
	}
	return asset.ID, nil
}
