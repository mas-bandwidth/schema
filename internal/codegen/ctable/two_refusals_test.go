package ctable

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

// TestTwoRefusalsFirstNamesTheReadC holds the C builder reads to the float rows
// of the shared pin table: a read that meets a NaN and a count above the int32
// storage cap is named by the refusal it meets first (docs/SPEC-TABLES.md
// §2.9). root_load_builder's reason (the report's reason enum) and root_load_builder's
// refuse_reason each answer the pin's strict column.
func TestTwoRefusalsFirstNamesTheReadC(t *testing.T) {
	slowtest.Gate(t, "the C compiler (cc)")
	t.Parallel()
	data, err := os.ReadFile("../goosetable/testdata/load_refusal_reasons.tsv")
	if err != nil {
		t.Fatal(err)
	}
	var body strings.Builder
	body.WriteString(`
#define name(r) ((r) == SCHEMA_TABLE_REFUSE_OK ? "ok" : (r) == SCHEMA_TABLE_REFUSE_NON_FINITE_FLOAT ? "non_finite_float" : (r) == SCHEMA_TABLE_REFUSE_COUNT_OVER_EXTENT_CAP ? "count_over_extent_cap" : "other")
#define reason_name(r) ((int)(r) == (int)SCHEMA_TABLE_NON_FINITE_FLOAT ? "non_finite_float" : (int)(r) == (int)SCHEMA_TABLE_REFUSE_COUNT_OVER_EXTENT_CAP ? "count_over_extent_cap" : "other")
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
        RootBuilder builder;
        TableReport tolerant_report, plain_report;
        int kept, read;
        if (!root_builder_init(&builder)) { return 1; }
        memset(&tolerant_report, 0, sizeof(tolerant_report));
        kept = root_load_builder(&builder, wire, (int64_t)sizeof(wire), &tolerant_report);
        printf("%s tolerant %%s\n", kept ? "ok" : reason_name(tolerant_report.reason));
        memset(&plain_report, 0, sizeof(plain_report));
        read = root_load_builder(&builder, wire, (int64_t)sizeof(wire), &plain_report);
        printf("%s plain %%s\n", read ? "ok" : name(plain_report.refuse_reason));
        root_builder_shutdown(&builder);
    }
`, cBytes(wire), f[0], f[0])
	}
	if len(want) != 3 {
		t.Fatalf("%d float rows, want 3", len(want))
	}
	out, err := runCCountRefusalProbe(t, unitFrom(t, twoRefusalsSchema), body.String())
	if err != nil {
		t.Fatalf("two-refusals probe: %v\n%s", err, out)
	}
	seen := 0
	for line := range strings.SplitSeq(strings.TrimSpace(out), "\n") {
		f := strings.Fields(line)
		if len(f) != 3 {
			t.Fatalf("probe line %q", line)
		}
		seen++
		if f[2] != want[f[0]] {
			t.Errorf("%s %s: %s, the pin %s", f[0], f[1], f[2], want[f[0]])
		}
	}
	if seen != 6 {
		t.Fatalf("%d probe lines, want 6:\n%s", seen, out)
	}
}
