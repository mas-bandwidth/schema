package elixirtable

// A bytes(N) BODY READS EXACTLY ITS CANONICAL BASE64 (docs/SPEC-TABLES.md
// §16.2, §16.8): the standard alphabet, padded, pad bits zero, and exactly the
// text the writer writes for those bytes. Any other spelling that decodes
// refuses the read. The rows are the vectors under
// testdata/wire/json-base64-canonical; each is compared on the verdict alone
// with the Go engine's (internal/tabletext), and the engine is first held to
// the verdict the vectors state. A canonical row is also compared on the
// bytes read, including the canonical spelling written with JSON escapes,
// which the engine pins to unescape before the base64 rule
// (internal/tabletext/base64escape_test.go). Running the generated Elixir
// makes this a functional test, gated by slowtest.

import (
	"bufio"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
	"github.com/mas-bandwidth/schema/v2/ir"
)

const elixirJsonBase64CanonicalDir = "../../../testdata/wire/json-base64-canonical"

type elixirJsonBase64CanonicalRow struct {
	name, unit, root, text, verdict string
	inline                          bool
}

func elixirJsonBase64CanonicalRows(t *testing.T) []elixirJsonBase64CanonicalRow {
	t.Helper()
	f, err := os.Open(filepath.Join(elixirJsonBase64CanonicalDir, "MANIFEST.tsv"))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	var rows []elixirJsonBase64CanonicalRow
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := sc.Text()
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		c := strings.Split(line, "\t")
		if len(c) != 6 {
			t.Fatalf("MANIFEST.tsv: %d columns, want 6: %q", len(c), line)
		}
		rows = append(rows, elixirJsonBase64CanonicalRow{c[0], c[1], c[2], c[3], c[4], false})
	}
	if err := sc.Err(); err != nil {
		t.Fatal(err)
	}
	if len(rows) == 0 {
		t.Fatal("MANIFEST.tsv names no rows")
	}
	// The canonical spelling reaches the base64 rule AFTER the JSON string is
	// unescaped: `\/\/\/\/` is `////` (three 0xFF bytes), and `\u0041Q==` is
	// `AQ==` (0x01).
	first := rows[0]
	rows = append(rows,
		elixirJsonBase64CanonicalRow{"canonical-escaped-slash-read", first.unit, first.root, `{"data":"\/\/\/\/"}`, "read", true},
		elixirJsonBase64CanonicalRow{"canonical-escaped-letter-read", first.unit, first.root, `{"data":"\u0041Q=="}`, "read", true},
	)
	return rows
}

// elixirJsonBase64CanonicalOracle answers the Go engine's verdict for each row,
// holds it to the verdict the vectors state, and keeps the canonical text and
// wire bytes of a row every engine reads.
func elixirJsonBase64CanonicalOracle(t *testing.T, rows []elixirJsonBase64CanonicalRow) ([]string, [][]byte) {
	t.Helper()
	var want []string
	var wire [][]byte
	for _, row := range rows {
		u := loadUnit(t, filepath.Join("../../..", row.unit))
		model := tabletext.NewModel(u)
		def := model.Lookup(row.root)
		if def == nil {
			t.Fatalf("%s: the unit declares no %s", row.name, row.root)
		}
		src := []byte(row.text)
		if !row.inline {
			var err error
			src, err = os.ReadFile(filepath.Join(elixirJsonBase64CanonicalDir, row.text))
			if err != nil {
				t.Fatal(err)
			}
		}
		inst := model.New(def)
		var rep tabletext.Report
		verdict := "refused"
		if model.Read(inst, src, &rep) {
			verdict = "read"
		}
		if verdict != row.verdict {
			t.Fatalf("%s: the Go engine answers %s, the vectors state %s", row.name, verdict, row.verdict)
		}
		var encoded []byte
		if verdict == "read" {
			var err error
			encoded, err = tablewire.Encode(model, inst)
			if err != nil {
				t.Fatal(err)
			}
		}
		want = append(want, verdict)
		wire = append(wire, encoded)
	}
	return want, wire
}

func TestElixirJsonBase64Canonical(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "the Elixir toolchain")

	rows := elixirJsonBase64CanonicalRows(t)
	want, wire := elixirJsonBase64CanonicalOracle(t, rows)

	unit, root := rows[0].unit, rows[0].root
	for _, row := range rows {
		if row.unit != unit || row.root != root {
			t.Fatalf("%s: unit %s root %s; this test reads one unit and root, %s %s", row.name, row.unit, row.root, unit, root)
		}
	}
	u := loadUnit(t, filepath.Join("../../..", unit))
	mod := ir.GoExportName(u.Package) + ".Table"

	all := generateElixir(t, u)
	files := map[string][]byte{}
	for _, name := range []string{"BuildVersion.ex", "TableRuntime.ex", "FixedRuntime.ex", "Table.ex"} {
		src, ok := all[name]
		if !ok {
			t.Fatalf("Generate emitted no %s", name)
		}
		files[name] = src
	}

	var probe strings.Builder
	fmt.Fprintf(&probe, `
defmodule Base64Probe do
 def record(name, got, want) do
  if got != want do
   IO.puts("FAIL " <> name <> ": got " <> inspect(got) <> ", want " <> inspect(want))
   Process.put(:failures, Process.get(:failures, 0) + 1)
  end
 end
 def check(name, result, want_verdict, want_wire) do
  got =
   case result do
    {:ok, _, _} -> "read"
    {:error, _, _} -> "refused"
    {:error, _} -> "refused"
    other -> raise("unexpected from_json result: " <> inspect(other))
   end
  record(name <> " verdict", got, want_verdict)
  if want_verdict == "read" do
   case result do
    {:ok, value, _} ->
     case %s.save(%s, value) do
      {:ok, saved} -> record(name <> " bytes", saved, want_wire)
      other -> raise("unexpected save result: " <> inspect(other))
     end
    _ -> :ok
   end
  end
 end
end
`, mod, elixirString(root))
	for i, row := range rows {
		src := []byte(row.text)
		if !row.inline {
			var err error
			src, err = os.ReadFile(filepath.Join(elixirJsonBase64CanonicalDir, row.text))
			if err != nil {
				t.Fatal(err)
			}
		}
		fmt.Fprintf(&probe, "Base64Probe.check(%s, %s.from_json(%s, %s), %s, Base.decode16!(%s, case: :mixed))\n",
			elixirString(row.name), mod, elixirString(root), elixirString(string(src)), elixirString(want[i]), elixirString(hex.EncodeToString(wire[i])))
	}
	probe.WriteString(`if Process.get(:failures, 0) != 0, do: raise("base64 canonical assertions failed")
IO.puts("ok")
`)
	out, err := runArmProbe(t, elixirBinary(t), files, probe.String())
	if err != nil || !strings.Contains(out, "ok") {
		t.Fatalf("json base64 canonical: %v\n%s", err, out)
	}
}
