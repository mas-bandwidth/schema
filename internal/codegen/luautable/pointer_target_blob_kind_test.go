package luautable_test

// BLOB-KIND POINTER TARGETS, LUAU (docs/SPEC-TABLES.md §3.1, §6.5;
// docs/rulings.json blob-target-outside-closure-refused): the two reserved
// blob ids are types every reader of the variable form can name, whatever its
// root's pointer closure reaches. A blob record named from a pointer slot this
// reader declares whose target is not that blob kind refuses the whole read;
// a blob record of a kind the root's closure lacks, reached only from a field
// this reader does not declare, is skipped and counted unknown (evolution);
// and a blob record no pointer reaches is damage.
//
// Every row of testdata/wire/pointer-target-type/blobkind/MANIFEST.tsv runs
// through the plain (strict) and the retaining read, generating each row's
// unit into t.TempDir() and feeding the vector files to the leg's own reader;
// the rows that read also run through the recovering reads (loadWalk and
// loadRetainWalk), which must agree on the unknown count. Rows 8, 9 and 10 of
// testdata/conformance/feature-witnesses/pointer-targets/cases.json (a
// declared table or blob kind outside the root's closure, named from a
// declared slot) run through the same four reads: the strict and retaining
// reads refuse, and the recovering reads answer the row's pinned kind_mismatch
// and unknown counts with the slot null. Only the verdict and the pinned
// counters are compared; a refusal's reason is logged, never compared
// (docs/rulings.json refusal-verdict-only). The Go engine is the oracle: each
// wire is checked against tablewire.Decode first.
//
// The same gate holds the leg's save and retaining save to never emitting a
// blob record no pointer reaches: the blob null, empty and set, each saved
// wire decoded with the engine and accepted with nothing unknown, the blob
// read back as written, and the three saves of one root pairwise different.

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/compiler"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/luau/luaurun"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
)

type blobKindCase struct {
	name, unit, root, file, verdict string
	unknown                         int // -1 when the row does not pin one
}

func blobKindCases(t *testing.T) []blobKindCase {
	t.Helper()
	path := filepath.Join(root, "testdata", "wire", "pointer-target-type", "blobkind", "MANIFEST.tsv")
	mf, err := os.Open(path)
	if err != nil {
		t.Fatalf("blob-kind manifest: %v", err)
	}
	defer mf.Close()
	var rows []blobKindCase
	sc := bufio.NewScanner(mf)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		f := strings.Split(line, "\t")
		if len(f) != 6 {
			t.Fatalf("%s: %d fields, want 6", path, len(f))
		}
		row := blobKindCase{name: f[0], unit: f[1], root: f[2], file: f[3], verdict: f[4], unknown: -1}
		if row.verdict != "read" && row.verdict != "refused" {
			t.Fatalf("%s: verdict %q, want read or refused", path, row.verdict)
		}
		if f[5] != "-" {
			n, err := strconv.Atoi(f[5])
			if err != nil {
				t.Fatalf("%s: unknown %q: %v", path, f[5], err)
			}
			row.unknown = n
		}
		rows = append(rows, row)
	}
	if err := sc.Err(); err != nil {
		t.Fatal(err)
	}
	if len(rows) == 0 {
		t.Fatalf("%s names no vectors", path)
	}
	return rows
}

// blobKindRead is one wire handed to the Luau reads: its name, the root it is
// read at and the schema name of the one pointer field whose slot the probe
// reports null or present.
type blobKindRead struct {
	name, root, field string
	wire              []byte
}

// blobKindAnswer is one read entry's answer for one wire.
type blobKindAnswer struct {
	verdict      string
	unknown      int
	kindMismatch int
	null         bool
	reason       string
}

// The four Luau read entries: the strict read by its plain name, the
// retaining read, and the two recovering reads reached by their explicit
// names (docs/SPEC-TABLES.md §6.5, §6.6).
var blobKindStrictEntries = []string{"plain", "retain"}
var blobKindWalkEntries = []string{"walk", "retainwalk"}

