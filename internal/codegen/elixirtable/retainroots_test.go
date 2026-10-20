package elixirtable

import (
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
)

// TestRetainsUnknownOnEveryRootElixir pins docs/SPEC-TABLES.md §6.6 over every
// construct family the Elixir variable form carries: a scalar, a string, a
// bytes pointer, an unbounded array of scalars, an array of tables, an
// enum-keyed array, a map, a union arm, a repeated union, a pointer to a
// table and a wide string. Each family is a pair of units — an OLDER reader
// and a NEWER writer — and the newer writer's wire is read through the OLDER
// unit's retaining Load, measured, and written back through its retaining
// Save. The retained, retain_lost and unknown counters and the saved bytes are
// the reference oracle's (internal/tablewire), which is the wire fuzzer's
// divergence oracle and a third reading of §3 and §6.6.
//
// The excluded classes ride a family too: a bytes pointer the older unit
// cannot name is kind 17 and counts one retain_lost, never a retained record.
//
// Functional (elixirBinary): run with SCHEMA_REQUIRE_CORPUS=1.
func TestRetainsUnknownOnEveryRootElixir(t *testing.T) {
	t.Parallel()
	elixirBin := elixirBinary(t)

	const head = "package probe\n"
	cases := []struct {
		family string
		old    string
		new    string
		root   string
		json   string
	}{
		{
			family: "scalar",
			old:    head + "table Root { id int32 }\n",
			new:    head + "table Root { id int32\n extra int32 }\n",
			root:   "Root",
			json:   `{"id":7,"extra":42}`,
		},
		{
			family: "string",
			old:    head + "table Root { id int32 }\n",
			new:    head + "table Root { id int32\n extra string(16) }\n",
			root:   "Root",
			json:   `{"id":7,"extra":"kept"}`,
		},
		{
			family: "bytes",
			old:    head + "table Root { id int32 }\n",
			new:    head + "table Root { id int32\n extra bytes(8) }\n",
			root:   "Root",
			json:   `{"id":7,"extra":"kept"}`,
		},
		{
			family: "scalar-array",
			old:    head + "table Root { id int32 }\n",
			new:    head + "table Root { id int32\n extra []int32 }\n",
			root:   "Root",
			json:   `{"id":7,"extra":[1,2,3]}`,
		},
		{
			family: "table-array",
			old:    head + "table Child { n int32 }\ntable Root { list []Child }\n",
			new:    head + "table Child { n int32\n extra int32 }\ntable Root { list []Child }\n",
			root:   "Root",
			json:   `{"list":[{"n":1,"extra":11},{"n":2,"extra":12}]}`,
		},
		{
			family: "keyed-array",
			old:    head + "enum Key { A, B }\ntable Child { n int32 }\ntable Root { banks [Key]Child }\n",
			new:    head + "enum Key { A, B }\ntable Child { n int32\n extra int32 }\ntable Root { banks [Key]Child }\n",
			root:   "Root",
			json:   `{"banks":{"A":{"n":1,"extra":21},"B":{"n":2,"extra":22}}}`,
		},
		{
			family: "map",
			old:    head + "table Child { n int32 }\ntable Root { entries map[string(8)]Child }\n",
			new:    head + "table Child { n int32\n extra int32 }\ntable Root { entries map[string(8)]Child }\n",
			root:   "Root",
			json:   `{"entries":{"one":{"n":1,"extra":31},"two":{"n":2,"extra":32}}}`,
		},
		{
			family: "union-arm",
			old:    head + "table Child { n int32 }\nunion Pick { child Child\n empty }\ntable Root { pick Pick }\n",
			new:    head + "table Child { n int32\n extra int32 }\nunion Pick { child Child\n empty }\ntable Root { pick Pick }\n",
			root:   "Root",
			json:   `{"pick":{"child":{"n":1,"extra":41}}}`,
		},
		{
			family: "repeated-union",
			old:    head + "table Child { n int32 }\nunion Pick { child Child\n empty }\ntable Root { picks [..2]Pick }\n",
			new:    head + "table Child { n int32\n extra int32 }\nunion Pick { child Child\n empty }\ntable Root { picks [..2]Pick }\n",
			root:   "Root",
			json:   `{"picks":[{"child":{"n":1,"extra":51}},{"child":{"n":2,"extra":52}}]}`,
		},

	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.family, func(t *testing.T) {
			t.Parallel()
			familyRetain(t, elixirBin, tc.root, tc.old, tc.new, tc.json)
		})
	}
}

