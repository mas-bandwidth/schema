package mootable_test

import (
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/compiler"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// TestMooReadsRefuseNonCanonical holds the moo table reads to the shared
// non-canonical spelling vectors (docs/SPEC-TABLES.md §3, §3.1, §3.3, §4;
// docs/rulings.json reads-refuse-non-canonical): every row of
// testdata/wire/non-canonical/MANIFEST.tsv is generated for this leg and read
// through every read entry it has for the row's form: the plain load, the
// form dispatch's strict read and the retaining load for the variable form,
// the message load for the message form, the announcement read for the
// announcement. A variable row that reads re-saves through the plain and the
// retaining writer to its canonical bytes: the vector itself, or the
// canonical rewrite pinned beside it for a lawful spelling no writer
// produces. A message row is read under the announcement as written, where
// the manifest's verdict holds and a read re-saves to the same bytes, and
// under another build version, where a field at its default and fields out of
// order are lawful evolution and read, and a union field written with arm
// reference 0 (§3.3 (c)) stays refused. The verdict is the contract, so no row
// compares a refusal's reason. moo refuses in every build.

type nonCanonicalRow struct {
	file    string
	form    string
	spell   string
	verdict string
}

func readNonCanonicalManifest(t *testing.T) []nonCanonicalRow {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(root, "testdata", "wire", "non-canonical", "MANIFEST.tsv"))
	if err != nil {
		t.Fatal(err)
	}
	var rows []nonCanonicalRow
	for i, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		f := strings.Split(line, "\t")
		if len(f) != 4 {
			t.Fatalf("MANIFEST.tsv:%d: %d columns, want file, form, case, verdict", i+1, len(f))
		}
		if f[0] == "file" {
			continue
		}
		switch f[1] {
		case "variable", "message", "announcement":
		default:
			t.Fatalf("MANIFEST.tsv:%d: form %q", i+1, f[1])
		}
		if f[3] != "read" && f[3] != "refused" {
			t.Fatalf("MANIFEST.tsv:%d: verdict %q", i+1, f[3])
		}
		rows = append(rows, nonCanonicalRow{file: f[0], form: f[1], spell: f[2], verdict: f[3]})
	}
	if len(rows) == 0 {
		t.Fatal("MANIFEST.tsv names no rows")
	}
	return rows
}

// nonCanonicalExpected is the canonical file a variable row that reads
// re-saves to: the canonical rewrite the manifest's generator pins under
// canonical/, the canonical vector for the reordered fields, else the vector.
func nonCanonicalExpected(t *testing.T, row nonCanonicalRow) []byte {
	t.Helper()
	dir := filepath.Join("testdata", "wire", "non-canonical")
	path := filepath.Join(dir, row.file)
	if row.form == "variable" {
		rewrite := filepath.Join(dir, "canonical", filepath.Base(row.file))
		if _, err := os.Stat(filepath.Join(root, rewrite)); err == nil {
			path = rewrite
		} else if row.file == "variable-fields-out-of-order.bin" {
			path = filepath.Join(dir, "variable-canonical.bin")
		}
	}
	return readRoot(t, path)
}

// nonCanonicalUnit is one vectors unit's emitted moo files.
type nonCanonicalUnit struct {
	unit  *ir.Unit
	files map[string][]byte
}

func generateNonCanonicalUnit(t *testing.T, schema string) nonCanonicalUnit {
	t.Helper()
	u, err := compiler.New().Load([]string{filepath.Join(root, schema)})
	if err != nil {
		t.Fatalf("%s: %v", schema, err)
	}
	files, err := generate(u)
	if err != nil {
		t.Fatalf("%s: %v", schema, err)
	}
	return nonCanonicalUnit{unit: u, files: files}
}

const nonCanonicalHex = `local function hexOf(b: buffer): string
	local parts = table.create(buffer.len(b))
	for i = 0, buffer.len(b) - 1 do
		parts[i + 1] = string.format("%02x", buffer.readu8(b, i))
	end
	return table.concat(parts)
end
local function verdict(ok: boolean): string
	return if ok then "read" else "refused"
end
`

