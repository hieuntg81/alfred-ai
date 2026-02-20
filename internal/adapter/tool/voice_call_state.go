package tool

import (
	"fmt"
	"sync"
	"time"

	"alfred-ai/internal/domain"
)

// CallPersistence is the interface for persisting call records.
type CallPersistence interface {
	// Append persists a call record (called on every state change).
	Append(record CallRecord) error
	// Load loads all persisted call records.
	Load() ([]CallRecord, error)
}

// transcriptWaiter represents a goroutine waiting for a transcript update.
type transcriptWaiter struct {
	ch      chan struct{}
	afterID int // wait for transcript entries after this index
}

// CallStore manages in-memory call state with a thread-safe state machine.
type CallStore struct {
	mu            sync.RWMutex
	calls         map[string]*CallRecord
	maxConcurrent int
	persistence   CallPersistence // optional, may be nil

	waiterMu sync.Mutex
	waiters  map[string][]transcriptWaiter // callID → waiters
}

// NewCallStore creates a new in-memory call store.
// If persistence is non-nil, records are loaded on creation and
// appended on every state change.
func NewCallStore(maxConcurrent int, persistence CallPersistence) *CallStore {
	if maxConcurrent <= 0 {
		maxConcurrent = 1
	}
	cs := &CallStore{
		calls:         make(map[string]*CallRecord),
		maxConcurrent: maxConcurrent,
		persistence:   persistence,
		waiters:       make(map[string][]transcriptWaiter),
	}

	// Load persisted records (if available).
	if persistence != nil {
		records, err := persistence.Load()
		if err == nil {
			for i := range records {
				cs.calls[records[i].ID] = &records[i]
			}
		}
	}

	return cs
}

// Create creates a new call record and stores it.
// Returns ErrVoiceCallMaxConcurrent if the limit is reached.
func (cs *CallStore) Create(record CallRecord) error {
	cs.mu.Lock()
	defer cs.mu.Unlock()

	// Count active (non-terminal) calls.
	active := 0
	for _, c := range cs.calls {
		if !c.State.IsTerminal() {
			active++
		}
	}
	if active >= cs.maxConcurrent {
		return domain.NewSubSystemError("voicecall", "CallStore.Create", domain.ErrLimitReached,
			fmt.Sprintf("%d/%d concurrent calls", active, cs.maxConcurrent))
	}

	now := time.Now().UTC()
	record.CreatedAt = now
	record.UpdatedAt = now
	cs.calls[record.ID] = &record

	if cs.persistence != nil {
		_ = cs.persistence.Append(record)
	}

	return nil
}

// Get retrieves a call record by ID.
func (cs *CallStore) Get(callID string) (*CallRecord, error) {
	cs.mu.RLock()
	defer cs.mu.RUnlock()

	call, ok := cs.calls[callID]
	if !ok {
		return nil, domain.ErrNotFound
	}
	// Return a copy to prevent mutation.
	cp := *call
	cp.Transcript = make([]TurnEntry, len(call.Transcript))
	copy(cp.Transcript, call.Transcript)
	return &cp, nil
}

// Transition moves a call to a new state.
// Returns an error if the transition is invalid.
func (cs *CallStore) Transition(callID string, newState CallState, detail string) error {
	cs.mu.Lock()
	defer cs.mu.Unlock()

	call, ok := cs.calls[callID]
	if !ok {
		return domain.ErrNotFound
	}

	if !call.State.CanTransitionTo(newState) {
		return fmt.Errorf("%w: cannot transition from %s to %s",
			domain.ErrInvalidInput, call.State, newState)
	}

	call.State = newState
	call.UpdatedAt = time.Now().UTC()
	if detail != "" {
		call.ErrorDetail = detail
	}

	if newState.IsTerminal() {
		now := call.UpdatedAt
		call.EndedAt = &now
		call.Duration = int(now.Sub(call.CreatedAt).Milliseconds())
	}

	if cs.persistence != nil {
		_ = cs.persistence.Append(*call)
	}

	return nil
}

