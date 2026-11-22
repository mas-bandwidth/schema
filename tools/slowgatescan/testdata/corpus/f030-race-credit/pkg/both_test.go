package pkg

import "testing"

// TestBoth is built with and without -race; it reaches slowtest.Gate.
func TestBoth(t *testing.T) {
	slowtest.Gate(t, "x")
}
