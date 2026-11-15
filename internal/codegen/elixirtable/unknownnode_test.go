package elixirtable

// A NODE RECORD WHOSE TYPE ID THIS READER CANNOT NAME KEEPS ITS INDEX, IS
// COUNTED UNKNOWN ONCE, AND EVERY POINTER THAT NAMES IT READS NULL
// (docs/SPEC-TABLES.md §3.1). The load is accepted: a newer writer's node type
// under an existing pointer is the forward-compatibility case. A node record of
// a type the reader DOES name, but not the type the pointer field declares,
// stays a refusal.
//
// The wires are the Go oracle's (internal/tablewire): every row's verdict is
// taken from tablewire.Decode on the same bytes before the generated Elixir is
// asked. It compiles and runs the generated unit with the real Elixir
// toolchain, so it is a functional test gated by slowtest.

import (
	"encoding/hex"
	"fmt"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
)

const unnamedNodeReaderSchema = `package unnoderead

table Leaf
{
    id uint32
}

table Other
{
    id uint32
}

union Slot
{
    leaf *Leaf
}

table Root
{
    head   *Leaf
    ptrs   []*Leaf
    choice Slot
    other  *Other
}
`

const unnamedNodeWriterPointer = `package unnodewrite

table Future
{
    id uint32
}

table Root
{
    head *Future
}
`

const unnamedNodeWriterArray = `package unnodewrite

table Future
{
    id uint32
}

table Root
{
    ptrs []*Future
}
`

const unnamedNodeWriterUnion = `package unnodewrite

table Future
{
    id uint32
}

union Slot
{
    leaf *Future
}

table Root
{
    choice Slot
}
`

const unnamedNodeWriterWrongType = `package unnodewrite

table Other
{
    id uint32
}

table Root
{
    head *Other
}
`

type unnamedNodeRow struct {
	name  string
	wire  []byte
	want  bool
	check string
}

func unnamedNodeWire(t *testing.T, src, text string) []byte {
	t.Helper()
	return cellsJSONWire(t, unitFrom(t, src), text)
}

func TestElixirPointerToUnnamedNodeTypeReadsNull(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "the Elixir toolchain")

	reader := unitFrom(t, unnamedNodeReaderSchema)
	rows := []unnamedNodeRow{
		{
			name:  "pointer-field",
			wire:  unnamedNodeWire(t, unnamedNodeWriterPointer, `{"head":{"id":7}}`),
			want:  true,
			check: `v["head"] == nil`,
		},
		{
			name:  "pointer-array-element",
			wire:  unnamedNodeWire(t, unnamedNodeWriterArray, `{"ptrs":[{"id":7}]}`),
			want:  true,
			check: `v["ptrs"] == [nil]`,
		},
		{
			name:  "union-arm-pointer",
			wire:  unnamedNodeWire(t, unnamedNodeWriterUnion, `{"choice":{"leaf":{"id":7}}}`),
			want:  true,
			check: `v["choice"] == {"leaf", nil}`,
		},
		{
			name: "named-record-of-another-type",
			wire: unnamedNodeWire(t, unnamedNodeWriterWrongType, `{"head":{"id":7}}`),
			want: false,
		},
	}

	m := tabletext.NewModel(reader)
	for _, row := range rows {
		var rep tabletext.Report
		ok, err := tablewire.Decode(m, m.New(reader.Tables["Root"]), row.wire, &rep)
		if ok != row.want {
			t.Fatalf("%s: the oracle verdict is ok=%v err=%v report=%+v, want %v", row.name, ok, err, rep, row.want)
		}
		if row.want && rep.Unknown != 1 {
			t.Fatalf("%s: the oracle counted unknown %d, want 1", row.name, rep.Unknown)
		}
		if !row.want && !tablewire.ReadRefusal(err) {
			t.Fatalf("%s: the oracle refusal is not a ReadRefusal: %v", row.name, err)
		}
	}

	files := generateElixir(t, reader)
	out, err := runArmProbe(t, elixirBinary(t), files, unnamedNodeProbe(rows))
	if err != nil || !strings.Contains(out, "ELIXIR_UNNAMED_NODE_OK") {
		t.Fatalf("a pointer to an unnamed node type does not read null: %v\n%s", err, out)
	}
	if strings.Contains(out, "warning:") {
		t.Fatalf("the probe raised a compiler warning:\n%s", out)
	}
}

func unnamedNodeProbe(rows []unnamedNodeRow) string {
	var b strings.Builder
	b.WriteString(`
check = fn
  true, _ -> :ok
  false, label -> IO.puts(:stderr, "FAIL: " <> label); Process.put(:failed, true)
end
zero = %{unknown: 0, widened: 0, retained: 0, retain_lost: 0, reason: "", missing_required: 0}
store = %{records: [], used: 0, cap: 4096, ids: [], id_cap: 256}
`)
	for _, row := range rows {
		if row.want {
			fmt.Fprintf(&b, `
wire = Base.decode16!(%q, case: :lower)
label = %q
case Unnoderead.Table.load("Root", wire) do
  {:ok, v, r} ->
    check.(r.unknown == 1, label <> ": plain load did not count the record unknown once: " <> inspect(r))
    check.(%s, label <> ": plain load did not read the pointer null: " <> inspect(v))
  other -> check.(false, label <> ": plain load refused an unnamed node type: " <> inspect(other))
end
case Unnoderead.Table.variable_load("Root", wire) do
  {:ok, v, r} ->
    check.(r.unknown == 1, label <> ": variable load did not count the record unknown once: " <> inspect(r))
    check.(%s, label <> ": variable load did not read the pointer null: " <> inspect(v))
  other -> check.(false, label <> ": variable load refused an unnamed node type: " <> inspect(other))
end
case Unnoderead.Table.load_retain("Root", wire, store, zero) do
  {:ok, v, r, _rt} ->
    check.(r.unknown == 1, label <> ": retaining load did not count the record unknown once: " <> inspect(r))
    check.(%s, label <> ": retaining load did not read the pointer null: " <> inspect(v))
  other -> check.(false, label <> ": retaining load refused an unnamed node type: " <> inspect(other))
end
`, hex.EncodeToString(row.wire), row.name, row.check, row.check, row.check)
			continue
		}
		fmt.Fprintf(&b, `
wire = Base.decode16!(%q, case: :lower)
label = %q
case Unnoderead.Table.load("Root", wire) do
  {:error, reason, _} when is_atom(reason) -> :ok
  other -> check.(false, label <> ": plain load landed a named record of the wrong type: " <> inspect(other))
end
case Unnoderead.Table.variable_load("Root", wire) do
  {:error, reason, _} when is_atom(reason) -> :ok
  other -> check.(false, label <> ": variable load landed a named record of the wrong type: " <> inspect(other))
end
case Unnoderead.Table.load_retain("Root", wire, store, zero) do
  {:error, reason, _, _} when is_atom(reason) -> :ok
  other -> check.(false, label <> ": retaining load landed a named record of the wrong type: " <> inspect(other))
end
`, hex.EncodeToString(row.wire), row.name)
	}
	b.WriteString(`
if Process.get(:failed, false), do: System.halt(1)
IO.puts("ELIXIR_UNNAMED_NODE_OK")
`)
	return b.String()
}
