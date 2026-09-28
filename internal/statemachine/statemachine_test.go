package statemachine_test

import (
	"testing"

	"github.com/forgequeue/forgequeue/internal/domain"
	"github.com/forgequeue/forgequeue/internal/statemachine"
)

func TestStateMachine_ValidTransitions(t *testing.T) {
	sm := statemachine.New()

	validCases := []struct {
		from domain.JobStatus
		to   domain.JobStatus
	}{
		{domain.StatusQueued, domain.StatusRunning},
		{domain.StatusQueued, domain.StatusCancelled},
		{domain.StatusRunning, domain.StatusCompleted},
		{domain.StatusRunning, domain.StatusFailed},
		{domain.StatusRunning, domain.StatusTimeout},
		{domain.StatusRunning, domain.StatusCancelled},
		{domain.StatusRunning, domain.StatusQueued}, // Infrastructure recovery
	}

	for _, tc := range validCases {
		t.Run(string(tc.from)+"->"+string(tc.to), func(t *testing.T) {
			if !sm.CanTransition(tc.from, tc.to) {
				t.Fatalf("expected transition %s -> %s to be allowed", tc.from, tc.to)
			}
			if err := sm.Transition(tc.from, tc.to); err != nil {
				t.Fatalf("expected nil error, got: %v", err)
			}
		})
	}
}

func TestStateMachine_InvalidTransitions(t *testing.T) {
	sm := statemachine.New()

	invalidCases := []struct {
		from domain.JobStatus
		to   domain.JobStatus
	}{
		// Terminal states cannot transition to anything
		{domain.StatusCompleted, domain.StatusRunning},
		{domain.StatusCompleted, domain.StatusQueued},
		{domain.StatusCompleted, domain.StatusFailed},
		{domain.StatusFailed, domain.StatusRunning},
		{domain.StatusFailed, domain.StatusCompleted},
		{domain.StatusTimeout, domain.StatusRunning},
		{domain.StatusTimeout, domain.StatusCompleted},
		{domain.StatusCancelled, domain.StatusRunning},
		{domain.StatusCancelled, domain.StatusCompleted},

		// Queued cannot jump directly to terminal execution states
		{domain.StatusQueued, domain.StatusCompleted},
		{domain.StatusQueued, domain.StatusFailed},
		{domain.StatusQueued, domain.StatusTimeout},

		// Same-state transition not permitted
		{domain.StatusQueued, domain.StatusQueued},
		{domain.StatusRunning, domain.StatusRunning},
	}

	for _, tc := range invalidCases {
		t.Run(string(tc.from)+"->"+string(tc.to), func(t *testing.T) {
			if sm.CanTransition(tc.from, tc.to) {
				t.Fatalf("expected transition %s -> %s to be forbidden", tc.from, tc.to)
			}
			if err := sm.Transition(tc.from, tc.to); err == nil {
				t.Fatalf("expected error for forbidden transition %s -> %s, got nil", tc.from, tc.to)
			}
		})
	}
}
