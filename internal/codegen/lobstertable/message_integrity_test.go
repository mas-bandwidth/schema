package lobstertable_test

import (
	"encoding/binary"
	"fmt"
	"hash/crc32"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/lobster/lobsterrun"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
)

func TestLobsterMessageIntegrity(t *testing.T) {
	slowtest.Gate(t, "lobster message integrity")
	path := filepath.Join(t.TempDir(), "Batch.schema")
	const src = `package batch
fixed table Checked | integrity="crc32c" {
 n int32
 count uint16
}
fixed table Plain {
 n int32
 count uint16
}
`
	if err := os.WriteFile(path, []byte(src), 0644); err != nil {
		t.Fatal(err)
	}
	g, err := generateLoadUnit(t, []string{path})
	if err != nil {
		t.Fatal(err)
	}
	m := tabletext.NewModel(g.u)
	inputs := map[string][]byte{}
	for _, name := range []string{"Checked", "Plain"} {
		for _, count := range []int{1, 3, 256} {
			values := make([]*tabletext.Instance, count)
			for i := range values {
				v := m.New(m.Lookup(name))
				var rep tabletext.Report
				if !m.Read(v, []byte(`{"n":1000,"count":7}`), &rep) || !rep.Silent() {
					t.Fatal(rep)
				}
				values[i] = v
			}
			data, err := tablewire.EncodeMessages(m, values)
			if err != nil {
				t.Fatal(err)
			}
			if name == "Checked" {
				n := len(data) - 4
				if n < 2 || binary.LittleEndian.Uint32(data[n:]) != crc32.Checksum(data[:n], crc32.MakeTable(crc32.Castagnoli)) {
					t.Fatal("reference omitted message CRC")
				}
			}
			inputs[fmt.Sprintf("%s%d", name, count)] = data
		}
	}
	var program strings.Builder
	for _, mod := range g.mods {
		fmt.Fprintf(&program, "import .%s\n", mod)
	}
	program.WriteString(`import .message
let vocab = schema_rt.own_vocabulary()
def probe(root:string, count:int):
    let expected = read_file(root + "{count}")
    assert expected
    let authored:[schema_rt.Record] = []
    for(count):
        let rec = schema_rt.new_record(schema_rt.table_of(root))
        rec.fields[0].cell.i = 1000
        rec.fields[1].cell.i = 7
        push(authored, rec)
    let wr = schema_rt.LoadReport {}
    let saved, used = schema_rt.save_messages(authored, wr)
    assert used == length(expected) and saved == expected
    assert schema_rt.measure_messages(authored, schema_rt.LoadReport {}) == used
    let report = schema_rt.LoadReport {}
    let values, read_count, ok = schema_rt.load_messages(root, vocab, expected, report, 256)
    assert ok and read_count == count and length(values) == count
    for(values) value:
        assert value.fields[0].cell.i == 1000 and value.fields[1].cell.i == 7
    if root == "Checked" and count == 3:
        for(length(expected)) i:
            let original = expected[i]
            for(8) bit:
                let bad = write_int8_le(expected, i, original ^ (1 << bit))
                let rep = schema_rt.LoadReport {}
                let body, n, accepted = schema_rt.load_messages(root, vocab, bad, rep, 256)
                assert not accepted and n == 0 and length(body) == 0
            write_int8_le(expected, i, original)
        for(5) n:
            let rep = schema_rt.LoadReport {}
            let body, truncated_count, accepted = schema_rt.load_messages(root, vocab, substring(expected, 0, n), rep, 256)
            assert not accepted and truncated_count == 0 and length(body) == 0
        let short_rep = schema_rt.LoadReport {}
        let short_body, needed, short_ok = schema_rt.load_messages(root, vocab, expected, short_rep, count - 1)
        assert not short_ok and needed == count and length(short_body) == 0
        let stripped = substring(expected, 0, length(expected) - 4)
        let missing_rep = schema_rt.LoadReport {}
        let missing, missing_count, missing_ok = schema_rt.load_messages(root, vocab, stripped, missing_rep, 256)
        assert not missing_ok and missing_count == 0 and length(missing) == 0
for([1,3,256]) count:
    probe("Checked", count)
    probe("Plain", count)
print("message integrity passed")
`)
	if err := os.WriteFile(filepath.Join(g.dir, "main.lobster"), []byte(program.String()), 0644); err != nil {
		t.Fatal(err)
	}
	out := lobsterrun.Run(t, g.dir, "main.lobster", inputs)
	if strings.TrimSpace(string(out)) != "message integrity passed" {
		t.Fatalf("%s", out)
	}
	// Removing only the checksum comparison must accept a checksum-only
	// corruption; a parser crash or a different refusal is not a control pass.
	runtime := filepath.Join(g.dir, "schema_tables.lobster")
	srcRuntime, err := os.ReadFile(runtime)
	if err != nil {
		t.Fatal(err)
	}
	const anchor = "    if read_uint32_le(data, n) != wire_crc32c(data, n):\n"
	if strings.Count(string(srcRuntime), anchor) != 1 {
		t.Fatal("checksum comparison must occur once")
	}
	patched := strings.Replace(string(srcRuntime), anchor, "    if false:\n", 1)
	if err := os.WriteFile(runtime, []byte(patched), 0644); err != nil {
		t.Fatal(err)
	}
	control := `import .BatchTable
import .message
let good = read_file("Checked3")
assert good
let bad = write_int8_le(good, length(good) - 1, good[length(good) - 1] ^ 1)
let rep = schema_rt.LoadReport {}
let values, n, ok = schema_rt.load_messages("Checked", schema_rt.own_vocabulary(), bad, rep, 256)
assert ok and n == 3 and length(values) == 3
print("checksum accepted")
`
	if err := os.WriteFile(filepath.Join(g.dir, "main.lobster"), []byte(control), 0644); err != nil {
		t.Fatal(err)
	}
	if out := strings.TrimSpace(string(lobsterrun.Run(t, g.dir, "main.lobster", inputs))); out != "checksum accepted" {
		t.Fatal(out)
	}

}
