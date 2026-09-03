package environmentsvc

import (
	"k8s.io/apimachinery/pkg/api/resource"

	environmentdom "github.com/Paca-AI/api/internal/domain/environment"
)

// minCPULimitMillicores/minMemoryLimitBytes are floors well above what
// Docker/Kubernetes would technically accept (Docker's own hard floor is a
// flat 6MB of memory, no minimum at all on CPU) — chosen instead as
// "plausibly enough to boot the agent-server image and not OOM/throttle
// immediately," which is the failure mode this validation exists to catch
// before it reaches the sandbox backend. See CreateEnvironment's call site.
const (
	minCPULimitMillicores = 100       // 100m == 0.1 vCPU
	minMemoryLimitBytes   = 256 << 20 // 256Mi
)

// validateCPULimit/validateMemoryLimit parse a caller-supplied cpu_limit/
// memory_limit override with the exact same resource.ParseQuantity both
// sandbox backends (services/agent-runner's docker and k8s packages) use to
// interpret these strings, so "valid here" means "valid there" — and reject
// anything that parses but is too small to actually run a container in,
// rather than letting it reach the backend and fail as a raw daemon error
// (see errors.go's ErrEnvironmentCPULimitInvalid doc comment for the
// motivating incident).
func validateCPULimit(raw string) error {
	q, err := resource.ParseQuantity(raw)
	if err != nil || q.MilliValue() < minCPULimitMillicores {
		return environmentdom.ErrEnvironmentCPULimitInvalid
	}
	return nil
}

func validateMemoryLimit(raw string) error {
	q, err := resource.ParseQuantity(raw)
	if err != nil || q.Value() < minMemoryLimitBytes {
		return environmentdom.ErrEnvironmentMemoryLimitInvalid
	}
	return nil
}
