package luautable_test

import (
	"bufio"
	"encoding/hex"
	"fmt"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/compiler"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/golang"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/gotable"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/luau/luaurun"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
)

// fixedBoundsSchema has a nested fixed struct so a forged layout can make the
// inner struct's children sum past its own size without touching the root.
const fixedBoundsSchema = `package probe
fixed table Inner {
    a int32 = 1
    b int32 = 2
}
fixed table Root {
    x int32 = 1
    inner Inner
    y int32 = 2
}
`

// fixedBoundsGoOracle writes a generated Go unit for fixedBoundsSchema and runs
// ITS fixed reader over the valid file and the forged layouts, so every refusal
// name and the record values the Luau test expects come from schema-go's own
// reader and never from a name typed in this test
// (docs/FIXED-FORM-ALGORITHM.md §1.1, §5.3; docs/SPEC-TABLES.md §3.4).
const fixedBoundsGoOracle = `package probe

import (
	"encoding/binary"
	"fmt"
	"testing"
)

func TestFixedBoundsOracle(t *testing.T) {
	one := Root{X: 11, Inner: Inner{A: 22, B: 33}, Y: 44}
	need := RootFixedMeasure(1)
	file := make([]byte, need)
	if n := RootFixedSave([]Root{one}, file); n != need {
		t.Fatalf("save %d want %d", n, need)
	}
	plan := make([]TableFixedEntry, 64)
	var r TableReport
	got := make([]Root, 1)
	if n := RootFixedLoad(got, file, plan, &r); n != 1 || r != (TableReport{}) {
		t.Fatalf("the valid file reads: n=%d %+v", n, r)
	}
	fmt.Printf("VALID|%d|%d|%d|%d|%x\n", got[0].X, got[0].Inner.A, got[0].Inner.B, got[0].Y, file)

	// The file's own layout: root entry 0, then the pre-order walk. The nested
	// struct is the first non-root entry of kind 13, and its two leaves follow.
	layoutAt := TableFixedHeaderBytes + 4
	count := int(binary.LittleEndian.Uint32(file[layoutAt:]))
	entryAt := func(k int) int { return layoutAt + 4 + 17*k }
	get32 := func(b []byte, at int) uint32 { return binary.LittleEndian.Uint32(b[at:]) }
	put32 := func(b []byte, at int, v uint32) { binary.LittleEndian.PutUint32(b[at:], v) }
	inner := -1
	for k := 1; k < count; k++ {
		if file[entryAt(k)+8] == 13 {
			inner = k
			break
		}
	}
	if inner < 0 || inner+2 >= count {
		t.Fatalf("the layout has no nested struct with two leaves: count=%d inner=%d", count, inner)
	}
	cases := []struct {
		name string
		edit func([]byte)
	}{
		// A child count past the last entry: the pre-order walk runs out of
		// layout (docs/FIXED-FORM-ALGORITHM.md §1.1 rule 5).
		{"children_past_end", func(b []byte) {
			put32(b, entryAt(0)+13, get32(b, entryAt(0)+13)+1)
		}},
		// The nested struct's children sum past its own size, while the root
		// still sums to recordBytes and every child passes the root's end.
		{"struct_sum_past_size", func(b []byte) {
			put32(b, entryAt(inner)+9, get32(b, entryAt(inner+1)+9))
		}},
		// The last leaf's offset + size passes the record body. The inner size
		// grows to the record body and the last leaf to the rest of it, which
		// leaves each child inside its parent but the root past recordBytes:
		// the root size is the body F.load copies (docs/FIXED-FORM-ALGORITHM.md
		// §3, §5.3; docs/SPEC-TABLES.md §3.4 THE RECORD).
		{"leaf_past_record", func(b []byte) {
			rec := get32(b, entryAt(0)+9)
			a := get32(b, entryAt(inner+1)+9)
			put32(b, entryAt(inner)+9, rec)
			put32(b, entryAt(inner+2)+9, rec-a)
			put32(b, entryAt(0)+9, get32(b, entryAt(1)+9)+rec+get32(b, entryAt(count-1)+9))
		}},
	}
	for _, c := range cases {
		b := append([]byte(nil), file...)
		c.edit(b)
		var rep TableReport
		n := RootFixedLoad(got, b, plan, &rep)
		fmt.Printf("FORGED|%s|%s|%t|%d|%x\n", c.name, rep.Reason, rep.Malformed, n, b)
	}
}
`

