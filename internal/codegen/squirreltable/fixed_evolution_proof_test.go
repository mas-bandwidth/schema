package squirreltable_test

// THE FIXED FORM'S EVOLUTION, REPORT, SCALAR-BOUND AND ORDINAL ROWS ON THE
// SQUIRREL LEG (docs/FIXED-FORM-ALGORITHM.md §4.5, §4.6, §5.3, §5.4), under the
// strict-only read (docs/rulings.json strict-only-read, refusal-verdict-only):
// a read lands the whole value or refuses the whole load, a value the writer
// could not have written refuses out_of_range and never clamps, and widened
// is the one counter. The rows are proved through Squirrel's own design under
// the per-language-design-proof ruling: the emitted <Root>FixedLoad selects a
// plan by hash from the lineage laid down at build time (fixedform.go,
// fixedlineage.go), and T.FixedReason names a refusal. Every file a row reads
// is framed here from the compiler's own layout and hash (FixedLineageOf), or
// written by the emitted FixedSave; every row that guards a runtime rule has a
// negative control that plants a break into the emitted module and requires
// the row to go red. Reason names are compared only against Squirrel's own
// pins, never across languages.

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/squirrel/sqrun"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/squirreltable"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// sqEvoFile frames a form-3 file: the header, the layout, and each body behind
// the record hash.
func sqEvoFile(hash uint64, layout []byte, bodies ...[]byte) []byte {
	out := []byte{3, 0, 0, 0, 0, 0, 0, 0}
	out = binary.LittleEndian.AppendUint64(out, hash)
	out = binary.LittleEndian.AppendUint32(out, uint32(len(layout)))
	out = append(out, layout...)
	for _, b := range bodies {
		out = binary.LittleEndian.AppendUint64(out, hash)
		out = append(out, b...)
	}
	return out
}

// sqEvoEntry is the lineage entry the unit's own build locks for root.
func sqEvoEntry(t *testing.T, u *ir.Unit, root string) squirreltable.FixedLineageEntry {
	t.Helper()
	e, ok := squirreltable.FixedLineageOf(u, root)
	if !ok {
		t.Fatalf("no fixed root %s", root)
	}
	return e
}

func sqEvoClone(b []byte) []byte { return append([]byte(nil), b...) }

// sqEvoGen is one generated unit in its own directory: the module the probe
// requires and the bytes a negative control plants a break into.
type sqEvoGen struct {
	unit  *ir.Unit
	dir   string
	mod   string
	files map[string][]byte
}

// sqEvoGenerate writes u's generated Squirrel (the table module and the
// runtime) into a fresh directory, handing the lineage to GenerateLineage when
// it is not nil.
func sqEvoGenerate(t *testing.T, u *ir.Unit, lineage map[string][]squirreltable.FixedLineageEntry) *sqEvoGen {
	t.Helper()
	files, err := sqGenerateAll(u, lineage)
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	g := &sqEvoGen{unit: u, dir: t.TempDir(), mod: squirreltable.ModuleName(u), files: files}
	sqWriteFiles(t, g.dir, files)
	return g
}

func (g *sqEvoGen) source() string { return string(g.files[g.mod+".nut"]) }

// plant replaces anchor, which must sit in the emitted module exactly once,
// and rewrites the unit's copy.
func (g *sqEvoGen) plant(t *testing.T, anchor, with string) {
	t.Helper()
	src := g.source()
	if strings.Count(src, anchor) != 1 {
		t.Fatalf("the anchor %q is not in %s.nut exactly once", anchor, g.mod)
	}
	g.files[g.mod+".nut"] = []byte(strings.Replace(src, anchor, with, 1))
	sqWriteFiles(t, g.dir, map[string][]byte{g.mod + ".nut": g.files[g.mod+".nut"]})
}

// dropLine deletes the one emitted line that holds every needle.
func (g *sqEvoGen) dropLine(t *testing.T, needles ...string) {
	t.Helper()
	var hit string
	n := 0
	for line := range strings.SplitSeq(g.source(), "\n") {
		all := true
		for _, nd := range needles {
			if !strings.Contains(line, nd) {
				all = false
				break
			}
		}
		if all {
			hit = line
			n++
		}
	}
	if n != 1 {
		t.Fatalf("%d lines of %s.nut hold %q, want exactly one", n, g.mod, needles)
	}
	g.plant(t, hit+"\n", "")
}

// exec runs main.nut in the unit's directory over inputs. Unlike sqrun.Run it
// answers an interpreter error instead of failing the test, so a negative
// control whose planted break raises still reads as red.
func (g *sqEvoGen) exec(t *testing.T, main string, inputs map[string][]byte) (string, error) {
	t.Helper()
	sq := sqrun.Require(t)
	if err := os.WriteFile(filepath.Join(g.dir, "inputs.nut"), sqrun.InputsModule(inputs), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(g.dir, "main.nut"), []byte(main), 0o644); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(sq, "main.nut")
	cmd.Dir = g.dir
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	err := cmd.Run()
	if err == nil && stderr.Len() > 0 {
		err = fmt.Errorf("stderr: %s", stderr.String())
	}
	return stdout.String(), err
}

