package elixirtable

// A bytes(N) payload is exactly its header and its count (docs/SPEC-TABLES.md
// §3): the bytes after the count number exactly the count, never fewer and
// never more, and a body too short for its header is damage. The rule holds on
// every variable-form read — load/2, variable_load/2 and load_retain/4 — at the
// root and inside a node record. The Go engine is the oracle: it refuses each
// damaged wire before the generated Elixir is asked, and the Elixir read must
// refuse it too (§4, §6.5). The lawful wire loads through all three reads and
// re-saves byte for byte.

import (
	"bytes"
	"encoding/binary"
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

type fixedBytesRow struct {
	name  string
	table string
	wire  []byte
	want  bool
}

// fixedBytesMarker returns the index of the four-byte value map_text.bin
// carries in its `blobs` map, whose bytes are de ad be ef. The count byte sits
// before it and the array body's L three bytes before that.
func fixedBytesMarker(t *testing.T, wire []byte) int {
	t.Helper()
	marker := []byte{0xde, 0xad, 0xbe, 0xef}
	idx := bytes.Index(wire, marker)
	if idx < 0 {
		t.Fatal("map_text.bin no longer carries the four-byte bytes(N) value")
	}
	if idx == 0 || wire[idx-1] != 4 {
		t.Fatalf("map_text.bin's bytes(N) count is not four: byte %d", wire[idx-1])
	}
	return idx
}

// fixedBytesSetCount rewrites the count of map_text.bin's four-byte bytes(N)
// value, leaving the body's L untouched: the bytes after the count no longer
// number the count.
func fixedBytesSetCount(t *testing.T, wire []byte, count byte) []byte {
	t.Helper()
	idx := fixedBytesMarker(t, wire)
	out := append([]byte(nil), wire...)
	out[idx-1] = count
	return out
}

// fixedBytesCutHeader shortens map_text.bin's four-byte bytes(N) array body to
// one byte, its element kind alone: the body is too short for its two-byte
// header. The enclosing map body's L shrinks by the five bytes removed.
func fixedBytesCutHeader(t *testing.T, wire []byte) []byte {
	t.Helper()
	idx := fixedBytesMarker(t, wire)
	if idx < 15 {
		t.Fatalf("map_text.bin's bytes(N) value at %d cannot sit in its map field", idx)
	}
	out := append([]byte(nil), wire[:idx-3]...)
	out = append(out, 1)               // the array body's L: the element kind only
	out = append(out, wire[idx-2])     // the element kind
	out = append(out, wire[idx+4:]...) // drop the count and the four value bytes
	at := idx - 15                     // the enclosing map field's L
	if out[at] < 5 {
		t.Fatalf("map_text.bin's map body is shorter than the five bytes removed: %d", out[at])
	}
	out[at] -= 5
	return out
}

// fixedBytesNodeWire builds a Root with one node record holding a bytes(8)
// field whose array body is arrayBody. The lengths are computed from the body,
// so a body too short for its header is framed exactly as a writer that wrote
// it would frame it.
func fixedBytesNodeWire(t *testing.T, u *ir.Unit, arrayBody []byte) []byte {
	t.Helper()
	var ids []uint64
	ref := func(id uint64) []byte {
		for i, v := range ids {
			if v == id {
				return binary.AppendUvarint(nil, uint64(i+1))
			}
		}
		ids = append(ids, id)
		return binary.AppendUvarint(nil, uint64(len(ids)))
	}

	childRef := ref(ir.TableWireId("child"))
	nodeRef := ref(ir.TableNodeWireId)
	nodeTypeRef := ref(ir.TableWireId(u.Tables["Node"].WireName()))
	blobRef := ref(ir.TableWireId("blob"))

	recBody := append([]byte(nil), blobRef...)
	recBody = append(recBody, byte(ir.TableKindArray))
	recBody = binary.AppendUvarint(recBody, uint64(len(arrayBody)))
	recBody = append(recBody, arrayBody...)
	recBody = append(recBody, 0)

	nodeTable := binary.AppendUvarint(nil, 1)
	nodeTable = append(nodeTable, nodeTypeRef...)
	nodeTable = binary.AppendUvarint(nodeTable, uint64(len(recBody)))
	nodeTable = append(nodeTable, recBody...)

	body := append([]byte(nil), childRef...)
	body = append(body, byte(ir.TableKindPointer))
	body = binary.AppendUvarint(body, 2)
	body = append(body, nodeRef...)
	body = append(body, byte(ir.TableKindString))
	body = binary.AppendUvarint(body, uint64(len(nodeTable)))
	body = append(body, nodeTable...)
	body = append(body, 0)

	wire := append([]byte{1}, body...)
	for _, id := range ids {
		wire = binary.LittleEndian.AppendUint64(wire, id)
	}
	return binary.LittleEndian.AppendUint64(wire, uint64(len(ids)))
}

func fixedBytesRows(t *testing.T, u *ir.Unit, wire []byte) []fixedBytesRow {
	t.Helper()
	idx := fixedBytesMarker(t, wire)
	value := wire[idx : idx+4]

	node := func(body []byte) []byte { return fixedBytesNodeWire(t, u, body) }
	cleanNodeBody := append([]byte{byte(ir.TableKindU8), 4}, value...)

	rows := []fixedBytesRow{
		{name: "valid", table: "Text", wire: append([]byte(nil), wire...), want: true},
		{name: "count-lowered-below", table: "Text", wire: fixedBytesSetCount(t, wire, 0)},
		{name: "count-lowered-within", table: "Text", wire: fixedBytesSetCount(t, wire, 2)},
		{name: "count-raised-above", table: "Text", wire: fixedBytesSetCount(t, wire, 5)},
		{name: "body-under-header", table: "Text", wire: fixedBytesCutHeader(t, wire)},
		{name: "node-valid", table: "Root", wire: node(cleanNodeBody), want: true},
		{name: "node-count-lowered", table: "Root", wire: node([]byte{byte(ir.TableKindU8), 0, value[0], value[1], value[2], value[3]})},
		{name: "node-count-raised", table: "Root", wire: node([]byte{byte(ir.TableKindU8), 5, value[0], value[1], value[2], value[3]})},
		{name: "node-body-under-header", table: "Root", wire: node([]byte{byte(ir.TableKindU8)})},
	}
	return rows
}

func fixedBytesProbe(mod string, rows []fixedBytesRow) string {
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
		fmt.Fprintf(&b, `
  wire = Base.decode16!(%q, case: :lower)
  want = %t
  label = %q
  case %s.load(%q, wire) do
    {:ok, value, report} ->
      check.(want, label <> ": plain load refused a lawful wire " <> inspect(report))
      if want do
        case %s.save(%q, value) do
          {:ok, out} -> check.(out == wire, label <> ": plain save does not reproduce the wire")
          other -> check.(false, label <> ": plain save failed " <> inspect(other))
        end
      end
    {:error, _reason, _report} ->
      check.(not want, label <> ": plain load landed a damaged wire")
    other -> check.(false, label <> ": plain load answered " <> inspect(other))
  end
  case %s.variable_load(%q, wire) do
    {:ok, _value, _report} -> check.(want, label <> ": framing load landed a damaged wire")
    {:error, _reason, _report} -> check.(not want, label <> ": framing load refused a lawful wire")
    other -> check.(false, label <> ": framing load answered " <> inspect(other))
  end
  case %s.load_retain(%q, wire, store, zero) do
    {:ok, value, report, retain} ->
      check.(want, label <> ": retaining load refused a lawful wire " <> inspect(report))
      if want do
        case %s.save_retain(%q, value, retain, report) do
          {:ok, out, _, _} -> check.(out == wire, label <> ": retaining save does not reproduce the wire")
          other -> check.(false, label <> ": retaining save failed " <> inspect(other))
        end
      end
    {:error, _reason, _report, _retain} ->
      check.(not want, label <> ": retaining load landed a damaged wire")
    other -> check.(false, label <> ": retaining load answered " <> inspect(other))
  end
`, hex.EncodeToString(row.wire), row.want, row.name, mod, row.table, mod, row.table,
			mod, row.table, mod, row.table, mod, row.table)
	}
	b.WriteString(`
if Process.get(:failed, false), do: System.halt(1)
IO.puts("ELIXIR_FIXED_BYTES_COUNT_OK")
`)
	return b.String()
}

