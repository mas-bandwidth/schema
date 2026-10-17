package jstable

// TestFixedRoadmapJsT15GuardChain is the js leg's row of the card
// the-guard-chain-answers-the-outer-tag: docs/roadmap.sexp js/W16 ("nested
// union answers outer tag") and js/R28 ("the guard chain: the plan entry
// carries offset+count into a pool of (guard, arg, argw) links, outermost
// first, the only bound the layout's 64; an arm inside an arm answers to the
// OUTER tag"). Law: docs/FIXED-FORM-ALGORITHM.md §4.1.
//
// THE FIXTURE IS THE FIVE LEGS' ONE (t15GuardChainSchema, the same text in
// gotable, cstable, javatable and darttable): a union whose arm is a union
// whose arm is a union whose arm is a table, three tags deep on one path. It
// needs internal/codegen/js to emit the class of a union whose arm is a union
// (a table union, outside the decls), or the module cannot construct the value.
// The generated modules run under the leg's node: the deepest leaf must land as
// written and must NOT land while any one of the three tags names another arm,
// on the IDENTITY plan and on the COMPILED plan a lineage entry selects — the
// same layout under another hash, so nothing but the plan moves.

import (
	"encoding/binary"
	"fmt"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/js"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
)

// t15GuardChainSchema is THE fixture of the five legs' T15GuardChain tests.
// The record body is [0] Outer's tag, [1] Middle's, [2] Inner's, [3..7) x.
const t15GuardChainSchema = `package probe

type LeafA { x int32 = 0 }
type LeafB { y int32 = 0 }
union Inner
{
    a LeafA
    b LeafB
}
union Middle
{
    i Inner
    n LeafB
}
union Outer
{
    m Middle
    p LeafB
}
fixed table Root
{
    o Outer
    mid Middle
    inn Inner
}
`

// t15OtherHash is the mask the probe's lineage entry is stamped with: this
// build's own layout under a hash that is not this build's, so a file carrying
// it reads through the plan COMPILED from the lock's bytes (§4.2).
const t15OtherHash = 0x00ff00ff00ff00ff

func TestFixedRoadmapJsT15GuardChain(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "node (it runs the generated modules)")
	rows := []struct {
		id  string
		run func(t *testing.T)
	}{
		{"js/W16", func(t *testing.T) { t15RunJs(t, false) }},
		{"js/R28", func(t *testing.T) { t15RunJs(t, true) }},
	}
	for _, row := range rows {
		t.Run(row.id, func(t *testing.T) {
			t.Parallel()
			row.run(t)
		})
	}
}

