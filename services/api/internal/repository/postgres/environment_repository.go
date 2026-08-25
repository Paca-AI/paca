package postgres

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jmoiron/sqlx"

	environmentdom "github.com/Paca-AI/api/internal/domain/environment"
)

// -------------------------------------------------------------------------
// sqlx record types
// -------------------------------------------------------------------------

type environmentRecord struct {
	ID                  string     `db:"id"`
	ProjectID           string     `db:"project_id"`
	Name                string     `db:"name"`
	Slug                string     `db:"slug"`
	SSHPort             *int       `db:"ssh_port"`
	PortsPendingRestart bool       `db:"ports_pending_restart"`
	Status              string     `db:"status"`
	Backend             string     `db:"backend"`
	BackendRef          *string    `db:"backend_ref"`
	Image               *string    `db:"image"`
	CPULimit            string     `db:"cpu_limit"`
	MemoryLimit         string     `db:"memory_limit"`
	DiskLimitGB         int        `db:"disk_limit_gb"`
	VolumeRef           *string    `db:"volume_ref"`
	SecretKeyEncrypted  string     `db:"secret_key_encrypted"`
	IdleTimeoutMinutes  int        `db:"idle_timeout_minutes"`
	LastActiveAt        time.Time  `db:"last_active_at"`
	ErrorMessage        *string    `db:"error_message"`
	CreatedBy           *string    `db:"created_by"`
	CreatedAt           time.Time  `db:"created_at"`
	UpdatedAt           time.Time  `db:"updated_at"`
	DeletedAt           *time.Time `db:"deleted_at"`
}

type environmentFolderRecord struct {
	ID            string    `db:"id"`
	EnvironmentID string    `db:"environment_id"`
	Path          string    `db:"path"`
	CreatedBy     *string   `db:"created_by"`
	CreatedAt     time.Time `db:"created_at"`
	UpdatedAt     time.Time `db:"updated_at"`
}

type environmentSSHKeyRecord struct {
	ID            string    `db:"id"`
	EnvironmentID string    `db:"environment_id"`
	Label         string    `db:"label"`
	PublicKey     string    `db:"public_key"`
	Fingerprint   string    `db:"fingerprint"`
	CreatedBy     *string   `db:"created_by"`
	CreatedAt     time.Time `db:"created_at"`
}

type environmentPortForwardRecord struct {
	ID            string    `db:"id"`
	EnvironmentID string    `db:"environment_id"`
	Label         string    `db:"label"`
	ContainerPort int       `db:"container_port"`
	HostPort      *int      `db:"host_port"`
	CreatedBy     *string   `db:"created_by"`
	CreatedAt     time.Time `db:"created_at"`
}

// -------------------------------------------------------------------------
// Repository
// -------------------------------------------------------------------------

// EnvironmentRepository is the sqlx implementation of environmentdom.Repository.
type EnvironmentRepository struct {
	db *sqlx.DB
}

// NewEnvironmentRepository returns a new EnvironmentRepository.
func NewEnvironmentRepository(db *sqlx.DB) *EnvironmentRepository {
	return &EnvironmentRepository{db: db}
}

// ssh_port is read-only from this repository's perspective — it's assigned
// and persisted directly by agent-runner's own Postgres connection (see
// environmentdom.Environment.SSHPort's doc comment), never written via
// CreateEnvironment/UpdateEnvironment/UpdateEnvironmentProvisioning below.
// ports_pending_restart is the opposite: services/api's own bookkeeping
// (see Environment.PortsPendingRestart's doc comment), written only by
// SetPortsPendingRestart below.
const environmentCols = `id, project_id, name, slug, ssh_port, ports_pending_restart, status, backend, backend_ref, image,
	cpu_limit, memory_limit, disk_limit_gb, volume_ref, secret_key_encrypted, idle_timeout_minutes,
	last_active_at, error_message, created_by, created_at, updated_at, deleted_at`

// -------------------------------------------------------------------------
// Environments
// -------------------------------------------------------------------------

