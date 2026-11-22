package pkg

import "testing"

// Both reach slowtest.Gate; the scan reads the calls, nothing runs them.
func TestFirst(t *testing.T) {
	slowtest.Gate(t, "x")
}

func TestSecond(t *testing.T) {
	slowtest.Gate(t, "x")
}
