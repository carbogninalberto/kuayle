package handler

import (
	"net/http"
	"testing"

	"github.com/google/uuid"
	"github.com/labstack/echo/v5"
	"github.com/stretchr/testify/assert"
)

// An invalid key is refused before the service is reached, so a nil service is
// proof the request never got past validation.
func TestTeamHandler_Update_RejectsInvalidKeys(t *testing.T) {
	for name, body := range map[string]string{
		"empty":     `{"key":""}`,
		"lowercase": `{"key":"plt"}`,
		"digits":    `{"key":"PL1"}`,
		"too long":  `{"key":"ABCDEFGHIJK"}`,
	} {
		t.Run(name, func(t *testing.T) {
			e := echo.New()
			c, rec := setupIssueContext(e, http.MethodPatch, "/", body)
			c.SetPathValues(echo.PathValues{{Name: "teamId", Value: uuid.New().String()}})
			assert.NoError(t, NewTeamHandler(nil).Update(c))
			assert.Equal(t, http.StatusBadRequest, rec.Code, rec.Body.String())
		})
	}
}
