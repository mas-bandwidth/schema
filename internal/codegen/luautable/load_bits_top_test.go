package luautable_test

import (
	"bytes"
	"encoding/binary"
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
	"github.com/mas-bandwidth/schema/v2/internal/codegen/luau"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/luau/luaurun"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/luautable"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
)

// bitsTopSchema declares bits(N) at each width whose top the double cannot
// hold exactly, or whose 8-byte pattern carries bit 63, plus bits(63) as a map
// value and as an array element (an array of bits(N) is wrapped in a type,
// and a bits(N) map key is refused by the compiler, docs/SPEC-TABLES.md §2.8).
const bitsTopSchema = `package probe

type W
{
    b bits(63)
}

table Box
{
    b40 bits(40)
    b53 bits(53)
    b54 bits(54)
    b60 bits(60)
    b62 bits(62)
    b63 bits(63)
    v map[uint8]bits(63)
    arr []W
}
`

// bitsTopSentinel is the lawful value saved in each target, unique on the
// wire, so the case's 8 bytes are found and overwritten in place.
func bitsTopSentinel(tag uint64) uint64 { return 0x3C3C3C00 | tag }

// bitsTopTargets names each target, its declared width and its sentinel tag.
var bitsTopTargets = []struct {
	target string
	bits   uint
	tag    uint64
}{
	{"B40", 40, 40},
	{"B53", 53, 53},
	{"B54", 54, 54},
	{"B60", 60, 60},
	{"B62", 62, 62},
	{"B63", 63, 63},
	{"V", 63, 0x80},
	{"Arr", 63, 0x81},
}

// bitsTopSave builds the lawful record with every sentinel and saves it with
// the port's own save.
const bitsTopSave = `local T = require("./BoxTable")
local Bx = require("./Box")
local S = require("./schema_save")
local v = T.NewBox()
v.B40 = 0x3C3C3C28i
v.B53 = 0x3C3C3C35i
v.B54 = 0x3C3C3C36i
v.B60 = 0x3C3C3C3Ci
v.B62 = 0x3C3C3C3Ei
v.B63 = 0x3C3C3C3Fi
local e = T.NewBoxVEntry()
e.Key = 1
e.Value = 0x3C3C3C80i
v.V = { e }
local w = Bx.NewW()
w.B = 0x3C3C3C81i
v.Arr = { w }
local b, why = S.save(T.BoxDescriptor, v)
assert(b ~= nil, tostring(why))
local parts = table.create(buffer.len(b))
for i = 0, buffer.len(b) - 1 do parts[i + 1] = string.format("%02x", buffer.readu8(b, i)) end
print(table.concat(parts))
`

// bitsTopGoOracle reads every case wire with the same unit's Go strict load
// and prints its verdict and the target's value; it is the expected answer.
const bitsTopGoOracle = `package probe

import (
	"encoding/hex"
	"fmt"
	"os"
	"strings"
	"testing"
)

func TestBitsTopOracle(t *testing.T) {
	data, err := os.ReadFile("cases.txt")
	if err != nil {
		t.Fatal(err)
	}
	for _, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
		f := strings.Fields(line)
		name, target := f[0], f[1]
		wire, err := hex.DecodeString(f[2])
		if err != nil {
			t.Fatal(err)
		}
		n := BoxLoadMeasure(wire)
		if n < 0 {
			n = 0
		}
		region := make([]byte, n)
		var report TableReport
		root := BoxLoad(region, wire, &report)
		if root == nil {
			fmt.Printf("@%s|refused|-\n", name)
			continue
		}
		var got uint64
		switch target {
		case "B40":
			got = root.B40
		case "B53":
			got = root.B53
		case "B54":
			got = root.B54
		case "B60":
			got = root.B60
		case "B62":
			got = root.B62
		case "B63":
			got = root.B63
		case "V":
			BoxVEach(&root.V, func(_ uint8, v *uint64) bool { got = *v; return true })
		case "Arr":
			BoxArrEach(&root.Arr, func(w *WRow) bool { got = w.B; return true })
		}
		fmt.Printf("@%s|read|%d\n", name, int64(got))
	}
}
`

// bitsTopLoad reads every case wire through the port's public strict load.
// A raised error is printed as its own verdict, never swallowed.
const bitsTopLoad = `local inputs = require("./inputs")
local load = require("./schema_load")
require("./BoxTable")
local cases = require("./cases")
for _, c in ipairs(cases) do
	local report = { malformed = false, refused = 0, unknown = 0, kindMismatch = 0, clamped = 0, widened = 0, duplicate = 0 }
	local ok, v = pcall(load.load, "Box", inputs[c.name], report)
	if not ok then
		print(c.name .. "|raised|-")
	elseif v == nil then
		print(c.name .. "|refused|-")
	else
		local got: any
		if c.target == "V" then
			got = v.V[1].Value
		elseif c.target == "Arr" then
			got = v.Arr[1].B
		else
			got = v[c.target]
		end
		print(c.name .. "|read|" .. tostring(got))
	end
end
`

