package elixirtable

import (
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
)

// The read-closed unit's schema, as the vectors under
// testdata/wire/json-read-closed/ read it: a flags field, an enum, a union, a
// map keyed by an unbounded string, a string value, and a pointered root for
// the `&` name's site.
const elixirJsonNameCapSchema = `package closed

enum Kind { Alpha, Beta }

flags Perms { Read, Write }

type Buff
{
    mult float32 = 1.0
}

union Effect
{
    buff Buff
    ping
}

table Node
{
    value int32 = 0
}

table Scene
{
    head *Node
}

table Closed
{
    kind   Kind
    perms  Perms
    effect Effect
    tags   map[string]int32
    note   string(300)
}
`

// TestElixirJsonNameCap holds docs/SPEC-TABLES.md §16.2's 255-byte cap on every
// schema-name site the read-closed vectors name: an object key (a known field
// or an unknown one, in a placed object or a skipped value), an enum variant
// name, a flags element, a union arm key and a pointer's first key. A 255-byte
// name reads and a 256-byte one refuses. A map key and a string value are data:
// a long one reads.
func TestElixirJsonNameCap(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "the Elixir toolchain")

	u := unitFrom(t, elixirJsonNameCapSchema)
	files := generateElixir(t, u)

	dir := filepath.Join("..", "..", "..", "testdata", "wire", "json-read-closed")
	data, err := os.ReadFile(filepath.Join(dir, "MANIFEST.tsv"))
	if err != nil {
		t.Fatal(err)
	}

	type vector struct {
		root    string
		file    string
		verdict string
	}
	rows := map[string]vector{}
	for i, line := range strings.Split(string(data), "\n") {
		line = strings.TrimRight(line, "\r")
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		fields := strings.Split(line, "\t")
		if len(fields) != 5 {
			t.Fatalf("%s/MANIFEST.tsv:%d: want name, root, json, verdict, description, got %d columns", dir, i+1, len(fields))
		}
		rows[fields[0]] = vector{root: fields[1], file: fields[2], verdict: fields[3]}
	}

	names := []string{
		"field-key-255", "field-key-256",
		"skipped-key-255", "skipped-key-256",
		"enum-name-255", "enum-name-256",
		"flags-element-255", "flags-element-256",
		"union-arm-255", "union-arm-256",
		"ampersand-255", "ampersand-256",
		"map-key-long", "string-value-long",
	}

	var probe strings.Builder
	probe.WriteString(`
defmodule Probe do
 def check(true, _), do: :ok
 def check(false, what) do
  IO.puts("FAIL " <> what)
  Process.put(:failures, Process.get(:failures, 0) + 1)
 end
 def row(mod, table, text, want_ok, name) do
  result = mod.from_json(table, text)
  ok = match?({:ok, _, _}, result)
  check(ok == want_ok, name <> " verdict: " <> inspect(result))
 end
end
`)
	for _, name := range names {
		row, ok := rows[name]
		if !ok {
			t.Fatalf("%s/MANIFEST.tsv names no vector %q", dir, name)
		}
		if row.verdict != "read" && row.verdict != "refused" {
			t.Fatalf("%s: verdict %q, want read or refused", name, row.verdict)
		}
		body, err := os.ReadFile(filepath.Join(dir, row.file))
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		fmt.Fprintf(&probe, "Probe.row(Closed.Table, %s, Base.decode16!(%q, case: :mixed), %t, %s)\n",
			elixirString(row.root), hex.EncodeToString(body), row.verdict == "read", elixirString(name))
	}
	probe.WriteString("if Process.get(:failures, 0) != 0, do: raise(\"name cap assertions failed\")\nIO.puts(\"ok\")\n")

	out, err := runArmProbe(t, elixirBinary(t), files, probe.String())
	if err != nil || !strings.Contains(out, "ok") {
		t.Fatalf("JSON schema-name cap: %v\n%s", err, out)
	}
}
