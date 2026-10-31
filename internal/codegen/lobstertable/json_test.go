package lobstertable_test

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/compiler"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/lobster"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/lobster/lobsterrun"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// jsonRow is one instance row (with text) or one json-hostile row of the
// tables conformance manifest.
type jsonRow struct {
	kind, name, unit, root string
	// wire is an instance's wire file; tree a hostile row's directory
	wire, tree string
	// verdict is a hostile row's standard verdict, and carried the ports
	// that carry it (empty: every port)
	verdict, uncarried string
	carried            []string
}

// lobsterVerdict is the verdict the Lobster leg owes a hostile row.
func (r jsonRow) lobsterVerdict() string {
	if len(r.carried) == 0 {
		return r.verdict
	}
	if slices.Contains(r.carried, "lobster") {
		return r.verdict
	}
	return r.uncarried
}

func jsonRows(t *testing.T) []jsonRow {
	t.Helper()
	f, err := os.Open(filepath.Join(root, "testdata", "conformance", "tables", "MANIFEST.txt"))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	var rows []jsonRow
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		x := strings.Fields(sc.Text())
		switch {
		case len(x) >= 5 && x[0] == "instance":
			if len(x) > 5 && x[5] == "no-text" {
				continue
			}
			rows = append(rows, jsonRow{kind: "instance", name: x[1], unit: x[2], root: x[3], wire: x[4]})
		case len(x) >= 6 && x[0] == "json-hostile":
			r := jsonRow{kind: "json-hostile", name: x[1], unit: x[2], root: x[3], tree: x[4], verdict: x[5]}
			if len(x) >= 8 {
				r.carried = strings.Split(strings.TrimPrefix(x[6], "carried-by="), ",")
				r.uncarried = strings.TrimPrefix(x[7], "uncarried=")
			}
			rows = append(rows, r)
		}
	}
	if err := sc.Err(); err != nil {
		t.Fatal(err)
	}
	return rows
}

// jsonProgram is the main for one unit: for instance i it writes the JSON
// text of the loaded wire w<i> to tw<i> and the wire Save writes from the
// golden text j<i> to tr<i>; for hostile row i it prints `H<i> <verdict>`.
func jsonProgram(g *loadUnit, rows []jsonRow) string {
	var b strings.Builder
	for _, m := range g.mods {
		fmt.Fprintf(&b, "import .%s\n", m)
	}
	b.WriteString("import .load\nimport .schema_save\nimport .strict_and_refusals\nimport .json\n")
	b.WriteString(`def instance(root:string, i:int):
    let rep = schema_rt.LoadReport {}
    let wire = read_file("w{i}")
    assert wire
    let rec = schema_rt.load_tolerant(root, wire, rep)
    assert rec
    let text, why = schema_rt.json_write(rec)
    if why != "":
        print("E{i} write: " + why)
    else:
        assert write_file("tw{i}", text)
    let golden = read_file("j{i}")
    assert golden
    let rrep = schema_rt.LoadReport {}
    let back = schema_rt.json_read(root, golden, rrep)
    if back:
        let out, reason = schema_rt.save_reasoned(back)
        if reason != "":
            print("E{i} save: " + reason)
        else:
            assert write_file("tr{i}", out)
    else:
        print("E{i} read: " + rrep.reason)
def hostile(root:string, i:int):
    let rep = schema_rt.LoadReport {}
    let text = read_file("h{i}")
    assert text
    let rec = schema_rt.json_read(root, text, rep)
    if rec and not rep.malformed:
        print("H{i} {rep.unknown},{rep.kind_mismatch},{rep.widened},{rep.clamped},{rep.duplicate},false,read")
    else:
        print("H{i} refused")
`)
	for i, r := range rows {
		if r.kind == "instance" {
			fmt.Fprintf(&b, "instance(%s, %d)\n", lobster.StringLit([]byte(r.root)), i)
		} else {
			fmt.Fprintf(&b, "hostile(%s, %d)\n", lobster.StringLit([]byte(r.root)), i)
		}
	}
	return b.String()
}

