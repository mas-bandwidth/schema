package jstable

// I13 (docs/PORTING.md:1278) is the text differential against a third
// implementation: the leg emits random instances as (wire, text), the
// compiler's own engine (internal/tabletext and internal/tablepack, written
// from the spec and from neither backend) reads the same wire and writes its
// text, the two are byte-compared, then the engine packs the leg's text back to
// wire and compares that. This is the JavaScript leg's cell, on the Go row's
// pattern (internal/codegen/gotable/textdifferential_test.go).
//
// The engine and the emitted writer share NO code: the engine's float loop is
// internal/tabletext/write.go's writeFloat/formatG over strconv, the leg's is
// TableVariableFloatText/TableVariableFormatG, emitted JavaScript running in
// node. A pinned corpus never reaches a float tie, so the random source is
// built here: a splitmix64 written into the probe body, the seed and the count
// passed by environment. Non-finite floats have no JSON spelling and both
// writers REFUSE rather than lose one, so the generator redraws until a
// pattern is finite. Instance 0 is the fixture proof: a reset Root and nothing
// else.
//
// A node subprocess over a generated module is a toolchain test, run through
// jsNode(t) and the make target tables-js-text-differential: no real time, no
// sockets, no network.

import (
	"bytes"
	"fmt"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/tablepack"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
	"github.com/mas-bandwidth/schema/v2/ir"
)

const jsTextDifferentialSchema = `package probe
type Sample {
 s float32
 d float64
}
fixed table Root {
 a float32
 b float64
 pair Sample
 many [..8]float32
 deep [..8]float64
}
`

// jsTextDifferentialProbe generates N random float-only instances with the
// generated storage class and writes each instance's wire (RootSave) and text
// (RootToJson). The seed, the count and the directory arrive through the
// environment so the outer test owns them. The splitmix64 is the Go row's, in
// BigInt, so the two legs draw the same patterns in the same order.
const jsTextDifferentialProbe = `import { writeFileSync } from "node:fs";
import { Root, RootSave, RootToJson } from "./ProbeVariable.js";

const dir = process.env.SCHEMA_TEXT_DIFF_DIR;
if (!dir) { console.error("SCHEMA_TEXT_DIFF_DIR is unset"); process.exit(2); }
const seedText = process.env.SCHEMA_TEXT_DIFF_SEED || "";
const nText = process.env.SCHEMA_TEXT_DIFF_N || "";
if (!/^[0-9]+$/.test(seedText)) { console.error("SCHEMA_TEXT_DIFF_SEED: " + JSON.stringify(seedText)); process.exit(2); }
if (!/^[0-9]+$/.test(nText)) { console.error("SCHEMA_TEXT_DIFF_N: " + JSON.stringify(nText)); process.exit(2); }

const M64 = (1n << 64n) - 1n;
let state = BigInt(seedText) & M64;
function next() {
  state = (state + 0x9E3779B97F4A7C15n) & M64;
  let z = state;
  z = ((z ^ (z >> 30n)) * 0xBF58476D1CE4E5B9n) & M64;
  z = ((z ^ (z >> 27n)) * 0x94D049BB133111EBn) & M64;
  return z ^ (z >> 31n);
}

const view = new DataView(new ArrayBuffer(8));
function nextFloat32() {
  for (;;) {
    view.setUint32(0, Number(next() & 0xFFFFFFFFn));
    const f = view.getFloat32(0);
    if (Number.isFinite(f)) { return f; }
  }
}
function nextFloat64() {
  for (;;) {
    view.setBigUint64(0, next());
    const f = view.getFloat64(0);
    if (Number.isFinite(f)) { return f; }
  }
}

const n = Number(nText);
for (let i = 0; i < n; i++) {
  const v = new Root();
  if (i !== 0) {
    v.a = nextFloat32();
    v.b = nextFloat64();
    v.pair.s = nextFloat32();
    v.pair.d = nextFloat64();
    v.many = [];
    for (let j = 0; j < 8; j++) { v.many.push(nextFloat32()); }
    v.deep = [];
    for (let j = 0; j < 8; j++) { v.deep.push(nextFloat64()); }
  }
  const wire = RootSave(v);
  if (wire === null || wire === undefined) { console.error("instance " + i + ": RootSave refused"); process.exit(1); }
  writeFileSync(dir + "/inst-" + i + ".wire", wire);
  const text = RootToJson(v);
  if (text === null || text === undefined) { console.error("instance " + i + ": RootToJson refused"); process.exit(1); }
  writeFileSync(dir + "/inst-" + i + ".json", text);
}
console.log("probe wrote " + n + " instances");
`

const (
	jsTextDifferentialSeed = 24845619678
	jsTextDifferentialN    = 200
)

