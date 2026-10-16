package jstable

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/js"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// TestUnionClassesImportTheTablesTheyBuild holds the JavaScript union emitter
// (internal/codegen/js emitTagUnion) to importing every table arm class its
// storage constructor instantiates, from the unit home that defines that class
// (<Home>Table.js when the union is in the unit's fixed-form closure, and
// <Home>Variable.js otherwise), and round-trips every union across the table
// corpus through its wire codec in node.
func TestUnionClassesImportTheTablesTheyBuild(t *testing.T) {
	t.Parallel()
	node := jsNode(t)

	corpusUnits := []struct {
		name  string
		paths []string
	}{
		{"backend", []string{repoPath("tables/backend")}},
		{"messages", []string{repoPath("tables/messages")}},
		{"m1", []string{repoPath("test/tables/M1.schema")}},
		{"m2", []string{repoPath("test/tables/M2.schema")}},
		{"stream", []string{repoPath("tables/stream")}},
		{"examples", []string{repoPath("tables/examples")}},
		{"v1", []string{repoPath("test/tables/V1.schema")}},
		{"v2", []string{repoPath("test/tables/V2.schema")}},
		{"r1", []string{repoPath("test/tables/R1.schema")}},
		{"r2", []string{repoPath("test/tables/R2.schema")}},
		{"a1", []string{repoPath("test/tables/A1.schema")}},
		{"a2", []string{repoPath("test/tables/A2.schema")}},
		{"rt1", []string{repoPath("test/tables/RT1.schema")}},
		{"rt2", []string{repoPath("test/tables/RT2.schema")}},
		{"lists", []string{repoPath("tables/lists")}},
		{"maps", []string{repoPath("tables/maps")}},
		{"arms", []string{repoPath("tables/arms")}},
	}

	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "package.json"), []byte("{\"type\":\"module\"}\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	var checks strings.Builder
	unionCount := 0

	for _, cu := range corpusUnits {
		u := loadUnitPaths(t, cu.paths)
		files, err := js.Generate(u)
		if err != nil {
			t.Fatalf("%s: js.Generate: %v", cu.name, err)
		}
		tables, err := Generate(u)
		if err != nil {
			t.Fatalf("%s: jstable.Generate: %v", cu.name, err)
		}
		for k, v := range tables {
			files[k] = v
		}
		sub := filepath.Join(dir, cu.name)
		if err := os.MkdirAll(sub, 0o700); err != nil {
			t.Fatal(err)
		}
		for name, data := range files {
			if strings.Contains(name, "/") {
				continue
			}
			if err := os.WriteFile(filepath.Join(sub, name), data, 0o600); err != nil {
				t.Fatal(err)
			}
		}
		for _, f := range u.Files {
			var allUnions []*ir.Union
			for _, d := range f.Decls {
				if un, ok := d.(*ir.Union); ok {
					allUnions = append(allUnions, un)
				}
			}
			allUnions = append(allUnions, f.TableUnions...)
			if len(allUnions) == 0 {
				continue
			}
			modVar := fmt.Sprintf("mod_%s_%s", cu.name, f.Base)
			checks.WriteString(fmt.Sprintf("const %s = await import(\"./%s/%s.js\");\n", modVar, cu.name, f.Base))
			for _, un := range allUnions {
				checks.WriteString(fmt.Sprintf("{\n  const u = new %s.%s();\n  assert.strictEqual(u.Type, %s.%sType.None);\n", modVar, un.Name, modVar, un.Name))
				for _, v := range un.Variants {
					if v.Void() {
						continue
					}
					checks.WriteString(fmt.Sprintf("  assert.notStrictEqual(u.%s, undefined);\n", ir.GoExportName(v.Name)))
				}
				checks.WriteString("}\n")
				unionCount++
			}
		}
	}
	if unionCount == 0 {
		t.Fatal("no unions discovered in corpus")
	}

	probe := fmt.Sprintf(`import assert from "node:assert/strict";

function roundTripFixed(make, mut, measure, save, load, newPlan, makeReport) {
  const src = make();
  mut(src);
  const buf = new Uint8Array(measure(1));
  const written = save([src], 1, buf);
  assert.strictEqual(written, buf.length);
  const dst = [make()];
  const plan = newPlan(64, 64);
  const rrep = makeReport();
  const count = load(dst, 1, buf, written, plan, rrep);
  assert.strictEqual(count, 1);
  assert.strictEqual(rrep.malformed, false);
  assert.strictEqual(rrep.refused, 0);
  assert.deepStrictEqual(dst[0], src);
}

function roundTripVariable(make, mut, save, load, makeReport) {
  const src = make();
  mut(src);
  const bytes = save(src);
  assert.ok(bytes instanceof Uint8Array && bytes.length > 0);
  const rrep = makeReport();
  const dst = load(bytes, rrep);
  assert.strictEqual(rrep.malformed, false);
  assert.strictEqual(rrep.refused, 0);
  assert.deepStrictEqual(dst, src);
}

%s

// 1. backend: Payload in Envelope
{
  const home = await import("./backend/BackenddemoTable.js");
  const root = await import("./backend/BackendTable.js");
  roundTripFixed(() => new home.Envelope(), (e) => {
    e.Payload.Type = mod_backend_Backend.PayloadType.Login;
    e.Payload.Login.PlayerId = 9001n;
    e.Payload.Login.ClientBuild = 42;
  }, root.EnvelopeFixedMeasure, root.EnvelopeFixedSave, root.EnvelopeFixedLoad, root.EnvelopeFixedNewPlan, () => new home.TableFixedReport());
}

// 2. messages: Origin, EditBody in Edit; ToolBody in ToolMessage
{
  const home = await import("./messages/MessagedemoTable.js");
  const root = await import("./messages/MessagesTable.js");
  roundTripFixed(() => new home.Edit(), (m) => {
    m.Revision = 3;
    m.Body.Type = mod_messages_Messages.EditBodyType.Insert;
    m.Body.Insert.At.Line = 12;
    m.Body.Insert.Origin.Type = mod_messages_Messages.OriginType.User;
    m.Body.Insert.Origin.User.Name[0] = 65;
    m.Body.Insert.Origin.User.NameLength = 1;
  }, root.EditFixedMeasure, root.EditFixedSave, root.EditFixedLoad, root.EditFixedNewPlan, () => new home.TableFixedReport());

  roundTripFixed(() => new home.ToolMessage(), (m) => {
    m.Sequence = 5;
    m.Body.Type = mod_messages_Messages.ToolBodyType.Open;
    m.Body.Open.Path[0] = 97;
    m.Body.Open.PathLength = 1;
  }, root.ToolMessageFixedMeasure, root.ToolMessageFixedSave, root.ToolMessageFixedLoad, root.ToolMessageFixedNewPlan, () => new home.TableFixedReport());
}

// 3. m1 & m2: Body in Msg
{
  const home1 = await import("./m1/Tblm1Table.js");
  const root1 = await import("./m1/M1Table.js");
  roundTripFixed(() => new home1.Msg(), (m) => {
    m.Seq = 11;
    m.Body.Type = mod_m1_M1.BodyType.Open;
    m.Body.Open.Path[0] = 120;
    m.Body.Open.PathLength = 1;
  }, root1.MsgFixedMeasure, root1.MsgFixedSave, root1.MsgFixedLoad, root1.MsgFixedNewPlan, () => new home1.TableFixedReport());

  const home2 = await import("./m2/Tblm2Table.js");
  const root2 = await import("./m2/M2Table.js");
  roundTripFixed(() => new home2.Msg(), (m) => {
    m.Seq = 22;
    m.Body.Type = mod_m2_M2.BodyType.Close;
    m.Body.Close.Path[0] = 121;
    m.Body.Close.PathLength = 1;
  }, root2.MsgFixedMeasure, root2.MsgFixedSave, root2.MsgFixedLoad, root2.MsgFixedNewPlan, () => new home2.TableFixedReport());
}

// 4. stream: Frame in Feed (variable form)
{
  const v = await import("./stream/StreamdemoVariable.js");
  roundTripVariable(() => new v.Feed(), (f) => {
    const h = new v.Header();
    h.name = "hello";
    f.frame = { arm: "header", value: h };
    const c = new v.Chunk();
    c.data = new Uint8Array([1, 2, 3]);
    f.parts = [c];
  }, v.FeedSave, v.FeedLoad, () => new v.TableVariableReport());
}

// 5. examples, v1, v2, r1, r2: Effect
{
  const homeEx = await import("./examples/TabledemoTable.js");
  const rootEx = await import("./examples/TablesTable.js");
  roundTripFixed(() => new homeEx.WeaponConfig(), (w) => {
    w.Effect.Type = mod_examples_Tables.EffectType.Debuff;
    w.Effect.Debuff.Amount = 15;
  }, rootEx.WeaponConfigFixedMeasure, rootEx.WeaponConfigFixedSave, rootEx.WeaponConfigFixedLoad, rootEx.WeaponConfigFixedNewPlan, () => new homeEx.TableFixedReport());

  const homeV1 = await import("./v1/Tblv1Table.js");
  const rootV1 = await import("./v1/V1Table.js");
  roundTripFixed(() => new homeV1.Cfg(), (c) => {
    c.Effect.Type = mod_v1_V1.EffectType.Boost;
    c.Effect.Boost.Power = 19;
  }, rootV1.CfgFixedMeasure, rootV1.CfgFixedSave, rootV1.CfgFixedLoad, rootV1.CfgFixedNewPlan, () => new homeV1.TableFixedReport());

  const homeV2 = await import("./v2/Tblv2Table.js");
  const rootV2 = await import("./v2/V2Table.js");
  roundTripFixed(() => new homeV2.Cfg(), (c) => {
    c.Effect.Type = mod_v2_V2.EffectType.Boost;
    c.Effect.Boost.Power = 23;
  }, rootV2.CfgFixedMeasure, rootV2.CfgFixedSave, rootV2.CfgFixedLoad, rootV2.CfgFixedNewPlan, () => new homeV2.TableFixedReport());

  const homeR1 = await import("./r1/Tblr1Table.js");
  const rootR1 = await import("./r1/R1Table.js");
  roundTripFixed(() => new homeR1.Cfg(), (c) => {
    c.Effect.Type = mod_r1_R1.EffectType.Boost;
    c.Effect.Boost.Power = 31;
  }, rootR1.CfgFixedMeasure, rootR1.CfgFixedSave, rootR1.CfgFixedLoad, rootR1.CfgFixedNewPlan, () => new homeR1.TableFixedReport());

  const homeR2 = await import("./r2/Tblr2Table.js");
  const rootR2 = await import("./r2/R2Table.js");
  roundTripFixed(() => new homeR2.Cfg(), (c) => {
    c.Effect.Type = mod_r2_R2.EffectType.Boost;
    c.Effect.Boost.Power = 37;
  }, rootR2.CfgFixedMeasure, rootR2.CfgFixedSave, rootR2.CfgFixedLoad, rootR2.CfgFixedNewPlan, () => new homeR2.TableFixedReport());
}

// 6. a1, a2: Value in Root
{
  const homeA1 = await import("./a1/Tbla1Table.js");
  const rootA1 = await import("./a1/A1Table.js");
  roundTripFixed(() => new homeA1.Root(), (r) => {
    r.Value.Type = mod_a1_A1.ValueType.E;
    r.Value.E.N = 7;
  }, rootA1.RootFixedMeasure, rootA1.RootFixedSave, rootA1.RootFixedLoad, rootA1.RootFixedNewPlan, () => new homeA1.TableFixedReport());

  const homeA2 = await import("./a2/Tbla2Table.js");
  const rootA2 = await import("./a2/A2Table.js");
  roundTripFixed(() => new homeA2.Root(), (r) => {
    r.Value.Type = mod_a2_A2.ValueType.E;
    r.Value.E.N = 9;
  }, rootA2.RootFixedMeasure, rootA2.RootFixedSave, rootA2.RootFixedLoad, rootA2.RootFixedNewPlan, () => new homeA2.TableFixedReport());
}

// 7. rt1, rt2: Pick in Node (variable) and Fitting in Parcel (fixed)
{
  const vRT1 = await import("./rt1/Tblrt1Variable.js");
  roundTripVariable(() => new vRT1.Node(), (n) => {
    const inner = new vRT1.Inner();
    inner.hits = 4;
    n.pick = { arm: "alpha", value: inner };
  }, vRT1.NodeSave, vRT1.NodeLoad, () => new vRT1.TableVariableReport());

  const vRT2 = await import("./rt2/Tblrt2Variable.js");
  roundTripVariable(() => new vRT2.Node(), (n) => {
    const inner = new vRT2.Inner();
    inner.hits = 6;
    n.pick = { arm: "alpha", value: inner };
  }, vRT2.NodeSave, vRT2.NodeLoad, () => new vRT2.TableVariableReport());

  const homeRT2 = await import("./rt2/Tblrt2Table.js");
  const rootRT2 = await import("./rt2/RT2Table.js");
  roundTripFixed(() => new homeRT2.Parcel(), (p) => {
    p.Fit.Type = mod_rt2_RT2.FittingType.Bolt;
    p.Fit.Bolt.Weight = 8;
  }, rootRT2.ParcelFixedMeasure, rootRT2.ParcelFixedSave, rootRT2.ParcelFixedLoad, rootRT2.ParcelFixedNewPlan, () => new homeRT2.TableFixedReport());
}

// 8. lists: Hit in Mixed
{
  const v = await import("./lists/ListdemoVariable.js");
  roundTripVariable(() => new v.Mixed(), (m) => {
    const p = new v.Point();
    p.x = 3;
    p.y = 4;
    m.hits = [{ arm: "point", value: p }];
  }, v.MixedSave, v.MixedLoad, () => new v.TableVariableReport());
}

// 9. maps: Force in Depth
{
  const v = await import("./maps/MapdemoVariable.js");
  roundTripVariable(() => new v.Depth(), (d) => {
    const s = new v.Squad();
    const it = new v.Item();
    it.count = 5;
    s.roster.set(1, it);
    d.arm = { arm: "squad", value: s };
  }, v.DepthSave, v.DepthLoad, () => new v.TableVariableReport());
}

// 10. arms: Carry, Reach, Inner, Outer, Slot, Slots
{
  const v = await import("./arms/ArmdemoVariable.js");
  roundTripVariable(() => new v.Holder(), (h) => {
    const lf = new v.Leaf();
    lf.items = [10, 20];
    h.carry = { arm: "leaf", value: lf };
  }, v.HolderSave, v.HolderLoad, () => new v.TableVariableReport());

  roundTripVariable(() => new v.Gate(), (g) => {
    const o = new v.Only();
    o.w = 77;
    g.reach = { arm: "only", value: o };
  }, v.GateSave, v.GateLoad, () => new v.TableVariableReport());

  roundTripVariable(() => new v.Twig(), (tw) => {
    const lf = new v.Leaf();
    lf.items = [30];
    tw.inner = { arm: "leaf", value: lf };
  }, v.TwigSave, v.TwigLoad, () => new v.TableVariableReport());

  roundTripVariable(() => new v.Nest(), (n) => {
    const lf = new v.Leaf();
    lf.items = [40];
    n.outer = { arm: "inner", value: { arm: "leaf", value: lf } };
  }, v.NestSave, v.NestLoad, () => new v.TableVariableReport());

  roundTripVariable(() => new v.Ring(), (r) => {
    const nd = new v.Node();
    nd.v = 55;
    r.items = [{ arm: "node", value: nd }];
  }, v.RingSave, v.RingLoad, () => new v.TableVariableReport());

  roundTripVariable(() => new v.Rack(), (r) => {
    const nd = new v.Node();
    nd.v = 66;
    r.items = [{ arm: "many", value: [nd] }];
  }, v.RackSave, v.RackLoad, () => new v.TableVariableReport());
}

console.log("unions verified: %d");
`, checks.String(), unionCount)

	probePath := filepath.Join(dir, "probe.mjs")
	if err := os.WriteFile(probePath, []byte(probe), 0o600); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(node, probePath)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("union probe failed: %v\n%s", err, out)
	}
}
