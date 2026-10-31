package lobstertable_test

import (
	"bytes"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/compiler"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/lobster"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/lobster/lobsterrun"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
)

// saveDump is the save program's tail: load the wire, save the record, print
// the saved bytes as one hex line, then reload the saved bytes and print THE
// VALUE DUMP of the reloaded record (dumpLobster's drec), so a round trip is
// held to the reference on both sides.
const saveDump = `def saverun(root:string, file:string):
    let data = read_file(file)
    assert data
    let rep = schema_rt.LoadReport {}
    let rec = schema_rt.load_tolerant(root, data, rep)
    assert rec
    let out = schema_rt.save(rec)
    let h = hexs(out)
    let hn = (length(h) + 39999) / 40000
    print("{hn}")
    var j = 0
    while j < hn:
        print(substring(h, j * 40000, min(40000, length(h) - j * 40000)))
        j++
    let rep2 = schema_rt.LoadReport {}
    let rec2 = schema_rt.load_tolerant(root, out, rep2)
    labels = []
    var dump = "nil"
    if rec2:
        push(labels, rec2)
        dump = drec(rec2)
    let n = (length(dump) + 39999) / 40000
    print("{n}")
    var i = 0
    while i < n:
        print(substring(dump, i * 40000, min(40000, length(dump) - i * 40000)))
        i++
`

// saveProgram is the main for one unit: every table module, load and save,
// the keyed key registrations, the shared dump, then one saverun per wire.
func saveProgram(g *loadUnit, roots []string) string {
	var b strings.Builder
	for _, m := range g.mods {
		fmt.Fprintf(&b, "import .%s\n", m)
	}
	b.WriteString("import .load\n")
	b.WriteString("import .schema_save\n")
	b.WriteString(keyedRegistrations(g.u))
	b.WriteString(dumpLobster)
	b.WriteString(saveDump)
	for i, r := range roots {
		fmt.Fprintf(&b, "saverun(%s, \"w%d\")\n", lobster.StringLit([]byte(r)), i)
	}
	return b.String()
}

// goSave is the reference writer's answer for one wire: decode it into the
// Go instance model and encode that instance (internal/tablewire.Encode).
func goSave(g *loadUnit, rootName string, wire []byte) ([]byte, error) {
	m := tabletext.NewModel(g.u)
	inst := m.New(m.Lookup(rootName))
	var rep tabletext.Report
	if _, err := tablewire.Decode(m, inst, wire, &rep); err != nil {
		return nil, err
	}
	return tablewire.Encode(m, inst)
}

// saveResult is one unit's rows as Lobster wrote and re-loaded them.
type saveResult struct {
	saved []string
	dump  []string
}

// runSaveRows generates one program for the rows, saves each wire under the
// pinned toolchain, and reads back the saved hex line and the reloaded dump.
func runSaveRows(t *testing.T, g *loadUnit, rows []loadRow) saveResult {
	t.Helper()
	var roots []string
	inputs := map[string][]byte{}
	for i, row := range rows {
		wire := row.data
		if wire == nil {
			var err error
			if wire, err = os.ReadFile(filepath.Join(root, row.wire)); err != nil {
				t.Fatalf("%s: %v", row.name, err)
			}
		}
		inputs[fmt.Sprintf("w%d", i)] = wire
		roots = append(roots, row.root)
	}
	if err := os.WriteFile(filepath.Join(g.dir, "main.lobster"), []byte(saveProgram(g, roots)), 0o644); err != nil {
		t.Fatal(err)
	}
	out := strings.Split(strings.TrimRight(string(lobsterrun.Run(t, g.dir, "main.lobster", inputs)), "\n"), "\n")
	res := saveResult{}
	for range rows {
		if len(out) < 2 {
			t.Fatalf("unit %s: the program's output ends early", rows[0].unit)
		}
		var hn int
		if _, err := fmt.Sscanf(out[0], "%d", &hn); err != nil || len(out) < 1+hn+1 {
			t.Fatalf("unit %s: the program's output is not hex count, hex, count, dump: %q", rows[0].unit, out[0])
		}
		res.saved = append(res.saved, strings.Join(out[1:1+hn], ""))
		out = out[1+hn:]
		var n int
		if _, err := fmt.Sscanf(out[0], "%d", &n); err != nil || len(out) < 1+n {
			t.Fatalf("unit %s: the program's output is not hex, count, dump: %q", rows[0].unit, out[0])
		}
		res.dump = append(res.dump, strings.Join(out[1:1+n], ""))
		out = out[1+n:]
	}
	if len(out) != 0 {
		t.Fatalf("unit %s: %d lines past the last row", rows[0].unit, len(out))
	}
	return res
}

