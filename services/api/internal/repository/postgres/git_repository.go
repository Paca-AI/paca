package postgres

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jmoiron/sqlx"

	gitdom "github.com/Paca-AI/api/internal/domain/git"
)

// GitRepository is the sqlx implementation of gitdom.Store.
type GitRepository struct{ db *sqlx.DB }

var _ gitdom.Store = (*GitRepository)(nil)

// NewGitRepository returns the Git integration store.
func NewGitRepository(db *sqlx.DB) *GitRepository { return &GitRepository{db: db} }

// ── records ──────────────────────────────────────────────────────────────────

type gitIdentityRecord struct {
	ID                 string          `db:"id"`
	ProviderID         string          `db:"provider_id"`
	Host               string          `db:"host"`
	ProviderIdentityID string          `db:"provider_identity_id"`
	Kind               string          `db:"kind"`
	Scope              string          `db:"scope"`
	ProjectID          *string         `db:"project_id"`
	Handle             string          `db:"handle"`
	DisplayName        string          `db:"display_name"`
	ProviderRef        json.RawMessage `db:"provider_ref"`
	ExpiresAt          *time.Time      `db:"expires_at"`
	CreatedBy          *string         `db:"created_by"`
	CreatedAt          time.Time       `db:"created_at"`
	UpdatedAt          time.Time       `db:"updated_at"`
}

const gitIdentityCols = `id, provider_id, host, provider_identity_id, kind, scope, project_id, handle, display_name, provider_ref, expires_at, created_by, created_at, updated_at`

func (r gitIdentityRecord) domain() (*gitdom.Identity, error) {
	id, err := uuid.Parse(r.ID)
	if err != nil {
		return nil, fmt.Errorf("git repo: bad identity id: %w", err)
	}
	return &gitdom.Identity{
		ID: id, ProviderID: r.ProviderID, Host: r.Host, ProviderIdentityID: r.ProviderIdentityID,
		Kind: r.Kind, Scope: gitdom.IdentityScope(r.Scope), ProjectID: parseOptUUID(r.ProjectID), Handle: r.Handle, DisplayName: r.DisplayName,
		ProviderRef: r.ProviderRef, ExpiresAt: r.ExpiresAt, CreatedBy: parseOptUUID(r.CreatedBy),
		CreatedAt: r.CreatedAt, UpdatedAt: r.UpdatedAt,
	}, nil
}

type gitRepositoryRecord struct {
	ID            string          `db:"id"`
	ProjectID     string          `db:"project_id"`
	ProviderID    string          `db:"provider_id"`
	Host          string          `db:"host"`
	ExternalID    string          `db:"external_id"`
	FullPath      string          `db:"full_path"`
	DefaultBranch string          `db:"default_branch"`
	CloneURL      string          `db:"clone_url"`
	Description   string          `db:"description"`
	IdentityID    *string         `db:"identity_id"`
	ProviderRef   json.RawMessage `db:"provider_ref"`
	WebhookRef    json.RawMessage `db:"webhook_ref"`
	CreatedBy     *string         `db:"created_by"`
	CreatedAt     time.Time       `db:"created_at"`
	UpdatedAt     time.Time       `db:"updated_at"`
}

const gitRepositoryCols = `id, project_id, provider_id, host, external_id, full_path, default_branch, clone_url, description, identity_id, provider_ref, webhook_ref, created_by, created_at, updated_at`

func (r gitRepositoryRecord) domain() (*gitdom.Repository, error) {
	id, err := uuid.Parse(r.ID)
	if err != nil {
		return nil, fmt.Errorf("git repo: bad repository id: %w", err)
	}
	pid, err := uuid.Parse(r.ProjectID)
	if err != nil {
		return nil, fmt.Errorf("git repo: bad project id: %w", err)
	}
	return &gitdom.Repository{
		ID: id, ProjectID: pid, ProviderID: r.ProviderID, Host: r.Host, ExternalID: r.ExternalID,
		FullPath: r.FullPath, DefaultBranch: r.DefaultBranch, CloneURL: r.CloneURL, Description: r.Description,
		IdentityID: parseOptUUID(r.IdentityID), ProviderRef: r.ProviderRef, WebhookRef: r.WebhookRef,
		CreatedBy: parseOptUUID(r.CreatedBy), CreatedAt: r.CreatedAt, UpdatedAt: r.UpdatedAt,
	}, nil
}

func parseOptUUID(s *string) *uuid.UUID {
	if s == nil {
		return nil
	}
	u, err := uuid.Parse(*s)
	if err != nil {
		return nil
	}
	return &u
}

func optUUIDArg(u *uuid.UUID) any {
	if u == nil {
		return nil
	}
	return u.String()
}

