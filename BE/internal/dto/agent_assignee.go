package dto

import "github.com/kuayle/kuayle-backend/internal/domain"

// AgentIdentityList is the workspace's agents and what the caller may do.
type AgentIdentityList struct {
	Agents []domain.AgentIdentity `json:"agents"`
	// CanManage: the caller chooses avatars and the teams of each agent.
	CanManage bool `json:"can_manage"`
	// CanAssign: the caller may assign and mention agents (never a guest).
	CanAssign bool `json:"can_assign"`
}

// SetIssueAgentsRequest replaces the agents assigned to an issue.
type SetIssueAgentsRequest struct {
	AgentIDs []string `json:"agent_ids"`
}
