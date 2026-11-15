package pythontable

// WIDENING ON READ for the Python table runtime (docs/SPEC-TABLES.md §4;
// docs/PORT-PYTHON.md, THE TABLE FRAME), held to the reference engine's read
// (internal/tablewire Decode and Encode), which shares no code with the
// generated runtime:
//
//	ladder    every integer declaration the runtime carries (the ten widths,
//	          a ranged int64 and a bits field) under every scalar wire kind,
//	          each with a payload whose top bit is set and one that is small,
//	          through both code shapes (the straight-line pair and the
//	          descriptor walk) and both pinned interpreters: the one read's
//	          verdict, value, widened count and saved bytes are the
//	          reference's; a refused read leaves the caller's report and
//	          buffer exactly as they were
//	manifest  every corpus row of testdata/conformance/tables that exercises
//	          widening is read under the reference's verdict (reports.txt), or
//	          skipped by name, NOT proven, when its unit gives exactly its
//	          expected "not carried yet" refusal; the held and skipped counts
//	          are asserted, and any other refusal fails
//
// TestPythonVtWideningNegativeControl breaks the runtime's widen.py in a copy,
// one rule at a time, and requires the ladder to go red on cells that name
// the broken rule.

import (
	"bufio"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"maps"
	"math"
	"math/big"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/check"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/python/pyrun"
	"github.com/mas-bandwidth/schema/v2/internal/parser"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// widenDecls are the scalar declarations the runtime carries.
var widenDecls = []string{
	"int8", "int16", "int32", "int64",
	"int128 | min = -170141183460469231731687303715884105728, max = 170141183460469231731687303715884105727",
	"uint8", "uint16", "uint32", "uint64", "uint128",
	// a widened value is held to the reader's range, and its sign is the
	// WIRE kind's, never the range's
	"int64 | min = 0, max = 1000",
	// bits(N) grown across a storage width: kind 7, so kind 6 widens into it
	"bits(12)",
	// the float rung: kind 10 into kind 11, exact, and a non-finite source
	// refuses non_finite_float (docs/SPEC-TABLES.md §3.3, §4)
	"float64",
}

// widenWireKinds are the scalar wire kinds a cell rides under: the integer
// ladders, the bool, the floats, a fixed-point kind and the timestamp.
var widenWireKinds = []byte{1, 2, 3, 4, 5, 18, 6, 7, 8, 9, 19, 10, 11, 20, 36}

var widenWidth = map[byte]int{1: 1, 2: 1, 3: 2, 4: 4, 5: 8, 18: 16, 6: 1, 7: 2, 8: 4, 9: 8, 19: 16, 10: 4, 11: 8, 20: 1, 36: 8}

// widenPayloads is a payload under kind whose top bit is set, so a widening
// that extends by the wrong signedness is seen, and one that is small, so a
// widened value lands inside a narrow declared range.
func widenPayloads(kind byte) map[string][]byte {
	switch kind {
	case 1:
		return map[string][]byte{"true": {1}}
	case 10:
		return map[string][]byte{
			"neg": binary.LittleEndian.AppendUint32(nil, math.Float32bits(-1.5)),
			"nan": binary.LittleEndian.AppendUint32(nil, 0x7FC00000),
			"inf": binary.LittleEndian.AppendUint32(nil, 0x7F800000),
		}
	case 11:
		return map[string][]byte{
			"neg": binary.LittleEndian.AppendUint64(nil, math.Float64bits(-2.25)),
			"nan": binary.LittleEndian.AppendUint64(nil, 0x7FF8000000000000),
			"inf": binary.LittleEndian.AppendUint64(nil, 0x7FF0000000000000),
		}
	}
	w := widenWidth[kind]
	top := make([]byte, w)
	for i := range top {
		top[i] = 0xFF
	}
	top[0] = 0xFD
	small := make([]byte, w)
	small[0] = 5
	return map[string][]byte{"top": top, "small": small}
}

// widenOneFieldWire is a form-1 file of one root field: the form byte, the
// field's reference, kind and payload, the terminator, and the id table.
func widenOneFieldWire(fieldID uint64, kind byte, payload []byte) []byte {
	wire := append([]byte{1, 1, kind}, payload...)
	wire = append(wire, 0)
	wire = binary.LittleEndian.AppendUint64(wire, fieldID)
	return binary.LittleEndian.AppendUint64(wire, 1)
}

type widenCell struct {
	name string
	wire []byte
}

// widenShapes are the two code shapes a Root takes: all scalars, so the
// straight-line pair, and a pointer beside them, so the descriptor walk. The
// bool beside the integer keeps the straight-line load's typed branches under
// the analyzer.
var widenShapes = []struct{ name, tail string }{
	{"straight", "    flag  bool\n"},
	{"walk", "    flag  bool\n    next  *Root\n"},
}

func widenProbe(n int) string {
	return pyrun.ImportPrelude + fmt.Sprintf(`from probe import LoadReport, Refused, Root

for i in range(%d):
    with open("w%%d" %% i, "rb") as f:
        data = bytearray(f.read())
    before = bytes(data)
    report = LoadReport()
    report.unknown = report.widened = report.retained = report.retain_lost = 7
    try:
        value = Root.load(data, report)
    except Refused:
        print("refused", report.unknown, report.widened, report.retained, report.retain_lost, bytes(data) == before)
        continue
    saved = value.save()
    print("read", value.value, report.widened, report.unknown, saved.hex(), value.measure() == len(saved), bytes(data) == before)
`, n)
}

// widenRefValue is the reference's value of Root.value.
func widenRefValue(inst *tabletext.Instance) string {
	c := inst.Fields[0].Cell
	f := inst.Fields[0].Def
	switch {
	case ir.TableWireFieldKind(f) == 18 || ir.TableWireFieldKind(f) == 19:
		if c.Wide == nil {
			return "0"
		}
		return c.Wide.String()
	case f.Type.Kind == ir.TFloat32 || f.Type.Kind == ir.TFloat64:
		return fmt.Sprint(c.F)
	case f.Type.Kind == ir.TInt && f.Type.Signed:
		return fmt.Sprint(c.I)
	}
	return new(big.Int).SetUint64(c.U).String()
}

type widenRun struct {
	faults  map[string]string
	widened int
	cells   int
}

// widenLadder runs every cell under one declaration and one shape, with
// mutate applied to the generated files when set, under the current pin and,
// when floor is set, the floor pin as well.
func widenLadder(t *testing.T, decl string, shape int, mutate func(*testing.T, map[string][]byte), floor, analyze bool) widenRun {
	t.Helper()
	src := fmt.Sprintf("package probe\n\ntable Root\n{\n    value %s\n%s}\n", decl, widenShapes[shape].tail)
	u := widenUnit(t, map[string][]byte{"Probe.schema": []byte(src)})
	files, err := Generate(u)
	if err != nil {
		t.Fatalf("%s: Generate refuses: %v", decl, err)
	}
	files = maps.Clone(files)
	if mutate != nil {
		mutate(t, files)
	}
	dir := t.TempDir()
	writeTree(t, dir, files)
	st := u.Tables["Root"]
	fieldID := ir.TableFieldWireId(st.Fields[0])
	var cells []widenCell
	for _, kind := range widenWireKinds {
		payloads := widenPayloads(kind)
		names := make([]string, 0, len(payloads))
		for name := range payloads {
			names = append(names, name)
		}
		sort.Strings(names)
		for _, name := range names {
			cells = append(cells, widenCell{fmt.Sprintf("kind %d %s", kind, name), widenOneFieldWire(fieldID, kind, payloads[name])})
		}
	}
	inputs := map[string][]byte{"main.py": []byte(widenProbe(len(cells)))}
	for i, c := range cells {
		inputs[fmt.Sprintf("w%d", i)] = c.wire
	}
	outs := []string{string(pyrun.Run(t, dir, "main.py", inputs))}
	if floor {
		outs = append(outs, string(pyrun.RunFloor(t, dir, "main.py", inputs)))
	}
	if analyze {
		if out, err := pyrun.Analyze(t, dir, "probe"); err != nil {
			t.Fatalf("mypy --strict over the generated package:\n%s", out)
		}
		if out, err := pyrun.Analyze(t, dir, "probe/_schema"); err != nil {
			t.Fatalf("mypy --strict over the runtime:\n%s", out)
		}
	}
	m := tabletext.NewModel(u)
	run := widenRun{faults: map[string]string{}, cells: len(cells)}
	for pin, out := range outs {
		lines := strings.Split(strings.TrimRight(out, "\n"), "\n")
		if len(lines) != len(cells) {
			t.Fatalf("%s (%s): %d lines for %d cells:\n%s", decl, widenShapes[shape].name, len(lines), len(cells), out)
		}
		for i, c := range cells {
			inst := m.New(m.Lookup("Root"))
			var r tabletext.Report
			ok, err := tablewire.Decode(m, inst, c.wire, &r)
			want := "refused 7 7 7 7 True"
			if ok && err == nil && !r.Refused {
				saved, err := tablewire.Encode(m, inst)
				if err != nil {
					t.Fatalf("%s under %s: the reference cannot save what it read: %v", c.name, decl, err)
				}
				want = fmt.Sprintf("read %s %d 0 %s True True", widenRefValue(inst), r.Widened, hex.EncodeToString(saved))
				if pin == 0 {
					run.widened += r.Widened
				}
			}
			if lines[i] != want {
				key := fmt.Sprintf("%s under %s (%s, pin %d)", c.name, decl, widenShapes[shape].name, pin)
				run.faults[key] = fmt.Sprintf("python %q, the reference %q", lines[i], want)
			}
		}
	}
	return run
}

// widenUnit parses and checks schema files given by name.
func widenUnit(t *testing.T, srcs map[string][]byte) *ir.Unit {
	t.Helper()
	names := make([]string, 0, len(srcs))
	for name := range srcs {
		names = append(names, name)
	}
	sort.Strings(names)
	files := make([]check.SourceFile, 0, len(names))
	for _, name := range names {
		ast, perrs := parser.Parse(name, srcs[name])
		if len(perrs) > 0 {
			t.Fatalf("parse %s: %v", name, perrs[0])
		}
		files = append(files, check.SourceFile{
			Path: name, Name: filepath.Base(name), Base: strings.TrimSuffix(filepath.Base(name), ".schema"),
			Bytes: srcs[name], AST: ast,
		})
	}
	u, cerrs := check.Unit(files)
	if len(cerrs) > 0 {
		t.Fatalf("check: %v", cerrs[0])
	}
	return u
}

// widenManifestHeld are the corpus widening rows this cut reads: tblk2
// generates under the table frame, so its reports.txt verdict is held by a
// read on both pins.
var widenManifestHeld = []string{"k1_as_k2"}

// widenNotCarried is one exact "not carried yet" refusal: Generate's error
// carrying generate, or the read refusing with the runtime reason read.
type widenNotCarried struct{ generate, read string }

// The not-carried refusals this runtime gives today.
var (
	notCarriedPacketTypes = widenNotCarried{generate: "the Python target does not carry packet types yet"}
	notCarriedKind        = widenNotCarried{read: "unsupported_kind"}
)

// widenManifestSkipped are the corpus widening rows whose units rest on a
// construct this cut does not carry: packet types in tables, lists and maps.
// They are NOT proven here. Each is skipped by name only when it gives
// exactly its expected not-carried refusal; any other Generate error or read
// verdict fails the test, so a card that carries the construct must move the
// row to widenManifestHeld and hold its reports.txt verdict.
var widenManifestSkipped = map[string]widenNotCarried{
	"field_scalar_widen":       notCarriedPacketTypes,
	"scalars_full_as_2":        notCarriedPacketTypes,
	"scalars_edges_as_2":       notCarriedPacketTypes,
	"a1_arm_moved_width_as_a2": notCarriedPacketTypes,
	"previous_form_wide_blob":  notCarriedPacketTypes,
	"list_scalar_widen":        notCarriedKind,
	"map_key_widen":            notCarriedKind,
}

// notCarriedReads are the runtime refusals that mean "not carried yet": a
// held row refusing with one of them has not been read at all.
var notCarriedReads = map[string]bool{"unsupported_kind": true, "fixed_form_as_file": true}

type manifestRow struct{ unit, root, wire string }

func widenManifest(t *testing.T, repo string) (map[string][]string, map[string]manifestRow, map[string]string) {
	t.Helper()
	f, err := os.Open(filepath.Join(repo, "testdata", "conformance", "tables", "MANIFEST.txt"))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	units := map[string][]string{}
	rows := map[string]manifestRow{}
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		fields := strings.Fields(sc.Text())
		if len(fields) == 0 || strings.HasPrefix(fields[0], "#") {
			continue
		}
		switch {
		case fields[0] == "unit" && len(fields) >= 3:
			units[fields[1]] = fields[2:]
		case (fields[0] == "report" || fields[0] == "instance") && len(fields) >= 5:
			rows[fields[1]] = manifestRow{unit: fields[2], root: fields[3], wire: fields[4]}
		}
	}
	if err := sc.Err(); err != nil {
		t.Fatal(err)
	}
	expect := map[string]string{}
	data, err := os.ReadFile(filepath.Join(repo, "testdata", "conformance", "tables", "reports.txt"))
	if err != nil {
		t.Fatal(err)
	}
	for _, line := range strings.Split(string(data), "\n") {
		fields := strings.Fields(line)
		if len(fields) == 2 && !strings.HasPrefix(fields[0], "#") {
			expect[fields[0]] = fields[1]
		}
	}
	return units, rows, expect
}