// blobKindProbeMain is the Luau probe: it reads every wire through the four
// read entries, printing the verdict and, for a read, the unknown and
// kind_mismatch counts and whether the field's slot stayed null; for a
// refusal, the reason.
func blobKindProbeMain(g *generatedUnit, reads []blobKindRead) string {
	var main strings.Builder
	main.WriteString(blobKindRequires(g, "local L = require(\"./schema_load\")\nlocal R = require(\"./schema_retain\")\nlocal St = require(\"./schema_tables\")\nlocal inputs = require(\"./inputs\")\n"))
	main.WriteString(`local function slotOf(root: string, field: string): string
	for _, fd in St.lookup(root).fields do
		if fd.name == field then return fd.slot end
	end
	error(root .. " declares no field " .. field)
end
local function say(name: string, entry: string, v: any, rep: any, slot: string)
	if v ~= nil then
		print(name .. " " .. entry .. " read " .. tostring(rep.unknown) .. " " .. tostring(rep.kindMismatch) .. " " .. (if v[slot] == nil then "null" else "present"))
	else
		print(name .. " " .. entry .. " refused " .. tostring(rep.reason))
	end
end
local function answer(name: string, bytes: buffer, root: string, field: string)
	local slot = slotOf(root, field)
	local rep: any = {}
	say(name, "plain", L.load(root, bytes, rep), rep, slot)
	local rr: any = {}
	say(name, "retain", (R.LoadRetain(root, bytes, rr, 65536)), rr, slot)
	local wr: any = {}
	L.resetReport(wr)
	say(name, "walk", L.loadWalk(root, bytes, wr), wr, slot)
	local rw: any = {}
	L.resetReport(rw)
	say(name, "retainwalk", (R.loadRetainWalk(root, bytes, rw, 65536)), rw, slot)
end
`)
	for _, r := range reads {
		fmt.Fprintf(&main, "answer(%q, inputs[%q], %q, %q)\n", r.name, r.name, r.root, r.field)
	}
	return main.String()
}

// blobKindRequires opens a probe with its header and a require of every
// emitted table module, so each descriptor is registered.
func blobKindRequires(g *generatedUnit, header string) string {
	var mods []string
	for name := range g.files {
		if strings.HasSuffix(name, "Table.luau") {
			mods = append(mods, strings.TrimSuffix(name, ".luau"))
		}
	}
	sort.Strings(mods)
	var b strings.Builder
	b.WriteString(header)
	for _, mod := range mods {
		fmt.Fprintf(&b, "require(%q)\n", "./"+mod)
	}
	return b.String()
}

// blobKindField names the one pointer field each blob-kind reader root
// declares, by its schema name; the probe looks up the slot the runtime
// stores it under.
func blobKindField(rootName string) string {
	if rootName == "Bin" {
		return "data"
	}
	return "body"
}

func blobKindProbe(t *testing.T, g *generatedUnit, reads []blobKindRead) map[string]blobKindAnswer {
	t.Helper()
	if err := os.WriteFile(filepath.Join(g.dir, "main.luau"), []byte(blobKindProbeMain(g, reads)), 0o644); err != nil {
		t.Fatal(err)
	}
	inputs := map[string][]byte{}
	for _, r := range reads {
		inputs[r.name] = r.wire
	}
	out := luaurun.Run(t, g.dir, "main.luau", inputs)

	got := map[string]blobKindAnswer{}
	for line := range strings.SplitSeq(strings.TrimRight(string(out), "\n"), "\n") {
		f := strings.Fields(line)
		switch {
		case len(f) == 4 && f[2] == "refused":
			got[f[0]+" "+f[1]] = blobKindAnswer{verdict: "refused", reason: f[3]}
		case len(f) == 6 && f[2] == "read":
			unknown, err := strconv.Atoi(f[3])
			if err != nil {
				t.Fatalf("%s: %q is not a count", line, f[3])
			}
			km, err := strconv.Atoi(f[4])
			if err != nil {
				t.Fatalf("%s: %q is not a count", line, f[4])
			}
			got[f[0]+" "+f[1]] = blobKindAnswer{verdict: "read", unknown: unknown, kindMismatch: km, null: f[5] == "null"}
		default:
			t.Fatalf("the probe printed %q", line)
		}
	}
	return got
}