func jsonOrEmpty(raw json.RawMessage) json.RawMessage {
	if len(raw) == 0 {
		return json.RawMessage(`{}`)
	}
	return raw
}

// ── identities ───────────────────────────────────────────────────────────────

// CreateIdentity inserts a new identity.
func (s *GitRepository) CreateIdentity(ctx context.Context, i *gitdom.Identity) error {
	var rec struct {
		ID        string    `db:"id"`
		CreatedAt time.Time `db:"created_at"`
		UpdatedAt time.Time `db:"updated_at"`
	}
	err := s.db.GetContext(ctx, &rec, `
		INSERT INTO git_identities (provider_id, host, provider_identity_id, kind, scope, project_id, handle, display_name, provider_ref, expires_at, created_by)
		VALUES ($1,$2,$3,$4,$5,$6::uuid,$7,$8,$9::jsonb,$10,$11::uuid)
		RETURNING id, created_at, updated_at`,
		i.ProviderID, i.Host, i.ProviderIdentityID, i.Kind, string(i.Scope), optUUIDArg(i.ProjectID), i.Handle, i.DisplayName,
		string(jsonOrEmpty(i.ProviderRef)), i.ExpiresAt, optUUIDArg(i.CreatedBy))
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			return gitdom.ErrIdentityExists
		}
		return fmt.Errorf("git repo: create identity: %w", err)
	}
	id, err := uuid.Parse(rec.ID)
	if err != nil {
		return fmt.Errorf("git repo: create identity: bad id: %w", err)
	}
	i.ID, i.CreatedAt, i.UpdatedAt = id, rec.CreatedAt, rec.UpdatedAt
	return nil
}

func (s *GitRepository) getIdentity(ctx context.Context, where string, args ...any) (*gitdom.Identity, error) {
	var rec gitIdentityRecord
	if err := s.db.GetContext(ctx, &rec, `SELECT `+gitIdentityCols+` FROM git_identities WHERE `+where, args...); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, gitdom.ErrNotFound
		}
		return nil, fmt.Errorf("git repo: find identity: %w", err)
	}
	return rec.domain()
}

// FindIdentity returns one identity by id.
func (s *GitRepository) FindIdentity(ctx context.Context, id uuid.UUID) (*gitdom.Identity, error) {
	return s.getIdentity(ctx, `id = $1`, id.String())
}

// FindIdentityByProviderRef returns the identity a provider knows by its own id.
func (s *GitRepository) FindIdentityByProviderRef(ctx context.Context, providerID, providerIdentityID string) (*gitdom.Identity, error) {
	return s.getIdentity(ctx, `provider_id = $1 AND provider_identity_id = $2`, providerID, providerIdentityID)
}