// ListEnvironments returns every non-deleted environment in a project,
// newest first, each with its Folders populated — the conversation
// composer's folder picker (apps/web's agent-picker.tsx) reads
// environment.folders straight off this response rather than issuing a
// separate per-environment folders fetch. One extra query per
// environment (ListFolders below), not a JOIN — acceptable for how few
// static environments a project realistically has, and it keeps this
// method free to reuse ListFolders' own column list/mapping rather than
// duplicating it.
func (r *EnvironmentRepository) ListEnvironments(ctx context.Context, projectID uuid.UUID) ([]*environmentdom.Environment, error) {
	var recs []environmentRecord
	err := r.db.SelectContext(ctx, &recs, `
		SELECT `+environmentCols+` FROM environments
		WHERE project_id = $1 AND deleted_at IS NULL
		ORDER BY created_at DESC`, projectID.String())
	if err != nil {
		return nil, err
	}
	result := make([]*environmentdom.Environment, 0, len(recs))
	for _, rec := range recs {
		env := environmentFromRecord(rec)
		folders, err := r.ListFolders(ctx, env.ID)
		if err != nil {
			return nil, err
		}
		env.Folders = folders
		result = append(result, env)
	}
	return result, nil
}

// FindEnvironmentByID returns a single environment by ID, regardless of
// which project it belongs to.
func (r *EnvironmentRepository) FindEnvironmentByID(ctx context.Context, id uuid.UUID) (*environmentdom.Environment, error) {
	var rec environmentRecord
	err := r.db.GetContext(ctx, &rec, `
		SELECT `+environmentCols+` FROM environments
		WHERE id = $1 AND deleted_at IS NULL`, id.String())
	if errors.Is(err, sql.ErrNoRows) {
		return nil, environmentdom.ErrEnvironmentNotFound
	}
	if err != nil {
		return nil, err
	}
	return environmentFromRecord(rec), nil
}

// FindVisibleEnvironmentInProject returns a single environment by ID, but
// only if it belongs to projectID — see the interface doc comment.
func (r *EnvironmentRepository) FindVisibleEnvironmentInProject(ctx context.Context, projectID, environmentID uuid.UUID) (*environmentdom.Environment, error) {
	var rec environmentRecord
	err := r.db.GetContext(ctx, &rec, `
		SELECT `+environmentCols+` FROM environments
		WHERE id = $1 AND project_id = $2 AND deleted_at IS NULL`, environmentID.String(), projectID.String())
	if errors.Is(err, sql.ErrNoRows) {
		return nil, environmentdom.ErrEnvironmentNotFound
	}
	if err != nil {
		return nil, err
	}
	return environmentFromRecord(rec), nil
}

// CreateEnvironment inserts a new environment record.
func (r *EnvironmentRepository) CreateEnvironment(ctx context.Context, e *environmentdom.Environment) error {
	rec := environmentToRecord(e)
	_, err := r.db.ExecContext(ctx, `
		INSERT INTO environments (id, project_id, name, slug, status, backend, backend_ref, image,
		  cpu_limit, memory_limit, disk_limit_gb, volume_ref, secret_key_encrypted, idle_timeout_minutes,
		  last_active_at, error_message, created_by, created_at, updated_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19)`,
		rec.ID, rec.ProjectID, rec.Name, rec.Slug, rec.Status, rec.Backend, rec.BackendRef, rec.Image,
		rec.CPULimit, rec.MemoryLimit, rec.DiskLimitGB, rec.VolumeRef, rec.SecretKeyEncrypted, rec.IdleTimeoutMinutes,
		rec.LastActiveAt, rec.ErrorMessage, rec.CreatedBy, rec.CreatedAt, rec.UpdatedAt,
	)
	return err
}

// UpdateEnvironment saves the full environment record (mutable fields only —
// see UpdateEnvironmentInput).
func (r *EnvironmentRepository) UpdateEnvironment(ctx context.Context, e *environmentdom.Environment) error {
	rec := environmentToRecord(e)
	_, err := r.db.ExecContext(ctx, `
		UPDATE environments SET
		  name=$1, idle_timeout_minutes=$2, updated_at=$3
		WHERE id=$4`,
		rec.Name, rec.IdleTimeoutMinutes, rec.UpdatedAt, rec.ID,
	)
	return err
}