func widenUnitFromPaths(t *testing.T, repo string, paths []string) *ir.Unit {
	t.Helper()
	srcs := map[string][]byte{}
	for _, p := range paths {
		full := filepath.Join(repo, p)
		info, err := os.Stat(full)
		if err != nil {
			t.Fatal(err)
		}
		var names []string
		if info.IsDir() {
			entries, err := os.ReadDir(full)
			if err != nil {
				t.Fatal(err)
			}
			for _, e := range entries {
				if !e.IsDir() && strings.HasSuffix(e.Name(), ".schema") {
					names = append(names, filepath.Join(full, e.Name()))
				}
			}
		} else {
			names = append(names, full)
		}
		for _, name := range names {
			data, err := os.ReadFile(name)
			if err != nil {
				t.Fatal(err)
			}
			srcs[name] = data
		}
	}
	return widenUnit(t, srcs)
}

// widenManifestProbe reads the row's wire once with a report and a buffer the
// caller owns: a refusal prints its reason and whether both are untouched, a
// read prints the write-back.
const widenManifestProbe = `import sys
from %s import LoadReport, Refused, %s

with open("wire", "rb") as f:
    data = bytearray(f.read())
before = bytes(data)
report = LoadReport()
report.unknown = report.widened = report.retained = report.retain_lost = 7
try:
    value = %s.load(data, report)
except Refused as err:
    untouched = (report.unknown, report.widened, report.retained, report.retain_lost) == (7, 7, 7, 7) and bytes(data) == before
    print("refused", err.reason, untouched)
    sys.exit(0)
print("read", value.save().hex(), bytes(data) == before)
`

