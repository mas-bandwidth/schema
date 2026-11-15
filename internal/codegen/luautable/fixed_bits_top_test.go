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

// fixedBitsTopSchema lays a bits(N) field of each width the top rule must hold
// at, between two markers: a narrow one, the last width a double holds
// exactly, and the widths past it (docs/FIXED-FORM-ALGORITHM.md §4.6).
const fixedBitsTopSchema = `package probe

fixed table S
{
    m1 uint32
    a bits(12)
    b bits(52)
    c bits(53)
    d bits(60)
    e bits(63)
    m2 uint32
}
`

// fixedBitsTopWidths names each bits field beside its N, in declaration order.
var fixedBitsTopWidths = []struct {
	name string
	bits int
}{
	{"a", 12}, {"b", 52}, {"c", 53}, {"d", 60}, {"e", 63},
}

// fixedBitsTopGoOracle saves one record with every bits(N) field at 2^N - 1,
// reads it back with schema-go's own fixed reader, then forges each field's
// bytes to 2^N and to all ones and reads each forged file. The field offsets
// come from the file's own layout (a flat root: each leaf follows the last).
// Its report is the expected answer; no verdict is typed into this test.
const fixedBitsTopGoOracle = `package probe

import (
	"encoding/binary"
	"fmt"
	"testing"
)

func TestFixedBitsTopOracle(t *testing.T) {
	one := S{M1: 0x11111111, A: 1<<12 - 1, B: 1<<52 - 1, C: 1<<53 - 1, D: 1<<60 - 1, E: 1<<63 - 1, M2: 0x22222222}
	need := SFixedMeasure(1)
	file := make([]byte, need)
	if n := SFixedSave([]S{one}, file); n != need {
		t.Fatalf("save %d want %d", n, need)
	}
	plan := make([]TableFixedEntry, 64)
	var r TableReport
	got := make([]S, 1)
	if n := SFixedLoad(got, file, plan, &r); n != 1 {
		t.Fatalf("the valid file reads: n=%d %+v", n, r)
	}
	g := got[0]
	fmt.Printf("VALID|%d|%d|%d|%d|%d|%d|%d|%x\n", g.M1, g.A, g.B, g.C, g.D, g.E, g.M2, file)

	layoutAt := TableFixedHeaderBytes + 4
	count := int(binary.LittleEndian.Uint32(file[layoutAt:]))
	entryAt := func(k int) int { return layoutAt + 4 + 17*k }
	rec := entryAt(count) + 8
	if count != 8 {
		t.Fatalf("the layout has %d entries, want the root and seven leaves", count)
	}
	off := 0
	names := []string{"m1", "a", "b", "c", "d", "e", "m2"}
	bits := map[string]uint{"a": 12, "b": 52, "c": 53, "d": 60, "e": 63}
	for k := 1; k < count; k++ {
		size := int(binary.LittleEndian.Uint32(file[entryAt(k)+9:]))
		name := names[k-1]
		n, ok := bits[name]
		if ok {
			forges := []struct {
				what string
				put  func([]byte)
			}{
				{"over", func(b []byte) {
					var w [8]byte
					binary.LittleEndian.PutUint64(w[:], uint64(1)<<n)
					copy(b, w[:size])
				}},
				{"ones", func(b []byte) {
					for i := range b {
						b[i] = 0xff
					}
				}},
			}
			for _, f := range forges {
				b := append([]byte(nil), file...)
				f.put(b[rec+off : rec+off+size])
				var rep TableReport
				fmt.Printf("FORGED|%s-%s|%d|%x\n", name, f.what, SFixedLoad(got, b, plan, &rep), b)
			}
		}
		off += size
	}
}
`

