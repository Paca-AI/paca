package main

import (
	"errors"
	"fmt"
	"testing"

	"github.com/Paca-AI/agent-runner/internal/sandbox"
)

// TestClassifyReconcileResult_NilErrorMeansClaimRunning covers
// reconcileOneEnvironment's success path: StartEnvironmentByID returning
// no error (the environment was already fine, or was just self-healed)
// must lead to persisting it as running.
func TestClassifyReconcileResult_NilErrorMeansClaimRunning(t *testing.T) {
	if got := classifyReconcileResult(nil); got != reconcileOutcomeClaimRunning {
		t.Errorf("classifyReconcileResult(nil) = %v, want reconcileOutcomeClaimRunning", got)
	}
}

// TestClassifyReconcileResult_BareErrEnvironmentGoneMeansMarkError covers
// the genuinely-unrecoverable case: a bare sandbox.ErrEnvironmentGone (the
// container/Pod and its volume/PVC are both gone) must be classified as an
// actionable error, not left for a silent retry that can never succeed.
func TestClassifyReconcileResult_BareErrEnvironmentGoneMeansMarkError(t *testing.T) {
	if got := classifyReconcileResult(sandbox.ErrEnvironmentGone); got != reconcileOutcomeMarkError {
		t.Errorf("classifyReconcileResult(ErrEnvironmentGone) = %v, want reconcileOutcomeMarkError", got)
	}
}

// TestClassifyReconcileResult_WrappedErrEnvironmentGoneMeansMarkError
// covers the shape every real call site actually returns: both backends'
// recreate paths wrap ErrEnvironmentGone with %w and extra context (see
// k8s.Manager.recreateGoneEnvironmentDeployment and
// docker.Manager.recreateGoneEnvironmentContainer), never return it bare.
// errors.Is must still see through that wrapping.
func TestClassifyReconcileResult_WrappedErrEnvironmentGoneMeansMarkError(t *testing.T) {
	wrapped := fmt.Errorf("sandbox/k8s: environment deployment %s no longer exists, and its volume %s is gone too: %w",
		"env-1", "env-1", sandbox.ErrEnvironmentGone)
	if got := classifyReconcileResult(wrapped); got != reconcileOutcomeMarkError {
		t.Errorf("classifyReconcileResult(wrapped ErrEnvironmentGone) = %v, want reconcileOutcomeMarkError", got)
	}
}

// TestClassifyReconcileResult_UnrelatedErrorMeansLeaveUnchanged covers the
// "leave it alone for a future retry" branch: a transient failure (a
// Docker/Kubernetes API hiccup, a slow image pull, the reconcileItemTimeout
// deadline itself) must never be misclassified as the unrecoverable case —
// doing so would flip a perfectly healthy environment to a stuck "error"
// status a human would have to manually clear, for a boot-time hiccup that
// coldStartEnvironment's own next attach would have self-healed anyway.
func TestClassifyReconcileResult_UnrelatedErrorMeansLeaveUnchanged(t *testing.T) {
	for name, err := range map[string]error{
		"plain error":             errors.New("connection reset"),
		"context deadline":        fmt.Errorf("sandbox/docker: start environment container env-1: %w", errors.New("context deadline exceeded")),
		"wrapped unrelated error": fmt.Errorf("sandbox/k8s: start environment: %w", errors.New("etcdserver: request timed out")),
	} {
		t.Run(name, func(t *testing.T) {
			if got := classifyReconcileResult(err); got != reconcileOutcomeLeaveUnchanged {
				t.Errorf("classifyReconcileResult(%v) = %v, want reconcileOutcomeLeaveUnchanged", err, got)
			}
		})
	}
}