// run is exec for a probe that must not raise.
func (g *sqEvoGen) run(t *testing.T, main string, inputs map[string][]byte) string {
	t.Helper()
	out, err := g.exec(t, main, inputs)
	if err != nil {
		t.Fatalf("sq: %v\nstdout:\n%s", err, out)
	}
	return strings.TrimSpace(out)
}

// sqEvoDumper is the Squirrel the probes share: the module, the inputs, the
// enum and union tables the dump follows, and dump(prefix, obj), which
// appends one "key=value" line per landed field to `lines`.
func sqEvoDumper(g *sqEvoGen) string {
	return fmt.Sprintf(`local T = dofile("schema_module.nut", true).require(%q);
local inputs = dofile("inputs.nut", true);
%s%slocal lines = [];
local dump = null;
local dumpValue = function(key, v) {
  local vt = typeof v;
  if (vt == "string") {
    if (enumOrd.rawin(v)) lines.append(key + "=" + enumOrd[v]);
    else lines.append(key + "=\"" + v + "\"");
  } else if (vt == "bool") lines.append(key + "=" + (v ? 1 : 0));
  else if (vt == "integer" || vt == "float") lines.append(key + "=" + v);
  else if (vt == "null") lines.append(key + "=null");
  else if (vt == "instance") dump(key, v);
  else if (vt == "blob") {
    local s = "";
    for (local i = 0; i < v.len(); i++) s += format("%%02x", v[i] & 0xff);
    lines.append(key + "=b" + s);
  }
};
dump = function(prefix, obj) {
  local arm = null;
  // a refused load may leave a forged tag no arm table holds
  try { arm = unionArm(obj); } catch (e) { lines.append(prefix + ".Type=" + obj.Type); return; }
  if (arm != null) {
    lines.append(prefix + ".Type=" + obj.Type);
    if (arm == "") return;
  }
  local keys = [];
  foreach (k, _ in obj.getclass()) {
    if (k == "constructor" || k == "Type") continue;
    keys.append(k);
  }
  keys.sort();
  foreach (k in keys) {
    if (arm != null && k != arm) continue;
    local v = obj[k];
    local key = prefix + "." + k;
    if (typeof v == "array") {
      lines.append(key + "_count=" + v.len());
      for (local i = 0; i < v.len(); i++) dumpValue(key + "[" + i + "]", v[i]);
    } else dumpValue(key, v);
  }
};
`, g.mod, squirrelEnumOrdinals(g.unit), squirrelUnionArms(g.unit))
}

// sqEvoRun is one probe's answer: the head ("ok <n> w<widened>", or "refused
// <reason> w<widened> <untouched|touched>", or "raised"), the landed fields
// by normalized key, and the bytes the load's values save back to.
type sqEvoRun struct {
	head  string
	dump  map[string]string
	saved []byte
}

// sqEvoProbe loads inputs["file"] at root into `rows` fresh records (cap < 0
// passes none). A refusal prints the head with whether the records still hold
// what they held before the load; a read prints the head, the dump and the
// re-saved bytes.
func sqEvoProbe(g *sqEvoGen, root string, rows, capacity int) string {
	capLit := "null"
	if capacity >= 0 {
		capLit = strconv.Itoa(capacity)
	}
	return sqEvoDumper(g) + fmt.Sprintf(`local rows = array(%[2]d);
for (local i = 0; i < %[2]d; i++) rows[i] = T.%[1]s();
for (local i = 0; i < %[2]d && i < 4; i++) dump("r" + i, rows[i]);
local before = lines;
lines = [];
local got = T.%[1]sFixedLoad(inputs["file"], rows, %[3]s);
local w = T.FixedWidened;
if (got < 0) {
  for (local i = 0; i < %[2]d && i < 4; i++) dump("r" + i, rows[i]);
  local same = before.len() == lines.len();
  for (local i = 0; same && i < lines.len(); i++) if (before[i] != lines[i]) same = false;
  print("refused " + T.FixedReason + " w" + w + (same ? " untouched" : " touched") + "\n");
  return;
}
print("ok " + got + " w" + w + "\n");
for (local i = 0; i < got && i < 8; i++) dump("r" + i, rows[i]);
foreach (l in lines) print(l + "\n");
rows.resize(got);
local m = T.%[1]sFixedMeasure(got);
local out = blob(m);
local n = T.%[1]sFixedSave(out, rows);
if (n < 0) { print("SAVENIL " + T.FixedReason + "\n"); return; }
local s = "x";
for (local i = 0; i < n; i++) s += format("%%02x", out[i] & 0xff);
print(s + "\n");
`, root, rows, capLit)
}

