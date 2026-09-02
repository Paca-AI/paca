package postgres

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jmoiron/sqlx"

	annotationdom "github.com/Paca-AI/api/internal/domain/annotation"
)

// -------------------------------------------------------------------------
// sqlx record types
// -------------------------------------------------------------------------

type pageAnnotationRecord struct {
	ID                       string     `db:"id"`
	ProjectID                string     `db:"project_id"`
	EnvironmentID            string     `db:"environment_id"`
	PortForwardID            *string    `db:"port_forward_id"`
	PagePath                 string     `db:"page_path"`
	ElementSelector          string     `db:"element_selector"`
	ElementSelectorFallbacks []byte     `db:"element_selector_fallbacks"`
	BoundingBox              []byte     `db:"bounding_box"`
	ElementSnapshot          []byte     `db:"element_snapshot"`
	ConsoleErrors            []byte     `db:"console_errors"`
	FailedRequests           []byte     `db:"failed_requests"`
	ScreenshotFileID         *string    `db:"screenshot_file_id"`
	Body                     string     `db:"body"`
	Status                   string     `db:"status"`
	TaskID                   *string    `db:"task_id"`
	CreatedBy                string     `db:"created_by"`
	CreatedByName            string     `db:"created_by_name"`
	CreatedByUsername        string     `db:"created_by_username"`
	CreatedByAvatarKey       *string    `db:"created_by_avatar_key"`
	CreatedByAvatarThumbKey  *string    `db:"created_by_avatar_thumb_key"`
	ResolvedBy               *string    `db:"resolved_by"`
	ResolvedAt               *time.Time `db:"resolved_at"`
	CreatedAt                time.Time  `db:"created_at"`
	UpdatedAt                time.Time  `db:"updated_at"`
	DeletedAt                *time.Time `db:"deleted_at"`
}

type annotationCommentRecord struct {
	ID                      string    `db:"id"`
	AnnotationID            string    `db:"annotation_id"`
	Body                    string    `db:"body"`
	CreatedBy               string    `db:"created_by"`
	CreatedByName           string    `db:"created_by_name"`
	CreatedByUsername       string    `db:"created_by_username"`
	CreatedByAvatarKey      *string   `db:"created_by_avatar_key"`
	CreatedByAvatarThumbKey *string   `db:"created_by_avatar_thumb_key"`
	CreatedAt               time.Time `db:"created_at"`
	UpdatedAt               time.Time `db:"updated_at"`
}

// authorCols is appended to both queries below via a LEFT JOIN against
// users (LEFT, not JOIN, so a since-deleted user doesn't hide an
// otherwise-still-valid annotation/comment row — mirrors task.Activity's
// own defensive-against-deleted-actor convention, even though
// created_by's FK doesn't currently allow that deletion either way).
const authorCols = `u.full_name AS created_by_name, u.username AS created_by_username,
	u.avatar_key AS created_by_avatar_key, u.avatar_thumb_key AS created_by_avatar_thumb_key`

const pageAnnotationCols = `pa.id, pa.project_id, pa.environment_id, pa.port_forward_id, pa.page_path, pa.element_selector,
	pa.element_selector_fallbacks, pa.bounding_box, pa.element_snapshot, pa.console_errors, pa.failed_requests,
	pa.screenshot_file_id, pa.body, pa.status, pa.task_id, pa.created_by, pa.resolved_by, pa.resolved_at,
	pa.created_at, pa.updated_at, pa.deleted_at, ` + authorCols

const annotationCommentCols = `ac.id, ac.annotation_id, ac.body, ac.created_by, ac.created_at, ac.updated_at, ` + authorCols

// -------------------------------------------------------------------------
// Repository
// -------------------------------------------------------------------------

// AnnotationRepository is the sqlx implementation of annotationdom.Repository.
type AnnotationRepository struct {
	db *sqlx.DB
}

// NewAnnotationRepository returns a new AnnotationRepository.
func NewAnnotationRepository(db *sqlx.DB) *AnnotationRepository {
	return &AnnotationRepository{db: db}
}

