package pythontable

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"maps"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v3/internal/check"
	"github.com/mas-bandwidth/schema/v3/internal/codegen/python"
	"github.com/mas-bandwidth/schema/v3/internal/codegen/python/pyrun"
	"github.com/mas-bandwidth/schema/v3/internal/parser"
	"github.com/mas-bandwidth/schema/v3/internal/slowtest"
	"github.com/mas-bandwidth/schema/v3/internal/tablecook"
	"github.com/mas-bandwidth/schema/v3/internal/tabletext"
	"github.com/mas-bandwidth/schema/v3/internal/tablewire"
	"github.com/mas-bandwidth/schema/v3/ir"
)

// M5: Keyed arrays index by key, refused at both ends, and yield (key, value).
func TestPythonKeyedAccessorCoversBothEnds(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "python keyed array accessor under both pins")
	u := loadUnit(t, "package probe\nenum Slot { head, body, feet }\ntable Bag { gear [Slot]int32 }\n")
	files, err := Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	writeTree(t, dir, files)
	probe := pyrun.ImportPrelude + `import probe
bag = probe.Bag()
bag.gear[1] = 10
bag.gear[2] = 20
bag.gear[3] = 30
assert list(bag.gear) == [(1, 10), (2, 20), (3, 30)]
assert bag.gear.items() == [(1, 10), (2, 20), (3, 30)]
for bad in (None, 0, -1, 4, True, False):
    for mode in ("get", "set"):
        try:
            if mode == "get":
                _ = bag.gear[bad]
            else:
                bag.gear[bad] = 99
            raise SystemExit(f"accepted {mode} {bad!r}")
        except KeyError:
            pass
print("ok")
`
	if got := pyBlockRunBoth(t, dir, "probe.py", map[string][]byte{"probe.py": []byte(probe)}); strings.TrimSpace(got) != "ok" {
		t.Fatalf("unexpected output: %q", got)
	}
}

func TestPythonKeyedBoundsNegativeControl(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "python keyed array negative controls")
	u := loadUnit(t, "package probe\nenum Slot { head, body, feet }\ntable Bag { gear [Slot]int32 }\n")
	baseFiles, err := Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name   string
		anchor string
		repl   string
		probe  string
	}{
		{
			name:   "low-guard",
			anchor: "key < 1 or key > 3:",
			repl:   "key < 0 or key > 3:",
			probe:  "import probe\nb = probe.Bag()\n_ = b.gear[0]\nprint('accepted-zero')\n",
		},
		{
			name:   "high-guard",
			anchor: "key < 1 or key > 3:",
			repl:   "key < 1 or key > 4:\n            raise KeyError(key)\n        if key == 4:\n            return 0\n        if False:",
			probe:  "import probe\nb = probe.Bag()\n_ = b.gear[4]\nprint('accepted-past-max')\n",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			files := maps.Clone(baseFiles)
			var modKey string
			for k, v := range files {
				if strings.Contains(string(v), tc.anchor) {
					modKey = k
					break
				}
			}
			if modKey == "" {
				t.Fatalf("anchor %q missing in generated files", tc.anchor)
			}
			files[modKey] = []byte(strings.Replace(string(files[modKey]), tc.anchor, tc.repl, 1))
			dir := t.TempDir()
			writeTree(t, dir, files)
			got := string(pyrun.Run(t, dir, "probe.py", map[string][]byte{"probe.py": []byte(pyrun.ImportPrelude + tc.probe)}))
			if !strings.Contains(got, "accepted-") {
				t.Fatalf("negative control %s did not admit invalid key: %q", tc.name, got)
			}
		})
	}
}

