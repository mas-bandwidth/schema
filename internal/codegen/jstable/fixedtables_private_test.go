package jstable

// TestJsFixedTablesPrivate holds the fixed form's tables to their module: a
// caller that imports the generated module can neither reach a mutable table
// nor move the decode bytes by writing one. The layout (and every other typed
// array a reader might need) is a module-private const and is handed out only
// as a copy; the object tables are frozen. The probe is a run of the generated
// module under the leg's own node (jsNode, $NODE), never a reading of the
// source alone, and the committed paired module is held to the same rule so a
// stale check-in cannot ship the vulnerability a fresh generation fixed.

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

func TestJsFixedTablesPrivate(t *testing.T) {
	t.Parallel()
	u := unitFrom(t, `package probe

fixed table T
{
    x int32
    y int32
}
`)
	files, err := Generate(u)
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
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
	probe := fmt.Sprintf(`// one generated probe, run under the leg's own node.
const home = "./%sTable.js";
const mod = await import(home);
const source = (await import("node:fs")).readFileSync(new URL(home, import.meta.url), "utf8");
let failures = 0;
function fail(why) { console.error("FAIL: " + why); failures++; }

function roundTrip() {
  const v = new mod.T(); mod.InitT(v); v.X = 7; v.Y = 9;
  const bytes = new Uint8Array(mod.TFixedMeasure(1));
  if (mod.TFixedSave([v], 1, bytes) !== bytes.length) { return { ok: false, bytes: null }; }
  const back = [new mod.T()]; mod.InitT(back[0]);
  const report = new mod.TableFixedReport();
  const n = mod.TFixedLoad(back, 1, bytes, bytes.length, mod.TFixedNewPlan(4096, 4096), report);
  const ok = n === 1 && back[0].X === 7 && back[0].Y === 9 && !report.malformed && report.refused === 0;
  return { ok: ok, bytes: bytes };
}

function same(a, b) {
  if (a === null || b === null || a.length !== b.length) { return false; }
  for (let i = 0; i < a.length; i++) { if (a[i] !== b[i]) { return false; } }
  return true;
}

// A BARE TYPED ARRAY IS THE MODULE'S TABLE, and a write to it moves the bytes
// the module writes for everybody. It must not be exported at all.
const bare = [...source.matchAll(/export\s+const\s+(\w+Fixed(?:Layout|Dst|Prefill|Identity|Known|Cover|Guard|LineagePlans))\s*=\s*new\s+(?:Uint|Int|Float)(?:8|16|32|64)?Array\s*\(/g)].map((m) => m[1]);
if (bare.length !== 0) {
  fail("a bare mutable typed array is exported: " + bare.join(", "));
  const base = roundTrip();
  const live = mod[bare[0]];
  if (live === undefined || typeof live.length !== "number" || live.length === 0) {
    fail("the exported table " + bare[0] + " is not a reachable mutable array");
  } else {
    const old = live[0];
    live[0] = (old + 1) & 0xff;
    const after = roundTrip();
    if (base.ok && after.ok && !same(base.bytes, after.bytes)) {
      fail("writing an element of the exported " + bare[0] + " changed the bytes the module writes");
    }
    live[0] = old;
  }
}

// THE ACCESSOR HANDS OUT A COPY: writing it cannot move the module's table.
if (typeof mod.TFixedLayoutCopy !== "function") {
  fail("TFixedLayoutCopy is not an exported accessor");
} else {
  const first = mod.TFixedLayoutCopy();
  const second = mod.TFixedLayoutCopy();
  if (!(first instanceof Uint8Array)) { fail("TFixedLayoutCopy did not return a Uint8Array"); }
  if (first === second) { fail("TFixedLayoutCopy handed out the module's own table, not a copy"); }
  const base = roundTrip();
  if (!base.ok) { fail("the generated module does not round trip before the copy is written"); }
  first[0] = (first[0] + 1) & 0xff;
  const after = roundTrip();
  if (!after.ok) { fail("decoding failed after writing the accessor's copy"); }
  if (!same(base.bytes, after.bytes)) { fail("writing the accessor's copy changed the bytes the module writes"); }
  if (!same(second, mod.TFixedLayoutCopy())) { fail("the module's own layout changed when its copy was written"); }
}

if (failures !== 0) { process.exit(1); }
console.log("ok");
`, home)
	if err := os.WriteFile(filepath.Join(dir, "probe.mjs"), []byte(probe), 0o600); err != nil {
		t.Fatal(err)
	}
	// THE COMMITTED TREE SHIPS, so the same rule holds there and not only in
	// the probe this test just generated. A fresh generation can be private
	// while the checked-in module still exports its table.
	committed := filepath.Join("..", "..", "..", "generated", "bench", "paired", "js", "FixedTableTable.js")
	shipped, err := os.ReadFile(committed)
	if err != nil {
		t.Fatalf("read the committed generated table: %v", err)
	}
	bareExport := regexp.MustCompile(`export\s+const\s+\w+Fixed(Layout|Dst|Prefill|Identity|Known|Cover|Guard|LineagePlans)\s*=\s*new\s+(Uint|Int|Float)(8|16|32|64)?Array\s*\(`)
	if m := bareExport.FindString(string(shipped)); m != "" {
		t.Errorf("the committed generated table still exports a bare mutable typed array: %s", m)
	}
	if !strings.Contains(string(shipped), "export function FixedTableFixedLayoutCopy()") {
		t.Error("the committed generated table has no FixedTableFixedLayoutCopy accessor")
	}

	node := jsNode(t)
	cmd := exec.Command(node, filepath.Join(dir, "probe.mjs"))
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("the fixed tables are reachable or mutable from outside their module: %v\n%s", err, out)
	}
}
