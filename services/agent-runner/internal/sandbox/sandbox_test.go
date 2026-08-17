package sandbox

import (
	"net/netip"
	"testing"

	"github.com/moby/moby/api/types/network"
)

// TestSelectContainerIP_PrefersNamedNetworkOverOthers is a regression test
// for the containerIP coin-flip finding in review: a container attached to
// two networks (e.g. this process's own ownNetworkName plus a private
// per-conversation dind network — see Start's insideDocker branch) used to
// get an arbitrary one of their addresses via Go's randomized map
// iteration, which silently broke reachability whenever it picked the
// network this process itself isn't attached to. Constructs the multi-
// network map directly rather than a real container, so this exercises the
// selection rule on its own — no Docker daemon required.
func TestSelectContainerIP_PrefersNamedNetworkOverOthers(t *testing.T) {
	networks := map[string]*network.EndpointSettings{
		"own-network":      {IPAddress: netip.MustParseAddr("172.18.0.5")},
		"conversation-net": {IPAddress: netip.MustParseAddr("172.19.0.7")},
	}

	got, ok := selectContainerIP(networks, "own-network")
	if !ok {
		t.Fatal("selectContainerIP reported no IP found")
	}
	if got != "172.18.0.5" {
		t.Errorf("selectContainerIP with preferredNetwork=%q = %q, want the preferred network's address %q",
			"own-network", got, "172.18.0.5")
	}
}

func TestSelectContainerIP_FallsBackToAnyValidAddressWhenPreferredAbsent(t *testing.T) {
	networks := map[string]*network.EndpointSettings{
		"bridge": {IPAddress: netip.MustParseAddr("172.17.0.3")},
	}

	got, ok := selectContainerIP(networks, "own-network")
	if !ok {
		t.Fatal("selectContainerIP reported no IP found")
	}
	if got != "172.17.0.3" {
		t.Errorf("selectContainerIP with an absent preferred network = %q, want fallback %q", got, "172.17.0.3")
	}
}

func TestSelectContainerIP_EmptyPreferredNetworkAcceptsAnyValidAddress(t *testing.T) {
	networks := map[string]*network.EndpointSettings{
		"bridge": {IPAddress: netip.MustParseAddr("172.17.0.3")},
	}

	got, ok := selectContainerIP(networks, "")
	if !ok {
		t.Fatal("selectContainerIP reported no IP found")
	}
	if got != "172.17.0.3" {
		t.Errorf("selectContainerIP(\"\") = %q, want %q", got, "172.17.0.3")
	}
}

func TestSelectContainerIP_NoValidAddressReturnsFalse(t *testing.T) {
	networks := map[string]*network.EndpointSettings{
		"bridge": {}, // no IPAddress assigned yet
	}

	if _, ok := selectContainerIP(networks, ""); ok {
		t.Error("selectContainerIP should report not-found when no network has a valid address yet")
	}
}

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
