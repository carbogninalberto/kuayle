package dto_test

import (
	"strings"
	"testing"

	"github.com/kuayle/kuayle-backend/internal/dto"
	"github.com/kuayle/kuayle-backend/pkg/validate"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCreateWorkspaceRequest_SlugValidation(t *testing.T) {
	tests := []struct {
		name    string
		slug    string
		wantErr bool
	}{
		{name: "single word", slug: "acme", wantErr: false},
		{name: "digits", slug: "acme2024", wantErr: false},
		{name: "hyphenated from a name with a space", slug: "ada-lovelace", wantErr: false},
		{name: "multiple hyphen groups", slug: "ada-b-lovelace", wantErr: false},
		{name: "max length", slug: strings.Repeat("a", 50), wantErr: false},

		{name: "empty", slug: "", wantErr: true},
		{name: "leading hyphen", slug: "-acme", wantErr: true},
		{name: "trailing hyphen", slug: "jos-", wantErr: true},
		{name: "consecutive hyphens", slug: "ada--lovelace", wantErr: true},
		{name: "only a hyphen", slug: "-", wantErr: true},
		{name: "uppercase", slug: "Acme", wantErr: true},
		{name: "space", slug: "ada lovelace", wantErr: true},
		{name: "underscore", slug: "ada_lovelace", wantErr: true},
		{name: "accented character", slug: "josé", wantErr: true},
		{name: "over max length", slug: strings.Repeat("a", 51), wantErr: true},
		// /mcp is the MCP endpoint, so a workspace there would lose its root page.
		{name: "reserved for the MCP endpoint", slug: "mcp", wantErr: true},
		{name: "starts with a reserved word", slug: "mcp-team", wantErr: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validate.Struct(dto.CreateWorkspaceRequest{
				Name: "Test Workspace",
				Slug: tt.slug,
			})

			if tt.wantErr {
				require.Error(t, err, "slug %q should be rejected", tt.slug)
				return
			}
			assert.NoError(t, err, "slug %q should be accepted", tt.slug)
		})
	}
}

// Regression test for the reported bug: the login screen derives a slug from the
// user's name, so a name containing a space produces a hyphenated slug. That slug
// must be accepted, otherwise the first user can never create a workspace.
func TestCreateWorkspaceRequest_AcceptsSlugDerivedFromNameWithSpace(t *testing.T) {
	err := validate.Struct(dto.CreateWorkspaceRequest{
		Name: "Ada Lovelace's Workspace",
		Slug: "ada-lovelace",
	})

	assert.NoError(t, err)
}
