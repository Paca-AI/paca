package postgres

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jmoiron/sqlx"

	mfadom "github.com/Paca-AI/api/internal/domain/mfa"
)

// MFARepository is the sqlx implementation of mfadom.Store.
type MFARepository struct {
	db *sqlx.DB
}

// NewMFARepository returns a new MFARepository.
func NewMFARepository(db *sqlx.DB) *MFARepository { return &MFARepository{db: db} }

type mfaRow struct {
	UserID       string    `db:"user_id"`
	SecretEnc    string    `db:"secret_enc"`
	Enabled      bool      `db:"enabled"`
	Recovery     []byte    `db:"recovery_codes"`
	LastUsedStep int64     `db:"last_used_step"`
	UpdatedAt    time.Time `db:"updated_at"`
}

// GetEnrollment returns the user's enrollment or mfadom.ErrNotFound.
func (r *MFARepository) GetEnrollment(ctx context.Context, userID uuid.UUID) (*mfadom.Enrollment, error) {
	var row mfaRow
	err := r.db.GetContext(ctx, &row, `
		SELECT user_id, secret_enc, enabled, recovery_codes, last_used_step, updated_at
		FROM user_mfa WHERE user_id = $1`, userID.String())
	if errors.Is(err, sql.ErrNoRows) {
		return nil, mfadom.ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("mfa repo: get enrollment: %w", err)
	}
	codes, err := decodeStrings(row.Recovery)
	if err != nil {
		return nil, err
	}
	return &mfadom.Enrollment{
		UserID: userID, SecretEnc: row.SecretEnc, Enabled: row.Enabled,
		RecoveryCodes: codes, LastUsedStep: row.LastUsedStep, UpdatedAt: row.UpdatedAt,
	}, nil
}

// SaveEnrollment upserts the enrollment.
func (r *MFARepository) SaveEnrollment(ctx context.Context, e *mfadom.Enrollment) error {
	codes, err := encodeStrings(e.RecoveryCodes)
	if err != nil {
		return err
	}
	_, err = r.db.ExecContext(ctx, `
		INSERT INTO user_mfa (user_id, secret_enc, enabled, recovery_codes, last_used_step)
		VALUES ($1, $2, $3, $4::jsonb, $5)
		ON CONFLICT (user_id) DO UPDATE SET
			secret_enc = EXCLUDED.secret_enc, enabled = EXCLUDED.enabled,
			recovery_codes = EXCLUDED.recovery_codes, last_used_step = EXCLUDED.last_used_step,
			updated_at = now()`,
		e.UserID.String(), e.SecretEnc, e.Enabled, codes, e.LastUsedStep)
	if err != nil {
		return fmt.Errorf("mfa repo: save enrollment: %w", err)
	}
	return nil
}

// DeleteEnrollment removes the enrollment; deleting a missing one is a no-op.
func (r *MFARepository) DeleteEnrollment(ctx context.Context, userID uuid.UUID) error {
	if _, err := r.db.ExecContext(ctx, `DELETE FROM user_mfa WHERE user_id = $1`, userID.String()); err != nil {
		return fmt.Errorf("mfa repo: delete enrollment: %w", err)
	}
	return nil
}

// AdvanceStep records step as used unless a step >= it already was.
func (r *MFARepository) AdvanceStep(ctx context.Context, userID uuid.UUID, step int64) (bool, error) {
	res, err := r.db.ExecContext(ctx, `
		UPDATE user_mfa SET last_used_step = $2, updated_at = now()
		WHERE user_id = $1 AND last_used_step < $2`, userID.String(), step)
	if err != nil {
		return false, fmt.Errorf("mfa repo: advance step: %w", err)
	}
	n, _ := res.RowsAffected()
	return n == 1, nil
}

