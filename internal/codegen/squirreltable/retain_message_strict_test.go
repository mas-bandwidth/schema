package squirreltable_test

// RETAIN-UNKNOWN AND MESSAGE FORM UNDER THE ONE STRICT READ
// (docs/SPEC-TABLES.md §3.3, §6.5, §6.6; docs/roadmap-evidence/squirrel.sexp
// cells port/squirrel/vt/message-form and port/squirrel/vt/retain-unknown).
//
// Functional tests, not unit tests: both run the pinned Squirrel interpreter
// (sqrun). The negative control plants its faults in the real generated
// runtime artifacts (schema_table_load.nut, schema_table_retain.nut, and
// schema_table_message.nut), not in a stand-in. They take no sockets and no
// network.

import (
	"encoding/hex"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/squirrel"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/squirrel/sqrun"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/squirreltable"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
)

type sqRetainMessageCase struct {
	name, sqSetup, jsonText string
}

const sqRetainMessageNewerSchema = `package probe
fixed table Child { n int32
  k int32 }
fixed table Rec { a int32 }
union Pick { alpha int32
 ping }
fixed table Item { n int32
  y int32 }
table Msg { c Child
  x int32
  extra string(8)
  neg int16
  nums [..4]int32
  rec Rec
  pick Pick
  items [..2]Item }
table SealedMsg | integrity = "crc32c" { x int32
  extra string(8) }
fixed table WireMsg { c Child
  x int32
  extra string(8)
  neg int16 }
`

const sqRetainMessageOlderSchema = `package probe
fixed table Child { n int32 }
fixed table Item { n int32 }
table Msg { c Child
  x int32 | min = 0, max = 100
  items [..2]Item }
table SealedMsg | integrity = "crc32c" { x int32 }
fixed table WireMsg { c Child
  x int32 | min = 0, max = 100 }
`

func sqRetainMessageCases() []sqRetainMessageCase {
	return []sqRetainMessageCase{
		{"extra", `v.Extra = "hi";`, `{"c":{"n":7},"x":42,"extra":"hi"}`},
		{"neg", `v.Neg = -5;`, `{"c":{"n":7},"x":42,"neg":-5}`},
		{"nums", `v.Nums = [1, -2, 3, 4];`, `{"c":{"n":7},"x":42,"nums":[1,-2,3,4]}`},
		{"rec", `v.Rec.A = 9;`, `{"c":{"n":7},"x":42,"rec":{"a":9}}`},
		{"pick", `v.Pick.Type = 1; v.Pick.Alpha = -7;`, `{"c":{"n":7},"x":42,"pick":{"alpha":-7}}`},
		{"nested", `v.C.K = 11;`, `{"c":{"n":7,"k":11},"x":42}`},
		{"element", `local it = Tn.Item(); it.N = 1; it.Y = 5; v.Items = [it];`, `{"c":{"n":7},"x":42,"items":[{"n":1,"y":5}]}`},
	}
}

