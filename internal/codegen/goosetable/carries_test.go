package goosetable_test

import (
	"maps"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/compiler"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/goose"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/goose/gooserun"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/goosetable"
	"github.com/mas-bandwidth/schema/v2/ir"
)

func unitFromSchema(t *testing.T, name, src string) *ir.Unit {
	t.Helper()
	p := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(p, []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	u, err := compiler.New().Load([]string{p})
	if err != nil {
		t.Fatal(err)
	}
	return u
}

func TestGooseCarriesDeclaredForms(t *testing.T) {
	t.Parallel()
	const src = `package formsdemo
fixed table OnlyWire | forms = "wire,json" {
    x int32
}
fixed table OnlyBlock | forms = "wire,block" {
    x int32
}
`
	u := unitFromSchema(t, "Forms.schema", src)
	files, err := goosetable.Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	tab := string(files["FormsTable.goose"])
	for _, want := range []string{"fn OnlyWireFromJson(", "fn OnlyWireToJson(", "fn OnlyBlockFixedSave(", "fn OnlyBlockFixedLoad("} {
		if !strings.Contains(tab, want) {
			t.Errorf("FormsTable.goose missing declared form symbol %q", want)
		}
	}
	for _, forbid := range []string{"fn OnlyBlockFromJson(", "fn OnlyBlockToJson("} {
		if strings.Contains(tab, forbid) {
			t.Errorf("FormsTable.goose emitted undeclared form symbol %q", forbid)
		}
	}
	block := string(files["FormsBlock.goose"])
	if !strings.Contains(block, "fn OnlyBlockBlockOpen(") {
		t.Errorf("FormsBlock.goose missing OnlyBlockBlockOpen")
	}
	if strings.Contains(block, "OnlyWireBlock") {
		t.Errorf("FormsBlock.goose emitted block surface for OnlyWire")
	}
	if _, ok := files["FormsCook.goose"]; ok {
		t.Errorf("FormsCook.goose emitted when no table carries cook")
	}
}

func TestGooseCarriesArrayRootJSON(t *testing.T) {
	t.Parallel()
	const src = `package arraydemo
table Goal {
    x int32
}
table GoalList | json = "array" {
    goals [..2]Goal
}
table Holder {
    list GoalList
}
`
	u := unitFromSchema(t, "Array.schema", src)
	files, err := goose.Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	tfiles, err := goosetable.Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	maps.Copy(files, tfiles)
	files["main.goose"] = []byte(`import .ArrayTable;
import .schema_json;
import .schema_tables;

fn main() {
    var list = arraydemo::NewGoalList();
    var rep = schema_rt::JsonReport { .. };
    let input = "[\n  {\n    \"x\": 7\n  },\n  {\n    \"x\": 9\n  }\n]\n";
    assert(arraydemo::GoalListFromJson(list, input, rep));
    assert(list.GoalsCount == 2 && list.Goals[0].X == 7 && list.Goals[1].X == 9);
    var out: u8[>..] = [];
    let n = arraydemo::GoalListToJson(list, out);
    assert(n == input.len);
    assert(out == input);

    // An object at the root of an array-root table is malformed.
    var bad = arraydemo::NewGoalList();
    var brep = schema_rt::JsonReport { .. };
    assert(!arraydemo::GoalListFromJson(bad, "{}", brep));
    assert(brep.damaged);

    // Nested inside Holder, GoalList also rides as a JSON array.
    var h = arraydemo::NewHolder();
    var hrep = schema_rt::JsonReport { .. };
    assert(arraydemo::HolderFromJson(h, "{\"list\":[{\"x\":11},{\"x\":13}]}", hrep));
    assert(h.List.GoalsCount == 2 && h.List.Goals[0].X == 11 && h.List.Goals[1].X == 13);

    // An object where GoalList's array belongs is the wrong shape: the
    // strict read refuses the whole text (docs/plans/strict-only.md).
    var hbad = arraydemo::NewHolder();
    var hbrep = schema_rt::JsonReport { .. };
    assert(!arraydemo::HolderFromJson(hbad, "{\"list\":{\"goals\":[]}}", hbrep));
}
`)
	gooserun.Run(t, t.TempDir(), "main.goose", files)
}

func TestGooseCarriesIntegrityCRC32C(t *testing.T) {
	t.Parallel()
	const src = `package integritydemo
fixed table Sealed | integrity = "crc32c" {
    x int32
    y int32
}
`
	u := unitFromSchema(t, "Integrity.schema", src)
	files, err := goose.Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	tfiles, err := goosetable.Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	maps.Copy(files, tfiles)
	files["main.goose"] = []byte(`import .IntegrityTable;
import .integritydemo_unit;
import .fixed;
import .fixedevolve;
import .message;
import .wire_load;
import .wire_save;
import .schema_tables;

fn main() {
    // Form 3: fixed save + tolerant load + evolve load + integrity_mismatch on flip.
    var rec = integritydemo::NewSealed();
    rec.X = 42;
    rec.Y = -7;
    var batch: integritydemo::Sealed[1] = [rec];
    let fneed = integritydemo::SealedFixedMeasure(1);
    assert(fneed > 4);
    var fbuf: u8[>..] = [];
    let fn_len = integritydemo::SealedFixedSave(batch, fbuf);
    assert(fn_len == fneed);
    var floaded: integritydemo::Sealed[1] = [integritydemo::NewSealed()];
    var frep = schema_rt::FixedReport { .. };
    assert(integritydemo::SealedFixedLoad(floaded, fbuf, frep) == 1);
    assert(!frep.damaged && floaded[0].X == 42 && floaded[0].Y == -7);

    var fevolved: integritydemo::Sealed[1] = [integritydemo::NewSealed()];
    var ferep = schema_rt::FixedEvolveReport { .. };
    assert(integritydemo::SealedFixedEvolveLoad(fevolved, fbuf, ferep) == 1);
    assert(!ferep.damaged && fevolved[0].X == 42 && fevolved[0].Y == -7);

    fbuf[fn_len - 1] = fbuf[fn_len - 1] ^ 0x01;
    var fbad: integritydemo::Sealed[1] = [integritydemo::NewSealed()];
    var fbrep = schema_rt::FixedReport { .. };
    assert(integritydemo::SealedFixedLoad(fbad, fbuf, fbrep) == -1);
    assert(fbrep.damaged && fbrep.reason == schema_rt::FixedRefuseIntegrityMismatch);

    var febad: integritydemo::Sealed[1] = [integritydemo::NewSealed()];
    var febrep = schema_rt::FixedEvolveReport { .. };
    assert(integritydemo::SealedFixedEvolveLoad(febad, fbuf, febrep) == -1);
    assert(febrep.damaged && febrep.reason == schema_rt::FixedRefuseIntegrityMismatch);

    // Form 2: message batch save + load + integrity_mismatch on flip.
    var mrep = schema_rt::MsgReport { .. };
    let mneed = integritydemo::SealedMeasureMessages(batch, mrep);
    assert(mneed > 4);
    var mbuf: u8[>..] = [];
    let mlen = integritydemo::SealedSaveMessages(batch, mbuf, mrep);
    assert(mlen == mneed);
    var voc = schema_rt::MsgVocabulary { .. };
    assert(schema_rt::announce_read(voc, integritydemo::SealedAnnounce, mrep));
    var mout: integritydemo::Sealed[1] = [integritydemo::NewSealed()];
    assert(integritydemo::SealedLoadMessages(mout, voc, mbuf, mrep) == 1);
    assert(!mrep.damaged && mout[0].X == 42 && mout[0].Y == -7);

    mbuf[mlen - 1] = mbuf[mlen - 1] ^ 0x80;
    var mbad: integritydemo::Sealed[1] = [integritydemo::NewSealed()];
    var mbrep = schema_rt::MsgReport { .. };
    assert(integritydemo::SealedLoadMessages(mbad, voc, mbuf, mbrep) == -1);
    assert(mbrep.damaged && mbrep.reason == schema_rt::MsgRefuseIntegrityMismatch);

    // Form 1: table wire save + load + integrity_mismatch on flip.
    var empty: u8[0] = [];
    var vals: schema_rt::LoadVal[>..] = [
        schema_rt::LoadVal { state: schema_rt::ValPresent, made: true, tab: 0, first: 1, count: 2, .. },
        schema_rt::LoadVal { state: schema_rt::ValPresent, shape: schema_rt::ShapeInt, bits: schema_rt::Int128 { lo: 42, hi: 0 }, .. },
        schema_rt::LoadVal { state: schema_rt::ValPresent, shape: schema_rt::ShapeInt, bits: schema_rt::Int128 { lo: (-7) as! u64, hi: 0xFFFFFFFFFFFFFFFF }, .. },
    ];
    var nodes: schema_rt::LoadNode[>..] = [];
    var wout: u8[>..<] = [];
    let refusal = schema_rt::save_table(integritydemo::TableUnit, "Sealed", empty, vals, nodes, 0, wout);
    assert(refusal == 0 && wout.len >= 5);
    var wbytes: u8[>..] = [];
    for b in wout { wbytes.push(b); }

    var lvals: schema_rt::LoadVal[>..] = [];
    var lnodes: schema_rt::LoadNode[>..] = [];
    var lrep = schema_rt::LoadReport { .. };
    let lroot = schema_rt::load_table(integritydemo::TableUnit, "Sealed", wbytes, lvals, lnodes, lrep);
    assert(lroot >= 0 && !lrep.body_damaged);

    var wcorrupt: u8[>..] = [];
    for b in wout { wcorrupt.push(b); }
    let last = wcorrupt.len - 1;
    wcorrupt[last] = wcorrupt[last] ^ 0x01;
    var bvals: schema_rt::LoadVal[>..] = [];
    var bnodes: schema_rt::LoadNode[>..] = [];
    var brep = schema_rt::LoadReport { .. };
    let broot = schema_rt::load_table(integritydemo::TableUnit, "Sealed", wcorrupt, bvals, bnodes, brep);
    assert(broot == -1 && brep.body_damaged && brep.reason == schema_rt::LoadRefuseIntegrityMismatch);
}
`)
	gooserun.Run(t, t.TempDir(), "main.goose", files)
}
