package migrations_test

import (
	"context"
	"database/sql"
	"os"
	"strings"
	"testing"

	"github.com/golang-migrate/migrate/v4"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

// The workspace DeepSeek credential becomes the default model server with the
// same encrypted key, and rolling back restores it.
func TestModelServerMigrationConvertsTheDeepSeekCredential(t *testing.T) {
	url := os.Getenv("DATABASE_URL")
	if url == "" {
		t.Skip("DATABASE_URL is not configured")
	}
	admin, err := sql.Open("pgx", url)
	require.NoError(t, err)
	name := "kuayle_model_servers_migration_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	_, err = admin.Exec(`CREATE DATABASE "` + name + `"`)
	require.NoError(t, err)
	t.Cleanup(func() {
		admin.ExecContext(context.Background(), `DROP DATABASE "`+name+`" WITH (FORCE)`)
		admin.Close()
	})
	target := databaseURLWithName(t, url, name)
	m, err := migrate.New("file://.", target)
	require.NoError(t, err)
	t.Cleanup(func() { m.Close() })
	require.NoError(t, m.Migrate(32))
	db, err := sql.Open("pgx", target)
	require.NoError(t, err)
	t.Cleanup(func() { db.Close() })
	fixture := insertBaseMigrationFixture(t, db)
	require.NoError(t, m.Migrate(75))
	_, err = db.Exec(`INSERT INTO dev_machine_workspace_model_credentials(workspace_id,provider,encrypted_api_key,default_model,thinking) VALUES($1,'deepseek','sealed-key','deepseek-v4-pro','max')`, fixture.workspaceA)
	require.NoError(t, err)
	require.NoError(t, m.Migrate(76))

	var server struct {
		name, openai, anthropic, key, model, fast, thinking string
		models                                             string
		isDefault                                          bool
	}
	require.NoError(t, db.QueryRow(`SELECT name,openai_base_url,anthropic_base_url,encrypted_api_key,default_model,fast_model,thinking,models::text,is_default FROM agent_model_servers WHERE workspace_id=$1`, fixture.workspaceA).
		Scan(&server.name, &server.openai, &server.anthropic, &server.key, &server.model, &server.fast, &server.thinking, &server.models, &server.isDefault))
	require.Equal(t, "DeepSeek", server.name)
	require.Equal(t, "https://api.deepseek.com", server.openai)
	require.Equal(t, "https://api.deepseek.com/anthropic", server.anthropic)
	require.Equal(t, "sealed-key", server.key)
	require.Equal(t, "deepseek-v4-pro", server.model)
	require.Equal(t, "deepseek-flash", server.fast)
	require.Equal(t, "max", server.thinking)
	require.JSONEq(t, `["deepseek-flash","deepseek-v4-pro"]`, server.models)
	require.True(t, server.isDefault)

	_, err = db.Exec(`INSERT INTO agent_model_servers(workspace_id,name,openai_base_url,encrypted_api_key,default_model,is_default) VALUES($1,'Local','http://ollama:11434/v1','k','qwen2.5-coder:32b',true)`, fixture.workspaceA)
	require.Error(t, err, "one default server per workspace")
	_, err = db.Exec(`INSERT INTO agent_model_servers(workspace_id,name,encrypted_api_key,default_model) VALUES($1,'Nowhere','k','m')`, fixture.workspaceA)
	require.Error(t, err, "a server needs at least one protocol URL")
	_, err = db.Exec(`INSERT INTO agent_model_servers(workspace_id,name,openai_base_url,encrypted_api_key,default_model) VALUES($1,'Local','http://ollama:11434/v1','k','qwen2.5-coder:32b')`, fixture.workspaceA)
	require.NoError(t, err, "models from any server can be named")

	require.NoError(t, m.Steps(-1))
	var key, model string
	require.NoError(t, db.QueryRow(`SELECT encrypted_api_key,default_model FROM dev_machine_workspace_model_credentials WHERE workspace_id=$1`, fixture.workspaceA).Scan(&key, &model))
	require.Equal(t, "sealed-key", key)
	require.Equal(t, "deepseek-v4-pro", model)
	require.NoError(t, m.Steps(1))
}
