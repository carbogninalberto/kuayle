package handler

import (
	"encoding/json"
	"testing"

	"github.com/google/uuid"
	"github.com/kuayle/kuayle-backend/internal/dto"
	"github.com/kuayle/kuayle-backend/pkg/validate"
	"github.com/stretchr/testify/require"
)

// BulkUpdate validates the body with validate.Struct before the service runs,
// so "No project" (project_id "") must pass that step.
func TestBulkUpdateRequestAcceptsEmptyProjectID(t *testing.T) {
	issue := uuid.NewString()
	for name, project := range map[string]string{"remove": `""`, "set": `"` + uuid.NewString() + `"`, "absent": `null`} {
		var req dto.BulkUpdateIssueRequest
		require.NoError(t, json.Unmarshal([]byte(`{"issue_ids":["`+issue+`"],"project_id":`+project+`}`), &req), name)
		require.NoError(t, validate.Struct(&req), name)
	}
}
