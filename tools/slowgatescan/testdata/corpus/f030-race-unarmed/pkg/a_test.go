package pkg

import "testing"

// TestGated reaches slowtest.Gate; the scan reads the call, nothing runs it.
func TestGated(t *testing.T) {
	slowtest.Gate(t, "x")
}

func TestFast(t *testing.T) {}