type fixedBoundsCase struct {
	reason    string
	malformed bool
	file      []byte
}

// fixedBoundsOracle generates and runs the Go unit above, returning the valid
// file, the values its own reader lands, and one forged case per layout.
func fixedBoundsOracle(t *testing.T, schemaSrc string) ([]byte, string, map[string]fixedBoundsCase) {
	t.Helper()
	dir := t.TempDir()
	schema := filepath.Join(dir, "Probe.schema")
	if err := os.WriteFile(schema, []byte(schemaSrc), 0o644); err != nil {
		t.Fatal(err)
	}
	paths, err := compiler.GatherPaths([]string{schema})
	if err != nil {
		t.Fatal(err)
	}
	u, err := compiler.New().Load(paths)
	if err != nil {
		t.Fatal(err)
	}
	packet, err := golang.Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	tables, err := gotable.Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	files := map[string][]byte{}
	maps.Copy(files, packet)
	maps.Copy(files, tables)
	mod := "module probe\n\ngo 1.26\n"
	for _, data := range files {
		if strings.Contains(string(data), `"github.com/mas-bandwidth/serialize.go"`) {
			mod += fmt.Sprintf("\nrequire github.com/mas-bandwidth/serialize.go v0.0.0\n\nreplace github.com/mas-bandwidth/serialize.go => %q\n",
				slowtest.SerializeGo(t, "../../.."))
			break
		}
	}
	files["go.mod"] = []byte(mod)
	files["oracle_test.go"] = []byte(fixedBoundsGoOracle)
	for name, data := range files {
		if err := os.WriteFile(filepath.Join(dir, name), data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	cmd := exec.Command("go", "test", "-count=1", "-timeout", "600s", "-run", "^TestFixedBoundsOracle$", "-v", ".")
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("the generated Go unit's fixed reader: %v\n%s", err, out)
	}
	var valid []byte
	var values string
	cases := map[string]fixedBoundsCase{}
	sc := bufio.NewScanner(strings.NewReader(string(out)))
	for sc.Scan() {
		line := sc.Text()
		if rest, ok := strings.CutPrefix(line, "VALID|"); ok {
			fields := strings.Split(rest, "|")
			if len(fields) != 5 {
				t.Fatalf("the Go oracle's VALID line %q is not values|hex", line)
			}
			values = strings.Join(fields[:4], "|")
			valid, err = hex.DecodeString(fields[4])
			if err != nil {
				t.Fatalf("the Go oracle's VALID line carries no hex file: %v", err)
			}
			continue
		}
		rest, ok := strings.CutPrefix(line, "FORGED|")
		if !ok {
			continue
		}
		fields := strings.Split(rest, "|")
		if len(fields) != 5 {
			t.Fatalf("the Go oracle's FORGED line %q is not name|reason|malformed|n|hex", line)
		}
		if fields[3] != "-1" {
			t.Fatalf("%s: the Go fixed reader returned %s, want -1", fields[0], fields[3])
		}
		file, err := hex.DecodeString(fields[4])
		if err != nil {
			t.Fatalf("the Go oracle's %s line carries no hex file: %v", fields[0], err)
		}
		cases[fields[0]] = fixedBoundsCase{reason: fields[1], malformed: fields[2] == "true", file: file}
	}
	if len(cases) != 3 || valid == nil {
		t.Fatalf("the Go oracle answered %d forged cases and valid=%t, want 3 and true\n%s", len(cases), valid != nil, out)
	}
	return valid, values, cases
}

// TestLuauFixedLayoutBounds holds schema_fixed.luau's layout plan to the record and the
// layout it was handed: a child count past the last entry, a struct's children
// summing past its size, and a leaf past the record are all layout_malformed
// before any record byte is read, as schema-go's own fixed reader refuses the
// same bytes (docs/FIXED-FORM-ALGORITHM.md §1.1 rules 3, 5 and §3, §5.3;
// docs/SPEC-TABLES.md §3.4, THE LAYOUT ARRIVES FROM AN UNTRUSTED PEER).
func TestLuauFixedLayoutBounds(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "the pinned Luau interpreter and the schema-go oracle")
	valid, wantValues, oracle := fixedBoundsOracle(t, fixedBoundsSchema)
	g := generateFiles(t, map[string]string{"Probe.schema": fixedBoundsSchema})
	if g.err != nil {
		t.Fatalf("the Luau unit does not generate: %v", g.err)
	}

	names := []string{"children_past_end", "struct_sum_past_size", "leaf_past_record"}
	inputs := map[string][]byte{"valid": valid}
	var main strings.Builder
	main.WriteString("--!strict\n")
	main.WriteString("local T = require(\"./ProbeTable\")\nlocal F = require(\"./schema_fixed\")\nlocal inputs = require(\"./inputs\")\n")
	main.WriteString("local function fixedOf(layout: any): any\n\treturn { hash = T.RootFixedHash, recordBytes = T.RootFixedRecordBytes, layout = layout }\nend\n")
	main.WriteString("local cases = {\n")
	for _, name := range names {
		inputs[name] = oracle[name].file
		fmt.Fprintf(&main, "\t{ %q, %s },\n", name, layoutLiteral(oracle[name].file))
	}
	main.WriteString("}\n")
	main.WriteString(`local function outcome(ok: boolean, err: any): string
	if ok then return "read|false" end
	if type(err) == "table" and err.name ~= nil then
		return err.name .. "|" .. tostring(err.malformed == true)
	end
	return "error|false"
end
for _, c in cases do
	local name, layout = c[1], c[2]
	-- THE ORDINARY LOAD: the forged FILE layout under a known hash, whose bytes
	-- differ from the selected writer's, is layout_malformed before a record is
	-- read (docs/FIXED-FORM-ALGORITHM.md §5.3 step 7).
	local ok, err = pcall(function()
		return F.load(T.RootDescriptor, fixedOf(T.RootFixedLayout), inputs[name])
	end)
	print("identity|" .. name .. "|" .. outcome(ok, err))
	-- AND THE FORGED-LINEAGE LOAD: a lineage entry whose layout is itself
	-- forged, so the byte comparison passes and the entry's bounded plan walk
	-- must refuse it, the name the entry's own lane carries (§5.9 #36).
	local fok, ferr = pcall(function()
		return F.load(T.RootDescriptor, fixedOf(layout), inputs[name])
	end)
	print("foreign|" .. name .. "|" .. outcome(fok, ferr))
end
local values = F.load(T.RootDescriptor, fixedOf(T.RootFixedLayout), inputs.valid)
print(string.format("valid|%d|%d|%d|%d", values[1].X, values[1].Inner.A, values[1].Inner.B, values[1].Y))
`)
	if err := os.WriteFile(filepath.Join(g.dir, "main.luau"), []byte(main.String()), 0o644); err != nil {
		t.Fatal(err)
	}
	out := string(luaurun.Run(t, g.dir, "main.luau", inputs))
	got := map[string]struct {
		reason    string
		malformed bool
	}{}
	var gotValues string
	for line := range strings.SplitSeq(strings.TrimSpace(out), "\n") {
		fields := strings.Split(line, "|")
		if fields[0] == "valid" && len(fields) == 5 {
			gotValues = strings.Join(fields[1:], "|")
			continue
		}
		if len(fields) != 4 {
			t.Fatalf("the Luau probe printed %q, want mode|name|reason|malformed", line)
		}
		got[fields[0]+"|"+fields[1]] = struct {
			reason    string
			malformed bool
		}{fields[2], fields[3] == "true"}
	}
	for _, mode := range []string{"identity", "foreign"} {
		for _, name := range names {
			want := oracle[name]
			g, ok := got[mode+"|"+name]
			if !ok {
				t.Errorf("%s %s: the Luau probe answered no line", mode, name)
				continue
			}
			if g.reason != want.reason || g.malformed != want.malformed {
				t.Errorf("%s %s: schema_fixed.luau answered %s (malformed %t), Go answered %s (malformed %t)",
					mode, name, g.reason, g.malformed, want.reason, want.malformed)
			}
		}
	}
	if gotValues != wantValues {
		t.Errorf("the valid file's records: schema_fixed.luau landed %q, Go landed %q", gotValues, wantValues)
	}
}
