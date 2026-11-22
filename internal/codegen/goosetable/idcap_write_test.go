package goosetable_test

import (
	"fmt"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/goose/gooserun"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
)

// THE WRITER REFUSES AN ID TABLE ITS READERS REFUSE (docs/SPEC-TABLES.md §3,
// ruling R8), for Goose, which refuses in every build. The unit is
// tablewire.IdCapSchema. A wire needing 4095 ids and one needing 4096 are
// loaded and saved back: both are written, with the count word in the
// trailer, and the saved file loads again. Then the 4096-id value has its
// `y` set, which needs 4097: the save refuses id_table_over_bound and
// writes nothing.
const idCapMain = probePrelude + `import .wire_value;

fn probe(file: const u8[:], set_y: bool) {
    let data = input(file);
    var vals: schema_rt::LoadVal[>..] = [];
    var nodes: schema_rt::LoadNode[>..] = [];
    var report = schema_rt::LoadReport { .. };
    let v = schema_rt::load_table(conformance_unit(), "Root", data, vals, nodes, report);
    if v < 0 { write_stdout(str(file, " load refused", "\n")); return; }
    if set_y {
        let y = schema_rt::value_child(vals, v, 2);
        vals[y].state = schema_rt::ValPresent;
        vals[y].bits.lo = 1;
    }
    var out: u8[>..<] = [];
    let refusal = schema_rt::save_table(conformance_unit(), "Root", data, vals, nodes, v, out);
    if refusal != 0 {
        write_stdout(str(file, " y=", (if set_y { 1 } else { 0 }), " refused ", schema_rt::save_refusal_name(refusal), " ", out.len, "\n"));
        return;
    }
    var vals2: schema_rt::LoadVal[>..] = [];
    var nodes2: schema_rt::LoadNode[>..] = [];
    var report2 = schema_rt::LoadReport { .. };
    let again = schema_rt::load_table(conformance_unit(), "Root", out[0..out.len], vals2, nodes2, report2);
    let count = (out[out.len - 8] as! i64) + (out[out.len - 7] as! i64) * 256;
    write_stdout(str(file, " y=", (if set_y { 1 } else { 0 }), " wrote ", count, " ", (if again >= 0 && !report2.body_damaged { "read" } else { "damaged" }), "\n"));
}

fn main() {
    probe("a.bin", false);
    probe("b.bin", false);
    probe("b.bin", true);
}
`

func TestGooseWriterRefusesMoreThan4096Ids(t *testing.T) {
	t.Parallel()
	u := probeUnit(t, tablewire.IdCapSchema())
	files := probeFiles(t, u, nil)
	m := tabletext.NewModel(u)
	var keys []string
	for i := 0; i < tablewire.IdCapVariants; i++ {
		keys = append(keys, fmt.Sprintf("\"V%d\":1", i))
	}
	for file, x := range map[string]int{"a.bin": 0, "b.bin": 1} {
		v := m.New(m.Lookup("Root"))
		var report tabletext.Report
		text := fmt.Sprintf(`{"keys":{%s},"x":%d}`, strings.Join(keys, ","), x)
		if !m.Read(v, []byte(text), &report) {
			t.Fatalf("oracle text: %+v", report)
		}
		wire, err := tablewire.Encode(m, v)
		if err != nil {
			t.Fatal(err)
		}
		files[file] = wire
	}
	files["main.goose"] = []byte(idCapMain)
	got := strings.TrimSpace(string(gooserun.Run(t, t.TempDir(), "main.goose", files)))
	want := "a.bin y=0 wrote 4095 read\nb.bin y=0 wrote 4096 read\nb.bin y=1 refused id_table_over_bound 0"
	if got != want {
		t.Fatalf("id cap rows:\n%s\nwant:\n%s", got, want)
	}
}
