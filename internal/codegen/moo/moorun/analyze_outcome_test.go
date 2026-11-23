package moorun

import (
	"errors"
	"testing"
)

// TestAnalyzeOutcomeRefusesSilentFailure holds SF-32: moo-analyze dying with
// no output must not read as a clean (empty) result.
func TestAnalyzeOutcomeRefusesSilentFailure(t *testing.T) {
	t.Parallel()
	if _, err := AnalyzeOutcome(nil, errors.New("signal: killed")); err == nil {
		t.Fatal("a failed run with no output was accepted as clean")
	}
	if _, err := AnalyzeOutcome([]byte(" \n"), errors.New("exit status 1")); err == nil {
		t.Fatal("a failed run with only whitespace was accepted as clean")
	}
	// Lawful twin: a failed run that says why returns what it said, and a
	// clean run returns empty with no error.
	got, err := AnalyzeOutcome([]byte("a.moo:1: error"), errors.New("exit status 1"))
	if err != nil || got == "" {
		t.Fatalf("a failed run with output must return the output: %q, %v", got, err)
	}
	got, err = AnalyzeOutcome(nil, nil)
	if err != nil || got != "" {
		t.Fatalf("a clean run must return empty and no error: %q, %v", got, err)
	}
}