// fixedBitsTopOracle generates and runs the Go unit above, returning the valid
// file, the values its own reader lands, and each forged file with the Go
// reader's verdict.
func fixedBitsTopOracle(t *testing.T) ([]byte, string, map[string][]byte, map[string]string) {
	t.Helper()
	dir := t.TempDir()
	schema := filepath.Join(dir, "Probe.schema")
	if err := os.WriteFile(schema, []byte(fixedBitsTopSchema), 0o644); err != nil {
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
	files["oracle_test.go"] = []byte(fixedBitsTopGoOracle)
	for name, data := range files {
		if err := os.WriteFile(filepath.Join(dir, name), data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	cmd := exec.Command("go", "test", "-count=1", "-timeout", "600s", "-run", "^TestFixedBitsTopOracle$", "-v", ".")
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("the generated Go unit's fixed reader: %v\n%s", err, out)
	}
	var valid []byte
	var values string
	forged := map[string][]byte{}
	verdicts := map[string]string{}
	sc := bufio.NewScanner(strings.NewReader(string(out)))
	sc.Buffer(make([]byte, 1<<16), 1<<20)
	for sc.Scan() {
		line := sc.Text()
		if rest, ok := strings.CutPrefix(line, "VALID|"); ok {
			fields := strings.Split(rest, "|")
			if len(fields) != 8 {
				t.Fatalf("the Go oracle's VALID line %q is not values|hex", line)
			}
			values = strings.Join(fields[:7], "|")
			if valid, err = hex.DecodeString(fields[7]); err != nil {
				t.Fatalf("the Go oracle's VALID line carries no hex file: %v", err)
			}
			continue
		}
		rest, ok := strings.CutPrefix(line, "FORGED|")
		if !ok {
			continue
		}
		fields := strings.Split(rest, "|")
		if len(fields) != 3 {
			t.Fatalf("the Go oracle's FORGED line %q is not name|n|hex", line)
		}
		file, err := hex.DecodeString(fields[2])
		if err != nil {
			t.Fatalf("the Go oracle's %s line carries no hex file: %v", fields[0], err)
		}
		forged[fields[0]] = file
		verdict := "read"
		if fields[1] == "-1" {
			verdict = "refused"
		}
		verdicts[fields[0]] = verdict
	}
	if valid == nil || len(forged) != 2*len(fixedBitsTopWidths) {
		t.Fatalf("the Go oracle answered %d forged files and valid=%t, want %d and true\n%s",
			len(forged), valid != nil, 2*len(fixedBitsTopWidths), out)
	}
	return valid, values, forged, verdicts
}

// TestLuauFixedBitsTopEveryWidth holds the fixed form's strict read to a
// bits(N) field's top at every width (docs/FIXED-FORM-ALGORITHM.md §4.6,
// docs/SPEC.md §5.1): 2^N - 1 reads back exactly, and 2^N or all ones in the
// field's bytes is refused, never raised, for N of 12, 52, 53, 60 and 63, as
// schema-go's own fixed reader answers the same bytes. The verdict is compared;
// a refusal's name is per-language detail (docs/rulings.json
// refusal-verdict-only).
func TestLuauFixedBitsTopEveryWidth(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "the pinned Luau interpreter and the schema-go oracle")
	valid, wantValues, forged, want := fixedBitsTopOracle(t)
	for name, verdict := range want {
		if verdict != "refused" {
			t.Fatalf("%s: the Go fixed reader answered %s; the oracle must refuse every forged top", name, verdict)
		}
	}
	g := generateFiles(t, map[string]string{"Probe.schema": fixedBitsTopSchema})
	if g.err != nil {
		t.Fatalf("the Luau unit does not generate: %v", g.err)
	}
	inputs := map[string][]byte{"valid": valid}
	var names []string
	for _, w := range fixedBitsTopWidths {
		for _, what := range []string{"over", "ones"} {
			name := w.name + "-" + what
			names = append(names, name)
			inputs[name] = forged[name]
		}
	}
	var main strings.Builder
	main.WriteString(`--!strict
local T = require("./ProbeTable")
local F = require("./schema_fixed")
local inputs = require("./inputs")
local fixed = { hash = T.SFixedHash, recordBytes = T.SFixedRecordBytes, layout = T.SFixedLayout,
	lineage = T.SFixedLineage, floor = T.SFixedFloor }
local function dec(v: any): string
	if typeof(v) == "integer" then return string.format("%d", integer.tonumber(integer.div(v, 1000000000i))) .. string.format("%09d", integer.tonumber(integer.rem(v, 1000000000i))) end
	return string.format("%d", v)
end
local function trim(s: string): string
	local t = string.gsub(s, "^0+", "")
	if t == "" then return "0" end
	return t
end
local function verdict(name: string)
	local r: any = {}
	local ok, out = pcall(F.read, T.SDescriptor, fixed, inputs[name], r)
	if not ok then
		print(name .. "|error " .. tostring(if type(out) == "table" then out.name else out))
	elseif out == nil then
		print(name .. "|refused")
	else
		print(name .. "|read")
	end
end
local r: any = {}
local back = F.read(T.SDescriptor, fixed, inputs.valid, r)
if back == nil then
	print("valid|refused " .. tostring(r.reason))
else
	local v = back[1]
	print("valid|" .. table.concat({ trim(dec(v.M1)), trim(dec(v.A)), trim(dec(v.B)), trim(dec(v.C)), trim(dec(v.D)), trim(dec(v.E)), trim(dec(v.M2)) }, "|"))
end
`)
	for _, name := range names {
		fmt.Fprintf(&main, "verdict(%q)\n", name)
	}
	if err := os.WriteFile(filepath.Join(g.dir, "main.luau"), []byte(main.String()), 0o644); err != nil {
		t.Fatal(err)
	}
	out := string(luaurun.Run(t, g.dir, "main.luau", inputs))
	got := map[string]string{}
	var gotValues string
	for line := range strings.SplitSeq(strings.TrimSpace(out), "\n") {
		if rest, ok := strings.CutPrefix(line, "valid|"); ok {
			gotValues = rest
			continue
		}
		name, verdict, ok := strings.Cut(line, "|")
		if !ok {
			t.Fatalf("the Luau probe printed %q, want name|verdict", line)
		}
		got[name] = verdict
	}
	if gotValues != wantValues {
		t.Errorf("2^N - 1 in every field: schema_fixed.luau landed %q, Go landed %q", gotValues, wantValues)
	}
	for _, name := range names {
		g, ok := got[name]
		if !ok {
			t.Errorf("%s: the Luau probe answered no line", name)
			continue
		}
		if g != want[name] {
			t.Errorf("%s: schema_fixed.luau answered %s, Go %s", name, g, want[name])
		}
	}
}
