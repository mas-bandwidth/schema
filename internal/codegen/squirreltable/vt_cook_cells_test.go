package squirreltable_test

// vt/cook-write, vt/cook-open, vt/block-build, vt/reflection, vt/unit-registry
// (docs/roadmap-evidence/squirrel.sexp): the strict read (ReadStrict / FromJson:
// one read returns the whole value or refuses the whole load) combined with
// the cook-write, cook-open, block-build, reflection, and unit-registry
// surfaces.
//
// Functional: runs the pinned Squirrel interpreter via sqrun under
// slowtest.Gate; the source gate takes no real time, sockets, or network.

import (
	"bytes"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/squirrel/sqrun"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/internal/tablecook"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
	"github.com/mas-bandwidth/schema/v2/ir"
)

const vtCookCellsSchema = `package cookcells

fixed table Row
{
    x int32
    y int32
}

table Root
{
    id int32 = 1 | min = 1, max = 100
    tag string(16)
    ok bool
    rows [..4]Row
}

fixed table Frame
{
    version int32
    rows [..8]Row
}
`

type vtCookArtifacts struct {
	cookNut      string
	cookWriteNut string
	blockBuild   string
	viewGo       string
	tableGo      string
	driverNut    string
}

func readVtCookArtifacts(t *testing.T) vtCookArtifacts {
	t.Helper()
	return vtCookArtifacts{
		cookNut:      squirrelRepoFile(t, "internal", "codegen", "squirreltable", "runtime", "cook.nut"),
		cookWriteNut: squirrelRepoFile(t, "internal", "codegen", "squirreltable", "runtime", "cookwrite.nut"),
		blockBuild:   squirrelRepoFile(t, "internal", "codegen", "squirreltable", "runtime", "blockbuild.nut"),
		viewGo:       squirrelRepoFile(t, "internal", "codegen", "squirreltable", "view.go"),
		tableGo:      squirrelRepoFile(t, "internal", "codegen", "squirreltable", "squirreltable.go"),
		driverNut:    squirrelRepoFile(t, "test", "conformance", "squirrel", "main.nut"),
	}
}

func vtCookCellsFindings(a vtCookArtifacts) []string {
	var out []string
	checks := []struct {
		src, name, want string
	}{
		{a.cookNut, "cook-open build version", `if (!eqB(bytes, 8, T.BuildVersionBytes)) return refuse(report, "wrong_build_version");`},
		{a.cookNut, "cook-open part lengths", `if (attribution != bytes.len() - dataOffset - dataLength) return refuse(report, "truncated");`},
		{a.cookWriteNut, "cook-write entry", `_M.Cook <- function(T, save, root, value, buf, order) {`},
		{a.blockBuild, "block-build bounds", `if (typeof c != "integer" || c < 0 || c > a.max) {`},
		{a.viewGo, "reflection TableInfo", `_M.%sTableInfo <- {`},
		{a.tableGo, "unit-registry Roots and Descriptors", `_M.Roots <- [`},
	}
	for _, c := range checks {
		if !strings.Contains(c.src, c.want) {
			out = append(out, fmt.Sprintf("%s: missing %q", c.name, c.want))
		}
	}
	cookBody, ok := squirrelSurface(a.driverNut, `if (mode == "cook-write")`)
	if !ok {
		out = append(out, "driver: missing cook-write surface")
	} else {
		if !strings.Contains(cookBody, "strictLoad(") || !strings.Contains(cookBody, "FromJson(") {
			out = append(out, "driver cook-write: missing strictLoad/FromJson")
		}
		if strings.Contains(cookBody, "loadWalk") {
			out = append(out, "driver cook-write: still contains loadWalk")
		}
	}
	return out
}

