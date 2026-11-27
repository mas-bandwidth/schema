package goosetable_test

// BLOB TARGETS OUTSIDE THE ROOT'S CLOSURE, GOOSE (docs/SPEC-TABLES.md §3.1,
// §6.5; docs/rulings.json blob-target-outside-closure-refused): the two
// reserved blob ids are types every reader of the variable form can name,
// whatever its root reaches. A blob record named from a pointer slot this
// reader DECLARES whose target is not that blob kind refuses the whole read,
// even when the root's closure never holds an edge of that kind. A blob record
// of a kind the root's closure lacks, named only from a field the reader does
// not declare, is skipped as evolution and read (counted unknown), and a
// record no pointer reaches is damage.
//
// The vectors are testdata/wire/pointer-target-type/blobkind and every row of
// testdata/conformance/feature-witnesses/pointer-targets/cases.json (whose
// rows "table outside root closure", "bytes outside root closure" and "string
// outside root closure" are refusals under that ruling), with the reference
// engine as the oracle for the witness rows. Every row runs through the plain
// and the retaining read, and a read row's report is held to its unknown
// count. Only the verdict is compared; a refusal's reason is logged, never
// compared (docs/rulings.json refusal-verdict-only). The same program saves a
// Text and a Bin value, with the blob null, empty and set, plain and
// retaining, so the leg's own writer is held to never emitting a blob record
// no pointer reaches.
//
// TestGoosePointerTargetBlobKindRefused also carries the negative control: a
// copy of the runtime with the old root-closure rule planted ("can name" a
// blob only when the root's own closure has that edge) must turn
// text-names-bytes red by name.

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v3/compiler"
	"github.com/mas-bandwidth/schema/v3/internal/codegen/goose/gooserun"
	"github.com/mas-bandwidth/schema/v3/internal/codegen/goosetable"
	"github.com/mas-bandwidth/schema/v3/internal/slowtest"
	"github.com/mas-bandwidth/schema/v3/internal/tabletext"
	"github.com/mas-bandwidth/schema/v3/internal/tablewire"
	"github.com/mas-bandwidth/schema/v3/ir"
)

type blobKindCase struct {
	name, unit, root, file, verdict string
	unknown                         int // -1 when the row does not pin one
	wire                            []byte
}

func blobKindCases(t *testing.T) []blobKindCase {
	t.Helper()
	dir := filepath.Join(root, "testdata", "wire", "pointer-target-type", "blobkind")
	path := filepath.Join(dir, "MANIFEST.tsv")
	mf, err := os.Open(path)
	if err != nil {
		t.Fatalf("blob-kind manifest: %v", err)
	}
	defer mf.Close()
	var rows []blobKindCase
	sc := bufio.NewScanner(mf)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		f := strings.Split(line, "\t")
		if len(f) != 6 {
			t.Fatalf("%s: %d fields, want 6", path, len(f))
		}
		row := blobKindCase{name: f[0], unit: f[1], root: f[2], file: f[3], verdict: f[4], unknown: -1}
		if row.verdict != "read" && row.verdict != "refused" {
			t.Fatalf("%s: verdict %q, want read or refused", path, row.verdict)
		}
		if f[5] != "-" {
			n, err := strconv.Atoi(f[5])
			if err != nil {
				t.Fatalf("%s: unknown %q: %v", path, f[5], err)
			}
			row.unknown = n
		}
		if row.wire, err = os.ReadFile(filepath.Join(dir, row.file)); err != nil {
			t.Fatalf("%s: %v", row.file, err)
		}
		rows = append(rows, row)
	}
	if err := sc.Err(); err != nil {
		t.Fatal(err)
	}
	if len(rows) == 0 {
		t.Fatalf("%s names no vectors", path)
	}
	return rows
}

// pointerWitnessUnit is the feature witnesses' unit, as blobKindUnit loads it.
const pointerWitnessUnit = "testdata/conformance/feature-witnesses/pointer-targets"

// pointerWitnessRefused are the rows the blob-target ruling flipped to
// refusals; the engine must refuse each, or the gate is not testing them.
var pointerWitnessRefused = []string{"table outside root closure", "bytes outside root closure", "string outside root closure"}

