package handler

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/kuayle/kuayle-backend/internal/middleware"
	"github.com/kuayle/kuayle-backend/internal/repository"
	"github.com/kuayle/kuayle-backend/internal/service"
	"github.com/kuayle/kuayle-backend/pkg/response"
	"github.com/labstack/echo/v5"
	log "github.com/sirupsen/logrus"
)

const (
	SCIMPath        = "/api/scim/v2/:slug"
	scimContentType = "application/scim+json"
	scimBodyLimit   = 1 << 20
)

type SCIMHandler struct {
	service   *service.SCIMService
	publicURL string
}

func NewSCIMHandler(service *service.SCIMService, publicURL string) *SCIMHandler {
	return &SCIMHandler{service: service, publicURL: strings.TrimRight(publicURL, "/")}
}

func (h *SCIMHandler) TokenStatus(c *echo.Context) error {
	status, err := h.service.Repo.TokenStatus(c.Request().Context(), middleware.GetWorkspace(c).ID)
	if err != nil {
		return response.InternalError(c)
	}
	return response.Success(c, http.StatusOK, status)
}

func (h *SCIMHandler) RotateToken(c *echo.Context) error {
	token, err := h.service.RotateToken(c.Request().Context(), middleware.GetWorkspace(c).ID)
	if err != nil {
		return response.InternalError(c)
	}
	c.Response().Header().Set("Cache-Control", "private, no-store")
	return response.Success(c, http.StatusCreated, map[string]string{"token": token})
}

func (h *SCIMHandler) RevokeToken(c *echo.Context) error {
	if err := h.service.Repo.RevokeToken(c.Request().Context(), middleware.GetWorkspace(c).ID); err != nil {
		return response.InternalError(c)
	}
	return c.NoContent(http.StatusNoContent)
}

func (h *SCIMHandler) MapGroup(c *echo.Context) error {
	groupID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		return response.NotFound(c, "SCIM group")
	}
	var teamID *uuid.UUID
	if c.Request().Method == http.MethodPut {
		var body struct {
			TeamID uuid.UUID `json:"team_id"`
		}
		if err := c.Bind(&body); err != nil || body.TeamID == uuid.Nil {
			return response.Error(c, http.StatusBadRequest, "BAD_REQUEST", "A team ID is required")
		}
		teamID = &body.TeamID
	}
	err = h.service.MapGroup(c.Request().Context(), middleware.GetWorkspace(c).ID, groupID, teamID)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		return response.NotFound(c, "SCIM group or team")
	case errors.Is(err, repository.ErrSCIMConflict):
		return response.Error(c, http.StatusConflict, "CONFLICT", "Team is already mapped to a SCIM group")
	case err != nil:
		return response.InternalError(c)
	}
	return c.NoContent(http.StatusNoContent)
}

type scimMeta struct {
	ResourceType string    `json:"resourceType"`
	Location     string    `json:"location"`
	Created      time.Time `json:"created"`
	LastModified time.Time `json:"lastModified"`
}

type scimUserResponse struct {
	domain.SCIMUser
	Schemas []string `json:"schemas"`
	Meta    scimMeta `json:"meta"`
}

type scimGroupResponse struct {
	domain.SCIMGroup
	Schemas []string `json:"schemas"`
	Meta    scimMeta `json:"meta"`
}

type scimList[T any] struct {
	Schemas      []string `json:"schemas"`
	TotalResults int      `json:"totalResults"`
	Resources    []T      `json:"Resources"`
	StartIndex   int      `json:"startIndex"`
	ItemsPerPage int      `json:"itemsPerPage"`
}

func (h *SCIMHandler) resourceURL(c *echo.Context, resource string) string {
	return h.publicURL + strings.Replace(SCIMPath, ":slug", url.PathEscape(c.Param("slug")), 1) + "/" + resource
}

func (h *SCIMHandler) userResponse(c *echo.Context, user domain.SCIMUser) scimUserResponse {
	return scimUserResponse{user, []string{service.SCIMUserSchema}, scimMeta{
		ResourceType: "User", Location: h.resourceURL(c, "Users/"+user.ID.String()),
		Created: user.CreatedAt, LastModified: user.UpdatedAt,
	}}
}

