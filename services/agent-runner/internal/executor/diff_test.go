package executor

import "testing"

// TestTurnBaselineOldText_FirstEditOfATurnFallsThroughToHEAD is a
// regression test for the diff-reconstruction bug flagged in review:
// gitDiffForPath's doc comment promises the diff represents content
// "before and after its most recent edit," but the implementation always
// diffed against static git HEAD — so a file edited twice in one turn
// showed a cumulative (not incremental) diff on the second edit. The first
// time a path is seen in a turn, there is no prior-this-turn content to
// diff against, so the caller must fall through to computing oldText from
// git HEAD instead.
func TestTurnBaselineOldText_FirstEditOfATurnFallsThroughToHEAD(t *testing.T) {
	turnBaseline := make(map[string]string)

	oldText, cached := turnBaselineOldText("/repo/main.go", "package main\n", turnBaseline)

	if cached {
		t.Fatalf("cached = true on the first edit of this turn, want false (caller should fall through to git HEAD)")
	}
	if oldText != nil {
		t.Errorf("oldText = %v, want nil when falling through to HEAD", oldText)
	}
	if _, ok := turnBaseline["/repo/main.go"]; ok {
		t.Error("turnBaseline should be left untouched on a cache miss — the caller sets it after resolving HEAD")
	}
}

// TestTurnBaselineOldText_SecondEditOfATurnDiffsFromTheFirstEditsResult is
// the core regression: once a path has a recorded baseline (set by the
// caller after the first edit's HEAD lookup), a second edit to the same
// path in the same turn must diff against that recorded content, not HEAD
// — otherwise the second diff card shows both edits combined.
func TestTurnBaselineOldText_SecondEditOfATurnDiffsFromTheFirstEditsResult(t *testing.T) {
	turnBaseline := map[string]string{
		"/repo/main.go": "package main\n",
	}

	oldText, cached := turnBaselineOldText("/repo/main.go", "package main\n\nfunc main() {}\n", turnBaseline)

	if !cached {
		t.Fatal("cached = false on a path already seen this turn, want true")
	}
	if oldText == nil || *oldText != "package main\n" {
		t.Errorf("oldText = %v, want the first edit's content (\"package main\\n\"), not HEAD's", oldText)
	}
}

// TestTurnBaselineOldText_AdvancesTheBaselineForAThirdEdit confirms the
// baseline keeps moving forward, not just updating once — a third edit in
// the same turn should diff against the second edit's result.
func TestTurnBaselineOldText_AdvancesTheBaselineForAThirdEdit(t *testing.T) {
	turnBaseline := make(map[string]string)

	// First edit: falls through to HEAD (simulated by the caller directly
	// seeding the baseline, as gitDiffForPath does after its own HEAD read).
	turnBaseline["/repo/main.go"] = "v1\n"

	// Second edit: diffs from v1, advances the baseline to v2.
	oldText, cached := turnBaselineOldText("/repo/main.go", "v2\n", turnBaseline)
	if !cached || oldText == nil || *oldText != "v1\n" {
		t.Fatalf("second edit: oldText = %v, cached = %v, want (\"v1\\n\", true)", oldText, cached)
	}

	// Third edit: must diff from v2 (the second edit's result), not v1 or HEAD.
	oldText, cached = turnBaselineOldText("/repo/main.go", "v3\n", turnBaseline)
	if !cached || oldText == nil || *oldText != "v2\n" {
		t.Fatalf("third edit: oldText = %v, cached = %v, want (\"v2\\n\", true) — must diff from the second edit's result, not the original HEAD baseline", oldText, cached)
	}
}

// TestTurnBaselineOldText_DifferentPathsDoNotShareABaseline confirms the
// map is genuinely keyed per path — editing one file must not affect the
// recorded baseline for a different file.
func TestTurnBaselineOldText_DifferentPathsDoNotShareABaseline(t *testing.T) {
	turnBaseline := map[string]string{
		"/repo/a.go": "a-v1\n",
	}

	// b.go has never been seen this turn, even though a.go has.
	_, cached := turnBaselineOldText("/repo/b.go", "b-v1\n", turnBaseline)
	if cached {
		t.Error("b.go should not be considered cached just because a.go is in turnBaseline")
	}

	// a.go's baseline must be unaffected by the b.go lookup above.
	oldText, cached := turnBaselineOldText("/repo/a.go", "a-v2\n", turnBaseline)
	if !cached || oldText == nil || *oldText != "a-v1\n" {
		t.Errorf("a.go: oldText = %v, cached = %v, want (\"a-v1\\n\", true)", oldText, cached)
	}
}