// load runs sqEvoProbe over file with two fresh records.
func (g *sqEvoGen) load(t *testing.T, root string, file []byte) sqEvoRun {
	t.Helper()
	return g.loadN(t, root, file, 2, -1)
}

func (g *sqEvoGen) loadN(t *testing.T, root string, file []byte, rows, capacity int) sqEvoRun {
	t.Helper()
	out, err := g.exec(t, sqEvoProbe(g, root, rows, capacity), map[string][]byte{"file": file})
	if err != nil {
		return sqEvoRun{head: "raised", dump: map[string]string{}}
	}
	lines := strings.Split(strings.TrimSpace(out), "\n")
	r := sqEvoRun{head: lines[0], dump: map[string]string{}}
	for _, l := range lines[1:] {
		if rest, ok := strings.CutPrefix(l, "x"); ok {
			b, err := decodeHex(rest)
			if err != nil {
				t.Fatal(err)
			}
			r.saved = b
			continue
		}
		if rest, ok := strings.CutPrefix(l, "SAVENIL "); ok {
			r.dump["SAVENIL"] = rest
			continue
		}
		k, v, _ := strings.Cut(l, "=")
		r.dump[normalizeSqFieldKey(k)] = v
	}
	return r
}

// save runs setup (Squirrel that fills `values`, an array of records, with T
// the unit's module) and answers the file root's FixedSave writes.
func (g *sqEvoGen) save(t *testing.T, root, setup string) []byte {
	t.Helper()
	main := fmt.Sprintf(`local T = dofile("schema_module.nut", true).require(%[1]q);
local values = [];
%[3]s
local m = T.%[2]sFixedMeasure(values.len());
local out = blob(m);
local n = T.%[2]sFixedSave(out, values);
if (n < 0) { print("SAVENIL " + T.FixedReason + "\n"); return; }
local s = "";
for (local i = 0; i < n; i++) s += format("%%02x", out[i] & 0xff);
print(s + "\n");
`, g.mod, root, setup)
	out := g.run(t, main, nil)
	if strings.HasPrefix(out, "SAVENIL") {
		t.Fatalf("the save refused: %s", out)
	}
	b, err := decodeHex(out)
	if err != nil {
		t.Fatalf("the save printed %q: %v", out, err)
	}
	return b
}

// refused is the head of a refusal by reason, with the widened counter at w.
func refused(reason string) string { return "refused " + reason + " " }

const sqEvoWidenOld = "package probe\nfixed table N\n{\n    lead uint32 = 1\n    v int16 = 0\n    f float32 = 0.0\n    trail uint32 = 2\n}\n"
const sqEvoWidenNew = "package probe\nfixed table N\n{\n    lead uint32 = 1\n    v int32 = 0\n    f float64 = 0.0\n    trail uint32 = 2\n    added int32 = 77\n}\n"

// sqEvoWidenFile is two records of the older N: lead 5, v -7, f -1234.5, trail 6.
func sqEvoWidenFile(eo squirreltable.FixedLineageEntry) []byte {
	body := binary.LittleEndian.AppendUint32(nil, 5)
	body = binary.LittleEndian.AppendUint16(body, 0xFFF9) // -7
	body = binary.LittleEndian.AppendUint32(body, 0xC49A5000)
	body = binary.LittleEndian.AppendUint32(body, 6)
	return sqEvoFile(eo.Wire, eo.Layout, body, body)
}