// pointerWitnessCases are the feature-witness rows as cases the probe reads:
// each row's input saved by the engine at its writer root, and the verdict
// and unknown count the engine's strict and retaining reads give at its
// reader root (they must agree with each other and with the row's counters).
func pointerWitnessCases(t *testing.T, m *tabletext.Model) []blobKindCase {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(root, pointerWitnessUnit, "cases.json"))
	if err != nil {
		t.Fatal(err)
	}
	var rows []struct {
		Name, Writer, Reader string
		Input                json.RawMessage
		KindMismatch         int `json:"kind_mismatch"`
		Unknown              int
	}
	if err := json.Unmarshal(data, &rows); err != nil {
		t.Fatal(err)
	}
	refused := map[string]bool{}
	var out []blobKindCase
	for i, row := range rows {
		writer, reader := m.Lookup(row.Writer), m.Lookup(row.Reader)
		if writer == nil || reader == nil {
			t.Fatalf("%s: no table %q or %q", row.Name, row.Writer, row.Reader)
		}
		v := m.New(writer)
		var r tabletext.Report
		if !m.Read(v, row.Input, &r) || !r.Silent() {
			t.Fatalf("%s: the writer's input does not read: %+v", row.Name, r)
		}
		wire, err := tablewire.Encode(m, v)
		if err != nil {
			t.Fatalf("%s: Encode: %v", row.Name, err)
		}
		var strict, retaining tabletext.Report
		sok, _ := tablewire.Decode(m, m.New(reader), wire, &strict)
		retain := tablewire.Retain{Capacity: 8192, IdCapacity: 1024}
		rok, _ := tablewire.DecodeRetain(m, m.New(reader), wire, &retain, &retaining)
		sRefused, rRefused := !sok || strict.Refused, !rok || retaining.Refused
		if sRefused != rRefused || (!sRefused && strict.Unknown != retaining.Unknown) {
			t.Fatalf("%s: the engine's strict (%+v) and retaining (%+v) reads disagree", row.Name, strict, retaining)
		}
		if sRefused != (row.KindMismatch > 0) || (!sRefused && strict.Unknown != row.Unknown) {
			t.Fatalf("%s: the engine answers %+v, cases.json says kind_mismatch %d unknown %d", row.Name, strict, row.KindMismatch, row.Unknown)
		}
		c := blobKindCase{
			name: fmt.Sprintf("witness-%d", i+1), unit: pointerWitnessUnit, root: row.Reader,
			file: fmt.Sprintf("witness-%d.bin", i+1), verdict: "read", unknown: strict.Unknown, wire: wire,
		}
		if sRefused {
			c.verdict, c.unknown = "refused", -1
			refused[row.Name] = true
		}
		out = append(out, c)
	}
	for _, name := range pointerWitnessRefused {
		if !refused[name] {
			t.Fatalf("cases.json row %q is not a refusal the engine gives", name)
		}
	}
	return out
}

// blobKindProbe is the program's per-row body: the plain and retaining reads
// of one wire, each printing its verdict and either the refusal's reason
// (logged, never compared) or its unknown count and whether the root's first
// field is null.
const blobKindProbe = `
fn blob_kind_case(name: const u8[:], file: const u8[:], root_name: const u8[:]) {
    let data = input(file);
    var vals: schema_rt::LoadVal[>..] = [];
    var nodes: schema_rt::LoadNode[>..] = [];
    var report = schema_rt::LoadReport { .. };
    let root = schema_rt::load_table(conformance_unit(), root_name, data, vals, nodes, report);
    if root >= 0 && !report.refused {
        let slot = vals[root].first;
        let is_null = vals[slot].bits.lo == 0 && vals[slot].bits.hi == 0 && vals[slot].state != schema_rt::ValPresent;
        write_stdout(str(name, " plain read ", report.unknown, " ", if is_null { "null" } else { "present" }, "\n"));
    } else {
        write_stdout(str(name, " plain refused ", schema_rt::load_refusal_name(schema_rt::load_error(report, root)), "_\n"));
    }
    var rvals: schema_rt::LoadVal[>..] = [];
    var rnodes: schema_rt::LoadNode[>..] = [];
    var rreport = schema_rt::LoadReport { .. };
    var rbytes: u8[>..<] = [];
    var rrecs: schema_rt::RetainRec[>..<] = [];
    var rids: u64[>..<] = [];
    var store = schema_rt::RetainStore { capacity: 65536, id_capacity: 1024, used: 0, bytes: rbytes, recs: rrecs, ids: rids };
    let rroot = schema_rt::load_table_retain(conformance_unit(), root_name, data, rvals, rnodes, rreport, store);
    if rroot >= 0 && !rreport.refused {
        let slot = rvals[rroot].first;
        let is_null = rvals[slot].bits.lo == 0 && rvals[slot].bits.hi == 0 && rvals[slot].state != schema_rt::ValPresent;
        write_stdout(str(name, " retain read ", rreport.unknown, " ", if is_null { "null" } else { "present" }, "\n"));
    } else {
        write_stdout(str(name, " retain refused ", schema_rt::load_refusal_name(schema_rt::load_error(rreport, rroot)), "_\n"));
    }
}
`

