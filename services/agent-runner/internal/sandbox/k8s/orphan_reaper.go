package k8s

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/Paca-AI/agent-runner/internal/sandbox"
)

var _ sandbox.AuthoritativeOrphanReaper = (*Manager)(nil)

// ReapAuthoritativeOrphans deletes authoritative Jobs whose exact turn/run
// lease is no longer active. Job deletion cascades to the primary sandbox Pod
// and its optional dind sidecar; legacy Jobs lack labelTurnID and are ignored.
func (m *Manager) ReapAuthoritativeOrphans(ctx context.Context, active sandbox.AuthoritativeRuntimeActive) (int, error) {
	selector := fmt.Sprintf("%s=%s,%s", labelManaged, managedValue, labelTurnID)
	jobs, err := m.clientset.BatchV1().Jobs(m.namespace).List(ctx, metav1.ListOptions{LabelSelector: selector})
	if err != nil {
		return 0, fmt.Errorf("sandbox/k8s: list authoritative jobs: %w", err)
	}

	var errs []error
	reaped := 0
	for _, job := range jobs.Items {
		turnID, turnErr := uuid.Parse(job.Labels[labelTurnID])
		runID, runErr := uuid.Parse(job.Labels[labelConvID])
		if turnErr != nil || runErr != nil {
			errs = append(errs, fmt.Errorf("sandbox/k8s: invalid authoritative labels turn=%q run=%q on job %s",
				job.Labels[labelTurnID], job.Labels[labelConvID], job.Name))
			continue
		}
		isActive, activeErr := active(ctx, turnID, runID)
		if activeErr != nil {
			errs = append(errs, activeErr)
			continue
		}
		if isActive {
			continue
		}
		if err := m.deleteJob(ctx, job.Name); err != nil {
			errs = append(errs, err)
			continue
		}
		reaped++
	}
	return reaped, errors.Join(errs...)
}