func (h *SCIMHandler) groupResponse(c *echo.Context, group domain.SCIMGroup) scimGroupResponse {
	for part := range strings.SplitSeq(c.QueryParam("excludedAttributes"), ",") {
		if strings.EqualFold(strings.TrimSpace(part), "members") {
			group.Members = nil
		}
	}
	return scimGroupResponse{group, []string{service.SCIMGroupSchema}, scimMeta{
		ResourceType: "Group", Location: h.resourceURL(c, "Groups/"+group.ID.String()),
		Created: group.CreatedAt, LastModified: group.UpdatedAt,
	}}
}

func (h *SCIMHandler) Users(c *echo.Context) error {
	return h.withSCIM(c, func(ctx context.Context, tx *repository.SCIMTransaction) (int, any, error) {
		if c.Request().Method == http.MethodPost {
			return h.saveUser(c, ctx, tx, domain.SCIMUser{}, http.StatusCreated)
		}
		query, err := service.ParseSCIMQuery(c.QueryParam("filter"), c.QueryParam("startIndex"), c.QueryParam("count"), false)
		if err != nil {
			return 0, nil, err
		}
		users, total, err := tx.Users(ctx, query)
		if err != nil {
			return 0, nil, err
		}
		resources := make([]scimUserResponse, 0, len(users))
		for _, user := range users {
			resources = append(resources, h.userResponse(c, user))
		}
		return http.StatusOK, scimList[scimUserResponse]{[]string{service.SCIMMessage + "ListResponse"}, total, resources, query.Start, len(resources)}, nil
	})
}

func (h *SCIMHandler) User(c *echo.Context) error {
	return h.withSCIM(c, func(ctx context.Context, tx *repository.SCIMTransaction) (int, any, error) {
		id, err := uuid.Parse(c.Param("id"))
		if err != nil {
			return 0, nil, sql.ErrNoRows
		}
		user, err := tx.User(ctx, id)
		if err != nil {
			return 0, nil, err
		}
		switch c.Request().Method {
		case http.MethodGet:
			return http.StatusOK, h.userResponse(c, user), nil
		case http.MethodDelete:
			return http.StatusNoContent, nil, tx.DeleteUser(ctx, user)
		default:
			return h.saveUser(c, ctx, tx, user, http.StatusOK)
		}
	})
}

func (h *SCIMHandler) saveUser(c *echo.Context, ctx context.Context, tx *repository.SCIMTransaction, user domain.SCIMUser, status int) (int, any, error) {
	body, err := readSCIMBody(c)
	if err != nil {
		return 0, nil, err
	}
	saved, err := service.SaveSCIMUser(ctx, tx, user, body, c.Request().Method == http.MethodPatch)
	if err != nil {
		return 0, nil, err
	}
	return status, h.userResponse(c, saved), nil
}

func (h *SCIMHandler) Groups(c *echo.Context) error {
	return h.withSCIM(c, func(ctx context.Context, tx *repository.SCIMTransaction) (int, any, error) {
		if c.Request().Method == http.MethodPost {
			return h.saveGroup(c, ctx, tx, domain.SCIMGroup{}, http.StatusCreated)
		}
		query, err := service.ParseSCIMQuery(c.QueryParam("filter"), c.QueryParam("startIndex"), c.QueryParam("count"), true)
		if err != nil {
			return 0, nil, err
		}
		groups, total, err := tx.Groups(ctx, query)
		if err != nil {
			return 0, nil, err
		}
		resources := make([]scimGroupResponse, 0, len(groups))
		for _, group := range groups {
			resources = append(resources, h.groupResponse(c, group))
		}
		return http.StatusOK, scimList[scimGroupResponse]{[]string{service.SCIMMessage + "ListResponse"}, total, resources, query.Start, len(resources)}, nil
	})
}

func (h *SCIMHandler) Group(c *echo.Context) error {
	return h.withSCIM(c, func(ctx context.Context, tx *repository.SCIMTransaction) (int, any, error) {
		id, err := uuid.Parse(c.Param("id"))
		if err != nil {
			return 0, nil, sql.ErrNoRows
		}
		if c.Request().Method == http.MethodDelete {
			return http.StatusNoContent, nil, tx.DeleteGroup(ctx, id)
		}
		group, err := tx.Group(ctx, id)
		if err != nil {
			return 0, nil, err
		}
		if c.Request().Method == http.MethodGet {
			return http.StatusOK, h.groupResponse(c, group), nil
		}
		return h.saveGroup(c, ctx, tx, group, http.StatusOK)
	})
}