// TestLuauLoadBitsTopEveryWidth pins docs/SPEC-TABLES.md §16.2's implied
// range of bits(N), [0, 2^N - 1], on the variable-form strict load for the
// widths a double cannot hold exactly and the 8-byte patterns with bit 63
// set: 2^N - 1 reads exactly, 2^N and all ones refuse the whole load
// (docs/rulings.json strict-only-read), a small lawful bits(63) reads, and
// nothing raises. The same unit's generated Go strict load is the oracle.
func TestLuauLoadBitsTopEveryWidth(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "the pinned Luau interpreter and the schema-go oracle")
	dir := t.TempDir()
	schemaPath := filepath.Join(dir, "Box.schema")
	if err := os.WriteFile(schemaPath, []byte(bitsTopSchema), 0o644); err != nil {
		t.Fatal(err)
	}
	u, err := compiler.New().Load([]string{schemaPath})
	if err != nil {
		t.Fatal(err)
	}

	files, err := luau.Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	tables, err := luautable.Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	maps.Copy(files, tables)
	luauDir := t.TempDir()
	for name, data := range files {
		if err := os.WriteFile(filepath.Join(luauDir, name), data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(luauDir, "main.luau"), []byte(bitsTopSave), 0o644); err != nil {
		t.Fatal(err)
	}
	valid, err := hex.DecodeString(strings.TrimSpace(string(luaurun.Run(t, luauDir, "main.luau", nil))))
	if err != nil {
		t.Fatalf("the saved wire: %v", err)
	}
	if len(valid) == 0 || valid[0] != 1 {
		t.Fatalf("the saved wire is not the variable form: %x", valid)
	}

	type bitsCase struct {
		name, target string
		wire         []byte
	}
	var cases []bitsCase
	for _, tg := range bitsTopTargets {
		var sentinel [8]byte
		binary.LittleEndian.PutUint64(sentinel[:], bitsTopSentinel(tg.tag))
		at := bytes.Index(valid, sentinel[:])
		if at < 0 || bytes.Count(valid, sentinel[:]) != 1 {
			t.Fatalf("%s: the sentinel is not on the wire exactly once", tg.target)
		}
		values := []struct {
			what  string
			value uint64
		}{
			{"top", uint64(1)<<tg.bits - 1},
			{"over", uint64(1) << tg.bits},
			{"ones", ^uint64(0)},
		}
		if tg.target == "B63" {
			values = append(values, struct {
				what  string
				value uint64
			}{"five", 5})
		}
		for _, v := range values {
			wire := append([]byte(nil), valid...)
			binary.LittleEndian.PutUint64(wire[at:at+8], v.value)
			cases = append(cases, bitsCase{name: fmt.Sprintf("%s-%s", tg.target, v.what), target: tg.target, wire: wire})
		}
	}

	goFiles, err := golang.Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	goTables, err := gotable.Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	maps.Copy(goFiles, goTables)
	runtime := tfSerializeGoRuntime(t)
	goFiles["go.mod"] = []byte(fmt.Sprintf("module probe\n\ngo 1.26\n\nrequire github.com/mas-bandwidth/serialize.go/v2 v2.0.0\nreplace github.com/mas-bandwidth/serialize.go/v2 => %q\n", runtime))
	goFiles["oracle_test.go"] = []byte(bitsTopGoOracle)
	var caseLines strings.Builder
	for _, c := range cases {
		fmt.Fprintf(&caseLines, "%s %s %s\n", c.name, c.target, hex.EncodeToString(c.wire))
	}
	goFiles["cases.txt"] = []byte(caseLines.String())
	goDir := t.TempDir()
	for name, data := range goFiles {
		if err := os.WriteFile(filepath.Join(goDir, name), data, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	cmd := exec.Command("nice", "-n", "19", "go", "test", "-count=1", "-timeout", "600s", "-run", "^TestBitsTopOracle$", "-v", ".")
	cmd.Dir = goDir
	cmd.Env = append(os.Environ(), "GOFLAGS=-mod=readonly")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("the generated Go oracle failed: %v\n%s", err, out)
	}
	want := map[string]string{}
	for line := range strings.SplitSeq(string(out), "\n") {
		if after, ok := strings.CutPrefix(line, "@"); ok {
			name, _, ok := strings.Cut(after, "|")
			if !ok {
				t.Fatalf("unexpected oracle line %q", line)
			}
			want[name] = after
		}
	}
	if len(want) != len(cases) {
		t.Fatalf("the oracle answered %d of %d cases:\n%s", len(want), len(cases), out)
	}

	var casesModule strings.Builder
	casesModule.WriteString("return {\n")
	inputs := map[string][]byte{}
	for _, c := range cases {
		fmt.Fprintf(&casesModule, "\t{ name = %q, target = %q },\n", c.name, c.target)
		inputs[c.name] = c.wire
	}
	casesModule.WriteString("}\n")
	if err := os.WriteFile(filepath.Join(luauDir, "cases.luau"), []byte(casesModule.String()), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(luauDir, "main.luau"), []byte(bitsTopLoad), 0o644); err != nil {
		t.Fatal(err)
	}
	luauOut := luaurun.Run(t, luauDir, "main.luau", inputs)
	got := map[string]string{}
	for line := range strings.SplitSeq(strings.TrimSpace(string(luauOut)), "\n") {
		name, _, ok := strings.Cut(line, "|")
		if !ok {
			t.Fatalf("unexpected Luau line %q", line)
		}
		got[name] = line
	}
	for _, c := range cases {
		if got[c.name] != want[c.name] {
			t.Errorf("%s: Luau %q, Go %q", c.name, got[c.name], want[c.name])
		}
	}
}