// runJSTextDifferential generates the probe unit with the JavaScript target,
// applies an optional in-memory edit of the emitted files, runs the node probe
// and then the engine comparison. It returns the divergence messages and a
// non-nil error when any instance differs, so the negative control asserts on
// the same output the row itself produces.
func runJSTextDifferential(t *testing.T, edit func(map[string][]byte)) ([]byte, error) {
	t.Helper()
	node := jsNode(t)
	u := unitFrom(t, jsTextDifferentialSchema)
	files, err := generateVariableFiles(u, nil)
	if err != nil {
		t.Fatalf("the JavaScript target refused the probe unit: %v", err)
	}
	home := runtimeHome(u) + "Variable.js"
	if _, ok := files[home]; !ok {
		t.Fatalf("no %s was generated for the probe unit", home)
	}
	if edit != nil {
		edit(files)
	}
	work := t.TempDir()
	for name, data := range files {
		if err := os.WriteFile(filepath.Join(work, filepath.Base(name)), data, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	script := filepath.Join(work, "textdiff.mjs")
	if err := os.WriteFile(script, []byte(jsTextDifferentialProbe), 0o600); err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(work, "out")
	if err := os.Mkdir(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(node, script)
	cmd.Dir = work
	cmd.Env = append(os.Environ(),
		"SCHEMA_TEXT_DIFF_DIR="+dir,
		"SCHEMA_TEXT_DIFF_SEED="+strconv.FormatUint(jsTextDifferentialSeed, 10),
		"SCHEMA_TEXT_DIFF_N="+strconv.Itoa(jsTextDifferentialN),
	)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return out, fmt.Errorf("the node probe did not run: %w", err)
	}
	m := tabletext.NewModel(u)
	msgs, compared, err := compareJSTextDifferential(m, dir, jsTextDifferentialN)
	if err != nil {
		return out, err
	}
	if len(msgs) > 0 {
		return []byte(strings.Join(msgs, "\n\n")), fmt.Errorf("%d of %d instance(s) differ", len(msgs), jsTextDifferentialN)
	}
	return append(out, fmt.Sprintf("%d instances compared in both directions\n", compared)...), nil
}

// TestTextDifferentialJavaScript is the row: the emitted JavaScript float
// writer and the engine's float writer agree, byte for byte, over 200 random
// float-only instances, text from wire and wire from text.
func TestTextDifferentialJavaScript(t *testing.T) {
	t.Parallel()
	out, err := runJSTextDifferential(t, nil)
	if err != nil {
		t.Fatalf("text differential: %v\n%s", err, out)
	}
	t.Logf("%s", out)
}

// jsTextDifferentialAnchor is the line of TableVariableFloatText the negative
// control rewrites: the precision window of the shortest-round-trip loop.
const jsTextDifferentialAnchor = "const low = single ? 6 : 15, high = single ? 9 : 17;"

// TestTextDifferentialNegativeControlJavaScript caps a float32's text at SIX
// significant digits, so a float32 that needs seven, eight or nine renders
// short and only the cross-implementation comparison sees it. The gate must go
// red on the text comparison, not on a compile or runtime error.
func TestTextDifferentialNegativeControlJavaScript(t *testing.T) {
	t.Parallel()
	out, err := runJSTextDifferential(t, func(files map[string][]byte) {
		total := 0
		for _, data := range files {
			total += strings.Count(string(data), jsTextDifferentialAnchor)
		}
		if total != 1 {
			t.Fatalf("NEGATIVE CONTROL FAILED: the sabotage anchor %q appeared %d times across the emitted files, want 1", jsTextDifferentialAnchor, total)
		}
		for name, data := range files {
			files[name] = bytes.Replace(data, []byte(jsTextDifferentialAnchor),
				[]byte("const low = single ? 6 : 15, high = single ? 6 : 17; /* SABOTAGED */"), 1)
		}
	})
	if err == nil {
		t.Fatalf("NEGATIVE CONTROL FAILED: a float32 writer capped at six significant digits left the gate GREEN\n%s", out)
	}
	if !bytes.Contains(out, []byte("leg text and engine text differ")) {
		t.Fatalf("NEGATIVE CONTROL FAILED: the gate went red, but not on the text comparison: %v\n%s", err, out)
	}
	t.Logf("the control is red on the text comparison, as it must be: %v", err)
}

// compareJSTextDifferential runs both directions of the engine comparison for
// every instance the probe wrote. Direction one: the engine reads the wire and
// writes its text, byte-compared against the leg's text. Direction two: the
// engine packs the leg's text back to wire, byte-compared against the leg's
// wire. Divergences are collected as messages carrying the float bits of the
// instance, so a failure replays from the bits alone. The engine-side walk
// (textWindow, floatBitsOf, the UnpackOneFile/Pack calls) is the Go row's.
func compareJSTextDifferential(m *tabletext.Model, dir string, n int) ([]string, int, error) {
	var msgs []string
	compared := 0
	for i := range n {
		wire, err := os.ReadFile(filepath.Join(dir, fmt.Sprintf("inst-%d.wire", i)))
		if err != nil {
			return msgs, compared, fmt.Errorf("read inst-%d.wire: %w", i, err)
		}
		legJSON, err := os.ReadFile(filepath.Join(dir, fmt.Sprintf("inst-%d.json", i)))
		if err != nil {
			return msgs, compared, fmt.Errorf("read inst-%d.json: %w", i, err)
		}
		engineJSON, err := engineTextOf(m, wire, dir)
		if err != nil {
			return msgs, compared, fmt.Errorf("instance %d: %w", i, err)
		}
		if !bytes.Equal(engineJSON, legJSON) {
			off, win := jsTextWindow(legJSON, engineJSON)
			msgs = append(msgs, fmt.Sprintf("instance %d: leg text and engine text differ at byte %d\n%s\nfloats: %s", i, off, win, jsFloatBitsOf(m, wire)))
			continue
		}
		wireBack, err := engineWireOf(m, legJSON, dir)
		if err != nil {
			return msgs, compared, fmt.Errorf("instance %d: %w", i, err)
		}
		if !bytes.Equal(wireBack, wire) {
			msgs = append(msgs, fmt.Sprintf("instance %d: engine wire packed from leg text differs from leg wire: engine=%x leg=%x\nfloats: %s", i, wireBack, wire, jsFloatBitsOf(m, wire)))
			continue
		}
		compared++
	}
	if len(msgs) == 0 && compared != n {
		return msgs, compared, fmt.Errorf("%d of %d instances compared", compared, n)
	}
	return msgs, compared, nil
}

// engineTextOf is direction one's engine half: tablepack.UnpackOneFile reads
// the wire and writes Root.json, which must come back with a clean report.
func engineTextOf(m *tabletext.Model, wire []byte, dir string) ([]byte, error) {
	d, err := os.MkdirTemp(dir, "d1-")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(d)
	report, err := tablepack.UnpackOneFile(m, "Root", wire, d)
	if err != nil {
		return nil, fmt.Errorf("UnpackOneFile: %w", err)
	}
	if report != (tabletext.Report{}) {
		return nil, fmt.Errorf("UnpackOneFile report not clean: %+v", report)
	}
	text, err := os.ReadFile(filepath.Join(d, "Root.json"))
	if err != nil {
		return nil, fmt.Errorf("read engine Root.json: %w", err)
	}
	return text, nil
}

// engineWireOf is direction two's engine half: tablepack.Pack reads the leg's
// text as Root.json and packs it to wire, with a clean report.
func engineWireOf(m *tabletext.Model, text []byte, dir string) ([]byte, error) {
	d, err := os.MkdirTemp(dir, "d2-")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(d)
	if err := os.WriteFile(filepath.Join(d, "Root.json"), text, 0o600); err != nil {
		return nil, err
	}
	wire, _, report, err := tablepack.Pack(m, "Root", d)
	if err != nil {
		return nil, fmt.Errorf("Pack: %w", err)
	}
	if report != (tabletext.Report{}) {
		return nil, fmt.Errorf("Pack report not clean: %+v", report)
	}
	return wire, nil
}

// jsFloatBitsOf decodes one wire with the engine and spells every float
// field's bits, so a divergence replays from the bits alone.
func jsFloatBitsOf(m *tabletext.Model, wire []byte) string {
	inst := m.New(m.Lookup("Root"))
	var report tabletext.Report
	if _, err := tablewire.Decode(m, inst, wire, &report); err != nil {
		return fmt.Sprintf("<decode error: %v>", err)
	}
	var b strings.Builder
	var walk func(fields []tabletext.Field, prefix string)
	walk = func(fields []tabletext.Field, prefix string) {
		for i := range fields {
			f := &fields[i]
			name := prefix + f.Def.Name
			emit := func(c tabletext.Cell, slot string) {
				switch f.Def.Type.Kind {
				case ir.TFloat32:
					fmt.Fprintf(&b, "%s=0x%08x(f32) ", slot, math.Float32bits(float32(c.F)))
				case ir.TFloat64:
					fmt.Fprintf(&b, "%s=0x%016x(f64) ", slot, math.Float64bits(c.F))
				}
			}
			if len(f.Elems) > 0 {
				for j := 0; j < f.Count && j < len(f.Elems); j++ {
					c := f.Elems[j]
					if c.Tab != nil {
						walk(c.Tab.Fields, fmt.Sprintf("%s[%d].", name, j))
					} else {
						emit(c, fmt.Sprintf("%s[%d]", name, j))
					}
				}
				continue
			}
			if f.Cell.Tab != nil {
				walk(f.Cell.Tab.Fields, name+".")
				continue
			}
			emit(f.Cell, name)
		}
	}
	walk(inst.Fields, "")
	return strings.TrimSpace(b.String())
}

// jsTextWindow returns the first byte offset where two texts differ and a
// window of each around it.
func jsTextWindow(a, b []byte) (int, string) {
	n := min(len(b), len(a))
	i := 0
	for i < n && a[i] == b[i] {
		i++
	}
	lo := max(i-32, 0)
	hiA := min(i+32, len(a))
	hiB := min(i+32, len(b))
	return i, fmt.Sprintf("leg[%d:%d]=%q engine[%d:%d]=%q", lo, hiA, a[lo:hiA], lo, hiB, b[lo:hiB])
}