// nonCanonicalProbe is the main for one row: one "entry=verdict" line per
// read entry, and for a read value the re-saved bytes as "<writer>=<hex>".
// A message row is read again under another build version ("other"); a batch
// read there is saved, read back under the announced build and saved again
// ("otherback", "othertwice").
const nonCanonicalProbe = `local rootName = %q
local form = %q
if form == "announcement" then
	local v = MS.NewVocabulary()
	print("announcement=" .. verdict(MS.AnnounceRead(v, inputs.wire, {})))
elseif form == "variable" then
	local plain = L.load(rootName, inputs.wire, L.newReport())
	print("plain=" .. verdict(plain ~= nil))
	local strict = Rf.ReadStrict(T, rootName, inputs.wire, L.newReport())
	print("strict=" .. verdict(strict ~= nil))
	local store = Rt.newStore(65536)
	local retained = Rt.LoadRetain(rootName, inputs.wire, L.newReport(), store)
	print("retain=" .. verdict(retained ~= nil))
	if plain ~= nil then
		local out = Sv.save(St.lookup(rootName), plain)
		print("save=" .. (if out ~= nil then hexOf(out) else "refused"))
	end
	if retained ~= nil then
		local out = Rt.SaveRetain(rootName, retained, store, L.newReport())
		print("saveretain=" .. (if out ~= nil then hexOf(out) else "refused"))
	end
else
	local v = MS.NewVocabulary()
	if not MS.AnnounceRead(v, inputs.ann, {}) then
		print("announce=refused")
		return
	end
	local n, values = MS.LoadMessages(v, rootName, inputs.wire, 256, {})
	print("message=" .. verdict(n > 0))
	if n > 0 then
		local out = MS.SaveMessages(rootName, values, {})
		print("save=" .. (if out ~= nil then hexOf(out) else "refused"))
	end
	local announced = v.buildVersion
	v.buildVersion = announced .. "-other"
	local on, ovalues = MS.LoadMessages(v, rootName, inputs.wire, 256, {})
	print("other=" .. verdict(on > 0))
	if on > 0 then
		local out = MS.SaveMessages(rootName, ovalues, {})
		if out == nil then
			print("otherback=refused")
			return
		end
		v.buildVersion = announced
		local bn, back = MS.LoadMessages(v, rootName, out, 256, {})
		print("otherback=" .. verdict(bn > 0))
		if bn > 0 then
			local twice = MS.SaveMessages(rootName, back, {})
			print("othertwice=" .. (if twice ~= nil and hexOf(twice) == hexOf(out) then "same" else "differs"))
		end
	end
end
`

func runNonCanonicalProbe(t *testing.T, g *generatedUnit, mod, rootName, form string, inputs map[string][]byte) map[string]string {
	t.Helper()
	main := "--!strict\nlocal inputs = require(\"./inputs\")\nlocal L = require(\"./table_load\")\nlocal Rf = require(\"./table_refusal\")\nlocal Rt = require(\"./table_retain\")\nlocal Sv = require(\"./table_save\")\nlocal MS = require(\"./table_message\")\nlocal St = require(\"./schema_tables\")\n" +
		g.requireAll() + fmt.Sprintf("local T = require(\"./%s\")\n", mod) + nonCanonicalHex + fmt.Sprintf(nonCanonicalProbe, rootName, form)
	out := g.run(t, main, inputs)
	got := map[string]string{"probe": out}
	for line := range strings.SplitSeq(out, "\n") {
		if k, v, ok := strings.Cut(strings.TrimSpace(line), "="); ok {
			got[k] = v
		}
	}
	return got
}