// M12: Byte order is explicit little-endian on the wire, never host-endian.
func scanPythonWireByteOrder(files map[string][]byte) []string {
	structCall := regexp.MustCompile(`struct\.(?:pack|unpack|pack_into|unpack_from)\(\s*(?:f?"([^"]*)"|f?'([^']*)')`)
	bytesOrder := regexp.MustCompile(`\.(?:from_bytes|to_bytes)\([^)]*`)
	var findings []string
	var keys []string
	for k := range files {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, rel := range keys {
		if !strings.HasSuffix(rel, ".py") {
			continue
		}
		isAccel := strings.HasSuffix(rel, "block.py") || strings.HasSuffix(rel, "blockbuild.py") || strings.HasSuffix(rel, "cook.py") || strings.HasSuffix(rel, "cookwrite.py")
		for lineNo, line := range strings.Split(string(files[rel]), "\n") {
			trimmed := strings.TrimSpace(line)
			if strings.HasPrefix(trimmed, "#") {
				continue
			}
			for _, m := range structCall.FindAllStringSubmatch(line, -1) {
				fmtStr := m[1] + m[2]
				if isAccel && (strings.HasPrefix(fmtStr, "{") || strings.HasPrefix(fmtStr, "<") || strings.HasPrefix(fmtStr, ">")) {
					continue
				}
				if !strings.HasPrefix(fmtStr, "<") {
					findings = append(findings, fmt.Sprintf("%s:%d non-little struct format %q", rel, lineNo+1, fmtStr))
				}
			}
			if !isAccel {
				for _, m := range bytesOrder.FindAllString(line, -1) {
					if strings.Contains(m, "sys.byteorder") || strings.Contains(m, `"big"`) || strings.Contains(m, `'big'`) {
						findings = append(findings, fmt.Sprintf("%s:%d non-little byteorder %q", rel, lineNo+1, m))
					}
				}
			}
		}
	}
	return findings
}

func TestPythonWireByteOrderIsExplicit(t *testing.T) {
	t.Parallel()
	rt, err := Runtime()
	if err != nil {
		t.Fatal(err)
	}
	u := loadUnit(t, "package probe\ntype Box { x int32\n y float32 }\n")
	pkt, err := python.Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	all := map[string][]byte{}
	maps.Copy(all, rt)
	for k, v := range pkt {
		all["packet/"+k] = v
	}
	if findings := scanPythonWireByteOrder(all); len(findings) != 0 {
		t.Fatalf("non-explicit byte order in Python wire runtime:\n%s", strings.Join(findings, "\n"))
	}
}

func TestPythonWireByteOrderNegativeControl(t *testing.T) {
	t.Parallel()
	rt, err := Runtime()
	if err != nil {
		t.Fatal(err)
	}
	cp := maps.Clone(rt)
	src := string(cp["_schema/load.py"])
	anchor := `int.from_bytes(view[n - 8 :], "little")`
	if !strings.Contains(src, anchor) {
		t.Fatalf("missing anchor %q in _schema/load.py", anchor)
	}
	cp["_schema/load.py"] = []byte(strings.Replace(src, anchor, `int.from_bytes(view[n - 8 :], sys.byteorder)`, 1))
	findings := scanPythonWireByteOrder(cp)
	if len(findings) != 1 || !strings.Contains(findings[0], "_schema/load.py") {
		t.Fatalf("expected 1 finding on _schema/load.py, got %v", findings)
	}
	cp2 := maps.Clone(rt)
	src2 := string(cp2["_schema/kind_float.py"])
	anchor2 := `struct.unpack("<f", raw)`
	if !strings.Contains(src2, anchor2) {
		t.Fatalf("missing anchor %q in _schema/kind_float.py", anchor2)
	}
	cp2["_schema/kind_float.py"] = []byte(strings.Replace(src2, anchor2, `struct.unpack("=f", raw)`, 1))
	findings2 := scanPythonWireByteOrder(cp2)
	if len(findings2) != 1 || !strings.Contains(findings2[0], "_schema/kind_float.py") {
		t.Fatalf("expected 1 finding on _schema/kind_float.py, got %v", findings2)
	}
}

// M14: Negative control for &node shared-node label in JSON text form.
func TestPythonJsonSharedNodesNegativeControl(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "python json shared-node label negative control")
	u := loadUnit(t, `package jsongraph
table Node { name string(8) }
table Root { a *Node
 b *Node }
`)
	files, err := Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	jt := string(files["jsongraph/_schema/json_table.py"])
	anchor := `if field.codec == "pointer" and counts.get(key, 0) > 1:`
	if !strings.Contains(jt, anchor) {
		t.Fatalf("missing anchor %q", anchor)
	}
	files["jsongraph/_schema/json_table.py"] = []byte(strings.Replace(jt, anchor, "if False:", 1))
	dir := t.TempDir()
	writeTree(t, dir, files)
	probe := pyrun.ImportPrelude + `import jsongraph
n = jsongraph.Node()
n.name = "shared"
r = jsongraph.Root()
r.a = n
r.b = n
print(r.to_json())
`
	got := string(pyrun.Run(t, dir, "probe.py", map[string][]byte{"probe.py": []byte(probe)}))
	if strings.Contains(got, `"&node"`) {
		t.Fatalf("negative control still emitted &node label:\n%s", got)
	}
}

