package jstable

import (
	"encoding/hex"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/js"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
)

// The reader declares [2..4]; the writer under another build declares [..4],
// so it can lawfully send the count 1 the reader's Min excludes.
const (
	jsCountMinVariableReader = "package probe\n\ntable Probe\n{\n    items [2..4]int32\n}\n"
	jsCountMinVariableWriter = "package probe\n\ntable Probe\n{\n    items [..4]int32\n}\n"
	jsCountMinFixedReader    = "package probe\n\nfixed table Msg\n{\n    items [2..4]int32\n}\n"
	jsCountMinFixedWriter    = "package probe\n\nfixed table Msg\n{\n    items [..4]int32\n}\n"
)

// TestJsCountBelowMinRefused pins SPEC §5.1 row 3 ("a counted array's count
// past its Max or below its Min") on every JavaScript read: the variable
// read, the JSON read, the fixed read (a forged count word in the reader's
// own layout; a writer outside the reader's lineage is refused layout_newer
// first, and a lineage read decodes its image through the same count) and the message
// read each refuse a count below the reader's Min, as the Go engine refuses
// count_over_bound (tablewire/decode.go, messagedecode.go, fixed.go,
// tabletext/read.go). The fixed writer refuses one too, as the variable and
// message writers already did. An absent field is no short count: it lands
// at the birth count. The lawful twin of every case reads.
func TestJsCountBelowMinRefused(t *testing.T) {
	t.Parallel()
	vr := unitFrom(t, jsCountMinVariableReader)
	vfiles, err := generateVariableFiles(vr, nil)
	if err != nil {
		t.Fatal(err)
	}
	vhome := runtimeHome(vr) + "Variable.js"
	rt := string(vfiles[vhome])
	if !strings.Contains(rt, "} else if (count < (fd.min || 0)) {") {
		t.Errorf("the variable read must refuse a count below the declared Min")
	}
	if !strings.Contains(runtimeFunction(t, rt, "TableVariableTextArray"), "if (placed < (fd.min || 0)) { report.oor++; }") {
		t.Errorf("the JSON read must refuse an array shorter than the declared Min")
	}
	fr := unitFrom(t, jsCountMinFixedReader)
	ffiles, err := Generate(fr)
	if err != nil {
		t.Fatal(err)
	}
	fsrc := string(ffiles["ProbeTable.js"])
	if !strings.Contains(fsrc, "if (n < 2 || n > 4) { report.refused = TableFixedRefusal.CountOverBound; return; }") {
		t.Errorf("the fixed read must refuse a count below the declared Min")
	}
	if !strings.Contains(fsrc, "value.itemsCount < 2 || value.itemsCount > 4) { return false; }") {
		t.Errorf("the fixed write must refuse a count below the declared Min")
	}
	if !strings.Contains(jsFn(fsrc, "MsgLoadMessageBody"), `if (keep > 4 || keep < 2) { TableMessageRefuse(r.Report, "count_over_bound"); return false; }`) {
		t.Errorf("the message read must refuse a count below the declared Min")
	}

	t.Run("node", func(t *testing.T) {
		slowtest.Gate(t, "node (it runs the generated reads)")
		node := jsNode(t)
		dir := t.TempDir()
		put := func(sub string, set map[string][]byte) {
			if err := os.MkdirAll(filepath.Join(dir, sub), 0o700); err != nil {
				t.Fatal(err)
			}
			for name, data := range set {
				if err := os.WriteFile(filepath.Join(dir, sub, name), data, 0o600); err != nil {
					t.Fatal(err)
				}
			}
		}
		vw := unitFrom(t, jsCountMinVariableWriter)
		vwfiles, err := generateVariableFiles(vw, nil)
		if err != nil {
			t.Fatal(err)
		}
		put("vr", vfiles)
		put("vw", vwfiles)
		frbase, err := js.Generate(fr)
		if err != nil {
			t.Fatal(err)
		}
		put("fr", frbase)
		put("fr", ffiles)
		fw := unitFrom(t, jsCountMinFixedWriter)
		fwfiles, err := Generate(fw)
		if err != nil {
			t.Fatal(err)
		}
		fwbase, err := js.Generate(fw)
		if err != nil {
			t.Fatal(err)
		}
		put("fw", fwbase)
		put("fw", fwfiles)
		ann := hex.EncodeToString(tablewire.Announce(fw))
		if err := os.WriteFile(filepath.Join(dir, "announce.txt"), []byte(ann), 0o600); err != nil {
			t.Fatal(err)
		}
		probe := strings.ReplaceAll(jsCountMinProbe, "VHOME", vhome)
		if err := os.WriteFile(filepath.Join(dir, "probe.mjs"), []byte(probe), 0o600); err != nil {
			t.Fatal(err)
		}
		cmd := exec.Command(node, "probe.mjs")
		cmd.Dir = dir
		out, err := cmd.CombinedOutput()
		if err != nil || strings.TrimSpace(string(out)) != "OK" {
			t.Errorf("count below Min (%v):\n%s", err, out)
		}
	})
}

