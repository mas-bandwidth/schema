// TestLuaGeneratesWideKinds: the lua target emits the fixed-point and 128-bit
// table kinds instead of refusing them (docs/SPEC-TABLES.md §3, §15): the
// fixed form names no kind, so the wide scalars are their declared storage
// images, and the message form carries their announced packing.
package compiler

import (
	"bytes"
	"fmt"
	"maps"
	"slices"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/luatable"
)

func TestLuaGeneratesWideKinds(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ name, src, typename string }{
		{"fixed-point", "package probe\ntable T\n{\n    q fixed(16, 16) | min = -1, max = 1\n}\n", "fixed(16, 16)"},
		{"int128", "package probe\ntable T\n{\n    i int128 | min = -1000, max = 1000\n}\n", "int128"},
		{"uint128", "package probe\ntable T\n{\n    u uint128\n}\n", "uint128"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			u := unitFromSource(t, tc.src)
			files, err := luaGenerate(u, nil)
			if err != nil {
				t.Fatalf("lua refused %s: %v", tc.name, err)
			}
			var module []byte
			for name, data := range files {
				if bytes.Contains(data, []byte("_M.Tables = {}")) {
					module = data
					_ = name
				}
			}
			if module == nil {
				t.Fatalf("lua emitted no table module for %s", tc.name)
			}
			if !bytes.Contains(module, []byte(tc.typename)) {
				t.Errorf("the table module does not name %s", tc.typename)
			}
		})
	}
}

// THE LUA ZERO-COST GATE (docs/PORTING.md I10; docs/SPEC-TABLES.md §2.2).
// Adding a table to a unit changes not one byte of the non-table generated
// code, and grows only the table sources: the unit's table module, its view
// (§8.2) and the table runtime beside them, a runtime file per form that a
// consumer requires only if it uses the form (block, cook). A table-free unit
// emits none of them. And the unit's table module carries no symbol of either
// accelerator, so a unit that never opens a block or a cook never loads one.
//
// The twin of TestTablesMoveNoGeneratedPacketByte (C++), the Go and Java ones:
// packetSrc is the packet-only unit and tableSrc is the same unit with one
// table added and no other declaration, so the comparison is like with like.

// luaZeroCostFindings compares the files a unit emits with a table (with) and
// without one (without). tableFiles names, by the module's basename, the files
// a table may add.
func luaZeroCostFindings(with, without map[string][]byte, grown func(name string) bool) []string {
	var out []string
	for _, name := range slices.Sorted(maps.Keys(without)) {
		got, ok := with[name]
		if !ok {
			out = append(out, fmt.Sprintf("file %s disappeared when a table was added", name))
			continue
		}
		if !bytes.Equal(got, without[name]) {
			out = append(out, fmt.Sprintf("file %s changed when a table was added: tables must move no packet byte", name))
		}
	}
	for _, name := range slices.Sorted(maps.Keys(with)) {
		if _, ok := without[name]; !ok && !grown(name) {
			out = append(out, fmt.Sprintf("adding a table grew the unexpected non-table file %s", name))
		}
	}
	return out
}

// luaTableSource reports whether a file is one a table is allowed to add: the
// unit's table module and view, and the runtime modules the table backend
// embeds, which the backend names by the glob of its own runtime directory.
func luaTableSource(t *testing.T, unitBase string) func(string) bool {
	t.Helper()
	runtime, err := luatable.Generate(unitFromSource(t, tableSrc))
	if err != nil {
		t.Fatal(err)
	}
	return func(name string) bool {
		if _, ok := runtime[name]; ok {
			return true
		}
		return name == unitBase+"Tables.lua" || name == unitBase+"View.lua"
	}
}

func TestLuaTablesMoveNoGeneratedPacketByte(t *testing.T) {
	t.Parallel()
	with, err := luaGenerate(unitFromSource(t, tableSrc), nil)
	if err != nil {
		t.Fatal(err)
	}
	without, err := luaGenerate(unitFromSource(t, packetSrc), nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(without) == 0 {
		t.Fatal("the packet-only unit emitted nothing: the comparison is empty")
	}
	for _, f := range luaZeroCostFindings(with, without, luaTableSource(t, "Probe")) {
		t.Error(f)
	}
	// a table-free unit emits none of the table sources
	for name := range without {
		if strings.HasSuffix(name, "Tables.lua") || strings.HasSuffix(name, "View.lua") {
			t.Errorf("a table-free unit emitted %s", name)
		}
		for _, runtimeFile := range slices.Sorted(maps.Keys(mustLuaRuntime(t))) {
			if name == runtimeFile {
				t.Errorf("a table-free unit emitted the table runtime file %s", name)
			}
		}
	}
	// no table source spells either accelerator: the unit's table module never
	// names the block or the cook runtime, their entry points or their module
	// tables, so a unit that never reads either never loads either
	module := string(with["ProbeTables.lua"])
	if module == "" {
		t.Fatal("the unit emitted no ProbeTables.lua")
	}
	for _, banned := range []string{"BlockOpen", "BlockBytes", "BlockMaxBytes", "CookMeasure", "Cook(", "C.Check", `require("block`, `require("cook`, "uncook"} {
		if strings.Contains(module, banned) {
			t.Errorf("the unit's table module spells the accelerator symbol %s", banned)
		}
	}
}

func mustLuaRuntime(t *testing.T) map[string][]byte {
	t.Helper()
	files, err := luatable.Generate(unitFromSource(t, tableSrc))
	if err != nil {
		t.Fatal(err)
	}
	return files
}

// TestLuaZeroCostGateGoesRed is the gate's NEGATIVE CONTROL (the twin of
// TestZeroCostForValueOnlyTables): the comparison is only worth running if it
// reports a table that moves a packet byte, a packet file that vanishes, and a
// non-table file that appears, so each is planted in a copy of the unit's own
// output and the comparison must name it.
func TestLuaZeroCostGateGoesRed(t *testing.T) {
	t.Parallel()
	with, err := luaGenerate(unitFromSource(t, tableSrc), nil)
	if err != nil {
		t.Fatal(err)
	}
	without, err := luaGenerate(unitFromSource(t, packetSrc), nil)
	if err != nil {
		t.Fatal(err)
	}
	grown := luaTableSource(t, "Probe")
	if got := luaZeroCostFindings(with, without, grown); len(got) != 0 {
		t.Fatalf("the unplanted comparison is red, so the control proves nothing: %v", got)
	}
	moved := maps.Clone(with)
	moved["Probe.lua"] = append(bytes.Clone(moved["Probe.lua"]), "-- a packet byte a table moved\n"...)
	if got := luaZeroCostFindings(moved, without, grown); len(got) != 1 || !strings.Contains(got[0], "Probe.lua changed") {
		t.Errorf("a packet file a table changed was not reported: %v", got)
	}
	gone := maps.Clone(with)
	delete(gone, "Probe.lua")
	if got := luaZeroCostFindings(gone, without, grown); len(got) != 1 || !strings.Contains(got[0], "Probe.lua disappeared") {
		t.Errorf("a packet file a table removed was not reported: %v", got)
	}
	extra := maps.Clone(with)
	extra["Stray.lua"] = []byte("return {}\n")
	if got := luaZeroCostFindings(extra, without, grown); len(got) != 1 || !strings.Contains(got[0], "Stray.lua") {
		t.Errorf("a non-table file a table grew was not reported: %v", got)
	}
}