// I7: Negative control for cross-endian order-word refusal on block and cook.
func TestPythonForeignOrderNegativeControl(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "python foreign order-word negative control")
	repo := filepath.Join("..", "..", "..")
	u := loadUnitFiles(t, filepath.Join(repo, "tables", "block"))
	files, err := Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	blk := string(files["blockdemo/_schema/block.py"])
	anchor := "if _u(view, 16, 8) != BYTE_ORDER:"
	if !strings.Contains(blk, anchor) {
		t.Fatalf("missing anchor %q in block.py", anchor)
	}
	files["blockdemo/_schema/block.py"] = []byte(strings.Replace(blk, anchor, "if False:", 1))
	image, err := os.ReadFile(filepath.Join(repo, "testdata", "wire", "tables", "block_render.bin"))
	if err != nil {
		t.Fatal(err)
	}
	swapped := append([]byte(nil), image...)
	binary.LittleEndian.PutUint64(swapped[16:24], 2) // opposite order word with native magic intact
	dir := t.TempDir()
	writeTree(t, dir, files)
	probe := pyrun.ImportPrelude + fmt.Sprintf(`from pathlib import Path
import blockdemo
from blockdemo._schema import block as B
h = B.BlockOpen(blockdemo, "RenderFrame", Path("swapped").read_bytes(), 0x%016x)
print("opened", h is not None)
`, ir.BuildVersion(u))
	out := string(pyrun.Run(t, dir, "probe.py", map[string][]byte{"probe.py": []byte(probe), "swapped": swapped}))
	if !strings.Contains(out, "opened True") {
		t.Fatalf("negative control did not open opposite-order block: %q", out)
	}
}

// I3: Forgery fuzzing over block and cook images, walking every opened handle.
func pySplitMix64(state *uint64) uint64 {
	*state += 0x9e3779b97f4a7c15
	z := *state
	z = (z ^ (z >> 30)) * 0xbf58476d1ce4e5b9
	z = (z ^ (z >> 27)) * 0x94d049bb133111eb
	return z ^ (z >> 31)
}