func (h *SCIMHandler) saveGroup(c *echo.Context, ctx context.Context, tx *repository.SCIMTransaction, group domain.SCIMGroup, status int) (int, any, error) {
	body, err := readSCIMBody(c)
	if err != nil {
		return 0, nil, err
	}
	saved, err := service.SaveSCIMGroup(ctx, tx, group, body, c.Request().Method == http.MethodPatch)
	if err != nil {
		return 0, nil, err
	}
	return status, h.groupResponse(c, saved), nil
}

func (h *SCIMHandler) withSCIM(c *echo.Context, run func(context.Context, *repository.SCIMTransaction) (int, any, error)) error {
	ctx := c.Request().Context()
	c.Response().Header().Set("Cache-Control", "private, no-store")
	header := strings.Fields(c.Request().Header.Get("Authorization"))
	if len(header) != 2 || !strings.EqualFold(header[0], "Bearer") {
		return scimFailure(c, repository.ErrSCIMUnauthorized)
	}
	var status int
	var body any
	err := h.service.WithToken(ctx, c.Param("slug"), header[1], c.Request().Method != http.MethodGet, func(tx *repository.SCIMTransaction) error {
		var err error
		status, body, err = run(ctx, tx)
		return err
	})
	if err != nil {
		return scimFailure(c, err)
	}
	if status == http.StatusNoContent {
		return c.NoContent(status)
	}
	if status == http.StatusCreated {
		switch item := body.(type) {
		case scimUserResponse:
			c.Response().Header().Set("Location", item.Meta.Location)
		case scimGroupResponse:
			c.Response().Header().Set("Location", item.Meta.Location)
		}
	}
	return scimJSON(c, status, body)
}

func readSCIMBody(c *echo.Context) (json.RawMessage, error) {
	body, err := io.ReadAll(http.MaxBytesReader(c.Response(), c.Request().Body, scimBodyLimit))
	if err != nil {
		return nil, err
	}
	return body, nil
}

func scimJSON(c *echo.Context, status int, body any) error {
	encoded, err := json.Marshal(body)
	if err != nil {
		return err
	}
	return c.Blob(status, scimContentType, encoded)
}

func scimFailure(c *echo.Context, err error) error {
	status, kind, detail := http.StatusInternalServerError, "", "internal error"
	var invalid *service.SCIMError
	var tooLarge *http.MaxBytesError
	switch {
	case errors.Is(err, repository.ErrSCIMUnauthorized):
		status, detail = http.StatusUnauthorized, err.Error()
		c.Response().Header().Set("WWW-Authenticate", "Bearer")
	case errors.Is(err, sql.ErrNoRows):
		status, detail = http.StatusNotFound, "SCIM resource not found"
	case errors.Is(err, repository.ErrSCIMConflict):
		status, kind, detail = http.StatusConflict, "uniqueness", err.Error()
	case errors.Is(err, repository.ErrSCIMOwner):
		status, detail = http.StatusForbidden, err.Error()
	case errors.Is(err, repository.ErrSCIMMember):
		status, kind, detail = http.StatusBadRequest, "invalidValue", err.Error()
	case errors.As(err, &invalid):
		status, kind, detail = http.StatusBadRequest, invalid.Type, invalid.Detail
	case errors.As(err, &tooLarge):
		status, detail = http.StatusRequestEntityTooLarge, "SCIM request exceeds the body limit"
	default:
		log.WithField("error_type", fmt.Sprintf("%T", err)).Error("SCIM request failed")
	}
	return scimJSON(c, status, struct {
		Schemas []string `json:"schemas"`
		Status  string   `json:"status"`
		Type    string   `json:"scimType,omitempty"`
		Detail  string   `json:"detail"`
	}{[]string{service.SCIMMessage + "Error"}, strconv.Itoa(status), kind, detail})
}
