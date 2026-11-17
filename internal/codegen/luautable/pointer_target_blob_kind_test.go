package luautable_test

// BLOB-KIND POINTER TARGETS, LUAU (docs/SPEC-TABLES.md §3.1, §6.5;
// docs/rulings.json blob-target-outside-closure-refused): the two reserved
// blob ids are types every reader of the variable form can name, whatever its
// root's pointer closure reaches. A blob record named from a pointer slot this
// reader declares whose target is not that blob kind refuses the whole read
// (wire_damaged); a blob record of a kind the root's closure lacks, reached
// only from a field this reader does not declare, is skipped and counted
// unknown (evolution); and a blob record no pointer reaches is damage.
//
// Every row of testdata/wire/pointer-target-type/blobkind/MANIFEST.tsv runs
// through the plain (strict) and the retaining read, generating each row's
// unit into t.TempDir() and feeding the vector files to the leg's own reader.
// The same gate holds the leg's save and retaining save to never emitting a
// blob record no pointer reaches: each saved wire is decoded with the engine
// (tablewire.Decode) and must be accepted with nothing unknown.

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
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

// blobKindAnswer is one read entry's answer for one vector.
type blobKindAnswer struct {
	verdict string
	unknown int
	null    bool
}

// blobKindProbeMain is the Luau probe: it reads every row through the plain
// (strict) load and the retaining load, printing the verdict, the unknown
// count and whether the row's one pointer stayed null.
func blobKindProbeMain(g *generatedUnit, rows []blobKindCase) string {
	var mods []string
	for name := range g.files {
		if strings.HasSuffix(name, "Table.luau") {
			mods = append(mods, strings.TrimSuffix(name, ".luau"))
		}
	}
	sort.Strings(mods)

	var main strings.Builder
	main.WriteString("local L = require(\"./schema_load\")\nlocal R = require(\"./schema_retain\")\nlocal inputs = require(\"./inputs\")\n")
	for _, mod := range mods {
		fmt.Fprintf(&main, "require(%q)\n", "./"+mod)
	}
	main.WriteString("local function answer(name: string, bytes: buffer, root: string, field: string)\n")
	main.WriteString("\tlocal report: any = {}\n")
	main.WriteString("\tlocal v = L.load(root, bytes, report)\n")
	main.WriteString("\tif v ~= nil then\n")
	main.WriteString("\t\tprint(name .. \" plain read \" .. tostring(report.unknown) .. \" \" .. (if v[field] == nil then \"null\" else \"present\"))\n")
	main.WriteString("\telse\n")
	main.WriteString("\t\tprint(name .. \" plain refused\")\n")
	main.WriteString("\tend\n")
	main.WriteString("\tlocal rr: any = {}\n")
	main.WriteString("\tlocal rv = R.LoadRetain(root, bytes, rr, 65536)\n")
	main.WriteString("\tif rv ~= nil then\n")
	main.WriteString("\t\tprint(name .. \" retain read \" .. tostring(rr.unknown) .. \" \" .. (if rv[field] == nil then \"null\" else \"present\"))\n")
	main.WriteString("\telse\n")
	main.WriteString("\t\tprint(name .. \" retain refused\")\n")
	main.WriteString("\tend\n")
	main.WriteString("end\n")
	for _, row := range rows {
		fmt.Fprintf(&main, "answer(%q, inputs[%q], %q, %q)\n", row.name, row.name, row.root, blobKindField(row.root))
	}
	return main.String()
}

// blobKindField names the one pointer slot each reader root declares.
func blobKindField(rootName string) string {
	if rootName == "Bin" {
		return "data"
	}
	return "body"
}

func blobKindProbe(t *testing.T, g *generatedUnit, rows []blobKindCase) map[string]blobKindAnswer {
	t.Helper()
	if err := os.WriteFile(filepath.Join(g.dir, "main.luau"), []byte(blobKindProbeMain(g, rows)), 0o644); err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(root, "testdata", "wire", "pointer-target-type", "blobkind")
	inputs := map[string][]byte{}
	for _, row := range rows {
		wire, err := os.ReadFile(filepath.Join(dir, row.file))
		if err != nil {
			t.Fatalf("%s: %v", row.file, err)
		}
		inputs[row.name] = wire
	}
	out := luaurun.Run(t, g.dir, "main.luau", inputs)

	got := map[string]blobKindAnswer{}
	for line := range strings.SplitSeq(strings.TrimRight(string(out), "\n"), "\n") {
		f := strings.Fields(line)
		switch {
		case len(f) == 3 && f[2] == "refused":
			got[f[0]+" "+f[1]] = blobKindAnswer{verdict: "refused"}
		case len(f) == 5 && f[2] == "read":
			n, err := strconv.Atoi(f[3])
			if err != nil {
				t.Fatalf("%s: %q is not a count", line, f[3])
			}
			got[f[0]+" "+f[1]] = blobKindAnswer{verdict: "read", unknown: n, null: f[4] == "null"}
		default:
			t.Fatalf("the probe printed %q", line)
		}
	}
	return got
}

