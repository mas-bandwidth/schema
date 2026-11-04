package elixirtable

import (
	"fmt"
	"strconv"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/memceiling"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// wideRecordSchema is a record past 4 GiB (docs/SPEC-TABLES.md §11): Huge
// holds three 2 GiB Parts and a field past 4 GiB. Every leg generates and
// compiles it with each stored size and offset whole.
const wideRecordSchema = "../../../testdata/wire/tables/storage-cap/wide/Wide.schema"

// wideRecordProbe runs after every generated module compiles (the table,
// cook and block modules among them) and prints each size and offset the
// table descriptors hold. It never constructs a record.
const wideRecordProbe = `
for {name, d} <- Widerecord.Table.descriptors() do
  IO.puts("#{name}.size=#{d.size}")
  IO.puts("#{name}.record=#{d.record}")
  IO.puts("#{name}.align=#{d.align}")

  for f <- d.fields do
    if Map.has_key?(f, :entry_size), do: IO.puts("#{name}.#{f.name}.elem=#{f.entry_size}")
    if Map.has_key?(f, :elem_size) and f[:elem_t] == :record, do: IO.puts("#{name}.#{f.name}.elem=#{f.elem_size}")
    if Map.has_key?(f, :elem_size) and Map.get(f, :array) == :list, do: IO.puts("#{name}.#{f.name}.elem=#{f.elem_size}")
    if name in ["Part", "Huge"], do: IO.puts("#{name}.#{f.name}.at=#{f.cook_off}")
  end
end
`

// wideRecordWant is what ir answers for every line the probe prints.
func wideRecordWant(u *ir.Unit) map[string]string {
	want := map[string]string{}
	entry := u.Tables["Root"].Fields[2].MapEntry
	for name, st := range map[string]*ir.Struct{"Part": u.Tables["Part"], "Huge": u.Tables["Huge"], "Root": u.Tables["Root"], entry.Name: entry} {
		l := ir.RecordLayout(u, st)
		want[name+".size"] = strconv.FormatInt(l.Size, 10)
		want[name+".record"] = strconv.FormatInt(l.Size, 10)
		want[name+".align"] = strconv.FormatInt(l.Align, 10)
		for _, fl := range l.Fields {
			f := fl.Field
			if name == "Part" || name == "Huge" {
				want[name+"."+f.Name+".at"] = strconv.FormatInt(fl.Offset, 10)
			}
			_, record := f.Type.Ref.(*ir.Struct)
			switch {
			case f.IsMap():
				want[name+"."+f.Name+".elem"] = strconv.FormatInt(ir.RecordLayout(u, f.MapEntry).Size, 10)
			case f.IsList() || f.Array != ir.ArrayNone && record:
				size, _ := ir.ListElementLayout(u, f)
				want[name+"."+f.Name+".elem"] = strconv.FormatInt(size, 10)
			}
		}
	}
	return want
}

// wideRecordRun generates the wide unit, applies patch to the generated
// files (nil for none), compiles them with the probe and answers what
// disagrees with ir.
func wideRecordRun(t *testing.T, patch func(map[string][]byte)) []string {
	t.Helper()
	u := loadUnit(t, wideRecordSchema)
	files := generateElixir(t, u)
	if patch != nil {
		patch(files)
	}
	out, err := runArmProbe(t, elixirBinary(t), files, wideRecordProbe)
	if err != nil {
		t.Fatalf("probe: %v\n%s", err, out)
	}
	got := map[string]string{}
	for line := range strings.SplitSeq(out, "\n") {
		if k, v, ok := strings.Cut(line, "="); ok {
			got[k] = v
		}
	}
	want := wideRecordWant(u)
	var bad []string
	for k, w := range want {
		if got[k] != w {
			bad = append(bad, fmt.Sprintf("%s: got %q, want %s", k, got[k], w))
		}
	}
	for k, v := range got {
		if _, ok := want[k]; !ok {
			bad = append(bad, fmt.Sprintf("%s=%s: not a fact ir derives", k, v))
		}
	}
	if huge, _ := strconv.ParseInt(want["Huge.size"], 10, 64); huge <= 1<<32 {
		bad = append(bad, "Huge is not past 4 GiB, so the unit holds nothing wide")
	}
	return bad
}

// TestElixirWideRecordCompiles: the Elixir leg generates a record past 4 GiB,
// every generated module compiles, and every record size, field offset,
// element size and map entry size the descriptors hold equals ir's, past
// 2^32 where ir's is.
func TestElixirWideRecordCompiles(t *testing.T) {
	memceiling.Guard(t, memceiling.Default)
	slowtest.Gate(t, "the Elixir toolchain")
	t.Parallel()
	if bad := wideRecordRun(t, nil); len(bad) > 0 {
		t.Fatal(strings.Join(bad, "\n"))
	}
}

// TestElixirWideRecordNegativeControl narrows Huge's size to 32 bits in the
// generated Table module and requires the probe to go red.
func TestElixirWideRecordNegativeControl(t *testing.T) {
	memceiling.Guard(t, memceiling.Default)
	slowtest.Gate(t, "the Elixir toolchain")
	t.Parallel()
	u := loadUnit(t, wideRecordSchema)
	size := ir.RecordLayout(u, u.Tables["Huge"]).Size
	from := "      size: " + elixirInt(size) + ",\n      align: 8,\n      type_id: " + fmt.Sprintf("0x%X", ir.TableWireId(u.Tables["Huge"].WireName()))
	to := "      size: " + elixirInt(size&0xffffffff) + from[len("      size: "+elixirInt(size)):]
	bad := wideRecordRun(t, func(files map[string][]byte) { elixirPatch(t, files, from, to) })
	if len(bad) == 0 {
		t.Fatal("a Huge size narrowed to 32 bits still matched ir")
	}
}

// elixirInt renders n as the emitter writes an integer literal, grouped by
// thousands with underscores.
func elixirInt(n int64) string {
	s := strconv.FormatInt(n, 10)
	if len(s) <= 4 {
		return s
	}
	var b strings.Builder
	for i, c := range s {
		if i > 0 && (len(s)-i)%3 == 0 {
			b.WriteByte('_')
		}
		b.WriteRune(c)
	}
	return b.String()
}
