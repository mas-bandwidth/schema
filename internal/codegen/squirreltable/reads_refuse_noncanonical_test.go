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

	"github.com/mas-bandwidth/schema/v2/internal/codegen/squirrel"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/squirrel/sqrun"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/squirreltable"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/ir"
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
}

// TestSquirrelReadsRefuseNonCanonical holds Squirrel variable-form (form 1)
// and message-form (form 2 and its announcement) reads to the canonical
// writer contract (docs/SPEC-TABLES.md §3, §3.1, §3.3, §4; docs/rulings.json
// reads-refuse-non-canonical): every row in testdata/wire/non-canonical/ is
// driven through every read entry Squirrel carries for that form (plain load,
// retaining load, text wire load, announcement read, and message batch load;
// Squirrel Measure, MeasureRetain, and MeasureMessages size in-memory values
// rather than wire bytes). Canonical inputs read and re-save to identical
// bytes; every non-canonical spelling is refused; absent, unknown, and
// widened fields stay lawful.
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
	headID := fieldIDByName(t, noncanon.unit, "Root", "head")
	rows = append(rows,
		sqNonCanonicalRow{
			name:     "direct/explicit-null-pointer-alone",
			form:     "variable",
			kind:     "explicit-null-pointer",
			wantRead: false,
			unit:     noncanon,
			root:     "Root",
			// form 1; ref 1 -> head, kind 17 (pointer), index 0 (null); terminator 0; id table [headID], count 1
			wire: buildForm1File([]byte{0x01, 17, 0x00, 0x00}, []uint64{headID}),
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

	inputs := map[string][]byte{
		"ann_noncanonical": annNonCanon,
		"ann_messageone":   annMsgOne,
		"evo_lawful":       evoWire,
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
    }
  }
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
		if k == "evo" {
			got["evo"] = rest
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

	for i, row := range rows {
		wantHex := hex.EncodeToString(row.wire)
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
			want := fmt.Sprintf("read:%d:%s", len(row.wire), wantHex)
			if val != want {
				t.Errorf("%s [%s]: got %q, want %q", row.name, entry, val, want)
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
		}
	}
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
