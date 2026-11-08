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

// fixedNamesSchema is the Point and Row pair of gotable's
// TestFixedLoadNamesEveryRefusal, in one unit so a single generated Go probe
// and a single Luau probe can answer every case.
const fixedNamesSchema = `package probe
fixed table Point {
    x int32 = 1
    y int32 = 2
}
fixed table Row {
    s string(16)
}
`

// fixedNamesOracle is written into the generated Go unit's package. It forges
// the SAME files TestFixedLoadNamesEveryRefusal forges and lets THAT unit's
// own fixed load name each one, so the expected bytes and verdicts are Go's
// engine's and never typed here (docs/FIXED-FORM-ALGORITHM.md §5.3).
const fixedNamesOracle = `package probe

import (
	"encoding/binary"
	"fmt"
	"testing"
)

func oraclePoint(t *testing.T, n int) []byte {
	t.Helper()
	vals := make([]Point, n)
	for i := range vals {
		vals[i] = Point{X: int32(4242 + i), Y: int32(-7 - i)}
	}
	buf := make([]byte, PointFixedMeasure(int64(n)))
	if got := PointFixedSave(vals, buf); got != int64(len(buf)) {
		t.Fatalf("save %d", got)
	}
	return buf
}

func oracleRow(t *testing.T) []byte {
	t.Helper()
	row := Row{S: [16]byte{'h', 'e', 'l', 'l', 'o'}, SLength: 5}
	need := RowFixedMeasure(1)
	buf := make([]byte, need)
	if n := RowFixedSave([]Row{row}, buf); n != need {
		t.Fatalf("save %d", n)
	}
	return buf
}

func TestFixedOracle(t *testing.T) {
	plan := make([]TableFixedEntry, 64)
	point := oraclePoint(t, 2)
	row := oracleRow(t)
	forge := func(b []byte, edit func([]byte) []byte) []byte { return edit(append([]byte(nil), b...)) }
	form := func(v byte) []byte { return forge(point, func(b []byte) []byte { b[0] = v; return b }) }
	rowRecordsAt := TableFixedHeaderBytes + 4 + int(binary.LittleEndian.Uint32(row[TableFixedHeaderBytes:]))
	rowBadText := forge(row, func(b []byte) []byte { copy(b[rowRecordsAt+12:], []byte{0xC0, 0xAF}); return b })
	rowBadLen := forge(row, func(b []byte) []byte { binary.LittleEndian.PutUint32(b[rowRecordsAt+8:], 17); return b })
	type c struct {
		name, root string
		data       []byte
	}
	cases := []c{
		{"previous_form", "Point", form(1)},
		{"message_form_as_file", "Point", form(2)},
		{"newer_form", "Point", form(6)},
		{"layout_malformed", "Point", forge(point, func(b []byte) []byte { b[TableFixedHeaderBytes+4] ^= 1; return b })},
		{"no_layout", "Point", forge(point, func(b []byte) []byte { b[len(b)-2*int(PointFixedRecordBytes)] ^= 1; return b })},
		{"truncated empty", "Point", nil},
		{"truncated short header", "Point", point[:19]},
		{"truncated ragged", "Point", append(append([]byte(nil), point...), 0)},
		{"reserved_not_zero", "Point", forge(point, func(b []byte) []byte { b[3] = 1; return b })},
		{"layout_unknown", "Point", forge(point, func(b []byte) []byte { b[TableFixedHashAt] ^= 1; return b })},
		{"text_ill_formed", "Row", rowBadText},
		{"length_over_bound", "Row", rowBadLen},
	}
	for _, c := range cases {
		r := TableReport{}
		var n int64
		switch c.root {
		case "Point":
			dst := make([]Point, 2)
			n = PointFixedLoad(dst, c.data, plan, &r)
		case "Row":
			dst := make([]Row, 1)
			n = RowFixedLoad(dst, c.data, plan, &r)
		}
		fmt.Printf("ORACLE %s|%d|%x\n", c.name, n, c.data)
	}
}
`

type fixedOracleCase struct {
	file []byte
}

