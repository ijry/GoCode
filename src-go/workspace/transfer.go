// Package workspace ports upstream src-tauri/src/workspace_transfer.rs: the
// per-process registry of in-flight workspace transfers and their cancellation
// tokens.
//
// Only the cancellation half is ported here (the `/backup_cancel` route). The
// download-ticket store — the other half of the upstream manager — lives in the
// web handlers (workspace_download_ticket.go), where the router already owns a
// per-process ticket store; consolidating it here is a separate refactor.
package workspace

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"sync"
)

// TransferManager tracks in-flight workspace transfers so a client can cancel
// one by id. Mirrors the `cancels` map of upstream WorkspaceTransferManager.
type TransferManager struct {
	mu      sync.Mutex
	cancels map[string]context.CancelFunc
}

// NewTransferManager returns an empty manager.
func NewTransferManager() *TransferManager {
	return &TransferManager{cancels: map[string]context.CancelFunc{}}
}

// RegisterTransfer mints a transfer id (32-char simple UUID, matching upstream)
// and a context the transfer's work should watch for cancellation. The caller
// must call FinishTransfer(id) when the work completes — or Cancel(id), which
// also removes the entry.
func (m *TransferManager) RegisterTransfer() (string, context.Context) {
	ctx, cancel := context.WithCancel(context.Background())
	id := newTransferID()
	m.mu.Lock()
	m.cancels[id] = cancel
	m.mu.Unlock()
	return id, ctx
}

// FinishTransfer drops a completed transfer's token without cancelling it.
func (m *TransferManager) FinishTransfer(id string) {
	m.mu.Lock()
	delete(m.cancels, id)
	m.mu.Unlock()
}

// Cancel cancels an in-flight transfer, returning true when one with this id
// existed. Mirrors upstream cancel: remove-then-cancel, so a second cancel of
// the same id returns false.
func (m *TransferManager) Cancel(id string) bool {
	m.mu.Lock()
	cancel, ok := m.cancels[id]
	delete(m.cancels, id)
	m.mu.Unlock()
	if !ok {
		return false
	}
	cancel()
	return true
}

// InFlight reports how many transfers are currently registered (test/status).
func (m *TransferManager) InFlight() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.cancels)
}

// newTransferID returns a 32-char lowercase hex id (upstream uses
// `Uuid::new_v4().simple().to_string()`).
func newTransferID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return ""
	}
	return hex.EncodeToString(b[:])
}
