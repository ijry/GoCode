package officewatch

import "testing"

func TestValidateWatchCap_GatesPortAndCap(t *testing.T) {
	defer Unregister("k1")
	Register("k1", 26001, "secret")

	if !ValidateWatchCap(26001, "secret") {
		t.Fatalf("exact cap should validate")
	}
	if ValidateWatchCap(26001, "wrong") {
		t.Fatalf("wrong cap must fail")
	}
	if ValidateWatchCap(26001, "") {
		t.Fatalf("empty cap must fail")
	}
	if ValidateWatchCap(26002, "secret") {
		t.Fatalf("unknown port (SSRF) must fail")
	}
	if !IsKnownWatchPort(26001) {
		t.Fatalf("registered port should be known")
	}
	if IsKnownWatchPort(26002) {
		t.Fatalf("unregistered port must not be known")
	}
}

func TestUnregisterRemovesGate(t *testing.T) {
	Register("k2", 26010, "c")
	Unregister("k2")
	if ValidateWatchCap(26010, "c") {
		t.Fatalf("unregistered watch must not validate")
	}
}

func TestSSELeaseAccounting(t *testing.T) {
	defer Unregister("k3")
	Register("k3", 26020, "c")

	AcquireSSELease(26020)
	AcquireSSELease(26020)
	if got := snapshotLeases(t, 26020); got != 2 {
		t.Fatalf("leases = %d, want 2", got)
	}
	ReleaseSSELease(26020)
	if got := snapshotLeases(t, 26020); got != 1 {
		t.Fatalf("leases = %d, want 1", got)
	}
	ReleaseSSELease(26020)
	ReleaseSSELease(26020) // saturating: must not go negative
	if got := snapshotLeases(t, 26020); got != 0 {
		t.Fatalf("leases = %d, want 0 (saturating)", got)
	}
}

func TestNoteProxyRequestMarksProxied(t *testing.T) {
	defer Unregister("k4")
	Register("k4", 26030, "c")
	NoteProxyRequest(26030)
	for _, e := range Snapshot() {
		if e.Port == 26030 {
			if !e.Proxied {
				t.Fatalf("expected proxied=true after NoteProxyRequest")
			}
			return
		}
	}
	t.Fatalf("entry not found")
}

func TestNewCapIsHex32AndUnique(t *testing.T) {
	a, b := NewCap(), NewCap()
	if len(a) != 32 {
		t.Fatalf("cap len = %d, want 32", len(a))
	}
	if a == b {
		t.Fatalf("two caps collided: %q", a)
	}
}

func snapshotLeases(t *testing.T, port int) int {
	t.Helper()
	for _, e := range Snapshot() {
		if e.Port == port {
			return e.SSELeases
		}
	}
	t.Fatalf("port %d not found in snapshot", port)
	return -1
}