// UpdateEnvironmentStatus sets status (and, when non-nil, backendRef/errMsg)
// in one call — backend_ref is only overwritten when a non-nil pointer is
// given (COALESCE), since most status transitions (e.g. stopping a running
// environment) don't have a new backend_ref to report.
func (r *EnvironmentRepository) UpdateEnvironmentStatus(ctx context.Context, id uuid.UUID, status string, backendRef, errMsg *string) error {
	_, err := r.db.ExecContext(ctx, `
		UPDATE environments
		SET status=$1, backend_ref=COALESCE($2, backend_ref), error_message=$3, updated_at=$4
		WHERE id=$5`,
		status, backendRef, errMsg, time.Now(), id.String(),
	)
	return err
}

// UpdateEnvironmentProvisioning persists the outcome of a successful
// CreateEnvironment call to agent-runner in one write: status, backend,
// backend_ref, and volume_ref. Additive beyond UpdateEnvironmentStatus (see
// that interface method's doc comment) — UpdateEnvironmentStatus's
// signature has no room for volume_ref or a backend change, since every
// other status transition (Start/Stop/error) never needs to report either
// of those; only the very first successful provisioning call does.
func (r *EnvironmentRepository) UpdateEnvironmentProvisioning(ctx context.Context, id uuid.UUID, status, backend, backendRef, volumeRef string) error {
	_, err := r.db.ExecContext(ctx, `
		UPDATE environments
		SET status=$1, backend=$2, backend_ref=$3, volume_ref=$4, updated_at=$5
		WHERE id=$6`,
		status, backend, backendRef, volumeRef, time.Now(), id.String(),
	)
	return err
}

// TouchEnvironment bumps last_active_at to now.
func (r *EnvironmentRepository) TouchEnvironment(ctx context.Context, id uuid.UUID) error {
	_, err := r.db.ExecContext(ctx, `UPDATE environments SET last_active_at=$1 WHERE id=$2`, time.Now(), id.String())
	return err
}

// SoftDeleteEnvironment sets deleted_at on the environment row and clears
// agents.default_environment_id for any agent pointing at it in the same
// transaction. A real FK's ON DELETE SET NULL (which this column does
// have — migrations/000042) never fires here since this is a soft delete,
// not a row DELETE — without this, an agent's default_environment_id is
// left dangling and every StartChatSession/SendChatMessage against it
// fails with ErrEnvironmentNotFound until someone manually clears it.
func (r *EnvironmentRepository) SoftDeleteEnvironment(ctx context.Context, id uuid.UUID) error {
	return WithTx(ctx, r.db, func(tx *sqlx.Tx) error {
		if _, err := tx.ExecContext(ctx, `UPDATE environments SET deleted_at=$1 WHERE id=$2`, time.Now(), id.String()); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `UPDATE agents SET default_environment_id=NULL WHERE default_environment_id=$1`, id.String()); err != nil {
			return err
		}
		return nil
	})
}

// SlugTaken reports whether slug is already used by a non-deleted
// environment in projectID.
func (r *EnvironmentRepository) SlugTaken(ctx context.Context, projectID uuid.UUID, slug string) (bool, error) {
	var taken bool
	err := r.db.GetContext(ctx, &taken,
		`SELECT EXISTS(SELECT 1 FROM environments WHERE project_id = $1 AND slug = $2 AND deleted_at IS NULL)`,
		projectID.String(), slug)
	return taken, err
}

// SetPortsPendingRestart writes Environment.PortsPendingRestart — see that
// field's own doc comment.
func (r *EnvironmentRepository) SetPortsPendingRestart(ctx context.Context, id uuid.UUID, pending bool) error {
	_, err := r.db.ExecContext(ctx,
		`UPDATE environments SET ports_pending_restart = $1, updated_at = $2 WHERE id = $3`,
		pending, time.Now(), id.String())
	return err
}

// -------------------------------------------------------------------------
// Folders
// -------------------------------------------------------------------------

