package luatable

import (
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/lua/luarun"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
)

// fuzzPinBlobSharedCase is one wire-fuzz divergence class: a mutant whose
// graph names one blob from two slots, which docs/SPEC-TABLES.md §2.5 makes
// `blob_shared` and §16.7 says every writer refuses by name.
type fuzzPinBlobSharedCase struct {
	unit, root, caseName string
	seed                 uint64
	wire                 string
}

// The classes TestLuaWireFuzzAgreesWithTheGoEngine reports at the unchanged
// base, one seed each (SCHEMA_SLOW=1 SCHEMA_LUA_FUZZ_DIVERGENCES names them).
var fuzzPinBlobSharedCases = []fuzzPinBlobSharedCase{
	{
		unit: "blobdemo", root: "Catalog", caseName: "blob_shared",
		seed: 0xf5e118f73bb8f76d,
		wire: "01010c047477696e021102031102040c070105041020304000861b638eba01bcc403b2f40f72193b61b746a665bb209250ffffffffffffffffe44f1c4f47c02e2f0500000000000000",
	},
	{
		unit: "mapdemo", root: "Docs", caseName: "blob_shared_string",
		seed: 0x73fe87f2b42fb7a8,
		wire: "01010e140d0208020c01610311020008020c016203110200040c09010506736861726564009f76486aa7da01abec105b36194ac93dea0ce83094fde47cffffffffffffffff58fcaffad8e04b700500000000000000",
	},
}

// TestLuaWireFuzzPinnedBlobShared pins the two blob_shared divergence classes
// of the wire-fuzz differential: a mutant that names one blob record from two
// slots (§2.5), which every writer refuses by name (§16.7). The Lua save and
// the compiler's engine must agree — both refuse — so the verdict is "".
func TestLuaWireFuzzPinnedBlobShared(t *testing.T) {
	t.Parallel()
	repo := filepath.Join("..", "..", "..")
	manifest := filepath.Join(repo, "testdata", "conformance", "tables", "MANIFEST.txt")
	units := manifestUnits(t, manifest)
	for _, tc := range fuzzPinBlobSharedCases {
		tc := tc
		t.Run(tc.caseName, func(t *testing.T) {
			t.Parallel()
			paths, ok := units[tc.unit]
			if !ok {
				t.Fatalf("the manifest declares no unit %s", tc.unit)
			}
			var full []string
			for _, p := range paths {
				full = append(full, filepath.Join(repo, p))
			}
			u := luarun.LoadUnit(t, full...)
			files, err := Generate(u)
			if err != nil {
				t.Fatalf("unit %s: %v", tc.unit, err)
			}
			dir := t.TempDir()
			luarun.WriteFiles(t, dir, files)
			if err := os.WriteFile(filepath.Join(dir, "main.lua"), []byte(luaFuzzProbe(ModuleName(u), tc.root)), 0o644); err != nil {
				t.Fatal(err)
			}
			wire, err := hex.DecodeString(tc.wire)
			if err != nil {
				t.Fatalf("seed %#016x mutant: %v", tc.seed, err)
			}
			mu := luaFuzzMutant{caseName: tc.caseName, seed: tc.seed, wire: wire}
			if err := os.WriteFile(filepath.Join(dir, "wires.bin"), luaFuzzWiresFile([]luaFuzzMutant{mu}), 0o644); err != nil {
				t.Fatal(err)
			}
			out, err := luaFuzzRun(luarun.Interpreter(t), dir, "wires.bin")
			if err != nil {
				t.Fatalf("unit %s root %s case %s: %v", tc.unit, tc.root, tc.caseName, err)
			}
			lines := strings.Split(strings.TrimRight(string(out), "\n"), "\n")
			if len(lines) != 1 {
				t.Fatalf("the probe answered %d lines for one mutant of %s: %q", len(lines), tc.caseName, out)
			}
			ans, err := parseLuaFuzzLine(lines[0])
			if err != nil {
				t.Fatalf("unit %s root %s: %v", tc.unit, tc.root, err)
			}
			m := tabletext.NewModel(u)
			def := m.Lookup(tc.root)
			if def == nil {
				t.Fatalf("unit %s declares no table %s", tc.unit, tc.root)
			}
			o := luaFuzzEngine(m, def, wire)
			if v := luaFuzzVerdict(ans, o); v != "" {
				t.Fatalf("seed %#016x unit %s root %s case %s: %s\n    mutant %x",
					tc.seed, tc.unit, tc.root, tc.caseName, v, wire)
			}
		})
	}
}