// ListSharedIdentities lists a provider's shared identities (every host).
func (s *GitRepository) ListSharedIdentities(ctx context.Context, providerID string) ([]*gitdom.Identity, error) {
	var recs []gitIdentityRecord
	if err := s.db.SelectContext(ctx, &recs, `SELECT `+gitIdentityCols+` FROM git_identities WHERE provider_id = $1 AND scope = 'shared' ORDER BY host, created_at`, providerID); err != nil {
		return nil, fmt.Errorf("git repo: list identities: %w", err)
	}
	out := make([]*gitdom.Identity, 0, len(recs))
	for _, r := range recs {
		d, err := r.domain()
		if err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, nil
}

// ListProjectIdentities lists the connections a project can use: the
// workspace's and its own.
func (s *GitRepository) ListProjectIdentities(ctx context.Context, projectID uuid.UUID) ([]*gitdom.Identity, error) {
	var recs []gitIdentityRecord
	if err := s.db.SelectContext(ctx, &recs, `SELECT `+gitIdentityCols+` FROM git_identities
		WHERE scope = 'shared' OR (scope = 'project' AND project_id = $1)
		ORDER BY provider_id, host, created_at`, projectID.String()); err != nil {
		return nil, fmt.Errorf("git repo: list project identities: %w", err)
	}
	out := make([]*gitdom.Identity, 0, len(recs))
	for _, r := range recs {
		d, err := r.domain()
		if err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, nil
}

// SetAgentIdentity points an agent at its own identity for a provider.
func (s *GitRepository) SetAgentIdentity(ctx context.Context, agentID uuid.UUID, providerID string, identityID uuid.UUID) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO git_agent_identities (agent_id, provider_id, identity_id) VALUES ($1,$2,$3)
		ON CONFLICT (agent_id, provider_id) DO UPDATE SET identity_id = EXCLUDED.identity_id`,
		agentID.String(), providerID, identityID.String())
	if err != nil {
		return fmt.Errorf("git repo: set agent identity: %w", err)
	}
	return nil
}

// FindAgentIdentity returns an agent's own identity for a provider.
func (s *GitRepository) FindAgentIdentity(ctx context.Context, agentID uuid.UUID, providerID string) (*gitdom.Identity, error) {
	var rec gitIdentityRecord
	err := s.db.GetContext(ctx, &rec, `
		SELECT i.id, i.provider_id, i.host, i.provider_identity_id, i.kind, i.scope, i.project_id, i.handle, i.display_name,
		       i.provider_ref, i.expires_at, i.created_by, i.created_at, i.updated_at
		FROM git_agent_identities a JOIN git_identities i ON i.id = a.identity_id
		WHERE a.agent_id = $1 AND a.provider_id = $2`, agentID.String(), providerID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, gitdom.ErrNotFound
		}
		return nil, fmt.Errorf("git repo: find agent identity: %w", err)
	}
	return rec.domain()
}

// DeleteIdentity removes an identity (and, by cascade, its agent mappings and endpoint).
func (s *GitRepository) DeleteIdentity(ctx context.Context, id uuid.UUID) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM git_identities WHERE id = $1`, id.String())
	if err != nil {
		return fmt.Errorf("git repo: delete identity: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return gitdom.ErrNotFound
	}
	return nil
}

// ── webhook endpoints ────────────────────────────────────────────────────────

// CreateWebhookEndpoint stores a (possibly still unbound) endpoint.
func (s *GitRepository) CreateWebhookEndpoint(ctx context.Context, e *gitdom.WebhookEndpoint) error {
	var created time.Time
	err := s.db.GetContext(ctx, &created, `
		INSERT INTO git_webhook_endpoints (key, provider_id, identity_id) VALUES ($1,$2,$3::uuid) RETURNING created_at`,
		e.Key, e.ProviderID, optUUIDArg(e.IdentityID))
	if err != nil {
		return fmt.Errorf("git repo: create webhook endpoint: %w", err)
	}
	e.CreatedAt = created
	return nil
}

type gitEndpointRecord struct {
	Key        string    `db:"key"`
	ProviderID string    `db:"provider_id"`
	IdentityID *string   `db:"identity_id"`
	CreatedAt  time.Time `db:"created_at"`
}

func (r gitEndpointRecord) domain() *gitdom.WebhookEndpoint {
	return &gitdom.WebhookEndpoint{Key: r.Key, ProviderID: r.ProviderID, IdentityID: parseOptUUID(r.IdentityID), CreatedAt: r.CreatedAt}
}

// FindWebhookEndpoint looks an endpoint up by its URL key.
func (s *GitRepository) FindWebhookEndpoint(ctx context.Context, key string) (*gitdom.WebhookEndpoint, error) {
	var rec gitEndpointRecord
	if err := s.db.GetContext(ctx, &rec, `SELECT key, provider_id, identity_id, created_at FROM git_webhook_endpoints WHERE key = $1`, key); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, gitdom.ErrNotFound
		}
		return nil, fmt.Errorf("git repo: find webhook endpoint: %w", err)
	}
	return rec.domain(), nil
}

// BindWebhookEndpoint attaches a pending endpoint to its identity.
func (s *GitRepository) BindWebhookEndpoint(ctx context.Context, key string, identityID uuid.UUID) error {
	res, err := s.db.ExecContext(ctx, `UPDATE git_webhook_endpoints SET identity_id = $2 WHERE key = $1`, key, identityID.String())
	if err != nil {
		return fmt.Errorf("git repo: bind webhook endpoint: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return gitdom.ErrNotFound
	}
	return nil
}

// FindWebhookEndpointForIdentity returns the endpoint bound to an identity.
func (s *GitRepository) FindWebhookEndpointForIdentity(ctx context.Context, identityID uuid.UUID) (*gitdom.WebhookEndpoint, error) {
	var rec gitEndpointRecord
	if err := s.db.GetContext(ctx, &rec, `SELECT key, provider_id, identity_id, created_at FROM git_webhook_endpoints WHERE identity_id = $1 ORDER BY created_at LIMIT 1`, identityID.String()); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, gitdom.ErrNotFound
		}
		return nil, fmt.Errorf("git repo: find webhook endpoint for identity: %w", err)
	}
	return rec.domain(), nil
}

// ── repositories ─────────────────────────────────────────────────────────────

