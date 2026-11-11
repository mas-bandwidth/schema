package elixirtable

import (
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
	"github.com/mas-bandwidth/schema/v2/ir"
)

func TestElixirNodeRecordEndsAtItsTerminator(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "Elixir node records must consume their entire declared body")

	u := unitFrom(t, `package nodeterm
table Node
{
    value int32
    name string(8)
}
table Root
{
    head *Node
}`)
	model := tabletext.NewModel(u)
	inst := model.New(u.Tables["Root"])
	var report tabletext.Report
	if !model.Read(inst, []byte(`{"head":{"value":1,"name":"one"}}`), &report) || report.Refused {
		t.Fatalf("oracle did not read the graph text: %+v", report)
	}
	good, err := tablewire.Encode(model, inst)
	if err != nil {
		t.Fatalf("oracle did not encode the graph: %v", err)
	}
	report = tabletext.Report{}
	if ok, err := tablewire.Decode(model, model.New(u.Tables["Root"]), good, &report); !ok || err != nil || report.Refused {
		t.Fatalf("oracle rejected the committed wire: ok=%v report=%+v err=%v", ok, report, err)
	}

	bad := earlyNodeTerminator(t, good)
	var damaged tabletext.Report
	if ok, err := tablewire.Decode(model, model.New(u.Tables["Root"]), bad, &damaged); ok || !tablewire.ReadRefusal(err) {
		t.Fatalf("oracle verdict for early node terminator: ok=%v report=%+v err=%v", ok, damaged, err)
	}

	probe := fmt.Sprintf(`alias Nodeterm.Table, as: T
good = Base.decode16!(%q, case: :lower)
bad = Base.decode16!(%q, case: :lower)
store = %%{records: [], used: 0, cap: 4096, ids: [], id_cap: 256}
zero = %%{unknown: 0, widened: 0, retained: 0, retain_lost: 0, reason: "", missing_required: 0}
check = fn true, _ -> :ok; false, label -> IO.puts(:stderr, "FAIL: " <> label); Process.put(:failed, true) end

for result <- [T.load("Root", good), T.variable_load("Root", good)] do
  check.(elem(result, 0) == :ok, "valid wire refused")
end
case T.load("Root", good) do
  {:ok, value, _} ->
    case T.save("Root", value) do
      {:ok, out} -> check.(out == good, "valid wire did not re-save byte for byte")
      other -> check.(false, "valid wire save failed: " <> inspect(other))
    end
  other -> check.(false, "valid wire load answered: " <> inspect(other))
end
for result <- [T.load("Root", bad), T.variable_load("Root", bad), T.load_retain("Root", bad, store, zero)] do
  check.(elem(result, 0) == :error, "damaged node record was accepted")
end
if Process.get(:failed, false), do: System.halt(1)
IO.puts("ELIXIR_NODE_TERMINATOR_OK")
`, hex.EncodeToString(good), hex.EncodeToString(bad))
	out, err := runArmProbe(t, elixirBinary(t), generateElixir(t, u), probe)
	if err != nil || !strings.Contains(out, "ELIXIR_NODE_TERMINATOR_OK") || strings.Contains(out, "FAIL:") {
		t.Fatalf("Elixir node-record terminator probe: %v\n%s", err, out)
	}
}

func earlyNodeTerminator(t *testing.T, wire []byte) []byte {
	t.Helper()
	if len(wire) < 9 {
		t.Fatal("wire too short")
	}
	count := binary.LittleEndian.Uint64(wire[len(wire)-8:])
	idsStart := len(wire) - 8 - int(count)*8
	if idsStart < 1 {
		t.Fatal("invalid id table")
	}
	ids := make([]uint64, count)
	for i := range ids {
		ids[i] = binary.LittleEndian.Uint64(wire[idsStart+i*8:])
	}
	readVarint := func(data []byte, pos *int) uint64 {
		n, size := binary.Uvarint(data[*pos:])
		if size <= 0 {
			t.Fatal("invalid varint in wire")
		}
		*pos += size
		return n
	}

	for pos := 1; pos < idsStart; {
		ref := readVarint(wire, &pos)
		if ref == 0 {
			break
		}
		if pos >= idsStart {
			t.Fatal("truncated root field")
		}
		kind := wire[pos]
		pos++
		if ref > uint64(len(ids)) {
			t.Fatal("root field reference exceeds id table")
		}
		if ids[ref-1] == ir.TableNodeWireId {
			if kind != 12 {
				t.Fatal("node table is not a kind 12 field")
			}
			length := int(readVarint(wire, &pos))
			payloadStart := pos
			payloadEnd := payloadStart + length
			if payloadEnd > idsStart {
				t.Fatal("node table exceeds root body")
			}
			return corruptFirstNodeBody(t, wire, payloadStart, payloadEnd)
		}
		switch kind {
		case 17:
			_ = readVarint(wire, &pos)
		case 12, 13, 14:
			length := int(readVarint(wire, &pos))
			pos += length
		default:
			t.Fatal("unexpected root field kind in fixture")
		}
	}
	t.Fatal("reserved node-table field not found")
	return nil
}

func corruptFirstNodeBody(t *testing.T, wire []byte, start, end int) []byte {
	t.Helper()
	pos := start
	count, size := binary.Uvarint(wire[pos:end])
	if size <= 0 || count == 0 {
		t.Fatal("invalid node table")
	}
	pos += size
	_ = readWireUvarint(t, wire, end, &pos) // node type reference
	bodyLen := int(readWireUvarint(t, wire, end, &pos))
	bodyStart := pos
	bodyEnd := bodyStart + bodyLen
	if bodyEnd > end {
		t.Fatal("node body exceeds node table")
	}
	fieldStarts := make([]int, 0, 3)
	for pos < bodyEnd {
		fieldStart := pos
		ref := readWireUvarint(t, wire, bodyEnd, &pos)
		if ref == 0 {
			break
		}
		fieldStarts = append(fieldStarts, fieldStart)
		if pos >= bodyEnd {
			t.Fatal("truncated node field kind")
		}
		kind := wire[pos]
		pos++
		switch kind {
		case 4:
			pos += 4
		case 12:
			length := int(readWireUvarint(t, wire, bodyEnd, &pos))
			pos += length
		default:
			t.Fatal("unexpected node field kind in fixture")
		}
		if pos > bodyEnd {
			t.Fatal("node field exceeds node body")
		}
	}
	if len(fieldStarts) < 2 || wire[fieldStarts[1]] >= 0x80 {
		t.Fatal("first node needs at least two single-byte field references")
	}
	bad := append([]byte(nil), wire...)
	bad[fieldStarts[1]] = 0
	return bad
}

func readWireUvarint(t *testing.T, wire []byte, end int, pos *int) uint64 {
	t.Helper()
	if *pos >= end {
		t.Fatal("truncated varint")
	}
	n, size := binary.Uvarint(wire[*pos:end])
	if size <= 0 {
		t.Fatal("invalid varint")
	}
	*pos += size
	return n
}
