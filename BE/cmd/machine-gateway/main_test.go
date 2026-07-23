package main

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestValidateGatewayDatabaseRoleRejectsElevatedPrivileges(t *testing.T) {
	require.NoError(t, validateGatewayDatabaseRole(gatewayDatabaseRole{Username: "kuayle_gateway"}))

	tests := []struct {
		name string
		role gatewayDatabaseRole
	}{
		{name: "superuser", role: gatewayDatabaseRole{Username: "gateway", Superuser: true}},
		{name: "create role", role: gatewayDatabaseRole{Username: "gateway", CreateRole: true}},
		{name: "create database", role: gatewayDatabaseRole{Username: "gateway", CreateDatabase: true}},
		{name: "replication", role: gatewayDatabaseRole{Username: "gateway", Replication: true}},
		{name: "bypass rls", role: gatewayDatabaseRole{Username: "gateway", BypassRLS: true}},
		{name: "database create", role: gatewayDatabaseRole{Username: "gateway", DatabaseCreate: true}},
		{name: "schema create", role: gatewayDatabaseRole{Username: "gateway", PublicCreate: true}},
		{name: "role membership", role: gatewayDatabaseRole{Username: "gateway", HasRoleMembership: true}},
		{name: "object ownership", role: gatewayDatabaseRole{Username: "gateway", OwnsObjects: true}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			require.ErrorContains(t, validateGatewayDatabaseRole(test.role), "administrative")
		})
	}
}