func sqRetainMessageOracle(t *testing.T) (map[string][]byte, []byte, []byte) {
	t.Helper()
	newerModel := tabletext.NewModel(retainUnitFrom(t, sqRetainMessageNewerSchema))
	olderModel := tabletext.NewModel(retainUnitFrom(t, sqRetainMessageOlderSchema))

	saves := map[string][]byte{}
	for _, c := range sqRetainMessageCases() {
		v := newerModel.New(newerModel.Lookup("Msg"))
		var rep tabletext.Report
		if !newerModel.Read(v, []byte(c.jsonText), &rep) {
			t.Fatalf("oracle read %s: %+v", c.name, rep)
		}
		wire, err := tablewire.Encode(newerModel, v)
		if err != nil {
			t.Fatalf("oracle encode %s: %v", c.name, err)
		}
		ov := olderModel.New(olderModel.Lookup("Msg"))
		store := &tablewire.Retain{Capacity: 1 << 20, IdCapacity: 1024}
		var orep tabletext.Report
		ok, err := tablewire.DecodeRetain(olderModel, ov, wire, store, &orep)
		if !ok || err != nil {
			t.Fatalf("oracle decode retain %s: ok=%v err=%v rep=%+v", c.name, ok, err, orep)
		}
		want, err := tablewire.EncodeRetain(olderModel, ov, store, &orep)
		if err != nil {
			t.Fatalf("oracle encode retain %s: %v", c.name, err)
		}
		saves[c.name] = want
	}

	// Wire with unknown extra="hi" followed by a mismatched kind on x (string instead of int32)
	// and a wire with unknown extra="hi" followed by a repeated field id for extra (body damage).
	extraWire := saves["extra"]
	count := int(extraWire[len(extraWire)-8])
	trailerStart := len(extraWire) - 8 - count*8
	trailer := extraWire[trailerStart:]
	mismatchBody := []byte{
		1,                    // form 1
		3, 12, 2, 0x68, 0x69, // extra = "hi" (captured into retain store)
		2, 12, 0, // x with kind 12 (string) -> kind_mismatch on int32 field x
		0, // root terminator
	}
	mismatchWire := append(mismatchBody, trailer...)

	damagedRepeatBody := []byte{
		1,                    // form 1
		3, 12, 2, 0x68, 0x69, // extra = "hi" (captured into retain store)
		3, 12, 2, 0x79, 0x6f, // repeated unknown id 3 in the same body -> bodyDamaged
		0, // root terminator
	}
	damagedRepeatWire := append(damagedRepeatBody, trailer...)

	return saves, mismatchWire, damagedRepeatWire
}

