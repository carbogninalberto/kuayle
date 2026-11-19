package repository

import (
	"context"
	"database/sql"
	"errors"

	"github.com/jmoiron/sqlx"
	"github.com/kuayle/kuayle-backend/pkg/crypto"
)

type InstanceSettingsRepository struct {
	db            *sqlx.DB
	encryptionKey []byte
}

func NewInstanceSettingsRepository(db *sqlx.DB, encryptionKey []byte) *InstanceSettingsRepository {
	return &InstanceSettingsRepository{db: db, encryptionKey: encryptionKey}
}

type instanceSetting struct {
	Value  string
	Secret bool
}

func (s instanceSetting) plain(key []byte) (string, error) {
	if s.Secret {
		return crypto.Decrypt(s.Value, key)
	}
	return s.Value, nil
}

func (r *InstanceSettingsRepository) Get(ctx context.Context, key string) (string, error) {
	var row instanceSetting
	err := r.db.GetContext(ctx, &row, "SELECT value, secret FROM instance_settings WHERE key=$1", key)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	return row.plain(r.encryptionKey)
}

// Update serializes read/modify/write for one key, including its first creation.
// The callback sees plaintext. Its error rolls back the entire change.
func (r *InstanceSettingsRepository) Update(ctx context.Context, key string, secret bool, change func(string) (string, error)) error {
	tx, err := r.db.BeginTxx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	_, err = tx.ExecContext(ctx, `INSERT INTO instance_settings(key,value,secret) VALUES($1,'',false) ON CONFLICT (key) DO NOTHING`, key)
	if err != nil {
		return err
	}
	var row instanceSetting
	if err := tx.GetContext(ctx, &row, "SELECT value, secret FROM instance_settings WHERE key=$1 FOR UPDATE", key); err != nil {
		return err
	}
	old, err := row.plain(r.encryptionKey)
	if err != nil {
		return err
	}
	value, err := change(old)
	if err != nil {
		return err
	}
	if secret {
		value, err = crypto.Encrypt(value, r.encryptionKey)
		if err != nil {
			return err
		}
	}
	if _, err := tx.ExecContext(ctx, "UPDATE instance_settings SET value=$2, secret=$3 WHERE key=$1", key, value, secret); err != nil {
		return err
	}
	return tx.Commit()
}
