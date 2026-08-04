package plugindom

import (
	"fmt"
	"strconv"
	"strings"
)

// ParseSemver parses a strict "X.Y.Z" (or "vX.Y.Z") version string into its
// major, minor, and patch integer components. Pre-release identifiers (e.g.
// "1.0.0-beta.1") and build metadata (e.g. "1.0.0+001") are rejected with an
// error so that callers never silently treat different precedence levels as
// equal. This is the single source of truth for strict semver parsing across
// the plugin system — manifest-authored version fields (Version,
// MinCoreVersion) and marketplace-upgrade comparisons both rely on it.
func ParseSemver(v string) ([3]int, error) {
	trimmed := strings.TrimPrefix(v, "v")
	if strings.ContainsRune(trimmed, '+') {
		return [3]int{}, fmt.Errorf("version %q must not contain build metadata", v)
	}
	if strings.ContainsRune(trimmed, '-') {
		return [3]int{}, fmt.Errorf("version %q must not contain pre-release identifiers; only strict X.Y.Z versions are supported", v)
	}
	parts := strings.SplitN(trimmed, ".", 3)
	if len(parts) != 3 {
		return [3]int{}, fmt.Errorf("expected major.minor.patch, got %q", v)
	}
	var result [3]int
	for i, p := range parts {
		n, err := strconv.Atoi(p)
		if err != nil || n < 0 {
			return [3]int{}, fmt.Errorf("non-numeric version component %q in %q", p, v)
		}
		result[i] = n
	}
	return result, nil
}

// CompareSemver returns a positive integer when a > b, 0 when equal, and a
// negative integer when a < b. Only strict "X.Y.Z" (or "vX.Y.Z") versions are
// accepted; pre-release identifiers and build metadata cause an error.
func CompareSemver(a, b string) (int, error) {
	pa, err := ParseSemver(a)
	if err != nil {
		return 0, fmt.Errorf("invalid version %q: %w", a, err)
	}
	pb, err := ParseSemver(b)
	if err != nil {
		return 0, fmt.Errorf("invalid version %q: %w", b, err)
	}
	for i := range pa {
		if pa[i] != pb[i] {
			return pa[i] - pb[i], nil
		}
	}
	return 0, nil
}

// parseLenientHostVersion extracts the major.minor.patch core from the
// running build's version string (PACA_VERSION), tolerating a leading "v"
// and any pre-release/build suffix (e.g. "v0.9.4-evup.1"). Returns ok=false
// for a version with no numeric core to compare, notably the "dev" default
// used by unreleased/local builds.
func parseLenientHostVersion(v string) (core [3]int, ok bool) {
	trimmed := strings.TrimPrefix(strings.TrimSpace(v), "v")
	if i := strings.IndexAny(trimmed, "-+"); i >= 0 {
		trimmed = trimmed[:i]
	}
	if trimmed == "" {
		return core, false
	}
	for i, part := range strings.Split(trimmed, ".") {
		if i >= 3 {
			break
		}
		n, err := strconv.Atoi(part)
		if err != nil {
			return [3]int{}, false
		}
		core[i] = n
	}
	return core, true
}

// CheckMinCoreVersion reports whether hostVersion satisfies the manifest's
// declared MinCoreVersion, returning a descriptive error when it does not.
// A manifest with no MinCoreVersion always passes. A hostVersion that isn't a
// comparable release build (e.g. the "dev" default for local/unreleased
// builds) is treated as unconstrained rather than rejected, so local
// development never gets blocked by a check it can't meaningfully evaluate.
func (m PluginManifest) CheckMinCoreVersion(hostVersion string) error {
	if m.MinCoreVersion == "" {
		return nil
	}
	minCore, err := ParseSemver(m.MinCoreVersion)
	if err != nil {
		// Should already be caught by Validate(), but a manifest read back
		// from storage before this field existed shouldn't panic here.
		return fmt.Errorf("invalid minCoreVersion %q: %w", m.MinCoreVersion, err)
	}
	hostCore, ok := parseLenientHostVersion(hostVersion)
	if !ok {
		return nil
	}
	for i := 0; i < 3; i++ {
		if hostCore[i] != minCore[i] {
			if hostCore[i] < minCore[i] {
				return fmt.Errorf(
					"plugin %q requires Paca %s or later (running %s)",
					m.ID, m.MinCoreVersion, hostVersion,
				)
			}
			return nil
		}
	}
	return nil
}
