package luatable

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/lua/luarun"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
)

const blockEligibilityEligibleSchema = `package eligible
fixed table Row { value uint128 }
fixed table Frame { rows [..16]Row }
`

const blockEligibilitySchema = `package eligibility
fixed table Row { value uint128 }
fixed table Frame { rows [..2]Row }
table Text { labels [..2]string }
table Indirect { child *Row }
table Nested { text Text }
union Choice { number int32 }
table Overlay { choice Choice }
table Mapped { entries map[int32]Row }
`

const blockEligibilityProbe = `local T = require("EligibilityTables")
local B = require("block")
local W = require("blockbuild")
local bad = {}
local function red(s) bad[#bad + 1] = s end
local block = assert(W.Begin(T, "Frame", {rows=1}))
assert(B.BlockOpen(T, "Frame", block.bytes))
for _, root in ipairs({"Text", "Indirect", "Nested", "Overlay", "Mapped"}) do
 local ok, v, why = pcall(W.Begin, T, root, {})
 if not ok or v ~= nil or why ~= "bad_layout" then red("begin:" .. root) end
 ok, v, why = pcall(W.BlockMaxBytes, T, root)
 if not ok or v ~= nil or why ~= "bad_layout" then red("maximum:" .. root) end
 ok, v, why = pcall(B.BlockOpen, T, root, block.bytes)
 if not ok or v ~= nil or why ~= "bad_layout" then red("open:" .. root) end
 ok, v, why = pcall(B.Layout, T, root, true)
 if not ok or v ~= nil or why ~= "bad_layout" then red("layout:" .. root) end
end
table.sort(bad)
for _, s in ipairs(bad) do print(s) end
`

func blockEligibilityGate(t *testing.T, file, from, to string) []string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "Eligibility.schema")
	if err := os.WriteFile(path, []byte(blockEligibilitySchema), 0o600); err != nil {
		t.Fatal(err)
	}
	u := luarun.LoadUnit(t, path)
	files, err := Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	if file != "" {
		src := string(files[file])
		if strings.Count(src, from) != 1 {
			t.Fatalf("plant anchor count in %s: %d", file, strings.Count(src, from))
		}
		files[file] = []byte(strings.Replace(src, from, to, 1))
	}
	luarun.WriteFiles(t, dir, files)
	if err := os.WriteFile(filepath.Join(dir, "main.lua"), []byte(blockEligibilityProbe), 0o600); err != nil {
		t.Fatal(err)
	}
	out := strings.TrimSpace(string(luarun.Run(t, dir, "main.lua", nil)))
	if out == "" {
		return nil
	}
	return strings.Split(out, "\n")
}

func blockEligibilityFiles(t *testing.T, pkg, schema string) map[string][]byte {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, pkg+".schema")
	if err := os.WriteFile(path, []byte(schema), 0o600); err != nil {
		t.Fatal(err)
	}
	u := luarun.LoadUnit(t, path)
	files, err := Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	return files
}

// TestLuaBlockEligibilityRuntimeIsUnitDependent pins the generation seam: a
// unit whose every table is block-eligible emits block.lua and blockbuild.lua
// exactly as the carried runtime, with no per-call eligibility test, while a
// unit that carries an ineligible table emits the test before layout,
// capacity and build. The shared output directory and package.loaded cases
// below hold the two copies to the same refusal.
func TestLuaBlockEligibilityRuntimeIsUnitDependent(t *testing.T) {
	t.Parallel()
	mixed := blockEligibilityFiles(t, "Eligibility", blockEligibilitySchema)
	eligible := blockEligibilityFiles(t, "Eligible", blockEligibilityEligibleSchema)
	for _, name := range []string{"block.lua", "blockbuild.lua"} {
		if !bytes.Contains(mixed[name], []byte("block_eligible")) {
			t.Errorf("%s carries no block_eligible test for the unit that needs it", name)
		}
		if bytes.Contains(eligible[name], []byte("block_eligible")) {
			t.Errorf("%s carries the eligibility test for a unit whose every table is eligible: its hot block paths pay for a check they cannot need", name)
		}
		if bytes.Contains(eligible[name], []byte("EligibilityGuard")) {
			t.Errorf("%s marks an eligibility guard it does not carry", name)
		}
	}
	if !bytes.Contains(mixed["block.lua"], []byte("does not carry block")) {
		t.Error("the unit that needs the guard lost the forms clause")
	}
	if !bytes.Equal(mixed["descriptor.lua"], eligible["descriptor.lua"]) {
		t.Error("descriptor.lua differs between units: the shared seal that repairs a guard-free copy must be one file")
	}
	if !bytes.Contains(eligible["descriptor.lua"], []byte("install_block_eligibility")) {
		t.Error("descriptor.lua cannot repair a shared guard-free block runtime")
	}
}

