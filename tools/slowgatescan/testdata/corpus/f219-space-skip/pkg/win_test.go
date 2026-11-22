package pkg

import "testing"

// Both reach slowtest.Gate; the scan reads the calls, nothing runs them.
func TestWin_Codec(t *testing.T) {
	slowtest.Gate(t, "x")
}

func TestWin_Other(t *testing.T) {
	slowtest.Gate(t, "x")
}