func (r *AnnotationRepository) ListForPage(ctx context.Context, environmentID uuid.UUID, pagePath string) ([]*annotationdom.PageAnnotation, error) {
	var recs []pageAnnotationRecord
	err := r.db.SelectContext(ctx, &recs, `
		SELECT `+pageAnnotationCols+` FROM page_annotations pa
		LEFT JOIN users u ON u.id = pa.created_by
		WHERE pa.environment_id = $1 AND pa.page_path = $2 AND pa.deleted_at IS NULL
		ORDER BY pa.created_at ASC`, environmentID.String(), pagePath)
	if err != nil {
		return nil, fmt.Errorf("annotation repo: list for page: %w", err)
	}
	return r.hydrateAll(ctx, recs)
}

func (r *AnnotationRepository) ListForEnvironment(ctx context.Context, environmentID uuid.UUID) ([]*annotationdom.PageAnnotation, error) {
	var recs []pageAnnotationRecord
	err := r.db.SelectContext(ctx, &recs, `
		SELECT `+pageAnnotationCols+` FROM page_annotations pa
		LEFT JOIN users u ON u.id = pa.created_by
		WHERE pa.environment_id = $1 AND pa.deleted_at IS NULL
		ORDER BY pa.created_at DESC`, environmentID.String())
	if err != nil {
		return nil, fmt.Errorf("annotation repo: list for environment: %w", err)
	}
	return r.hydrateAll(ctx, recs)
}

func (r *AnnotationRepository) FindVisibleInProject(ctx context.Context, projectID, annotationID uuid.UUID) (*annotationdom.PageAnnotation, error) {
	var rec pageAnnotationRecord
	err := r.db.GetContext(ctx, &rec, `
		SELECT `+pageAnnotationCols+` FROM page_annotations pa
		LEFT JOIN users u ON u.id = pa.created_by
		WHERE pa.id = $1 AND pa.project_id = $2 AND pa.deleted_at IS NULL`, annotationID.String(), projectID.String())
	if err != nil {
		return nil, annotationdom.ErrAnnotationNotFound
	}
	a, err := annotationFromRecord(rec)
	if err != nil {
		return nil, err
	}
	comments, err := r.listComments(ctx, a.ID)
	if err != nil {
		return nil, err
	}
	a.Comments = comments
	return a, nil
}

func (r *AnnotationRepository) Create(ctx context.Context, a *annotationdom.PageAnnotation) error {
	fallbacks, err := json.Marshal(a.SelectorFallbacks)
	if err != nil {
		return fmt.Errorf("annotation repo: marshal selector fallbacks: %w", err)
	}
	bbox, err := json.Marshal(a.BoundingBox)
	if err != nil {
		return fmt.Errorf("annotation repo: marshal bounding box: %w", err)
	}
	snapshot, err := json.Marshal(a.ElementSnapshot)
	if err != nil {
		return fmt.Errorf("annotation repo: marshal element snapshot: %w", err)
	}
	consoleErrors, err := json.Marshal(a.ConsoleErrors)
	if err != nil {
		return fmt.Errorf("annotation repo: marshal console errors: %w", err)
	}
	failedRequests, err := json.Marshal(a.FailedRequests)
	if err != nil {
		return fmt.Errorf("annotation repo: marshal failed requests: %w", err)
	}

	_, err = r.db.ExecContext(ctx, `
		INSERT INTO page_annotations (
			id, project_id, environment_id, port_forward_id, page_path, element_selector,
			element_selector_fallbacks, bounding_box, element_snapshot, console_errors, failed_requests,
			screenshot_file_id, body, status, created_by, created_at, updated_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17)`,
		a.ID.String(), a.ProjectID.String(), a.EnvironmentID.String(), uuidPtrToStringPtr(a.PortForwardID),
		a.PagePath, a.ElementSelector, fallbacks, bbox, snapshot, consoleErrors, failedRequests,
		uuidPtrToStringPtr(a.ScreenshotFileID), a.Body, a.Status, a.CreatedBy.String(), a.CreatedAt, a.UpdatedAt,
	)
	if err != nil {
		return fmt.Errorf("annotation repo: create: %w", err)
	}
	return nil
}

