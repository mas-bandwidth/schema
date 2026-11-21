package squirreltable_test

import (
	"bufio"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v3/internal/codegen/squirrel"
	"github.com/mas-bandwidth/schema/v3/internal/codegen/squirrel/sqrun"
	"github.com/mas-bandwidth/schema/v3/internal/codegen/squirreltable"
	"github.com/mas-bandwidth/schema/v3/internal/slowtest"
	"github.com/mas-bandwidth/schema/v3/internal/tabletext"
	"github.com/mas-bandwidth/schema/v3/internal/tablewire"
	"github.com/mas-bandwidth/schema/v3/ir"
)

type sqNonCanonicalUnit struct {
	key    string
	module string
	unit   *ir.Unit
}

type sqNonCanonicalRow struct {
	name     string
	file     string
	form     string
	kind     string
	wantRead bool
	unit     *sqNonCanonicalUnit
	root     string
	annKey   string
	wire     []byte
	// expect is the canonical file a read row re-saves to: the vector itself,
	// or the canonical rewrite pinned beside it for a lawful spelling no
	// writer produces (docs/SPEC-TABLES.md §3)
	expect []byte
}

// TestSquirrelReadsRefuseNonCanonical holds Squirrel variable-form (form 1)
// and message-form (form 2 and its announcement) reads to the canonical
// writer contract (docs/SPEC-TABLES.md §3, §3.1, §3.3, §4; docs/rulings.json
// reads-refuse-non-canonical): every row in testdata/wire/non-canonical/ is
// driven through every read entry Squirrel carries for that form (plain load,
// retaining load, text wire load, announcement read, and message batch load;
// Squirrel Measure, MeasureRetain, and MeasureMessages size in-memory values
// rather than wire bytes). A read row re-saves to its canonical bytes; a
// refused row is refused by every entry; absent, unknown, and widened fields
// stay lawful. Each message row is read again under another build version,
// where a field at its default and fields out of order read and rewrite to a
// batch that reads back canonical, and only a union field written with arm
// reference 0 stays refused (§3.3 (c)). A field the record does not declare
// is refused under the reader's own build version and read, counted unknown,
// under another (§3.3 (b)); a None element of an array of unions reads under
// both.
func TestSquirrelReadsRefuseNonCanonical(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "the pinned Squirrel interpreter")

	repo, err := sqrun.RepoRoot()
	if err != nil {
		t.Fatal(err)
	}

	dir := t.TempDir()
	generate := func(key string, paths []string) *sqNonCanonicalUnit {
		t.Helper()
		u := loadUnit(t, repo, paths)
		files, err := squirrel.Generate(u)
		if err != nil {
			t.Fatalf("%s: squirrel.Generate: %v", key, err)
		}
		tables, err := squirreltable.Generate(u)
		if err != nil {
			t.Fatalf("%s: squirreltable.Generate: %v", key, err)
		}
		out := filepath.Join(dir, key)
		if err := os.MkdirAll(out, 0o755); err != nil {
			t.Fatal(err)
		}
		for name, data := range files {
			writeFile(t, filepath.Join(out, name), data)
		}
		for name, data := range tables {
			writeFile(t, filepath.Join(out, name), data)
		}
		return &sqNonCanonicalUnit{
			key:    key,
			module: squirreltable.ModuleName(u),
			unit:   u,
		}
	}

	generateInline := func(key, src string) *sqNonCanonicalUnit {
		t.Helper()
		schemaDir := filepath.Join(dir, key+"_schema")
		if err := os.MkdirAll(schemaDir, 0o755); err != nil {
			t.Fatal(err)
		}
		schemaPath := filepath.Join(schemaDir, "Unit.schema")
		writeFile(t, schemaPath, []byte(src))
		u := loadUnit(t, schemaDir, []string{"Unit.schema"})
		files, err := squirrel.Generate(u)
		if err != nil {
			t.Fatalf("%s: squirrel.Generate: %v", key, err)
		}
		tables, err := squirreltable.Generate(u)
		if err != nil {
			t.Fatalf("%s: squirreltable.Generate: %v", key, err)
		}
		out := filepath.Join(dir, key)
		if err := os.MkdirAll(out, 0o755); err != nil {
			t.Fatal(err)
		}
		for name, data := range files {
			writeFile(t, filepath.Join(out, name), data)
		}
		for name, data := range tables {
			writeFile(t, filepath.Join(out, name), data)
		}
		return &sqNonCanonicalUnit{
			key:    key,
			module: squirreltable.ModuleName(u),
			unit:   u,
		}
	}

	noncanon := generate("noncanonical", []string{"test/non-canonical/NonCanonical.schema"})
	msgone := generate("messageone", []string{"test/message-one-encoding/One.schema"})

	vectorDir := filepath.Join(repo, "testdata", "wire", "non-canonical")
	manifestFile, err := os.Open(filepath.Join(vectorDir, "MANIFEST.tsv"))
	if err != nil {
		t.Fatal(err)
	}
	defer manifestFile.Close()

	var rows []sqNonCanonicalRow
	seenLocal := map[string]bool{}
	sc := bufio.NewScanner(manifestFile)
	lineNo := 0
	for sc.Scan() {
		lineNo++
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		cols := strings.Split(line, "\t")
		if len(cols) != 4 {
			t.Fatalf("MANIFEST.tsv:%d: got %d columns, want 4", lineNo, len(cols))
		}
		if lineNo == 1 && cols[0] == "file" && cols[3] == "verdict" {
			continue
		}
		rel := cols[0]
		form := cols[1]
		kind := cols[2]
		verdict := cols[3]
		if verdict != "read" && verdict != "refused" {
			t.Fatalf("MANIFEST.tsv:%d (%s): unexpected verdict %q", lineNo, rel, verdict)
		}
		wire, err := os.ReadFile(filepath.Join(vectorDir, filepath.FromSlash(rel)))
		if err != nil {
			t.Fatalf("MANIFEST.tsv:%d (%s): %v", lineNo, rel, err)
		}
		if !strings.Contains(rel, "/") {
			seenLocal[rel] = true
		}

		row := sqNonCanonicalRow{
			name:     rel,
			file:     rel,
			form:     form,
			kind:     kind,
			wantRead: verdict == "read",
			wire:     wire,
			expect:   wire,
		}
		if form == "variable" {
			rewrite := filepath.Join(vectorDir, "canonical", filepath.Base(rel))
			if data, err := os.ReadFile(rewrite); err == nil {
				row.expect = data
			} else if rel == "variable-fields-out-of-order.bin" {
				data, err := os.ReadFile(filepath.Join(vectorDir, "variable-canonical.bin"))
				if err != nil {
					t.Fatal(err)
				}
				row.expect = data
			}
		}
		switch {
		case strings.HasPrefix(rel, "../message-one-encoding/"):
			row.unit = msgone
			row.root = "One"
			row.annKey = "ann_messageone"
		case form == "variable":
			row.unit = noncanon
			row.root = "Root"
		case form == "message":
			row.unit = noncanon
			row.root = "Message"
			row.annKey = "ann_noncanonical"
		case form == "announcement":
			row.unit = noncanon
			row.root = "Message"
		default:
			t.Fatalf("MANIFEST.tsv:%d (%s): unsupported form %q", lineNo, rel, form)
		}
		rows = append(rows, row)
	}
	if err := sc.Err(); err != nil {
		t.Fatal(err)
	}
	if len(rows) == 0 {
		t.Fatal("MANIFEST.tsv named no rows")
	}

	entries, err := os.ReadDir(vectorDir)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".bin") && !seenLocal[e.Name()] {
			t.Fatalf("vector %s on disk is not reached by MANIFEST.tsv", e.Name())
		}
	}

	// Direct isolated spellings where no graph-orphan or dangling-index side
	// effect is present, plus lawful evolution cases (absent, unknown, widened).
	// An explicit null pointer reads and rewrites by the current writer rule
	// (docs/rulings.json reads-refuse-non-canonical, the 2026-11-15
	// correction): its rewrite is the empty root body.
	headID := fieldIDByName(t, noncanon.unit, "Root", "head")
	rows = append(rows,
		sqNonCanonicalRow{
			name:     "direct/explicit-null-pointer-alone",
			form:     "variable",
			kind:     "explicit-null-pointer",
			wantRead: true,
			unit:     noncanon,
			root:     "Root",
			// form 1; ref 1 -> head, kind 17 (pointer), index 0 (null); terminator 0; id table [headID], count 1
			wire:   buildForm1File([]byte{0x01, 17, 0x00, 0x00}, []uint64{headID}),
			expect: buildForm1File([]byte{0x00}, nil),
		},
		sqNonCanonicalRow{
			name:     "direct/empty-node-table-alone",
			form:     "variable",
			kind:     "empty-node-table",
			wantRead: false,
			unit:     noncanon,
			root:     "Root",
			// form 1; ref 1 -> NODE_TABLE_ID, kind 12, L=1, declared=0; terminator 0; id table [0xffffffffffffffff], count 1
			wire: buildForm1File([]byte{0x01, 12, 0x01, 0x00, 0x00}, []uint64{0xffffffffffffffff}),
		},
	)

	// The node table out of its place on its own (§3.1): the writer's file
	// with the node-table field moved to the front of the root body and the
	// id table renumbered to the first-use order of that body, so the
	// table's place is the one thing wrong; the writer's own file reads.
	nodePlace := generateInline("node_place", `package nodeplace
table Node { v int32 }
table Root
{
    a    int32
    head *Node
}
`)
	placed := engineFile(t, nodePlace.unit, "Root", `{"a":1,"head":{"v":2}}`)
	placedIDs := int(binary.LittleEndian.Uint64(placed[len(placed)-8:]))
	placedBody := placed[1 : len(placed)-8-8*placedIDs]
	var ids []uint64
	for i := range placedIDs {
		at := len(placed) - 8 - 8*placedIDs + 8*i
		ids = append(ids, binary.LittleEndian.Uint64(placed[at:at+8]))
	}
	if len(ids) != 5 || ids[0] != ir.TableWireId("a") || ids[1] != ir.TableWireId("head") || ids[2] != ir.TableNodeWireId ||
		len(placedBody) < 12 || placedBody[0] != 1 || placedBody[1] != byte(ir.TableKindI32) || placedBody[6] != 2 ||
		placedBody[7] != byte(ir.TableKindPointer) || placedBody[8] != 2 || placedBody[9] != 3 ||
		placedBody[10] != byte(ir.TableKindString) || placedBody[len(placedBody)-1] != 0 {
		t.Fatalf("the writer's layout moved: ids=%x body=%x", ids, placedBody)
	}
	field := func(ref byte, b []byte) []byte { return append([]byte{ref}, b[1:]...) }
	var misplacedBody []byte
	misplacedBody = append(misplacedBody, field(1, placedBody[9:len(placedBody)-1])...)
	misplacedBody = append(misplacedBody, field(2, placedBody[0:6])...)
	misplacedBody = append(misplacedBody, field(3, placedBody[6:9])...)
	misplacedBody = append(misplacedBody, 0)
	rows = append(rows,
		sqNonCanonicalRow{
			name:     "direct/node-table-out-of-place-alone",
			form:     "variable",
			kind:     "node-table-out-of-place",
			wantRead: false,
			unit:     nodePlace,
			root:     "Root",
			wire:     buildForm1File(misplacedBody, []uint64{ids[2], ids[0], ids[1], ids[3], ids[4]}),
		},
		sqNonCanonicalRow{
			name:     "direct/node-table-in-place",
			form:     "variable",
			kind:     "canonical",
			wantRead: true,
			unit:     nodePlace,
			root:     "Root",
			wire:     placed,
			expect:   placed,
		},
	)

	olderEvo := generateInline("evo_old", `package evo
table Root {
    mode uint8 = 7
    count uint16
}
`)
	// Unknown field from a newer writer + widened count (uint8 -> uint16) + absent mode (elided at default 7): lawful!
	evoCountID := fieldIDByName(t, olderEvo.unit, "Root", "count")
	const unknownFieldID uint64 = 0x123456789abcdef0
	var evoBody []byte
	if unknownFieldID != evoCountID {
		// first-use id order: unknownFieldID at ref 1 (uint8 = 9), evoCountID at ref 2 (uint8 = 5 widened into uint16)
		evoBody = []byte{0x01, 6, 9, 0x02, 6, 5, 0x00}
	}
	evoWire := buildForm1File(evoBody, []uint64{unknownFieldID, evoCountID})

	annNonCanon, err := os.ReadFile(filepath.Join(vectorDir, "announcement.bin"))
	if err != nil {
		t.Fatal(err)
	}
	annMsgOne, err := os.ReadFile(filepath.Join(repo, "testdata", "wire", "message-one-encoding", "announcement.bin"))
	if err != nil {
		t.Fatal(err)
	}

	// A field the record does not declare (§3.3 (b)): a newer writer's batch
	// carries `extra`, which the older reader cannot name.
	msgNew := generateInline("msg_new", `package msgnew
fixed table Message {
    mode  uint8 = 7
    count uint8
    extra uint8
}
`)
	msgOld := generateInline("msg_old", `package msgold
fixed table Message {
    mode  uint8 = 7
    count uint8
}
`)
	newerBatch := engineBatch(t, msgNew.unit, "Message", `{"count": 3, "extra": 5}`)

	// A None element of an array of unions is positional and reads (§3.3 (c)).
	msgNone := generateInline("msg_none", `package msgnone
union Choice {
    item uint8
}
fixed table Root {
    tick    uint8
    choice  Choice
    choices [..2]Choice
}
`)
	noneBatch := engineBatch(t, msgNone.unit, "Root", `{"tick": 1, "choices": [{}, {"item": 3}]}`)

	inputs := map[string][]byte{
		"ann_noncanonical": annNonCanon,
		"ann_messageone":   annMsgOne,
		"evo_lawful":       evoWire,
		"newer_batch":      newerBatch,
		"none_batch":       noneBatch,
	}

	var probe strings.Builder
	probe.WriteString("local inputs = dofile(\"inputs.nut\", true);\n")
	probe.WriteString(`local HEXB = array(256);
for (local i = 0; i < 256; i++) HEXB[i] = format("%02x", i);
local join = function(a) {
  if (a.len() == 0) return "";
  while (a.len() > 1) {
    local b = array((a.len() + 1) / 2);
    for (local i = 0; i < b.len(); i++) b[i] = (2 * i + 1 < a.len()) ? a[2 * i] + a[2 * i + 1] : a[2 * i];
    a = b;
  }
  return a[0];
};
local hexOf = function(b) {
  if (b == null) return "null";
  local parts = array(b.len());
  for (local i = 0; i < b.len(); i++) parts[i] = HEXB[b[i] & 0xff];
  return join(parts);
};
local strOf = function(b) {
  local parts = array(b.len());
  for (local i = 0; i < b.len(); i++) parts[i] = format("%c", b[i] & 0xff);
  return join(parts);
};
local mods = {};
local getMod = function(unit, mod) {
  if (!(unit in mods)) mods[unit] <- dofile(unit + "/schema_module.nut", true);
  return mods[unit];
};
`)

	probe.WriteString("local rows = [\n")
	for i, row := range rows {
		wKey := fmt.Sprintf("w_%d", i)
		tKey := fmt.Sprintf("t_%d", i)
		inputs[wKey] = row.wire
		if row.form == "variable" {
			inputs[tKey] = []byte(base64.StdEncoding.EncodeToString(row.wire))
		}
		fmt.Fprintf(&probe, "  { idx=%d, form=%q, unit=%q, mod=%q, root=%q, w=%q, t=%q, ann=%q },\n",
			i, row.form, row.unit.key, row.unit.module, row.root, wKey, tKey, row.annKey)
	}
	probe.WriteString("];\n")
	probe.WriteString(`foreach (row in rows) {
  local schema = getMod(row.unit, row.mod);
  local T = schema.require(row.mod);
  local wire = inputs[row.w];
  if (row.form == "variable") {
    local R = schema.require("schema_table_refusal");
    local L = schema.require("schema_table_load");
    local TX = schema.require("schema_table_text");
    // 1. Plain load (<Root>Load) + Measure/Save round-trip
    local r1 = {};
    local v1 = T[row.root + "Load"](wire, r1);
    if (v1 == null || ("refused" in r1 && r1.refused)) {
      print(row.idx + ":load:refused\n");
    } else {
      local m1 = T[row.root + "Measure"](v1);
      local s1 = T[row.root + "Save"](v1);
      print(row.idx + ":load:read:" + m1 + ":" + hexOf(s1) + "\n");
    }
    // 2. Retaining load (<Root>LoadRetain) + MeasureRetain/SaveRetain round-trip
    local store = { capacity = 1048576, idCapacity = 1024 };
    local r2 = {};
    local v2 = T[row.root + "LoadRetain"](wire, store, r2);
    if (v2 == null || ("refused" in r2 && r2.refused)) {
      local clean = ("recs" in store ? store.recs.len() == 0 : true) && ("used" in store ? store.used == 0 : true);
      print(row.idx + ":retain:" + (clean ? "refused" : "refused_dirty_store") + "\n");
    } else {
      local m2 = T[row.root + "MeasureRetain"](v2, store);
      local out2 = m2 >= 0 ? blob(m2) : null;
      local sr2 = {};
      local n2 = out2 != null ? T[row.root + "SaveRetain"](v2, store, out2, sr2) : -1;
      print(row.idx + ":retain:read:" + n2 + ":" + hexOf(out2) + "\n");
    }
    // 3. Text wire load (schema_table_text.LoadText)
    local r3 = {};
    local v3 = TX.LoadText(T, row.root, strOf(inputs[row.t]), r3);
    if (v3 == null || ("refused" in r3 && r3.refused)) {
      print(row.idx + ":text:refused\n");
    } else {
      local s3 = T[row.root + "Save"](v3);
      print(row.idx + ":text:read:" + s3.len() + ":" + hexOf(s3) + "\n");
    }
    // 4. ReadStrict (schema_table_refusal.ReadStrict)
    local r4 = {};
    local v4 = R.ReadStrict(T, row.root, wire, r4);
    if (v4 == null || ("refused" in r4 && r4.refused)) {
      print(row.idx + ":strict:refused\n");
    } else {
      local s4 = T[row.root + "Save"](v4);
      print(row.idx + ":strict:read:" + s4.len() + ":" + hexOf(s4) + "\n");
    }
    // 5. Walk load (schema_table_load.loadWalk)
    local r5 = L.report(null);
    local v5 = L.loadWalk(T, row.root, wire, r5);
    if (v5 == null || r5.bodyDamaged || r5.refused) {
      print(row.idx + ":walk:refused\n");
    } else {
      local s5 = T[row.root + "Save"](v5);
      print(row.idx + ":walk:read:" + s5.len() + ":" + hexOf(s5) + "\n");
    }
  } else if (row.form == "announcement") {
    local vocab = {};
    local ar = {};
    local ok = T.AnnounceRead(vocab, wire, ar);
    if (!ok || ("refused" in ar && ar.refused) || ("bodyDamaged" in ar && ar.bodyDamaged)) {
      print(row.idx + ":announce:refused\n");
    } else {
      print(row.idx + ":announce:read\n");
    }
  } else if (row.form == "message") {
    local vocab = {};
    local ar = {};
    if (!T.AnnounceRead(vocab, inputs[row.ann], ar)) {
      print(row.idx + ":message:announce_failed\n");
    } else {
      local out = [T[row.root]()];
      out[0].Mode = 99;
      local mr = {};
      local n = T[row.root + "LoadMessages"](out, vocab, wire, mr);
      if (n != 1 || ("refused" in mr && mr.refused) || ("bodyDamaged" in mr && mr.bodyDamaged)) {
        local resetOk = out[0].Mode == 7;
        print(row.idx + ":message:" + (resetOk ? "refused" : "refused_dirty_slot") + "\n");
      } else {
        local szr = {};
        local z = T[row.root + "MeasureMessages"](out, szr);
        local b = z >= 0 ? blob(z) : null;
        local sr = {};
        local w = b != null ? T[row.root + "SaveMessages"](out, b, sr) : -1;
        print(row.idx + ":message:read:" + w + ":" + hexOf(b) + "\n");
      }
      // the same batch under ANOTHER build version (§3.3): its rewrite must
      // read back under the announcement as written, to the same bytes
      local other = {};
      T.AnnounceRead(other, inputs[row.ann], {});
      other.BuildVersion = other.BuildVersion ^ 1;
      local o = [T[row.root]()];
      local orep = {};
      local on = T[row.root + "LoadMessages"](o, other, wire, orep);
      if (on != 1 || ("refused" in orep && orep.refused) || ("bodyDamaged" in orep && orep.bodyDamaged)) {
        print(row.idx + ":other:refused\n");
      } else {
        local oz = T[row.root + "MeasureMessages"](o, {});
        local ob = oz >= 0 ? blob(oz) : null;
        if (ob == null || T[row.root + "SaveMessages"](o, ob, {}) < 0) {
          print(row.idx + ":other:save_refused\n");
        } else {
          local again = [T[row.root]()];
          local ar2 = {};
          local an = T[row.root + "LoadMessages"](again, vocab, ob, ar2);
          if (an != 1 || ("refused" in ar2 && ar2.refused) || ("bodyDamaged" in ar2 && ar2.bodyDamaged)) {
            print(row.idx + ":other:rewrite_refused\n");
          } else {
            local tz = T[row.root + "MeasureMessages"](again, {});
            local tb = tz >= 0 ? blob(tz) : null;
            if (tb != null) T[row.root + "SaveMessages"](again, tb, {});
            print(row.idx + ":other:" + (hexOf(tb) == hexOf(ob) ? "read" : "rewrite_not_canonical") + "\n");
          }
        }
      }
    }
  }
}
// A field the record does not declare: refused under the reader's own build
// version, read and counted unknown under another
{
  local TN = getMod("msg_new", ` + fmt.Sprintf("%q", msgNew.module) + `).require(` + fmt.Sprintf("%q", msgNew.module) + `);
  local TO = getMod("msg_old", ` + fmt.Sprintf("%q", msgOld.module) + `).require(` + fmt.Sprintf("%q", msgOld.module) + `);
  local own = {};
  TO.AnnounceRead(own, TO.TableMessageAnnouncement, {});
  local vocab = {};
  TN.AnnounceRead(vocab, TN.TableMessageAnnouncement, {});
  local out = [TO.Message()];
  local rep = {};
  local n = TO.MessageLoadMessages(out, vocab, inputs["newer_batch"], rep);
  local otherOk = n == 1 && !(("refused" in rep) && rep.refused) && rep.unknown == 1 && out[0].Count == 3;
  vocab.BuildVersion = own.BuildVersion;
  out = [TO.Message()];
  rep = {};
  n = TO.MessageLoadMessages(out, vocab, inputs["newer_batch"], rep);
  local sameRefused = n != 1 || (("refused" in rep) && rep.refused) || (("bodyDamaged" in rep) && rep.bodyDamaged);
  print("undeclared:" + (otherOk ? "other_read" : "other_bad") + "," + (sameRefused ? "same_refused" : "same_read") + "\n");
}
// A None element of an array of unions reads under this build and another
{
  local TU = getMod("msg_none", ` + fmt.Sprintf("%q", msgNone.module) + `).require(` + fmt.Sprintf("%q", msgNone.module) + `);
  local vocab = {};
  TU.AnnounceRead(vocab, TU.TableMessageAnnouncement, {});
  local verdicts = [];
  foreach (pass in [0, 1]) {
    local out = [TU.Root()];
    local rep = {};
    local n = TU.RootLoadMessages(out, vocab, inputs["none_batch"], rep);
    local ok = n == 1 && !(("refused" in rep) && rep.refused) && out[0].Tick == 1 &&
      out[0].Choices.len() == 2 && out[0].Choices[0].Type == 0 && out[0].Choices[1].Type != 0 &&
      out[0].Choice.Type == 0;
    verdicts.append(ok ? "read" : "bad");
    vocab.BuildVersion = vocab.BuildVersion ^ 1;
  }
  print("noneelement:" + verdicts[0] + "," + verdicts[1] + "\n");
}
// Lawful evolution check: absent field + unknown field + widened field still read
{
  local schema = getMod("evo_old", ` + fmt.Sprintf("%q", olderEvo.module) + `);
  local T = schema.require(` + fmt.Sprintf("%q", olderEvo.module) + `);
  local r = {};
  local v = T.RootLoad(inputs["evo_lawful"], r);
  local st = { capacity = 1048576, idCapacity = 1024 };
  local rr = {};
  local vr = T.RootLoadRetain(inputs["evo_lawful"], st, rr);
  local ok = v != null && !r.refused && v.Mode == 7 && v.Count == 5 && r.unknown == 1 && r.widened == 1 &&
             vr != null && !rr.refused && vr.Mode == 7 && vr.Count == 5 && rr.retained == 1;
  print("evo:" + (ok ? "ok" : "bad") + "\n");
}
`)

	writeFile(t, filepath.Join(dir, "main.nut"), []byte(probe.String()))
	rawOut := strings.TrimRight(string(sqrun.Run(t, dir, "main.nut", inputs)), "\n")

	got := map[string]string{}
	for line := range strings.SplitSeq(rawOut, "\n") {
		k, rest, ok := strings.Cut(line, ":")
		if !ok {
			t.Fatalf("unexpected probe line %q", line)
		}
		if k == "evo" || k == "undeclared" || k == "noneelement" {
			got[k] = rest
			continue
		}
		entry, val, ok := strings.Cut(rest, ":")
		if !ok {
			t.Fatalf("unexpected probe row line %q", line)
		}
		got[k+":"+entry] = val
	}

	if got["evo"] != "ok" {
		t.Errorf("lawful evolution (absent + unknown + widened): got %q, want ok", got["evo"])
	}
	if got["undeclared"] != "other_read,same_refused" {
		t.Errorf("a field the record does not declare: got %q, want other_read,same_refused", got["undeclared"])
	}
	if got["noneelement"] != "read,read" {
		t.Errorf("a None element of an array of unions (this build, another): got %q, want read,read", got["noneelement"])
	}

	for i, row := range rows {
		wantHex := hex.EncodeToString(row.expect)
		checkEntry := func(entry string) {
			key := fmt.Sprintf("%d:%s", i, entry)
			val, ok := got[key]
			if !ok {
				t.Errorf("%s [%s]: probe produced no result", row.name, entry)
				return
			}
			if !row.wantRead {
				if val != "refused" {
					t.Errorf("%s (%s) [%s]: got %q, want refused", row.name, row.kind, entry, val)
				}
				return
			}
			if row.form == "announcement" {
				if val != "read" {
					t.Errorf("%s [%s]: got %q, want read", row.name, entry, val)
				}
				return
			}
			want := fmt.Sprintf("read:%d:%s", len(row.expect), wantHex)
			if val != want {
				t.Errorf("%s [%s]: got %q, want %q", row.name, entry, val, want)
			}
		}
		// under another build version only a union field written with arm
		// reference 0 stays refused (§3.3 (c))
		checkOther := func() {
			key := fmt.Sprintf("%d:other", i)
			want := "read"
			if row.kind == "union-arm-zero" {
				want = "refused"
			}
			if got[key] != want {
				t.Errorf("%s (%s) [another build]: got %q, want %q", row.name, row.kind, got[key], want)
			}
		}

		switch row.form {
		case "variable":
			checkEntry("load")
			checkEntry("strict")
			checkEntry("walk")
			checkEntry("retain")
			checkEntry("text")
		case "announcement":
			checkEntry("announce")
		case "message":
			checkEntry("message")
			checkOther()
		}
	}
}

