package workspace

import "testing"

func TestRegisterCancelFinish(t *testing.T) {
	m := NewTransferManager()

	id, ctx := m.RegisterTransfer()
	if len(id) != 32 {
		t.Fatalf("id = %q, want 32 hex chars", id)
	}
	if m.InFlight() != 1 {
		t.Fatalf("in-flight = %d, want 1", m.InFlight())
	}
	if ctx.Err() != nil {
		t.Fatalf("fresh context should be live")
	}

	// First cancel wins and cancels the context.
	if !m.Cancel(id) {
		t.Fatalf("first cancel should return true")
	}
	if ctx.Err() == nil {
		t.Fatalf("cancel must cancel the context")
	}
	// Second cancel of the same id finds nothing (remove-then-cancel).
	if m.Cancel(id) {
		t.Fatalf("second cancel should return false")
	}
	if m.InFlight() != 0 {
		t.Fatalf("in-flight = %d, want 0", m.InFlight())
	}

	// Finish removes the token WITHOUT cancelling.
	id2, ctx2 := m.RegisterTransfer()
	m.FinishTransfer(id2)
	if ctx2.Err() != nil {
		t.Fatalf("finish must not cancel the context")
	}
	if m.Cancel(id2) {
		t.Fatalf("cancel after finish should return false")
	}
}

func TestCancelUnknownID(t *testing.T) {
	m := NewTransferManager()
	if m.Cancel("does-not-exist") {
		t.Fatalf("unknown id should return false")
	}
}
