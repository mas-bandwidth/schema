package luautable_test

import (
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

const variableSaveChecksSchema = `package probe

table Probe {
	bounded64 int64 | min = -5, max = 5
	bounded128 int128 | min = -5, max = 5
	compressed float32 | min = 0, max = 1, resolution = 0.25
	forty bits(40)
	data bytes(4)
	name string(4)
	rules [..4]string(4)
	u32 uint32
	fixedbytes [4]uint8
	counted [1..4]uint8
}
`

type variableSaveValidCase struct {
	name, goSetup, luaSetup string
}

var variableSaveValidCases = []variableSaveValidCase{
	{"bounded64-min", `v.Bounded64 = -5`, `v.Bounded64 = -5`},
	{"bounded64-max", `v.Bounded64 = 5`, `v.Bounded64 = 5`},
	{"bounded128-min", `v.Bounded128 = serialize.Int128{Lo: ^uint64(4), Hi: ^uint64(0)}`, `v.Bounded128 = R.int128(-5i, -1i)`},
	{"bounded128-max", `v.Bounded128 = serialize.Int128{Lo: 5, Hi: 0}`, `v.Bounded128 = R.int128(5i, 0i)`},
	{"compressed-min", `v.Compressed = 0`, `v.Compressed = 0`},
	{"compressed-max", `v.Compressed = 1`, `v.Compressed = 1`},
	{"forty-max", `v.Forty = (uint64(1) << 40) - 1`, `v.Forty = 1099511627775i`},
	{"data-bound", `copy(v.Data[:], []byte("abcd")); v.DataLength = 4`, `v.Data = buffer.fromstring("abcd"); v.DataLength = 4`},
	{"name-utf8", `copy(v.Name[:], []byte("é")); v.NameLength = 2`, `v.Name = buffer.fromstring("é"); v.NameLength = 2`},
	{"rules-utf8", `if !ProbeRulesAppend(&v, "é") { t.Fatal("Go text-list append") }`, `v.RulesLength = {}; v.Rules[1] = buffer.fromstring("é"); v.RulesLength[1] = 2; v.RulesCount = 1`},
	{"u32-max", `v.U32 = 4294967295`, `v.U32 = 4294967295`},
	{"bytes-full", `v.Fixedbytes = [4]uint8{1, 2, 3, 4}`, `v.Fixedbytes = buffer.fromstring("\x01\x02\x03\x04")`},
	{"counted-max", `v.CountedCount = 4`, `v.CountedCount = 4`},
	{"counted-min", `v.CountedCount = 1`, `v.CountedCount = 1`},
}

type variableSaveInvalidCase struct {
	name, luaSetup string
}

var variableSaveInvalidCases = []variableSaveInvalidCase{
	{"bounded64-low", `v.Bounded64 = -6`},
	{"bounded64-high", `v.Bounded64 = 6`},
	{"bounded128-low", `v.Bounded128 = R.int128(-6i, -1i)`},
	{"bounded128-high", `v.Bounded128 = R.int128(6i, 0i)`},
	{"compressed-low", `v.Compressed = -0.01`},
	{"compressed-high", `v.Compressed = 1.01`},
	{"forty-wide", `v.Forty = 1099511627776i`},
	{"data-over-bound", `v.Data = buffer.fromstring("abcde"); v.DataLength = 5`},
	{"name-over-bound", `v.Name = buffer.fromstring("abcde"); v.NameLength = 5`},
	{"name-invalid-utf8", `v.Name = buffer.fromstring("\xFF"); v.NameLength = 1`},
	{"rules-invalid-utf8", `v.RulesLength = {}; v.Rules[1] = buffer.fromstring("\xFF"); v.RulesLength[1] = 1; v.RulesCount = 1`},
	{"u32-overflow", `v.U32 = 4294967296`},
	{"u32-fraction", `v.U32 = 1.5`},
	{"bytes-short", `v.Fixedbytes = buffer.create(3)`},
	{"counted-over-bound", `v.CountedCount = 5`},
	{"counted-under-bound", `v.CountedCount = 0`},
}

// TestLuauVariableSaveRefusesOffDeclaration holds the variable-form writer to
// the write contract (docs/SPEC.md §5.1): every off-declaration value refuses
// without raising, and every lawful neighbor writes the same bytes as the
// generated Go table writer.
func TestLuauVariableSaveRefusesOffDeclaration(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "the pinned Luau interpreter and Go toolchain")

	schema := filepath.Join(t.TempDir(), "Probe.schema")
	if err := os.WriteFile(schema, []byte(variableSaveChecksSchema), 0o644); err != nil {
		t.Fatal(err)
	}
	u, err := compiler.New().Load([]string{schema})
	if err != nil {
		t.Fatalf("schema: %v", err)
	}
	luauFiles, err := luau.Generate(u)
	if err != nil {
		t.Fatal(err)
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

	goDir := t.TempDir()
	goFiles["go.mod"] = fmt.Appendf(nil, "module probe\n\ngo 1.26\n\nrequire github.com/mas-bandwidth/serialize.go/v2 v2.0.0\nreplace github.com/mas-bandwidth/serialize.go/v2 => %q\n", tfSerializeGoRuntime(t))
	var oracle strings.Builder
	oracle.WriteString(`package probe
import (
  "encoding/hex"
  "fmt"
  "testing"
  serialize "github.com/mas-bandwidth/serialize.go/v2"
)
func TestVariableSaveOracle(t *testing.T) {
  t.Parallel()
`)
	for _, c := range variableSaveValidCases {
		fmt.Fprintf(&oracle, "{ var v Probe; v.CountedCount = 1; %s; n := ProbeMeasure(&v); b := make([]byte, n); if ProbeSave(&v, b) != n { t.Fatalf(\"%s: Go save failed\") }; fmt.Printf(\"@%s|%%s\\n\", hex.EncodeToString(b)) }\n", c.goSetup, c.name, c.name)
	}
	oracle.WriteString("}\n")
	goFiles["oracle_test.go"] = []byte(oracle.String())
	for name, data := range goFiles {
		if err := os.WriteFile(filepath.Join(goDir, name), data, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	cmd := exec.Command("go", "test", "-count=1", "-run", "^TestVariableSaveOracle$", "-v", ".")
	cmd.Dir = goDir
	goOut, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("generated Go oracle: %v\n%s", err, goOut)
	}
	want := map[string][]byte{}
	for line := range strings.SplitSeq(string(goOut), "\n") {
		if after, ok := strings.CutPrefix(line, "@"); ok {
			name, encoded, ok := strings.Cut(after, "|")
			if !ok {
				t.Fatalf("bad Go oracle line %q", line)
			}
			b, err := hex.DecodeString(encoded)
			if err != nil {
				t.Fatalf("Go oracle %s: %v", name, err)
			}
			want[name] = b
		}
	}
	if len(want) != len(variableSaveValidCases) {
		t.Fatalf("Go oracle returned %d valid cases, want %d:\n%s", len(want), len(variableSaveValidCases), goOut)
	}

	luauDir := t.TempDir()
	luauTable, err := luautable.Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	maps.Copy(luauFiles, luauTable)
	for name, data := range luauFiles {
		if err := os.WriteFile(filepath.Join(luauDir, name), data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	var program strings.Builder
	program.WriteString(`local P = require("./ProbeTable")
local St = require("./schema_tables")
local R = require("./schema_runtime")
local save = require("./schema_save")
local d = St.lookup("Probe")
local function hex(b)
  local a = table.create(buffer.len(b))
  for i = 0, buffer.len(b) - 1 do a[i + 1] = string.format("%02x", buffer.readu8(b, i)) end
  return table.concat(a)
end
local function run(name, setup)
  local v = P.NewProbe()
  setup(v)
  local ok, b, why = pcall(save.save, d, v)
  if not ok then print("@" .. name .. "|raised")
  elseif b == nil and why ~= nil then print("@" .. name .. "|refused")
  elseif b == nil then print("@" .. name .. "|missing refusal")
  else print("@" .. name .. "|" .. hex(b)) end
end
`)
	for _, c := range variableSaveValidCases {
		fmt.Fprintf(&program, "run(%q, function(v) v.CountedCount = 1; %s end)\n", c.name, c.luaSetup)
	}
	for _, c := range variableSaveInvalidCases {
		fmt.Fprintf(&program, "run(%q, function(v) v.CountedCount = 1; %s end)\n", c.name, c.luaSetup)
	}
	if err := os.WriteFile(filepath.Join(luauDir, "main.luau"), []byte(program.String()), 0o644); err != nil {
		t.Fatal(err)
	}
	luauOut := strings.TrimSpace(string(luaurun.Run(t, luauDir, "main.luau", nil)))
	got := map[string]string{}
	for line := range strings.SplitSeq(luauOut, "\n") {
		name, result, ok := strings.Cut(strings.TrimPrefix(line, "@"), "|")
		if !ok {
			t.Fatalf("unexpected Luau output %q", line)
		}
		got[name] = result
	}
	for _, c := range variableSaveValidCases {
		if got[c.name] != hex.EncodeToString(want[c.name]) {
			t.Errorf("%s: Luau save %q, Go oracle %x", c.name, got[c.name], want[c.name])
		}
	}
	for _, c := range variableSaveInvalidCases {
		if got[c.name] != "refused" {
			t.Errorf("%s: Luau save result %q, want refusal without raising", c.name, got[c.name])
		}
	}
}