// jsonResult is one unit's answers: the written text and read-back wire per
// instance, the verdict per hostile row, and the program's error lines.
type jsonResult struct {
	written, saved map[int][]byte
	verdict        map[int]string
	errs           []string
}

func runJsonUnit(t *testing.T, g *loadUnit, rows []jsonRow) jsonResult {
	t.Helper()
	inputs := map[string][]byte{}
	for i, r := range rows {
		if r.kind == "instance" {
			wire, err := os.ReadFile(filepath.Join(root, r.wire))
			if err != nil {
				t.Fatal(err)
			}
			text, err := os.ReadFile(filepath.Join(root, "testdata", "conformance", "tables", "json", r.name+".json"))
			if err != nil {
				t.Fatal(err)
			}
			inputs[fmt.Sprintf("w%d", i)] = wire
			inputs[fmt.Sprintf("j%d", i)] = text
		} else {
			text, err := os.ReadFile(filepath.Join(root, r.tree, r.root+".json"))
			if err != nil {
				t.Fatal(err)
			}
			inputs[fmt.Sprintf("h%d", i)] = text
		}
		for _, p := range []string{"tw", "tr"} {
			_ = os.Remove(filepath.Join(g.dir, fmt.Sprintf("%s%d", p, i)))
		}
	}
	if err := os.WriteFile(filepath.Join(g.dir, "main.lobster"), []byte(jsonProgram(g, rows)), 0o644); err != nil {
		t.Fatal(err)
	}
	out := lobsterrun.Run(t, g.dir, "main.lobster", inputs)
	res := jsonResult{written: map[int][]byte{}, saved: map[int][]byte{}, verdict: map[int]string{}}
	for line := range strings.SplitSeq(strings.TrimRight(string(out), "\n"), "\n") {
		if line == "" {
			continue
		}
		var i int
		var v string
		if _, err := fmt.Sscanf(line, "H%d %s", &i, &v); err == nil && strings.HasPrefix(line, "H") {
			res.verdict[i] = v
			continue
		}
		if strings.HasPrefix(line, "E") {
			res.errs = append(res.errs, line)
			continue
		}
		t.Fatalf("unit %s: the program printed %q", rows[0].unit, line)
	}
	for i, r := range rows {
		if r.kind != "instance" {
			continue
		}
		if b, err := os.ReadFile(filepath.Join(g.dir, fmt.Sprintf("tw%d", i))); err == nil {
			res.written[i] = b
		}
		if b, err := os.ReadFile(filepath.Join(g.dir, fmt.Sprintf("tr%d", i))); err == nil {
			res.saved[i] = b
		}
	}
	return res
}

// goJsonWrite is the reference engine's text of a wire: Decode, then Write,
// closed by the newline every port's ToJson closes it with.
func goJsonWrite(t *testing.T, u *ir.Unit, rootName string, wire []byte) []byte {
	t.Helper()
	m := tabletext.NewModel(u)
	inst := m.New(m.Lookup(rootName))
	var rep tabletext.Report
	if _, err := tablewire.Decode(m, inst, wire, &rep); err != nil {
		t.Fatalf("%s: the reference refuses the wire: %v", rootName, err)
	}
	text, err := m.Write(inst)
	if err != nil {
		t.Fatalf("%s: the reference refuses the text: %v", rootName, err)
	}
	return append(text, '\n')
}

// goJsonVerdict is the reference engine's hostile verdict for one text.
func goJsonVerdict(u *ir.Unit, rootName string, text []byte) string {
	m := tabletext.NewModel(u)
	inst := m.New(m.Lookup(rootName))
	var rep tabletext.Report
	if !m.Read(inst, text, &rep) || rep.Malformed {
		return "refused"
	}
	return fmt.Sprintf("%d,%d,%d,%d,%d,false,read", rep.Unknown, rep.KindMismatch, rep.Widened, rep.Clamped, rep.Duplicate)
}