func (r *AnnotationRepository) SetScreenshotFileID(ctx context.Context, id, fileID uuid.UUID) error {
	result, err := r.db.ExecContext(ctx, `
		UPDATE page_annotations SET screenshot_file_id = $1, updated_at = $2
		WHERE id = $3 AND deleted_at IS NULL`,
		fileID.String(), time.Now(), id.String(),
	)
	if err != nil {
		return fmt.Errorf("annotation repo: set screenshot file id: %w", err)
	}
	n, _ := result.RowsAffected()
	if n == 0 {
		return annotationdom.ErrAnnotationNotFound
	}
	return nil
}

func (r *AnnotationRepository) SetStatus(ctx context.Context, id uuid.UUID, status string, resolvedBy *uuid.UUID, resolvedAt *time.Time) error {
	result, err := r.db.ExecContext(ctx, `
		UPDATE page_annotations SET status = $1, resolved_by = $2, resolved_at = $3, updated_at = $4
		WHERE id = $5 AND deleted_at IS NULL`,
		status, uuidPtrToStringPtr(resolvedBy), resolvedAt, time.Now(), id.String(),
	)
	if err != nil {
		return fmt.Errorf("annotation repo: set status: %w", err)
	}
	n, _ := result.RowsAffected()
	if n == 0 {
		return annotationdom.ErrAnnotationNotFound
	}
	return nil
}

func (r *AnnotationRepository) SetTaskID(ctx context.Context, id, taskID uuid.UUID) error {
	result, err := r.db.ExecContext(ctx, `
		UPDATE page_annotations SET task_id = $1, updated_at = $2 WHERE id = $3 AND deleted_at IS NULL`,
		taskID.String(), time.Now(), id.String(),
	)
	if err != nil {
		return fmt.Errorf("annotation repo: set task id: %w", err)
	}
	n, _ := result.RowsAffected()
	if n == 0 {
		return annotationdom.ErrAnnotationNotFound
	}
	return nil
}

func (r *AnnotationRepository) AddComment(ctx context.Context, c *annotationdom.AnnotationComment) error {
	_, err := r.db.ExecContext(ctx, `
		INSERT INTO page_annotation_comments (id, annotation_id, body, created_by, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6)`,
		c.ID.String(), c.AnnotationID.String(), c.Body, c.CreatedBy.String(), c.CreatedAt, c.UpdatedAt,
	)
	if err != nil {
		return fmt.Errorf("annotation repo: add comment: %w", err)
	}
	return nil
}

func (r *AnnotationRepository) CreatePendingScreenshotFile(ctx context.Context, fileID, uploadedBy uuid.UUID, storageKey, bucket, fileName, contentType string, fileSize int64) error {
	now := time.Now()
	_, err := r.db.ExecContext(ctx, `
		INSERT INTO files (id, storage_key, bucket, file_name, content_type, file_size, upload_status, uploaded_by, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, 'pending', $7, $8, $9)`,
		fileID.String(), storageKey, bucket, fileName, contentType, fileSize, uploadedBy.String(), now, now,
	)
	if err != nil {
		return fmt.Errorf("annotation repo: create pending screenshot file: %w", err)
	}
	return nil
}

func (r *AnnotationRepository) MarkScreenshotFileUploaded(ctx context.Context, fileID uuid.UUID) error {
	_, err := r.db.ExecContext(ctx, `
		UPDATE files SET upload_status = 'uploaded', updated_at = $1 WHERE id = $2 AND upload_status = 'pending'`,
		time.Now(), fileID.String(),
	)
	if err != nil {
		return fmt.Errorf("annotation repo: mark screenshot file uploaded: %w", err)
	}
	return nil
}

