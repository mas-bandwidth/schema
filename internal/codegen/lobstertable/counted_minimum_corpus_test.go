package lobstertable_test

import (
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
)

// THE COUNTED-MINIMUM MESSAGE CORPUS ON LOBSTER (docs/SPEC-TABLES.md §3.3(a),
// §22.2; review rows F-116 and F-120). The rows are the committed corpus
// testdata/conformance/feature-witnesses/counted-minimum-message (cases.tsv),
// which the reference engine holds: the message writer refuses a live count
// outside [2, 4] (-1 and 0 among them, where it once wrote the three bytes of
// an omitted field) from save and measure alike, and writes the reference's
// bytes inside it, the birth value elided; the message read refuses the birth
// value stored explicitly (the 11 bytes the old writer emitted), beside its
// two controls. Lobster refuses a table write in every build.
func TestLobsterCountedMinimumMessageCorpus(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "the Lobster toolchain")
	const dir = "../../../testdata/conformance/feature-witnesses/counted-minimum-message"
	schema, err := os.ReadFile(filepath.Join(dir, "Probe.schema"))
	if err != nil {
		t.Fatal(err)
	}
	text, err := os.ReadFile(filepath.Join(dir, "cases.tsv"))
	if err != nil {
		t.Fatal(err)
	}
	wire := func(name string) []byte {
		b, err := os.ReadFile(filepath.Join(dir, name+".bin"))
		if err != nil {
			t.Fatal(err)
		}
		return b
	}
	var body strings.Builder
	body.WriteString(`
def counted(count:int) -> schema_rt.Record:
    let rec = schema_rt.new_record(schema_rt.table_of("Root"))
    let sl = fslot(rec, "few")
    while length(sl.elems) < max(4, count):
        push(sl.elems, schema_rt.Value {})
    for(max(0, count)) i:
        sl.elems[i].i = 0
    sl.count = count
    return rec
def write_row(name:string, count:int):
    let measured = schema_rt.measure_messages([counted(count)], schema_rt.LoadReport {})
    let mw, size = schema_rt.save_messages([counted(count)], schema_rt.LoadReport {})
    print(name + " " + (if size < 0: "refused" else: "wrote " + fhex(substring(mw, 0, size))) + " measure " + (if measured < 0: "refused" else: string(measured)))
def read_row(name:string, file:string):
    let data = read_file(file)
    assert data
    let v = schema_rt.new_vocabulary(4096)
    assert schema_rt.announce_read(v, schema_rt.announce(), schema_rt.LoadReport {})
    let rep = schema_rt.LoadReport {}
    let vals, used, ok = schema_rt.load_messages("Root", v, data, rep, 2)
    print(name + (if ok and used > 0 and length(vals) == 1: " read" else: " refused"))
`)
	inputs := map[string][]byte{}
	want := map[string]string{}
	var order []string
	for i, line := range strings.Split(string(text), "\n") {
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		f := strings.Split(line, "\t")
		if len(f) != 5 {
			t.Fatalf("cases.tsv:%d: %d fields, want 5", i+1, len(f))
		}
		kind, name, value, verdict, expect := f[0], f[1], f[2], f[3], f[4]
		order = append(order, name)
		switch kind {
		case "read":
			inputs[value+".bin"] = wire(value)
			fmt.Fprintf(&body, "read_row(%q, %q)\n", name, value+".bin")
			want[name] = verdict
		case "write":
			count, err := strconv.Atoi(value)
			if err != nil {
				t.Fatalf("%s: count %q", name, value)
			}
			fmt.Fprintf(&body, "write_row(%q, %d)\n", name, count)
			if verdict == "wrote" {
				b := wire(expect)
				want[name] = "wrote " + hex.EncodeToString(b) + " measure " + strconv.Itoa(len(b))
			} else {
				want[name] = "refused measure refused"
			}
		default:
			t.Fatalf("%s: kind %q", name, kind)
		}
	}
	out := finiteRun(t, finiteGenerate(t, string(schema)), body.String(), inputs)
	rows := map[string]string{}
	for _, line := range out {
		name, rest, _ := strings.Cut(line, " ")
		rows[name] = rest
	}
	for _, name := range order {
		t.Run(name, func(t *testing.T) {
			got, ran := rows[name]
			if !ran {
				t.Fatalf("the row did not run; the probe printed %q", out)
			}
			if got != want[name] {
				t.Errorf("%q, want %q", got, want[name])
			}
		})
	}
}