// widenManifestRun generates the row's unit and reads its wire on both pins.
// A Generate error is returned, not reported: the caller decides whether it
// is the row's expected not-carried refusal.
func widenManifestRun(t *testing.T, repo string, units map[string][]string, row manifestRow) (string, error) {
	t.Helper()
	u := widenUnitFromPaths(t, repo, units[row.unit])
	files, err := Generate(u)
	if err != nil {
		return "", err
	}
	dir := t.TempDir()
	writeTree(t, dir, files)
	wire, err := os.ReadFile(filepath.Join(repo, row.wire))
	if err != nil {
		t.Fatal(err)
	}
	probe := pyrun.ImportPrelude + fmt.Sprintf(widenManifestProbe, u.Package, row.root, row.root)
	inputs := map[string][]byte{"main.py": []byte(probe), "wire": wire}
	got := strings.TrimSpace(string(pyrun.Run(t, dir, "main.py", inputs)))
	if floor := strings.TrimSpace(string(pyrun.RunFloor(t, dir, "main.py", inputs))); floor != got {
		t.Fatalf("%s: the pins disagree: %q and %q", row.unit, got, floor)
	}
	return got, nil
}

// TestPythonVtWidening holds widening on read to the reference engine.
//
// Functional (pyrun): run with SCHEMA_SLOW=1.
func TestPythonVtWidening(t *testing.T) {
	t.Parallel()
	t.Run("ladder", func(t *testing.T) {
		t.Parallel()
		for di, decl := range widenDecls {
			for shape := range widenShapes {
				t.Run(fmt.Sprintf("%s/%s", strings.Fields(decl)[0]+fmt.Sprint(di), widenShapes[shape].name), func(t *testing.T) {
					t.Parallel()
					run := widenLadder(t, decl, shape, nil, true, di == 0 || strings.HasPrefix(decl, "float"))
					keys := make([]string, 0, len(run.faults))
					for k := range run.faults {
						keys = append(keys, k)
					}
					sort.Strings(keys)
					for _, k := range keys {
						t.Errorf("%s: %s", k, run.faults[k])
					}
					t.Logf("%s (%s): %d cells, %d widened by the reference", decl, widenShapes[shape].name, run.cells, run.widened)
				})
			}
		}
	})
	t.Run("ladder-widens", func(t *testing.T) {
		t.Parallel()
		// the grid must exercise widening, or the ladder is green by holding
		// nothing: int16 alone takes kind 2 under both payloads
		run := widenLadder(t, "int16", 0, nil, false, false)
		if run.widened != 2 {
			t.Fatalf("int16 widened %d cells under the reference, want 2", run.widened)
		}
	})
	t.Run("manifest", func(t *testing.T) {
		t.Parallel()
		// both pins up front: without SCHEMA_SLOW the whole manifest skips by
		// name here, so the tally below never counts a row the pins skipped
		pyrun.Interpreter(t)
		pyrun.Floor(t)
		repo := filepath.Join("..", "..", "..")
		units, rows, expect := widenManifest(t, repo)
		var mu sync.Mutex
		var held, skipped []string
		// rows waits for every row before the tally below is taken
		t.Run("rows", func(t *testing.T) {
			for _, name := range widenManifestHeld {
				t.Run(name, func(t *testing.T) {
					t.Parallel()
					row, ok := rows[name]
					if !ok {
						t.Fatalf("the manifest carries no row %s", name)
					}
					want, ok := expect[name]
					if !ok {
						t.Fatalf("reports.txt carries no verdict for %s", name)
					}
					got, err := widenManifestRun(t, repo, units, row)
					if err != nil {
						t.Fatalf("%s: the held unit %s is refused: %v", name, row.unit, err)
					}
					fields := strings.Fields(got)
					if len(fields) != 3 || fields[0] != want {
						t.Fatalf("%s: python %q, reports.txt %s", name, got, want)
					}
					switch want {
					case "refused":
						if notCarriedReads[fields[1]] {
							t.Fatalf("%s: refused %s, a construct not carried yet, not a verdict on widening", name, fields[1])
						}
						if fields[2] != "True" {
							t.Fatalf("%s: the refusal wrote the caller's report or buffer: %q", name, got)
						}
					case "read":
						canon, err := os.ReadFile(filepath.Join(repo, "testdata", "conformance", "tables", "save-wire", name+".bin"))
						if err != nil {
							t.Fatal(err)
						}
						if fields[1] != hex.EncodeToString(canon) {
							t.Fatalf("%s: the write-back is not the canonical wire:\npython    %s\ncanonical %x", name, fields[1], canon)
						}
						if fields[2] != "True" {
							t.Fatalf("%s: the read wrote the caller's buffer: %q", name, got)
						}
					default:
						t.Fatalf("%s: reports.txt verdict %q is neither read nor refused", name, want)
					}
					mu.Lock()
					held = append(held, name)
					mu.Unlock()
					t.Logf("%s: held, %s", name, got)
				})
			}
			names := make([]string, 0, len(widenManifestSkipped))
			for name := range widenManifestSkipped {
				names = append(names, name)
			}
			sort.Strings(names)
			for _, name := range names {
				nc := widenManifestSkipped[name]
				t.Run(name, func(t *testing.T) {
					t.Parallel()
					row, ok := rows[name]
					if !ok {
						t.Fatalf("the manifest carries no row %s", name)
					}
					if _, ok := expect[name]; !ok {
						t.Fatalf("reports.txt carries no verdict for %s", name)
					}
					got, err := widenManifestRun(t, repo, units, row)
					var reason string
					switch {
					case nc.generate != "":
						if err == nil {
							t.Fatalf("%s: the unit %s now generates; move the row to widenManifestHeld and hold its reports.txt verdict", name, row.unit)
						}
						if !strings.Contains(err.Error(), nc.generate) {
							t.Fatalf("%s: Generate refuses the unit %s for another reason than %q: %v", name, row.unit, nc.generate, err)
						}
						reason = err.Error()
					default:
						if err != nil {
							t.Fatalf("%s: Generate refuses the unit %s: %v", name, row.unit, err)
						}
						fields := strings.Fields(got)
						if len(fields) != 3 || fields[0] != "refused" || fields[1] != nc.read {
							t.Fatalf("%s: python %q, want the not-carried refusal %s; if the construct is now carried, move the row to widenManifestHeld and hold its reports.txt verdict", name, got, nc.read)
						}
						if fields[2] != "True" {
							t.Fatalf("%s: the refusal wrote the caller's report or buffer: %q", name, got)
						}
						reason = "the read refuses " + nc.read
					}
					mu.Lock()
					skipped = append(skipped, name)
					mu.Unlock()
					t.Skipf("%s: NOT PROVEN, not carried yet: %s", name, reason)
				})
			}
		})
		if t.Failed() {
			return
		}
		sort.Strings(held)
		sort.Strings(skipped)
		if len(held) != len(widenManifestHeld) || len(skipped) != len(widenManifestSkipped) {
			t.Fatalf("manifest: %d held %v and %d skipped %v, want %d held and %d skipped", len(held), held, len(skipped), skipped, len(widenManifestHeld), len(widenManifestSkipped))
		}
		t.Logf("manifest: %d held by a read (%s), %d skipped as not carried yet and not proven (%s)", len(held), strings.Join(held, ", "), len(skipped), strings.Join(skipped, ", "))
	})
}