// jsonUnits groups the rows by unit, in manifest order, and generates each.
func jsonUnits(t *testing.T) ([]string, map[string][]jsonRow, map[string]*loadUnit) {
	t.Helper()
	units, _ := readManifest(t)
	byUnit := map[string][]jsonRow{}
	var order []string
	for _, r := range jsonRows(t) {
		if _, seen := byUnit[r.unit]; !seen {
			order = append(order, r.unit)
		}
		byUnit[r.unit] = append(byUnit[r.unit], r)
	}
	gen := map[string]*loadUnit{}
	for _, key := range order {
		paths, err := compiler.GatherPaths(prefix(units[key]))
		if err != nil {
			t.Fatalf("unit %s: %v", key, err)
		}
		g, err := generateLoadUnit(t, paths)
		if err != nil {
			t.Fatalf("unit %s: the Lobster table surface refuses it: %v", key, err)
		}
		gen[key] = g
	}
	return order, byUnit, gen
}

// TestLobsterJsonMatchesTheReference holds runtime/json.lobster to the Go
// reference engine (internal/tabletext) over the tables conformance manifest
// under the pinned toolchain (docs/SPEC-TABLES.md §16; docs/PORT-LOBSTER.md,
// THE HARNESS). For every instance with a text: ToJson of the loaded wire is
// byte for byte tabletext's Write and the golden json/<name>.json, and
// FromJson of the golden text then Save is the wire golden. For every
// json-hostile row: the verdict is tabletext's Read verdict and the one the
// manifest says the Lobster leg owes.
func TestLobsterJsonMatchesTheReference(t *testing.T) {
	t.Parallel()
	order, byUnit, gen := jsonUnits(t)
	instances, hostiles := 0, 0
	for _, key := range order {
		g, rows := gen[key], byUnit[key]
		res := runJsonUnit(t, g, rows)
		for _, e := range res.errs {
			t.Errorf("unit %s: %s", key, e)
		}
		for i, r := range rows {
			if r.kind == "instance" {
				wire, _ := os.ReadFile(filepath.Join(root, r.wire))
				golden, _ := os.ReadFile(filepath.Join(root, "testdata", "conformance", "tables", "json", r.name+".json"))
				want := goJsonWrite(t, g.u, r.root, wire)
				if string(want) != string(golden) {
					t.Fatalf("%s: the reference's text is not the golden; the comparison is not grounded", r.name)
				}
				if got := res.written[i]; string(got) != string(want) {
					t.Errorf("json-write %s: the text is not the reference's at byte %s", r.name, divergence(string(got), string(want)))
					continue
				}
				if got := res.saved[i]; string(got) != string(wire) {
					t.Errorf("json-read %s: the saved wire is not the golden (%d bytes, want %d)", r.name, len(got), len(wire))
					continue
				}
				instances++
				continue
			}
			text, _ := os.ReadFile(filepath.Join(root, r.tree, r.root+".json"))
			goV := goJsonVerdict(g.u, r.root, text)
			got := res.verdict[i]
			if got != goV {
				t.Errorf("json-hostile %s: lobster %s, the reference %s", r.name, got, goV)
				continue
			}
			if got != r.lobsterVerdict() {
				t.Errorf("json-hostile %s: lobster %s, the manifest owes %s", r.name, got, r.lobsterVerdict())
				continue
			}
			hostiles++
		}
	}
	if instances == 0 || hostiles == 0 {
		t.Fatalf("%d instances and %d hostile rows compared: the comparison is vacuous", instances, hostiles)
	}
	t.Logf("%d instances written and read back as the reference does; %d hostile verdicts equal the reference's", instances, hostiles)
}