const environmentFolderCols = `id, environment_id, path, created_by, created_at, updated_at`

// ListFolders returns every folder in an environment, oldest first (the
// order they were added — mirrors ListMCPServers-style listing elsewhere in
// this package).
func (r *EnvironmentRepository) ListFolders(ctx context.Context, environmentID uuid.UUID) ([]*environmentdom.EnvironmentFolder, error) {
	var recs []environmentFolderRecord
	err := r.db.SelectContext(ctx, &recs, `
		SELECT `+environmentFolderCols+` FROM environment_folders
		WHERE environment_id = $1
		ORDER BY created_at ASC`, environmentID.String())
	if err != nil {
		return nil, err
	}
	result := make([]*environmentdom.EnvironmentFolder, 0, len(recs))
	for _, rec := range recs {
		result = append(result, envFolderFromRecord(rec))
	}
	return result, nil
}

// FindFolderByID returns a single folder by ID.
func (r *EnvironmentRepository) FindFolderByID(ctx context.Context, id uuid.UUID) (*environmentdom.EnvironmentFolder, error) {
	var rec environmentFolderRecord
	err := r.db.GetContext(ctx, &rec, `
		SELECT `+environmentFolderCols+` FROM environment_folders WHERE id = $1`, id.String())
	if errors.Is(err, sql.ErrNoRows) {
		return nil, environmentdom.ErrFolderNotFound
	}
	if err != nil {
		return nil, err
	}
	return envFolderFromRecord(rec), nil
}

// CreateFolder inserts a new folder record.
func (r *EnvironmentRepository) CreateFolder(ctx context.Context, f *environmentdom.EnvironmentFolder) error {
	rec := envFolderToRecord(f)
	_, err := r.db.ExecContext(ctx, `
		INSERT INTO environment_folders (id, environment_id, path, created_by, created_at, updated_at)
		VALUES ($1,$2,$3,$4,$5,$6)`,
		rec.ID, rec.EnvironmentID, rec.Path, rec.CreatedBy, rec.CreatedAt, rec.UpdatedAt,
	)
	if err != nil {
		// uq_environment_folders_path is the only unique constraint on this
		// table, so any violation here unambiguously means the path is
		// already in use — translated to the domain sentinel at the
		// repository boundary (reusing global_role_repository.go's
		// uniqueViolationConstraint, package-shared) rather than requiring
		// the service to pre-check via ListFolders, which would race.
		if _, ok := uniqueViolationConstraint(err); ok {
			return environmentdom.ErrFolderPathTaken
		}
		return err
	}
	return nil
}

// DeleteFolder hard-deletes a folder row. Folders have no soft-delete
// column (unlike environments) — path uniqueness (uq_environment_folders_path)
// is a plain per-environment unique index, not a partial one, so a deleted
// folder's path must actually be gone for the same path to be re-added.
func (r *EnvironmentRepository) DeleteFolder(ctx context.Context, id uuid.UUID) error {
	_, err := r.db.ExecContext(ctx, `DELETE FROM environment_folders WHERE id = $1`, id.String())
	return err
}

// -------------------------------------------------------------------------
// SSH keys
// -------------------------------------------------------------------------

const environmentSSHKeyCols = `id, environment_id, label, public_key, fingerprint, created_by, created_at`

// ListSSHKeys returns every SSH key registered on an environment, oldest first.
func (r *EnvironmentRepository) ListSSHKeys(ctx context.Context, environmentID uuid.UUID) ([]*environmentdom.EnvironmentSSHKey, error) {
	var recs []environmentSSHKeyRecord
	err := r.db.SelectContext(ctx, &recs, `
		SELECT `+environmentSSHKeyCols+` FROM environment_ssh_keys
		WHERE environment_id = $1
		ORDER BY created_at ASC`, environmentID.String())
	if err != nil {
		return nil, err
	}
	result := make([]*environmentdom.EnvironmentSSHKey, 0, len(recs))
	for _, rec := range recs {
		result = append(result, sshKeyFromRecord(rec))
	}
	return result, nil
}

