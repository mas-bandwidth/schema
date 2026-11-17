package elixirtable

// AN UNKNOWN KEYED OR ARRAY FIELD IS WALKED, NOT MERELY FRAMED
// (docs/SPEC-TABLES.md §3, §4). A field this reader cannot name is skipped by the framing
// alone, and the framing of an enum-keyed body (kind 16) or an array body
// (kind 14) includes the closed element-kind lists: a body whose element kind
// is off the list, whose key references no id or names one twice, whose slot
// length crosses its L, or that leaves bytes after its last pair is damage
// and refuses the whole read. The engine is the oracle; only its verdict is
// compared, never a refusal's name.
//
// Functional (elixirBinary): run with SCHEMA_REQUIRE_CORPUS=1.

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

func ukwLeb(v uint64) []byte { return binary.AppendUvarint(nil, v) }

func ukwCat(parts ...[]byte) []byte {
	var out []byte
	for _, p := range parts {
		out = append(out, p...)
	}
	return out
}

func ukwScalar(ref uint64, kind int, value []byte) []byte {
	return ukwCat(ukwLeb(ref), []byte{byte(kind)}, value)
}

func ukwFramed(ref uint64, kind int, content []byte) []byte {
	return ukwCat(ukwLeb(ref), []byte{byte(kind)}, ukwLeb(uint64(len(content))), content)
}

func ukwFile(ids []uint64, body []byte) []byte {
	out := append([]byte{ir.TableWireForm}, body...)
	for _, id := range ids {
		out = binary.LittleEndian.AppendUint64(out, id)
	}
	return binary.LittleEndian.AppendUint64(out, uint64(len(ids)))
}

func ukwKeyed(elemKind int, count uint64, pairs ...[]byte) []byte {
	return ukwCat([]byte{byte(elemKind)}, ukwLeb(count), ukwCat(pairs...))
}

func ukwArray(elemKind int, count uint64, elems ...[]byte) []byte {
	return ukwCat([]byte{byte(elemKind)}, ukwLeb(count), ukwCat(elems...))
}

func ukwPair(keyRef uint64, payload []byte) []byte {
	return ukwCat(ukwLeb(keyRef), ukwLeb(uint64(len(payload))), payload)
}

