package gatedpkg

import "testing"

// The one package that calls the gate.
func TestGatedPkg(t *testing.T) {
	slowtest.Gate(t, "x")
}