// FindSSHKeyByID returns a single SSH key by ID.
func (r *EnvironmentRepository) FindSSHKeyByID(ctx context.Context, id uuid.UUID) (*environmentdom.EnvironmentSSHKey, error) {
	var rec environmentSSHKeyRecord
	err := r.db.GetContext(ctx, &rec, `
		SELECT `+environmentSSHKeyCols+` FROM environment_ssh_keys WHERE id = $1`, id.String())
	if errors.Is(err, sql.ErrNoRows) {
		return nil, environmentdom.ErrSSHKeyNotFound
	}
	if err != nil {
		return nil, err
	}
	return sshKeyFromRecord(rec), nil
}

// CreateSSHKey inserts a new SSH key record.
func (r *EnvironmentRepository) CreateSSHKey(ctx context.Context, k *environmentdom.EnvironmentSSHKey) error {
	rec := sshKeyToRecord(k)
	_, err := r.db.ExecContext(ctx, `
		INSERT INTO environment_ssh_keys (id, environment_id, label, public_key, fingerprint, created_by, created_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7)`,
		rec.ID, rec.EnvironmentID, rec.Label, rec.PublicKey, rec.Fingerprint, rec.CreatedBy, rec.CreatedAt,
	)
	if err != nil {
		// uq_environment_ssh_keys_fingerprint is the only unique constraint
		// here — same backstop-at-the-repository-boundary rationale as
		// CreateFolder above (the service also pre-checks via
		// FindSSHKeyByFingerprint, but that check alone is racy).
		if _, ok := uniqueViolationConstraint(err); ok {
			return environmentdom.ErrSSHKeyFingerprintTaken
		}
		return err
	}
	return nil
}

// DeleteSSHKey hard-deletes an SSH key row — no soft-delete: a removed key
// must stop authenticating immediately and permanently, with no "undelete"
// path, matching how a revoked deploy key behaves.
func (r *EnvironmentRepository) DeleteSSHKey(ctx context.Context, id uuid.UUID) error {
	_, err := r.db.ExecContext(ctx, `DELETE FROM environment_ssh_keys WHERE id = $1`, id.String())
	return err
}

// FindSSHKeyByFingerprint resolves the environment an inbound SSH
// connection is authorized for, by the public key fingerprint it presented.
func (r *EnvironmentRepository) FindSSHKeyByFingerprint(ctx context.Context, environmentID uuid.UUID, fingerprint string) (*environmentdom.EnvironmentSSHKey, error) {
	var rec environmentSSHKeyRecord
	err := r.db.GetContext(ctx, &rec, `
		SELECT `+environmentSSHKeyCols+` FROM environment_ssh_keys
		WHERE environment_id = $1 AND fingerprint = $2`, environmentID.String(), fingerprint)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, environmentdom.ErrSSHKeyNotFound
	}
	if err != nil {
		return nil, err
	}
	return sshKeyFromRecord(rec), nil
}

// -------------------------------------------------------------------------
// Port forwards
// -------------------------------------------------------------------------

const environmentPortForwardCols = `id, environment_id, label, container_port, host_port, created_by, created_at`

// ListPortForwards returns every port forward on an environment, oldest first.
func (r *EnvironmentRepository) ListPortForwards(ctx context.Context, environmentID uuid.UUID) ([]*environmentdom.EnvironmentPortForward, error) {
	var recs []environmentPortForwardRecord
	err := r.db.SelectContext(ctx, &recs, `
		SELECT `+environmentPortForwardCols+` FROM environment_port_forwards
		WHERE environment_id = $1
		ORDER BY created_at ASC`, environmentID.String())
	if err != nil {
		return nil, err
	}
	result := make([]*environmentdom.EnvironmentPortForward, 0, len(recs))
	for _, rec := range recs {
		result = append(result, envPortForwardFromRecord(rec))
	}
	return result, nil
}