func TestPythonForgeryFuzz(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "python block and cook forgery walk fuzz")
	repo := filepath.Join("..", "..", "..")
	bu := loadUnitFiles(t, filepath.Join(repo, "tables", "block"))
	bFiles, err := Generate(bu)
	if err != nil {
		t.Fatal(err)
	}
	blockImg, err := os.ReadFile(filepath.Join(repo, "testdata", "wire", "tables", "block_render.bin"))
	if err != nil {
		t.Fatal(err)
	}
	cu := loadUnit(t, "package cookfuzz\ntable Node { x int32\n next *Node }\ntable Root { head *Node\n label string(16) }\n")
	cm := tabletext.NewModel(cu)
	var tr tabletext.Report
	inst := cm.New(cm.Lookup("Root"))
	if !cm.Read(inst, []byte(`{"label":"hello","head":{"x":7,"next":{"x":9}}}`), &tr) {
		t.Fatalf("cm.Read: %+v", tr)
	}
	cookImg, err := tablecook.Cook(cm, inst, tablecook.Options{})
	if err != nil {
		t.Fatal(err)
	}
	cFiles, err := Generate(cu)
	if err != nil {
		t.Fatal(err)
	}

	bDir := t.TempDir()
	writeTree(t, bDir, bFiles)
	cDir := t.TempDir()
	writeTree(t, cDir, cFiles)

	seed := uint64(0x5059464f52474533)
	bInputs := map[string][]byte{}
	cInputs := map[string][]byte{}
	const mutants = 250
	for i := range mutants {
		bm := append([]byte(nil), blockImg...)
		op := pySplitMix64(&seed) % 4
		switch op {
		case 0:
			cut := int(pySplitMix64(&seed) % uint64(len(bm)))
			bm = bm[:cut]
		case 1:
			pos := int(pySplitMix64(&seed) % uint64(len(bm)))
			bm[pos] ^= byte(1 << (pySplitMix64(&seed) % 8))
		case 2:
			off := 24 + int(pySplitMix64(&seed)%32)
			if off+8 <= len(bm) {
				binary.LittleEndian.PutUint64(bm[off:off+8], pySplitMix64(&seed)%uint64(len(bm)*4))
			}
		default:
			bm = append(bm, 0, 0, 0, 0)
		}
		bInputs[fmt.Sprintf("b%d", i)] = bm

		cmut := append([]byte(nil), cookImg...)
		cop := pySplitMix64(&seed) % 4
		switch cop {
		case 0:
			cut := int(pySplitMix64(&seed) % uint64(len(cmut)))
			cmut = cmut[:cut]
		case 1:
			pos := int(pySplitMix64(&seed) % uint64(len(cmut)))
			cmut[pos] ^= byte(1 << (pySplitMix64(&seed) % 8))
		case 2:
			if len(cmut) >= 48 {
				binary.LittleEndian.PutUint64(cmut[32:40], pySplitMix64(&seed)%1024)
			}
		default:
			cmut = append(cmut, 0xff)
		}
		_, cerr := tablecook.Check(cm, cmut)
		cInputs[fmt.Sprintf("c%d", i)] = cmut
		if cerr == nil {
			cInputs[fmt.Sprintf("ok%d", i)] = []byte("1")
		} else {
			cInputs[fmt.Sprintf("ok%d", i)] = []byte("0")
		}
	}

	bProbe := pyrun.ImportPrelude + fmt.Sprintf(`from pathlib import Path
import blockdemo
from blockdemo import Refused
from blockdemo._schema import block as B
ver = 0x%016x
opened = 0
refused = 0
for i in range(%d):
    data = Path(f"b{i}").read_bytes()
    try:
        h = B.BlockOpen(blockdemo, "RenderFrame", data, ver)
    except Refused:
        refused += 1
        continue
    assert h.used <= len(data), f"b{i}: used {h.used} > len {len(data)}"
    _ = B.Dump(h)
    opened += 1
print(f"ok {opened} {refused}")
`, ir.BuildVersion(bu), mutants)
	if out := func() string {
		bInputs["probe.py"] = []byte(bProbe)
		return pyBlockRunBoth(t, bDir, "probe.py", bInputs)
	}(); !strings.HasPrefix(strings.TrimSpace(out), "ok ") {
		t.Fatalf("block forgery fuzz failed: %q", out)
	}

	cProbe := pyrun.ImportPrelude + fmt.Sprintf(`from pathlib import Path
import cookfuzz
from cookfuzz import Refused
from cookfuzz._schema import cook as C
opened = 0
refused = 0
for i in range(%d):
    data = Path(f"c{i}").read_bytes()
    want_ok = Path(f"ok{i}").read_text() == "1"
    try:
        C.check_cook(cookfuzz.Root, data)
        h = C.open_cook(cookfuzz.Root, data)
        _ = C.dump_cook(h)
        got_ok = True
        opened += 1
    except Refused as exc:
        got_ok = False
        why = str(exc)
        refused += 1
    if got_ok:
        assert want_ok, f"c{i}: python admitted cook that tablecook.Check refused"
    elif want_ok:
        assert "padding" in why or "slack" in why or "tail" in why, f"c{i}: unexpected python refusal on oracle-valid cook: {why}"
print(f"ok {opened} {refused}")
`, mutants)
	if out := func() string {
		cInputs["probe.py"] = []byte(cProbe)
		return pyBlockRunBoth(t, cDir, "probe.py", cInputs)
	}(); !strings.HasPrefix(strings.TrimSpace(out), "ok ") {
		t.Fatalf("cook forgery fuzz failed: %q", out)
	}
}

