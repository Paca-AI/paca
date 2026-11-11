package bootstrap

import (
	"context"
	"errors"
	"log/slog"

	"github.com/google/uuid"
	"github.com/jmoiron/sqlx"

	"github.com/Paca-AI/api/internal/domain/changeoutbox"
	docdom "github.com/Paca-AI/api/internal/domain/doc"
	sprintdom "github.com/Paca-AI/api/internal/domain/sprint"
	taskdom "github.com/Paca-AI/api/internal/domain/task"
	"github.com/Paca-AI/api/internal/platform/messaging"
	pgRepo "github.com/Paca-AI/api/internal/repository/postgres"
	sprintsvc "github.com/Paca-AI/api/internal/service/sprint"
	"github.com/Paca-AI/api/internal/transport/http/dto"
	"github.com/Paca-AI/api/internal/worker"
)

// newChangeReconciler wires the reconciler that announces database changes
// whose realtime announcement was lost (see worker.ChangeReconciler). Without a
// publisher (no Valkey) there is nothing to announce on, so it returns nil.
func newChangeReconciler(db *sqlx.DB, publisher *messaging.Publisher, tasks *pgRepo.TaskRepository, sprints *pgRepo.SprintRepository, docs *pgRepo.DocumentRepository, log *slog.Logger) *worker.ChangeReconciler {
	if publisher == nil {
		return nil
	}
	loaders := map[string]worker.ChangeLoader{
		changeoutbox.EntityTask: func(ctx context.Context, id uuid.UUID) (worker.ChangeRepair, error) {
			t, err := tasks.FindTaskByID(ctx, id)
			if errors.Is(err, taskdom.ErrTaskNotFound) {
				return worker.ChangeRepair{}, nil
			}
			if err != nil {
				return worker.ChangeRepair{}, err
			}
			return worker.ChangeRepair{Found: true, Snapshot: dto.TaskSnapshot(t)}, nil
		},
		changeoutbox.EntitySprint: func(ctx context.Context, id uuid.UUID) (worker.ChangeRepair, error) {
			sp, err := sprints.FindSprintByID(ctx, id)
			if errors.Is(err, sprintdom.ErrSprintNotFound) {
				return worker.ChangeRepair{}, nil
			}
			if err != nil {
				return worker.ChangeRepair{}, err
			}
			return worker.ChangeRepair{Found: true, Snapshot: sprintsvc.SprintSnapshot(sp)}, nil
		},
		changeoutbox.EntityDoc: func(ctx context.Context, id uuid.UUID) (worker.ChangeRepair, error) {
			// Only existence matters: a document's body is not attached to a change.
			if _, err := docs.FindDocumentTitleByID(ctx, id); err != nil {
				if errors.Is(err, docdom.ErrDocNotFound) {
					return worker.ChangeRepair{}, nil
				}
				return worker.ChangeRepair{}, err
			}
			return worker.ChangeRepair{Found: true}, nil
		},
	}
	return worker.NewChangeReconciler(pgRepo.NewChangeOutboxRepository(db), publisher, publisher, loaders, log)
}
