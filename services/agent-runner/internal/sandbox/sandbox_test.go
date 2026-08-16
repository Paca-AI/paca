package sandbox

import "testing"

// TestImageConfirmed_UnconfirmedRefReturnsFalse is a regression test for
// the efficiency finding in review: ensureImage called docker.ImageList
// (enumerating every image on the host) on every single sandbox start with
// no caching, even though the image ref is pinned for the process's
// lifetime. These tests exercise the confirmedImages cache directly — the
// surrounding Docker calls (ImageList/ImagePull) need a live daemon and are
// covered by test/e2e instead.
func TestImageConfirmed_UnconfirmedRefReturnsFalse(t *testing.T) {
	m := &Manager{confirmedImages: make(map[string]bool)}
	if m.imageConfirmed("ghcr.io/block/goose:latest") {
		t.Error("imageConfirmed = true for a ref never confirmed")
	}
}

func TestConfirmImage_MakesImageConfirmedReturnTrue(t *testing.T) {
	m := &Manager{confirmedImages: make(map[string]bool)}
	const ref = "ghcr.io/block/goose:latest"

	m.confirmImage(ref)

	if !m.imageConfirmed(ref) {
		t.Error("imageConfirmed = false immediately after confirmImage for the same ref")
	}
}

func TestConfirmImage_DoesNotConfirmADifferentRef(t *testing.T) {
	m := &Manager{confirmedImages: make(map[string]bool)}
	m.confirmImage("ghcr.io/block/goose:latest")

	if m.imageConfirmed("ghcr.io/block/goose:v2") {
		t.Error("imageConfirmed = true for a different ref that was never confirmed")
	}
}

func TestNewManager_StartsWithNoImagesConfirmed(t *testing.T) {
	m, err := NewManager(20000, 100)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	if m.imageConfirmed("ghcr.io/block/goose:latest") {
		t.Error("a freshly-constructed Manager should have no images confirmed yet")
	}
}