// TestSquirrelFixedEvolutionNumericWidening is numeric-evolution (E3, C8, R20,
// R21 under strict-only): a newer reader handed the older writer's entry reads
// its file through the compiled plan — int16 into int32 sign-extends, float32
// into float64 exactly, an appended field lands its declared default, and
// widened counts once per widened leaf per record (four over two records) —
// while the older reader's identity plan widens nothing. The writer's RANGE
// governs a compiled read (§4.6): v and every element of vals declared [0, 100]
// by the old writer and [0, 200] by the reader refuse out_of_range at 150 and
// land at 100. The negative controls drop the widening count, zero-extend the
// int16, and drop the writer's range.
func TestSquirrelFixedEvolutionNumericWidening(t *testing.T) {
	sqrun.Require(t)
	t.Parallel()
	older, newer := fixedSynthetic(t, sqEvoWidenOld), fixedSynthetic(t, sqEvoWidenNew)
	eo := sqEvoEntry(t, older, "N")
	file := sqEvoWidenFile(eo)
	lineage := map[string][]squirreltable.FixedLineageEntry{"N": {eo}}
	got := sqEvoGenerate(t, newer, lineage).load(t, "N", file)
	if got.head != "ok 2 w4" {
		t.Errorf("the widening read: %s, want two widened leaves per record", got.head)
	}
	for k, w := range map[string]string{"r0.lead": "5", "r0.v": "-7", "r0.f": "-1234.5", "r0.trail": "6", "r0.added": "77", "r1.v": "-7", "r1.f": "-1234.5", "r1.added": "77"} {
		if got.dump[k] != w {
			t.Errorf("%s landed %q, want %s", k, got.dump[k], w)
		}
	}
	if len(got.saved) == 0 {
		t.Errorf("the widened values do not save back: %v", got.dump)
	}
	if own := sqEvoGenerate(t, older, nil).load(t, "N", file); own.head != "ok 2 w0" || own.dump["r0.v"] != "-7" {
		t.Errorf("the identity plan: %s v=%s, want ok 2 w0 v=-7", own.head, own.dump["r0.v"])
	}

	g := sqEvoGenerate(t, newer, lineage)
	g.plant(t, "    _M.FixedWidened <- _M.FixedWidened + 1;\n    v.V = _rt_fixed.get(b, at + 4, 2);\n", "    v.V = _rt_fixed.get(b, at + 4, 2);\n")
	if g.load(t, "N", file).head == got.head {
		t.Errorf("the negative control: an integer widening not counted still reads w4")
	}
	g = sqEvoGenerate(t, newer, lineage)
	g.plant(t, "v.V = _rt_fixed.get(b, at + 4, 2);", "v.V = _rt_fixed.getu(b, at + 4, 2);")
	if g.load(t, "N", file).dump["r0.v"] == "-7" {
		t.Errorf("the negative control: a zero-extended int16 still lands -7")
	}

	// THE WRITER'S RANGE (§4.6): a value inside the reader's range and outside
	// the writer's is not one the writer wrote.
	const ranged = "package probe\nfixed table W\n{\n    lead uint32 = 1\n    v int32 = 0 | min = 0, max = %[1]d\n    vals [4]int32 | min = 0, max = %[1]d\n    trail uint32 = 2\n}\n"
	wOld, wNew := fixedSynthetic(t, fmt.Sprintf(ranged, 100)), fixedSynthetic(t, fmt.Sprintf(ranged, 200))
	we := sqEvoEntry(t, wOld, "W")
	if we.Wire == sqEvoEntry(t, wNew, "W").Wire {
		t.Fatalf("a widened range does not move the definition hash")
	}
	wBody := func(v, elem uint32) []byte {
		b := binary.LittleEndian.AppendUint32(nil, 1)
		b = binary.LittleEndian.AppendUint32(b, v)
		for _, x := range []uint32{10, 20, elem, 40} {
			b = binary.LittleEndian.AppendUint32(b, x)
		}
		return binary.LittleEndian.AppendUint32(b, 2)
	}
	wLin := map[string][]squirreltable.FixedLineageEntry{"W": {we}}
	gw := sqEvoGenerate(t, wNew, wLin)
	for _, c := range []struct {
		name    string
		v, elem uint32
		want    string
	}{
		{"v at the writer's max", 100, 30, "ok 1 w0"},
		{"v past the writer's max, inside the reader's", 150, 30, refused("out_of_range")},
		{"vals[2] past the writer's max", 100, 150, refused("out_of_range")},
		{"v past the reader's max", 201, 30, refused("out_of_range")},
	} {
		got := gw.load(t, "W", sqEvoFile(we.Wire, we.Layout, wBody(c.v, c.elem)))
		if !strings.HasPrefix(got.head, c.want) {
			t.Errorf("%s: %s, want %s", c.name, got.head, c.want)
		}
		if c.want == "ok 1 w0" && (got.dump["r0.v"] != "100" || got.dump["r0.vals[2]"] != "30") {
			t.Errorf("%s: landed %v", c.name, got.dump)
		}
	}
	// the reader's own writer may write 150: its identity plan holds its own range
	wn := sqEvoEntry(t, wNew, "W")
	if got := gw.load(t, "W", sqEvoFile(wn.Wire, wn.Layout, wBody(150, 150))); got.head != "ok 1 w0" || got.dump["r0.v"] != "150" {
		t.Errorf("the reader's own 150: %s %v", got.head, got.dump)
	}
	gc := sqEvoGenerate(t, wNew, wLin)
	gc.plant(t, "  if (v.V < 0 || v.V > 100) return \"out_of_range\";\n", "")
	if got := gc.load(t, "W", sqEvoFile(we.Wire, we.Layout, wBody(150, 30))); strings.HasPrefix(got.head, refused("out_of_range")) {
		t.Errorf("the negative control: without the writer's range, 150 still refuses: %s", got.head)
	}
}