// TestLobsterTableSaveRoundTripsEveryInstance saves every instance of the
// tables manifest through runtime/schema_save.lobster (docs/SPEC-TABLES.md §3,
// §3.1; docs/PORT-LOBSTER.md, THE RUNTIME LAYOUT) and holds the bytes to the
// Go reference engine's (internal/tablewire.Encode of the instance it
// decoded), then loads the saved bytes back through runtime/load.lobster and
// holds THE VALUE DUMP to the reference's. A row the Go writer refuses by
// name (blob_shared, §2.5) is named and not compared.
func TestLobsterTableSaveRoundTripsEveryInstance(t *testing.T) {
	t.Parallel()
	units, _ := readManifest(t)
	byUnit := map[string][]loadRow{}
	var order []string
	for _, row := range loadRows(t) {
		if row.kind != "instance" {
			continue
		}
		if _, ok := units[row.unit]; !ok {
			t.Fatalf("row %s names unit %s, which the manifest does not declare", row.name, row.unit)
		}
		if _, seen := byUnit[row.unit]; !seen {
			order = append(order, row.unit)
		}
		byUnit[row.unit] = append(byUnit[row.unit], row)
	}
	compared, skipped := 0, 0
	for _, key := range order {
		paths, err := compiler.GatherPaths(prefix(units[key]))
		if err != nil {
			t.Fatalf("unit %s: %v", key, err)
		}
		g, err := generateLoadUnit(t, paths)
		if err != nil {
			t.Logf("refused unit %s (%d rows): %v", key, len(byUnit[key]), err)
			skipped += len(byUnit[key])
			continue
		}
		var rows []loadRow
		var want [][]byte
		for _, row := range byUnit[key] {
			wire := row.data
			if wire == nil {
				if wire, err = os.ReadFile(filepath.Join(root, row.wire)); err != nil {
					t.Fatalf("%s: %v", row.name, err)
				}
			}
			out, goErr := goSave(g, row.root, wire)
			if goErr != nil {
				t.Logf("instance %s is refused by the Go writer and not compared: %v", row.name, goErr)
				skipped++
				continue
			}
			rows = append(rows, row)
			want = append(want, out)
		}
		if len(rows) == 0 {
			continue
		}
		res := runSaveRows(t, g, rows)
		for i, row := range rows {
			wantHex := hex.EncodeToString(want[i])
			if res.saved[i] != wantHex {
				t.Errorf("instance %s: the saved bytes are not the reference's:\nlobster: %s\ngo:      %s", row.name, res.saved[i], wantHex)
				continue
			}
			_, goDump := goLoad(g.u, row.root, want[i])
			if res.dump[i] != goDump {
				t.Errorf("instance %s: the re-loaded value is not the reference's at byte %s", row.name, divergence(res.dump[i], goDump))
				continue
			}
			compared++
		}
	}
	if compared == 0 {
		t.Fatalf("compared %d instances: the comparison is vacuous", compared)
	}
	t.Logf("%d instances saved byte for byte and re-loaded equal to the reference's; %d skipped by name", compared, skipped)
}

// TestLobsterTableSaveNegativeControl patches the emitted save runtime in
// memory — the FORM BYTE line (docs/SPEC-TABLES.md §3), matched exactly once —
// and requires the byte comparison to go red, with the unpatched control
// green.
func TestLobsterTableSaveNegativeControl(t *testing.T) {
	t.Parallel()
	units, _ := readManifest(t)
	var row loadRow
	found := false
	for _, r := range loadRows(t) {
		if r.kind == "instance" && r.name == "root_full" {
			row, found = r, true
			break
		}
	}
	if !found {
		t.Fatalf("the manifest names no instance root_full")
	}
	paths, err := compiler.GatherPaths(prefix(units[row.unit]))
	if err != nil {
		t.Fatal(err)
	}
	g, err := generateLoadUnit(t, paths)
	if err != nil {
		t.Fatal(err)
	}
	wire, err := os.ReadFile(filepath.Join(root, row.wire))
	if err != nil {
		t.Fatal(err)
	}
	want, err := goSave(g, row.root, wire)
	if err != nil {
		t.Fatal(err)
	}
	if got := runSaveRows(t, g, []loadRow{row}).saved[0]; got != hex.EncodeToString(want) {
		t.Fatalf("the control, unpatched: got %s, want %x", got, want)
	}
	path := filepath.Join(g.dir, "schema_save.lobster")
	src, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	const anchor = "    so_u8(out, 1) // THE FORM BYTE\n"
	if n := strings.Count(string(src), anchor); n != 1 {
		t.Fatalf("the form-byte anchor is in the emitted runtime %d times, want 1", n)
	}
	patched := strings.Replace(string(src), anchor, "    so_u8(out, 2) // THE FORM BYTE\n", 1)
	if err := os.WriteFile(path, []byte(patched), 0o644); err != nil {
		t.Fatal(err)
	}
	got := runSaveRows(t, g, []loadRow{row}).saved[0]
	if got == hex.EncodeToString(want) {
		t.Fatalf("the patched runtime still writes the reference's bytes: %s", got)
	}
	if !bytes.HasPrefix([]byte(got), []byte("02")) {
		t.Fatalf("the patched runtime did not write form 2: %s", got)
	}
	t.Logf("the byte comparison went red under the patch: form 2 wrote %s, want %x", got, want)
}
