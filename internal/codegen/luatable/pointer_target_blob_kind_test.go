// TestLuaPointerTargetBlobKindRefused holds docs/SPEC-TABLES.md §3.1 and §6.5
// (docs/rulings.json blob-target-outside-closure-refused) on the Lua leg: the
// two reserved blob ids are types every reader of the variable form can name,
// whatever its root's pointer closure reaches. A blob record named from a
// pointer slot this reader DECLARES whose target is not that blob kind refuses
// the whole read (wire_damaged); a blob record of a kind the root's closure
// lacks, reached only from a field this reader does not declare, is skipped
// and counted unknown (evolution: the field once, the record once); a blob
// record no pointer reaches is damage.
//
// Every row of testdata/wire/pointer-target-type/blobkind/MANIFEST.tsv, and
// every row of testdata/conformance/feature-witnesses/pointer-targets, runs
// through every public read of the variable form (the plain load, the strict
// read, the retaining load) with the Go engine (tablewire.Decode,
// tablewire.DecodeRetain) as the oracle: the verdict, a refusal's reason and a
// read's unknown count are compared. The walk Load wraps is held to the
// unknown count of every read row. The same gate holds the leg's save and
// retaining save to never emitting a blob record no pointer reaches.
package luatable

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v3/internal/codegen/lua/luarun"
	"github.com/mas-bandwidth/schema/v3/internal/slowtest"
	"github.com/mas-bandwidth/schema/v3/internal/tabletext"
	"github.com/mas-bandwidth/schema/v3/internal/tablewire"
)

// blobKindRow is one wire read at one root: a manifest vector, or a witness
// row's value saved by the engine at the writer root.
type blobKindRow struct {
	name, root, field string
	wire              []byte
	verdict           string // "read" or "refused"
	unknown           int    // -1 where the row pins none
}

// blobKindVerdict is one read's answer: the verdict, a refusal's reason name,
// a read's unknown count and whether the row's pointer slot reads null.
type blobKindVerdict struct {
	verdict, reason string
	unknown         int
	ptr             string
}

// blobKindEngine answers the engine's strict and retaining reads of a row.
func blobKindEngine(m *tabletext.Model, row blobKindRow) map[string]blobKindVerdict {
	answers := map[string]blobKindVerdict{}
	reads := map[string]func(*tabletext.Instance, *tabletext.Report) (bool, error){
		"strict": func(inst *tabletext.Instance, r *tabletext.Report) (bool, error) {
			return tablewire.Decode(m, inst, row.wire, r)
		},
		"retain": func(inst *tabletext.Instance, r *tabletext.Report) (bool, error) {
			retain := tablewire.Retain{Capacity: 8192, IdCapacity: 1024}
			return tablewire.DecodeRetain(m, inst, row.wire, &retain, r)
		},
	}
	for name, read := range reads {
		var report tabletext.Report
		ok, err := read(m.New(m.Lookup(row.root)), &report)
		if ok && err == nil && !report.Refused {
			answers[name] = blobKindVerdict{verdict: "read", unknown: report.Unknown}
			continue
		}
		answers[name] = blobKindVerdict{verdict: "refused", reason: tablewire.RefusalReason(err)}
	}
	return answers
}

const luaBlobKindProbe = `local inputs = require("inputs")
local T = require(inputs.module)
local load = require("load")
local refusal = require("refusal")
local retain = require("retain")

local function fresh()
  return { damaged = false, refused = 0, unknown = 0, kindChanged = 0,
    outOfRange = 0, widened = 0, repeatedKey = 0, sharedBlob = 0,
    missingRequired = 0, wireDamaged = false, reason = "",
    retained = 0, retainLost = 0 }
end
local function ptr(v, field)
  if v == nil or field == "" then return "-" end
  return v[field] == nil and "null" or "set"
end

for i = 1, tonumber(inputs.n) do
  local root, wire, field = inputs["root" .. i], inputs["wire" .. i], inputs["field" .. i]
  local wr = fresh()
  local wv = load.loadWalk(T, root, wire, wr)
  local lr = fresh()
  local lv = load.Load(T, root, wire, lr)
  local sok, sv = pcall(refusal.ReadStrict, T, root, wire)
  local sreason = ""
  if not sok then sreason = tostring(sv):match("^[%a_]+") or tostring(sv) end
  if sok and sv == nil then sreason = "nil" end
  local rr = fresh()
  local rv = retain.LoadRetain(T, root, wire, { list = {}, used = 0 }, rr)
  print(table.concat({ i,
    tostring(wv ~= nil), wr.unknown,
    tostring(lv ~= nil), lr.unknown, tostring(lr.reason), ptr(lv, field),
    tostring(sok and sv ~= nil), sreason, ptr(sok and sv or nil, field),
    tostring(rv ~= nil), rr.unknown, tostring(rr.reason), ptr(rv, field) }, "|"))
end
`