func TestMooReadsRefuseNonCanonical(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "moo (it runs the emitted table_load.moo, table_retain.moo and table_message.moo)")
	rows := readNonCanonicalManifest(t)
	units := map[string]nonCanonicalUnit{
		"noncanonical": generateNonCanonicalUnit(t, "test/non-canonical/NonCanonical.schema"),
		"one":          generateNonCanonicalUnit(t, "test/message-one-encoding/One.schema"),
	}
	if len(rows) == 0 {
		t.Fatal("rows is empty: the loop below would check nothing")
	}
	for _, row := range rows {
		t.Run(row.file, func(t *testing.T) {
			t.Parallel()
			unit, rootName, mod := "noncanonical", "Root", "NonCanonicalTable"
			ann := filepath.Join("testdata", "wire", "non-canonical", "announcement.bin")
			if strings.HasPrefix(row.file, "../message-one-encoding/") {
				unit, rootName, mod = "one", "One", "OneTable"
				ann = filepath.Join("testdata", "wire", "message-one-encoding", "announcement.bin")
			} else if row.form == "message" {
				rootName = "Message"
			}
			nu := units[unit]
			if _, ok := nu.files[mod+".moo"]; !ok {
				t.Fatalf("unit %s emitted no %s.moo", unit, mod)
			}
			g := writeUnit(t, nu.unit, nu.files)
			inputs := map[string][]byte{"wire": readRoot(t, filepath.Join("testdata", "wire", "non-canonical", row.file))}
			if row.form == "message" {
				inputs["ann"] = readRoot(t, ann)
			}
			got := runNonCanonicalProbe(t, g, mod, rootName, row.form, inputs)
			var entries, saves []string
			switch row.form {
			case "announcement":
				entries = []string{"announcement"}
			case "variable":
				entries = []string{"plain", "strict", "retain"}
				saves = []string{"save", "saveretain"}
			case "message":
				entries = []string{"message"}
				saves = []string{"save"}
			}
			for _, entry := range entries {
				if got[entry] != row.verdict {
					t.Errorf("%s (%s): %s = %q, want %q\nprobe:\n%s", row.file, row.spell, entry, got[entry], row.verdict, got["probe"])
				}
			}
			if row.form == "message" {
				// §3.3: under another build version only the union field
				// written with arm reference 0 stays refused
				other := "read"
				if row.spell == "union-arm-zero" {
					other = "refused"
				}
				if got["other"] != other {
					t.Errorf("%s (%s): under another build the message load = %q, want %q\nprobe:\n%s", row.file, row.spell, got["other"], other, got["probe"])
				}
				if other == "read" && (got["otherback"] != "read" || got["othertwice"] != "same") {
					t.Errorf("%s (%s): the rewrite under another build reads back %q and re-saves %q, want read and same\nprobe:\n%s", row.file, row.spell, got["otherback"], got["othertwice"], got["probe"])
				}
			}
			if row.verdict != "read" {
				return
			}
			want := hex.EncodeToString(nonCanonicalExpected(t, row))
			for _, s := range saves {
				if got[s] != want {
					t.Errorf("%s: the read value re-saves (%s) to %q, want %s", row.file, s, got[s], want)
				}
			}
		})
	}
	t.Run("union None element", testMooUnionNoneElementReads)
	t.Run("unknown union field at None", testMooUnknownUnionFieldNoneRefused)
	t.Run("undeclared field at the reader's own build", testMooUndeclaredFieldAtOwnBuildRefused)
	t.Run("node table not last, id table in first-use order", testMooNodeTablePlacementAlone)
	t.Run("present optional at its default", testMooPresentOptionalAtDefault)
}

// testMooPresentOptionalAtDefault shows what moo does today with a present
// optional holding its declared default (an evolution T -> ?T): the message
// load at the reader's own build version reads the Go writer's batch, which
// carries the field, as the Go engine does; the at-default rule judges only a
// field that is neither optional nor required.
func testMooPresentOptionalAtDefault(t *testing.T) {
	t.Parallel()
	u := unitFromSource(t, "package probe\n\nfixed table Root\n{\n    tick uint8\n    o    ?uint8\n}\n")
	files, err := generate(u)
	if err != nil {
		t.Fatal(err)
	}
	g := writeUnit(t, u, files)
	m := tabletext.NewModel(u)
	value := m.New(m.Lookup("Root"))
	var report tabletext.Report
	if !m.Read(value, []byte(`{"tick": 1, "o": 0}`), &report) {
		t.Fatalf("value: %+v", report)
	}
	batch, err := tablewire.EncodeMessage(m, value)
	if err != nil {
		t.Fatal(err)
	}
	absent := m.New(m.Lookup("Root"))
	if !m.Read(absent, []byte(`{"tick": 1}`), &report) {
		t.Fatalf("value: %+v", report)
	}
	if elided, err := tablewire.EncodeMessage(m, absent); err != nil || hex.EncodeToString(elided) == hex.EncodeToString(batch) {
		t.Fatalf("the present optional at its default is not on the wire: %x (%v)", batch, err)
	}
	var vocab tablewire.Vocabulary
	if err := vocab.AnnounceRead(tablewire.Announce(u), &report); err != nil {
		t.Fatal(err)
	}
	if ok, err := tablewire.DecodeMessage(m, m.New(m.Lookup("Root")), batch, &vocab, &report); !ok || err != nil {
		t.Fatalf("the Go engine refuses the present optional at its default: %v", err)
	}
	got := runNonCanonicalProbe(t, g, "ProbeTable", "Root", "message", map[string][]byte{"wire": batch, "ann": tablewire.Announce(u)})
	if got["message"] != "read" || got["save"] != hex.EncodeToString(batch) {
		t.Errorf("a present optional at its default: message=%q save=%q, want read and %x\nprobe:\n%s", got["message"], got["save"], batch, got["probe"])
	}
}

