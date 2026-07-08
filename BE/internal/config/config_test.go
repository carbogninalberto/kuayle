package config

import (
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseSysadmins(t *testing.T) {
	id := uuid.New()
	ids, err := parseSysadmins("  " + id.String() + " , " + id.String() + ",")

	require.NoError(t, err)
	assert.Contains(t, ids, id)
	assert.Len(t, ids, 1)
}

func TestParseSysadminsRejectsInvalidID(t *testing.T) {
	_, err := parseSysadmins("not-a-uuid")

	require.Error(t, err)
	assert.Contains(t, err.Error(), "SYSADMINS")
}

func TestIsSysAdmin(t *testing.T) {
	id := uuid.New()
	cfg := &Config{sysadminIDs: map[uuid.UUID]struct{}{id: {}}}

	assert.True(t, cfg.IsSysAdmin(id))
	assert.False(t, cfg.IsSysAdmin(uuid.New()))
	assert.False(t, cfg.IsSysAdmin(uuid.Nil))
}
