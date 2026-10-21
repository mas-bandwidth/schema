// A DEPRECATED ENUM VARIANT READS AS THE FIELD DEFAULT AND IS REFUSED ON WRITE
// (docs/SPEC-TABLES.md §2.15), on the id-table wire, form 3, the message form
// and JSON/text. The wire is written by the SAME schema with the variant LIVE,
// so the bytes are those of an older writer; the same bytes read with the
// variant live yield it (the reversed witness), proving the test sees the rule.
package squirreltable_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/compiler"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/squirrel/sqrun"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/squirreltable"
	"github.com/mas-bandwidth/schema/v2/ir"
)

const variantDeprecatedLiveSchema = `package probe
enum Team { Red, Blue }
enum Ship {
    Fighter
    Bomber
    Scout
}
union Pick {
    ship Ship
    n   int32
}
fixed table Root {
    ship  Ship = Scout
    fleet [..2]Ship
    byRole [Team]Ship
    pick  Pick
}
`

const variantDeprecatedSchema = `package probe
enum Team { Red, Blue }
enum Ship {
    Fighter
    Bomber | deprecated
    Scout
}
union Pick {
    ship Ship
    n   int32
}
fixed table Root {
    ship  Ship = Scout
    fleet [..2]Ship
    byRole [Team]Ship
    pick  Pick
}
`

// variantDeprecatedLiveScript writes one value holding Bomber on every form
// this port carries. It prints five hex lines: wire, fixed, announcement,
// message, text.
const variantDeprecatedLiveScript = `local inputs = dofile("inputs.nut", true);
local S = dofile("schema_module.nut", true);
local T = S.require("__MODULE__");
local W = S.require("schema_table_save");
local J = S.require("schema_table_json");
local function textblob(s) { local out = blob(s.len()); for (local i = 0; i < s.len(); i++) out[i] = s[i] & 0xff; return out; }
local v = T.Root();
v.Ship = "Bomber";
v.Fleet = ["Bomber"];
v.ByRole = ["Bomber", null];
local tag = 0;
foreach (a in T.PickDescriptor.arms) if (a.member == "Ship") tag = a.tag;
v.Pick.Type = tag;
v.Pick.Ship = "Bomber";
local wire = W.save(T, "Root", v);
::hexline(wire);
local fixed = blob(4096);
local fn = T.RootFixedSave(fixed, [v]);
local fb = blob(fn);
for (local i = 0; i < fn; i++) fb[i] = fixed[i];
::hexline(fb);
::hexline(T.TableMessageAnnouncement);
local mb = blob(4096);
local mr = {};
local mn = T.RootSaveMessages([v], mb, mr);
local cut = blob(mn);
for (local i = 0; i < mn; i++) cut[i] = mb[i];
::hexline(cut);
::hexline(textblob(J.ToJson(T, "Root", v)));
`