const vtCookCellsProbe = `local inputs = dofile("inputs.nut", true);
local schema = dofile("cookcells/schema_module.nut", true);
local T = schema.require("CookcellsTables");
local R = schema.require("schema_table_refusal");
local J = schema.require("schema_table_json");

local HEXB = array(256);
for (local i = 0; i < 256; i++) HEXB[i] = format("%02x", i);
local hexOf = function(b, n = -1) {
  if (b == null) return "null";
  local lim = n < 0 ? b.len() : n;
  local s = "";
  for (local i = 0; i < lim; i++) s += HEXB[b[i] & 0xff];
  return s;
};

// 1. unit-registry: Roots, Descriptors, make()
local roots = "";
foreach (i, r in T.Roots) roots += (i > 0 ? "," : "") + r;
local madeRoot = T.Descriptors.Root.make();
local regOk = (roots == "Row,Root,Frame") && ("Root" in T.Descriptors) && ("Row" in T.Descriptors) && ("Frame" in T.Descriptors) && (typeof madeRoot == "instance") && (madeRoot[T.Descriptors.Root.fields[0].member] == 1);
print("unit-registry|" + (regOk ? "ok" : "bad:" + roots) + "\n");

// 2. reflection: RootTableInfo and RootTableFields
local info = T.RootTableInfo;
local f0 = info.fields[0];
local f3 = info.fields[3];
local reflOk = (info.name == "Root") && (info.table == true) && (info.variable == true) && (info.numFields == 4) &&
  (f0.name == "id") && (f0.hasRange == true) && (f0.rangeIntMin == 1) && (f0.rangeIntMax == 100) &&
  (f3.name == "rows") && (f3.isArray == true) && (f3.counted == true) && (f3.arrayBound == 4);
print("reflection|" + (reflOk ? "ok" : "bad") + "\n");

// 3. strict read (ReadStrict + FromJson) -> cook-write (LE and BE)
local repWire = {};
local valWire = R.ReadStrict(T, "Root", inputs.valid_wire, repWire);
local repBad = {};
local valBad = R.ReadStrict(T, "Root", inputs.oor_wire, repBad);
local repJson = {};
local valJson = J.FromJson(T, "Root", inputs.valid_json, repJson);

if (valWire == null || valJson == null || valBad != null || !("reason" in repBad) || repBad.reason != "out_of_range") {
  print("cook-write|bad-strict-read\n");
} else {
  local sz = T.CookMeasure("Root", valWire);
  local szJ = T.CookMeasure("Root", valJson);
  local leBuf = blob(sz);
  local beBuf = blob(sz);
  local shortBuf = blob(sz - 1);
  for (local i = 0; i < shortBuf.len(); i++) shortBuf[i] = 0xa5;
  local okLe = T.Cook("Root", valWire, leBuf, 1);
  local okBe = T.Cook("Root", valJson, beBuf, 2);
  local okShort = T.Cook("Root", valWire, shortBuf, 1);
  local untouched = true;
  for (local i = 0; i < shortBuf.len(); i++) if ((shortBuf[i] & 0xff) != 0xa5) untouched = false;
  if (!okLe || !okBe || okShort || !untouched || sz != szJ) {
    print("cook-write|bad-write\n");
  } else {
    print("cook-write|" + hexOf(leBuf) + "|" + hexOf(beBuf) + "\n");
  }

  // 4. cook-open: valid LE opens; BE refuses foreign_order; bad build version refuses wrong_build_version; truncated refuses truncated
  local rOpen = { refused = false };
  local opened = T.cookOpen("Root", leBuf, rOpen);
  local rForeign = { refused = false };
  local openedForeign = T.cookOpen("Root", beBuf, rForeign);
  local badVer = blob(leBuf.len());
  for (local i = 0; i < leBuf.len(); i++) badVer[i] = leBuf[i];
  badVer[8] = (badVer[8] ^ 1) & 0xff;
  local rVer = { refused = false };
  local openedVer = T.cookOpen("Root", badVer, rVer);
  local trunc = blob(leBuf.len() - 1);
  for (local i = 0; i < trunc.len(); i++) trunc[i] = leBuf[i];
  local rTrunc = { refused = false };
  local openedTrunc = T.cookOpen("Root", trunc, rTrunc);
  local openOk = (opened != null) && (opened.Region == 64) && (opened.Length > 0) && (opened.Length < leBuf.len() - 64) &&
    (openedForeign == null) && ("reason" in rForeign) && (rForeign.reason == "foreign_order") &&
    (openedVer == null) && ("reason" in rVer) && (rVer.reason == "wrong_build_version") &&
    (openedTrunc == null) && ("reason" in rTrunc) && (rTrunc.reason == "truncated");
  print("cook-open|" + (openOk ? "ok" : "bad") + "\n");
}

// 5. block-build: create, begin, set, bytes, blockOpen, and bounds refusal
local storage = T.blockCreate("Frame");
local refusal = { Array = null, Count = 0, Maximum = 0 };
local blk = T.blockBegin("Frame", storage, { rows = 2 }, refusal);
local blkOk = false;
if (blk != null) {
  local ok1 = T.blockSet(blk, "Frame", 0, "version", 7);
  local rows = T.blockRows(blk, "rows");
  local ok2 = T.blockSet(blk, "Row", rows.At + 0 * rows.Stride, "x", 11);
  local ok3 = T.blockSet(blk, "Row", rows.At + 1 * rows.Stride, "y", 22);
  local used = T.blockBytes(blk);
  local bSlice = blob(used);
  for (local i = 0; i < used; i++) bSlice[i] = blk.Blob[blk.Base + i];
  local bRep = { refused = false };
  local bOpen = T.blockOpen("Frame", bSlice, bRep);
  local overRef = { Array = null, Count = 0, Maximum = 0 };
  local overBlk = T.blockBegin("Frame", storage, { rows = 9 }, overRef);
  local negRef = { Array = null, Count = 0, Maximum = 0 };
  local negBlk = T.blockBegin("Frame", storage, { rows = -1 }, negRef);
  blkOk = ok1 && ok2 && ok3 && (bOpen != null) && (bOpen.Used == used) &&
    (overBlk == null) && (overRef.Array == "rows") && (overRef.Count == 9) && (overRef.Maximum == 8) &&
    (negBlk == null) && (negRef.Array == "rows") && (negRef.Count == -1);
}
T.blockDestroy(storage);
print("block-build|" + (blkOk ? "ok" : "bad") + "\n");
`