// SetProviderCallID sets the provider's call ID after initiation.
func (cs *CallStore) SetProviderCallID(callID, providerCallID string) error {
	cs.mu.Lock()
	defer cs.mu.Unlock()

	call, ok := cs.calls[callID]
	if !ok {
		return domain.ErrNotFound
	}
	call.ProviderCallID = providerCallID
	call.UpdatedAt = time.Now().UTC()

	if cs.persistence != nil {
		_ = cs.persistence.Append(*call)
	}
	return nil
}

// AppendTranscript adds a transcript entry to a call and notifies waiters.
func (cs *CallStore) AppendTranscript(callID string, entry TurnEntry) error {
	cs.mu.Lock()
	call, ok := cs.calls[callID]
	if !ok {
		cs.mu.Unlock()
		return domain.ErrNotFound
	}

	if entry.Timestamp.IsZero() {
		entry.Timestamp = time.Now().UTC()
	}
	call.Transcript = append(call.Transcript, entry)
	call.UpdatedAt = time.Now().UTC()
	transcriptLen := len(call.Transcript)

	if cs.persistence != nil {
		_ = cs.persistence.Append(*call)
	}
	cs.mu.Unlock()

	// Notify waiters.
	cs.waiterMu.Lock()
	defer cs.waiterMu.Unlock()

	waiters := cs.waiters[callID]
	var remaining []transcriptWaiter
	for _, w := range waiters {
		if transcriptLen > w.afterID {
			close(w.ch)
		} else {
			remaining = append(remaining, w)
		}
	}
	cs.waiters[callID] = remaining
	return nil
}

// WaitForTranscript blocks until a new transcript entry is added after afterIndex,
// or until the timeout expires. Returns the latest transcript entries.
func (cs *CallStore) WaitForTranscript(callID string, afterIndex int, timeout time.Duration) ([]TurnEntry, error) {
	cs.mu.RLock()
	call, ok := cs.calls[callID]
	if !ok {
		cs.mu.RUnlock()
		return nil, domain.ErrNotFound
	}

	// If there are already new entries, return immediately.
	if len(call.Transcript) > afterIndex {
		entries := make([]TurnEntry, len(call.Transcript)-afterIndex)
		copy(entries, call.Transcript[afterIndex:])
		cs.mu.RUnlock()
		return entries, nil
	}

	// Check if call ended.
	if call.State.IsTerminal() {
		cs.mu.RUnlock()
		return nil, domain.ErrInvalidInput
	}
	cs.mu.RUnlock()

	// Register waiter.
	ch := make(chan struct{})
	w := transcriptWaiter{ch: ch, afterID: afterIndex}

	cs.waiterMu.Lock()
	cs.waiters[callID] = append(cs.waiters[callID], w)
	cs.waiterMu.Unlock()

	// Wait for notification or timeout.
	timer := time.NewTimer(timeout)
	defer timer.Stop()

	select {
	case <-ch:
		// New transcript entry available.
		cs.mu.RLock()
		defer cs.mu.RUnlock()
		call, ok := cs.calls[callID]
		if !ok {
			return nil, domain.ErrNotFound
		}
		if len(call.Transcript) > afterIndex {
			entries := make([]TurnEntry, len(call.Transcript)-afterIndex)
			copy(entries, call.Transcript[afterIndex:])
			return entries, nil
		}
		return nil, nil

	case <-timer.C:
		// Clean up waiter.
		cs.waiterMu.Lock()
		waiters := cs.waiters[callID]
		for i, ww := range waiters {
			if ww.ch == ch {
				cs.waiters[callID] = append(waiters[:i], waiters[i+1:]...)
				break
			}
		}
		cs.waiterMu.Unlock()
		return nil, nil
	}
}

// ActiveCalls returns all non-terminal call records.
func (cs *CallStore) ActiveCalls() []CallRecord {
	cs.mu.RLock()
	defer cs.mu.RUnlock()

	var active []CallRecord
	for _, c := range cs.calls {
		if !c.State.IsTerminal() {
			cp := *c
			active = append(active, cp)
		}
	}
	return active
}

// FindByProviderID looks up a call by the provider's call ID.
func (cs *CallStore) FindByProviderID(providerCallID string) (*CallRecord, error) {
	cs.mu.RLock()
	defer cs.mu.RUnlock()

	for _, c := range cs.calls {
		if c.ProviderCallID == providerCallID {
			cp := *c
			return &cp, nil
		}
	}
	return nil, domain.ErrNotFound
}
