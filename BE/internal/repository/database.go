package repository

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/stdlib"
	"github.com/jmoiron/sqlx"
)

// Open pins every pooled session to READ COMMITTED. The VOLATILE access
// functions recheck membership and team visibility with a fresh statement
// snapshot after lock waits; a REPEATABLE READ or SERIALIZABLE database/role
// default would reuse the pre-wait snapshot and could authorize a write against
// a team that was privatized while the statement waited. Callers that need a
// stronger level (workspace import) request it explicitly per transaction.
func Open(dsn string) (*sqlx.DB, error) {
	config, err := pgx.ParseConfig(dsn)
	if err != nil {
		return nil, err
	}
	db := sqlx.NewDb(stdlib.OpenDB(*config, stdlib.OptionAfterConnect(func(ctx context.Context, conn *pgx.Conn) error {
		if _, err := conn.Exec(ctx, `SET default_transaction_isolation TO 'read committed'`); err != nil {
			return fmt.Errorf("set read committed isolation: %w", err)
		}
		return nil
	})), "pgx")
	if err := db.Ping(); err != nil {
		_ = db.Close()
		return nil, err
	}
	var isolation string
	if err := db.Get(&isolation, `SELECT current_setting('default_transaction_isolation')`); err != nil {
		_ = db.Close()
		return nil, err
	}
	if isolation != "read committed" {
		_ = db.Close()
		return nil, fmt.Errorf("database sessions must default to read committed isolation, got %q", isolation)
	}
	return db, nil
}