// engineFile is the Go engine's variable-form file of one value given as
// table JSON.
func engineFile(t *testing.T, u *ir.Unit, table, value string) []byte {
	t.Helper()
	m := tabletext.NewModel(u)
	inst := m.New(m.Lookup(table))
	var report tabletext.Report
	if !m.Read(inst, []byte(value), &report) {
		t.Fatalf("%s %s: %+v", table, value, report)
	}
	file, err := tablewire.Encode(m, inst)
	if err != nil {
		t.Fatal(err)
	}
	return file
}

// engineBatch is the Go engine's message batch of one value given as table
// JSON: the writer every leg's batch is held to.
func engineBatch(t *testing.T, u *ir.Unit, table, value string) []byte {
	t.Helper()
	m := tabletext.NewModel(u)
	inst := m.New(m.Lookup(table))
	var report tabletext.Report
	if !m.Read(inst, []byte(value), &report) {
		t.Fatalf("%s %s: %+v", table, value, report)
	}
	batch, err := tablewire.EncodeMessage(m, inst)
	if err != nil {
		t.Fatal(err)
	}
	return batch
}

func fieldIDByName(t *testing.T, u *ir.Unit, tableName, fieldName string) uint64 {
	t.Helper()
	st := u.Tables[tableName]
	if st == nil {
		t.Fatalf("table %s not found", tableName)
	}
	for _, f := range st.Fields {
		if f.Name == fieldName {
			return ir.TableFieldWireId(f)
		}
	}
	t.Fatalf("field %s.%s not found", tableName, fieldName)
	return 0
}

func buildForm1File(body []byte, ids []uint64) []byte {
	out := make([]byte, 0, 1+len(body)+8*len(ids)+8)
	out = append(out, 1)
	out = append(out, body...)
	var word [8]byte
	for _, id := range ids {
		binary.LittleEndian.PutUint64(word[:], id)
		out = append(out, word[:]...)
	}
	binary.LittleEndian.PutUint64(word[:], uint64(len(ids)))
	out = append(out, word[:]...)
	return out
}
