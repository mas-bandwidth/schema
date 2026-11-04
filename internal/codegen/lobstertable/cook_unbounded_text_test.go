package lobstertable_test

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/lobster/lobsterrun"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/internal/tablecook"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// Bare list elements and map keys are reference slots into the holder's
// extent, not bounded inline strings (SPEC-TABLES section 2.13.1).
func TestLobsterCookUnboundedText(t *testing.T) {
	t.Parallel()
	row := cookWriteRow{
		name: "feature_full", unit: "feature_full", root: "Root",
		paths:  []string{"testdata/conformance/feature-witnesses/full/Features.schema"},
		wire:   "testdata/conformance/feature-witnesses/full/Root.bin",
		little: "testdata/conformance/tables/cook-write/feature_full.cook",
		big:    "testdata/conformance/tables/cook-write/feature_full-be.cook",
	}
	read := func(name string) []byte {
		b, err := os.ReadFile(filepath.Join(root, name))
		if err != nil {
			t.Fatal(err)
		}
		return b
	}
	little, big, wire := read(row.little), read(row.big), read(row.wire)
	g := generateCookUnit(t, row)
	got := runCookProbe(t, g, row, []byte("not a cook"))
	if got.measure != len(little) || !bytes.Equal(got.little, little) || !bytes.Equal(got.big, big) {
		t.Fatalf("unbounded text cook differs: measured %d, little %d, big %d; want %d", got.measure, len(got.little), len(got.big), len(little))
	}
	if !bytes.Equal(got.saved, wire) {
		t.Fatalf("unbounded text lost values after cook: saved %d bytes, want %d", len(got.saved), len(wire))
	}
	const word = "unbounded text is populated"
	start := bytes.Index(little, []byte(word)) - 8
	if start < 64 || binary.LittleEndian.Uint32(little[start:]) != uint32(len(word)) {
		t.Fatal("reference text blob not found")
	}
	var imports strings.Builder
	for _, mod := range g.mods {
		fmt.Fprintf(&imports, "import .%s\n", mod)
	}
	program := imports.String() + fmt.Sprintf(`let data = read_file("bad.cook")
assert data
let value = %s.%sReadCook(data)
print(if value: "accepted" else: "refused")
`, g.pkg, row.root)
	for _, mutation := range []struct {
		name string
		edit func([]byte)
	}{
		{"empty_nonnull", func(b []byte) { binary.LittleEndian.PutUint32(b[start:], 0) }},
		{"length_past_holder", func(b []byte) { binary.LittleEndian.PutUint32(b[start:], 0xffffffff) }},
		{"terminator", func(b []byte) { b[start+8+len(word)] = 1 }},
	} {
		t.Run(mutation.name, func(t *testing.T) {
			bad := bytes.Clone(little)
			mutation.edit(bad)
			if err := os.WriteFile(filepath.Join(g.dir, "refusal.lobster"), []byte(program), 0644); err != nil {
				t.Fatal(err)
			}
			out := lobsterrun.Run(t, g.dir, "refusal.lobster", map[string][]byte{"bad.cook": bad})
			if strings.TrimSpace(string(out)) != "refused" {
				t.Fatalf("damaged text blob was accepted: %s", out)
			}
		})
	}
	runtime := filepath.Join(g.dir, "cook.lobster")
	src, err := os.ReadFile(runtime)
	if err != nil {
		t.Fatal(err)
	}
	const anchor = "        cc_text(c, at, v.s)"
	if strings.Count(string(src), anchor) != 1 {
		t.Fatal("text extent control anchor must occur once")
	}
	if err := os.WriteFile(runtime, []byte(strings.Replace(string(src), anchor, "        return", 1)), 0644); err != nil {
		t.Fatal(err)
	}
	broken := runCookProbe(t, g, row, []byte("not a cook"))
	if bytes.Equal(broken.little, little) || bytes.Equal(broken.saved, wire) {
		t.Fatal("removing text extents escaped cook-byte or value comparison")
	}
}