func sqRetainMessageSetup(t *testing.T) (map[string][]byte, map[string][]byte, string, map[string][]byte) {
	t.Helper()
	newerUnit := retainUnitFrom(t, sqRetainMessageNewerSchema)
	olderUnit := retainUnitFrom(t, sqRetainMessageOlderSchema)

	newerTypes, err := squirrel.Generate(newerUnit)
	if err != nil {
		t.Fatalf("generate newer squirrel types: %v", err)
	}
	newerTables, err := squirreltable.Generate(newerUnit)
	if err != nil {
		t.Fatalf("generate newer squirrel tables: %v", err)
	}
	maps.Copy(newerTypes, newerTables)

	olderTypes, err := squirrel.Generate(olderUnit)
	if err != nil {
		t.Fatalf("generate older squirrel types: %v", err)
	}
	olderTables, err := squirreltable.Generate(olderUnit)
	if err != nil {
		t.Fatalf("generate older squirrel tables: %v", err)
	}
	maps.Copy(olderTypes, olderTables)

	saves, mismatchWire, damagedRepeatWire := sqRetainMessageOracle(t)
	inputs := map[string][]byte{
		"mismatch_wire":       mismatchWire,
		"damaged_repeat_wire": damagedRepeatWire,
	}
	for name, b := range saves {
		inputs["go_"+name] = b
	}

	var sqCases strings.Builder
	for _, c := range sqRetainMessageCases() {
		fmt.Fprintf(&sqCases, `  ["%s", function(Tn) {
    local v = Tn.Msg();
    v.C.N = 7;
    v.X = 42;
    %s
    return v;
  }],
`, c.name, c.sqSetup)
	}

	probe := `local inputs = dofile("inputs.nut", true);
local sNew = dofile("newer/schema_module.nut", true);
local sOld = dofile("older/schema_module.nut", true);
local Tn = sNew.require("ProbeTables");
local To = sOld.require("ProbeTables");

local sameBytes = function(a, b) {
  if (a == null || b == null || a.len() != b.len()) return false;
  for (local i = 0; i < a.len(); i++) {
    if ((a[i] & 0xff) != (b[i] & 0xff)) return false;
  }
  return true;
};

local saveVar = function(T, root, v) {
  local n = T[root + "Measure"](v);
  if (n < 0) return null;
  local b = T[root + "Save"](v);
  if (b == null || b.len() != n) return null;
  return b;
};

local cases = [
` + sqCases.String() + `];

foreach (c in cases) {
  local name = c[0];
  local v = c[1](Tn);
  local G = saveVar(Tn, "Msg", v);
  if (G == null) { print(name + ":newer_save_refused\n"); continue; }
  local store = { capacity = 1048576, idCapacity = 1024 };
  local rp = { unknown=0, mismatch=0, widened=0, oor=0, repeated=0, bodyDamaged=false, refused=false };
  local msg = To.MsgLoadRetain(G, store, rp);
  if (msg == null || rp.bodyDamaged || rp.refused) { print(name + ":load_refused\n"); continue; }
  local sz = To.MsgMeasureRetain(msg, store);
  if (sz < 0) { print(name + ":measure_refused\n"); continue; }
  local out = blob(sz);
  local srp = {};
  local n = To.MsgSaveRetain(msg, store, out, srp);
  if (n != sz || srp.retainLost > 0) { print(name + ":save_refused\n"); continue; }
  print(name + ":" + sameBytes(out, inputs["go_" + name]) + ":" + rp.retained + ":" + rp.retainLost + "\n");
}

// Integrity-sealed retaining round trip (SealedMsg | integrity = "crc32c")
{
  local v = Tn.SealedMsg();
  v.X = 42;
  v.Extra = "hi";
  local G = saveVar(Tn, "SealedMsg", v);
  local store = { capacity = 1048576, idCapacity = 1024 };
  local rp = { unknown=0, mismatch=0, widened=0, oor=0, repeated=0, bodyDamaged=false, refused=false };
  local msg = To.SealedMsgLoadRetain(G, store, rp);
  if (msg == null || rp.bodyDamaged || rp.refused) {
    print("sealed:load_refused\n");
  } else {
    local sz = To.SealedMsgMeasureRetain(msg, store);
    local out = blob(sz);
    local srp = {};
    local n = To.SealedMsgSaveRetain(msg, store, out, srp);
    local ok = n == sz && srp.retainLost == 0 && sameBytes(out, G);
    print("sealed:" + ok + ":" + rp.retained + ":" + rp.retainLost + "\n");
  }

  // Corrupted CRC32C trailer on SealedMsg is refused and leaves store empty
  local badG = blob(G.len());
  for (local i = 0; i < G.len(); i++) badG[i] = G[i];
  badG[badG.len() - 1] = (badG[badG.len() - 1] ^ 0xff) & 0xff;
  local badStore = { capacity = 1048576, idCapacity = 1024 };
  local badRp = { unknown=0, mismatch=0, widened=0, oor=0, repeated=0, bodyDamaged=false, refused=false };
  local badMsg = To.SealedMsgLoadRetain(badG, badStore, badRp);
  local cleanBad = badMsg == null && badRp.bodyDamaged && ("reason" in badRp) && badRp.reason == "integrity_mismatch" && badStore.recs.len() == 0 && badStore.used == 0 && badRp.retained == 0 && badRp.retainLost == 0;
  print("strict_integrity:" + cleanBad + ":" + badRp.retained + ":" + badStore.recs.len() + "\n");
}

// Strict refusal: out_of_range on a known field alongside unknown fields
{
  local v = Tn.Msg();
  v.C.N = 7;
  v.X = 500;
  v.Extra = "hi";
  local G = saveVar(Tn, "Msg", v);
  local store = { capacity = 1048576, idCapacity = 1024 };
  local rp = { unknown=0, mismatch=0, widened=0, oor=0, repeated=0, bodyDamaged=false, refused=false };
  local msg = To.MsgLoadRetain(G, store, rp);
  local clean = msg == null && rp.refused && ("reason" in rp) && rp.reason == "out_of_range" && store.recs.len() == 0 && store.used == 0 && rp.retained == 0 && rp.retainLost == 0;
  print("strict_oor:" + clean + ":" + rp.retained + ":" + store.recs.len() + "\n");
}

// Strict refusal: kind_mismatch on a known field after an unknown field was captured
{
  local store = { capacity = 1048576, idCapacity = 1024 };
  local rp = { unknown=0, mismatch=0, widened=0, oor=0, repeated=0, bodyDamaged=false, refused=false };
  local msg = To.MsgLoadRetain(inputs["mismatch_wire"], store, rp);
  local clean = msg == null && rp.refused && ("reason" in rp) && rp.reason == "kind_mismatch" && store.recs.len() == 0 && store.used == 0 && rp.retained == 0 && rp.retainLost == 0;
  print("strict_mismatch:" + clean + ":" + rp.retained + ":" + store.recs.len() + "\n");
}

// Strict refusal: repeated field id (bodyDamaged) after an unknown field was captured
{
  local store = { capacity = 1048576, idCapacity = 1024 };
  local rp = { unknown=0, mismatch=0, widened=0, oor=0, repeated=0, bodyDamaged=false, refused=false };
  local msg = To.MsgLoadRetain(inputs["damaged_repeat_wire"], store, rp);
  local clean = msg == null && rp.bodyDamaged && ("reason" in rp) && rp.reason == "malformed" && store.recs.len() == 0 && store.used == 0 && rp.retained == 0 && rp.retainLost == 0;
  print("strict_damaged:" + clean + ":" + rp.retained + ":" + store.recs.len() + "\n");
}

// Message form (form 2) batch with unknown peer fields, and refusal of form 2 on LoadRetain
{
  local annLen = Tn.AnnounceMeasure();
  local ann = blob(annLen);
  Tn.Announce(ann);
  local vocab = {};
  local arp = {};
  local annOk = To.AnnounceRead(vocab, ann, arp);

  local m0 = Tn.WireMsg(); m0.C.N = 7; m0.X = 42; m0.Extra = "hi"; m0.Neg = -5;
  local m1 = Tn.WireMsg(); m1.C.N = 9; m1.X = 84; m1.Extra = "yo"; m1.Neg = 3;
  local mrp = {};
  local batchLen = Tn.WireMsgMeasureMessages([m0, m1], mrp);
  local batch = blob(batchLen);
  local srep = {};
  Tn.WireMsgSaveMessages([m0, m1], batch, srep);

  // Passing a form-2 message batch to LoadRetain on a variable root refuses with message_form_as_file
  local f2Store = { capacity = 1048576, idCapacity = 1024 };
  local f2Rp = { unknown=0, mismatch=0, widened=0, oor=0, repeated=0, bodyDamaged=false, refused=false };
  local f2Msg = To.MsgLoadRetain(batch, f2Store, f2Rp);
  local cleanF2 = f2Msg == null && f2Rp.refused && ("reason" in f2Rp) && f2Rp.reason == "message_form_as_file" && f2Store.recs.len() == 0 && f2Store.used == 0;
  print("strict_form2_on_retain:" + cleanF2 + ":" + f2Rp.retained + ":" + f2Store.recs.len() + "\n");

  // Calling LoadRetain on a fixed table (WireMsg) refuses with retention_requires_variable_region_root
  local fxStore = { capacity = 1048576, idCapacity = 1024 };
  local fxRp = { unknown=0, mismatch=0, widened=0, oor=0, repeated=0, bodyDamaged=false, refused=false };
  local fxMsg = To.WireMsgLoadRetain(batch, fxStore, fxRp);
  local cleanFx = fxMsg == null && fxRp.refused && ("reason" in fxRp) && fxRp.reason == "retention_requires_variable_region_root" && fxStore.recs.len() == 0 && fxStore.used == 0;
  print("strict_fixed_retain:" + cleanFx + ":" + fxRp.retained + ":" + fxStore.recs.len() + "\n");

  // Clean message batch read on older WireMsg skips unknown fields and delivers both rows
  local outClean = [To.WireMsg(), To.WireMsg()];
  local lrep = {};
  local nClean = To.WireMsgLoadMessages(outClean, vocab, batch, lrep);
  local cleanBatchOk = annOk && arp.unknown == 0 && nClean == 2 && !lrep.refused && lrep.unknown == 4 &&
    outClean[0].C.N == 7 && outClean[0].X == 42 && outClean[1].C.N == 9 && outClean[1].X == 84;
  print("msg_batch_clean:" + cleanBatchOk + ":" + nClean + ":" + lrep.unknown + "\n");

  // Refused message batch (second message has X = 500 > 100) refuses whole batch and resets touched rows
  local mBad = Tn.WireMsg(); mBad.C.N = 9; mBad.X = 500; mBad.Extra = "yo";
  local badBatchLen = Tn.WireMsgMeasureMessages([m0, mBad], {});
  local badBatch = blob(badBatchLen);
  Tn.WireMsgSaveMessages([m0, mBad], badBatch, {});
  local outBad = [To.WireMsg(), To.WireMsg(), To.WireMsg()];
  outBad[0].X = 111; outBad[1].X = 222; outBad[2].X = 333;
  local brep = {};
  local nBad = To.WireMsgLoadMessages(outBad, vocab, badBatch, brep);
  local badBatchOk = nBad == -1 && brep.refused && brep.reason == "out_of_range" &&
    outBad[0].C.N == 0 && outBad[0].X == 0 && outBad[1].C.N == 0 && outBad[1].X == 0 && outBad[2].X == 333;
  print("msg_batch_oor:" + badBatchOk + ":" + nBad + ":" + outBad[0].X + "," + outBad[1].X + "," + outBad[2].X + "\n");
}
`
	return newerTypes, olderTypes, probe, inputs
}