// ResolvePortForward looks up which project/environment/port-forward
// currently owns hostPort, scoped to projects userID is a member of (see
// annotationdom.Repository.ResolvePortForward's doc comment for why —
// this must never confirm or deny a forward's existence in a project the
// caller isn't a member of).
func (r *AnnotationRepository) ResolvePortForward(ctx context.Context, userID uuid.UUID, hostPort int) (*annotationdom.PortForwardMatch, error) {
	var row struct {
		ProjectID     string `db:"project_id"`
		EnvironmentID string `db:"environment_id"`
		PortForwardID string `db:"port_forward_id"`
		Label         string `db:"label"`
	}
	err := r.db.GetContext(ctx, &row, `
		SELECT e.project_id AS project_id, epf.environment_id AS environment_id, epf.id AS port_forward_id, epf.label AS label
		FROM environment_port_forwards epf
		JOIN environments e ON e.id = epf.environment_id AND e.deleted_at IS NULL
		JOIN project_members pm ON pm.project_id = e.project_id AND pm.user_id = $1 AND pm.deleted_at IS NULL
		WHERE epf.host_port = $2
		LIMIT 1`, userID.String(), hostPort)
	if err != nil {
		return nil, annotationdom.ErrPortForwardNotFound
	}
	projectID, err := uuid.Parse(row.ProjectID)
	if err != nil {
		return nil, fmt.Errorf("annotation repo: parse project id: %w", err)
	}
	environmentID, err := uuid.Parse(row.EnvironmentID)
	if err != nil {
		return nil, fmt.Errorf("annotation repo: parse environment id: %w", err)
	}
	portForwardID, err := uuid.Parse(row.PortForwardID)
	if err != nil {
		return nil, fmt.Errorf("annotation repo: parse port forward id: %w", err)
	}
	return &annotationdom.PortForwardMatch{
		ProjectID:     projectID,
		EnvironmentID: environmentID,
		PortForwardID: portForwardID,
		Label:         row.Label,
	}, nil
}

// -------------------------------------------------------------------------
// mapping helpers
// -------------------------------------------------------------------------

func (r *AnnotationRepository) hydrateAll(ctx context.Context, recs []pageAnnotationRecord) ([]*annotationdom.PageAnnotation, error) {
	result := make([]*annotationdom.PageAnnotation, 0, len(recs))
	for _, rec := range recs {
		a, err := annotationFromRecord(rec)
		if err != nil {
			return nil, err
		}
		comments, err := r.listComments(ctx, a.ID)
		if err != nil {
			return nil, err
		}
		a.Comments = comments
		result = append(result, a)
	}
	return result, nil
}

func (r *AnnotationRepository) listComments(ctx context.Context, annotationID uuid.UUID) ([]*annotationdom.AnnotationComment, error) {
	var recs []annotationCommentRecord
	err := r.db.SelectContext(ctx, &recs, `
		SELECT `+annotationCommentCols+` FROM page_annotation_comments ac
		LEFT JOIN users u ON u.id = ac.created_by
		WHERE ac.annotation_id = $1 AND ac.deleted_at IS NULL
		ORDER BY ac.created_at ASC`, annotationID.String())
	if err != nil {
		return nil, fmt.Errorf("annotation repo: list comments: %w", err)
	}
	comments := make([]*annotationdom.AnnotationComment, 0, len(recs))
	for _, rec := range recs {
		c, err := commentFromRecord(rec)
		if err != nil {
			return nil, err
		}
		comments = append(comments, c)
	}
	return comments, nil
}