const jsCountMinProbe = `import { readFileSync } from "node:fs";
const vr = await import("./vr/VHOME");
const vw = await import("./vw/VHOME");
const fr = await import("./fr/ProbeTable.js");
const fw = await import("./fw/ProbeTable.js");
let failed = false;
function fail(why) { failed = true; console.log("FAILED: " + why); }

// THE VARIABLE READ
function vsave(items) { const p = new vw.Probe(); p.items = items; return vw.ProbeSave(p); }
{
  const r = new vr.TableVariableReport();
  const x = vr.ProbeLoad(vsave([77]), r);
  if (x !== null || r.oor !== 1 || r.damaged || r.mismatch !== 0) { fail("variable read of count 1 under [2..4]: " + (x === null ? "refused" : JSON.stringify(x.items)) + " " + JSON.stringify(r)); }
  const r2 = new vr.TableVariableReport();
  const y = vr.ProbeLoad(vsave([77, 78]), r2);
  if (y === null || y.items.length !== 2) { fail("variable read of count 2 under [2..4] was refused"); }
  const z = vr.ProbeLoad(vsave([]), new vr.TableVariableReport());
  if (z === null || z.items.length !== 2) { fail("variable read of an absent array is not the birth count"); }
}
// THE JSON READ
{
  const r = new vr.TableVariableReport();
  const x = vr.ProbeFromJson('{"items":[77]}', r);
  if (x !== null) { fail("JSON read of count 1 under [2..4] read " + JSON.stringify(x.items) + " oor=" + r.oor); }
  const y = vr.ProbeFromJson('{"items":[77,78]}', new vr.TableVariableReport());
  if (y === null || y.items.length !== 2) { fail("JSON read of count 2 under [2..4] was refused"); }
  const z = vr.ProbeFromJson('{}', new vr.TableVariableReport());
  if (z === null || z.items.length !== 2) { fail("JSON read of an absent array is not the birth count"); }
}
// THE FIXED READ AND WRITE
function fresh(m, count) { const v = new m.Msg(); m.InitMsg(v); v.itemsCount = count; v.items[0] = 77; return v; }
function fsave(m, count) {
  const n = m.MsgFixedMeasure(1);
  const b = new Uint8Array(n);
  const got = m.MsgFixedSave([fresh(m, count)], 1, b);
  return got === n ? b : null;
}
function fload(b) {
  const out = [fresh(fr, 2)];
  const rep = new fr.TableFixedReport();
  const n = fr.MsgFixedLoad(out, 1, b, b.length, fr.MsgFixedNewPlan(4096, 4096), rep);
  return { n, rep, v: out[0] };
}
{
  const lawful = fsave(fr, 2);
  if (lawful === null || fload(lawful).n !== 1) { fail("fixed read of count 2 under [2..4] was refused"); }
  // the reader's own layout with its count word rewritten to 1
  const forged = Uint8Array.from(lawful);
  const dv = new DataView(forged.buffer);
  let at = -1;
  for (let i = 0; i + 8 <= forged.length; i++) {
    if (dv.getInt32(i, true) === 2 && dv.getInt32(i + 4, true) === 77) { at = i; }
  }
  if (at < 0) { fail("no count word found in the fixed save"); }
  else {
    dv.setInt32(at, 1, true);
    const got = fload(forged);
    if (got.n !== -1 || got.rep.refused !== fr.TableFixedRefusal.CountOverBound) { fail("fixed read of a forged count 1 under [2..4]: n=" + got.n + " refused=" + got.rep.refused); }
  }
  if (fsave(fr, 1) !== null) { fail("fixed write of count 1 under [2..4] was written"); }
}
// THE MESSAGE READ
{
  const voc = new fr.TableVocabulary();
  voc.Init(new Array(16));
  const arep = {};
  if (!fr.AnnounceRead(voc, Uint8Array.from(Buffer.from(readFileSync("announce.txt", "utf8").trim(), "hex")), arep)) {
    fail("the announcement was refused " + JSON.stringify(arep));
  } else {
    for (const [count, want] of [[1, "refused"], [2, "read"]]) {
      const v = fresh(fw, count);
      const size = fw.MsgMeasureMessages([v], {});
      const b = new Uint8Array(size);
      if (size < 0 || fw.MsgSaveMessages([v], b, {}) !== size) { fail("the [..4] writer refused the message of count " + count); continue; }
      const out = [fresh(fr, 2)];
      const rep = {};
      const n = fr.MsgLoadMessages(out, voc, b, rep);
      const refused = n !== 1 || rep.Damaged || (rep.Oor | 0) > 0 || (rep.Mismatch | 0) > 0 || Boolean(rep.Reason);
      if ((refused ? "refused" : "read") !== want) { fail("message read of count " + count + " under [2..4]: n=" + n + " " + JSON.stringify(rep)); }
    }
  }
}
if (!failed) { console.log("OK"); }
`
