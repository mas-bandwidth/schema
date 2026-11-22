package lobstertable_test

import (
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
)

// THE WRITER REFUSES AN ID TABLE ITS READERS REFUSE (docs/SPEC-TABLES.md §3,
// ruling R8), for Lobster, which refuses in every build. The unit is
// tablewire.IdCapSchema; the three values need 4095, 4096 and 4097 ids. The
// first two are written and the strict read accepts them, with the count word
// in the trailer; the third is refused by name and writes nothing.
func TestLobsterWriterRefusesMoreThan4096Ids(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "the Lobster toolchain")
	g := finiteGenerate(t, tablewire.IdCapSchema())
	out := finiteRun(t, g, keyedRegistrations(g.u)+`
def idcap(x:int, y:int):
    let rec = schema_rt.new_record(schema_rt.table_of("Root"))
    let k = fslot(rec, "keys")
    while length(k.elems) < `+"4094"+`:
        push(k.elems, schema_rt.Value {})
    for(k.elems) e:
        e.i = 1
    fslot(rec, "x").cell.i = x
    fslot(rec, "y").cell.i = y
    let w, why = schema_rt.save_reasoned(rec)
    if why != "":
        print("{x}{y} refused {why} {length(w)}")
    else:
        let back, reason = schema_rt.load_strict("Root", w, schema_rt.LoadReport {})
        let tail = w.substring(length(w) - 8, 8)
        let count = tail[0] + tail[1] * 256
        print("{x}{y} wrote {count} {if back: "read" else: reason}")
idcap(0, 0)
idcap(1, 0)
idcap(1, 1)
`, nil)
	got := strings.Join(out, "\n")
	want := "00 wrote 4095 read\n10 wrote 4096 read\n11 refused id_table_over_bound 0"
	if got != want {
		t.Fatalf("id cap rows:\n%s\nwant:\n%s", got, want)
	}
}
