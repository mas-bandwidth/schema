package fixture

import "testing"

// A fixture under testdata: the go tool builds no package here.
func TestFixture(t *testing.T) {
	slowtest.Gate(t, "x")
}