// fixedGoOracle generates the same unit for Go and runs its own fixed load over
// the forged files, holding each to Go's verdict, REFUSED (-1), and returning
// the bytes. The generated Go compiles in t.TempDir(). The reason is per-language
// diagnostic detail and is not compared (docs/rulings.json refusal-verdict-only).
func fixedGoOracle(t *testing.T, schemaSrc string) map[string]fixedOracleCase {
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
	files["oracle_test.go"] = []byte(fixedNamesOracle)
	for name, data := range files {
		if err := os.WriteFile(filepath.Join(dir, name), data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	cmd := exec.Command("go", "test", "-count=1", "-timeout", "600s", "-run", "^TestFixedOracle$", "-v", ".")
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("the generated Go unit's fixed load: %v\n%s", err, out)
	}
	cases := map[string]fixedOracleCase{}
	sc := bufio.NewScanner(strings.NewReader(string(out)))
	for sc.Scan() {
		line := sc.Text()
		rest, ok := strings.CutPrefix(line, "ORACLE ")
		if !ok {
			continue
		}
		fields := strings.Split(rest, "|")
		if len(fields) != 3 {
			t.Fatalf("oracle line %q is not name|n|hex", line)
		}
		file, err := hex.DecodeString(fields[2])
		if err != nil {
			t.Fatalf("oracle line %q carries no hex file: %v", line, err)
		}
		if fields[1] != "-1" {
			t.Fatalf("%s: the Go fixed load returned %s, want -1 (refused)", fields[0], fields[1])
		}
		cases[fields[0]] = fixedOracleCase{file: file}
	}
	if len(cases) != 12 {
		t.Fatalf("the generated Go oracle answered %d cases, want 12\n%s", len(cases), out)
	}
	return cases
}

// TestLuauFixedLoadNamesEveryRefusal holds schema_fixed.luau to Go's VERDICT for
// every case of gotable's TestFixedLoadNamesEveryRefusal that has a Luau surface
// (docs/SPEC-TABLES.md §3.4, §6.5; docs/FIXED-FORM-ALGORITHM.md §5.3): the files
// are built and refused by the same unit's generated Go fixed load, then
// schema_fixed.luau loads those exact bytes and must refuse each one with a named
// refusal and hand back nothing. Which name it gives is per-language diagnostic
// detail and is not compared (docs/rulings.json refusal-verdict-only).
// plan_too_large (no plan argument) and batch_too_large (F.load takes no
// capacity) have no Luau surface.
func TestLuauFixedLoadNamesEveryRefusal(t *testing.T) {
	slowtest.Gate(t, "the pinned Luau interpreter and the schema-go oracle")
	t.Parallel()
	oracle := fixedGoOracle(t, fixedNamesSchema)
	g := generateFiles(t, map[string]string{"Probe.schema": fixedNamesSchema})
	if g.err != nil {
		t.Fatalf("the Luau unit does not generate: %v", g.err)
	}

	names := []string{
		"previous_form", "message_form_as_file", "newer_form", "layout_malformed",
		"no_layout", "truncated empty", "truncated short header", "truncated ragged",
		"reserved_not_zero", "layout_unknown", "text_ill_formed", "length_over_bound",
	}
	roots := map[string]string{
		"previous_form": "Point", "message_form_as_file": "Point", "newer_form": "Point",
		"layout_malformed": "Point", "no_layout": "Point", "truncated empty": "Point",
		"truncated short header": "Point", "truncated ragged": "Point",
		"reserved_not_zero": "Point", "layout_unknown": "Point",
		"text_ill_formed": "Row", "length_over_bound": "Row",
	}
	inputs := map[string][]byte{}
	for _, name := range names {
		inputs[name] = oracle[name].file
	}

	var main strings.Builder
	main.WriteString("local T = require(\"./ProbeTable\")\nlocal F = require(\"./schema_fixed\")\nlocal inputs = require(\"./inputs\")\n")
	main.WriteString("local function fixedOf(root: string)\n")
	main.WriteString("\treturn { hash = T[root .. \"FixedHash\"], recordBytes = T[root .. \"FixedRecordBytes\"], layout = T[root .. \"FixedLayout\"] }\nend\n")
	main.WriteString("local cases = {\n")
	for _, name := range names {
		fmt.Fprintf(&main, "\t{ %q, %q },\n", name, roots[name])
	}
	main.WriteString("}\n")
	main.WriteString(`for _, c in cases do
	local name, root = c[1], c[2]
	local ok, err = pcall(function()
		return F.load(T[root .. "Descriptor"], fixedOf(root), inputs[name])
	end)
	if ok then
		print(name .. "|ok")
	elseif type(err) == "table" and type(err.name) == "string" and err.name ~= "" then
		print(name .. "|refused")
	else
		print(name .. "|error " .. tostring(err))
	end
end
`)
	if err := os.WriteFile(filepath.Join(g.dir, "main.luau"), []byte(main.String()), 0o644); err != nil {
		t.Fatal(err)
	}
	out := string(luaurun.Run(t, g.dir, "main.luau", inputs))
	got := map[string]string{}
	for line := range strings.SplitSeq(strings.TrimSpace(out), "\n") {
		name, verdict, ok := strings.Cut(line, "|")
		if !ok {
			t.Fatalf("the Luau probe printed %q, want name|verdict", line)
		}
		got[name] = verdict
	}
	for _, name := range names {
		g, ok := got[name]
		if !ok {
			t.Errorf("%s: the Luau probe answered no line", name)
			continue
		}
		if g != "refused" {
			t.Errorf("%s: schema_fixed.luau answered %s, Go refused it", name, g)
		}
	}
}