// blobKindSaveProbe is the writer half, kept apart so a read-only run never
// carries an unused function the compiler still type-checks.
const blobKindSaveProbe = `
fn blob_kind_save(name: const u8[:], file: const u8[:], root_name: const u8[:]) {
    let data = input(file);
    var vals: schema_rt::LoadVal[>..] = [];
    var nodes: schema_rt::LoadNode[>..] = [];
    var report = schema_rt::LoadReport { .. };
    let root = schema_rt::load_table(conformance_unit(), root_name, data, vals, nodes, report);
    if root < 0 || report.refused {
        write_stdout(str(name, " save-plain refused\n"));
    } else {
        var out: u8[>..<] = [];
        let refusal = schema_rt::save_table(conformance_unit(), root_name, data, vals, nodes, root, out);
        if refusal == 0 { write_stdout(str(name, " save-plain ", hex(out), "\n")); }
        else { write_stdout(str(name, " save-plain refused\n")); }
    }
    var rvals: schema_rt::LoadVal[>..] = [];
    var rnodes: schema_rt::LoadNode[>..] = [];
    var rreport = schema_rt::LoadReport { .. };
    var rbytes: u8[>..<] = [];
    var rrecs: schema_rt::RetainRec[>..<] = [];
    var rids: u64[>..<] = [];
    var store = schema_rt::RetainStore { capacity: 65536, id_capacity: 1024, used: 0, bytes: rbytes, recs: rrecs, ids: rids };
    let rroot = schema_rt::load_table_retain(conformance_unit(), root_name, data, rvals, rnodes, rreport, store);
    if rroot < 0 || rreport.refused {
        write_stdout(str(name, " save-retain refused\n"));
    } else {
        var out: u8[>..<] = [];
        let refusal = schema_rt::save_table_retain(conformance_unit(), root_name, data, rvals, rnodes, rroot, store, rreport, out);
        if refusal == 0 { write_stdout(str(name, " save-retain ", hex(out), "\n")); }
        else { write_stdout(str(name, " save-retain refused\n")); }
    }
}
`

// blobKindAnswer is one read entry's answer for one vector.
type blobKindAnswer struct {
	verdict, reason string
	unknown         int
	null            bool
}

// blobKindSave is one value the leg's own writer must save: a root, a name,
// and the wire the engine writes for it.
type blobKindSave struct {
	name, root string
	wire       []byte
}

func blobKindUnit(t *testing.T, unit string) *ir.Unit {
	t.Helper()
	gathered, err := compiler.GatherPaths(prefix([]string{unit}))
	if err != nil {
		t.Fatalf("%s: %v", unit, err)
	}
	u, err := compiler.New().Load(gathered)
	if err != nil {
		t.Fatalf("%s does not load: %v", unit, err)
	}
	return u
}

// blobKindRun generates u for Goose, reads every row, saves every value, and
// answers both sets of printed answers.
func blobKindRun(t *testing.T, u *ir.Unit, rows []blobKindCase, saves []blobKindSave, override map[string][]byte) (map[string]blobKindAnswer, map[string][]byte) {
	t.Helper()
	files := probeFiles(t, u, override)
	var main strings.Builder
	main.WriteString(probePrelude)
	main.WriteString(blobKindProbe)
	if len(saves) > 0 {
		main.WriteString(blobKindSaveProbe)
	}
	main.WriteString("\nfn main() {\n")
	for _, row := range rows {
		files[row.file] = row.wire
		fmt.Fprintf(&main, "    blob_kind_case(%q, %q, %q);\n", row.name, row.file, row.root)
	}
	for _, s := range saves {
		files[s.name] = s.wire
		fmt.Fprintf(&main, "    blob_kind_save(%q, %q, %q);\n", s.name, s.name, s.root)
	}
	main.WriteString("}\n")
	files["main.goose"] = []byte(main.String())
	out := gooserun.Run(t, t.TempDir(), "main.goose", files)
	return parseBlobKind(t, out)
}

