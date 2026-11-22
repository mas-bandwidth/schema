package lobstertable_test

import (
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v3/internal/codegen/lobster/lobsterrun"
	"github.com/mas-bandwidth/schema/v3/internal/slowtest"
	"github.com/mas-bandwidth/schema/v3/internal/tablecook"
	"github.com/mas-bandwidth/schema/v3/internal/tabletext"
	"github.com/mas-bandwidth/schema/v3/internal/tablewire"
)

// cookCheckSchema reaches every clause the cook check reads a value for: a
// bool byte, a float32's and a float64's bits, a union's tag (a void arm, a
// scalar arm and a body arm), a map's keys, a bounded string's used length, a
// counted array's count and dead slots, an optional's present byte, list and
// text placements in the extent, and references to a node of the right type.
const cookCheckSchema = `package probe

table Leaf {
    n     int32
    flag  bool
    ratio float32
    next  *Leaf
}

union Pick {
    empty
    count uint16
    leaf  Leaf
}

table Root {
    flag   bool
    ratio  float32
    wide   float64
    pick   Pick
    label  string(6)
    marks  [..3]int16
    maybe  ?int32
    names  map[string(4)]int32
    ids    map[int16]uint8
    items  []int32
    words  []string
    keys   map[string]int32
    head   *Leaf
    tail   *Leaf
    title  *string
}
`

const cookCheckRecord = `{"flag":true,"ratio":1.5,"wide":-2.25,"pick":{"leaf":{"n":3,"flag":true,"ratio":0.5}},"label":"abc","marks":[1,2],"maybe":7,"names":{"a":1,"bb":2,"ccc":3},"ids":{"-3":1,"4":2,"9":3},"items":[5,6,7],"words":["hello","there"],"keys":{"x":1,"yy":2},"head":{"n":1,"flag":false,"ratio":2,"next":{"n":2,"flag":true,"ratio":4}},"tail":{"n":9,"flag":true,"ratio":8},"title":"t"}`

// cookCheckProgram cooks the record the wire names, prints the cook, then
// inverts each byte in turn and answers whether ReadCook reads the result;
// last it points the second Leaf of the head's chain back at the first, a
// cycle no single inverted byte makes.
const cookCheckProgram = `import .schema_save
let wire = read_file("wire.bin")
assert wire
let rec = schema_rt.load("Root", wire, schema_rt.LoadReport {})
assert rec
let cook = probe.RootCook(rec, false)
assert length(cook) > 0
hex(cook)
var p = 0
while p < length(cook):
    var d = substring(cook, 0, length(cook))
    d = write_int8_le(d, p, (cook[p] ^ 0xff) & 0xff)
    print(if probe.RootReadCook(d): "record" else: "refused")
    p++
let base = schema_rt.cook_align_up(64, read_uint64_le(cook, 40))
let dlen = read_uint64_le(cook, 24)
let entries = read_uint64_le(cook, 32) / 16
let ct = schema_rt.cook_table_of("Leaf")
assert ct
let off = ct.fields[3].off
let leafid = schema_rt.table_of("Leaf").id
var first = -1
var slot = -1
var i = 0
while i < entries:
    let e = base + dlen + i * 16
    if read_uint64_le(cook, e + 8) == leafid:
        let at = read_uint64_le(cook, e) + off
        let delta = read_uint64_le(cook, base + at)
        if delta != 0 and first < 0:
            // head names head.next, whose own next is null: point it back
            first = at - off
            slot = at + delta + off
    i++
assert first >= 0 and read_uint64_le(cook, base + slot) == 0
let cyclic = write_int64_le(copy(cook), base + slot, first - slot)
print("cycle " + (if probe.RootReadCook(cyclic): "record" else: "refused"))
`