const variantDeprecatedReadScript = `local inputs = dofile("inputs.nut", true);
local S = dofile("schema_module.nut", true);
local T = S.require("__MODULE__");
local L = S.require("schema_table_load");
local W = S.require("schema_table_save");
local J = S.require("schema_table_json");
local function show(x) { return x == null ? "<null>" : ("" + x); }
local function counters(r) {
  return r.unknown + "," + r.kindMismatch + "," + r.widened + "," + r.clamped + "," +
    (("duplicate" in r) ? r.duplicate : 0) + "," + (r.malformed ? "true" : "false");
}
local r = { unknown = 0, kindMismatch = 0, widened = 0, clamped = 0, duplicate = 0, malformed = false, refused = false };
local v = L.loadTolerant(T, "Root", inputs.wire, r);
print("wire=" + (v == null ? "null" : show(v.Ship) + "|" + show(v.Fleet[0]) + "|" + show(v.ByRole[0]) + "|" + show(v.Pick.Ship) + "|" + counters(r)) + "\n");

local recs = [T.Root()];
local n = T.RootFixedLoadTolerant(inputs.fixed, recs, null);
print("fixed=" + n + "|" + show(recs[0].Ship) + "|" + show(recs[0].Fleet[0]) + "|" + show(recs[0].ByRole[0]) + "|" + show(recs[0].Pick.Ship) + "|" +
  (T.FixedReason == null ? "-" : T.FixedReason) + "\n");

local vocab = {};
local ar = {};
local ok = T.AnnounceRead(vocab, inputs.announce, ar);
local mr = {};
local decoded = [T.Root()];
local mn = T.RootLoadMessages(decoded, vocab, inputs.message, mr);
print("message=" + ok + "|" + mn + "|" + show(decoded[0].Ship) + "|" + show(decoded[0].Fleet[0]) + "|" + show(decoded[0].ByRole[0]) + "|" + show(decoded[0].Pick.Ship) + "|" + counters(mr) + "\n");

local jr = {};
local jv = J.FromJsonTolerant(T, "Root", inputs.json, jr);
print("json=" + (jv == null ? "null" : show(jv.Ship) + "|" + show(jv.Fleet[0]) + "|" + show(jv.ByRole[0]) + "|" + show(jv.Pick.Ship) + "|" + counters(jr)) + "\n");

local bad = T.Root();
bad.Ship = "Bomber";
bad.Fleet = ["Bomber"];
local s = W.save(T, "Root", bad);
print("save=" + (s == null ? "refused" : "" + s) + "|" + (W.SaveReason == null ? "-" : W.SaveReason) + "\n");
local fx = blob(4096);
local fs = T.RootFixedSave(fx, [bad]);
print("fixedsave=" + fs + "|" + (T.FixedReason == null ? "-" : T.FixedReason) + "\n");
local msr = {};
local msz = T.RootMeasureMessages([bad], msr);
print("messagesave=" + msz + "|" + (msr.refused ? "refused" : "accepted") + "|" + (msr.reason == null ? "-" : msr.reason) + "\n");
local jm = J.ToJsonMeasure(T, "Root", bad);
local jt = J.ToJson(T, "Root", bad);
print("jsonsave=" + jm + "|" + (jt == null ? "refused" : "accepted") + "\n");

local arr = T.Root();
arr.Fleet = ["Bomber"];
print("arrsave=" + (W.save(T, "Root", arr) == null ? "refused" : "accepted") + "\n");
local afx = blob(4096);
print("arrfixedsave=" + T.RootFixedSave(afx, [arr]) + "|" + (T.FixedReason == null ? "-" : T.FixedReason) + "\n");
local amr = {};
print("arrmessagesave=" + T.RootMeasureMessages([arr], amr) + "|" + (amr.refused ? "refused" : "accepted") + "|" + (amr.reason == null ? "-" : amr.reason) + "\n");
print("arrjsonsave=" + J.ToJsonMeasure(T, "Root", arr) + "|" + (J.ToJson(T, "Root", arr) == null ? "refused" : "accepted") + "\n");
`

const variantDeprecatedWitnessScript = `local inputs = dofile("inputs.nut", true);
local S = dofile("schema_module.nut", true);
local T = S.require("__MODULE__");
local L = S.require("schema_table_load");
local J = S.require("schema_table_json");
local function show(x) { return x == null ? "<null>" : ("" + x); }
local r = { unknown = 0, kindMismatch = 0, widened = 0, clamped = 0, duplicate = 0, malformed = false, refused = false };
local v = L.loadTolerant(T, "Root", inputs.wire, r);
print("wire=" + show(v.Ship) + "|" + show(v.Fleet[0]) + "|" + show(v.ByRole[0]) + "|" + show(v.Pick.Ship) + "|" + r.unknown + "\n");
local recs = [T.Root()];
local n = T.RootFixedLoadTolerant(inputs.fixed, recs, null);
print("fixed=" + n + "|" + show(recs[0].Ship) + "|" + show(recs[0].Fleet[0]) + "|" + show(recs[0].ByRole[0]) + "|" + show(recs[0].Pick.Ship) + "\n");
local vocab = {};
local ar = {};
local ok = T.AnnounceRead(vocab, inputs.announce, ar);
local decoded = [T.Root()];
local rr = {};
local mn = T.RootLoadMessages(decoded, vocab, inputs.message, rr);
print("message=" + ok + "|" + mn + "|" + show(decoded[0].Ship) + "|" + show(decoded[0].Fleet[0]) + "|" + show(decoded[0].ByRole[0]) + "|" + show(decoded[0].Pick.Ship) + "|" + rr.unknown + "\n");
local jr = {};
local jv = J.FromJsonTolerant(T, "Root", inputs.json, jr);
print("json=" + show(jv.Ship) + "|" + show(jv.Fleet[0]) + "|" + show(jv.ByRole[0]) + "|" + show(jv.Pick.Ship) + "|" + jr.unknown + "\n");
`

