package ctable

import (
	"strconv"
	"strings"
	"testing"
)

// Overlaid fields can share a wire offset while having different declared
// extents. Each emitted count/present row must carry its own declaration's span.
func TestCFixedDeadExtentOwnership(t *testing.T) {
	const schema = `package probe
 fixed table Narrow { small [..2]uint8 }
 fixed table Wide { large [..2]uint64 }
 fixed table PresentWide { longer ?[8]uint8 }
 fixed table PresentNarrow { shorter ?uint8 }
 union Counted { narrow Narrow
 wide Wide }
 union Wrapped { wide PresentWide
 narrow PresentNarrow }
 fixed table Root { counted Counted
 wrapped Wrapped }
 `
	header := string(generate(t, schema)["ProbeTable.h"])
	for _, tc := range []struct {
		note string
		want int64
	}{
		{"small count", 1}, {"large count", 8},
		{"longer present, absent payload defaults", 8}, {"shorter present, absent payload defaults", 1},
	} {
		line := cDeadPlanLine(t, header, "root", tc.note)
		fields := strings.Split(line[strings.Index(line, "{")+1:strings.LastIndex(line, "}")], ",")
		if len(fields) != 22 {
			t.Fatalf("unexpected plan row field count: %d", len(fields))
		}
		got, err := strconv.ParseInt(strings.TrimSuffix(strings.TrimSpace(fields[17]), "u"), 10, 64)
		if err != nil {
			t.Fatal(err)
		}
		if got != tc.want {
			t.Errorf("%s dead extent %d, want declared %d", tc.note, got, tc.want)
		}
	}
}