// blobKindReads loads each manifest row's vector file.
func blobKindReads(t *testing.T, rows []blobKindCase) []blobKindRead {
	t.Helper()
	dir := filepath.Join(root, "testdata", "wire", "pointer-target-type", "blobkind")
	var reads []blobKindRead
	for _, row := range rows {
		wire, err := os.ReadFile(filepath.Join(dir, row.file))
		if err != nil {
			t.Fatalf("%s: %v", row.file, err)
		}
		reads = append(reads, blobKindRead{name: row.name, root: row.root, field: blobKindField(row.root), wire: wire})
	}
	return reads
}

// blobKindProblems names every answer that is not the row's verdict. The
// strict and retaining reads answer the manifest's verdict; on a row that
// reads, the recovering reads also read, with the pinned unknown count and no
// kind_mismatch. The one pointer is present on text-canonical and null on
// text-unknown-field-bytes.
func blobKindProblems(rows []blobKindCase, got map[string]blobKindAnswer) []string {
	var problems []string
	for _, row := range rows {
		entries := blobKindStrictEntries
		if row.verdict == "read" {
			entries = append(append([]string(nil), blobKindStrictEntries...), blobKindWalkEntries...)
		}
		for _, entry := range entries {
			answer, ok := got[row.name+" "+entry]
			if !ok {
				problems = append(problems, fmt.Sprintf("%s %s: the probe printed no answer", row.name, entry))
				continue
			}
			if answer.verdict != row.verdict {
				problems = append(problems, fmt.Sprintf("%s %s: verdict %q, the manifest says %q", row.name, entry, answer.verdict, row.verdict))
				continue
			}
			if row.verdict != "read" {
				continue
			}
			if row.unknown >= 0 && answer.unknown != row.unknown {
				problems = append(problems, fmt.Sprintf("%s %s: unknown %d, the manifest says %d", row.name, entry, answer.unknown, row.unknown))
			}
			if answer.kindMismatch != 0 {
				problems = append(problems, fmt.Sprintf("%s %s: kind_mismatch %d on a row that reads", row.name, entry, answer.kindMismatch))
			}
			switch row.name {
			case "text-canonical":
				if answer.null {
					problems = append(problems, fmt.Sprintf("%s %s: the declared pointer read null", row.name, entry))
				}
			case "text-unknown-field-bytes":
				// A newer writer's field names the record: the declared slot
				// stays null (docs/SPEC-TABLES.md §3.1).
				if !answer.null {
					problems = append(problems, fmt.Sprintf("%s %s: the declared pointer is not null", row.name, entry))
				}
			}
		}
	}
	return problems
}

// blobKindWitness is one row of the pointer-target feature witnesses.
type blobKindWitness struct {
	Name, Writer, Reader string
	Input, Expected      json.RawMessage
	KindMismatch         int `json:"kind_mismatch"`
	Unknown              int
}

// blobKindWitnessDir holds the pointer-target feature witnesses; rows 8, 9
// and 10 (one-based) are the cases this ruling flipped to refusals.
var blobKindWitnessDir = filepath.Join("testdata", "conformance", "feature-witnesses", "pointer-targets")

// blobKindWitnessReads builds the wires of witness rows 8, 9 and 10 with the
// engine (the writer root saves the row's input) and checks the engine
// refuses each at the reader root. It returns each row and its wire.
func blobKindWitnessReads(t *testing.T) ([]blobKindWitness, []blobKindRead) {
	t.Helper()
	dir := filepath.Join(root, blobKindWitnessDir)
	m := blobKindModel(t, blobKindWitnessDir)
	data, err := os.ReadFile(filepath.Join(dir, "cases.json"))
	if err != nil {
		t.Fatal(err)
	}
	var all []blobKindWitness
	if err := json.Unmarshal(data, &all); err != nil {
		t.Fatal(err)
	}
	if len(all) < 10 {
		t.Fatalf("cases.json holds %d rows, want at least 10", len(all))
	}
	rows := all[7:10]
	var reads []blobKindRead
	for _, row := range rows {
		if row.KindMismatch == 0 {
			t.Fatalf("%s: the witness row does not refuse", row.Name)
		}
		var expected map[string]any
		if err := json.Unmarshal(row.Expected, &expected); err != nil || len(expected) != 1 {
			t.Fatalf("%s: the expected value names %d fields, want 1 (%v)", row.Name, len(expected), err)
		}
		var field string
		for k := range expected {
			field = k
		}
		v := m.New(m.Lookup(row.Writer))
		var r tabletext.Report
		if !m.Read(v, row.Input, &r) || !r.Silent() {
			t.Fatalf("%s: the writer's input does not read: %+v", row.Name, r)
		}
		wire, err := tablewire.Encode(m, v)
		if err != nil {
			t.Fatalf("%s: Encode: %v", row.Name, err)
		}
		var report tabletext.Report
		ok, err := tablewire.Decode(m, m.New(m.Lookup(row.Reader)), wire, &report)
		if ok || !tablewire.ReadRefusal(err) {
			t.Fatalf("%s: the engine does not refuse the witness wire: %v %+v", row.Name, err, report)
		}
		reads = append(reads, blobKindRead{name: strings.ReplaceAll(row.Name, " ", "-"), root: row.Reader, field: field, wire: wire})
	}
	return rows, reads
}