func TestPythonForgeryFuzzNegativeControl(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "python block forgery fuzz negative control")
	repo := filepath.Join("..", "..", "..")
	bu := loadUnitFiles(t, filepath.Join(repo, "tables", "block"))
	files, err := Generate(bu)
	if err != nil {
		t.Fatal(err)
	}
	blk := string(files["blockdemo/_schema/block.py"])
	a1 := "if offset_of > len(view):"
	a2 := "if rows > len(view) - offset_of:"
	a3 := "if padding > len(view) - used:"
	if !strings.Contains(blk, a1) || !strings.Contains(blk, a2) || !strings.Contains(blk, a3) {
		t.Fatal("missing block extent anchors")
	}
	blk = strings.Replace(blk, a1, "if False:", 1)
	blk = strings.Replace(blk, a2, "if False:", 1)
	blk = strings.Replace(blk, a3, "if False:", 1)
	files["blockdemo/_schema/block.py"] = []byte(blk)
	blockImg, err := os.ReadFile(filepath.Join(repo, "testdata", "wire", "tables", "block_render.bin"))
	if err != nil {
		t.Fatal(err)
	}
	short := blockImg[:len(blockImg)-32]
	dir := t.TempDir()
	writeTree(t, dir, files)
	probe := pyrun.ImportPrelude + fmt.Sprintf(`from pathlib import Path
import blockdemo
from blockdemo._schema import block as B
data = Path("short").read_bytes()
h = B.BlockOpen(blockdemo, "RenderFrame", data, 0x%016x)
print("admitted-over-extent", h.used > len(data))
`, ir.BuildVersion(bu))
	out := string(pyrun.Run(t, dir, "probe.py", map[string][]byte{"probe.py": []byte(probe), "short": short}))
	if !strings.Contains(out, "admitted-over-extent True") {
		t.Fatalf("negative control did not catch truncated block admission: %q", out)
	}
}