// testMooNodeTablePlacementAlone holds §3.1's placement rule by itself: a
// root whose node table rides before a field, with the id table in the order
// the walk first meets each entry (the root body, then the records), so no
// other refusal reaches it. The same root with the node table last reads,
// and the Go engine decodes it and re-encodes the same bytes; both loads
// refuse the misplaced table.
func testMooNodeTablePlacementAlone(t *testing.T) {
	t.Parallel()
	u := unitFromSource(t, "package probe\n\ntable Leaf\n{\n    v int32\n}\n\ntable Root\n{\n    head *Leaf\n    tail int32\n}\n")
	files, err := generate(u)
	if err != nil {
		t.Fatal(err)
	}
	g := writeUnit(t, u, files)
	leaf := ir.TableWireId(u.Tables["Leaf"].WireName())
	spell := func(nodeTableLast bool) []byte {
		b := &wb{}
		order := []uint64{ir.TableWireId("head"), ir.TableWireId("tail"), ir.TableNodeWireId, leaf, ir.TableWireId("v")}
		if !nodeTableLast {
			order[1], order[2] = order[2], order[1]
		}
		b.ids = order
		refOf := func(id uint64) uint64 {
			for i, e := range b.ids {
				if e == id {
					return uint64(i) + 1
				}
			}
			t.Fatalf("id %x is not in the table", id)
			return 0
		}
		nodeTable := func() {
			b.leb(refOf(ir.TableNodeWireId))
			b.u8(ir.TableKindString)
			at := b.openLen()
			b.leb(1)
			b.leb(refOf(leaf))
			rec := b.openLen()
			b.i32("v", 5)
			b.end()
			b.closeLen(rec)
			b.closeLen(at)
		}
		b.field("head", ir.TableKindPointer)
		b.leb(2)
		if !nodeTableLast {
			nodeTable()
		}
		b.i32("tail", 9)
		if nodeTableLast {
			nodeTable()
		}
		b.end()
		return b.finish()
	}
	m := tabletext.NewModel(u)
	for _, last := range []bool{true, false} {
		wire := spell(last)
		want := "refused"
		if last {
			want = "read"
		}
		inst := m.New(m.Lookup("Root"))
		var report tabletext.Report
		ok, err := tablewire.Decode(m, inst, wire, &report)
		engine := "refused"
		if ok && err == nil {
			engine = "read"
			again, err := tablewire.EncodeVariable(m, inst)
			if err != nil || hex.EncodeToString(again) != hex.EncodeToString(wire) {
				t.Fatalf("node table last: the Go engine re-encodes %x (%v), want %x", again, err, wire)
			}
		}
		if engine != want {
			t.Fatalf("node table last=%v: the Go engine answers %s, want %s (%v %+v)", last, engine, want, err, report)
		}
		got := runNonCanonicalProbe(t, g, "ProbeTable", "Root", "variable", map[string][]byte{"wire": wire})
		for _, entry := range []string{"plain", "strict", "retain"} {
			if got[entry] != want {
				t.Errorf("node table last=%v: %s = %q, want %q\nprobe:\n%s", last, entry, got[entry], want, got["probe"])
			}
		}
	}
}

// testMooUnionNoneElementReads is the control for §3.3 (c): the refusal is of
// a union FIELD written with arm reference 0, never of a None element of an
// array of unions, which is positional. The Go writer's batch carrying one
// reads under this reader's build version and under another.
func testMooUnionNoneElementReads(t *testing.T) {
	t.Parallel()
	const schema = "package messagenone\n\nunion Choice\n{\n    item uint8\n}\n\nfixed table Root\n{\n    tick    uint8\n    choice  Choice\n    choices [..2]Choice\n}\n"
	u := unitFromSource(t, schema)
	files, err := generate(u)
	if err != nil {
		t.Fatal(err)
	}
	g := writeUnit(t, u, files)
	m := tabletext.NewModel(u)
	value := m.New(m.Lookup("Root"))
	var report tabletext.Report
	if !m.Read(value, []byte(`{"tick": 1, "choices": [{}, {"item": 3}]}`), &report) {
		t.Fatalf("value: %+v", report)
	}
	batch, err := tablewire.EncodeMessage(m, value)
	if err != nil {
		t.Fatal(err)
	}
	got := runNonCanonicalProbe(t, g, "ProbeTable", "Root", "message", map[string][]byte{"wire": batch, "ann": tablewire.Announce(u)})
	want := hex.EncodeToString(batch)
	if got["message"] != "read" || got["save"] != want || got["other"] != "read" || got["otherback"] != "read" || got["othertwice"] != "same" {
		t.Errorf("a None element of an array of unions: message=%q save=%q (want read, %s), other=%q otherback=%q othertwice=%q (want read, read, same)\nprobe:\n%s", got["message"], got["save"], want, got["other"], got["otherback"], got["othertwice"], got["probe"])
	}
}