// TestSquirrelFixedEvolutionReports is reports (E9, R16, R18 under
// strict-only, docs/FIXED-FORM-ALGORITHM.md §5.4): widened is the one counter,
// and the report is the load's own — every load resets FixedWidened,
// FixedReason and FixedField, so a widening file loaded twice reads w4 both
// times and a clean load after a refusal names no reason. An append the
// reader knows is not an event (w0), a range that fills its storage width
// cannot fire (256 records of every int8 and uint8 land, w0), and no clamp,
// duplicate or kind-mismatch counter is carried. The negative controls drop
// the widened reset and the reason reset.
func TestSquirrelFixedEvolutionReports(t *testing.T) {
	sqrun.Require(t)
	t.Parallel()
	older, newer := fixedSynthetic(t, sqEvoWidenOld), fixedSynthetic(t, sqEvoWidenNew)
	eo := sqEvoEntry(t, older, "N")
	lineage := map[string][]squirreltable.FixedLineageEntry{"N": {eo}}
	widen := sqEvoWidenFile(eo)
	bad := sqEvoClone(widen)
	bad[0] = 4
	nonFinite := sqEvoClone(widen)
	at := 20 + len(eo.Layout) + 8 + 6
	binary.LittleEndian.PutUint32(nonFinite[at:], 0x7F800000)
	inputs := map[string][]byte{"a_widen": widen, "b_widen": widen, "c_bad": bad, "d_widen": widen, "e_nonfinite": nonFinite, "f_widen": widen}
	const seq = `foreach (nm in ["a_widen", "b_widen", "c_bad", "d_widen", "e_nonfinite", "f_widen"]) {
  local rows = [T.N(), T.N()];
  local got = T.NFixedLoad(inputs[nm], rows, null);
  print(nm + " " + got + " " + T.FixedReason + " " + T.FixedField + " " + T.FixedWidened + "\n");
}
`
	want := "a_widen 2 null null 4\nb_widen 2 null null 4\nc_bad -1 newer_form null 0\nd_widen 2 null null 4\ne_nonfinite -1 non_finite_float N.f 1\nf_widen 2 null null 4"
	g := sqEvoGenerate(t, newer, lineage)
	if got := g.run(t, sqEvoDumper(g)+seq, inputs); got != want {
		t.Errorf("R16: the report across six loads:\n%s\nwant\n%s", got, want)
	}
	for _, counter := range []string{"FixedClamped", "FixedDuplicate", "FixedKindMismatch", "FixedUnknown"} {
		if strings.Contains(g.source(), counter) {
			t.Errorf("E9: the strict fixed reader carries the retired counter %s", counter)
		}
	}

	// an appended field the reader knows is not an event
	appended := fixedSynthetic(t, strings.Replace(sqEvoWidenOld, "    trail uint32 = 2\n", "    trail uint32 = 2\n    extra int32 = 9\n", 1))
	ga := sqEvoGenerate(t, appended, lineage)
	body := binary.LittleEndian.AppendUint32(nil, 5)
	body = binary.LittleEndian.AppendUint16(body, 7)
	body = binary.LittleEndian.AppendUint32(body, 0)
	body = binary.LittleEndian.AppendUint32(body, 6)
	if got := ga.load(t, "N", sqEvoFile(eo.Wire, eo.Layout, body)); got.head != "ok 1 w0" || got.dump["r0.extra"] != "9" {
		t.Errorf("an append: %s extra=%s, want ok 1 w0 extra=9", got.head, got.dump["r0.extra"])
	}

	// R18: a range that fills its width cannot fire
	gr := sqEvoGenerate(t, fixedSynthetic(t, "package probe\nfixed table R\n{\n    a int8 = 0 | min = -128, max = 127\n    b uint8\n}\n"), nil)
	var bodies [][]byte
	for x := range 256 {
		bodies = append(bodies, []byte{byte(x), byte(x)})
	}
	e := sqEvoEntry(t, gr.unit, "R")
	if got := gr.loadN(t, "R", sqEvoFile(e.Wire, e.Layout, bodies...), 256, -1); got.head != "ok 256 w0" {
		t.Errorf("R18: a range that fills its width moved: %s", got.head)
	}

	gc := sqEvoGenerate(t, newer, lineage)
	gc.plant(t, "_M.NFixedLoad <- function(b, out, cap) {\n  _M.FixedReason <- null;\n  _M.FixedField <- null;\n  _M.FixedWidened <- 0;\n", "_M.NFixedLoad <- function(b, out, cap) {\n  _M.FixedReason <- null;\n  _M.FixedField <- null;\n  if (!(\"FixedWidened\" in _M)) _M.FixedWidened <- 0;\n")
	if got, _ := gc.exec(t, sqEvoDumper(gc)+seq, inputs); strings.TrimSpace(got) == want {
		t.Errorf("the negative control: a widened count that accumulates across loads still reads w4 each time")
	}
	gc = sqEvoGenerate(t, newer, lineage)
	gc.plant(t, "_M.NFixedLoad <- function(b, out, cap) {\n  _M.FixedReason <- null;\n  _M.FixedField <- null;\n", "_M.NFixedLoad <- function(b, out, cap) {\n  if (!(\"FixedReason\" in _M)) _M.FixedReason <- null;\n  if (!(\"FixedField\" in _M)) _M.FixedField <- null;\n")
	if got, _ := gc.exec(t, sqEvoDumper(gc)+seq, inputs); strings.TrimSpace(got) == want {
		t.Errorf("the negative control: a reason that outlives its load still reads null after a refusal")
	}
}