// luaBlobKindAnswers runs the probe over rows and answers, per row, the
// walk's unknown count (-1 where the walk refused) and each public read's
// answer by entry name.
func luaBlobKindAnswers(t *testing.T, files map[string][]byte, module string, rows []blobKindRow) ([]int, []map[string]blobKindVerdict) {
	t.Helper()
	inputs := map[string][]byte{"module": []byte(module), "n": []byte(strconv.Itoa(len(rows)))}
	for i, row := range rows {
		k := strconv.Itoa(i + 1)
		inputs["root"+k], inputs["wire"+k], inputs["field"+k] = []byte(row.root), row.wire, []byte(row.field)
	}
	out, stderr, err := try(t, files, luaBlobKindProbe, inputs)
	if err != nil {
		t.Fatalf("probe: %v: %s", err, stderr)
	}
	lines := strings.Split(strings.TrimSpace(out), "\n")
	if len(lines) != len(rows) {
		t.Fatalf("probe printed %d lines for %d rows: %q", len(lines), len(rows), out)
	}
	walks := make([]int, len(rows))
	answers := make([]map[string]blobKindVerdict, len(rows))
	for i, line := range lines {
		f := strings.Split(line, "|")
		if len(f) != 14 || f[0] != strconv.Itoa(i+1) {
			t.Fatalf("probe line %q, want 14 fields for row %d", line, i+1)
		}
		name := rows[i].name
		walks[i] = -1
		if f[1] == "true" {
			walks[i] = atoi(t, name, "walk unknown", f[2])
		}
		answer := func(read bool, unknown, reason, ptr string) blobKindVerdict {
			if read {
				return blobKindVerdict{verdict: "read", unknown: atoi(t, name, "unknown", unknown), ptr: ptr}
			}
			return blobKindVerdict{verdict: "refused", reason: reason}
		}
		answers[i] = map[string]blobKindVerdict{
			"load":   answer(f[3] == "true", f[4], f[5], f[6]),
			"strict": answer(f[7] == "true", "-1", f[8], f[9]),
			"retain": answer(f[10] == "true", f[11], f[12], f[13]),
		}
	}
	return walks, answers
}

// blobKindProblems names every Lua answer that is not the row's verdict, the
// engine's verdict, the engine's refusal reason or the pinned unknown count.
func blobKindProblems(rows []blobKindRow, engine []map[string]blobKindVerdict, walks []int, lua []map[string]blobKindVerdict) []string {
	var problems []string
	for i, row := range rows {
		oracle := engine[i]["strict"]
		if walks[i] >= 0 && row.verdict == "read" && row.unknown >= 0 && walks[i] != row.unknown {
			problems = append(problems, fmt.Sprintf("%s (walk): unknown %d, want %d", row.name, walks[i], row.unknown))
		}
		for _, entry := range []string{"load", "strict", "retain"} {
			a := lua[i][entry]
			if a.verdict != row.verdict {
				problems = append(problems, fmt.Sprintf("%s (%s): the read %s, the row says %s", row.name, entry, a.verdict, row.verdict))
				continue
			}
			if a.verdict == "refused" {
				if a.reason != oracle.reason {
					problems = append(problems, fmt.Sprintf("%s (%s): refused %s, the engine refuses %s", row.name, entry, a.reason, oracle.reason))
				}
				continue
			}
			if a.unknown >= 0 && row.unknown >= 0 && a.unknown != row.unknown {
				problems = append(problems, fmt.Sprintf("%s (%s): unknown %d, want %d", row.name, entry, a.unknown, row.unknown))
			}
			if row.name == "text-unknown-field-bytes" && a.ptr != "null" {
				problems = append(problems, fmt.Sprintf("%s (%s): the declared slot reads %s, want null", row.name, entry, a.ptr))
			}
		}
	}
	return problems
}

