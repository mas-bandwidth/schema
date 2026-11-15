package mootable_test

// THE STRICT READ OF ONE VALUE OF A FIXED ROOT ASKS ITS LOADER FOR ONE RECORD
// (docs/FIXED-FORM-ALGORITHM.md §5.3 step 10, docs/SPEC-TABLES.md §6.5): the
// fixed loader takes the caller's capacity and refuses a file holding more
// records before it looks at any record, so the strict read of a fixed root
// passes capacity one and never builds a value for a record it will refuse.
//
// The gate is two parts. The scan holds the embedded runtime to the capacity
// one call; the run holds the verdicts, a one-record file reads and a
// two-record file refuses, so the capacity changes no answer.

import (
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/mootable"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
)

func TestMooReadStrictFixedRootAsksForOneRecord(t *testing.T) {
	slowtest.Gate(t, "moo (it runs the emitted table runtime)")
	t.Parallel()

	src, ok := mootable.Runtime()["table_refusal.moo"]
	if !ok {
		t.Fatal("the embedded runtime has no table_refusal.moo")
	}
	const call = "F.load(d, fixed, b, rep, 1)"
	if n := strings.Count(string(src), call); n != 1 {
		t.Errorf("table_refusal.moo's strict fixed read passes the loader the room for one record: found %d calls %q, want 1", n, call)
	}

	u := unitFromSource(t, "package probe\nfixed table Fx\n{\n    a uint32 = 3\n}\n")
	g := fixedUnit(t, u, nil)
	main := "--!strict\nlocal R = require(\"./table_refusal\")\nlocal F = require(\"./table_fixed\")\nlocal T = require(\"./ProbeTable\")\n" + `local a = T.NewFx()
a.A = 7
local b = T.NewFx()
b.A = 8
local one = F.save(T.FxDescriptor, T.FxFixed, { a }) :: buffer
local two = F.save(T.FxDescriptor, T.FxFixed, { a, b }) :: buffer
local function read(wire: buffer): string
	local rep: any = {}
	local v = R.ReadStrict(T, "Fx", wire, rep)
	if v == nil then
		return "refused"
	end
	return "read"
end
print(read(one))
print(read(two))
`
	if got, want := g.run(t, main, nil), "read\nrefused"; got != want {
		t.Fatalf("the strict read of a fixed root: got\n%s\nwant\n%s", got, want)
	}
}
