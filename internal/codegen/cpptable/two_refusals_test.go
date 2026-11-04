package cpptable

import (
	"encoding/hex"
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
)

// twoRefusalsSchema is the float unit of
// internal/codegen/goosetable/testdata/load_refusal_reasons.tsv.
const twoRefusalsSchema = "package probe\n table Root { f float32\n values []int32 }\n"

// TestTwoRefusalsFirstNamesTheReadCPP holds the C++ builder reads to the float
// rows of the shared pin table: a read that meets a NaN and a count above the
// int32 storage cap is named by the refusal it meets first (docs/SPEC-TABLES.md
// §2.9). LoadBuilderTolerant's reason, LoadBuilder's refuse_reason and
// ReadStrict's reason each answer the pin's strict column.
func TestTwoRefusalsFirstNamesTheReadCPP(t *testing.T) {
	slowtest.Gate(t, "the C++ compiler (c++)")
	t.Parallel()
	data, err := os.ReadFile("../goosetable/testdata/load_refusal_reasons.tsv")
	if err != nil {
		t.Fatal(err)
	}
	var body strings.Builder
	body.WriteString(`
    auto name = []( TableRefuseReason r ) -> const char * {
        if ( r == ok ) { return "ok"; }
        if ( r == non_finite_float ) { return "non_finite_float"; }
        if ( r == count_over_extent_cap ) { return "count_over_extent_cap"; }
        return "other";
    };
`)
	want := map[string]string{}
	for line := range strings.SplitSeq(string(data), "\n") {
		f := strings.Split(line, "\t")
		if len(f) != 6 || f[1] != "float" {
			continue
		}
		wire, err := hex.DecodeString(f[2])
		if err != nil {
			t.Fatal(err)
		}
		want[f[0]] = f[3]
		fmt.Fprintf(&body, `
    {
        static const uint8_t wire[] = { %s };
        RootBuilder tolerant;
        TableReport tolerant_report;
        const bool kept = RootLoadBuilderTolerant( tolerant, wire, (int64_t)sizeof(wire), &tolerant_report );
        printf( "%s tolerant %%s\n", kept ? "ok" : name( tolerant_report.reason ) );
        RootBuilder plain;
        TableReport plain_report;
        const bool read = RootLoadBuilder( plain, wire, (int64_t)sizeof(wire), &plain_report );
        printf( "%s plain %%s\n", read ? "ok" : name( plain_report.refuse_reason ) );
        TableRefuseReason why = ok;
        const Root * root = RootReadStrict( wire, (int64_t)sizeof(wire), &why );
        if ( root != NULL ) { table_release( TableDefaultAllocator(), (void *)root ); }
        printf( "%s strict %%s\n", root != NULL ? "ok" : name( why ) );
    }
`, cppBytes(wire), f[0], f[0], f[0])
	}
	if len(want) != 3 {
		t.Fatalf("%d float rows, want 3", len(want))
	}
	out, err := runCPPCountRefusalProbe(t, loadUnitFromSource(t, twoRefusalsSchema), body.String())
	if err != nil {
		t.Fatalf("two-refusals probe: %v\n%s", err, out)
	}
	seen := 0
	for line := range strings.SplitSeq(strings.TrimSpace(string(out)), "\n") {
		f := strings.Fields(line)
		if len(f) != 3 {
			t.Fatalf("probe line %q", line)
		}
		seen++
		if f[2] != want[f[0]] {
			t.Errorf("%s %s: %s, the pin %s", f[0], f[1], f[2], want[f[0]])
		}
	}
	if seen != 9 {
		t.Fatalf("%d probe lines, want 9:\n%s", seen, out)
	}
}
