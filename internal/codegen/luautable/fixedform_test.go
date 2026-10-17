package luautable_test

import (
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"math/big"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/mas-bandwidth/schema/v2/compiler"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/luau"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/luau/luaurun"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/luautable"
)

// fixedCorpusVectors pairs the corpus files with the schema and root that wrote
// them (docs/FIXED-FORM-ALGORITHM.md §7), the same nine the Go leg reads.
type fixedVector struct {
	file, schema, root string
}

var fixedCorpusVectors = []fixedVector{
	{"fx1.bin", "test/tables/FX1.schema", "FxRoot"},
	{"fx2.bin", "test/tables/FX2.schema", "FxRoot"},
	{"p1.bin", "test/tables/P1.schema", "Chain"},
	{"p3.bin", "test/tables/P3.schema", "Chain"},
	{"keyed.bin", "tables/examples/Keyed.schema", "KeyedConfig"},
	{"pack.bin", "tables/examples/Pack.schema", "PackConfig"},
	{"fxw.bin", "test/tables/FXW.schema", "FxWide"},
	{"fu1.bin", "test/tables/FU1.schema", "FuRoot"},
	{"fu2.bin", "test/tables/FU2.schema", "FuRoot"},
}

const hexProbe = `local function hex(b: buffer): string
	local parts = table.create(buffer.len(b))
	for i = 0, buffer.len(b) - 1 do
		parts[i + 1] = string.format("%02x", buffer.readu8(b, i))
	end
	return table.concat(parts)
end
`