// patchJson rewrites the emitted json.lobster of a generated unit, the anchor
// matched exactly once.
func patchJson(t *testing.T, g *loadUnit, anchor, with string) {
	t.Helper()
	path := filepath.Join(g.dir, "json.lobster")
	src, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if n := strings.Count(string(src), anchor); n != 1 {
		t.Fatalf("the anchor %q is in the emitted runtime %d times, want 1", anchor, n)
	}
	if err := os.WriteFile(path, []byte(strings.Replace(string(src), anchor, with, 1)), 0o644); err != nil {
		t.Fatal(err)
	}
}

// TestLobsterJsonNegativeControl is the reversed witness: it patches the
// emitted runtime in memory and requires the comparison to go red on exactly
// the rows the patched rule decides, and nowhere else.
//
// READ: the one line that counts a repeated TABLE key (§16.2) stops counting,
// so exactly the hostile rows whose duplicate comes from a table object's key
// change verdict — every row the reference counts a duplicate for, less the
// rows whose repeat is an enum-keyed object's slot key (keyed-*,
// dup-key-rejected-keyed) or a map's key (*map-dup-key), each of which counts
// on its own line.
//
// WRITE: the `&node` label's spelling (§16.7) changes, so exactly the
// instances whose golden text carries a label go red.
func TestLobsterJsonNegativeControl(t *testing.T) {
	t.Parallel()
	order, byUnit, gen := jsonUnits(t)
	for _, key := range order {
		patchJson(t, gen[key], "            if seen[index]:\n                r.report.duplicate++\n", "            if seen[index]:\n                r.report.duplicate += 0\n")
		patchJson(t, gen[key], `        j_raw(w, "\"&node\": " + number_to_string(x.label, 10, 1))
        j_line(w, depth)`, `        j_raw(w, "\"&nod\": " + number_to_string(x.label, 10, 1))
        j_line(w, depth)`)
	}
	var wantRed, gotRed []string
	for _, key := range order {
		g, rows := gen[key], byUnit[key]
		res := runJsonUnit(t, g, rows)
		for i, r := range rows {
			if r.kind == "instance" {
				golden, _ := os.ReadFile(filepath.Join(root, "testdata", "conformance", "tables", "json", r.name+".json"))
				// only the REFERENCE spelling is patched, so a text goes red
				// exactly when it refers back to a shared node
				if referenceCount(string(golden)) > 0 {
					wantRed = append(wantRed, "write "+r.name)
				}
				if string(res.written[i]) != string(golden) {
					gotRed = append(gotRed, "write "+r.name)
				}
				continue
			}
			text, _ := os.ReadFile(filepath.Join(root, r.tree, r.root+".json"))
			goV := goJsonVerdict(g.u, r.root, text)
			if goV != "refused" && !strings.Contains(r.name, "keyed") && !strings.Contains(r.name, "map-dup-key") && strings.Split(goV, ",")[4] != "0" {
				wantRed = append(wantRed, "hostile "+r.name)
			}
			if res.verdict[i] != goV {
				gotRed = append(gotRed, "hostile "+r.name)
			}
		}
	}
	sort.Strings(wantRed)
	sort.Strings(gotRed)
	if len(wantRed) == 0 {
		t.Fatal("the control names no row: it is vacuous")
	}
	if strings.Join(gotRed, "\n") != strings.Join(wantRed, "\n") {
		t.Fatalf("the patched runtime went red on\n%s\nwant exactly\n%s", strings.Join(gotRed, "\n"), strings.Join(wantRed, "\n"))
	}
	t.Logf("the patched runtime went red on exactly its %d rows: %s", len(gotRed), strings.Join(gotRed, ", "))
}

// referenceCount is the number of `&node` REFERENCES in a written text: a
// label object holding nothing but its label.
func referenceCount(text string) int {
	n := 0
	for i := 0; ; {
		at := strings.Index(text[i:], `"&node": `)
		if at < 0 {
			return n
		}
		i += at + len(`"&node": `)
		j := i
		for j < len(text) && text[j] >= '0' && text[j] <= '9' {
			j++
		}
		rest := strings.TrimLeft(text[j:], " \n")
		if strings.HasPrefix(rest, "}") {
			n++
		}
	}
}
