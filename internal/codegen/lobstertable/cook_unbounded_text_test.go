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
		{"reserved", func(b []byte) { binary.LittleEndian.PutUint32(b[start+4:], 1) }},
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
