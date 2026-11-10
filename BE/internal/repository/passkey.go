package repository

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jmoiron/sqlx"
	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/lib/pq"
)

var (
	// ErrPasskeyExists reports a credential ID that is already registered.
	ErrPasskeyExists = errors.New("passkey already registered")
	// ErrLastPasskey reports a refused removal of the only remaining passkey.
	ErrLastPasskey = errors.New("last passkey cannot be removed")
)

type passkeyRow struct {
	ID                uuid.UUID      `db:"id"`
	UserID            uuid.UUID      `db:"user_id"`
	CredentialID      []byte         `db:"credential_id"`
	PublicKey         []byte         `db:"public_key"`
	SignCount         int64          `db:"sign_count"`
	Transports        pq.StringArray `db:"transports"`
	AAGUID            []byte         `db:"aaguid"`
	AttestationType   string         `db:"attestation_type"`
	AttestationFormat string         `db:"attestation_format"`
	UserVerified      bool           `db:"user_verified"`
	BackupEligible    bool           `db:"backup_eligible"`
	BackupState       bool           `db:"backup_state"`
	Name              string         `db:"name"`
	CreatedAt         time.Time      `db:"created_at"`
	LastUsedAt        *time.Time     `db:"last_used_at"`
}

func (r passkeyRow) domain() *domain.PasskeyCredential {
	return &domain.PasskeyCredential{
		ID: r.ID, UserID: r.UserID, CredentialID: r.CredentialID, PublicKey: r.PublicKey,
		SignCount: uint32(r.SignCount), Transports: []string(r.Transports), AAGUID: r.AAGUID,
		AttestationType: r.AttestationType, AttestationFormat: r.AttestationFormat,
		UserVerified: r.UserVerified, BackupEligible: r.BackupEligible, BackupState: r.BackupState,
		Name: r.Name, CreatedAt: r.CreatedAt, LastUsedAt: r.LastUsedAt,
	}
}

const passkeyColumns = `id, user_id, credential_id, public_key, sign_count, transports, aaguid, attestation_type,
	attestation_format, user_verified, backup_eligible, backup_state, name, created_at, last_used_at`

type PasskeyRepository struct {
	db *sqlx.DB
}

func NewPasskeyRepository(db *sqlx.DB) *PasskeyRepository {
	return &PasskeyRepository{db: db}
}

func (r *PasskeyRepository) Create(ctx context.Context, p *domain.PasskeyCredential) error {
	transports := p.Transports
	if transports == nil {
		transports = []string{}
	}
	err := r.db.QueryRowContext(ctx, `INSERT INTO passkey_credentials
		(id, user_id, credential_id, public_key, sign_count, transports, aaguid, attestation_type, attestation_format,
		 user_verified, backup_eligible, backup_state, name)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13) RETURNING created_at`,
		p.ID, p.UserID, p.CredentialID, p.PublicKey, int64(p.SignCount), pq.Array(transports), p.AAGUID, p.AttestationType,
		p.AttestationFormat, p.UserVerified, p.BackupEligible, p.BackupState, p.Name).Scan(&p.CreatedAt)
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "23505" {
		return ErrPasskeyExists
	}
	return err
}

func (r *PasskeyRepository) ListByUser(ctx context.Context, userID uuid.UUID) ([]domain.PasskeyCredential, error) {
	var rows []passkeyRow
	if err := r.db.SelectContext(ctx, &rows, `SELECT `+passkeyColumns+` FROM passkey_credentials WHERE user_id = $1 ORDER BY created_at, id`, userID); err != nil {
		return nil, err
	}
	out := make([]domain.PasskeyCredential, 0, len(rows))
	for _, row := range rows {
		out = append(out, *row.domain())
	}
	return out, nil
}