// widenSitesProbe sends an arm, an array element, a map key and the float
// rung through the one _accepts/_read pair. The rule is the kind pair's, so
// each site is a FieldDesc of the kind its declaration carries; a narrowing
// refuses and leaves ctx.widened at zero.
const widenSitesProbe = pyrun.ImportPrelude + `from probe._schema import Refused
from probe._schema import widen
from probe._schema.descriptor import FieldDesc
from probe._schema.load import Ctx, Reader

LO = -(1 << 127)
HI = (1 << 127) - 1


def probe(label, kind, codec, wire_kind, hexbits):
    data = bytes.fromhex(hexbits)
    r = Reader(memoryview(data), 0, len(data), [])
    ctx = Ctx([])
    fd = FieldDesc(name=label, field_id=1, kind=kind, codec=codec, lo=LO, hi=HI)
    if not widen._accepts(fd, wire_kind):
        print(label, "refused", "kind_mismatch", ctx.widened, r.pos)
        return
    try:
        value = widen._read(r, fd, wire_kind, ctx)
    except Refused as err:
        print(label, "refused", err.reason, ctx.widened, r.pos)
        return
    print(label, "read", repr(value), ctx.widened, r.pos)


# an arm, an element and a map key are kind comparisons like a field's
probe("arm-wide", 5, "int", 4, "fdffffff")
probe("element-wide", 18, "int", 5, "fdffffffffffffff")
probe("map-key-wide", 8, "int", 7, "fdff")
# the float rung's kind pair decides, not the codec string
probe("float-wide", 11, "unsupported", 10, "0000c0bf")
# a narrowing at any of them is a kind mismatch, and no value is returned
probe("arm-narrow", 4, "int", 5, "fdffffffffffffff")
probe("element-narrow", 7, "int", 8, "fdffffff")
probe("map-key-narrow", 8, "int", 9, "fdffffffffffffff")
# a non-finite source never reaches the rung
probe("float-non-finite", 11, "unsupported", 10, "0000c07f")
`

