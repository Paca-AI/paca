package sandbox

import (
	"context"

	"github.com/google/uuid"
)

// AuthoritativeRuntimeActive answers whether the exact turn/run pair still
// owns a live database lease. Errors are fail-safe: a backend must retain the
// resource when the control plane cannot prove it is orphaned.
type AuthoritativeRuntimeActive func(context.Context, uuid.UUID, uuid.UUID) (bool, error)

// AuthoritativeOrphanReaper is the lease-aware cleanup capability implemented
// by every built-in sandbox backend. It is separate from Backend so tests and
// third-party backends that only exercise ordinary lifecycle operations do not
// need to manufacture control-plane state.
type AuthoritativeOrphanReaper interface {
	ReapAuthoritativeOrphans(context.Context, AuthoritativeRuntimeActive) (int, error)
}
