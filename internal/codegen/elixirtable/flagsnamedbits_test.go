package elixirtable

// THE TEXT WRITER REFUSES A FLAGS MASK WITH A BIT NO VARIANT NAMES
// (docs/SPEC-TABLES.md §5, §16.2): the file read keeps such a bit whole
// (§4.1), and the text form has no spelling for it, so to_json/2 answers
// {:error, :invalid_value} exactly as the Go engine's writer does. The same
// clause carries a flags FIELD and a flags LIST ELEMENT, and every named-bits
// value still writes its committed text.

import (
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
)

const flagsNamedBitsSchema = `package flagsbits

flags Perm { Read, Write, Own }

table Root
{
    perm  Perm
    perms []Perm
}
`

func fieldSlot(t *testing.T, inst *tabletext.Instance, name string) *tabletext.Field {
	t.Helper()
	for i := range inst.Fields {
		if inst.Fields[i].Def.Name == name {
			return &inst.Fields[i]
		}
	}
	t.Fatalf("%s has no field %s", inst.Def.Name, name)
	return nil
}

func TestElixirTextWriterRefusesUnnamedFlagsBit(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "the Elixir toolchain")
	elixirBin := elixirBinary(t)

	root, err := filepath.Abs(filepath.Join("..", "..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	lists := loadDirUnit(t, filepath.Join(root, "tables", "lists"))
	corpusWire, err := os.ReadFile(filepath.Join(root, "testdata", "wire", "tables", "list_mixed.bin"))
	if err != nil {
		t.Fatal(err)
	}
	corpusText, err := os.ReadFile(filepath.Join(root, "testdata", "conformance", "tables", "json", "list_mixed.json"))
	if err != nil {
		t.Fatal(err)
	}

	// THE GO ENGINE'S VERDICT: the committed named wire writes the committed
	// text, and one unnamed bit on the flags list element refuses invalid_value.
	listsModel := tabletext.NewModel(lists)
	named := listsModel.New(listsModel.Lookup("Mixed"))
	var report tabletext.Report
	if ok, err := tablewire.Decode(listsModel, named, corpusWire, &report); !ok || err != nil {
		t.Fatalf("the oracle did not read the committed list_mixed wire: %v %+v", err, report)
	}
	text, err := listsModel.Write(named)
	if err != nil {
		t.Fatalf("the oracle refused the committed named bits: %v", err)
	}
	if got := string(text) + "\n"; got != string(corpusText) {
		t.Fatalf("the oracle's named bits do not write the committed text:\ngot  %q\nwant %q", got, corpusText)
	}
	unnamedList := listsModel.New(listsModel.Lookup("Mixed"))
	if ok, err := tablewire.Decode(listsModel, unnamedList, corpusWire, &report); !ok || err != nil {
		t.Fatalf("the oracle did not read the committed list_mixed wire: %v %+v", err, report)
	}
	fieldSlot(t, unnamedList, "perms").Elems = []tabletext.Cell{{U: 3}, {U: 8}}
	fieldSlot(t, unnamedList, "perms").Count = 2
	if _, err := listsModel.Write(unnamedList); !errors.As(err, new(*tabletext.InvalidValue)) {
		t.Fatalf("the oracle wrote a flags list element with an unnamed bit: %v", err)
	}

	// The committed named-bits value still writes its text; the same value with
	// an unnamed bit on the list element refuses, and the refusal is the tagged
	// one the surface already answers.
	listsProbe := fmt.Sprintf(`
check = fn
  true, _ -> :ok
  false, label -> IO.puts(:stderr, "FAIL: " <> label); Process.put(:failed, true)
end
wire = Base.decode16!(%q, case: :lower)
want = Base.decode16!(%q, case: :lower)
case Listdemo.Table.load("Mixed", wire) do
  {:ok, value, report} ->
    check.(report.reason == "", "named wire: a clean load carries a reason")
    case Listdemo.Table.to_json("Mixed", value) do
      {:ok, text} -> check.(text == want, "named bits do not write the committed text")
      other -> check.(false, "named bits refused: " <> inspect(other))
    end
    unnamed = Map.put(value, "perms", [3, 8])
    case Listdemo.Table.to_json("Mixed", unnamed) do
      {:error, :invalid_value} -> :ok
      other -> check.(false, "flags list element with an unnamed bit answered " <> inspect(other))
    end
  other -> check.(false, "the committed wire did not load: " <> inspect(other))
end
if Process.get(:failed, false), do: System.halt(1)
IO.puts("ELIXIR_FLAGS_LIST_BIT_OK")
`, hex.EncodeToString(corpusWire), hex.EncodeToString(corpusText))
	out, err := runArmProbe(t, elixirBin, generateElixir(t, lists), listsProbe)
	if err != nil || !strings.Contains(out, "ELIXIR_FLAGS_LIST_BIT_OK") || strings.Contains(out, "warning:") {
		t.Fatalf("the committed flags list does not hold the text rule: %v\n%s", err, out)
	}

	// A FLAGS FIELD AND A FLAGS LIST ELEMENT, each with one unnamed bit. The Go
	// engine refuses each, the file read keeps both bits, and the Elixir text
	// writer refuses the loaded value as invalid_value.
	u := unitFrom(t, flagsNamedBitsSchema)
	m := tabletext.NewModel(u)
	for _, tc := range []struct {
		name  string
		build func(*tabletext.Instance)
	}{
		{"flags field", func(v *tabletext.Instance) { fieldSlot(t, v, "perm").Cell.U = 1 << 3 }},
		{"flags list element", func(v *tabletext.Instance) {
			f := fieldSlot(t, v, "perms")
			f.Elems = []tabletext.Cell{{U: 3}, {U: 8}}
			f.Count = 2
		}},
	} {
		v := m.New(m.Lookup("Root"))
		tc.build(v)
		if _, err := m.Write(v); !errors.As(err, new(*tabletext.InvalidValue)) {
			t.Fatalf("the oracle wrote %s with an unnamed bit: %v", tc.name, err)
		}
	}

	combined := m.New(m.Lookup("Root"))
	fieldSlot(t, combined, "perm").Cell.U = 1 << 3
	perms := fieldSlot(t, combined, "perms")
	perms.Elems = []tabletext.Cell{{U: 3}, {U: 8}}
	perms.Count = 2
	wire, err := tablewire.Encode(m, combined)
	if err != nil {
		t.Fatalf("the oracle did not encode the unnamed bits: %v", err)
	}
	loaded := m.New(m.Lookup("Root"))
	if ok, err := tablewire.Decode(m, loaded, wire, &report); !ok || err != nil {
		t.Fatalf("the oracle did not read its own unnamed-bit wire: %v %+v", err, report)
	}
	if got := fieldSlot(t, loaded, "perm").Cell.U; got != 1<<3 {
		t.Fatalf("the oracle read kept flags field bits %#x, want %#x", got, 1<<3)
	}
	if got := fieldSlot(t, loaded, "perms"); len(got.Elems) != 2 || got.Elems[1].U != 1<<3 {
		t.Fatalf("the oracle read kept flags list elements %+v, want a second element %#x", got.Elems, 1<<3)
	}

	flagsProbe := fmt.Sprintf(`
check = fn
  true, _ -> :ok
  false, label -> IO.puts(:stderr, "FAIL: " <> label); Process.put(:failed, true)
end
wire = Base.decode16!(%q, case: :lower)
case Flagsbits.Table.load("Root", wire) do
  {:ok, value, report} ->
    check.(report.reason == "", "unnamed wire: a clean load carries a reason")
    check.(value["perm"] == 8, "the flags field lost its unnamed bit: " <> inspect(value["perm"]))
    check.(value["perms"] == [3, 8], "the flags list element lost its unnamed bit: " <> inspect(value["perms"]))
    case Flagsbits.Table.save("Root", value) do
      {:ok, out} -> check.(out == wire, "the file write did not keep the unnamed bits")
      other -> check.(false, "the file write refused the unnamed bits: " <> inspect(other))
    end
    case Flagsbits.Table.to_json("Root", value) do
      {:error, :invalid_value} -> :ok
      other -> check.(false, "the text writer answered " <> inspect(other) <> " for the unnamed bits")
    end
  other -> check.(false, "the unnamed-bit wire did not load: " <> inspect(other))
end
if Process.get(:failed, false), do: System.halt(1)
IO.puts("ELIXIR_FLAGS_FIELD_BIT_OK")
`, hex.EncodeToString(wire))
	out, err = runArmProbe(t, elixirBin, generateElixir(t, u), flagsProbe)
	if err != nil || !strings.Contains(out, "ELIXIR_FLAGS_FIELD_BIT_OK") || strings.Contains(out, "warning:") {
		t.Fatalf("the flags field and list element do not hold the text rule: %v\n%s", err, out)
	}
}