// TestPythonVtWideningSites holds the rule at an arm, an element and a map
// key: the kind pair alone decides, so a widening reads and counts one and a
// narrowing refuses without a value. The construct loaders themselves land
// with the cards that carry those kinds.
//
// Functional (pyrun): run with SCHEMA_SLOW=1.
func TestPythonVtWideningSites(t *testing.T) {
	t.Parallel()
	u := widenUnit(t, map[string][]byte{"Probe.schema": []byte("package probe\n\ntable Root\n{\n    value int32\n}\n")})
	files, err := Generate(u)
	if err != nil {
		t.Fatalf("Generate refuses the probe unit: %v", err)
	}
	dir := t.TempDir()
	writeTree(t, dir, files)
	inputs := map[string][]byte{"main.py": []byte(widenSitesProbe)}
	want := strings.Join([]string{
		"arm-wide read -3 1 4",
		"element-wide read -3 1 8",
		"map-key-wide read 65533 1 2",
		"float-wide read -1.5 1 4",
		"arm-narrow refused kind_mismatch 0 0",
		"element-narrow refused kind_mismatch 0 0",
		"map-key-narrow refused kind_mismatch 0 0",
		"float-non-finite refused non_finite_float 0 4",
	}, "\n")
	got := strings.TrimSpace(string(pyrun.Run(t, dir, "main.py", inputs)))
	if floor := strings.TrimSpace(string(pyrun.RunFloor(t, dir, "main.py", inputs))); floor != got {
		t.Fatalf("the pins disagree:\ncurrent:\n%s\nfloor:\n%s", got, floor)
	}
	if got != want {
		t.Fatalf("the rule at the sites:\npython\n%s\nwant\n%s", got, want)
	}
}

