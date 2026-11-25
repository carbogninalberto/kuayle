package dto

import "time"

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
}

type InstanceConfigResponse struct {
	// Groups lists the variable groups in display order.
	Groups    []string                         `json:"groups"`
	Variables []InstanceConfigVariableResponse `json:"variables"`
}

type UpdateInstanceConfigRequest struct {
	Value *string `json:"value"`
}
