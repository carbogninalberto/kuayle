package service

import (
	"testing"

	"github.com/kuayle/kuayle-backend/internal/dto"
	"github.com/stretchr/testify/require"
)

// KUA-181: the command reaches the secret only as $KUAYLE_MIGRATION_SECRET.
// A command or description that contains the supplied secret is refused, so
// the secret is never stored in plain text or shown to the model, in an
// approval or in the machine's list; the refusal never repeats it.
func TestMigrationActionRefusesTheSecretInItsCommandOrDescription(t *testing.T) {
	const dsn = "postgres://hunter2@example/db"
	for _, input := range []dto.DevMachineMigrationActionInput{
		{Name: "migrate", Description: "Run the database migrations", Command: `migrate -url "postgres://hunter2@example/db" up`, Secret: dsn},
		{Name: "migrate", Description: "Migrates postgres://hunter2@example/db", Command: `migrate -url "$KUAYLE_MIGRATION_SECRET" up`, Secret: dsn},
	} {
		for _, required := range []bool{true, false} {
			candidate := input
			_, err := validateMigrationAction(&candidate, required)
			require.ErrorIs(t, err, ErrInvalidMigrationAction, input.Description)
			require.NotContains(t, err.Error(), dsn)
		}
	}

	valid := dto.DevMachineMigrationActionInput{Name: "migrate", Description: "Run the database migrations", Command: `migrate -url "$KUAYLE_MIGRATION_SECRET" up`, Secret: dsn}
	got, err := validateMigrationAction(&valid, true)
	require.NoError(t, err)
	require.Equal(t, dsn, got)
}
