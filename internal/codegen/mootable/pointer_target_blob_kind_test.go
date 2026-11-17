package mootable_test

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/compiler"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
)

// TestMooPointerTargetBlobKindRefused holds every moo read of the variable
// form to the blob-kind vectors (docs/SPEC-TABLES.md §3.1, §6.5;
// docs/rulings.json blob-target-outside-closure-refused). The two reserved
// blob ids are types every reader names, whatever its root reaches: a declared
// slot naming a blob record of the other kind refuses the whole read, a blob
// record no pointer reaches is damage, and a blob record reached only from a
// field this reader does not declare is evolution, counted unknown. Each row
// of testdata/wire/pointer-target-type/blobkind/MANIFEST.tsv runs through the
// plain load, the strict read and the retaining load.
func TestMooPointerTargetBlobKindRefused(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "moo (it runs the emitted readers)")

	dir := filepath.Join(root, "testdata", "wire", "pointer-target-type", "blobkind")
	rows, inputs := readBlobKindRows(t, dir)
	byUnit := map[string][]blobKindRow{}
	var units []string
	for _, r := range rows {
		if _, ok := byUnit[r.unit]; !ok {
			units = append(units, r.unit)
		}
		byUnit[r.unit] = append(byUnit[r.unit], r)
	}

	var reader *generatedUnit
	for _, unit := range units {
		g := generateBlobKindUnit(t, unit)
		if unit == rows[0].unit {
			reader = g
		}
		out := g.run(t, blobKindProbe(g, byUnit[unit]), inputs)
		got := parseBlobKindProbe(t, out)
		for _, r := range byUnit[unit] {
			for _, path := range []string{"load", "strict", "retain"} {
				checkBlobKindAnswer(t, r, path, got[r.caseName+"\t"+path])
			}
		}
	}
	if reader == nil {
		t.Fatal("the manifest names no unit")
	}

	t.Run("the writer emits no unreachable blob record", func(t *testing.T) {
		checkBlobKindSaves(t, reader)
	})
	t.Run("negative control: the root closure decides which blobs a reader can name", func(t *testing.T) {
		// THE OLD RULE: a blob id is nameable only when the root's closure
		// reaches that kind, so a declared slot naming the other kind reads
		// null instead of refusing. Planted in a copy of the generated reader,
		// text-names-bytes must stop refusing.
		const have = "if n.tid ~= want and (declared[n.tid] or n.tid == StringBlobId or n.tid == BytesBlobId) then"
		const planted = "if n.tid ~= want and declared[n.tid] then"
		path := filepath.Join(reader.dir, "table_load.moo")
		src, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Count(string(src), have) != 1 {
			t.Fatalf("the blob nameability anchor is not in the emitted reader exactly once")
		}
		if err := os.WriteFile(path, []byte(strings.Replace(string(src), have, planted, 1)), 0o644); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() {
			if err := os.WriteFile(path, src, 0o644); err != nil {
				t.Error(err)
			}
		})
		out := reader.run(t, blobKindProbe(reader, byUnit[rows[0].unit]), inputs)
		got := parseBlobKindProbe(t, out)
		f := got["text-names-bytes\tload"]
		if f == nil {
			t.Fatal("text-names-bytes: the planted reader answered nothing")
		}
		if f[0] == "refused" {
			t.Error("text-names-bytes: the gate passed a reader that names a blob only when the root's closure reaches it")
		}
	})
}

type blobKindRow struct {
	caseName string
	unit     string
	rootName string
	read     bool
	unknown  int
	slot     string
}