func runSqRetainMessageProbe(t *testing.T, newerFiles, olderFiles map[string][]byte, probe string, inputs map[string][]byte) map[string]string {
	t.Helper()
	dir := t.TempDir()
	newerDir := filepath.Join(dir, "newer")
	olderDir := filepath.Join(dir, "older")
	if err := os.MkdirAll(newerDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(olderDir, 0o755); err != nil {
		t.Fatal(err)
	}
	for name, data := range newerFiles {
		writeFile(t, filepath.Join(newerDir, name), data)
	}
	for name, data := range olderFiles {
		writeFile(t, filepath.Join(olderDir, name), data)
	}
	writeFile(t, filepath.Join(dir, "main.nut"), []byte(probe))
	out := strings.TrimRight(string(sqrun.Run(t, dir, "main.nut", inputs)), "\n")
	answers := map[string]string{}
	for line := range strings.SplitSeq(out, "\n") {
		if name, _, ok := strings.Cut(line, ":"); ok {
			answers[name] = line
		}
	}
	return answers
}

func TestSquirrelRetainOnMessageForm(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "the pinned Squirrel interpreter")
	newerFiles, olderFiles, probe, inputs := sqRetainMessageSetup(t)
	normal := runSqRetainMessageProbe(t, newerFiles, olderFiles, probe, inputs)
	for _, c := range sqRetainMessageCases() {
		line := normal[c.name]
		if !strings.HasPrefix(line, c.name+":true:") {
			t.Errorf("%s: retain round trip %q, want %s:true:<retained>:0", c.name, line, c.name)
			continue
		}
		parts := strings.Split(line, ":")
		if len(parts) != 4 || parts[2] == "0" || parts[3] != "0" {
			t.Errorf("%s: retained,lost = %q, want a placed record and no loss", c.name, line)
		}
	}
	wantExact := map[string]string{
		"sealed":                 "sealed:true:1:0",
		"strict_integrity":       "strict_integrity:true:0:0",
		"strict_oor":             "strict_oor:true:0:0",
		"strict_mismatch":        "strict_mismatch:true:0:0",
		"strict_damaged":         "strict_damaged:true:0:0",
		"strict_form2_on_retain": "strict_form2_on_retain:true:0:0",
		"strict_fixed_retain":    "strict_fixed_retain:true:0:0",
		"msg_batch_clean":        "msg_batch_clean:true:2:4",
		"msg_batch_oor":          "msg_batch_oor:true:-1:0,0,333",
	}
	for k, want := range wantExact {
		if got := normal[k]; got != want {
			t.Errorf("%s: got %q, want %q (hex mismatch=%s)", k, got, want, hex.EncodeToString(inputs["mismatch_wire"]))
		}
	}
}