func TestElixirFixedBytesCountFillsItsLength(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "the Elixir toolchain")

	real, err := os.ReadFile(filepath.Join("..", "..", "..", "testdata", "wire", "tables", "map_text.bin"))
	if err != nil {
		t.Fatalf("read map_text.bin: %v", err)
	}
	text, err := os.ReadFile(filepath.Join("..", "..", "..", "tables", "maps", "Text.schema"))
	if err != nil {
		t.Fatalf("read Text.schema: %v", err)
	}
	u := unitFrom(t, string(text)+`
table Node { blob bytes(8) }
table Root { child *Node }
`)
	m := tabletext.NewModel(u)
	rows := fixedBytesRows(t, u, real)

	for _, row := range rows {
		table := u.Tables[row.table]
		if table == nil {
			t.Fatalf("%s: no table %s", row.name, row.table)
		}
		var report tabletext.Report
		ok, decodeErr := tablewire.Decode(m, m.New(table), row.wire, &report)
		if ok != row.want {
			t.Fatalf("%s: oracle verdict %v (%v) %+v, want %v", row.name, ok, decodeErr, report, row.want)
		}
		if !row.want && (!tablewire.ReadRefusal(decodeErr) || !report.Refused || report.Reason == "") {
			t.Fatalf("%s: oracle refusal is not a read refusal: %v %+v", row.name, decodeErr, report)
		}
	}

	mod := ir.GoExportName(u.Package) + ".Table"
	out, err := runArmProbe(t, elixirBinary(t), generateElixir(t, u), fixedBytesProbe(mod, rows))
	if err != nil || !strings.Contains(out, "ELIXIR_FIXED_BYTES_COUNT_OK") || strings.Contains(out, "warning:") {
		t.Fatalf("the fixed bytes count rule does not hold: %v\n%s", err, out)
	}
}