// ConsumeRecoveryCode atomically removes one recovery-code hash.
func (r *MFARepository) ConsumeRecoveryCode(ctx context.Context, userID uuid.UUID, hash string) (bool, error) {
	res, err := r.db.ExecContext(ctx, `
		UPDATE user_mfa SET recovery_codes = recovery_codes - $2::text, updated_at = now()
		WHERE user_id = $1 AND recovery_codes ? $2::text`, userID.String(), hash)
	if err != nil {
		return false, fmt.Errorf("mfa repo: consume recovery code: %w", err)
	}
	n, _ := res.RowsAffected()
	return n == 1, nil
}

// GetUserCIDRs returns the account's allow-list (empty when unrestricted).
func (r *MFARepository) GetUserCIDRs(ctx context.Context, userID uuid.UUID) ([]string, error) {
	var raw []byte
	err := r.db.GetContext(ctx, &raw, `SELECT allowed_cidrs FROM users WHERE id = $1`, userID.String())
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("mfa repo: get user cidrs: %w", err)
	}
	return decodeStrings(raw)
}

// SetUserCIDRs replaces the account's allow-list.
func (r *MFARepository) SetUserCIDRs(ctx context.Context, userID uuid.UUID, cidrs []string) error {
	enc, err := encodeStrings(cidrs)
	if err != nil {
		return err
	}
	if _, err := r.db.ExecContext(ctx, `UPDATE users SET allowed_cidrs = $2::jsonb WHERE id = $1`,
		userID.String(), enc); err != nil {
		return fmt.Errorf("mfa repo: set user cidrs: %w", err)
	}
	return nil
}

// GetPolicy returns the singleton instance policy.
func (r *MFARepository) GetPolicy(ctx context.Context) (*mfadom.Policy, error) {
	var row struct {
		Roles      []byte `db:"privileged_roles"`
		RequireMFA bool   `db:"require_mfa"`
		IPs        []byte `db:"ip_allow_list"`
	}
	err := r.db.GetContext(ctx, &row, `SELECT privileged_roles, require_mfa, ip_allow_list FROM auth_security_settings WHERE id = TRUE`)
	if errors.Is(err, sql.ErrNoRows) {
		return &mfadom.Policy{PrivilegedRoles: []string{mfadom.AllRoles}}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("mfa repo: get policy: %w", err)
	}
	roles, err := decodeStrings(row.Roles)
	if err != nil {
		return nil, err
	}
	ips, err := decodeStrings(row.IPs)
	if err != nil {
		return nil, err
	}
	return &mfadom.Policy{PrivilegedRoles: roles, RequireMFA: row.RequireMFA, IPAllowList: ips}, nil
}

// SavePolicy replaces the singleton instance policy.
func (r *MFARepository) SavePolicy(ctx context.Context, p *mfadom.Policy, updatedBy *uuid.UUID) error {
	roles, err := encodeStrings(p.PrivilegedRoles)
	if err != nil {
		return err
	}
	ips, err := encodeStrings(p.IPAllowList)
	if err != nil {
		return err
	}
	var by *string
	if updatedBy != nil {
		s := updatedBy.String()
		by = &s
	}
	_, err = r.db.ExecContext(ctx, `
		UPDATE auth_security_settings
		SET privileged_roles = $1::jsonb, require_mfa = $2, ip_allow_list = $3::jsonb, updated_at = now(), updated_by = $4
		WHERE id = TRUE`, roles, p.RequireMFA, ips, by)
	if err != nil {
		return fmt.Errorf("mfa repo: save policy: %w", err)
	}
	return nil
}

var _ mfadom.Store = (*MFARepository)(nil)

// encodeStrings marshals a string list as a JSON array (never null).
func encodeStrings(in []string) (string, error) {
	if in == nil {
		in = []string{}
	}
	b, err := json.Marshal(in)
	if err != nil {
		return "", fmt.Errorf("mfa repo: encode list: %w", err)
	}
	return string(b), nil
}

// decodeStrings unmarshals a JSON array column.
func decodeStrings(raw []byte) ([]string, error) {
	var out []string
	if len(raw) == 0 {
		return out, nil
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, fmt.Errorf("mfa repo: decode list: %w", err)
	}
	return out, nil
}