// TestLuaBlockEligibilitySharedOutputFunctional reproduces a shared output
// directory: a mixed unit and an all-fixed unit are written over one another
// as consecutive CLI --out writes do, with the all-fixed unit last. The mixed
// unit must still refuse its ineligible roots.
func TestLuaBlockEligibilitySharedOutputFunctional(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "Lua 5.4 functional block eligibility across units")
	dir := t.TempDir()
	luarun.WriteFiles(t, dir, blockEligibilityFiles(t, "Eligibility", blockEligibilitySchema))
	luarun.WriteFiles(t, dir, blockEligibilityFiles(t, "Eligible", blockEligibilityEligibleSchema))
	if err := os.WriteFile(filepath.Join(dir, "main.lua"), []byte(blockEligibilityProbe), 0o600); err != nil {
		t.Fatal(err)
	}
	out := strings.TrimSpace(string(luarun.Run(t, dir, "main.lua", nil)))
	if out != "" {
		t.Fatalf("a shared output directory let an ineligible block run guard-free: %s", out)
	}
}

// TestLuaBlockEligibilityLoadOrderFunctional reproduces Lua's package.loaded
// cache: the all-fixed unit's block and blockbuild modules are loaded first,
// then the mixed unit's descriptors are added on the path. The cached runtime
// must still refuse the mixed unit's ineligible roots.
func TestLuaBlockEligibilityLoadOrderFunctional(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "Lua 5.4 functional block eligibility load order")
	pureDir := t.TempDir()
	mixedDir := t.TempDir()
	luarun.WriteFiles(t, pureDir, blockEligibilityFiles(t, "Eligible", blockEligibilityEligibleSchema))
	luarun.WriteFiles(t, mixedDir, blockEligibilityFiles(t, "Eligibility", blockEligibilitySchema))
	main := fmt.Sprintf(`local _pure = require("EligibleTables")
local _B = require("block")
local _W = require("blockbuild")
package.path = %q .. "/?.lua;" .. package.path
`, mixedDir) + blockEligibilityProbe
	if err := os.WriteFile(filepath.Join(pureDir, "main.lua"), []byte(main), 0o600); err != nil {
		t.Fatal(err)
	}
	out := strings.TrimSpace(string(luarun.Run(t, pureDir, "main.lua", nil)))
	if out != "" {
		t.Fatalf("a cached eligible runtime let an ineligible block run guard-free: %s", out)
	}
}

func TestLuaBlockEligibilityFunctional(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "Lua 5.4 functional block eligibility")
	if got := blockEligibilityGate(t, "", "", ""); len(got) != 0 {
		t.Fatalf("ineligible block accepted: %v", got)
	}
}

func TestLuaBlockEligibilityNegativeControlFunctional(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "Lua 5.4 functional block eligibility controls")
	for _, tc := range []struct{ file, anchor, replacement, clause string }{
		{"blockbuild.lua", "if desc.block_eligible ~= true then return nil, \"bad_layout\" end -- BlockMaxBytes", "if false then return nil, \"bad_layout\" end -- BlockMaxBytes", "maximum"},
		{"blockbuild.lua", "if desc ~= nil and desc.block_eligible ~= true then return nil, \"bad_layout\" end -- Begin", "if false then return nil, \"bad_layout\" end -- Begin", "begin"},
		{"block.lua", "if bd ~= nil and bd.block_eligible ~= true then return nil, \"bad_layout\" end", "if false then return nil, \"bad_layout\" end", "open"},
		{"block.lua", "if projection and d ~= nil and d.block_eligible ~= true then return nil, \"bad_layout\" end", "if false then return nil, \"bad_layout\" end", "layout"},
	} {
		t.Run(tc.clause, func(t *testing.T) {
			t.Parallel()
			got := blockEligibilityGate(t, tc.file, tc.anchor, tc.replacement)
			// Only this entry point may go red. Some bypasses fail later with a
			// layout error; that still cannot masquerade as the specified refusal.
			want := []string{}
			for _, root := range []string{"Indirect", "Mapped", "Nested", "Overlay", "Text"} {
				want = append(want, tc.clause+":"+root)
			}
			if strings.Join(got, "\n") != strings.Join(want, "\n") {
				t.Fatalf("wrong plant failure: got %v, want %v", got, want)
			}
			t.Log(fmt.Sprintf("guard bypass caught: %v", got))
		})
	}
}
