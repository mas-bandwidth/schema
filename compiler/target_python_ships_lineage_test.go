//go:build schema_leg_python

// The python target takes the lock's lineage as data
// (docs/FIXED-FORM-ALGORITHM.md §5.2, §5.9 #1), generated through the driver
// and never handed in by the test: under a REAL LOCK (schema lock, a widening,
// schema lock again) the OLDER layout's entry is laid down beside the current
// one, with the lock's record body.
package compiler

import (
	"fmt"
	"regexp"
	"strings"
	"testing"
)

func TestPythonShipsTheLockLineage(t *testing.T) {
	c, u, older, current, lin := fixedLineageShipLockedUnit(t)
	files, err := c.Generate(u, "python", nil)
	if err != nil {
		t.Fatalf("python: generate: %v", err)
	}
	for _, h := range []uint64{older, current} {
		needle := fmt.Sprintf("_s_fx.Entry(0x%016x, ", h)
		found := false
		for _, data := range files {
			if strings.Contains(string(data), needle) {
				found = true
			}
		}
		if !found {
			t.Errorf("no emitted module lays down layout 0x%016x: the driver handed python no lineage", h)
		}
	}
	entry := regexp.MustCompile(fmt.Sprintf(`_s_fx\.Entry\(0x%016x, (\d+), `, older))
	matches := 0
	for name, data := range files {
		for _, m := range entry.FindAllStringSubmatch(string(data), -1) {
			matches++
			if m[1] != fmt.Sprint(lin[0].Record) {
				t.Errorf("%s: the OLDER layout 0x%016x lays down a record body of %s, want the lock's %d", name, older, m[1], lin[0].Record)
			}
		}
	}
	if matches != 1 {
		t.Errorf("the older layout 0x%016x is laid down %d times, want exactly once", older, matches)
	}
}