// FindPortForwardByID returns a single port forward by ID.
func (r *EnvironmentRepository) FindPortForwardByID(ctx context.Context, id uuid.UUID) (*environmentdom.EnvironmentPortForward, error) {
	var rec environmentPortForwardRecord
	err := r.db.GetContext(ctx, &rec, `
		SELECT `+environmentPortForwardCols+` FROM environment_port_forwards WHERE id = $1`, id.String())
	if errors.Is(err, sql.ErrNoRows) {
		return nil, environmentdom.ErrPortForwardNotFound
	}
	if err != nil {
		return nil, err
	}
	return envPortForwardFromRecord(rec), nil
}

// CreatePortForward inserts a new port forward record. host_port is never
// written here — it's assigned once by agent-runner's own Postgres
// connection, the same "generated once, nil until assigned" lifecycle as
// Environment.SSHPort (see that field's doc comment).
func (r *EnvironmentRepository) CreatePortForward(ctx context.Context, pf *environmentdom.EnvironmentPortForward) error {
	rec := envPortForwardToRecord(pf)
	_, err := r.db.ExecContext(ctx, `
		INSERT INTO environment_port_forwards (id, environment_id, label, container_port, created_by, created_at)
		VALUES ($1,$2,$3,$4,$5,$6)`,
		rec.ID, rec.EnvironmentID, rec.Label, rec.ContainerPort, rec.CreatedBy, rec.CreatedAt,
	)
	if err != nil {
		// uq_environment_port_forwards_container_port is the only unique
		// constraint on insert here — same backstop-at-the-repository-
		// boundary rationale as CreateFolder above.
		if _, ok := uniqueViolationConstraint(err); ok {
			return environmentdom.ErrPortForwardContainerPortTaken
		}
		return err
	}
	return nil
}

// DeletePortForward hard-deletes a port forward row — no soft-delete: a
// removed forward must stop relaying immediately and permanently, and its
// host_port must be immediately free for reuse (uq_environment_port_forwards_host_port
// has no partial-on-deleted_at clause, unlike uq_environments_ssh_port).
func (r *EnvironmentRepository) DeletePortForward(ctx context.Context, id uuid.UUID) error {
	_, err := r.db.ExecContext(ctx, `DELETE FROM environment_port_forwards WHERE id = $1`, id.String())
	return err
}

// -------------------------------------------------------------------------
// record <-> domain mapping
// -------------------------------------------------------------------------

func environmentFromRecord(rec environmentRecord) *environmentdom.Environment {
	e := &environmentdom.Environment{
		ID:                  mustParseUUID(rec.ID),
		ProjectID:           mustParseUUID(rec.ProjectID),
		Name:                rec.Name,
		Slug:                rec.Slug,
		SSHPort:             rec.SSHPort,
		PortsPendingRestart: rec.PortsPendingRestart,
		Status:              rec.Status,
		Backend:             rec.Backend,
		BackendRef:          rec.BackendRef,
		Image:               rec.Image,
		CPULimit:            rec.CPULimit,
		MemoryLimit:         rec.MemoryLimit,
		DiskLimitGB:         rec.DiskLimitGB,
		VolumeRef:           rec.VolumeRef,
		SecretKeyEncrypted:  rec.SecretKeyEncrypted,
		IdleTimeoutMinutes:  rec.IdleTimeoutMinutes,
		LastActiveAt:        rec.LastActiveAt,
		ErrorMessage:        rec.ErrorMessage,
		CreatedAt:           rec.CreatedAt,
		UpdatedAt:           rec.UpdatedAt,
		DeletedAt:           rec.DeletedAt,
	}
	if rec.CreatedBy != nil {
		id := mustParseUUID(*rec.CreatedBy)
		e.CreatedBy = &id
	}
	return e
}

