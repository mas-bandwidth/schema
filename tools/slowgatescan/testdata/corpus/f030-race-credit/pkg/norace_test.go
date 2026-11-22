//go:build !race

package pkg

import "testing"

// TestNoRace is not built under -race; it reaches slowtest.Gate.
func TestNoRace(t *testing.T) {
	slowtest.Gate(t, "x")
}