// TestSquirrelFixedEvolutionScalarBounds is scalar-bounds (C7, R30 under
// strict-only, docs/FIXED-FORM-ALGORITHM.md §4.6): a ranged int32, int128,
// uint16 and uint64 land at both ends of their ranges and REFUSE out_of_range
// one past either end — never clamped — with the comparison in the field's own
// type (an int128 below its range by its high word, a uint64 at 2^63 + 7 past
// its max unsigned). A compressed float rides as the float32 itself: saved
// unquantized, refused outside its range and landed at its ends; its
// resolution is in the digest under 'Q', so a finer or a dropped resolution is
// a compiled plan that reads the old writer's bits unchanged. The negative
// controls drop the int32 and the int128 range checks.
func TestSquirrelFixedEvolutionScalarBounds(t *testing.T) {
	sqrun.Require(t)
	t.Parallel()
	u := fixedSynthetic(t, "package probe\nfixed table T\n{\n    x int32 = 0 | min = -5, max = 5\n    s int128 = 0 | min = -500, max = 500\n    u uint16 = 10 | min = 10, max = 1000\n    big uint64 = 0 | min = 0, max = 1000\n}\n")
	e := sqEvoEntry(t, u, "T")
	body := func(x int32, sLo, sHi uint64, v uint16, big uint64) []byte {
		b := binary.LittleEndian.AppendUint32(nil, uint32(x))
		b = binary.LittleEndian.AppendUint64(b, sLo)
		b = binary.LittleEndian.AppendUint64(b, sHi)
		b = binary.LittleEndian.AppendUint16(b, v)
		return binary.LittleEndian.AppendUint64(b, big)
	}
	s128 := func(v int64) (uint64, uint64) {
		if v < 0 {
			return uint64(v), ^uint64(0)
		}
		return uint64(v), 0
	}
	if e.Record != 8+int64(len(body(0, 0, 0, 0, 0))) {
		t.Fatalf("the record body is %d, the test frames %d", e.Record-8, len(body(0, 0, 0, 0, 0)))
	}
	type c struct {
		name       string
		x          int32
		s          int64
		sHi        *uint64
		v          uint16
		big        uint64
		ok         bool
		wx, ws, wu string
	}
	hiOne := uint64(1)
	cases := []c{
		{"in range", 3, -42, nil, 500, 7, true, "3", "-42", "500"},
		{"every low end", -5, -500, nil, 10, 0, true, "-5", "-500", "10"},
		{"every high end", 5, 500, nil, 1000, 1000, true, "5", "500", "1000"},
		{"x one below", -6, 0, nil, 10, 0, false, "", "", ""},
		{"x one above", 6, 0, nil, 10, 0, false, "", "", ""},
		{"s one below", 0, -501, nil, 10, 0, false, "", "", ""},
		{"s one above", 0, 501, nil, 10, 0, false, "", "", ""},
		{"s past by its high word", 0, 0, &hiOne, 10, 0, false, "", "", ""},
		{"u one below", 0, 0, nil, 9, 0, false, "", "", ""},
		{"u one above", 0, 0, nil, 1001, 0, false, "", "", ""},
		{"big past its max, unsigned", 0, 0, nil, 10, 1<<63 + 7, false, "", "", ""},
	}
	g := sqEvoGenerate(t, u, nil)
	file := func(k c) []byte {
		lo, hi := s128(k.s)
		if k.sHi != nil {
			hi = *k.sHi
		}
		return sqEvoFile(e.Wire, e.Layout, body(k.x, lo, hi, k.v, k.big))
	}
	for _, k := range cases {
		got := g.load(t, "T", file(k))
		if !k.ok {
			if !strings.HasPrefix(got.head, refused("out_of_range")) {
				t.Errorf("C7 %s: %s, want out_of_range", k.name, got.head)
			}
			continue
		}
		lo, hi := s128(k.s)
		ws := fmt.Sprint(int64(lo))
		if got.head != "ok 1 w0" || got.dump["r0.x"] != k.wx || got.dump["r0.u"] != k.wu || got.dump["r0.s[0]"] != ws || got.dump["r0.s[1]"] != fmt.Sprint(int64(hi)) {
			t.Errorf("C7 %s: %s %v, want x=%s s=%s u=%s", k.name, got.head, got.dump, k.wx, k.ws, k.wu)
		}
	}
	gc := sqEvoGenerate(t, u, nil)
	gc.dropLine(t, "v.X < -5", "out_of_range")
	if got := gc.load(t, "T", file(cases[3])); strings.HasPrefix(got.head, refused("out_of_range")) {
		t.Errorf("the negative control: the dropped int32 check still refuses: %s", got.head)
	}
	gc = sqEvoGenerate(t, u, nil)
	gc.dropLine(t, "v.S[1]", "out_of_range")
	if got := gc.load(t, "T", file(cases[7])); strings.HasPrefix(got.head, refused("out_of_range")) {
		t.Errorf("the negative control: the dropped int128 check still refuses: %s", got.head)
	}

	const cf = "package probe\nfixed table Reading\n{\n    lead uint32 = 1\n    aim float32 = 0.0%s\n    trail uint32 = 2\n}\n"
	old := fixedSynthetic(t, fmt.Sprintf(cf, " | min = -1.0, max = 2.0, resolution = 0.1"))
	if d := ir.TableFixedDefinitionsDigest(old.Tables["Reading"]); !bytes.Contains(d, []byte{'Q'}) {
		t.Errorf("R30: the digest does not carry the resolution under 'Q'")
	}
	gr := sqEvoGenerate(t, old, nil)
	saved := gr.save(t, "Reading", "local v = T.Reading();\nv.Aim = 0.30000001192092896;\nvalues.append(v);\n")
	aimAt := len(saved) - 8
	if binary.LittleEndian.Uint32(saved[aimAt:]) != 0x3e99999a {
		t.Errorf("R30: the compressed float is not saved as its float32 bits: %x", saved[aimAt:])
	}
	for _, k := range []struct {
		bits uint32
		ok   bool
	}{
		{0xc0000000, false}, // -2.0
		{0x40800000, false}, // 4.0
		{0xbf800001, false}, // just below -1.0
		{0xbf800000, true},  // -1.0
		{0x40000000, true},  // 2.0
	} {
		f := sqEvoClone(saved)
		binary.LittleEndian.PutUint32(f[aimAt:], k.bits)
		got := gr.load(t, "Reading", f)
		if k.ok && (got.head != "ok 1 w0" || len(got.saved) == 0 || binary.LittleEndian.Uint32(got.saved[len(got.saved)-8:]) != k.bits) {
			t.Errorf("R30 0x%08x: %s, want it to land and save back as written", k.bits, got.head)
		}
		if !k.ok && !strings.HasPrefix(got.head, refused("out_of_range")) {
			t.Errorf("R30 0x%08x: %s, want out_of_range", k.bits, got.head)
		}
	}
	lin := map[string][]squirreltable.FixedLineageEntry{"Reading": {sqEvoEntry(t, old, "Reading")}}
	for name, src := range map[string]string{"finer": fmt.Sprintf(cf, " | min = -1.0, max = 2.0, resolution = 0.01"), "dropped": fmt.Sprintf(cf, "")} {
		un := fixedSynthetic(t, src)
		if sqEvoEntry(t, un, "Reading").Wire == lin["Reading"][0].Wire {
			t.Errorf("R30 %s: the resolution does not move the hash", name)
		}
		got := sqEvoGenerate(t, un, lin).load(t, "Reading", saved)
		if got.head != "ok 1 w0" || len(got.saved) == 0 || binary.LittleEndian.Uint32(got.saved[len(got.saved)-8:]) != 0x3e99999a {
			t.Errorf("R30 %s: %s, want the old writer's bits unchanged", name, got.head)
		}
	}
}

