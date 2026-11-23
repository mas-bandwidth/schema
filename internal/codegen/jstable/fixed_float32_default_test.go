package jstable

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/js"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/internal/testproc"
)

// TestJsFixedFloat32DefaultIsTheStoredFloat32 holds a fixed table's float32
// default to the value a float32 holds: the record stores four bytes, so a
// default float32 cannot spell exactly (0.7, under a compressed range or
// plain) is constructed, and restored by Init, at its float32 value — the value
// a fixed load lands — and a fresh record saves and loads back equal to itself.
// A float64 default keeps its double.
func TestJsFixedFloat32DefaultIsTheStoredFloat32(t *testing.T) {
	t.Parallel()
	const schema = `package probe

fixed table Msg
{
    seventh float32 = 0.7 | min = 0.7, max = 1, resolution = 0.01
    plain   float32 = 0.1
    wide    float64 = 0.1
}
`
	u := unitFrom(t, schema)
	files, err := Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	base, err := js.Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	slowtest.Gate(t, "node (it runs the generated fixed form)")
	node := jsNode(t)
	dir := t.TempDir()
	for _, set := range []map[string][]byte{base, files} {
		for name, data := range set {
			if err := os.WriteFile(filepath.Join(dir, name), data, 0o600); err != nil {
				t.Fatal(err)
			}
		}
	}
	const probe = `import { isDeepStrictEqual } from "node:util";
import * as m from "./ProbeTable.js";
let bad = [];
const v = new m.Msg();
if (v.seventh !== Math.fround(0.7)) { bad.push("constructed seventh " + v.seventh); }
if (v.plain !== Math.fround(0.1)) { bad.push("constructed plain " + v.plain); }
if (v.wide !== 0.1) { bad.push("constructed wide " + v.wide); }
v.seventh = 1; v.plain = 2; v.wide = 3;
m.InitMsg(v);
if (v.seventh !== Math.fround(0.7) || v.plain !== Math.fround(0.1) || v.wide !== 0.1) { bad.push("Init restored " + JSON.stringify(v)); }
const src = new m.Msg();
const buf = new Uint8Array(m.MsgFixedMeasure(1));
const w = m.MsgFixedSave([src], 1, buf);
const dst = [new m.Msg()];
const rep = new m.TableFixedReport();
const n = m.MsgFixedLoad(dst, 1, buf, w, m.MsgFixedNewPlan(64, 64), rep);
if (n !== 1 || rep.damaged || rep.refused !== 0) { bad.push("round trip refused"); }
else if (!isDeepStrictEqual(dst[0], src)) { bad.push("loaded " + JSON.stringify(dst[0]) + ", saved " + JSON.stringify(src)); }
console.log(bad.length === 0 ? "OK" : bad.join("\n"));
`
	if err := os.WriteFile(filepath.Join(dir, "probe.mjs"), []byte(probe), 0o600); err != nil {
		t.Fatal(err)
	}
	cmd := testproc.Command(t, node, "probe.mjs")
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil || strings.TrimSpace(string(out)) != "OK" {
		t.Errorf("fixed float32 default (%v):\n%s", err, out)
	}
}