func TestSquirrelRetainOnMessageFormNegativeControl(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "the pinned Squirrel interpreter")
	newerFiles, olderFiles, probe, inputs := sqRetainMessageSetup(t)

	controls := []struct {
		name, file, from, to, caught string
	}{
		{
			name:   "retain strict oor and mismatch guard",
			file:   "schema_table_load.nut",
			from:   `    if (retain != null && (report.oor > 0 || report.mismatch > 0)) {`,
			to:     `    if (false && retain != null && (report.oor > 0 || report.mismatch > 0)) {`,
			caught: "strict_oor",
		},
		{
			name:   "retain strict bodyDamaged store clear",
			file:   "schema_table_load.nut",
			from:   `  if (retain != null && report.bodyDamaged) {`,
			to:     `  if (false && retain != null && report.bodyDamaged) {`,
			caught: "strict_damaged",
		},
		{
			name:   "retain fixed root refusal",
			file:   "schema_table_load.nut",
			from:   `    if (retain != null) return retainClear(retain, report, "retention_requires_variable_region_root", true);`,
			to:     `    if (false) return null;`,
			caught: "strict_fixed_retain",
		},
		{
			name:   "retain integrity trailer check",
			file:   "schema_table_load.nut",
			from:   `  if (b.len() < 5 || u32le(b, b.len() - 4) != crc32c(b, b.len() - 4)) return null;`,
			to:     `  if (b.len() < 5) return null;`,
			caught: "strict_integrity",
		},
		{
			name: "retain emit record placement",
			file: "schema_table_retain.nut",
			from: "    rec.placed = true; // the walk REACHED it, whatever came of the emit\n" +
				"    if (bad) {\n" +
				"      while (ids.list.len() > mark) delete ids.index[ids.list.pop()];\n" +
				"      while (store.ids.len() > idMark) store.ids.pop();\n" +
				"      store.emitLost = store.emitLost + 1;\n" +
				"      continue;\n" +
				"    }\n" +
				"    foreach (x in inner.b) w.b.append(x);",
			to:     "    rec.placed = true;\n    // sabotaged: drop retained bytes",
			caught: "extra",
		},
		{
			name:   "message batch strict out_of_range refusal",
			file:   "schema_table_message.nut",
			from:   `} else if (x < fd.lo || x > fd.hi) { refuseRead(r, "out_of_range"); return 0; }`,
			to:     `}`,
			caught: "msg_batch_oor",
		},
	}

	for _, c := range controls {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			src := string(olderFiles[c.file])
			if n := strings.Count(src, c.from); n != 1 {
				t.Fatalf("negative control %s: %s matched %d times, want 1: %q", c.name, c.file, n, c.from)
			}
			patchedOlder := maps.Clone(olderFiles)
			patchedOlder[c.file] = []byte(strings.Replace(src, c.from, c.to, 1))
			got := runSqRetainMessageProbe(t, newerFiles, patchedOlder, probe, inputs)
			if strings.HasPrefix(got[c.caught], c.caught+":true:") {
				t.Errorf("negative control %s: %s stayed green (%q)", c.name, c.caught, got[c.caught])
			}
		})
	}
}