func environmentToRecord(e *environmentdom.Environment) environmentRecord {
	rec := environmentRecord{
		ID:                 e.ID.String(),
		ProjectID:          e.ProjectID.String(),
		Name:               e.Name,
		Slug:               e.Slug,
		Status:             e.Status,
		Backend:            e.Backend,
		BackendRef:         e.BackendRef,
		Image:              e.Image,
		CPULimit:           e.CPULimit,
		MemoryLimit:        e.MemoryLimit,
		DiskLimitGB:        e.DiskLimitGB,
		VolumeRef:          e.VolumeRef,
		SecretKeyEncrypted: e.SecretKeyEncrypted,
		IdleTimeoutMinutes: e.IdleTimeoutMinutes,
		LastActiveAt:       e.LastActiveAt,
		ErrorMessage:       e.ErrorMessage,
		CreatedAt:          e.CreatedAt,
		UpdatedAt:          e.UpdatedAt,
		DeletedAt:          e.DeletedAt,
	}
	if e.CreatedBy != nil {
		s := e.CreatedBy.String()
		rec.CreatedBy = &s
	}
	return rec
}

func envFolderFromRecord(rec environmentFolderRecord) *environmentdom.EnvironmentFolder {
	f := &environmentdom.EnvironmentFolder{
		ID:            mustParseUUID(rec.ID),
		EnvironmentID: mustParseUUID(rec.EnvironmentID),
		Path:          rec.Path,
		CreatedAt:     rec.CreatedAt,
		UpdatedAt:     rec.UpdatedAt,
	}
	if rec.CreatedBy != nil {
		id := mustParseUUID(*rec.CreatedBy)
		f.CreatedBy = &id
	}
	return f
}

func envFolderToRecord(f *environmentdom.EnvironmentFolder) environmentFolderRecord {
	rec := environmentFolderRecord{
		ID:            f.ID.String(),
		EnvironmentID: f.EnvironmentID.String(),
		Path:          f.Path,
		CreatedAt:     f.CreatedAt,
		UpdatedAt:     f.UpdatedAt,
	}
	if f.CreatedBy != nil {
		s := f.CreatedBy.String()
		rec.CreatedBy = &s
	}
	return rec
}

func sshKeyFromRecord(rec environmentSSHKeyRecord) *environmentdom.EnvironmentSSHKey {
	k := &environmentdom.EnvironmentSSHKey{
		ID:            mustParseUUID(rec.ID),
		EnvironmentID: mustParseUUID(rec.EnvironmentID),
		Label:         rec.Label,
		PublicKey:     rec.PublicKey,
		Fingerprint:   rec.Fingerprint,
		CreatedAt:     rec.CreatedAt,
	}
	if rec.CreatedBy != nil {
		id := mustParseUUID(*rec.CreatedBy)
		k.CreatedBy = &id
	}
	return k
}

func sshKeyToRecord(k *environmentdom.EnvironmentSSHKey) environmentSSHKeyRecord {
	rec := environmentSSHKeyRecord{
		ID:            k.ID.String(),
		EnvironmentID: k.EnvironmentID.String(),
		Label:         k.Label,
		PublicKey:     k.PublicKey,
		Fingerprint:   k.Fingerprint,
		CreatedAt:     k.CreatedAt,
	}
	if k.CreatedBy != nil {
		s := k.CreatedBy.String()
		rec.CreatedBy = &s
	}
	return rec
}

func envPortForwardFromRecord(rec environmentPortForwardRecord) *environmentdom.EnvironmentPortForward {
	pf := &environmentdom.EnvironmentPortForward{
		ID:            mustParseUUID(rec.ID),
		EnvironmentID: mustParseUUID(rec.EnvironmentID),
		Label:         rec.Label,
		ContainerPort: rec.ContainerPort,
		HostPort:      rec.HostPort,
		CreatedAt:     rec.CreatedAt,
	}
	if rec.CreatedBy != nil {
		id := mustParseUUID(*rec.CreatedBy)
		pf.CreatedBy = &id
	}
	return pf
}

func envPortForwardToRecord(pf *environmentdom.EnvironmentPortForward) environmentPortForwardRecord {
	rec := environmentPortForwardRecord{
		ID:            pf.ID.String(),
		EnvironmentID: pf.EnvironmentID.String(),
		Label:         pf.Label,
		ContainerPort: pf.ContainerPort,
		HostPort:      pf.HostPort,
		CreatedAt:     pf.CreatedAt,
	}
	if pf.CreatedBy != nil {
		s := pf.CreatedBy.String()
		rec.CreatedBy = &s
	}
	return rec
}