// testMooUnknownUnionFieldNoneRefused holds §3.3 (c) on the skip path: a
// reader whose `One` does not declare `pick` meets it as a newer writer's
// field under another build and skips it, and the skip refuses it written
// with arm reference 0, as the Go engine's does. The canonical batch, which
// elides the None union, reads.
func testMooUnknownUnionFieldNoneRefused(t *testing.T) {
	t.Parallel()
	u := unitFromSource(t, "package messageone\n\nfixed table One\n{\n    mode  uint8 = 7\n    count uint8\n}\n")
	files, err := generate(u)
	if err != nil {
		t.Fatal(err)
	}
	g := writeUnit(t, u, files)
	ann := readRoot(t, filepath.Join("testdata", "wire", "message-one-encoding", "announcement.bin"))
	for file, want := range map[string]string{"canonical.bin": "read", "union-none.bin": "refused"} {
		wire := readRoot(t, filepath.Join("testdata", "wire", "message-one-encoding", file))
		got := runNonCanonicalProbe(t, g, "ProbeTable", "One", "message", map[string][]byte{"wire": wire, "ann": ann})
		if got["message"] != want {
			t.Errorf("%s under a reader without pick: message = %q, want %q\nprobe:\n%s", file, got["message"], want, got["probe"])
		}
	}
}

// testMooUndeclaredFieldAtOwnBuildRefused holds §3.3 (b)'s "a field the
// record does not declare" under the reader's own build version: the reader
// whose `One` does not declare `pick` is handed a batch carrying `pick` with
// a set arm, announced at the reader's own build version, and refuses it; the
// same batch under the writer's build version reads, `pick` skipped as a newer
// writer's field, and the batch without `pick` reads under both.
func testMooUndeclaredFieldAtOwnBuildRefused(t *testing.T) {
	t.Parallel()
	writer := generateNonCanonicalUnit(t, "test/message-one-encoding/One.schema")
	m := tabletext.NewModel(writer.unit)
	value := m.New(m.Lookup("One"))
	var report tabletext.Report
	if !m.Read(value, []byte(`{"count": 5, "pick": {"plain": 3}}`), &report) {
		t.Fatalf("value: %+v", report)
	}
	withPick, err := tablewire.EncodeMessage(m, value)
	if err != nil {
		t.Fatal(err)
	}
	u := unitFromSource(t, "package messageone\n\nfixed table One\n{\n    mode  uint8 = 7\n    count uint8\n}\n")
	files, err := generate(u)
	if err != nil {
		t.Fatal(err)
	}
	g := writeUnit(t, u, files)
	const probe = `--!strict
local inputs = require("./inputs")
local MS = require("./table_message")
%slocal own = MS.NewVocabulary()
if not MS.AnnounceRead(own, inputs.own, {}) then print("own=refused") return end
for _, at in { "writer", "own" } do
	local v = MS.NewVocabulary()
	if not MS.AnnounceRead(v, inputs.ann, {}) then print("announce=refused") return end
	if at == "own" then
		v.buildVersion = own.buildVersion
	end
	local n = MS.LoadMessages(v, "One", inputs.wire, 4, {})
	print(at .. "=" .. (if n > 0 then "read" else "refused"))
end
`
	ann := readRoot(t, filepath.Join("testdata", "wire", "message-one-encoding", "announcement.bin"))
	for name, c := range map[string]struct {
		wire []byte
		own  string
	}{
		"pick":    {withPick, "refused"},
		"no pick": {readRoot(t, filepath.Join("testdata", "wire", "message-one-encoding", "canonical.bin")), "read"},
	} {
		out := g.run(t, fmt.Sprintf(probe, g.requireAll()), map[string][]byte{"wire": c.wire, "ann": ann, "own": tablewire.Announce(u)})
		if want := "writer=read\nown=" + c.own; out != want {
			t.Errorf("%s: moo answered %q, want %q", name, out, want)
		}
	}
}