func parseBlobKind(t *testing.T, out []byte) (map[string]blobKindAnswer, map[string][]byte) {
	t.Helper()
	reads := map[string]blobKindAnswer{}
	saves := map[string][]byte{}
	for line := range strings.SplitSeq(strings.TrimRight(string(out), "\n"), "\n") {
		f := strings.Fields(line)
		switch {
		case len(f) == 4 && (f[1] == "plain" || f[1] == "retain") && f[2] == "refused" && strings.HasSuffix(f[3], "_"):
			reads[f[0]+" "+f[1]] = blobKindAnswer{verdict: "refused", reason: strings.TrimSuffix(f[3], "_")}
		case len(f) == 5 && (f[1] == "plain" || f[1] == "retain") && f[2] == "read":
			n, err := strconv.Atoi(f[3])
			if err != nil {
				t.Fatalf("%s: %q is not a count", line, f[3])
			}
			reads[f[0]+" "+f[1]] = blobKindAnswer{verdict: "read", unknown: n, null: f[4] == "null"}
		case len(f) == 3 && (f[1] == "save-plain" || f[1] == "save-retain") && f[2] != "refused":
			saves[f[0]+" "+f[1]] = probeHex(t, f[2])
		case len(f) == 3 && (f[1] == "save-plain" || f[1] == "save-retain") && f[2] == "refused":
			// a refused save leaves no wire; the caller reports it by name
		default:
			t.Fatalf("the probe printed %q", line)
		}
	}
	return reads, saves
}

// blobKindProblems holds each row's answers to its verdict (and a read row to
// its unknown count). A refusal's reason is not compared: only the verdict
// is held across legs (docs/rulings.json refusal-verdict-only).
func blobKindProblems(rows []blobKindCase, answers map[string]blobKindAnswer) []string {
	var problems []string
	for _, row := range rows {
		for _, entry := range []string{"plain", "retain"} {
			answer, ok := answers[row.name+" "+entry]
			if !ok {
				problems = append(problems, fmt.Sprintf("%s %s: the probe printed no answer", row.name, entry))
				continue
			}
			if answer.verdict != row.verdict {
				problems = append(problems, fmt.Sprintf("%s %s: verdict %q, the manifest says %q", row.name, entry, answer.verdict, row.verdict))
				continue
			}
			if row.verdict != "read" {
				continue
			}
			if row.unknown >= 0 && answer.unknown != row.unknown {
				problems = append(problems, fmt.Sprintf("%s %s: unknown %d, the manifest says %d", row.name, entry, answer.unknown, row.unknown))
			}
			if row.name == "text-unknown-field-bytes" && !answer.null {
				problems = append(problems, fmt.Sprintf("%s %s: the pointer did not read null", row.name, entry))
			}
		}
	}
	return problems
}

// blobKindWriters is the engine's own save of each root, the blob null, empty
// and set: the values the leg's writer must save without an unreachable
// record.
func blobKindWriters(t *testing.T, m *tabletext.Model) []blobKindSave {
	t.Helper()
	var out []blobKindSave
	for _, c := range []struct{ root, field string }{{"Text", "body"}, {"Bin", "data"}} {
		for _, v := range []struct {
			tag  string
			data []byte
		}{{"null", nil}, {"empty", []byte{}}, {"set", []byte("hello")}} {
			inst := m.New(m.Lookup(c.root))
			if v.data != nil {
				fv, ok := inst.FieldByKey(c.field)
				if !ok {
					t.Fatalf("%s has no field %s", c.root, c.field)
				}
				fv.Cell.Blob = &tabletext.Blob{Data: v.data}
			}
			wire, err := tablewire.Encode(m, inst)
			if err != nil {
				t.Fatalf("%s.%s=%q: Encode: %v", c.root, c.field, v.data, err)
			}
			out = append(out, blobKindSave{name: strings.ToLower(c.root) + "-" + v.tag + ".bin", root: c.root, wire: wire})
		}
	}
	return out
}

// blobKindCleanReadBack is the engine's strict and retaining read of a wire
// the leg's writer saved: a conforming save is accepted with nothing unknown.
func blobKindCleanReadBack(m *tabletext.Model, root string, wire []byte) []string {
	var problems []string
	var report tabletext.Report
	ok, err := tablewire.Decode(m, m.New(m.Lookup(root)), wire, &report)
	if !ok || err != nil || report.Refused || report.Unknown != 0 {
		problems = append(problems, fmt.Sprintf("(strict) %v %+v", err, report))
	}
	retain := tablewire.Retain{Capacity: 8192, IdCapacity: 1024}
	var rreport tabletext.Report
	rok, rerr := tablewire.DecodeRetain(m, m.New(m.Lookup(root)), wire, &retain, &rreport)
	if !rok || rerr != nil || rreport.Refused || rreport.Unknown != 0 {
		problems = append(problems, fmt.Sprintf("(retaining) %v %+v", rerr, rreport))
	}
	return problems
}