// blobKindManifestRows reads the blob-kind MANIFEST generically.
func blobKindManifestRows(t *testing.T, dir string) (unit string, rows []blobKindRow) {
	t.Helper()
	manifest, err := os.ReadFile(filepath.Join(dir, "MANIFEST.tsv"))
	if err != nil {
		t.Fatalf("vectors manifest: %v", err)
	}
	for _, r := range luaPointerTargetRows(t, string(manifest)) {
		if unit == "" {
			unit = r.unit
		}
		if r.unit != unit {
			t.Fatalf("%s reads %s; this probe reads %s", r.name, r.unit, unit)
		}
		wire, err := os.ReadFile(filepath.Join(dir, r.file))
		if err != nil {
			t.Fatalf("%s: %v", r.name, err)
		}
		field := "body"
		if r.root == "Bin" {
			field = "data"
		}
		rows = append(rows, blobKindRow{name: r.name, root: r.root, field: field, wire: wire, verdict: r.verdict, unknown: r.unknown})
	}
	if len(rows) == 0 {
		t.Fatal("MANIFEST.tsv names no vectors")
	}
	return unit, rows
}

// blobKindWitnessRows saves each witness row's input with the engine at the
// writer root and reads it at the reader root: kind_mismatch above zero is a
// refusal, otherwise the row reads with its unknown count.
func blobKindWitnessRows(t *testing.T, m *tabletext.Model, dir string) []blobKindRow {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(dir, "cases.json"))
	if err != nil {
		t.Fatal(err)
	}
	var cases []struct {
		Name, Writer, Reader string
		Input                json.RawMessage
		KindMismatch         int `json:"kind_mismatch"`
		Unknown              int
	}
	if err := json.Unmarshal(data, &cases); err != nil {
		t.Fatal(err)
	}
	var rows []blobKindRow
	for _, c := range cases {
		v := m.New(m.Lookup(c.Writer))
		var r tabletext.Report
		if !m.Read(v, c.Input, &r) || !r.Silent() {
			t.Fatalf("%s: the writer's input does not read: %+v", c.Name, r)
		}
		wire, err := tablewire.Encode(m, v)
		if err != nil {
			t.Fatalf("%s: Encode: %v", c.Name, err)
		}
		row := blobKindRow{name: "witness " + c.Name, root: c.Reader, wire: wire, verdict: "read", unknown: c.Unknown}
		if c.KindMismatch > 0 {
			row.verdict, row.unknown = "refused", -1
		}
		rows = append(rows, row)
	}
	return rows
}

// plantRootClosureRule puts the old rule back in a copy of the generated
// reader: a blob record of a kind the root's closure lacks is one the reader
// cannot name, so it reads as an unknown record and every pointer naming it
// reads null, a declared wrong-kind slot included.
func plantRootClosureRule(t *testing.T, files map[string][]byte) map[string][]byte {
	t.Helper()
	const anchor = "      if rec.tid == STRING_BLOB_ID then\n        local body"
	const planted = "      if outside then\n        local e = node_entry(state, ctx)\n        e.kind, e.blob, e.box, e.codec, e.value, e.at, e.lim = \"unknown\", nil, nil, nil, nil, nil, nil\n        ctx.nodes[#ctx.nodes + 1] = e\n      elseif rec.tid == STRING_BLOB_ID then\n        local body"
	src := string(files["load.lua"])
	if !strings.Contains(src, "if outside then report.unknown = report.unknown + 1 end") || strings.Count(src, anchor) != 1 {
		t.Fatal("the closure anchor is not in the generated load.lua exactly once")
	}
	out := maps.Clone(files)
	out["load.lua"] = []byte(strings.Replace(src, anchor, planted, 1))
	return out
}