func TestElixirUnknownKeyedFieldIsWalked(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "the Elixir toolchain")
	elixirBin := elixirBinary(t)

	u := unitFrom(t, `package unknownkeyed

table Root { a int32 = 0 }
`)
	model := tabletext.NewModel(u)
	root := model.Lookup("Root")
	known := ir.TableWireId("a")
	unknown := ir.TableWireId("zzz")
	slot := ir.TableWireId("A")
	ids := []uint64{known, unknown, slot}

	// The i32 bytes satisfy Root's declared field; the unknown field carries
	// the body under test. Key references are 1-based indices into the id
	// table, so 2 and 3 name ids the table holds.
	withUnknown := func(field []byte) []byte {
		body := ukwCat(
			ukwScalar(1, ir.TableKindI32, make([]byte, 4)),
			field,
			[]byte{0},
		)
		return ukwFile(ids, body)
	}

	nestedArray := ukwArray(ir.TableKindU8, 0)
	nestedKeyed := ukwKeyed(ir.TableKindU8, 0)

	rows := []struct {
		name   string
		field  []byte
		damage string
	}{
		{"lawful-keyed", ukwFramed(2, ir.TableKindKeyed, ukwKeyed(ir.TableKindU8, 1, ukwPair(2, []byte{0x2a}))), ""},
		{"lawful-array", ukwFramed(2, ir.TableKindArray, ukwArray(ir.TableKindU8, 1, []byte{0x2a})), ""},
		{"pair-key-zero", ukwFramed(2, ir.TableKindKeyed, ukwKeyed(ir.TableKindU8, 1, ukwPair(0, []byte{1}))), "a keyed slot names None"},
		{"pair-key-past-id-table", ukwFramed(2, ir.TableKindKeyed, ukwKeyed(ir.TableKindU8, 1, ukwPair(99, []byte{1}))), "a keyed slot names no id"},
		{"pair-key-twice", ukwFramed(2, ir.TableKindKeyed, ukwKeyed(ir.TableKindU8, 2, ukwPair(2, []byte{1}), ukwPair(2, []byte{0}))), "one keyed slot named twice"},
		{"pair-length-past-body", ukwFramed(2, ir.TableKindKeyed, ukwKeyed(ir.TableKindU8, 1, ukwCat(ukwLeb(2), ukwLeb(5), []byte{1}))), "a slot length crosses its L"},
		{"pair-count-past-body", ukwFramed(2, ir.TableKindKeyed, ukwKeyed(ir.TableKindU8, 2, ukwPair(2, []byte{1}))), "a count past its pairs"},
		{"bytes-after-pairs", ukwFramed(2, ir.TableKindKeyed, ukwCat(ukwKeyed(ir.TableKindU8, 1, ukwPair(2, []byte{1})), []byte{9})), "bytes after the last pair"},
		{"keyed-header-short", ukwFramed(2, ir.TableKindKeyed, []byte{byte(ir.TableKindU8)}), "a body too short for its header"},
		{"keyed-element-string", ukwFramed(2, ir.TableKindKeyed, ukwKeyed(ir.TableKindString, 1, ukwPair(2, []byte("hi")))), "string is off the keyed list"},
		{"keyed-element-array", ukwFramed(2, ir.TableKindKeyed, ukwKeyed(ir.TableKindArray, 1, ukwPair(2, nestedArray))), "array is off the keyed list"},
		{"keyed-element-union", ukwFramed(2, ir.TableKindKeyed, ukwKeyed(ir.TableKindUnion, 1, ukwPair(2, []byte{0}))), "union is off the keyed list"},
		{"keyed-element-keyed", ukwFramed(2, ir.TableKindKeyed, ukwKeyed(ir.TableKindKeyed, 1, ukwPair(2, nestedKeyed))), "keyed is off the keyed list"},
		{"keyed-element-pointer", ukwFramed(2, ir.TableKindKeyed, ukwKeyed(ir.TableKindPointer, 1, ukwPair(2, []byte{0}))), "pointer is off the keyed list"},
		{"keyed-element-escape", ukwFramed(2, ir.TableKindKeyed, ukwKeyed(ir.TableKindEscape, 1, ukwPair(2, []byte{}))), "escape is off the keyed list"},
		{"keyed-element-no-payload", ukwFramed(2, ir.TableKindKeyed, ukwKeyed(ir.TableKindNoPayload, 1, ukwPair(2, []byte{}))), "no-payload is off the keyed list"},
		{"keyed-element-timestamp", ukwFramed(2, ir.TableKindKeyed, ukwKeyed(ir.TableKindTimestamp, 1, ukwPair(2, make([]byte, 8)))), "timestamp is off the keyed list"},
		{"array-element-array", ukwFramed(2, ir.TableKindArray, ukwArray(ir.TableKindArray, 1, nestedArray)), "array is off the array list"},
		{"array-element-keyed", ukwFramed(2, ir.TableKindArray, ukwArray(ir.TableKindKeyed, 1, nestedKeyed)), "keyed is off the array list"},
		{"array-element-escape", ukwFramed(2, ir.TableKindArray, ukwArray(ir.TableKindEscape, 1, ukwLeb(0))), "escape is off the array list"},
		{"array-element-no-payload", ukwFramed(2, ir.TableKindArray, ukwArray(ir.TableKindNoPayload, 1, ukwLeb(0))), "no-payload is off the array list"},
		{"array-element-timestamp", ukwFramed(2, ir.TableKindArray, ukwArray(ir.TableKindTimestamp, 1, make([]byte, 8))), "timestamp is off the array list"},
	}

	module := ir.GoExportName(u.Package) + ".Table"
	var probe strings.Builder
	probe.WriteString(`
defmodule UnknownKeyedProbe do
  def check(true, _label), do: :ok

  def check(false, label) do
    IO.puts(:stderr, "FAIL: " <> label)
    System.halt(1)
  end
end
`)
	for _, row := range rows {
		wire := withUnknown(row.field)
		var report tabletext.Report
		ok, err := tablewire.Decode(model, model.New(root), wire, &report)
		if !ok && !tablewire.ReadRefusal(err) {
			t.Fatalf("%s: the engine refused without a read refusal: %v", row.name, err)
		}
		if ok && row.damage != "" {
			t.Fatalf("%s: the engine read what the row calls damage: %s", row.name, row.damage)
		}
		fmt.Fprintf(&probe, `
case %s.load("Root", Base.decode16!(%q, case: :lower)) do
  {:ok, _value, _report} -> UnknownKeyedProbe.check(%t, %q <> ": read a damaged unknown body")
  {:error, _why, _report} -> UnknownKeyedProbe.check(%t, %q <> ": refused a lawful unknown body")
  other -> UnknownKeyedProbe.check(false, %q <> ": answered " <> inspect(other))
end
`, module, hex.EncodeToString(wire), ok, row.name, !ok, row.name, row.name)
	}
	probe.WriteString(`IO.puts("UNKNOWN_KEYED_WALKED_OK")`)

	out, err := runArmProbe(t, elixirBin, generateElixir(t, u), probe.String())
	if err != nil || !strings.Contains(out, "UNKNOWN_KEYED_WALKED_OK") {
		t.Fatalf("an unknown keyed or array field is not walked as the engine walks it: %v\n%s", err, out)
	}
}