// CreateRepository links a repository to a project.
func (s *GitRepository) CreateRepository(ctx context.Context, r *gitdom.Repository) error {
	var rec struct {
		ID        string    `db:"id"`
		CreatedAt time.Time `db:"created_at"`
		UpdatedAt time.Time `db:"updated_at"`
	}
	err := s.db.GetContext(ctx, &rec, `
		INSERT INTO git_repositories (project_id, provider_id, host, external_id, full_path, default_branch, clone_url, description, identity_id, provider_ref, webhook_ref, created_by)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9::uuid,$10::jsonb,$11::jsonb,$12::uuid)
		RETURNING id, created_at, updated_at`,
		r.ProjectID.String(), r.ProviderID, r.Host, r.ExternalID, r.FullPath, r.DefaultBranch, r.CloneURL, r.Description,
		optUUIDArg(r.IdentityID), string(jsonOrEmpty(r.ProviderRef)), string(jsonOrEmpty(r.WebhookRef)), optUUIDArg(r.CreatedBy))
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			return gitdom.ErrAlreadyLinked
		}
		return fmt.Errorf("git repo: create repository: %w", err)
	}
	id, err := uuid.Parse(rec.ID)
	if err != nil {
		return fmt.Errorf("git repo: create repository: bad id: %w", err)
	}
	r.ID, r.CreatedAt, r.UpdatedAt = id, rec.CreatedAt, rec.UpdatedAt
	return nil
}

// FindRepository returns one linked repository.
func (s *GitRepository) FindRepository(ctx context.Context, id uuid.UUID) (*gitdom.Repository, error) {
	var rec gitRepositoryRecord
	if err := s.db.GetContext(ctx, &rec, `SELECT `+gitRepositoryCols+` FROM git_repositories WHERE id = $1`, id.String()); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, gitdom.ErrNotFound
		}
		return nil, fmt.Errorf("git repo: find repository: %w", err)
	}
	return rec.domain()
}