func readBlobKindRows(t *testing.T, dir string) ([]blobKindRow, map[string][]byte) {
	t.Helper()
	manifest, err := os.ReadFile(filepath.Join(dir, "MANIFEST.tsv"))
	if err != nil {
		t.Fatal(err)
	}
	var rows []blobKindRow
	inputs := map[string][]byte{}
	for i, line := range strings.Split(string(manifest), "\n") {
		line = strings.TrimRight(line, "\r")
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		f := strings.Split(line, "\t")
		if len(f) != 6 {
			t.Fatalf("MANIFEST.tsv:%d: %d fields, want 6", i+1, len(f))
		}
		if f[4] != "read" && f[4] != "refused" {
			t.Fatalf("MANIFEST.tsv:%d: verdict %q, want read or refused", i+1, f[4])
		}
		r := blobKindRow{caseName: f[0], unit: f[1], rootName: f[2], read: f[4] == "read", unknown: -1, slot: "Body"}
		if r.rootName == "Bin" {
			r.slot = "Data"
		}
		if f[5] != "-" {
			n, err := strconv.Atoi(f[5])
			if err != nil {
				t.Fatalf("MANIFEST.tsv:%d: unknown %q: %v", i+1, f[5], err)
			}
			r.unknown = n
		}
		wire, err := os.ReadFile(filepath.Join(dir, f[3]))
		if err != nil {
			t.Fatal(err)
		}
		inputs[r.caseName] = wire
		rows = append(rows, r)
	}
	if len(rows) == 0 {
		t.Fatal("MANIFEST.tsv names no vectors")
	}
	return rows, inputs
}

func generateBlobKindUnit(t *testing.T, unit string) *generatedUnit {
	t.Helper()
	paths, err := compiler.GatherPaths([]string{filepath.Join(root, unit)})
	if err != nil {
		t.Fatal(err)
	}
	u, err := compiler.New().Load(paths)
	if err != nil {
		t.Fatal(err)
	}
	files, err := generate(u)
	if err != nil {
		t.Fatal(err)
	}
	g := writeUnit(t, u, files)
	return g
}

func blobKindProbe(g *generatedUnit, rows []blobKindRow) string {
	var b strings.Builder
	b.WriteString("--!strict\nlocal inputs = require(\"./inputs\")\nlocal L = require(\"./table_load\")\nlocal R = require(\"./table_retain\")\n")
	b.WriteString(g.requireAll())
	b.WriteString("local rows: { any } = {\n")
	for _, r := range rows {
		fmt.Fprintf(&b, "\t{ case = %q, root = %q, slot = %q },\n", r.caseName, r.rootName, r.slot)
	}
	b.WriteString(`}
local function line(row: any, path: string, v: any, rep: any)
	local bodyNull = "-"
	if v ~= nil then
		bodyNull = tostring(v[row.slot] == nil)
	end
	print(string.format("%s\t%s\t%s\t%d\t%s", row.case, path,
		if v ~= nil and rep.reason == nil then "read" else "refused", rep.unknown, bodyNull))
end
for _, row in rows do
	local wire = inputs[row.case]
	local rep = L.newReport()
	line(row, "load", L.load(row.root, wire, rep), rep)
	rep = L.newReport()
	line(row, "strict", L.ReadStrict(row.root, wire, rep), rep)
	rep = L.newReport()
	line(row, "retain", R.LoadRetain(row.root, wire, rep, R.newStore(1048576, nil)), rep)
end
`)
	return b.String()
}

func parseBlobKindProbe(t *testing.T, out string) map[string][]string {
	t.Helper()
	got := map[string][]string{}
	for line := range strings.SplitSeq(strings.TrimSpace(out), "\n") {
		f := strings.Split(line, "\t")
		if len(f) != 5 {
			t.Fatalf("the probe printed %q", line)
		}
		got[f[0]+"\t"+f[1]] = f[2:]
	}
	return got
}