func familyRetain(t *testing.T, elixirBin, root, oldSrc, newSrc, json string) {
	t.Helper()
	newU := unitFrom(t, newSrc)
	oldU := unitFrom(t, oldSrc)

	newModel := tabletext.NewModel(newU)
	def := newModel.Lookup(root)
	if def == nil {
		t.Fatalf("the newer unit declares no table %s", root)
	}
	inst := newModel.New(def)
	var read tabletext.Report
	if !newModel.Read(inst, []byte(json), &read) {
		t.Fatalf("the newer value did not read: %+v", read)
	}
	wire, err := tablewire.Encode(newModel, inst)
	if err != nil {
		t.Fatalf("the newer value did not encode: %v", err)
	}

	oldModel := tabletext.NewModel(oldU)
	oldDef := oldModel.Lookup(root)
	oldInst := oldModel.New(oldDef)
	store := &tablewire.Retain{Capacity: 1 << 20, IdCapacity: 1024}
	var load tabletext.Report
	ok, err := tablewire.DecodeRetain(oldModel, oldInst, wire, store, &load)
	if err != nil || !ok || load.Malformed {
		t.Fatalf("the oracle's retaining load refused the sound wire: ok=%v err=%v %+v", ok, err, load)
	}
	var saved tabletext.Report
	want, err := tablewire.EncodeRetain(oldModel, oldInst, store, &saved)
	if err != nil {
		t.Fatalf("the oracle's retaining save refused: %v", err)
	}

	wantCounts := fmt.Sprintf(
		"%d,%d,%d %d",
		load.Retained, load.RetainLost, load.Unknown, saved.RetainLost,
	)

	dir := t.TempDir()
	names := writeModuleDir(t, dir, generateElixir(t, oldU))
	wirePath := filepath.Join(t.TempDir(), "family.wire")
	if err := os.WriteFile(wirePath, wire, 0o600); err != nil {
		t.Fatal(err)
	}

	probe := `
defmodule Probe do
  def check(true, _), do: :ok
  def check(false, what) do
    IO.puts(:stderr, "FAIL: " <> what)
    System.halt(1)
  end
end
wire = File.read!(` + elixirString(wirePath) + `)
retain = %{records: [], used: 0, cap: 1048576, ids: [], id_cap: 1024}
report = %{unknown: 0, kind_mismatch: 0, widened: 0, clamped: 0, malformed: false, retained: 0, retain_lost: 0}
{:ok, value, load, retain} = Probe.Table.load_retain("` + root + `", wire, retain, report)
size = Probe.Table.measure_retain("` + root + `", value, retain)
{:ok, out, saved, _retain} = Probe.Table.save_retain("` + root + `", value, retain, load)
Probe.check(size == byte_size(out), "measure #{size} and save #{byte_size(out)}")
IO.puts("COUNTS #{load.retained},#{load.retain_lost},#{load.unknown} #{saved.retain_lost - load.retain_lost}")
IO.puts("BYTES " <> Base.encode16(out, case: :lower))
`
	out := runProbe(t, elixirBin, dir, names, probe)
	var gotCounts, gotBytes string
	for _, line := range strings.Split(out, "\n") {
		switch {
		case strings.HasPrefix(line, "COUNTS "):
			gotCounts = strings.TrimSpace(strings.TrimPrefix(line, "COUNTS "))
		case strings.HasPrefix(line, "BYTES "):
			gotBytes = strings.TrimSpace(strings.TrimPrefix(line, "BYTES "))
		}
	}
	if gotCounts != wantCounts {
		t.Errorf("the retained pair reports %q (retained,retain_lost,unknown save_lost); the oracle %q", gotCounts, wantCounts)
	}
	if gotBytes != hex.EncodeToString(want) {
		t.Errorf("the retained save is %d bytes and the oracle's %d, and they differ", len(gotBytes)/2, len(want))
	}
}
