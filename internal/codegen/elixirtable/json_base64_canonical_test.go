package elixirtable

import (
	"bufio"
	"bytes"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
)

// TestElixirJsonBase64Canonical holds the Elixir JSON read of a bytes value to
// docs/SPEC-TABLES.md §16.2 and §16.8 (docs/rulings.json json-base64-canonical):
// the read accepts exactly canonical base64 (standard alphabet, padded, pad
// bits zero) of the JSON string's DECODED value and refuses any other spelling
// that decodes. Every vector under testdata/wire/json-base64-canonical/ is
// compared on the verdict with the reference engine (internal/tabletext), and
// a row the engine reads is compared on the bytes as well. Two more rows spell
// the canonical text with JSON escapes, which are undone before the base64 is
// judged (internal/tabletext/base64escape_test.go).
func TestElixirJsonBase64Canonical(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "the Elixir toolchain")
	const dir = "../../../testdata/wire/json-base64-canonical"
	u := loadUnit(t, "../../../tables/stream/Stream.schema")
	model := tabletext.NewModel(u)
	st := model.Lookup("Chunk")
	if st == nil {
		t.Fatal("no Chunk table")
	}

	type row struct {
		name, text, verdict string
		want                []byte
	}
	var rows []row
	manifest, err := os.Open(filepath.Join(dir, "MANIFEST.tsv"))
	if err != nil {
		t.Fatal(err)
	}
	defer manifest.Close()
	scan := bufio.NewScanner(manifest)
	for scan.Scan() {
		line := scan.Text()
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		cols := strings.Split(line, "\t")
		if len(cols) != 6 {
			t.Fatalf("MANIFEST.tsv: want 6 columns, got %q", line)
		}
		if cols[1] != "tables/stream/Stream.schema" || cols[2] != "Chunk" {
			t.Fatalf("MANIFEST.tsv: row %s names %s %s, this test reads Stream.schema Chunk", cols[0], cols[1], cols[2])
		}
		text, err := os.ReadFile(filepath.Join(dir, cols[3]))
		if err != nil {
			t.Fatal(err)
		}
		var want []byte
		if cols[4] == "read" {
			want = []byte("AB")
		}
		rows = append(rows, row{cols[0], string(text), cols[4], want})
	}
	if err := scan.Err(); err != nil {
		t.Fatal(err)
	}
	if len(rows) < 4 {
		t.Fatalf("MANIFEST.tsv: want at least 4 vectors, got %d", len(rows))
	}
	rows = append(rows,
		row{"escaped-solidus-read", `{ "data": "\/\/\/\/" }`, "read", []byte{0xff, 0xff, 0xff}},
		row{"escaped-letter-read", `{ "data": "AQ==" }`, "read", []byte{0x01}},
	)

	var probe strings.Builder
	probe.WriteString(`
defmodule Probe do
 def check(true, _), do: :ok
 def check(false, name) do
  IO.puts("FAIL " <> name)
  Process.put(:failures, Process.get(:failures, 0) + 1)
 end
 def row(text, accepted, want, json, name) do
  case Streamdemo.Table.from_json("Chunk", text) do
   {:ok, value, _report} ->
    check(accepted, name <> " verdict: read, the reference refuses")
    check(Map.get(value, "data") == want, name <> " bytes: " <> inspect(Map.get(value, "data")))
    check(Streamdemo.Table.to_json("Chunk", value) == {:ok, json}, name <> " canonical text")
   {:error, _, _report} ->
    check(not accepted, name <> " verdict: refused, the reference reads")
   other -> check(false, name <> " unexpected result: " <> inspect(other))
  end
 end
end
`)
	for _, r := range rows {
		inst := model.New(st)
		report := tabletext.Report{}
		ok := model.Read(inst, []byte(r.text), &report)
		if ok != (r.verdict == "read") {
			t.Fatalf("reference engine: row %s reads=%t, the standard says %s (%+v)", r.name, ok, r.verdict, report)
		}
		var canon []byte
		if ok {
			fv, found := inst.FieldByKey("data")
			if !found {
				t.Fatal("no data field on Chunk")
			}
			got := fv.Cell.Str
			if !bytes.Equal(got, r.want) {
				t.Fatalf("reference engine: row %s read %x, want %x", r.name, got, r.want)
			}
			canon, err = model.Write(inst)
			if err != nil {
				t.Fatal(err)
			}
			canon = append(canon, '\n')
		}
		fmt.Fprintf(&probe, "Probe.row(Base.decode16!(%q, case: :mixed), %t, Base.decode16!(%q, case: :mixed), Base.decode16!(%q, case: :mixed), %q)\n",
			hex.EncodeToString([]byte(r.text)), ok, hex.EncodeToString(r.want), hex.EncodeToString(canon), r.name)
	}
	probe.WriteString("if Process.get(:failures, 0) != 0, do: raise(\"canonical base64 assertions failed\")\nIO.puts(\"JSON_BASE64_CANONICAL_OK\")\n")
	out, err := runArmProbe(t, elixirBinary(t), generateElixir(t, u), probe.String())
	if err != nil || !strings.Contains(out, "JSON_BASE64_CANONICAL_OK") {
		t.Fatalf("Elixir JSON canonical base64: %v\n%s", err, out)
	}
}
