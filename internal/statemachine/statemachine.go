package statemachine

import (
	"fmt"
	"sync"

	"github.com/forgequeue/forgequeue/internal/domain"
)

// AllowedTransitions maps a current state to the valid destination states.
var AllowedTransitions = map[domain.JobStatus]map[domain.JobStatus]bool{
	domain.StatusQueued: {
		domain.StatusRunning:   true,
		domain.StatusCancelled: true,
	},
	domain.StatusRunning: {
		domain.StatusCompleted: true,
		domain.StatusFailed:    true,
		domain.StatusTimeout:   true,
		domain.StatusCancelled: true,
		// Re-queueing on transient infrastructure recovery
		domain.StatusQueued:    true,
	},
	domain.StatusCompleted: {},
	domain.StatusFailed:    {},
	domain.StatusTimeout:   {},
	domain.StatusCancelled: {},
}

// StateMachine enforces valid state transitions across the application.
type StateMachine struct {
	mu sync.RWMutex
}

// New creates a new StateMachine instance.
func New() *StateMachine {
	return &StateMachine{}
}

// CanTransition returns true if the transition from current to next is permitted.
func (sm *StateMachine) CanTransition(current, next domain.JobStatus) bool {
	sm.mu.RLock()
	defer sm.mu.RUnlock()

	targets, ok := AllowedTransitions[current]
	if !ok {
		return false
	}
	return targets[next]
}

// Transition validates whether a transition is allowed and returns an error if illegal.
func (sm *StateMachine) Transition(current, next domain.JobStatus) error {
	if !sm.CanTransition(current, next) {
		return fmt.Errorf("%w: cannot transition from %s to %s", domain.ErrInvalidStateChange, current, next)
	}
	return nil
}

// ValidateNext verifies that target state is reachable from current state.
func ValidateNext(current, next domain.JobStatus) error {
	targets, ok := AllowedTransitions[current]
	if !ok || !targets[next] {
		return fmt.Errorf("%w: invalid transition from %s to %s", domain.ErrInvalidStateChange, current, next)
	}
	return nil
}
