package lua

import (
	"fmt"
	"math/big"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/lua/luarun"
)

// TestLuaTypeWireWideFixedRoundTrip pins the 128-bit fixed/ufixed
// round-trip: the reader must hand stream:serializeFixed the whole-unit
// bounds in the form its runtime takes (plain integers), not raw { lo, hi }
// scalars, or Read<T> refuses Write<T>'s own output as value_out_of_range
// (SPEC §4.3, §4.6).
func TestLuaTypeWireWideFixedRoundTrip(t *testing.T) {
	t.Parallel()
	// I + F must equal a storage width (SPEC §4.6): 70 + 58 and 100 + 28
	// both make the 128-bit storage this case pins.
	schema := []byte(`package widefixed
type WideFixedRT
{
    s fixed(70, 58) | min = -1000, max = 1000
    u ufixed(100, 28) | min = 0, max = 100000
}
`)
	path := filepath.Join(t.TempDir(), "WideFixed.schema")
	if err := os.WriteFile(path, schema, 0o644); err != nil {
		t.Fatal(err)
	}
	u := luarun.LoadUnit(t, path)
	files, err := Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	luarun.WriteFiles(t, dir, files)

	var probe strings.Builder
	probe.WriteString("local serialize = require(\"serialize\")\n")
	probe.WriteString("local M = require(\"WideFixed\")\n")
	probe.WriteString("local function check(name, sval, uval)\n")
	probe.WriteString("  local v = M.NewWideFixedRT()\n")
	probe.WriteString("  v.S = sval\n")
	probe.WriteString("  v.U = uval\n")
	probe.WriteString("  local w = serialize.WriteStream.new(512)\n")
	probe.WriteString("  if not M.WriteWideFixedRT(w, v) then error(name .. \" write failed\") end\n")
	probe.WriteString("  w:flush()\n")
	probe.WriteString("  local r = serialize.ReadStream.new(w:data())\n")
	probe.WriteString("  local g = M.NewWideFixedRT()\n")
	probe.WriteString("  if not M.ReadWideFixedRT(r, g) then error(name .. \" read refused\") end\n")
	probe.WriteString("  if g.S.lo ~= sval.lo or g.S.hi ~= sval.hi or g.U.lo ~= uval.lo or g.U.hi ~= uval.hi then error(name .. \" mismatch\") end\n")
	probe.WriteString("  print(name .. \" OK\")\n")
	probe.WriteString("end\n")

	scenarios := []struct {
		name string
		s, u int64
	}{
		{"min", -1000, 0},
		{"mid", 0, 50000},
		{"max", 1000, 100000},
	}
	for _, sc := range scenarios {
		sRaw := Int128Literal(new(big.Int).Lsh(big.NewInt(sc.s), 58))
		uRaw := Int128Literal(new(big.Int).Lsh(big.NewInt(sc.u), 28))
		fmt.Fprintf(&probe, "check(%q, %s, %s)\n", sc.name, sRaw, uRaw)
	}

	if err := os.WriteFile(filepath.Join(dir, "main.lua"), []byte(probe.String()), 0o644); err != nil {
		t.Fatal(err)
	}
	out := luarun.Run(t, dir, "main.lua", nil)
	for _, sc := range scenarios {
		if !strings.Contains(string(out), sc.name+" OK") {
			t.Fatalf("wide fixed round trip %s failed:\n%s", sc.name, out)
		}
	}
}