type vtCookOracle struct {
	validWire []byte
	oorWire   []byte
	validJSON []byte
	littleHex string
	bigHex    string
}

func buildVtCookOracle(t *testing.T, u *ir.Unit) vtCookOracle {
	t.Helper()
	m := tabletext.NewModel(u)
	root := m.Lookup("Root")
	if root == nil {
		t.Fatal("Root not found")
	}
	inst := m.New(root)
	for i := range inst.Fields {
		switch inst.Fields[i].Def.Name {
		case "id":
			inst.Fields[i].Cell.I = 42
			inst.Fields[i].Cell.U = 42
		case "tag":
			inst.Fields[i].Cell.Str = []byte("hello")
			inst.Fields[i].Count = 5
		case "ok":
			inst.Fields[i].Cell.B = true
		case "rows":
			rowDef := m.Lookup("Row")
			r0 := m.New(rowDef)
			r0.Fields[0].Cell.I = 3
			r0.Fields[0].Cell.U = 3
			r0.Fields[1].Cell.I = 4
			r0.Fields[1].Cell.U = 4
			inst.Fields[i].Elems = []tabletext.Cell{{Tab: r0}}
			inst.Fields[i].Count = 1
		}
	}
	wire, err := tablewire.Encode(m, inst)
	if err != nil {
		t.Fatalf("encode valid wire: %v", err)
	}
	little, err := tablecook.Cook(m, inst, tablecook.Options{Big: false})
	if err != nil {
		t.Fatalf("cook little: %v", err)
	}
	big, err := tablecook.Cook(m, inst, tablecook.Options{Big: true})
	if err != nil {
		t.Fatalf("cook big: %v", err)
	}

	// Plant an out-of-range value in a copy of validWire: Root.id = 42 (0x2a) -> 101 (0x65, > max 100).
	oorWire := append([]byte(nil), wire...)
	at := bytes.IndexByte(oorWire, 42)
	if at < 0 {
		t.Fatalf("42 not found in validWire %x", wire)
	}
	oorWire[at] = 101

	return vtCookOracle{
		validWire: wire,
		oorWire:   oorWire,
		validJSON: []byte(`{"id":42,"tag":"hello","ok":true,"rows":[{"x":3,"y":4}]}`),
		littleHex: hex.EncodeToString(little),
		bigHex:    hex.EncodeToString(big),
	}
}

func runVtCookProbe(t *testing.T, patch func(dir string)) (map[string]string, vtCookOracle) {
	t.Helper()
	u := loadSource(t, vtCookCellsSchema)
	oracle := buildVtCookOracle(t, u)
	dir := t.TempDir()
	_ = emitUnit(t, dir, "cookcells", u)
	if patch != nil {
		patch(dir)
	}
	writeFile(t, filepath.Join(dir, "main.nut"), []byte(vtCookCellsProbe))
	inputs := map[string][]byte{
		"valid_wire": oracle.validWire,
		"oor_wire":   oracle.oorWire,
		"valid_json": oracle.validJSON,
	}
	out := sqrun.Run(t, dir, "main.nut", inputs)
	got := map[string]string{}
	for line := range strings.SplitSeq(strings.TrimRight(string(out), "\n"), "\n") {
		k, v, ok := strings.Cut(line, "|")
		if !ok {
			t.Fatalf("bad probe line %q", line)
		}
		got[k] = v
	}
	return got, oracle
}

func checkVtCookProbe(got map[string]string, oracle vtCookOracle) []string {
	var fails []string
	for _, cell := range []string{"unit-registry", "reflection", "cook-open", "block-build"} {
		if got[cell] != "ok" {
			fails = append(fails, fmt.Sprintf("%s: got %q, want ok", cell, got[cell]))
		}
	}
	wantCook := oracle.littleHex + "|" + oracle.bigHex
	if got["cook-write"] != wantCook {
		fails = append(fails, fmt.Sprintf("cook-write: got %q, want %q", got["cook-write"], wantCook))
	}
	return fails
}