// I13: Text differential against the Go engine over 200 random float instances.
func runPythonTextDifferential(t *testing.T, patch func(map[string][]byte)) error {
	t.Helper()
	u := loadUnit(t, `package floatdiff
table Sample {
    f32 float32
    f64 float64
    arr [4]float32
    tag int32
}
`)
	files, err := Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	if patch != nil {
		patch(files)
	}
	dir := t.TempDir()
	writeTree(t, dir, files)
	m := tabletext.NewModel(u)
	st := m.Lookup("Sample")
	seed := uint64(0x5059544558544446)
	const count = 200
	inputs := map[string][]byte{}
	wantTexts := make([]string, count)
	wantWires := make([][]byte, count)
	for i := range count {
		var f32 float32
		for {
			b := uint32(pySplitMix64(&seed))
			cand := math.Float32frombits(b)
			if !math.IsNaN(float64(cand)) && !math.IsInf(float64(cand), 0) {
				f32 = cand
				break
			}
		}
		var f64 float64
		for {
			b := pySplitMix64(&seed)
			cand := math.Float64frombits(b)
			if !math.IsNaN(cand) && !math.IsInf(cand, 0) {
				f64 = cand
				break
			}
		}
		var a [4]float32
		for k := range 4 {
			for {
				b := uint32(pySplitMix64(&seed))
				cand := math.Float32frombits(b)
				if !math.IsNaN(float64(cand)) && !math.IsInf(float64(cand), 0) {
					a[k] = cand
					break
				}
			}
		}
		tag := int32(pySplitMix64(&seed) % 1000)
		js := fmt.Sprintf(`{"f32":%v,"f64":%v,"arr":[%v,%v,%v,%v],"tag":%d}`,
			f32, f64, a[0], a[1], a[2], a[3], tag)
		var rep tabletext.Report
		inst := m.New(st)
		if !m.Read(inst, []byte(js), &rep) {
			t.Fatalf("sample %d m.Read: %+v", i, rep)
		}
		canonText, err := m.Write(inst)
		if err != nil {
			t.Fatalf("sample %d m.Write: %v", i, err)
		}
		wire, err := tablewire.Encode(m, inst)
		if err != nil {
			t.Fatalf("sample %d Encode: %v", i, err)
		}
		wantTexts[i] = string(canonText) + "\n"
		wantWires[i] = wire
		inputs[fmt.Sprintf("w%d", i)] = wire
	}
	probe := pyrun.ImportPrelude + fmt.Sprintf(`from pathlib import Path
import floatdiff
for i in range(%d):
    w = Path(f"w{i}").read_bytes()
    v = floatdiff.Sample.load(w)
    txt = v.to_json()
    v2 = floatdiff.Sample.from_json(txt)
    w2 = v2.save()
    print(f"HEX {i} {txt.encode('utf-8').hex()} {w2.hex()}")
`, count)
	out := string(func() []byte { inputs["probe.py"] = []byte(probe); return pyrun.Run(t, dir, "probe.py", inputs) }())
	lines := strings.Split(strings.TrimSpace(out), "\n")
	if len(lines) != count {
		return fmt.Errorf("got %d lines, want %d", len(lines), count)
	}
	for i, ln := range lines {
		parts := strings.Fields(ln)
		if len(parts) != 4 {
			return fmt.Errorf("line %d malformed: %q", i, ln)
		}
		wantTxtHex := fmt.Sprintf("%x", []byte(wantTexts[i]))
		wantWireHex := fmt.Sprintf("%x", wantWires[i])
		if parts[2] != wantTxtHex {
			return fmt.Errorf("sample %d text mismatch: got %s, want %s", i, parts[2], wantTxtHex)
		}
		if parts[3] != wantWireHex {
			return fmt.Errorf("sample %d wire mismatch: got %s, want %s", i, parts[3], wantWireHex)
		}
	}
	return nil
}

func TestPythonTextDifferential(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "python 200-draw float text differential against Go engine")
	if err := runPythonTextDifferential(t, nil); err != nil {
		t.Fatal(err)
	}
}

func TestPythonTextDifferentialNegativeControl(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "python float text differential negative control")
	err := runPythonTextDifferential(t, func(files map[string][]byte) {
		jt := string(files["floatdiff/_schema/json_table.py"])
		anchor := "low, high = (6, 9) if single else (15, 17)"
		if !strings.Contains(jt, anchor) {
			t.Fatalf("missing anchor %q", anchor)
		}
		files["floatdiff/_schema/json_table.py"] = []byte(strings.Replace(jt, anchor, "low, high = (6, 6) if single else (15, 17)", 1))
	})
	if err == nil {
		t.Fatal("negative control passed with float32 capped at 6 digits")
	}
}

