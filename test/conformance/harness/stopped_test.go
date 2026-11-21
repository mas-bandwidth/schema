package main

import "testing"

// TestDriverThatStopsEarlyFailsByName holds the harness to a driver that exits
// non-zero partway through a surface (the Rust json-hostile driver once exited
// on its first ToJson refusal and blanked all 198 rows): the surface is red,
// the exit is named, and every case the driver never answered fails BY NAME.
// It never counts absent, and the success footer never prints.
func TestDriverThatStopsEarlyFailsByName(t *testing.T) {
	needShell(t)
	c := newFakeCorpus(t)
	out, ok := c.runHarness(t, c.committed(t, map[string]fakeLeg{
		referenceLang: {},
		"zz":          {stopEarly: "wire"},
	}))
	if ok {
		t.Errorf("a driver that stopped early left the harness green:\n%s", out)
	}
	mustSay(t, out,
		[]string{
			"FAIL",
			"the driver exited 1",
			"alpha: no answer: the driver exited 1 before it reached this case",
		},
		[]string{footer, "+1a"})
}