// cookCheckRun runs the program, with sabotage applied to the emitted
// cook.lobster first when it is not nil, and answers the cook, one verdict
// per inverted byte, the cycle's verdict, and the Go reference's Uncook
// verdict for each forged file (Uncook runs Check first).
func cookCheckRun(t *testing.T, sabotage func(string) string) (lobster, reference []string, cycle string) {
	t.Helper()
	slowtest.Gate(t, "the Lobster toolchain")
	g, err := generateLoadUnit(t, []string{writeSchema(t, "Probe", cookCheckSchema)})
	if err != nil {
		t.Fatal(err)
	}
	m := tabletext.NewModel(g.u)
	root := m.Lookup("Root")
	rec := m.New(root)
	var report tabletext.Report
	if !m.Read(rec, []byte(cookCheckRecord), &report) || !report.Silent() {
		t.Fatalf("fixture read: %+v", report)
	}
	wire, err := tablewire.Encode(m, rec)
	if err != nil {
		t.Fatal(err)
	}
	if sabotage != nil {
		p := filepath.Join(g.dir, "cook.lobster")
		src, err := os.ReadFile(p)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(sabotage(string(src))), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	var program strings.Builder
	for _, mod := range g.mods {
		fmt.Fprintf(&program, "import .%s\n", mod)
	}
	program.WriteString(lobsterrun.HexProbe)
	program.WriteString(cookCheckProgram)
	if err := os.WriteFile(filepath.Join(g.dir, "check.lobster"), []byte(program.String()), 0o644); err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(string(lobsterrun.Run(t, g.dir, "check.lobster", map[string][]byte{"wire.bin": wire}))), "\n")
	cook, err := hex.DecodeString(lines[0])
	// a cyclic graph a read let through is reported as a leak after the
	// verdicts, so the lines past the cycle's are not read
	if err != nil || len(lines) < len(cook)+2 {
		t.Fatalf("the probe printed %d lines for a cook of %d bytes (%v)", len(lines), len(cook), err)
	}
	if _, err := tablecook.Uncook(m, root, cook); err != nil {
		t.Fatalf("the reference refuses the unmodified cook: %v", err)
	}
	lobster = lines[1 : len(cook)+1]
	reference = make([]string, len(cook))
	for p := range cook {
		d := append([]byte(nil), cook...)
		d[p] ^= 0xff
		reference[p] = "record"
		if _, err := tablecook.Uncook(m, root, d); err != nil {
			reference[p] = "refused"
		}
	}
	return lobster, reference, lines[len(cook)+1]
}

// cookCheckDisagreements lists the positions where ReadCook reads a file the
// reference refuses, or refuses one it reads.
func cookCheckDisagreements(lobster, reference []string) []string {
	var bad []string
	for p := range reference {
		if lobster[p] != reference[p] {
			bad = append(bad, fmt.Sprintf("byte %d: lobster %s, reference %s", p, lobster[p], reference[p]))
		}
	}
	return bad
}

// TestLobsterReadCookRunsTheCookCheck holds ReadCook to ONE VERDICT with the
// reference's cook read (docs/SPEC-TABLES.md §7, §7.4; internal/tablecook's
// Uncook asks Check first): over every single inverted byte of a cook that
// reaches each value the check reads, ReadCook reads exactly the files the
// reference reads, and it refuses a cycle.
func TestLobsterReadCookRunsTheCookCheck(t *testing.T) {
	t.Parallel()
	lobster, reference, cycle := cookCheckRun(t, nil)
	if bad := cookCheckDisagreements(lobster, reference); len(bad) > 0 {
		t.Fatalf("%d of %d forged cooks get another verdict than the reference's: %v", len(bad), len(reference), bad)
	}
	if cycle != "cycle refused" {
		t.Fatalf("a cyclic cook: %s, want refused", cycle)
	}
}

// TestLobsterReadCookRunsTheCookCheckNegativeControl removes the check from
// ReadCook in an emitted copy and requires the gate red.
func TestLobsterReadCookRunsTheCookCheckNegativeControl(t *testing.T) {
	t.Parallel()
	const call = "    if cook_check(data, read_uint64_le(data, 0) != cook_magic) != \"ok\":\n        return nil\n"
	lobster, reference, cycle := cookCheckRun(t, func(src string) string {
		if strings.Count(src, call) != 1 {
			t.Fatal("the control's anchor, ReadCook's call of the check, is not in cook.lobster once")
		}
		return strings.Replace(src, call, "", 1)
	})
	bad := cookCheckDisagreements(lobster, reference)
	if len(bad) == 0 && cycle == "cycle refused" {
		t.Fatal("with the check removed, ReadCook still agrees with the reference on every forgery: the gate cannot fail")
	}
	t.Logf("negative control: the check removed, %d forgeries disagree; %s", len(bad), cycle)
}
