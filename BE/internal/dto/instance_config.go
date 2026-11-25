package dto

import (
	"time"

	"github.com/google/uuid"
)

// InstanceConfigVariableResponse is one environment variable on Settings ->
// Instance configuration. A secret never carries its value, only Set.
type InstanceConfigVariableResponse struct {
	Name        string `json:"name"`
	Group       string `json:"group"`
	Description string `json:"description"`
	Kind        string `json:"kind"`
	Secret      bool   `json:"secret"`
	Set         bool   `json:"set"`
	// Value is the effective value; empty for a secret or a withheld value.
	Value string `json:"value,omitempty"`
	// Withheld marks a plain setting whose value carries credentials.
	Withheld bool   `json:"withheld,omitempty"`
	Default  string `json:"default,omitempty"`
	// Source is environment, default or override.
	Source string `json:"source"`
	// Tunable variables can change at runtime; BaseValue and BaseSource are
	// what a reset returns to, Min and Max bound a duration.
	Tunable    bool       `json:"tunable"`
	BaseValue  string     `json:"base_value,omitempty"`
	BaseSource string     `json:"base_source,omitempty"`
	Min        string     `json:"min,omitempty"`
	Max        string     `json:"max,omitempty"`
	UpdatedAt  *time.Time `json:"updated_at,omitempty"`
	// RestartEditable variables are read at startup; a sysadmin changes them
	// with an apply-and-restart. BaseValue and BaseSource are what removing
	// the change returns to, Rule how a value is checked.
	RestartEditable bool                        `json:"restart_editable"`
	Rule            *InstanceConfigRuleResponse `json:"rule,omitempty"`
}

// InstanceConfigRuleResponse describes what a restart-editable variable
// accepts, for the input the page shows. The server checks every value again.
type InstanceConfigRuleResponse struct {
	// Format refines a string: text, url, host or email.
	Format     string   `json:"format,omitempty"`
	Choices    []string `json:"choices,omitempty"`
	Min        string   `json:"min,omitempty"`
	Max        string   `json:"max,omitempty"`
	AllowEmpty bool     `json:"allow_empty,omitempty"`
}

type InstanceConfigResponse struct {
	// Groups lists the variable groups in display order.
	Groups    []string                         `json:"groups"`
	Variables []InstanceConfigVariableResponse `json:"variables"`
	Restart   InstanceRestartStatusResponse    `json:"restart"`
}

// InstanceRestartChangeResponse is one variable of an apply, with its
// effective value before and after.
type InstanceRestartChangeResponse struct {
	Name string `json:"name"`
	From string `json:"from"`
	To   string `json:"to"`
}

// InstanceRestartRunResponse is one "Apply and restart". Status is restarting,
// applying, confirmed or rolled_back.
type InstanceRestartRunResponse struct {
	ID          uuid.UUID                       `json:"id"`
	Status      string                          `json:"status"`
	RequestedBy *uuid.UUID                      `json:"requested_by,omitempty"`
	RequestedAt time.Time                       `json:"requested_at"`
	FinishedAt  *time.Time                      `json:"finished_at,omitempty"`
	Failure     string                          `json:"failure,omitempty"`
	Changes     []InstanceRestartChangeResponse `json:"changes"`
}

// InstanceRestartStatusResponse tells whether "Apply and restart" is possible
// and what the latest one did.
type InstanceRestartStatusResponse struct {
	Supported bool                        `json:"supported"`
	Run       *InstanceRestartRunResponse `json:"run,omitempty"`
	// Notice explains that stored settings were not used by this process.
	Notice string `json:"notice,omitempty"`
}

// InstanceRestartRequest proposes values: a string sets one, null returns the
// variable to its environment value.
type InstanceRestartRequest struct {
	Changes map[string]*string `json:"changes"`
}

type InstanceRestartIssueResponse struct {
	Name    string `json:"name,omitempty"`
	Message string `json:"message"`
}

// InstanceRestartPlanResponse is the result of the dry run.
type InstanceRestartPlanResponse struct {
	Valid   bool                            `json:"valid"`
	Changes []InstanceRestartChangeResponse `json:"changes"`
	Issues  []InstanceRestartIssueResponse  `json:"issues"`
}

type UpdateInstanceConfigRequest struct {
	Value *string `json:"value"`
}