// blobKindWitnessProblems names every answer that is not the witness row's:
// the strict and retaining reads refuse; the recovering reads answer the
// row's kind_mismatch and unknown counts with the slot null.
func blobKindWitnessProblems(rows []blobKindWitness, got map[string]blobKindAnswer) []string {
	var problems []string
	for _, row := range rows {
		name := strings.ReplaceAll(row.Name, " ", "-")
		for _, entry := range blobKindStrictEntries {
			if a, ok := got[name+" "+entry]; !ok || a.verdict != "refused" {
				problems = append(problems, fmt.Sprintf("%s %s: %+v, want refused", name, entry, a))
			}
		}
		for _, entry := range blobKindWalkEntries {
			a, ok := got[name+" "+entry]
			if !ok || a.verdict != "read" || a.kindMismatch != row.KindMismatch || a.unknown != row.Unknown || !a.null {
				problems = append(problems, fmt.Sprintf("%s %s: %+v, want kind_mismatch %d unknown %d and the slot null", name, entry, a, row.KindMismatch, row.Unknown))
			}
		}
	}
	return problems
}

func TestLuauPointerTargetBlobKindRefused(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "the pinned Luau interpreter (it runs the emitted readers)")

	rows := blobKindCases(t)
	byUnit := map[string][]blobKindCase{}
	for _, row := range rows {
		byUnit[row.unit] = append(byUnit[row.unit], row)
	}

	for unit, unitRows := range byUnit {
		g := generateFiles(t, blobKindSources(t, unit))
		reads := blobKindReads(t, unitRows)

		t.Run(unit+" every row answers its verdict", func(t *testing.T) {
			got := blobKindProbe(t, g, reads)
			blobKindLog(t, got)
			for _, p := range blobKindProblems(unitRows, got) {
				t.Error(p)
			}
		})

		t.Run(unit+" negative control: the root-closure rule accepts text-names-bytes", func(t *testing.T) {
			planted := copyGeneratedUnit(t, g)
			plantLuau(t, planted, "local declared = S.tableId(tid) or tid == StringBlobId or tid == BytesBlobId", "local declared = S.tableId(tid)")
			problems := blobKindProblems(unitRows, blobKindProbe(t, planted, reads))
			named := false
			for _, p := range problems {
				if strings.Contains(p, "text-names-bytes") {
					named = true
				}
			}
			if !named {
				t.Errorf("the planted old rule did not go red by name on text-names-bytes: %v", problems)
			} else {
				t.Logf("the planted old rule went red by name: %v", problems)
			}
		})

		if unit != "test/pointer-target-type/blobkind/reader" {
			continue
		}
		t.Run("the writer emits no unreachable blob record", func(t *testing.T) {
			m := blobKindModel(t, unit)
			saves := blobKindSaves(t, g)
			for name, wire := range saves {
				rootName, rest, _ := strings.Cut(name, ".")
				dest := m.New(m.Lookup(rootName))
				var report tabletext.Report
				ok, err := tablewire.Decode(m, dest, wire, &report)
				if !ok || err != nil || report.Refused || report.Unknown != 0 {
					t.Errorf("%s: the leg's own save does not read back clean: %v %+v", name, err, report)
					continue
				}
				text, err := m.Write(dest)
				if err != nil {
					t.Fatalf("%s: Write: %v", name, err)
				}
				var got map[string]any
				if err := json.Unmarshal(text, &got); err != nil {
					t.Fatal(err)
				}
				want := blobKindWritten(rootName, strings.SplitN(rest, ".", 2)[0])
				if !reflect.DeepEqual(got[blobKindField(rootName)], want) {
					t.Errorf("%s: the engine reads back %s, want %s = %v", name, text, blobKindField(rootName), want)
				}
			}
			for _, rootName := range []string{"Text", "Bin"} {
				for _, path := range []string{"save", "retain-save"} {
					null, empty, set := saves[rootName+".null."+path], saves[rootName+".empty."+path], saves[rootName+".set."+path]
					t.Logf("%s %s: null %d bytes, empty %d bytes, set %d bytes", rootName, path, len(null), len(empty), len(set))
					if bytes.Equal(null, empty) || bytes.Equal(null, set) || bytes.Equal(empty, set) {
						t.Errorf("%s %s: the null, empty and set saves are not pairwise different: %x %x %x", rootName, path, null, empty, set)
					}
				}
			}
		})
	}

	t.Run("witness rows 8 to 10: a declared kind outside the closure, named from a declared slot", func(t *testing.T) {
		wrows, wreads := blobKindWitnessReads(t)
		g := generateFiles(t, blobKindSources(t, blobKindWitnessDir))
		got := blobKindProbe(t, g, wreads)
		blobKindLog(t, got)
		for _, p := range blobKindWitnessProblems(wrows, got) {
			t.Error(p)
		}

		t.Run("negative control: a declared placeholder also counted unknown", func(t *testing.T) {
			planted := copyGeneratedUnit(t, g)
			plantLuau(t, planted, "report.unknown = report.unknown - 1", "")
			problems := blobKindWitnessProblems(wrows, blobKindProbe(t, planted, wreads))
			if len(problems) == 0 {
				t.Error("the gate passed a recovering read that counts a declared placeholder unknown")
			} else {
				t.Logf("the planted double count went red: %v", problems)
			}
		})
	})
}