// build writes a unit's Luau modules into a fresh directory and returns the
// directory, the module basename and the files.
func build(t *testing.T, schema string) (string, string, map[string][]byte) {
	t.Helper()
	paths, err := compiler.GatherPaths([]string{filepath.Join(root, schema)})
	if err != nil {
		t.Fatal(err)
	}
	u, err := compiler.New().Load(paths)
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
	for k, v := range tables {
		files[k] = v
	}
	dir := t.TempDir()
	for name, data := range files {
		if err := os.WriteFile(filepath.Join(dir, name), data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir, strings.TrimSuffix(filepath.Base(schema), ".schema") + "Table", files
}

func fixedVars(root string) string {
	return fmt.Sprintf(`local fixed = { hash = T.%[1]sFixedHash, recordBytes = T.%[1]sFixedRecordBytes, layout = T.%[1]sFixedLayout }`, root)
}

// TestLuauFixedFormAgreesWithTheByteOracle writes every fixed corpus root from
// the value its own reader lands on the oracle's bytes, and requires the bytes
// back: the writer agrees with the C++ reference and the reader lands the
// oracle's values (docs/FIXED-FORM-ALGORITHM.md §7 row 1). A reader one field
// older and one newer is exercised on the FU pair: the newer reader wide-reads
// the older file, and the older reader refuses the newer file by name (§5.3).
func TestLuauFixedFormAgreesWithTheByteOracle(t *testing.T) {
	t.Parallel()
	corpus := filepath.Join(root, "build", "fixedform-corpus")
	var compared atomic.Int32
	for _, v := range fixedCorpusVectors {
		v := v
		t.Run(strings.TrimSuffix(v.file, ".bin"), func(t *testing.T) {
			t.Parallel()
			oracle, err := os.ReadFile(filepath.Join(corpus, v.file))
			if err != nil {
				if os.Getenv("SCHEMA_REQUIRE_CORPUS") == "" {
					t.Skip("the oracle is absent; SCHEMA_REQUIRE_CORPUS unset")
				}
				t.Fatalf("the oracle file %s is absent: build it with make tables-fixedform-corpus: %v", v.file, err)
			}
			dir, mod, _ := build(t, v.schema)
			main := "--!strict\nlocal T = require(\"./%s\")\nlocal F = require(\"./fixed\")\nlocal inputs = require(\"./inputs\")\n" +
				fixedVars(v.root) + "\nlocal values = F.load(T.%sDescriptor, fixed, inputs.oracle)\nlocal out = F.save(T.%sDescriptor, fixed, values)\nprint(hex(out))\n"
			main = fmt.Sprintf(main, mod, v.root, v.root)
			main = hexProbe + main
			if err := os.WriteFile(filepath.Join(dir, "main.luau"), []byte(main), 0o644); err != nil {
				t.Fatal(err)
			}
			out := luaurun.Run(t, dir, "main.luau", map[string][]byte{"oracle": oracle})
			got, err := hex.DecodeString(strings.TrimSpace(string(out)))
			if err != nil {
				t.Fatalf("the probe printed %q: %v", out, err)
			}
			if string(got) != string(oracle) {
				at := 0
				for at < len(got) || at < len(oracle) {
					var a, b byte
					if at < len(got) {
						a = got[at]
					}
					if at < len(oracle) {
						b = oracle[at]
					}
					if a != b {
						break
					}
					at++
				}
				lo := at - 8
				if lo < 0 {
					lo = 0
				}
				hi := at + 16
				if hi > len(got) {
					hi = len(got)
				}
				ohi := at + 16
				if ohi > len(oracle) {
					ohi = len(oracle)
				}
				t.Errorf("%s: differs at byte %d: got %x oracle %x (%d bytes)", v.file, at, got[lo:hi], oracle[lo:ohi], len(oracle))
			}
			compared.Add(1)
		})
	}
	t.Cleanup(func() { t.Logf("compared %d fixed roots against the byte oracle", compared.Load()) })
}

// layoutLiteral renders the oracle header's layout as Luau static data for a
// foreign (older) writer's Fixed value.
func layoutLiteral(oracle []byte) string {
	count := binary.LittleEndian.Uint32(oracle[20:24])
	var b strings.Builder
	b.WriteString("{\n")
	p := 24
	for i := 0; i < int(count); i++ {
		id := binary.LittleEndian.Uint64(oracle[p : p+8])
		kind := oracle[p+8]
		size := binary.LittleEndian.Uint32(oracle[p+9 : p+13])
		children := binary.LittleEndian.Uint32(oracle[p+13 : p+17])
		fmt.Fprintf(&b, "\t{ id = %s, kind = %d, size = %d, children = %d },\n",
			luau.IntegerLit(new(big.Int).SetUint64(id)), kind, size, children)
		p += 17
	}
	b.WriteString("}")
	return b.String()
}

func fixedLiteral(oracle []byte) string {
	hash := binary.LittleEndian.Uint64(oracle[8:16])
	recordBytes := binary.LittleEndian.Uint32(oracle[20+4+9 : 20+4+13])
	return fmt.Sprintf("{ hash = %s, recordBytes = %d, layout = %s }",
		luau.IntegerLit(new(big.Int).SetUint64(hash)), recordBytes, layoutLiteral(oracle))
}

// TestLuauFixedFormVersioning pins the §3.4 invariant: a reader one appended
// field newer reads the older writer's record, widening int16->int32 and
// f32->f64, and writes the older writer's bytes back (appended fields absent
// from the plan); the older reader refuses the newer file as layout_newer.
func TestLuauFixedFormVersioning(t *testing.T) {
	t.Parallel()
	corpus := filepath.Join(root, "build", "fixedform-corpus")
	oldOracle, err := os.ReadFile(filepath.Join(corpus, "fu1.bin"))
	if err != nil {
		t.Fatalf("the FU1 oracle is absent: make tables-fixedform-corpus: %v", err)
	}
	newOracle, err := os.ReadFile(filepath.Join(corpus, "fu2.bin"))
	if err != nil {
		t.Fatalf("the FU2 oracle is absent: make tables-fixedform-corpus: %v", err)
	}

	dir, mod, _ := build(t, "test/tables/FU2.schema")
	main := hexProbe + fmt.Sprintf(`--!strict
local T = require("./%s")
local F = require("./fixed")
local inputs = require("./inputs")
local foreign = %s
local values = F.load(T.FuRootDescriptor, %s, inputs.old, foreign)
local out = F.save(T.FuRootDescriptor, foreign, values)
print(hex(out))
`, mod, fixedLiteral(oldOracle), fmt.Sprintf("{ hash = T.FuRootFixedHash, recordBytes = T.FuRootFixedRecordBytes, layout = T.FuRootFixedLayout }"))
	if err := os.WriteFile(filepath.Join(dir, "main.luau"), []byte(main), 0o644); err != nil {
		t.Fatal(err)
	}
	out := luaurun.Run(t, dir, "main.luau", map[string][]byte{"old": oldOracle})
	got, err := hex.DecodeString(strings.TrimSpace(string(out)))
	if err != nil {
		t.Fatalf("the newer probe printed %q: %v", out, err)
	}
	if string(got) != string(oldOracle) {
		at := 0
		for at < len(got) || at < len(oldOracle) {
			var a, b byte
			if at < len(got) {
				a = got[at]
			}
			if at < len(oldOracle) {
				b = oldOracle[at]
			}
			if a != b {
				break
			}
			at++
		}
		t.Errorf("the newer reader did not read the older record back to its bytes (first diff %d)", at)
	}

	dir, mod, _ = build(t, "test/tables/FU1.schema")
	main = fmt.Sprintf(`--!strict
local T = require("./%s")
local F = require("./fixed")
local inputs = require("./inputs")
local ok, err = pcall(function()
	return F.load(T.FuRootDescriptor, %s, inputs.new)
end)
if ok then print("read") elseif type(err) == "table" then print(err.name) else print("error") end
`, mod, fmt.Sprintf("{ hash = T.FuRootFixedHash, recordBytes = T.FuRootFixedRecordBytes, layout = T.FuRootFixedLayout }"))
	if err := os.WriteFile(filepath.Join(dir, "main.luau"), []byte(main), 0o644); err != nil {
		t.Fatal(err)
	}
	out = luaurun.Run(t, dir, "main.luau", map[string][]byte{"new": newOracle})
	if strings.TrimSpace(string(out)) != "layout_newer" {
		t.Errorf("the older reader did not refuse the newer file as layout_newer: %q", out)
	}
}

// TestLuauFixedFormRefusals reaches each named refusal with one forged layout
// (docs/FIXED-FORM-ALGORITHM.md §1.1, §4.2; docs/SPEC-TABLES.md §3).
func TestLuauFixedFormRefusals(t *testing.T) {
	t.Parallel()
	corpus := filepath.Join(root, "build", "fixedform-corpus")
	oracle, err := os.ReadFile(filepath.Join(corpus, "fx1.bin"))
	if err != nil {
		t.Fatalf("the FX1 oracle is absent: make tables-fixedform-corpus: %v", err)
	}
	forged := map[string]struct {
		file []byte
		fix  string
	}{
		"previous_form": {func() []byte { b := append([]byte(nil), oracle...); b[0] = 1; return b }(),
			"{ hash = T.FxRootFixedHash, recordBytes = T.FxRootFixedRecordBytes, layout = T.FxRootFixedLayout }"},
		"layout_malformed": {oracle,
			"{ hash = T.FxRootFixedHash, recordBytes = T.FxRootFixedRecordBytes, layout = {} }"},
		"layout_record_too_large": {oracle,
			`{ hash = T.FxRootFixedHash, recordBytes = T.FxRootFixedRecordBytes, layout = { { id = 0i, kind = 13, size = 70000, children = 0 } } }`},
		"plan_too_large": {oracle, planTooLarge},
	}
	for name, f := range forged {
		f := f
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			dir, mod, _ := build(t, "test/tables/FX1.schema")
			main := fmt.Sprintf(`--!strict
local T = require("./%s")
local F = require("./fixed")
local inputs = require("./inputs")
local forged = %s
local ok, err = pcall(function()
	return F.load(T.FxRootDescriptor, forged, inputs.oracle, forged)
end)
if ok then print("read") elseif type(err) == "table" then print(err.name) else print("error") end
`, mod, f.fix)
			if err := os.WriteFile(filepath.Join(dir, "main.luau"), []byte(main), 0o644); err != nil {
				t.Fatal(err)
			}
			out := luaurun.Run(t, dir, "main.luau", map[string][]byte{"oracle": f.file})
			if got := strings.TrimSpace(string(out)); got != name {
				t.Errorf("the forged layout reached %q, want %q", got, name)
			}
		})
	}
}

