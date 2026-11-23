package ctable

import (
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
)

// THE NEWER READER READS OLD DATA, NEVER THE OTHER WAY (docs/SPEC-TABLES.md
// §4, A TABLE LOAD HOLDS A MASK TO ITS WIDTH; SPEC.md §4.2). The C leg runs
// testdata/flags_past_width_main.c over the generated code of a two-variant
// mask: a variable file and a fixed record, each this build own save with
// bit 2 planted in the mask byte, refuse out_of_range beside a twin that
// reads; a message from a newer three-variant writer refuses the same way;
// and both saves of a mask past W abort in a debug build. Red on the code
// that kept such a bit.

const flagsPastWidthNarrow = "package probe\n flags Features { One, Two }\n fixed table Root {\n  tag uint8\n  direct Features\n }\n"

const flagsPastWidthWide = "package probe\n flags Features { One, Two, Three }\n fixed table Root {\n  tag uint8\n  direct Features\n }\n"

func cByteList(b []byte) string {
	var s strings.Builder
	for _, c := range b {
		fmt.Fprintf(&s, "0x%02x,", c)
	}
	return s.String()
}

// flagsPastWidthWideMessage is the newer writer: its announcement and one
// message of Root carrying the given mask, from the Go engine.
func flagsPastWidthWideMessage(t *testing.T, mask string) ([]byte, []byte) {
	t.Helper()
	u := unitFrom(t, flagsPastWidthWide)
	m := tabletext.NewModel(u)
	v := m.New(m.Lookup("Root"))
	var r tabletext.Report
	if !m.Read(v, []byte("{\"direct\":"+mask+"}"), &r) {
		t.Fatalf("the wide engine refused %s: %+v", mask, r)
	}
	msg, err := tablewire.EncodeMessage(m, v)
	if err != nil {
		t.Fatal(err)
	}
	return tablewire.Announce(u), msg
}

func TestCFlagsPastWidthRefused(t *testing.T) {
	slowtest.Gate(t, "the C compiler (cc)")
	main, err := os.ReadFile("testdata/flags_past_width_main.c")
	if err != nil {
		t.Fatal(err)
	}
	ann, inside := flagsPastWidthWideMessage(t, "[\"One\"]")
	_, past := flagsPastWidthWideMessage(t, "[\"One\",\"Three\"]")
	source := strings.NewReplacer(
		"WIDE_ANNOUNCEMENT", cByteList(ann),
		"WIDE_INSIDE", cByteList(inside),
		"WIDE_PAST", cByteList(past),
	).Replace(string(main))
	runCGenerated(t, flagsPastWidthNarrow, source, identityCCFlags(t)...)
}