func annotationFromRecord(rec pageAnnotationRecord) (*annotationdom.PageAnnotation, error) {
	id, err := uuid.Parse(rec.ID)
	if err != nil {
		return nil, fmt.Errorf("annotation repo: parse id: %w", err)
	}
	projectID, err := uuid.Parse(rec.ProjectID)
	if err != nil {
		return nil, fmt.Errorf("annotation repo: parse project id: %w", err)
	}
	environmentID, err := uuid.Parse(rec.EnvironmentID)
	if err != nil {
		return nil, fmt.Errorf("annotation repo: parse environment id: %w", err)
	}
	createdBy, err := uuid.Parse(rec.CreatedBy)
	if err != nil {
		return nil, fmt.Errorf("annotation repo: parse created_by: %w", err)
	}

	var fallbacks []string
	if err := json.Unmarshal(rec.ElementSelectorFallbacks, &fallbacks); err != nil {
		return nil, fmt.Errorf("annotation repo: unmarshal selector fallbacks: %w", err)
	}
	var bbox annotationdom.BoundingBox
	if err := json.Unmarshal(rec.BoundingBox, &bbox); err != nil {
		return nil, fmt.Errorf("annotation repo: unmarshal bounding box: %w", err)
	}
	var snapshot annotationdom.ElementSnapshot
	if err := json.Unmarshal(rec.ElementSnapshot, &snapshot); err != nil {
		return nil, fmt.Errorf("annotation repo: unmarshal element snapshot: %w", err)
	}
	var consoleErrors []annotationdom.ConsoleEntry
	if err := json.Unmarshal(rec.ConsoleErrors, &consoleErrors); err != nil {
		return nil, fmt.Errorf("annotation repo: unmarshal console errors: %w", err)
	}
	var failedRequests []annotationdom.FailedRequest
	if err := json.Unmarshal(rec.FailedRequests, &failedRequests); err != nil {
		return nil, fmt.Errorf("annotation repo: unmarshal failed requests: %w", err)
	}

	portForwardID := stringPtrToUUIDPtr(rec.PortForwardID)
	screenshotFileID := stringPtrToUUIDPtr(rec.ScreenshotFileID)
	taskID := stringPtrToUUIDPtr(rec.TaskID)
	resolvedBy := stringPtrToUUIDPtr(rec.ResolvedBy)

	return &annotationdom.PageAnnotation{
		ID:                id,
		ProjectID:         projectID,
		EnvironmentID:     environmentID,
		PortForwardID:     portForwardID,
		PagePath:          rec.PagePath,
		ElementSelector:   rec.ElementSelector,
		SelectorFallbacks: fallbacks,
		BoundingBox:       bbox,
		ElementSnapshot:   snapshot,
		ConsoleErrors:     consoleErrors,
		FailedRequests:    failedRequests,
		ScreenshotFileID:  screenshotFileID,
		Body:              rec.Body,
		Status:            rec.Status,
		TaskID:            taskID,
		CreatedBy:         createdBy,
		CreatedByAuthor: annotationdom.Author{
			Name:           rec.CreatedByName,
			Username:       rec.CreatedByUsername,
			AvatarKey:      rec.CreatedByAvatarKey,
			AvatarThumbKey: rec.CreatedByAvatarThumbKey,
		},
		ResolvedBy: resolvedBy,
		ResolvedAt: rec.ResolvedAt,
		CreatedAt:  rec.CreatedAt,
		UpdatedAt:  rec.UpdatedAt,
		DeletedAt:  rec.DeletedAt,
	}, nil
}

func commentFromRecord(rec annotationCommentRecord) (*annotationdom.AnnotationComment, error) {
	id, err := uuid.Parse(rec.ID)
	if err != nil {
		return nil, fmt.Errorf("annotation repo: parse comment id: %w", err)
	}
	annotationID, err := uuid.Parse(rec.AnnotationID)
	if err != nil {
		return nil, fmt.Errorf("annotation repo: parse comment annotation id: %w", err)
	}
	createdBy, err := uuid.Parse(rec.CreatedBy)
	if err != nil {
		return nil, fmt.Errorf("annotation repo: parse comment created_by: %w", err)
	}
	return &annotationdom.AnnotationComment{
		ID:           id,
		AnnotationID: annotationID,
		Body:         rec.Body,
		CreatedBy:    createdBy,
		CreatedByAuthor: annotationdom.Author{
			Name:           rec.CreatedByName,
			Username:       rec.CreatedByUsername,
			AvatarKey:      rec.CreatedByAvatarKey,
			AvatarThumbKey: rec.CreatedByAvatarThumbKey,
		},
		CreatedAt: rec.CreatedAt,
		UpdatedAt: rec.UpdatedAt,
	}, nil
}
