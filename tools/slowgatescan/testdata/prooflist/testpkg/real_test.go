package testpkg

import "testing"

// slowtest.Gate( is named here only so the fixture package is treated as
// gated by scanMakefiles; the lint parses declarations and never runs this.
func TestReal(t *testing.T) {
	t.Parallel()
	const probe = `func TestPlanted(t *testing.T) {}`
	_ = probe
}
