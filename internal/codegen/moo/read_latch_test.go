package moo

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/moo/moorun"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// latchSchema declares ranged integers whose ranges do not fit int32: a uint32
// range and a uint64 range past int64. Both are read as raw bits and compared
// in generated code, so a read past the range still refuses through the
// stream's one latch (docs/PORT-MOO.md, THE WIRE).
const latchSchema = `package latch

type NarrowRange
{
    r uint32 = 3000000000 | min = 3000000000, max = 4000000000
}

type WideRange
{
    r uint64 = 1 | min = 1, max = 18446744073709551614
}
`

func unitFromSource(t testing.TB, name, src string) *ir.Unit {
	t.Helper()
	path := filepath.Join(t.TempDir(), name+".schema")
	if err := os.WriteFile(path, []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	return unitFromFiles(t, []string{path})
}

// TestMooTypeWireReadRefusalLatchesTheStream requires every read that refuses
// a value out of its declared range to leave the stream refused: a caller
// testing stream:ok() must not carry on with a half-read value, exactly as a
// refusal from inside serialize.moo's stream does. The const and reserved
// rules stay unlatched, untouched here (docs/PORT-MOO.md, THE WIRE).
func TestMooTypeWireReadRefusalLatchesTheStream(t *testing.T) {
	slowtest.Gate(t, "moo (it runs the generated modules under the pinned interpreter)")
	t.Parallel()
	dir, _ := generateInto(t, unitFromSource(t, "Latch", latchSchema))
	got := strings.TrimSpace(readLatchProbe(t, dir))
	want := strings.Join([]string{
		"narrow-in read=true ok=true",
		"narrow-out read=false ok=false",
		"wide-in read=true ok=true",
		"wide-out read=false ok=false",
	}, "\n")
	if got != want {
		t.Fatalf("read refusals:\n%s\nwant:\n%s", got, want)
	}
}

// TestMooTypeWireReadRefusalLatchesTheStreamNegativeControl plants the bare
// unlatched return back into the emitted reads and requires the gate red: it
// sees a refused read whose stream still reports ok.
func TestMooTypeWireReadRefusalLatchesTheStreamNegativeControl(t *testing.T) {
	slowtest.Gate(t, "moo (it runs the generated modules under the pinned interpreter)")
	t.Parallel()
	dir, files := generateInto(t, unitFromSource(t, "Latch", latchSchema))
	latch := string(files["Latch.moo"])
	anchor := "\t\tstream:fail(\"value_out_of_range\")\n"
	if strings.Count(latch, anchor) != 2 {
		t.Fatalf("negative-control anchor matched %d times in Latch.moo", strings.Count(latch, anchor))
	}
	writeFile(t, dir, "Latch.moo", strings.ReplaceAll(latch, anchor, ""))
	got := readLatchProbe(t, dir)
	for _, red := range []string{"narrow-out read=false ok=true", "wide-out read=false ok=true"} {
		if !strings.Contains(got, red) {
			t.Fatalf("negative control: want %q in:\n%s", red, got)
		}
	}
}

// readLatchProbe writes and runs the probe: each ranged field is read once in
// range and once from all-ones bits past the range, reporting whether the read
// returned and whether the stream still reports ok.
func readLatchProbe(t testing.TB, dir string) string {
	t.Helper()
	probe := `--!strict
local serialize = require("./serialize")
local Latch = require("./Latch")

local function image(write: (any) -> boolean): buffer
	local ws = serialize.WriteStream.new(buffer.create(64))
	assert(write(ws), "write refused")
	ws:flush()
	return ws:data()
end
local function allOnes(n: number): buffer
	local b = buffer.create(n)
	for i = 0, n - 1 do buffer.writeu8(b, i, 0xFF) end
	return b
end
local function report(name: string, ok: boolean, rs: any)
	print(name .. " read=" .. tostring(ok) .. " ok=" .. tostring(rs:ok()))
end

do -- the uint32 range, in range
	local v = Latch.NewNarrowRange(); v.R = 3500000000
	local b = image(function(s) return Latch.WriteNarrowRange(s, v) end)
	local rs = serialize.ReadStream.new(b)
	report("narrow-in", Latch.ReadNarrowRange(rs, Latch.NewNarrowRange()), rs)
end

do -- the uint32 range, past max
	local rs = serialize.ReadStream.new(allOnes(4))
	report("narrow-out", Latch.ReadNarrowRange(rs, Latch.NewNarrowRange()), rs)
end

do -- the uint64 range past int64, in range
	local v = Latch.NewWideRange(); v.R = 5
	local b = image(function(s) return Latch.WriteWideRange(s, v) end)
	local rs = serialize.ReadStream.new(b)
	report("wide-in", Latch.ReadWideRange(rs, Latch.NewWideRange()), rs)
end

do -- the uint64 range past int64, past max
	local rs = serialize.ReadStream.new(allOnes(8))
	report("wide-out", Latch.ReadWideRange(rs, Latch.NewWideRange()), rs)
end
`
	writeFile(t, dir, "main.moo", probe)
	return string(moorun.Run(t, dir, "main.moo", nil))
}
