package luautable_test

import (
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

// The generated Go oracle saves one valid form-1 wire and reads it back with
// the last eight bytes, the id table's entry count (docs/SPEC-TABLES.md §3),
// replaced by each hostile count. Its report is the expected answer; no
// verdict is typed into this test.
const negativeCountGoOracle = `package probe

import (
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"testing"
)

func TestNegativeCountOracle(t *testing.T) {
	var v Box
	v.A = 7
	n := BoxMeasure(&v)
	valid := make([]byte, n)
	if got := BoxSave(&v, valid); got != n {
		t.Fatalf("save %d bytes, want %d", got, n)
	}
	fmt.Printf("valid:%s\n", hex.EncodeToString(valid))
	region := make([]byte, BoxLoadMeasure(valid))
	cases := []struct {
		name  string
		count uint64
	}{
		{"minus-one", ^uint64(0)},
		{"high-bit", uint64(1) << 63},
		{"past-end", uint64(len(valid)/8) + 1},
	}
	for _, c := range cases {
		wire := append([]byte(nil), valid...)
		binary.LittleEndian.PutUint64(wire[len(wire)-8:], c.count)
		var report TableReport
		root := BoxLoadTolerant(region, wire, &report)
		fmt.Printf("@%s|malformed=%v|value=%v\n", c.name, report.Malformed, root != nil)
	}
}
`

const negativeCountSchema = `package probe

table Box
{
    a int32
}
`

// TestLuauLoadRefusesNegativeIdCount pins docs/SPEC-TABLES.md §3's trailer
// rule: the id table's entry count is an unsigned u64, and a count the buffer
// cannot hold is malformed, never a walk whose endpos runs past the buffer.
// The same unit's Go engine produces the valid wire and the expected report
// for a count of -1, a count with its high bit set, and a count one past what
// len/8 can hold; each must agree and raise no interpreter error.
func TestLuauLoadRefusesNegativeIdCount(t *testing.T) {
	slowtest.Gate(t, "the pinned Luau interpreter and the schema-go oracle")
	t.Parallel()
	dir := t.TempDir()
	schemaPath := filepath.Join(dir, "Box.schema")
	if err := os.WriteFile(schemaPath, []byte(negativeCountSchema), 0o644); err != nil {
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
	g := &generatedUnit{dir: t.TempDir(), files: files, loaded: true}
	for name, data := range files {
		if err := os.WriteFile(filepath.Join(g.dir, name), data, 0o644); err != nil {
			t.Fatal(err)
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
	goFiles["go.mod"] = []byte(fmt.Sprintf("module probe\n\ngo 1.26\n\nrequire github.com/mas-bandwidth/serialize.go v0.0.0\nreplace github.com/mas-bandwidth/serialize.go => %q\n", runtime))
	goFiles["oracle_test.go"] = []byte(negativeCountGoOracle)
	goDir := t.TempDir()
	for name, data := range goFiles {
		if err := os.WriteFile(filepath.Join(goDir, name), data, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	cmd := exec.Command("nice", "-n", "19", "go", "test", "-count=1", "-timeout", "600s", "-run", "^TestNegativeCountOracle$", "-v", ".")
	cmd.Dir = goDir
	cmd.Env = append(os.Environ(), "GOFLAGS=-mod=readonly")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("the generated Go oracle failed: %v\n%s", err, out)
	}
	var valid []byte
	want := map[string]string{}
	for line := range strings.SplitSeq(string(out), "\n") {
		if after, ok := strings.CutPrefix(line, "valid:"); ok {
			valid, err = hex.DecodeString(after)
			if err != nil {
				t.Fatalf("the oracle's valid wire: %v", err)
			}
		}
		if after, ok := strings.CutPrefix(line, "@"); ok {
			name, rest, ok := strings.Cut(after, "|")
			if !ok {
				t.Fatalf("unexpected oracle line %q", line)
			}
			want[name] = name + "|" + rest
		}
	}
	if len(valid) < 9 || len(want) != 3 {
		t.Fatalf("the oracle printed %d wire bytes and %d cases:\n%s", len(valid), len(want), out)
	}

	cases := []struct {
		name  string
		count uint64
	}{
		{"minus-one", ^uint64(0)},
		{"high-bit", uint64(1) << 63},
		{"past-end", uint64(len(valid)/8) + 1},
	}
	inputs := map[string][]byte{}
	for _, c := range cases {
		wire := append([]byte(nil), valid...)
		binary.LittleEndian.PutUint64(wire[len(wire)-8:], c.count)
		inputs[c.name] = wire
	}

	main := `local inputs = require("./inputs")
local load = require("./schema_load")
require("./BoxTable")
for _, name in ipairs({ "minus-one", "high-bit", "past-end" }) do
	local report = { malformed = false, refused = 0, unknown = 0, kindMismatch = 0, clamped = 0, widened = 0, duplicate = 0 }
	local v = load.load("Box", inputs[name], report)
	print(name .. "|malformed=" .. tostring(report.malformed) .. "|value=" .. tostring(v ~= nil))
end
`
	if err := os.WriteFile(filepath.Join(g.dir, "main.luau"), []byte(main), 0o644); err != nil {
		t.Fatal(err)
	}
	luauOut := luaurun.Run(t, g.dir, "main.luau", inputs)
	got := map[string]string{}
	for line := range strings.SplitSeq(strings.TrimSpace(string(luauOut)), "\n") {
		if line == "" {
			continue
		}
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