func checkBlobKindAnswer(t *testing.T, r blobKindRow, path string, f []string) {
	t.Helper()
	key := r.caseName + "\t" + path
	if f == nil {
		t.Errorf("%s: the probe answered nothing", key)
		return
	}
	want := "refused"
	if r.read {
		want = "read"
	}
	if f[0] != want {
		t.Errorf("%s: %s answers %s, want %s", r.caseName, path, f[0], want)
		return
	}
	if !r.read {
		return
	}
	if r.unknown >= 0 {
		unknown, err := strconv.Atoi(f[1])
		if err != nil {
			t.Fatalf("%s: unknown %q: %v", key, f[1], err)
		}
		if unknown != r.unknown {
			t.Errorf("%s: unknown %d, want %d", key, unknown, r.unknown)
		}
	}
	if r.caseName == "text-unknown-field-bytes" && f[2] != "true" {
		t.Errorf("%s: the declared string slot is not null", key)
	}
	if r.caseName == "text-canonical" && f[2] != "false" {
		t.Errorf("%s: the declared string slot read null", key)
	}
}

// checkBlobKindSaves saves Text and Bin, the blob set and null, through the
// plain save and the retaining save, and holds each wire to the engine: accepted,
// nothing unknown. A conforming writer numbers only the records its slots name.
func checkBlobKindSaves(t *testing.T, g *generatedUnit) {
	t.Helper()
	m := tabletext.NewModel(g.unit)
	out := g.run(t, blobKindSaveProbe(g), nil)
	for line := range strings.SplitSeq(strings.TrimSpace(out), "\n") {
		f := strings.Split(line, "\t")
		if len(f) != 4 || (f[0] != "save" && f[0] != "retain") {
			t.Fatalf("the save probe printed %q", line)
		}
		wire := decodeHex(t, f[3])
		st := m.Lookup(f[1])
		if st == nil {
			t.Fatalf("no root %s", f[1])
		}
		var report tabletext.Report
		ok, err := tablewire.Decode(m, m.New(st), wire, &report)
		if !ok || err != nil || report.Refused || report.Unknown != 0 {
			t.Errorf("%s %s blob %s: the engine did not accept the save with unknown 0: %v %+v", f[0], f[1], f[2], err, report)
		}
	}
}

func blobKindSaveProbe(g *generatedUnit) string {
	return "--!strict\nlocal save = require(\"./table_save\")\nlocal R = require(\"./table_retain\")\nlocal St = require(\"./schema_tables\")\n" +
		g.requireAll() + `
local function hex(b: buffer): string
	local parts = table.create(buffer.len(b))
	for i = 0, buffer.len(b) - 1 do
		parts[i + 1] = string.format("%02x", buffer.readu8(b, i))
	end
	return table.concat(parts)
end
local function emit(root: string, slot: string, data: buffer?)
	local d = St.lookup(root)
	local v = d.new()
	if data ~= nil then
		v[slot] = data
	end
	local which = if data ~= nil then "set" else "null"
	local out, why = save.save(d, v)
	if out == nil then
		print("SAVENIL\t" .. root .. "\t" .. which .. "\t" .. tostring(why))
		return
	end
	print("save\t" .. root .. "\t" .. which .. "\t" .. hex(out))
	local rep = { retainLost = 0 }
	local rout, rwhy = R.SaveRetain(root, v, R.newStore(1048576, nil), rep)
	if rout == nil then
		print("RETAINNIL\t" .. root .. "\t" .. which .. "\t" .. tostring(rwhy))
		return
	end
	print("retain\t" .. root .. "\t" .. which .. "\t" .. hex(rout))
end
local hello = buffer.fromstring("hello")
emit("Text", "Body", hello)
emit("Text", "Body", nil)
emit("Bin", "Data", hello)
emit("Bin", "Data", nil)
`
}

func decodeHex(t *testing.T, s string) []byte {
	t.Helper()
	if len(s)%2 != 0 {
		t.Fatalf("odd hex %q", s)
	}
	out := make([]byte, len(s)/2)
	for i := 0; i < len(out); i++ {
		n, err := strconv.ParseUint(s[i*2:i*2+2], 16, 8)
		if err != nil {
			t.Fatalf("hex %q: %v", s, err)
		}
		out[i] = byte(n)
	}
	return out
}