func TestSquirrelVtCookCells(t *testing.T) {
	t.Parallel()
	if f := vtCookCellsFindings(readVtCookArtifacts(t)); len(f) > 0 {
		t.Fatalf("source gate: %v", f)
	}
	slowtest.Gate(t, "the pinned Squirrel interpreter")
	got, oracle := runVtCookProbe(t, nil)
	if fails := checkVtCookProbe(got, oracle); len(fails) > 0 {
		for _, f := range fails {
			t.Error(f)
		}
	}
}

func TestSquirrelVtCookCellsNegativeControl(t *testing.T) {
	t.Parallel()
	base := readVtCookArtifacts(t)
	if f := vtCookCellsFindings(base); len(f) > 0 {
		t.Fatalf("unplanted source gate already red: %v", f)
	}

	// Source-level plants in copies of the real artifacts.
	sourcePlants := []struct {
		name   string
		mutate func(a vtCookArtifacts) vtCookArtifacts
	}{
		{
			name: "cook-open build version",
			mutate: func(a vtCookArtifacts) vtCookArtifacts {
				a.cookNut = strings.Replace(a.cookNut, `if (!eqB(bytes, 8, T.BuildVersionBytes)) return refuse(report, "wrong_build_version");`, "// SABOTAGED", 1)
				return a
			},
		},
		{
			name: "block-build bounds",
			mutate: func(a vtCookArtifacts) vtCookArtifacts {
				a.blockBuild = strings.Replace(a.blockBuild, `if (typeof c != "integer" || c < 0 || c > a.max) {`, `if (typeof c != "integer" || c > a.max) {`, 1)
				return a
			},
		},
		{
			name: "driver cook-write loadWalk",
			mutate: func(a vtCookArtifacts) vtCookArtifacts {
				a.driverNut = strings.Replace(a.driverNut, `local got = strictLoad(opened, root, readBytes(inst[4]));`, `local got = L.loadWalk(opened, root, readBytes(inst[4]));`, 1)
				return a
			},
		},
	}
	for _, sp := range sourcePlants {
		if f := vtCookCellsFindings(sp.mutate(base)); len(f) == 0 {
			t.Fatalf("NEGATIVE CONTROL FAILED: source plant %s left vtCookCellsFindings green", sp.name)
		}
	}

	slowtest.Gate(t, "the pinned Squirrel interpreter")

	// Interpreter-level plants in copies of the real emitted files for each cell.
	runtimePlants := []struct {
		cell, file, anchor, replacement string
	}{
		{
			cell:        "unit-registry",
			file:        "CookcellsTables.nut",
			anchor:      `_M.Roots <- ["Row", "Root", "Frame"];`,
			replacement: `_M.Roots <- ["Row", "Root"];`,
		},
		{
			cell:        "reflection",
			file:        "CookcellsTables.nut",
			anchor:      `rangeIntMax = 100,`,
			replacement: `rangeIntMax = 99,`,
		},
		{
			cell:        "cook-write",
			file:        "schema_table_cookwrite.nut",
			anchor:      `put(buf, 16, order, 8, order);`,
			replacement: `put(buf, 16, 0, 8, order);`,
		},
		{
			cell:        "cook-open",
			file:        "schema_table_cook.nut",
			anchor:      `if (!eqB(bytes, 8, T.BuildVersionBytes)) return refuse(report, "wrong_build_version");`,
			replacement: `// SABOTAGED`,
		},
		{
			cell:        "block-build",
			file:        "schema_table_blockbuild.nut",
			anchor:      `if (typeof c != "integer" || c < 0 || c > a.max) {`,
			replacement: `if (typeof c != "integer" || c < 0) {`,
		},
	}
	for _, rp := range runtimePlants {
		got, oracle := runVtCookProbe(t, func(dir string) {
			p := filepath.Join(dir, "cookcells", rp.file)
			b, err := os.ReadFile(p)
			if err != nil {
				t.Fatal(err)
			}
			src := string(b)
			if !strings.Contains(src, rp.anchor) {
				t.Fatalf("%s: anchor %q not found in %s", rp.cell, rp.anchor, rp.file)
			}
			writeFile(t, p, []byte(strings.Replace(src, rp.anchor, rp.replacement, 1)))
		})
		fails := checkVtCookProbe(got, oracle)
		found := false
		for _, f := range fails {
			if strings.HasPrefix(f, rp.cell+":") {
				found = true
			}
		}
		if !found {
			t.Fatalf("NEGATIVE CONTROL FAILED: planting fault in %s (%s) left %s green: fails=%v got=%v", rp.file, rp.anchor, rp.cell, fails, got)
		}
	}
}