func TestLuaPointerTargetBlobKindRefused(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "Lua 5.4 functional blob-kind pointer-target probe")

	repo, err := filepath.Abs(filepath.Join("..", "..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	vectors := filepath.Join(repo, "testdata", "wire", "pointer-target-type", "blobkind")
	unit, rows := blobKindManifestRows(t, vectors)
	u := luarun.LoadUnit(t, filepath.Join(repo, unit))
	m := tabletext.NewModel(u)
	files, err := Generate(u)
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	engine := make([]map[string]blobKindVerdict, len(rows))
	for i, row := range rows {
		engine[i] = blobKindEngine(m, row)
	}

	t.Run("every vector answers the engine's verdict", func(t *testing.T) {
		for i, row := range rows {
			for name, a := range engine[i] {
				if a.verdict != row.verdict || (a.verdict == "read" && row.unknown >= 0 && a.unknown != row.unknown) {
					t.Fatalf("%s: the engine's %s read answers %+v; the manifest says %s unknown %d", row.name, name, a, row.verdict, row.unknown)
				}
			}
		}
		walks, lua := luaBlobKindAnswers(t, files, ModuleName(u), rows)
		for i, row := range rows {
			t.Logf("%s: walk unknown %d; load %+v; strict %+v; retain %+v", row.name, walks[i], lua[i]["load"], lua[i]["strict"], lua[i]["retain"])
		}
		for _, p := range blobKindProblems(rows, engine, walks, lua) {
			t.Error(p)
		}
	})

	t.Run("negative control: the root-closure rule reads text-names-bytes", func(t *testing.T) {
		walks, lua := luaBlobKindAnswers(t, plantRootClosureRule(t, files), ModuleName(u), rows)
		named := false
		for _, p := range blobKindProblems(rows, engine, walks, lua) {
			if strings.HasPrefix(p, "text-names-bytes ") {
				named = true
			}
		}
		if !named {
			t.Error("the planted root-closure rule did not go red on text-names-bytes")
		}
	})

	t.Run("the pointer-target witness rows answer the engine's verdict", func(t *testing.T) {
		dir := filepath.Join(repo, "testdata", "conformance", "feature-witnesses", "pointer-targets")
		wu := luarun.LoadUnit(t, dir)
		wm := tabletext.NewModel(wu)
		wrows := blobKindWitnessRows(t, wm, dir)
		wfiles, err := Generate(wu)
		if err != nil {
			t.Fatalf("generate: %v", err)
		}
		wengine := make([]map[string]blobKindVerdict, len(wrows))
		for i, row := range wrows {
			wengine[i] = blobKindEngine(wm, row)
			for name, a := range wengine[i] {
				if a.verdict != row.verdict || (a.verdict == "read" && a.unknown != row.unknown) {
					t.Fatalf("%s: the engine's %s read answers %+v; cases.json says %s unknown %d", row.name, name, a, row.verdict, row.unknown)
				}
			}
		}
		walks, lua := luaBlobKindAnswers(t, wfiles, ModuleName(wu), wrows)
		for i, row := range wrows {
			t.Logf("%s: load %+v; strict %+v; retain %+v", row.name, lua[i]["load"], lua[i]["strict"], lua[i]["retain"])
		}
		for _, p := range blobKindProblems(wrows, wengine, walks, lua) {
			t.Error(p)
		}
	})

	// A CONFORMING WRITER NEVER EMITS A BLOB RECORD NO POINTER REACHES: the
	// leg's save and retaining save of each reader root, the blob null, empty
	// and set, decode with the engine accepted and nothing unknown.
	t.Run("the writer emits no unreachable blob record", func(t *testing.T) {
		const probe = `local T = require(require("inputs").module)
local save = require("save")
local retain = require("retain")
local hexline = require("hexline")
for _, c in ipairs({ { "Text", "body" }, { "Bin", "data" } }) do
  for _, blob in ipairs({ false, "", "hello" }) do
    local v = T["New" .. c[1]]()
    if blob then v[c[2]] = { value = blob } end
    hexline(assert(save.Save(T, c[1], v)))
    local report = { retained = 0, retainLost = 0 }
    hexline(assert(retain.SaveRetain(T, c[1], v, nil, report)))
  end
end
`
		dir := t.TempDir()
		luarun.WriteFiles(t, dir, files)
		luarun.WriteHexProbe(t, dir)
		if err := os.WriteFile(filepath.Join(dir, "main.lua"), []byte(probe), 0o644); err != nil {
			t.Fatal(err)
		}
		wires := luarun.HexLines(t, luarun.Run(t, dir, "main.lua", map[string][]byte{"module": []byte(ModuleName(u))}))
		if len(wires) != 12 {
			t.Fatalf("the save probe printed %d wires, want 12", len(wires))
		}
		// each root's saves come null, empty, set, each as (save, retaining
		// save): the three blobs must really reach the wire, so no two of one
		// save path's three wires are equal
		for base := 0; base < 12; base += 6 {
			for path := range 2 {
				null, empty, set := wires[base+path], wires[base+2+path], wires[base+4+path]
				if bytes.Equal(null, empty) || bytes.Equal(null, set) || bytes.Equal(empty, set) {
					t.Errorf("save %d path %d: the null, empty and set saves are not three wires: %x %x %x", base/6, path, null, empty, set)
				}
			}
		}
		for i, wire := range wires {
			root := "Text"
			if i >= 6 {
				root = "Bin"
			}
			row := blobKindRow{name: fmt.Sprintf("%s save %d", root, i%6), root: root, wire: wire}
			for name, a := range blobKindEngine(m, row) {
				if a.verdict != "read" || a.unknown != 0 {
					t.Errorf("%s (%s): the engine's %s read answers %+v, want read with unknown 0", row.name, hex.EncodeToString(wire), name, a)
				}
			}
		}
	})
}