// t15RunJs generates the packet module (the classes) and the table module with
// a lineage of the same layout under another hash, then runs the probe: both
// plans, clean and with each tag poisoned. chain also asserts, on a plan the
// probe compiles itself, the deepest entry's offset+count into the pool and its
// links OUTERMOST FIRST.
func t15RunJs(t *testing.T, chain bool) {
	t.Helper()
	node := jsNode(t)
	u := unitFrom(t, t15GuardChainSchema)
	self, ok := FixedLineageOf(u, "Root")
	if !ok {
		t.Fatal("no lineage entry for Root")
	}
	other := self.Wire ^ t15OtherHash
	files, err := js.Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	tables, err := GenerateLineage(u, map[string][]FixedLineageEntry{"Root": {{Wire: other, Layout: self.Layout, Record: self.Record}}})
	if err != nil {
		t.Fatal(err)
	}
	maps.Copy(files, tables)
	// The destination rows are module-private; the probe compiles a plan of its
	// own to read the chain off it, so it alone re-exports them.
	tableFile, ok := files["ProbeTable.js"]
	if !ok || !strings.Contains(string(tableFile), "const RootFixedDst = ") {
		t.Fatal("ProbeTable.js carries no RootFixedDst rows")
	}
	files["ProbeTable.js"] = append(tableFile, []byte("\nexport { RootFixedDst };\n")...)
	dir := t.TempDir()
	for name, data := range files {
		if strings.Contains(name, "/") {
			continue
		}
		if err := os.WriteFile(filepath.Join(dir, name), data, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	home := runtimeHome(u)
	var hash [8]byte
	binary.LittleEndian.PutUint64(hash[:], other)
	check := ""
	if chain {
		check = `
{
  const theirs = new TableFixedLayoutView();
  if (!TableFixedParseLayout(RootFixedLayout, 0, RootFixedLayout.length, theirs)) { fail("the layout does not parse"); }
  const plan = RootFixedNewPlan(4096, 4096);
  const made = TableFixedCompile(theirs, RootFixedLayout, RootFixedDst, plan, new TableFixedReport());
  if (made <= 0) { fail("the compiled plan refused a three-link chain: " + made); }
  let found = false;
  for (let i = 0; i < plan.count && !found; i++) {
    const b = i * TableFixedLanes;
    if (plan.entries[b + 10] !== 3) { continue; }
    found = true;
    const at = plan.entries[b + 9];
    for (let j = 0; j < 3; j++) {
      const g = plan.remap[at + 3 * j], a = plan.remap[at + 3 * j + 1], w = plan.remap[at + 3 * j + 2];
      if (g !== j || a !== 1 || w !== 1) { fail("link " + j + " is (" + g + ", " + a + ", " + w + "), so the chain is not OUTERMOST FIRST"); }
    }
    if (plan.entries[b + 5] !== 2) { fail("the guard lane is not the chain's innermost link"); }
  }
  if (!found) { fail("no compiled entry carries a three-link chain (offset+count into the pool)"); }
}
`
	}
	src := fmt.Sprintf(`import { Root, InitRoot, TableFixedReport, TableFixedLayoutView, TableFixedParseLayout, TableFixedCompile, TableFixedLanes } from "./%[1]sTable.js";
import { RootFixedLoad, RootFixedNewPlan, RootFixedSave, RootFixedMeasure, RootFixedLayout, RootFixedDst, RootFixedRecordBytes } from "./ProbeTable.js";
import { OuterType, MiddleType, InnerType } from "./Probe.js";

let failures = 0;
function fail(why) { console.error("FAIL: " + why); failures++; }

const v = new Root(); InitRoot(v);
v.O.Type = OuterType.M;
v.O.M.Type = MiddleType.I;
v.O.M.I.Type = InnerType.A;
v.O.M.I.A.X = 42;
const buf = new Uint8Array(RootFixedMeasure(1));
if (RootFixedSave([v], 1, buf) !== buf.length) { fail("the save"); }
const rec = buf.length - RootFixedRecordBytes + 8;
if (buf[rec] !== 1 || buf[rec + 1] !== 1 || buf[rec + 2] !== 1) { fail("the writer did not put the three tags where the chain says"); }
const other = [%[2]s];
for (let path = 0; path < 2; path++) {
  for (let poison = 0; poison <= 3; poison++) {
    const file = buf.slice();
    if (path === 1) { for (let k = 0; k < 8; k++) { file[8 + k] = other[k]; file[rec - 8 + k] = other[k]; } }
    if (poison > 0) { file[rec + poison - 1] = 2; }
    const back = [new Root()]; InitRoot(back[0]);
    const r = new TableFixedReport();
    const n = RootFixedLoad(back, 1, file, file.length, RootFixedNewPlan(4096, 4096), r);
    const which = path === 0 ? "identity" : "compiled";
    if (n !== 1 || r.refused !== 0 || r.malformed) { fail(which + " poison " + poison + ": n=" + n + " refused=" + r.refused); continue; }
    const x = back[0].O.M.I.A.X;
    if (poison === 0 && x !== 42) { fail(which + ": the record as written did not land its deepest leaf"); }
    if (poison > 0 && x === 42) { fail(which + ": the deepest leaf landed while tag " + (poison - 1) + " names another arm"); }
  }
}
%[3]s
if (failures === 0) { console.log("PROBE OK"); } else { process.exit(1); }
`, home, strings.Trim(strings.Join(strings.Fields(fmt.Sprintf("%d", hash[:])), ", "), "[]"), check)
	if err := os.WriteFile(filepath.Join(dir, "probe.mjs"), []byte(src), 0o600); err != nil {
		t.Fatal(err)
	}
	out, err := exec.Command(node, filepath.Join(dir, "probe.mjs")).CombinedOutput()
	if err != nil || !strings.Contains(string(out), "PROBE OK") {
		t.Fatalf("the driven reader failed (%v):\n%s", err, out)
	}
}