const sqEvoOrdinalOld = "package probe\nenum Tier { Bronze, Silver }\nunion Pick\n{\n    a int32\n    b int32\n}\nfixed table Lineage\n{\n    tier Tier\n    pick Pick\n    seq int32\n}\n"

// TestSquirrelFixedEvolutionOrdinals is ordinals (C9, C10, R31 under
// strict-only, docs/FIXED-FORM-ALGORITHM.md §4.6): an enum ordinal past the
// writer's variants and a union tag past its arms are forged and REFUSE
// out_of_range — never None — on the identity plan and on a compiled plan
// whose reader declares MORE variants and arms (the writer's count governs, so
// the reader's Gold and c are not reachable from the old writer's 3). Zero
// lands None; full-width ordinals ride at their storage width (an enum and a
// union of 300 lands its 300th, refuses the 301st). The negative controls drop
// the compiled plan's tag and ordinal checks and the identity ordinal check.
func TestSquirrelFixedEvolutionOrdinals(t *testing.T) {
	sqrun.Require(t)
	t.Parallel()
	uOld := fixedSynthetic(t, sqEvoOrdinalOld)
	uNew := fixedSynthetic(t, strings.Replace(strings.Replace(sqEvoOrdinalOld, "Silver }", "Silver, Gold }", 1), "    b int32\n", "    b int32\n    c int32\n", 1))
	eo := sqEvoEntry(t, uOld, "Lineage")
	rec := func(tier, tag byte, arm int32) []byte {
		b := []byte{tier, tag}
		b = binary.LittleEndian.AppendUint32(b, uint32(arm))
		return binary.LittleEndian.AppendUint32(b, 42)
	}
	clean := sqEvoFile(eo.Wire, eo.Layout, rec(2, 2, 9))
	none := sqEvoFile(eo.Wire, eo.Layout, rec(0, 0, 0))
	badTier := sqEvoFile(eo.Wire, eo.Layout, rec(3, 1, 9))
	badTag := sqEvoFile(eo.Wire, eo.Layout, rec(1, 3, 9))
	lin := map[string][]squirreltable.FixedLineageEntry{"Lineage": {eo}}
	for _, name := range []string{"identity", "compiled"} {
		g := sqEvoGenerate(t, uOld, nil)
		if name == "compiled" {
			g = sqEvoGenerate(t, uNew, lin)
		}
		if c := g.load(t, "Lineage", clean); c.head != "ok 1 w0" || c.dump["r0.tier"] != "2" || c.dump["r0.pick.type"] != "2" || c.dump["r0.pick.b"] != "9" || c.dump["r0.seq"] != "42" {
			t.Errorf("%s, the clean record: %s %v", name, c.head, c.dump)
		}
		if c := g.load(t, "Lineage", none); c.head != "ok 1 w0" || c.dump["r0.tier"] != "null" || c.dump["r0.pick.type"] != "0" {
			t.Errorf("%s, zero is None: %s %v", name, c.head, c.dump)
		}
		for fname, f := range map[string][]byte{"C9 an ordinal past the writer's variants": badTier, "C10 a tag past the writer's arms": badTag} {
			if got := g.load(t, "Lineage", f); !strings.HasPrefix(got.head, refused("out_of_range")) {
				t.Errorf("%s, %s: %s %v, want out_of_range", name, fname, got.head, got.dump)
			}
		}
	}
	g := sqEvoGenerate(t, uNew, lin)
	g.dropLine(t, "v.Pick.Type > 2", "out_of_range")
	if got := g.load(t, "Lineage", badTag); strings.HasPrefix(got.head, refused("out_of_range")) {
		t.Errorf("the negative control: the compiled plan without its tag check still refuses: %s", got.head)
	}
	g = sqEvoGenerate(t, uNew, lin)
	g.dropLine(t, "ev > 2", "out_of_range")
	if got := g.load(t, "Lineage", badTier); strings.HasPrefix(got.head, refused("out_of_range")) {
		t.Errorf("the negative control: the compiled plan without its ordinal check still refuses: %s", got.head)
	}
	g = sqEvoGenerate(t, uOld, nil)
	g.dropLine(t, "ev > 2", "out_of_range")
	if got := g.load(t, "Lineage", badTier); strings.HasPrefix(got.head, refused("out_of_range")) {
		t.Errorf("the negative control: the identity plan without its ordinal check still refuses: %s", got.head)
	}

	// FULL WIDTH: 300 variants and 300 arms ride two-byte ordinals.
	var src strings.Builder
	src.WriteString("package probe\nenum Big {")
	for i := range 300 {
		if i > 0 {
			src.WriteString(",")
		}
		fmt.Fprintf(&src, " V%d", i+1)
	}
	src.WriteString(" }\nunion Wide\n{\n")
	for i := range 300 {
		fmt.Fprintf(&src, "    a%d int8\n", i+1)
	}
	src.WriteString("}\nfixed table F\n{\n    e Big\n    w Wide\n}\n")
	uw := fixedSynthetic(t, src.String())
	ew := sqEvoEntry(t, uw, "F")
	if ew.Record != 8+2+3 {
		t.Fatalf("the full-width record is %d bytes, want 13", ew.Record)
	}
	full := func(ord, tag uint16) []byte {
		b := binary.LittleEndian.AppendUint16(nil, ord)
		b = binary.LittleEndian.AppendUint16(b, tag)
		return sqEvoFile(ew.Wire, ew.Layout, append(b, 0x7f))
	}
	gw := sqEvoGenerate(t, uw, nil)
	if got := gw.load(t, "F", full(300, 300)); got.head != "ok 1 w0" || got.dump["r0.e"] != "300" || got.dump["r0.w.type"] != "300" || got.dump["r0.w.a300"] != "127" {
		t.Errorf("the 300th variant and arm: %s %v", got.head, got.dump)
	}
	for name, f := range map[string][]byte{"the 301st variant": full(301, 1), "the 301st arm": full(1, 301), "a variant past a byte's reach": full(0xFFFF, 1)} {
		if got := gw.load(t, "F", f); !strings.HasPrefix(got.head, refused("out_of_range")) {
			t.Errorf("%s: %s, want out_of_range", name, got.head)
		}
	}
}
