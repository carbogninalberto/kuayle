package service

import (
	"context"
	"errors"
	"strings"

	"github.com/google/uuid"
	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/kuayle/kuayle-backend/internal/repository"
)

var (
	ErrAssistantCapabilitiesDenied      = errors.New("assistant capabilities access denied")
	ErrAssistantCapabilitiesUnavailable = errors.New("assistant tools are not configured")
	ErrUnknownAssistantTool             = errors.New("unknown assistant tool")
)

// AssistantCapability describes one built-in assistant tool for Settings →
// Assistant. Descriptions are the model-facing text, shown as they are sent.
type AssistantCapability struct {
	Name             string `json:"name"`
	Description      string `json:"description"`
	RequiresApproval bool   `json:"requires_approval"`
	ChangesData      bool   `json:"changes_data"`
	// Available: the server wired the tool's dependencies.
	Available bool `json:"available"`
	// Enabled: the workspace has not turned the tool off.
	Enabled bool `json:"enabled"`
	// Custom marks a workspace custom tool (custom_<name>); its switch is the
	// tool's own enabled flag.
	Custom bool `json:"custom,omitempty"`
}

type AssistantCapabilities struct {
	Tools []AssistantCapability `json:"tools"`
	// CanManage: the caller is an owner or admin and may switch tools.
	CanManage bool `json:"can_manage"`
}

// builtInTools lists every built-in tool, from the registry, with whether its
// dependencies are wired.
func (s *ConversationService) builtInTools() ([]assistantTool, []bool) {
	tools := append(assistantTools(), attachImagesTool)
	available := make([]bool, len(tools))
	for i, tool := range tools {
		available[i] = s.tools.available(tool)
	}
	available[len(tools)-1] = s.images != nil
	return tools, available
}

// capabilityAccess allows current non-guest members and reports whether the
// caller is an owner or admin. Writes re-check the role in SQL.
func (s *ConversationService) capabilityAccess(ctx context.Context, ws uuid.UUID) (uuid.UUID, bool, error) {
	if s.tools == nil || s.tools.settings == nil {
		return uuid.Nil, false, ErrAssistantCapabilitiesUnavailable
	}
	access, ok := domain.AccessFromContext(ctx)
	if !ok || access.WorkspaceID != ws {
		return uuid.Nil, false, ErrAssistantCapabilitiesDenied
	}
	member, err := s.tools.members.GetMember(ctx, ws, access.UserID)
	if err != nil {
		return uuid.Nil, false, err
	}
	if member == nil || member.Role == domain.RoleGuest {
		return uuid.Nil, false, ErrAssistantCapabilitiesDenied
	}
	return access.UserID, member.Role == domain.RoleOwner || member.Role == domain.RoleAdmin, nil
}

func (s *ConversationService) Capabilities(ctx context.Context, ws uuid.UUID) (*AssistantCapabilities, error) {
	_, manage, err := s.capabilityAccess(ctx, ws)
	if err != nil {
		return nil, err
	}
	disabled, err := s.tools.disabledTools(ctx, ws)
	if err != nil {
		return nil, err
	}
	tools, available := s.builtInTools()
	result := &AssistantCapabilities{Tools: make([]AssistantCapability, 0, len(tools)), CanManage: manage}
	for i, tool := range tools {
		result.Tools = append(result.Tools, capability(tool, available[i], disabled))
	}
	if s.tools.customTools != nil {
		custom, err := s.tools.customTools.List(ctx, ws)
		if err != nil {
			return nil, err
		}
		for _, tool := range custom {
			if tool.Status == domain.AssistantSkillActive {
				result.Tools = append(result.Tools, customCapability(tool))
			}
		}
	}
	return result, nil
}

func customCapability(tool domain.AssistantCustomTool) AssistantCapability {
	return AssistantCapability{Name: domain.CustomToolPrefix + tool.Name, Description: tool.Description, RequiresApproval: tool.RunMode != domain.AssistantToolRunAuto,
		ChangesData: !tool.ReadOnly, Available: true, Enabled: tool.Enabled, Custom: true}
}

// SetCapability turns one built-in tool on or off for the workspace.
func (s *ConversationService) SetCapability(ctx context.Context, ws uuid.UUID, name string, enabled bool) (*AssistantCapability, error) {
	user, _, err := s.capabilityAccess(ctx, ws)
	if err != nil {
		return nil, err
	}
	if custom, ok := strings.CutPrefix(name, domain.CustomToolPrefix); ok && s.tools.customTools != nil {
		tools, err := s.tools.customTools.List(ctx, ws)
		if err != nil {
			return nil, err
		}
		for _, tool := range tools {
			if tool.Name == custom && tool.Status == domain.AssistantSkillActive {
				updated, err := s.tools.customTools.SetEnabled(ctx, ws, tool.ID, user, enabled)
				if errors.Is(err, repository.ErrAssistantToolDenied) {
					return nil, repository.ErrAssistantToolSettingDenied
				}
				if err != nil {
					return nil, err
				}
				result := customCapability(*updated)
				return &result, nil
			}
		}
	}
	tools, available := s.builtInTools()
	for i, tool := range tools {
		if tool.name != name {
			continue
		}
		if err = s.tools.settings.Set(ctx, ws, user, name, enabled); err != nil {
			return nil, err
		}
		result := capability(tool, available[i], map[string]bool{name: !enabled})
		return &result, nil
	}
	return nil, ErrUnknownAssistantTool
}

func capability(tool assistantTool, available bool, disabled map[string]bool) AssistantCapability {
	return AssistantCapability{Name: tool.name, Description: tool.description, RequiresApproval: tool.approval, ChangesData: tool.write, Available: available, Enabled: !disabled[tool.name]}
}
