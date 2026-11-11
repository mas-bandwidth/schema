package elixirtable

import (
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

const textOffShapeSchema = `package textcheck

union Origin {
    note string(24)
}

fixed table Root {
    label   string(8)
    origins [..2]Origin
}
`

type textOffShapeBits struct {
	b []byte
	n int
}

func (w *textOffShapeBits) put(v uint64, n int) {
	for i := 0; i < n; i++ {
		if w.n%8 == 0 {
			w.b = append(w.b, 0)
		}
		if v>>uint(i)&1 == 1 {
			w.b[w.n/8] |= 1 << uint(w.n%8)
		}
		w.n++
	}
}

func (w *textOffShapeBits) align() {
	for w.n%8 != 0 {
		w.put(0, 1)
	}
}

func (w *textOffShapeBits) bytes(p []byte) {
	for _, by := range p {
		w.put(uint64(by), 8)
	}
}

func namedField(t *testing.T, st *ir.Struct, name string) *ir.Field {
	t.Helper()
	for _, f := range st.Fields {
		if f.Name == name {
			return f
		}
	}
	t.Fatalf("table %s has no field %s", st.Name, name)
	return nil
}

func TestElixirLoadMessagesRefusesTextOffShape(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "an external toolchain")

	u := unitFrom(t, textOffShapeSchema)
	m := tabletext.NewModel(u)
	root := findTable(t, u, "Root")
	label := namedField(t, root, "label")
	origins := namedField(t, root, "origins")
	un := tabletext.UnionOf(origins)
	if un == nil {
		t.Fatal("the origins field holds no union")
	}
	var note ir.UnionVariant
	for _, arm := range un.Variants {
		if arm.Name == "note" {
			note = arm
		}
	}
	if note.Name == "" {
		t.Fatal("the Origin union has no note arm")
	}

	vocabulary := tablewire.MessageVocabulary(u)
	slots := make(map[string]uint64, len(vocabulary))
	for i, e := range vocabulary {
		slots[e.Key()] = uint64(i + 1)
	}
	slot := func(entry ir.TableVocabularyEntry) uint64 {
		s, ok := slots[entry.Key()]
		if !ok {
			t.Fatalf("no announced slot for %x at kind %d", entry.Id, entry.Kind)
		}
		return s
	}

	refBits := ir.TableMessageRefBits(len(vocabulary))
	labelEntry := ir.TableFieldEntry(label)
	originsEntry := ir.TableFieldEntry(origins)
	noteEntry := ir.TableArmEntry(note)

	fieldBatch := func(text []byte) []byte {
		w := &textOffShapeBits{}
		w.put(slot(labelEntry), refBits)
		w.put(uint64(len(text)), ir.TableMessageBitsRequired(0, labelEntry.Shape.Max))
		w.align()
		w.bytes(text)
		w.put(0, refBits)
		w.align()
		return append([]byte{ir.TableWireMessageForm, 0}, w.b...)
	}

	arrayBatch := func(text []byte) []byte {
		w := &textOffShapeBits{}
		w.put(slot(originsEntry), refBits)
		w.put(1, ir.TableMessageCountBits(originsEntry.Shape))
		w.put(slot(noteEntry), refBits)
		w.put(uint64(len(text)), ir.TableMessageBitsRequired(0, noteEntry.Shape.Max))
		w.align()
		w.bytes(text)
		w.put(0, refBits)
		w.align()
		return append([]byte{ir.TableWireMessageForm, 0}, w.b...)
	}

	type textCase struct {
		name  string
		batch []byte
	}

	var invalid []textCase
	for _, c := range []struct {
		name string
		text []byte
	}{
		{"field_ill_formed", []byte{'a', 0xff, 'b'}},
		{"field_zero", []byte{'a', 0, 'b'}},
		{"field_past_max", []byte(strings.Repeat("a", int(labelEntry.Shape.Max)+1))},
		{"array_ill_formed", []byte{'a', 0xff, 'b'}},
		{"array_zero", []byte{'a', 0, 'b'}},
		{"array_past_max", []byte(strings.Repeat("a", int(noteEntry.Shape.Max)+1))},
	} {
		batch := fieldBatch(c.text)
		if strings.HasPrefix(c.name, "array_") {
			batch = arrayBatch(c.text)
		}
		invalid = append(invalid, textCase{name: c.name, batch: batch})
	}

	var lawful []textCase
	for _, c := range []struct {
		name string
		text []byte
	}{
		{"field_exactly_max", []byte(strings.Repeat("a", int(labelEntry.Shape.Max)))},
		{"field_multibyte_utf8", []byte("h\u00e9llo")},
		{"array_exactly_max", []byte(strings.Repeat("a", int(noteEntry.Shape.Max)))},
		{"array_multibyte_utf8", []byte(strings.Repeat("\u00e9", int(noteEntry.Shape.Max)/2))},
	} {
		batch := fieldBatch(c.text)
		if strings.HasPrefix(c.name, "array_") {
			batch = arrayBatch(c.text)
		}
		lawful = append(lawful, textCase{name: c.name, batch: batch})
	}

	for _, c := range invalid {
		if engineMessageAccepts(t, u, m, c.batch) {
			t.Fatalf("%s: the engine accepted a batch whose text is off shape", c.name)
		}
	}
	for _, c := range lawful {
		if !engineMessageAccepts(t, u, m, c.batch) {
			t.Fatalf("%s: the engine refused a lawful text batch", c.name)
		}
	}

	dir := t.TempDir()
	announcement := filepath.Join(dir, "announcement.bin")
	if err := os.WriteFile(announcement, tablewire.Announce(u), 0o600); err != nil {
		t.Fatal(err)
	}
	writeBatch := func(c textCase) string {
		path := filepath.Join(dir, c.name+".bin")
		if err := os.WriteFile(path, c.batch, 0o600); err != nil {
			t.Fatal(err)
		}
		return path
	}
	var invalidPairs, lawfulPairs []string
	for _, c := range invalid {
		invalidPairs = append(invalidPairs, fmt.Sprintf("{%s, %s}", elixirString(c.name), elixirString(writeBatch(c))))
	}
	for _, c := range lawful {
		lawfulPairs = append(lawfulPairs, fmt.Sprintf("{%s, %s}", elixirString(c.name), elixirString(writeBatch(c))))
	}

	tables, err := Generate(u)
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	module, ok := tables[TableMessageModule+".ex"]
	if !ok {
		t.Fatalf("the unit grows no %s.ex", TableMessageModule)
	}
	ns := ir.GoExportName(u.Package)
	probe := fmt.Sprintf(`defmodule Probe do
  alias %s.TableMessage, as: M

  def check(true, _what), do: :ok

  def check(false, what) do
    IO.puts(:stderr, "FAIL: " <> what)
    Process.put(:failed, true)
  end

  def run do
    vocab =
      case M.announce_read(File.read!(%s)) do
        {:ok, v} -> v
        other -> raise "announce_read refused the bytes: #{inspect(other)}"
      end

    for {name, path} <- [%s] do
      case M.load_messages(vocab, File.read!(path)) do
        {:malformed, 0, []} -> :ok
        other -> check(false, "#{name}: load did not refuse the whole batch: #{inspect(other)}")
      end
    end

    for {name, path} <- [%s] do
      batch = File.read!(path)

      case M.load_messages(vocab, batch) do
        {:ok, values} ->
          case M.save_messages(vocab, values) do
            {:ok, again} -> check(again == batch, "#{name}: save did not reproduce the batch")
            other -> check(false, "#{name}: save refused: #{inspect(other)}")
          end

        other ->
          check(false, "#{name}: lawful twin did not load: #{inspect(other)}")
      end
    end

    if Process.get(:failed), do: System.halt(1), else: IO.puts("ok")
  end
end

Probe.run()
`, ns, elixirString(announcement), strings.Join(invalidPairs, ", "), strings.Join(lawfulPairs, ", "))

	out, err := runElixirModuleProbe(t, module, probe)
	if err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
}

func engineMessageAccepts(t *testing.T, u *ir.Unit, m *tabletext.Model, batch []byte) bool {
	t.Helper()
	var v tablewire.Vocabulary
	var report tabletext.Report
	if err := v.AnnounceRead(tablewire.Announce(u), &report); err != nil {
		t.Fatalf("the engine refused its own announcement: %v", err)
	}
	inst := m.New(m.Lookup("Root"))
	n, ok, err := tablewire.DecodeMessages(m, []*tabletext.Instance{inst}, batch, &v, &report)
	return ok && err == nil && n == 1
}