// J2: Runtime home stays at <package>/_schema/*.py byte-identical when an earlier-sorting schema file joins.
func checkPythonRuntimeHome(baseFiles, joinedFiles map[string][]byte, pkg string) []string {
	var findings []string
	prefix := pkg + "/_schema/"
	for k, bv := range baseFiles {
		if !strings.HasPrefix(k, prefix) {
			continue
		}
		base := filepath.Base(k)
		// unit_view.py and message_unit.py carry the unit's declarations; static runtime modules must stay byte-identical.
		if base == "unit_view.py" || base == "message_unit.py" || base == "_message_unit.py" || base == "retain_known.py" {
			if _, ok := joinedFiles[k]; !ok {
				findings = append(findings, "missing unit module "+k)
			}
			continue
		}
		jv, ok := joinedFiles[k]
		if !ok {
			findings = append(findings, "relocated or missing runtime file "+k)
			continue
		}
		if !bytes.Equal(bv, jv) {
			findings = append(findings, "runtime file changed when earlier file joined: "+k)
		}
	}
	return findings
}

func loadSchemaSourceFiles(t *testing.T, extra map[string]string) *ir.Unit {
	t.Helper()
	repo := filepath.Join("..", "..", "..")
	matches, err := filepath.Glob(filepath.Join(repo, "tables", "examples", "*.schema"))
	if err != nil {
		t.Fatal(err)
	}
	for name := range extra {
		matches = append(matches, name)
	}
	sort.Strings(matches)
	var sfs []check.SourceFile
	for _, p := range matches {
		var data []byte
		if content, ok := extra[p]; ok {
			data = []byte(content)
		} else {
			data, err = os.ReadFile(p)
			if err != nil {
				t.Fatal(err)
			}
		}
		base := filepath.Base(p)
		tree, perrs := parser.Parse(base, data)
		if len(perrs) != 0 {
			t.Fatalf("parse %s: %v", base, perrs)
		}
		sfs = append(sfs, check.SourceFile{Path: p, Name: base, Base: strings.TrimSuffix(base, ".schema"), Bytes: data, AST: tree})
	}
	u, cerrs := check.Unit(sfs)
	if len(cerrs) != 0 {
		t.Fatalf("check: %v", cerrs)
	}
	return u
}

func TestPythonRuntimeHome(t *testing.T) {
	t.Parallel()
	uBase := loadSchemaSourceFiles(t, nil)
	baseFiles, err := Generate(uBase)
	if err != nil {
		t.Fatal(err)
	}
	uJoined := loadSchemaSourceFiles(t, map[string]string{
		"Aaa.schema": "package tabledemo\ntable AaaExtra { x int32 }\n",
	})
	joinedFiles, err := Generate(uJoined)
	if err != nil {
		t.Fatal(err)
	}
	if findings := checkPythonRuntimeHome(baseFiles, joinedFiles, uBase.Package); len(findings) != 0 {
		t.Fatalf("runtime home drifted:\n%s", strings.Join(findings, "\n"))
	}
	if _, ok := joinedFiles["tabledemo/Aaa.py"]; !ok {
		t.Fatal("joining Aaa.schema did not emit tabledemo/Aaa.py")
	}
}

func TestPythonRuntimeHomeNegativeControl(t *testing.T) {
	t.Parallel()
	uBase := loadSchemaSourceFiles(t, nil)
	baseFiles, err := Generate(uBase)
	if err != nil {
		t.Fatal(err)
	}
	uJoined := loadSchemaSourceFiles(t, map[string]string{
		"Aaa.schema": "package tabledemo\ntable AaaExtra { x int32 }\n",
	})
	joinedFiles, err := Generate(uJoined)
	if err != nil {
		t.Fatal(err)
	}
	// Plant a file-order rule relocating load.py under the first schema file's stem.
	relocated := maps.Clone(joinedFiles)
	relocated["tabledemo/_aaa_schema/load.py"] = relocated["tabledemo/_schema/load.py"]
	delete(relocated, "tabledemo/_schema/load.py")
	findings := checkPythonRuntimeHome(baseFiles, relocated, uBase.Package)
	if len(findings) == 0 {
		t.Fatal("negative control missed relocated runtime file")
	}
}