// blobKindProblems names every answer that is not the row's verdict.
func blobKindProblems(rows []blobKindCase, got map[string]blobKindAnswer) []string {
	var problems []string
	for _, row := range rows {
		for _, entry := range []string{"plain", "retain"} {
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
			// A newer writer's field names the record: the declared slot stays
			// null (docs/SPEC-TABLES.md §3.1; the manifest's text case).
			if row.name == "text-unknown-field-bytes" && !answer.null {
				problems = append(problems, fmt.Sprintf("%s %s: the pointer is not null", row.name, entry))
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
		g := generateFiles(t, src)

		t.Run(unit+" every row answers its verdict", func(t *testing.T) {
			got := blobKindProbe(t, g, unitRows)
			for _, row := range unitRows {
				plain, retain := got[row.name+" plain"], got[row.name+" retain"]
				t.Logf("%s: plain %s unknown %d null %t; retain %s unknown %d null %t", row.name, plain.verdict, plain.unknown, plain.null, retain.verdict, retain.unknown, retain.null)
			}
			for _, p := range blobKindProblems(unitRows, got) {
				t.Error(p)
			}
		})

		t.Run(unit+" negative control: the root-closure rule accepts text-names-bytes", func(t *testing.T) {
			planted := copyGeneratedUnit(t, g)
			plantBlobKindRootClosureRule(t, planted)
			problems := blobKindProblems(unitRows, blobKindProbe(t, planted, unitRows))
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
			for name, wire := range blobKindSaves(t, g) {
				var report tabletext.Report
				ok, err := tablewire.Decode(m, m.New(m.Lookup(strings.SplitN(name, ".", 2)[0])), wire, &report)
				if !ok || err != nil || report.Refused || report.Unknown != 0 {
					t.Errorf("%s: the leg's own save does not read back clean: %v %+v", name, err, report)
				}
			}
		})
	}
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

// plantBlobKindRootClosureRule puts the old rule back in a copy of the emitted
// reader: a blob id is nameable only when the root's own closure reaches a
// blob of that kind, so a wrong-kind record is skipped and its pointer null.
func plantBlobKindRootClosureRule(t *testing.T, g *generatedUnit) {
	t.Helper()
	const anchor = "local declared = S.tableId(tid) or tid == StringBlobId or tid == BytesBlobId"
	const planted = "local declared = S.tableId(tid)"
	path := filepath.Join(g.dir, "schema_load.luau")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Count(string(data), anchor) != 1 {
		t.Fatalf("the root-closure anchor is not in the emitted schema_load.luau exactly once")
	}
	patched := strings.Replace(string(data), anchor, planted, 1)
	if err := os.WriteFile(path, []byte(patched), 0o644); err != nil {
		t.Fatal(err)
	}
}

// blobKindSaves runs the leg's own save and retaining save of Text and Bin,
// the blob set and null, and returns each saved wire by name.
func blobKindSaves(t *testing.T, g *generatedUnit) map[string][]byte {
	t.Helper()
	var mods []string
	for name := range g.files {
		if strings.HasSuffix(name, "Table.luau") {
			mods = append(mods, strings.TrimSuffix(name, ".luau"))
		}
	}
	sort.Strings(mods)

	var main strings.Builder
	main.WriteString("local S = require(\"./schema_save\")\nlocal R = require(\"./schema_retain\")\nlocal St = require(\"./schema_tables\")\n")
	for _, mod := range mods {
		fmt.Fprintf(&main, "require(%q)\n", "./"+mod)
	}
	main.WriteString(`local function hex(b: buffer): string
	local parts = table.create(buffer.len(b))
	for i = 0, buffer.len(b) - 1 do
		parts[i + 1] = string.format("%02x", buffer.readu8(b, i))
	end
	return table.concat(parts)
end
for _, c in { { "Text", "body" }, { "Bin", "data" } } do
	for _, set in { false, true } do
		local d = St.lookup(c[1])
		local v = d.new()
		if set then v[c[2]] = buffer.fromstring("hello") end
		local name = c[1] .. "." .. c[2] .. (if set then ".set" else ".null")
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
`)
	if err := os.WriteFile(filepath.Join(g.dir, "save.luau"), []byte(main.String()), 0o644); err != nil {
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
	if len(saves) != 8 {
		t.Fatalf("the save probe answered %d saved wires, want 8", len(saves))
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

// blobKindModel compiles one manifest unit into the engine's model.
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
