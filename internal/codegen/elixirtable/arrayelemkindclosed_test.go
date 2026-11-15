package elixirtable

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
)

// TestElixirArrayElementKindsClosed holds docs/SPEC-TABLES.md §3, §4 on the
// Elixir variable leg: the element kind an array or keyed body opens with is
// drawn from a closed list, and any other kind is damage under any count, a
// count of 0 included, in a declared field and in an unknown one alike. Every
// pinned array_elem_* vector rides an unknown root field of graphdemo Scene,
// so the wire-alone walk answers each one; the kind 17 controls are on the
// list and must read.
func TestElixirArrayElementKindsClosed(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "the Elixir toolchain")

	elixirBin := elixirBinary(t)
	root, err := filepath.Abs(filepath.Join("..", "..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	paths, err := filepath.Glob(filepath.Join(root, "tables", "pointers", "*.schema"))
	if err != nil || len(paths) == 0 {
		t.Fatalf("pointer schema paths: %v (%d files)", err, len(paths))
	}
	u := loadUnit(t, paths...)
	framing := filepath.Join(root, "testdata", "wire", "tables", "framing")

	cases := []struct {
		name  string
		reads bool
	}{
		{name: "array_elem_kind_32_count_2", reads: false},
		{name: "array_elem_kind_31_count_1", reads: false},
		{name: "array_elem_kind_16_count_1", reads: false},
		{name: "array_elem_unassigned_count_0", reads: false},
		{name: "array_elem_reserved_count_0", reads: false},
		{name: "array_elem_zero_count_0", reads: false},
		{name: "array_elem_kind_17_count_0", reads: true},
		{name: "array_elem_kind_17_count_1", reads: true},
	}

	var probe strings.Builder
	probe.WriteString(`
defmodule ElemKindProbe do
  def check(true, _), do: :ok
  def check(false, what) do
    IO.puts(:stderr, "FAIL: " <> what)
    System.halt(1)
  end
end
`)
	for _, tc := range cases {
		wire := filepath.Join(framing, tc.name+".bin")
		if _, err := os.Stat(wire); err != nil {
			t.Fatalf("%s: %v", tc.name, err)
		}
		fmt.Fprintf(&probe, `
case Graphdemo.Table.load("Scene", File.read!(%q)) do
  {:ok, value, %%{reason: ""}} when is_map(value) -> ElemKindProbe.check(%t, %q <> ": read, want refused")
  {:error, _why, %%{reason: reason}} when reason != "" -> ElemKindProbe.check(%t, %q <> ": refused, want read")
  other -> ElemKindProbe.check(false, %q <> ": unexpected " <> inspect(other, limit: 8))
end
`, wire, tc.reads, tc.name, !tc.reads, tc.name, tc.name)
	}
	probe.WriteString("IO.puts(\"ok: Elixir array element kinds are closed\")\n")

	out, err := runArmProbe(t, elixirBin, generateElixir(t, u), probe.String())
	if err != nil || !strings.Contains(out, "ok: Elixir array element kinds are closed") {
		t.Fatalf("array element kinds are not closed: %v\n%s", err, out)
	}
}
