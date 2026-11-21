package luautable_test

import (
	"encoding/binary"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/compiler"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/luau"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/luau/luaurun"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/luautable"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
)

// Ranged integers at the edges of their kinds: negative bases at 8, 32 and
// 64 bits (the signed kinds' LEB base is zigzagged), and bases past 2^53 at
// the int64 and uint64 tops, where a double cannot hold the base.
const luauRangedBaseSchema = `package probe

fixed table Edge
{
    a int8   = -121 | min = -128, max = -121
    w int32  = -2147483641 | min = -2147483648, max = -2147483641
    q int64  = -9223372036854775801 | min = -9223372036854775808, max = -9223372036854775801
    p int64  = 9223372036854775800 | min = 9223372036854775800, max = 9223372036854775807
    r uint64 = 18446744073709551608 | min = 18446744073709551608, max = 18446744073709551615
}
`

const luauRangedBaseProbe = `local inputs = require("./inputs")
require("./ProbeTable")
local M = require("./schema_message")
local St = require("./schema_tables")
local voc = M.NewVocabulary()
assert(M.AnnounceRead(voc, inputs.announce, {}), "announce")
local Edge = St.lookup("Edge")
local function hex(b: buffer?): string
	if b == nil then return "nil" end
	local parts = table.create(buffer.len(b))
	for i = 0, buffer.len(b) - 1 do parts[i + 1] = string.format("%02x", buffer.readu8(b, i)) end
	return table.concat(parts)
end
local function row(label: string, a: number, w: number, wide: buffer)
	local v: any = Edge.new()
	v.A = a
	v.W = w
	v.Q = buffer.readinteger(wide, 0)
	v.P = buffer.readinteger(wide, 8)
	v.R = buffer.readinteger(wide, 16)
	local r: any = {}
	local ok, b = pcall(M.SaveMessages, voc, "Edge", { v }, r)
	print(label .. " " .. (if ok then hex(b) else "raised"))
end
row("min", -128, -2147483648, inputs.min)
row("low", -127, -2147483647, inputs.low)
row("top", -122, -2147483642, inputs.top)
`

// TestLuauMessageWriteRangedBaseExact pins the Luau message writer's ranged
// offset (docs/SPEC-TABLES.md §3.3: the wire carries value - base in the
// announced bits) at the edges of every kind width: a signed kind's base is
// the zigzag-decoded value, never the raw LEB word, and a 64-bit base is
// exact past 2^53. Each batch is compared byte for byte with the Go engine's
// encoding of the same value (tablewire.EncodeMessages).
func TestLuauMessageWriteRangedBaseExact(t *testing.T) {
	t.Parallel()
	p := filepath.Join(t.TempDir(), "Probe.schema")
	if err := os.WriteFile(p, []byte(luauRangedBaseSchema), 0o644); err != nil {
		t.Fatal(err)
	}
	u, err := compiler.New().Load([]string{p})
	if err != nil {
		t.Fatalf("the unit does not load: %v", err)
	}
	dir := t.TempDir()
	for _, gen := range []func() (map[string][]byte, error){
		func() (map[string][]byte, error) { return luau.Generate(u) },
		func() (map[string][]byte, error) { return luautable.Generate(u) },
	} {
		files, err := gen()
		if err != nil {
			t.Fatal(err)
		}
		for name, data := range files {
			if err := os.WriteFile(filepath.Join(dir, name), data, 0o644); err != nil {
				t.Fatal(err)
			}
		}
	}
	if err := os.WriteFile(filepath.Join(dir, "main.luau"), []byte(luauRangedBaseProbe), 0o644); err != nil {
		t.Fatal(err)
	}
	model := tabletext.NewModel(u)
	oracle := func(text string) string {
		inst := model.New(model.Lookup("Edge"))
		var report tabletext.Report
		if !model.Read(inst, []byte(text), &report) || !report.Silent() {
			t.Fatalf("oracle value %s: %+v", text, report)
		}
		b, err := tablewire.EncodeMessages(model, []*tabletext.Instance{inst})
		if err != nil {
			t.Fatal(err)
		}
		return hex.EncodeToString(b)
	}
	wide := func(q int64, p int64, r uint64) []byte {
		b := make([]byte, 24)
		binary.LittleEndian.PutUint64(b[0:], uint64(q))
		binary.LittleEndian.PutUint64(b[8:], uint64(p))
		binary.LittleEndian.PutUint64(b[16:], r)
		return b
	}
	rows := []struct{ label, json string }{
		{"min", `{"a":-128,"w":-2147483648,"q":-9223372036854775808,"p":9223372036854775807,"r":18446744073709551615}`},
		{"low", `{"a":-127,"w":-2147483647,"q":-9223372036854775807,"p":9223372036854775801,"r":18446744073709551609}`},
		{"top", `{"a":-122,"w":-2147483642,"q":-9223372036854775802,"p":9223372036854775806,"r":18446744073709551614}`},
	}
	raw := luaurun.Run(t, dir, "main.luau", map[string][]byte{
		"announce": tablewire.Announce(u),
		"min":      wide(-9223372036854775808, 9223372036854775807, 18446744073709551615),
		"low":      wide(-9223372036854775807, 9223372036854775801, 18446744073709551609),
		"top":      wide(-9223372036854775802, 9223372036854775806, 18446744073709551614),
	})
	got := map[string]string{}
	for line := range strings.SplitSeq(strings.TrimSpace(string(raw)), "\n") {
		k, v, _ := strings.Cut(line, " ")
		got[k] = v
	}
	for _, r := range rows {
		if want := oracle(r.json); got[r.label] != want {
			t.Errorf("%s: Luau wrote %s, the Go engine %s", r.label, got[r.label], want)
		}
	}
}