// A reader follows checked relative references, not the writer's placement
// order. The oracle validates each edited cook before the native reader sees it.
func TestLobsterCookTextExtentReferences(t *testing.T) {
	slowtest.Gate(t, "native cooked text extent references and overlap control")
	path := writeSchema(t, "Probe", "package probe\ntable Root { words []string }\n")
	g, err := generateLoadUnit(t, []string{path})
	if err != nil {
		t.Fatal(err)
	}
	m := tabletext.NewModel(g.u)
	rec := m.New(m.Lookup("Root"))
	var report tabletext.Report
	if !m.Read(rec, []byte(`{"words":["alpha","beta"]}`), &report) || !report.Silent() {
		t.Fatalf("fixture read: %+v", report)
	}
	wire, err := tablewire.Encode(m, rec)
	if err != nil {
		t.Fatal(err)
	}
	var imports strings.Builder
	for _, mod := range g.mods {
		fmt.Fprintf(&imports, "import .%s\n", mod)
	}
	program := imports.String() + `import .schema_save
let data = read_file("input.cook")
let want = read_file("wire.bin")
assert data and want
let back = probe.RootReadCook(data)
if back:
    print(if schema_rt.save(back) == want: "identical" else: "changed")
else:
    print("refused")
`
	if err := os.WriteFile(filepath.Join(g.dir, "extent.lobster"), []byte(program), 0o644); err != nil {
		t.Fatal(err)
	}
	run := func(b []byte) string {
		t.Helper()
		return strings.TrimSpace(string(lobsterrun.Run(t, g.dir, "extent.lobster", map[string][]byte{"input.cook": b, "wire.bin": wire})))
	}
	var aliasControl []byte
	for _, big := range []bool{false, true} {
		t.Run(fmt.Sprintf("big=%t", big), func(t *testing.T) {
			valid, err := tablecook.Cook(m, rec, tablecook.Options{Big: big})
			if err != nil {
				t.Fatal(err)
			}
			h, err := tablecook.ReadHeader(valid, ir.BuildVersion(g.u))
			if err != nil {
				t.Fatal(err)
			}
			ord := h.Order()
			at := h.DataOffset() + ir.RecordLayout(g.u, m.Lookup("Root")).FieldByName("words").Offset
			array := at + int64(ord.Uint64(valid[at:]))
			blob0 := array + int64(ord.Uint64(valid[array:]))
			blob1 := array + 8 + int64(ord.Uint64(valid[array+8:]))
			end := h.DataOffset() + h.DataLength
			// This fixture's two 8-byte references and two padded text blobs
			// occupy a contiguous extent. Move the references behind the blobs.
			if blob0 != array+16 || blob1 != blob0+16 || end != blob1+16 {
				t.Fatal("unexpected two-string extent layout")
			}
			backward := bytes.Clone(valid)
			copy(backward[array:array+32], valid[blob0:end])
			copy(backward[array+32:end], valid[array:blob0])
			newArray := array + 32
			ord.PutUint64(backward[at:], uint64(newArray-at))
			ord.PutUint64(backward[newArray:], uint64(array-newArray))
			ord.PutUint64(backward[newArray+8:], uint64(array+16-(newArray+8)))
			padded := bytes.Clone(valid)
			ord.PutUint32(padded[blob0+4:], 0xdeadbeef)
			alias := bytes.Clone(valid)
			ord.PutUint64(alias[array+8:], uint64(blob0-(array+8)))
			if !big {
				aliasControl = alias
			}
			for _, c := range []struct {
				name string
				data []byte
				want string
			}{
				{"canonical", valid, "identical"},
				{"pad_is_ignored", padded, "identical"},
				{"backward_references", backward, "identical"},
				{"aliased_blobs", alias, "refused"},
			} {
				t.Run(c.name, func(t *testing.T) {
					_, checkErr := tablecook.Check(m, c.data)
					if (checkErr == nil) != (c.want == "identical") {
						t.Fatalf("oracle disagrees: %v", checkErr)
					}
					if got := run(c.data); got != c.want {
						t.Fatalf("native %s, want %s", got, c.want)
					}
				})
			}
			for _, c := range []struct {
				name  string
				delta int64
			}{
				{"before_holder", h.DataOffset() - array - 8},
				{"past_holder", end - array},
				{"header_past_holder", end - array - 4},
				{"unaligned", blob0 - array + 1},
				{"positive_overflow", 0x7fffffffffffffff},
				{"negative_overflow", -0x8000000000000000},
			} {
				t.Run(c.name, func(t *testing.T) {
					bad := bytes.Clone(valid)
					ord.PutUint64(bad[array:], uint64(c.delta))
					if _, err := tablecook.Check(m, bad); err == nil {
						t.Fatal("oracle accepted malformed reference")
					}
					if got := run(bad); got != "refused" {
						t.Fatalf("native %s, want refused", got)
					}
				})
			}
		})
	}
	// Removing only the overlap condition must admit the aliased second
	// string and change its value. Neither the fixture nor the oracle changes.
	runtime := filepath.Join(g.dir, "cook.lobster")
	src, err := os.ReadFile(runtime)
	if err != nil {
		t.Fatal(err)
	}
	const anchor = "if start < other.end and other.start < end:"
	if strings.Count(string(src), anchor) != 1 {
		t.Fatal("overlap control anchor must occur once")
	}
	if err := os.WriteFile(runtime, []byte(strings.Replace(string(src), anchor, "if false and start < other.end and other.start < end:", 1)), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := run(aliasControl); got != "changed" {
		t.Fatalf("overlap control %s, want accepted changed value", got)
	}
}