// blobKindLog logs every answer, the refusal reason included (logged, never
// compared: docs/rulings.json refusal-verdict-only).
func blobKindLog(t *testing.T, got map[string]blobKindAnswer) {
	t.Helper()
	keys := make([]string, 0, len(got))
	for k := range got {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		t.Logf("%s: %+v", k, got[k])
	}
}

// blobKindWritten is the engine's text of the blob the save probe wrote: a
// *string as itself, a *bytes as base64.
func blobKindWritten(rootName, state string) any {
	switch state {
	case "null":
		return nil
	case "empty":
		return ""
	}
	if rootName == "Bin" {
		return "aGVsbG8="
	}
	return "hello"
}

// blobKindSources reads a unit's schema files.
func blobKindSources(t *testing.T, unit string) map[string]string {
	t.Helper()
	schemas, err := filepath.Glob(filepath.Join(root, unit, "*.schema"))
	if err != nil {
		t.Fatalf("%s: %v", unit, err)
	}
	if len(schemas) == 0 {
		t.Fatalf("%s declares no schema", unit)
	}
	src := map[string]string{}
	for _, path := range schemas {
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		src[filepath.Base(path)] = string(data)
	}
	return src
}

// copyGeneratedUnit copies every emitted module into a fresh directory, so a
// planted rule never touches the reader the main gate runs.
func copyGeneratedUnit(t *testing.T, g *generatedUnit) *generatedUnit {
	t.Helper()
	cp := &generatedUnit{dir: t.TempDir(), files: map[string][]byte{}, loaded: true}
	for name, data := range g.files {
		if err := os.WriteFile(filepath.Join(cp.dir, name), data, 0o644); err != nil {
			t.Fatal(err)
		}
		cp.files[name] = data
	}
	return cp
}

