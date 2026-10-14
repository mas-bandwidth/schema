//go:build windows

package main

import (
	"strings"
	"testing"
)

func TestTreelockWindowsRefusal(t *testing.T) {
	// Obligation retained: Windows platform locking and certification are not yet implemented.
	// Verify that acquireFlock returns an explicit unsupported refusal and never claims unearned ownership.
	err := acquireFlock(0)
	if err == nil {
		t.Fatal("expected acquireFlock on Windows to return an error, got nil (never claim unearned ownership)")
	}
	if !strings.Contains(err.Error(), "unsupported on windows") {
		t.Fatalf("unexpected error message: %v", err)
	}
}