// blobKindOldRule copies the runtime and plants the old rule: a blob id is
// named only when the root's own pointer closure holds that edge, so a
// wrong-kind blob reads as an unknown record and the pointer null.
func blobKindOldRule(t *testing.T) []byte {
	t.Helper()
	src, ok := goosetable.Runtime()["wire_load.goose"]
	if !ok {
		t.Fatal("the runtime has no wire_load.goose")
	}
	text := string(src)
	for _, plant := range []struct{ want, with string }{
		{
			"        if type_id == StringTypeId {\n            c.nodes[k].blob = KindString;",
			"        if type_id == StringTypeId {\n            if c.unit.tables[root].place_str { c.nodes[k].blob = KindString; }",
		},
		{
			"        if type_id == BytesTypeId {\n            c.nodes[k].blob = KindArray;",
			"        if type_id == BytesTypeId {\n            if c.unit.tables[root].place_bytes { c.nodes[k].blob = KindArray; }",
		},
	} {
		if !strings.Contains(text, plant.want) {
			t.Fatalf("the runtime no longer carries the line the old rule replaces: %q", plant.want)
		}
		text = strings.Replace(text, plant.want, plant.with, 1)
	}
	return []byte(text)
}

// logBlobKindReasons logs each refusal's reason, for the reader; the gate
// compares verdicts only.
func logBlobKindReasons(t *testing.T, rows []blobKindCase, answers map[string]blobKindAnswer) {
	t.Helper()
	for _, row := range rows {
		for _, entry := range []string{"plain", "retain"} {
			if a, ok := answers[row.name+" "+entry]; ok && a.verdict == "refused" {
				t.Logf("%s %s: refused, reason %q", row.name, entry, a.reason)
			}
		}
	}
}

func TestGoosePointerTargetBlobKindRefused(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "the Goose toolchain (it builds and runs the generated reader)")

	rows := blobKindCases(t)
	byUnit := map[string][]blobKindCase{}
	for _, row := range rows {
		byUnit[row.unit] = append(byUnit[row.unit], row)
	}

	for unit, unitRows := range byUnit {
		u := blobKindUnit(t, unit)
		m := tabletext.NewModel(u)
		saves := blobKindWriters(t, m)
		answers, saved := blobKindRun(t, u, unitRows, saves, nil)
		logBlobKindReasons(t, unitRows, answers)
		for _, p := range blobKindProblems(unitRows, answers) {
			t.Error(p)
		}
		for _, s := range saves {
			for _, entry := range []string{"save-plain", "save-retain"} {
				wire, ok := saved[s.name+" "+entry]
				if !ok {
					t.Errorf("%s %s: the probe printed no saved wire", s.name, entry)
					continue
				}
				for _, p := range blobKindCleanReadBack(m, s.root, wire) {
					t.Errorf("%s %s: the leg's own save does not read back clean: %s", s.name, entry, p)
				}
			}
		}
		// The probe must really write the blob: a save with the blob empty or
		// set differs from the save with it null, on both save paths.
		for _, r := range []string{"text", "bin"} {
			for _, entry := range []string{"save-plain", "save-retain"} {
				null := saved[r+"-null.bin "+entry]
				for _, tag := range []string{"empty", "set"} {
					if got, ok := saved[r+"-"+tag+".bin "+entry]; ok && bytes.Equal(got, null) {
						t.Errorf("%s-%s %s: the save is the null save's bytes, so the blob was not written", r, tag, entry)
					}
				}
			}
		}
	}

	t.Run("feature witnesses, the engine as oracle", func(t *testing.T) {
		u := blobKindUnit(t, pointerWitnessUnit)
		witness := pointerWitnessCases(t, tabletext.NewModel(u))
		answers, _ := blobKindRun(t, u, witness, nil, nil)
		logBlobKindReasons(t, witness, answers)
		for _, p := range blobKindProblems(witness, answers) {
			t.Error(p)
		}
	})

	t.Run("negative control: the old root-closure rule reads text-names-bytes", func(t *testing.T) {
		const unit = "test/pointer-target-type/blobkind/reader"
		u := blobKindUnit(t, unit)
		answers, _ := blobKindRun(t, u, rows, nil, map[string][]byte{"wire_load.goose": blobKindOldRule(t)})
		problems := blobKindProblems(rows, answers)
		named := false
		for _, p := range problems {
			if strings.HasPrefix(p, "text-names-bytes ") {
				named = true
				break
			}
		}
		if !named {
			t.Errorf("the planted root-closure rule did not turn text-names-bytes red; problems: %v", problems)
		}
	})
}