func (s *GitRepository) selectRepos(ctx context.Context, where string, args ...any) ([]*gitdom.Repository, error) {
	var recs []gitRepositoryRecord
	if err := s.db.SelectContext(ctx, &recs, `SELECT `+gitRepositoryCols+` FROM git_repositories WHERE `+where, args...); err != nil {
		return nil, fmt.Errorf("git repo: list repositories: %w", err)
	}
	out := make([]*gitdom.Repository, 0, len(recs))
	for _, r := range recs {
		d, err := r.domain()
		if err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, nil
}

// ListProjectRepositories lists a project's linked repositories.
func (s *GitRepository) ListProjectRepositories(ctx context.Context, projectID uuid.UUID) ([]*gitdom.Repository, error) {
	repos, err := s.selectRepos(ctx, `project_id = $1 ORDER BY created_at`, projectID.String())
	if err != nil {
		return nil, err
	}
	// The handle of the bot serving each repository, so the UI can say what to @mention.
	var handles []struct {
		ID     string `db:"id"`
		Handle string `db:"handle"`
	}
	if err := s.db.SelectContext(ctx, &handles, `SELECT r.id, i.handle FROM git_repositories r
		JOIN git_identities i ON i.id = r.identity_id WHERE r.project_id = $1`, projectID.String()); err != nil {
		return nil, fmt.Errorf("git repo: list repository handles: %w", err)
	}
	byID := make(map[string]string, len(handles))
	for _, h := range handles {
		byID[h.ID] = h.Handle
	}
	for _, r := range repos {
		r.IdentityHandle = byID[r.ID.String()]
	}
	return repos, nil
}

// WebhookAttempts returns the project's repositories for which registering a
// webhook was ever attempted, read from the audit log.
func (s *GitRepository) WebhookAttempts(ctx context.Context, projectID uuid.UUID) (map[uuid.UUID]bool, error) {
	var ids []string
	if err := s.db.SelectContext(ctx, &ids, `SELECT DISTINCT repository_id FROM git_action_log
		WHERE project_id = $1 AND tool = 'webhook.register' AND repository_id IS NOT NULL`, projectID.String()); err != nil {
		return nil, fmt.Errorf("git repo: list webhook attempts: %w", err)
	}
	out := make(map[uuid.UUID]bool, len(ids))
	for _, id := range ids {
		u, err := uuid.Parse(id)
		if err != nil {
			return nil, fmt.Errorf("git repo: list webhook attempts: bad id: %w", err)
		}
		out[u] = true
	}
	return out, nil
}

// DeleteRepository unlinks a repository from its project.
func (s *GitRepository) DeleteRepository(ctx context.Context, projectID, id uuid.UUID) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM git_repositories WHERE id = $1 AND project_id = $2`, id.String(), projectID.String())
	if err != nil {
		return fmt.Errorf("git repo: delete repository: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return gitdom.ErrNotFound
	}
	return nil
}

// ListIdentityRepositories lists the repositories linked through a connection.
func (s *GitRepository) ListIdentityRepositories(ctx context.Context, identityID uuid.UUID) ([]*gitdom.Repository, error) {
	return s.selectRepos(ctx, `identity_id = $1 ORDER BY created_at`, identityID.String())
}

// SetRepositoryDescription updates the free-text description of a project's
// repository.
func (s *GitRepository) SetRepositoryDescription(ctx context.Context, projectID, id uuid.UUID, description string) error {
	res, err := s.db.ExecContext(ctx, `UPDATE git_repositories SET description = $3, updated_at = NOW() WHERE id = $1 AND project_id = $2`,
		id.String(), projectID.String(), description)
	if err != nil {
		return fmt.Errorf("git repo: set repository description: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return gitdom.ErrNotFound
	}
	return nil
}

// SetRepositoryWebhook records the webhook registered for a repository.
func (s *GitRepository) SetRepositoryWebhook(ctx context.Context, id uuid.UUID, ref json.RawMessage) error {
	res, err := s.db.ExecContext(ctx, `UPDATE git_repositories SET webhook_ref = $2::jsonb, updated_at = NOW() WHERE id = $1`,
		id.String(), string(jsonOrEmpty(ref)))
	if err != nil {
		return fmt.Errorf("git repo: set repository webhook: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return gitdom.ErrNotFound
	}
	return nil
}

// SetRepositoryIdentity rebinds a repository to a connection and forgets the
// webhook registered through the previous one.
func (s *GitRepository) SetRepositoryIdentity(ctx context.Context, id, identityID uuid.UUID, providerRef json.RawMessage) error {
	res, err := s.db.ExecContext(ctx, `UPDATE git_repositories SET identity_id = $2, provider_ref = $3::jsonb, webhook_ref = '{}'::jsonb, updated_at = NOW() WHERE id = $1`,
		id.String(), identityID.String(), string(jsonOrEmpty(providerRef)))
	if err != nil {
		return fmt.Errorf("git repo: set repository identity: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return gitdom.ErrNotFound
	}
	return nil
}

// CountRepositories counts the linked repositories of each connection.
func (s *GitRepository) CountRepositories(ctx context.Context, identityIDs []uuid.UUID) (map[uuid.UUID]int, error) {
	out := make(map[uuid.UUID]int, len(identityIDs))
	if len(identityIDs) == 0 {
		return out, nil
	}
	ids := make([]string, len(identityIDs))
	for i, id := range identityIDs {
		ids[i] = id.String()
	}
	var rows []struct {
		ID string `db:"identity_id"`
		N  int    `db:"n"`
	}
	if err := s.db.SelectContext(ctx, &rows, `SELECT identity_id, COUNT(*) AS n FROM git_repositories WHERE identity_id = ANY($1::uuid[]) GROUP BY identity_id`, ids); err != nil {
		return nil, fmt.Errorf("git repo: count repositories: %w", err)
	}
	for _, r := range rows {
		if id, err := uuid.Parse(r.ID); err == nil {
			out[id] = r.N
		}
	}
	return out, nil
}

// FindLinkedRepositories returns every project's link to a repository.
func (s *GitRepository) FindLinkedRepositories(ctx context.Context, providerID, host, fullPath string) ([]*gitdom.Repository, error) {
	return s.selectRepos(ctx, `provider_id = $1 AND host = $2 AND lower(full_path) = lower($3) ORDER BY created_at`, providerID, host, fullPath)
}

// ── inbox ────────────────────────────────────────────────────────────────────

// InsertInboxEvent stores an event once per (event id, repository).
func (s *GitRepository) InsertInboxEvent(ctx context.Context, e *gitdom.InboxEvent) (bool, error) {
	status := e.Status
	if status == "" {
		status = gitdom.InboxReceived
	}
	res, err := s.db.ExecContext(ctx, `
		INSERT INTO git_event_inbox (event_id, repository_id, endpoint_key, kind, action, payload, status, reason)
		VALUES ($1,$2,$3,$4,$5,$6::jsonb,$7,$8)
		ON CONFLICT (event_id, repository_id) DO NOTHING`,
		e.EventID, e.RepositoryID.String(), e.EndpointKey, e.Kind, e.Action, string(jsonOrEmpty(e.Payload)), string(status), e.Reason)
	if err != nil {
		return false, fmt.Errorf("git repo: insert inbox event: %w", err)
	}
	n, _ := res.RowsAffected()
	return n > 0, nil
}

// DeleteInboxEvent removes an inbox row.
func (s *GitRepository) DeleteInboxEvent(ctx context.Context, eventID string, repositoryID uuid.UUID) error {
	if _, err := s.db.ExecContext(ctx, `DELETE FROM git_event_inbox WHERE event_id = $1 AND repository_id = $2`, eventID, repositoryID.String()); err != nil {
		return fmt.Errorf("git repo: delete inbox event: %w", err)
	}
	return nil
}

// ── change requests and task links ───────────────────────────────────────────

type gitCRRecord struct {
	ID           string     `db:"id"`
	RepositoryID string     `db:"repository_id"`
	Number       string     `db:"number"`
	Title        string     `db:"title"`
	State        string     `db:"state"`
	Draft        bool       `db:"draft"`
	HeadRef      string     `db:"head_ref"`
	HeadSHA      string     `db:"head_sha"`
	BaseRef      string     `db:"base_ref"`
	Author       string     `db:"author"`
	HTMLURL      string     `db:"html_url"`
	MergedAt     *time.Time `db:"merged_at"`
	OpenedBy     *string    `db:"opened_by_agent_id"`
	OpenedConv   *string    `db:"opened_conversation_id"`
	CreatedAt    time.Time  `db:"created_at"`
	UpdatedAt    time.Time  `db:"updated_at"`
}

const gitCRCols = `id, repository_id, number, title, state, draft, head_ref, head_sha, base_ref, author, html_url, merged_at,
	opened_by_agent_id, opened_conversation_id, created_at, updated_at`

func (r gitCRRecord) domain() *gitdom.ChangeRequest {
	id, _ := uuid.Parse(r.ID)
	rid, _ := uuid.Parse(r.RepositoryID)
	return &gitdom.ChangeRequest{ID: id, RepositoryID: rid, Number: r.Number, Title: r.Title, State: r.State, Draft: r.Draft,
		HeadRef: r.HeadRef, HeadSHA: r.HeadSHA, BaseRef: r.BaseRef, Author: r.Author, URL: r.HTMLURL, MergedAt: r.MergedAt,
		OpenedByAgentID: parseUUIDPtr(r.OpenedBy), OpenedConversationID: parseUUIDPtr(r.OpenedConv),
		CreatedAt: r.CreatedAt, UpdatedAt: r.UpdatedAt}
}

// FindChangeRequest returns the cached change request.
func (s *GitRepository) FindChangeRequest(ctx context.Context, repositoryID uuid.UUID, number string) (*gitdom.ChangeRequest, error) {
	var rec gitCRRecord
	if err := s.db.GetContext(ctx, &rec, `SELECT `+gitCRCols+` FROM git_change_requests WHERE repository_id = $1 AND number = $2`,
		repositoryID.String(), number); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, gitdom.ErrNotFound
		}
		return nil, fmt.Errorf("git repo: find change request: %w", err)
	}
	return rec.domain(), nil
}

// UpsertChangeRequest creates or updates the cached change request. The agent
// that opened it is kept once known: a later webhook update never clears it.
func (s *GitRepository) UpsertChangeRequest(ctx context.Context, cr *gitdom.ChangeRequest) (*gitdom.ChangeRequest, error) {
	var rec gitCRRecord
	err := s.db.GetContext(ctx, &rec, `
		INSERT INTO git_change_requests (repository_id, number, title, state, draft, head_ref, head_sha, base_ref, author, html_url, merged_at,
			opened_by_agent_id, opened_conversation_id)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13)
		ON CONFLICT (repository_id, number) DO UPDATE SET
			title = CASE WHEN EXCLUDED.title = '' THEN git_change_requests.title ELSE EXCLUDED.title END,
			state = EXCLUDED.state, draft = EXCLUDED.draft,
			head_ref = CASE WHEN EXCLUDED.head_ref = '' THEN git_change_requests.head_ref ELSE EXCLUDED.head_ref END,
			head_sha = CASE WHEN EXCLUDED.head_sha = '' THEN git_change_requests.head_sha ELSE EXCLUDED.head_sha END,
			base_ref = EXCLUDED.base_ref, author = EXCLUDED.author,
			html_url = CASE WHEN EXCLUDED.html_url = '' THEN git_change_requests.html_url ELSE EXCLUDED.html_url END,
			merged_at = EXCLUDED.merged_at,
			opened_by_agent_id = COALESCE(EXCLUDED.opened_by_agent_id, git_change_requests.opened_by_agent_id),
			opened_conversation_id = COALESCE(EXCLUDED.opened_conversation_id, git_change_requests.opened_conversation_id),
			updated_at = NOW()
		RETURNING `+gitCRCols,
		cr.RepositoryID.String(), cr.Number, cr.Title, cr.State, cr.Draft, cr.HeadRef, cr.HeadSHA, cr.BaseRef, cr.Author, cr.URL, cr.MergedAt,
		uuidPtrString(cr.OpenedByAgentID), uuidPtrString(cr.OpenedConversationID))
	if err != nil {
		return nil, fmt.Errorf("git repo: upsert change request: %w", err)
	}
	return rec.domain(), nil
}

// LinkTaskToChangeRequest records that a change request belongs to a task.
func (s *GitRepository) LinkTaskToChangeRequest(ctx context.Context, taskID, changeRequestID uuid.UUID) error {
	_, err := s.db.ExecContext(ctx, `INSERT INTO git_task_links (task_id, change_request_id) VALUES ($1,$2) ON CONFLICT DO NOTHING`, taskID.String(), changeRequestID.String())
	if err != nil {
		return fmt.Errorf("git repo: link task: %w", err)
	}
	return nil
}

// LinkTaskBranch records that a branch belongs to a task.
func (s *GitRepository) LinkTaskBranch(ctx context.Context, taskID, repositoryID uuid.UUID, branch string) error {
	_, err := s.db.ExecContext(ctx, `INSERT INTO git_task_branches (task_id, repository_id, branch_name) VALUES ($1,$2,$3) ON CONFLICT DO NOTHING`, taskID.String(), repositoryID.String(), branch)
	if err != nil {
		return fmt.Errorf("git repo: link task branch: %w", err)
	}
	return nil
}

// FindTaskByRef resolves PREFIX-N within a project to a task.
func (s *GitRepository) FindTaskByRef(ctx context.Context, projectID uuid.UUID, prefix string, number int) (uuid.UUID, bool, error) {
	var id string
	err := s.db.GetContext(ctx, &id, `
		SELECT t.id FROM tasks t JOIN projects p ON p.id = t.project_id
		WHERE t.project_id = $1 AND upper(p.task_id_prefix) = upper($2) AND t.task_number = $3 AND t.deleted_at IS NULL`,
		projectID.String(), prefix, number)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return uuid.Nil, false, nil
		}
		return uuid.Nil, false, fmt.Errorf("git repo: find task by ref: %w", err)
	}
	u, err := uuid.Parse(id)
	if err != nil {
		return uuid.Nil, false, fmt.Errorf("git repo: find task by ref: bad id: %w", err)
	}
	return u, true, nil
}

// ── audit ────────────────────────────────────────────────────────────────────

// AppendActionLog records one audited action.
func (s *GitRepository) AppendActionLog(ctx context.Context, e *gitdom.ActionLogEntry) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO git_action_log (project_id, repository_id, conversation_id, actor_account_id, tool, subject, ok, detail)
		VALUES ($1::uuid,$2::uuid,$3::uuid,$4::uuid,$5,$6,$7,$8)`,
		optUUIDArg(e.ProjectID), optUUIDArg(e.RepositoryID), optUUIDArg(e.ConversationID), optUUIDArg(e.ActorAccountID),
		e.Tool, e.Subject, e.OK, e.Detail)
	if err != nil {
		return fmt.Errorf("git repo: append action log: %w", err)
	}
	return nil
}