var planTooLarge = func() string {
	var b strings.Builder
	b.WriteString("{ hash = T.FxRootFixedHash, recordBytes = T.FxRootFixedRecordBytes, layout = { { id = 0i, kind = 13, size = 0, children = 0 }")
	for i := 0; i < 4097; i++ {
		b.WriteString(", { id = 0i, kind = 4, size = 4, children = 0 }")
	}
	b.WriteString(" } }")
	return b.String()
}()

// TestLuauFixedFormNegativeControl patches the emitted runtime in memory at one
// anchor, matched exactly once, so the writer skips each record's last field;
// the oracle comparison must then go red.
func TestLuauFixedFormNegativeControl(t *testing.T) {
	t.Parallel()
	corpus := filepath.Join(root, "build", "fixedform-corpus")
	oracle, err := os.ReadFile(filepath.Join(corpus, "fx1.bin"))
	if err != nil {
		t.Fatalf("the FX1 oracle is absent: make tables-fixedform-corpus: %v", err)
	}
	dir, mod, files := build(t, "test/tables/FX1.schema")
	anchor := "-- FIXED-WRITE-ANCHOR\n\tfor _, f in d.fields do\n\t\tlocal c = childById(n, f.id)"
	runtime := string(files["fixed.luau"])
	if strings.Count(runtime, anchor) != 1 {
		t.Fatalf("the negative control's anchor appears %d times, want exactly once", strings.Count(runtime, anchor))
	}
	patched := strings.Replace(runtime, anchor,
		"-- FIXED-WRITE-ANCHOR\n\tfor i, f in d.fields do\n\t\tif i == #d.fields then continue end\n\t\tlocal c = childById(n, f.id)", 1)
	if err := os.WriteFile(filepath.Join(dir, "fixed.luau"), []byte(patched), 0o644); err != nil {
		t.Fatal(err)
	}
	main := hexProbe + fmt.Sprintf(`--!strict
local T = require("./%s")
local F = require("./fixed")
local inputs = require("./inputs")
local fixed = %s
local values = F.load(T.FxRootDescriptor, fixed, inputs.oracle)
local out = F.save(T.FxRootDescriptor, fixed, values)
print(hex(out))
`, mod, fmt.Sprintf("{ hash = T.FxRootFixedHash, recordBytes = T.FxRootFixedRecordBytes, layout = T.FxRootFixedLayout }"))
	if err := os.WriteFile(filepath.Join(dir, "main.luau"), []byte(main), 0o644); err != nil {
		t.Fatal(err)
	}
	out := luaurun.Run(t, dir, "main.luau", map[string][]byte{"oracle": oracle})
	got, err := hex.DecodeString(strings.TrimSpace(string(out)))
	if err != nil {
		t.Fatalf("the patched probe printed %q: %v", out, err)
	}
	if string(got) == string(oracle) {
		t.Fatal("the negative control stayed green: skipping the last field must move the oracle bytes")
	}
}