// GetByCredentialID returns nil when no passkey has that WebAuthn credential ID.
func (r *PasskeyRepository) GetByCredentialID(ctx context.Context, credentialID []byte) (*domain.PasskeyCredential, error) {
	var row passkeyRow
	err := r.db.GetContext(ctx, &row, `SELECT `+passkeyColumns+` FROM passkey_credentials WHERE credential_id = $1`, credentialID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return row.domain(), nil
}

// Exists reports whether the user still owns the passkey.
func (r *PasskeyRepository) Exists(ctx context.Context, id, userID uuid.UUID) (bool, error) {
	var exists bool
	err := r.db.GetContext(ctx, &exists, `SELECT EXISTS(SELECT 1 FROM passkey_credentials WHERE id = $1 AND user_id = $2)`, id, userID)
	return exists, err
}

// Rename returns nil when the user owns no such passkey.
func (r *PasskeyRepository) Rename(ctx context.Context, id, userID uuid.UUID, name string) (*domain.PasskeyCredential, error) {
	var row passkeyRow
	err := r.db.GetContext(ctx, &row, `UPDATE passkey_credentials SET name = $3 WHERE id = $1 AND user_id = $2 RETURNING `+passkeyColumns, id, userID, name)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return row.domain(), nil
}

// Delete removes the user's passkey and, through the foreign key, the sessions
// it minted. With keepOne it refuses to remove the user's only passkey; the
// user row is locked so two removals cannot both leave none. It reports
// whether a passkey was removed.
func (r *PasskeyRepository) Delete(ctx context.Context, id, userID uuid.UUID, keepOne bool) (bool, error) {
	tx, err := r.db.BeginTxx(ctx, nil)
	if err != nil {
		return false, err
	}
	defer tx.Rollback()
	var locked uuid.UUID
	if err := tx.QueryRowContext(ctx, `SELECT id FROM users WHERE id = $1 FOR UPDATE`, userID).Scan(&locked); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return false, nil
		}
		return false, err
	}
	if keepOne {
		var others int
		if err := tx.GetContext(ctx, &others, `SELECT COUNT(*) FROM passkey_credentials WHERE user_id = $1 AND id <> $2`, userID, id); err != nil {
			return false, err
		}
		var owned bool
		if err := tx.GetContext(ctx, &owned, `SELECT EXISTS(SELECT 1 FROM passkey_credentials WHERE id = $1 AND user_id = $2)`, id, userID); err != nil {
			return false, err
		}
		if owned && others == 0 {
			return false, ErrLastPasskey
		}
	}
	result, err := tx.ExecContext(ctx, `DELETE FROM passkey_credentials WHERE id = $1 AND user_id = $2`, id, userID)
	if err != nil {
		return false, err
	}
	removed, err := result.RowsAffected()
	if err != nil {
		return false, err
	}
	return removed > 0, tx.Commit()
}

// RecordUse stores the outcome of a verified assertion. The update applies
// only while the stored counter is still the one the assertion was checked
// against, so a concurrent use or removal of the passkey reports false.
func (r *PasskeyRepository) RecordUse(ctx context.Context, id uuid.UUID, previousCount, signCount uint32, backupState bool) (bool, error) {
	result, err := r.db.ExecContext(ctx, `UPDATE passkey_credentials SET sign_count = $3, backup_state = $4, last_used_at = NOW()
		WHERE id = $1 AND sign_count = $2`, id, int64(previousCount), int64(signCount), backupState)
	if err != nil {
		return false, err
	}
	updated, err := result.RowsAffected()
	return updated > 0, err
}

// SaveChallenge stores a pending ceremony and discards the ones it replaces:
// the user's earlier registration, or the login the same browser started
// before (replacesHash). Expired challenges are purged on the way.
func (r *PasskeyRepository) SaveChallenge(ctx context.Context, ch *domain.PasskeyChallenge, replacesHash string) error {
	tx, err := r.db.BeginTxx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `DELETE FROM passkey_challenges WHERE expires_at < NOW()`); err != nil {
		return err
	}
	if replacesHash != "" {
		if _, err := tx.ExecContext(ctx, `DELETE FROM passkey_challenges WHERE handle_hash = $1`, replacesHash); err != nil {
			return err
		}
	}
	if ch.UserID != nil {
		if _, err := tx.ExecContext(ctx, `DELETE FROM passkey_challenges WHERE user_id = $1 AND kind = $2`, *ch.UserID, ch.Kind); err != nil {
			return err
		}
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO passkey_challenges (handle_hash, kind, user_id, session_data, expires_at) VALUES ($1,$2,$3,$4,$5)`,
		ch.HandleHash, ch.Kind, ch.UserID, ch.SessionData, ch.ExpiresAt); err != nil {
		return err
	}
	return tx.Commit()
}

// ConsumeChallenge deletes the challenge and returns it, so it can be used at
// most once whatever the ceremony's outcome. It returns nil when there is no
// such challenge; the caller checks expiry and ownership.
func (r *PasskeyRepository) ConsumeChallenge(ctx context.Context, handleHash string) (*domain.PasskeyChallenge, error) {
	ch := domain.PasskeyChallenge{HandleHash: handleHash}
	err := r.db.QueryRowContext(ctx, `DELETE FROM passkey_challenges WHERE handle_hash = $1 RETURNING kind, user_id, session_data, expires_at`,
		handleHash).Scan(&ch.Kind, &ch.UserID, &ch.SessionData, &ch.ExpiresAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &ch, nil
}