func loadInlineUnit(t *testing.T, path, src string) *ir.Unit {
	t.Helper()
	if err := os.WriteFile(path, []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	paths, err := compiler.GatherPaths([]string{path})
	if err != nil {
		t.Fatal(err)
	}
	u, err := compiler.New().Load(paths)
	if err != nil {
		t.Fatalf("load %s: %v", path, err)
	}
	return u
}

func variantScript(t *testing.T, script string, u *ir.Unit) string {
	t.Helper()
	return strings.Replace(script, "__MODULE__", squirreltable.ModuleName(u), 1)
}

func TestDeprecatedVariantReadsDefaultWriteRefused(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	liveU := loadInlineUnit(t, filepath.Join(dir, "live.schema"), variantDeprecatedLiveSchema)
	depU := loadInlineUnit(t, filepath.Join(dir, "dep.schema"), variantDeprecatedSchema)

	liveDir := filepath.Join(dir, "live")
	depDir := filepath.Join(dir, "dep")
	for _, d := range []string{liveDir, depDir} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	writeGenerated(t, liveDir, liveU)
	writeGenerated(t, depDir, depU)

	writeFile(t, filepath.Join(liveDir, "main.nut"), []byte(variantScript(t, variantDeprecatedLiveScript, liveU)))
	writeFile(t, filepath.Join(depDir, "main.nut"), []byte(variantScript(t, variantDeprecatedReadScript, depU)))

	lines, err := sqrun.HexLines(sqrun.Run(t, liveDir, "main.nut", nil))
	if err != nil {
		t.Fatal(err)
	}
	if len(lines) != 5 {
		t.Fatalf("the live writer printed %d artifacts, want 5", len(lines))
	}
	inputs := map[string][]byte{
		"wire": lines[0], "fixed": lines[1], "announce": lines[2],
		"message": lines[3], "json": lines[4],
	}
	out := string(sqrun.Run(t, depDir, "main.nut", inputs))

	got := map[string]string{}
	for line := range strings.SplitSeq(strings.TrimRight(out, "\n"), "\n") {
		k, v, ok := strings.Cut(line, "=")
		if !ok {
			t.Fatalf("probe line %q is not key=value", line)
		}
		got[k] = v
	}

	for _, want := range []struct{ key, value string }{
		// the id-table wire: the field reads its declared default (Scout), the
		// array element the ELEMENT default (None), the union arm's enum payload
		// its arm field's zero default (None), nothing unknown
		{"wire", "Scout|<null>|<null>|<null>|0,0,0,0,0,false"},
		// form 3: the field's default ordinal (Scout = 3), the element None = 0
		{"fixed", "1|Scout|<null>|<null>|<null>|-"},
		// the message form
		{"message", "true|1|Scout|<null>|<null>|<null>|0,0,0,0,0,false"},
		// JSON/text
		{"json", "Scout|<null>|<null>|<null>|0,0,0,0,0,false"},
		// every writer refuses, invalid_value naming <Table>.<field>
		{"save", "refused|invalid_value: Root.ship names the deprecated variant Bomber"},
		{"fixedsave", "-1|invalid_value: Root.ship names the deprecated variant Bomber"},
		{"messagesave", "-1|refused|invalid_value: Root.ship names the deprecated variant Bomber"},
		{"jsonsave", "-1|refused"},
		{"arrsave", "refused"},
		{"arrfixedsave", "-1|invalid_value: Root.fleet names the deprecated variant Bomber"},
		{"arrmessagesave", "-1|refused|invalid_value: Root.fleet names the deprecated variant Bomber"},
		{"arrjsonsave", "-1|refused"},
	} {
		if got[want.key] != want.value {
			t.Errorf("%s reads %q, want %q", want.key, got[want.key], want.value)
		}
	}

	// THE REVERSED WITNESS: the same bytes read with Bomber LIVE yield it.
	writeFile(t, filepath.Join(liveDir, "witness.nut"), []byte(variantScript(t, variantDeprecatedWitnessScript, liveU)))
	wout := string(sqrun.Run(t, liveDir, "witness.nut", inputs))
	for _, want := range []string{
		"wire=Bomber|Bomber|Bomber|Bomber|0",
		"fixed=1|Bomber|Bomber|Bomber|Bomber",
		"message=true|1|Bomber|Bomber|Bomber|Bomber|0",
		"json=Bomber|Bomber|Bomber|Bomber|0",
	} {
		if !strings.Contains(wout, want+"\n") {
			t.Errorf("the reversed witness does not read %q:\n%s", want, wout)
		}
	}
}