// TestPythonVtWideningNegativeControl breaks one rule of widen.py at a time,
// in a copy of the runtime, and requires the ladder to go red on a cell the
// rule decides.
//
// Functional (pyrun): run with SCHEMA_SLOW=1.
func TestPythonVtWideningNegativeControl(t *testing.T) {
	t.Parallel()
	const path = "probe/_schema/widen.py"
	plant := func(from, to string) func(*testing.T, map[string][]byte) {
		return func(t *testing.T, files map[string][]byte) {
			src := string(files[path])
			if strings.Count(src, from) != 1 {
				t.Fatalf("%s carries %q %d times, want 1: the control would be vacuous", path, from, strings.Count(src, from))
			}
			files[path] = []byte(strings.Replace(src, from, to, 1))
		}
	}
	for _, ctl := range []struct {
		name, decl, cell string
		mutate           func(*testing.T, map[string][]byte)
	}{
		// the signed ladder zero-extended: a negative int8 lands as 253
		{"signed-ladder", "int16", "kind 2 top", plant("signed=kind in _SIGNED", "signed=False")},
		// no rung accepted: every widening refuses as a kind mismatch
		{"rungs-closed", "uint64", "kind 6 small", plant("return kind in _RUNGS.get(fd.kind, _NONE)", "return False")},
		// the rung read but not counted
		{"widened-count", "int128 | min = -170141183460469231731687303715884105728, max = 170141183460469231731687303715884105727", "kind 5 top",
			plant("    ctx.widened += 1\n", "")},
		// a narrowing accepted: a wider kind under a narrower declaration
		{"narrowing", "int8", "kind 3 small", plant("    3: frozenset({2}),\n", "    2: frozenset({3}),\n    3: frozenset({2}),\n")},
		// the float rung closed: kind 10 refuses into a float64
		{"float-rung", "float64", "kind 10 neg", plant("    11: frozenset({10}),\n", "")},
	} {
		for shape := range widenShapes {
			t.Run(ctl.name+"/"+widenShapes[shape].name, func(t *testing.T) {
				t.Parallel()
				run := widenLadder(t, ctl.decl, shape, ctl.mutate, false, false)
				if len(run.faults) == 0 {
					t.Fatalf("NEGATIVE CONTROL FAILED: %s left the ladder green under %s", ctl.name, ctl.decl)
				}
				named := false
				for k := range run.faults {
					if strings.HasPrefix(k, ctl.cell+" ") {
						named = true
					}
				}
				if !named {
					t.Fatalf("NEGATIVE CONTROL FAILED: %s went red, but not on %s: %v", ctl.name, ctl.cell, run.faults)
				}
				t.Logf("%s (%s): %d red cells, %s among them", ctl.name, widenShapes[shape].name, len(run.faults), ctl.cell)
			})
		}
	}
}