// plantLuau replaces one line of a copy of the emitted schema_load.luau; the
// anchor must be there exactly once.
func plantLuau(t *testing.T, g *generatedUnit, anchor, planted string) {
	t.Helper()
	path := filepath.Join(g.dir, "schema_load.luau")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Count(string(data), anchor) != 1 {
		t.Fatalf("the anchor %q is not in the emitted schema_load.luau exactly once", anchor)
	}
	patched := strings.Replace(string(data), anchor, planted, 1)
	if err := os.WriteFile(path, []byte(patched), 0o644); err != nil {
		t.Fatal(err)
	}
}

// blobKindSaves runs the leg's own save and retaining save of Text and Bin,
// the blob null, empty and set, and returns each saved wire by
// <root>.<null|empty|set>.<save|retain-save>. The blob is stored under the
// slot the descriptor names for the field, the key the runtime reads.
func blobKindSaves(t *testing.T, g *generatedUnit) map[string][]byte {
	t.Helper()
	main := blobKindRequires(g, "local S = require(\"./schema_save\")\nlocal R = require(\"./schema_retain\")\nlocal St = require(\"./schema_tables\")\n")
	main += `local function hex(b: buffer): string
	local parts = table.create(buffer.len(b))
	for i = 0, buffer.len(b) - 1 do
		parts[i + 1] = string.format("%02x", buffer.readu8(b, i))
	end
	return table.concat(parts)
end
for _, c in { { "Text", "body" }, { "Bin", "data" } } do
	local d = St.lookup(c[1])
	local slot: string? = nil
	for _, fd in d.fields do
		if fd.name == c[2] then slot = fd.slot end
	end
	assert(slot ~= nil, c[1] .. " declares no field " .. c[2])
	for _, state in { "null", "empty", "set" } do
		local v = d.new()
		if state == "empty" then v[slot] = buffer.create(0) end
		if state == "set" then v[slot] = buffer.fromstring("hello") end
		local name = c[1] .. "." .. state
		local w, why = S.save(d, v)
		if w == nil then
			print(name .. "|save-refused|" .. tostring(why and why.name))
		else
			print(name .. "|save|" .. hex(w))
		end
		local rw, rwhy = R.SaveRetain(c[1], v, nil, {})
		if rw == nil then
			print(name .. "|retain-save-refused|" .. tostring(rwhy and rwhy.name))
		else
			print(name .. "|retain-save|" .. hex(rw))
		end
	end
end
`
	if err := os.WriteFile(filepath.Join(g.dir, "save.luau"), []byte(main), 0o644); err != nil {
		t.Fatal(err)
	}
	out := luaurun.Run(t, g.dir, "save.luau", nil)

	saves := map[string][]byte{}
	for line := range strings.SplitSeq(strings.TrimRight(string(out), "\n"), "\n") {
		f := strings.Split(line, "|")
		if len(f) != 3 {
			t.Fatalf("the save probe printed %q", line)
		}
		if strings.HasSuffix(f[1], "refused") {
			t.Errorf("%s: the leg's own %s: %s", f[0], strings.ReplaceAll(f[1], "-", " "), f[2])
			continue
		}
		wire, err := hexDecode(f[2])
		if err != nil {
			t.Fatalf("%s: %q is not hex: %v", line, f[2], err)
		}
		saves[f[0]+"."+f[1]] = wire
	}
	if len(saves) != 12 {
		t.Fatalf("the save probe answered %d saved wires, want 12", len(saves))
	}
	return saves
}

func hexDecode(s string) ([]byte, error) {
	if len(s)%2 != 0 {
		return nil, fmt.Errorf("odd length")
	}
	out := make([]byte, len(s)/2)
	for i := range out {
		n, err := strconv.ParseUint(s[i*2:i*2+2], 16, 8)
		if err != nil {
			return nil, err
		}
		out[i] = byte(n)
	}
	return out, nil
}

// blobKindModel compiles one unit into the engine's model.
func blobKindModel(t *testing.T, unit string) *tabletext.Model {
	t.Helper()
	paths, err := compiler.GatherPaths([]string{filepath.Join(root, unit)})
	if err != nil {
		t.Fatalf("%s: %v", unit, err)
	}
	u, err := compiler.New().Load(paths)
	if err != nil {
		t.Fatalf("%s: %v", unit, err)
	}
	return tabletext.NewModel(u)
}