// DeleteAgentIdentity removes an agent's identity mapping for a provider.
func (s *GitRepository) DeleteAgentIdentity(ctx context.Context, agentID uuid.UUID, providerID string) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM git_agent_identities WHERE agent_id = $1 AND provider_id = $2`, agentID.String(), providerID)
	if err != nil {
		return fmt.Errorf("git repo: delete agent identity: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return gitdom.ErrNotFound
	}
	return nil
}

// ── identity setup sessions ──────────────────────────────────────────────────

type gitSetupRecord struct {
	State       string     `db:"state"`
	ProviderID  string     `db:"provider_id"`
	Host        string     `db:"host"`
	Scope       string     `db:"scope"`
	AgentID     *string    `db:"agent_id"`
	ProjectID   *string    `db:"project_id"`
	EndpointKey string     `db:"endpoint_key"`
	ReturnURL   string     `db:"return_url"`
	CreatedBy   string     `db:"created_by"`
	ExpiresAt   time.Time  `db:"expires_at"`
	CompletedAt *time.Time `db:"completed_at"`
	CreatedAt   time.Time  `db:"created_at"`
}

// CreateSetupSession records a started setup flow.
func (s *GitRepository) CreateSetupSession(ctx context.Context, x *gitdom.SetupSession) error {
	err := s.db.GetContext(ctx, &x.CreatedAt, `
		INSERT INTO git_setup_sessions (state, provider_id, host, scope, agent_id, project_id, endpoint_key, return_url, created_by, expires_at)
		VALUES ($1,$2,$3,$4,$5::uuid,$6::uuid,$7,$8,$9,$10) RETURNING created_at`,
		x.State, x.ProviderID, x.Host, string(x.Scope), optUUIDArg(x.AgentID), optUUIDArg(x.ProjectID), x.EndpointKey, x.ReturnURL, x.CreatedBy.String(), x.ExpiresAt)
	if err != nil {
		return fmt.Errorf("git repo: create setup session: %w", err)
	}
	return nil
}

// FindSetupSession returns the session for a state.
func (s *GitRepository) FindSetupSession(ctx context.Context, state string) (*gitdom.SetupSession, error) {
	var r gitSetupRecord
	if err := s.db.GetContext(ctx, &r, `
		SELECT state, provider_id, host, scope, agent_id, project_id, endpoint_key, return_url, created_by, expires_at, completed_at, created_at
		FROM git_setup_sessions WHERE state = $1`, state); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, gitdom.ErrNotFound
		}
		return nil, fmt.Errorf("git repo: find setup session: %w", err)
	}
	created, err := uuid.Parse(r.CreatedBy)
	if err != nil {
		return nil, fmt.Errorf("git repo: setup session created_by: %w", err)
	}
	return &gitdom.SetupSession{
		State: r.State, ProviderID: r.ProviderID, Host: r.Host, Scope: gitdom.IdentityScope(r.Scope), AgentID: parseOptUUID(r.AgentID), ProjectID: parseOptUUID(r.ProjectID),
		EndpointKey: r.EndpointKey, ReturnURL: r.ReturnURL, CreatedBy: created, ExpiresAt: r.ExpiresAt, CompletedAt: r.CompletedAt, CreatedAt: r.CreatedAt,
	}, nil
}

// CompleteSetupSession marks a session finished.
func (s *GitRepository) CompleteSetupSession(ctx context.Context, state string) error {
	res, err := s.db.ExecContext(ctx, `UPDATE git_setup_sessions SET completed_at = COALESCE(completed_at, NOW()) WHERE state = $1`, state)
	if err != nil {
		return fmt.Errorf("git repo: complete setup session: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return gitdom.ErrNotFound
	}
	return nil
}

// DeleteWebhookEndpoint removes an endpoint (for example one whose setup failed).
func (s *GitRepository) DeleteWebhookEndpoint(ctx context.Context, key string) error {
	if _, err := s.db.ExecContext(ctx, `DELETE FROM git_webhook_endpoints WHERE key = $1`, key); err != nil {
		return fmt.Errorf("git repo: delete webhook endpoint: %w", err)
	}
	return nil
}

// ── access grant sessions ────────────────────────────────────────────────────

type gitAccessRecord struct {
	State       string     `db:"state"`
	ProviderID  string     `db:"provider_id"`
	IdentityID  string     `db:"identity_id"`
	ProjectID   string     `db:"project_id"`
	Owner       string     `db:"owner"`
	ReturnURL   string     `db:"return_url"`
	CreatedBy   string     `db:"created_by"`
	ExpiresAt   time.Time  `db:"expires_at"`
	CompletedAt *time.Time `db:"completed_at"`
	CreatedAt   time.Time  `db:"created_at"`
}

// CreateAccessSession records a started access grant.
func (s *GitRepository) CreateAccessSession(ctx context.Context, x *gitdom.AccessSession) error {
	err := s.db.GetContext(ctx, &x.CreatedAt, `
		INSERT INTO git_access_sessions (state, provider_id, identity_id, project_id, owner, return_url, created_by, expires_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8) RETURNING created_at`,
		x.State, x.ProviderID, x.IdentityID.String(), x.ProjectID.String(), x.Owner, x.ReturnURL, x.CreatedBy.String(), x.ExpiresAt)
	if err != nil {
		return fmt.Errorf("git repo: create access session: %w", err)
	}
	return nil
}

// FindAccessSession returns the session for a state.
func (s *GitRepository) FindAccessSession(ctx context.Context, state string) (*gitdom.AccessSession, error) {
	var r gitAccessRecord
	if err := s.db.GetContext(ctx, &r, `
		SELECT state, provider_id, identity_id, project_id, owner, return_url, created_by, expires_at, completed_at, created_at
		FROM git_access_sessions WHERE state = $1`, state); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, gitdom.ErrNotFound
		}
		return nil, fmt.Errorf("git repo: find access session: %w", err)
	}
	identity, err1 := uuid.Parse(r.IdentityID)
	project, err2 := uuid.Parse(r.ProjectID)
	created, err3 := uuid.Parse(r.CreatedBy)
	if err := errors.Join(err1, err2, err3); err != nil {
		return nil, fmt.Errorf("git repo: access session ids: %w", err)
	}
	return &gitdom.AccessSession{State: r.State, ProviderID: r.ProviderID, IdentityID: identity, ProjectID: project, Owner: r.Owner,
		ReturnURL: r.ReturnURL, CreatedBy: created, ExpiresAt: r.ExpiresAt, CompletedAt: r.CompletedAt, CreatedAt: r.CreatedAt}, nil
}

// CompleteAccessSession marks a session finished.
func (s *GitRepository) CompleteAccessSession(ctx context.Context, state string) error {
	res, err := s.db.ExecContext(ctx, `UPDATE git_access_sessions SET completed_at = COALESCE(completed_at, NOW()) WHERE state = $1`, state)
	if err != nil {
		return fmt.Errorf("git repo: complete access session: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return gitdom.ErrNotFound
	}
	return nil
}
