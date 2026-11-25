package handler

import "time"

// WithProjectClock replaces the clock GetWorkspaceTrend takes its day
// boundaries from, so tests can pin "today".
func WithProjectClock(now func() time.Time) ProjectHandlerOption {
	return func(h *ProjectHandler) { h.now = now }
}
