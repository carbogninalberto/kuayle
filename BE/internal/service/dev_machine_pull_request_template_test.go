package service

import (
	"context"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/kuayle/kuayle-backend/internal/dto"
	"github.com/stretchr/testify/require"
)

func TestScopeSettingPullRequestTemplateValidation(t *testing.T) {
	workspaceID := uuid.New()
	update := func(template *string) (*domain.DevMachineScopeSetting, error) {
		return newTestDevMachineService(&devMachineStoreFake{}).UpdateScopeSetting(context.Background(), workspaceID, dto.DevMachineScopeSettingRequest{
			ScopeType: "workspace", PullRequestTemplate: template,
		})
	}
	setting, err := update(dmStrPtr("  ## Why\n{{description}}\n\n## Issue\n{{issue}}\n  "))
	require.NoError(t, err)
	require.Equal(t, "## Why\n{{description}}\n\n## Issue\n{{issue}}", *setting.PullRequestTemplate, "the template is trimmed")

	for _, inherit := range []*string{nil, dmStrPtr(""), dmStrPtr(" \n ")} {
		setting, err = update(inherit)
		require.NoError(t, err)
		require.Nil(t, setting.PullRequestTemplate, "an empty template inherits")
	}

	for name, template := range map[string]string{
		"too long":            strings.Repeat("x", domain.PullRequestTemplateMaxRunes+1),
		"unknown placeholder": "## Summary\n{{summary}}",
		"NUL":                 "{{description}}\x00",
	} {
		_, err = update(dmStrPtr(template))
		require.ErrorIs(t, err, ErrInvalidMachineInput, name)
	}
}
