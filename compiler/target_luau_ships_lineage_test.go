// The Luau target takes the lock's lineage as data (docs/FIXED-FORM-ALGORITHM.md
// §5.2, §5.9 #1): the same REAL LOCK compiler/fixedlineageship_test.go writes
// with schema lock, a widening and schema lock again, generated through the
// driver. The OLDER layout's wire hash is in the emitted module as static data
// beside the current one, and its known entry carries §5.2's record_bytes, THE
// WHOLE RECORD: eight hash bytes plus the lock's body, added once by the
// driver and never again by the backend. When the tag drops, luau joins
// fixedLineageShipTargets and this file goes.
package compiler

import (
	"fmt"
	"regexp"
	"strconv"
	"testing"
)

func TestLuauShipsTheLockLineage(t *testing.T) {
	c, u, older, current, lin := fixedLineageShipLockedUnit(t)
	files, err := c.Generate(u, "luau", nil)
	if err != nil {
		t.Fatalf("luau: generate: %v", err)
	}
	want := 8 + lin[0].Record
	known := func(h uint64) *regexp.Regexp {
		return regexp.MustCompile(fmt.Sprintf(`table\.freeze\(\{ hash = 0x%016xi, layout = [^\n]*?(?:\n[^\n]*?)*?layoutBytes = \d+, recordBytes = (\d+) \}\)`, h))
	}
	for _, h := range []uint64{older, current} {
		matches := 0
		for name, data := range files {
			for _, m := range known(h).FindAllStringSubmatch(string(data), -1) {
				matches++
				got, err := strconv.ParseInt(m[1], 10, 64)
				if err != nil {
					t.Fatalf("%s: the record size is a number: %q", name, m[1])
				}
				if h == older && got != want {
					t.Errorf("%s: the OLDER layout 0x%016x ships record_bytes %d, want the whole record 8 + %d = %d", name, h, got, lin[0].Record, want)
				}
			}
		}
		if matches != 1 {
			t.Errorf("the emitted modules carry %d known entries for layout 0x%016x, want exactly one: the driver handed luau no lineage", matches, h)
		}
	}
}
