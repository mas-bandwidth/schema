package pkg

import "testing"

// An ordinary package. The next line spells the call in a comment only:
// slowtest.Gate(t, "x")
func TestFast(t *testing.T) {
	const note = `slowtest.Gate(t, "x") is spelled in this string, not called`
	_ = note
}
