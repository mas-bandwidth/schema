package lobstertable_test

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/compiler"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/lobster/lobsterrun"
)

// blockProbe is the main that reads each block image, reads it reflectively
// (docs/SPEC-TABLES.md §19.2), rebuilds it through §19.1's builder and prints
// the canonical row dump; each construct is named with its count.
func blockProbe(roots, files []string) string {
	var b strings.Builder
	b.WriteString("import .RenderTable\nimport .PaddedTable\nimport .block_form\n")
	b.WriteString(`def esc(s:string) -> string:
    var o = ""
    for(s) c, i:
        if c == 10:
            o += "|"
        else:
            o += substring(s, i, 1)
    return o
def run(name:string, root:string, file:string):
    let data = read_file(file)
    assert data
    let bl = schema_rt.lookup_block(root)
    assert bl
    print("construct {name} {root} projection 1")
    for(bl.projection.fields) bf:
        if bf.out_of_line:
            print("construct {name} array {bf.name} {read_uint32_le(data, bf.offset + 8)}")
    let rec = schema_rt.block_read(root, data)
    assert rec
    let built = schema_rt.block_build(root, rec)
    var same = length(data) <= length(built)
    var first = -1
    var i = 0
    while same and i < length(data):
        if read_uint8_le(built, i) != read_uint8_le(data, i):
            same = false
            first = i
        i++
    print("rebuild {name} " + (if same: "equal" else: "different at {first}"))
    let d1 = schema_rt.block_dump(root, data)
    let d2 = schema_rt.block_dump(root, built)
    print("redump {name} " + (if d1 == d2: "equal" else: "different"))
    let n = (length(d1) + 39999) / 40000
    print("{n}")
    var j = 0
    while j < n:
        print(esc(substring(d1, j * 40000, min(40000, length(d1) - j * 40000))))
        j++
`)
	for i := range roots {
		fmt.Fprintf(&b, "run(%q, %q, \"b%d\")\n", strings.Split(files[i], "/")[len(strings.Split(files[i], "/"))-1], roots[i], i)
	}
	return b.String()
}

// TestLobsterBlockForm reads the two block images of the tables manifest
// through the emitted layout (docs/SPEC-TABLES.md §19.2), compares THE ROW
// DUMP to the pinned oracle byte for byte, rebuilds each block through §19.1's
// builder and requires the bytes and a second read to agree.
func TestLobsterBlockForm(t *testing.T) {
	t.Parallel()
	units, _ := readManifest(t)
	paths, err := compiler.GatherPaths(prefix(units["blockdemo"]))
	if err != nil {
		t.Fatal(err)
	}
	g, err := generateLoadUnit(t, paths)
	if err != nil {
		t.Fatalf("blockdemo is refused: %v", err)
	}
	roots := []string{"RenderFrame", "PaddedFrame"}
	files := []string{"testdata/wire/tables/block_render.bin", "testdata/wire/tables/block_padded.bin"}
	dumps := []string{"testdata/conformance/tables/block/block_render.dump", "testdata/conformance/tables/block/block_padded.dump"}
	inputs := map[string][]byte{}
	for i := range roots {
		data, err := os.ReadFile(filepath.Join(root, files[i]))
		if err != nil {
			t.Fatal(err)
		}
		inputs[fmt.Sprintf("b%d", i)] = data
	}
	if err := os.WriteFile(filepath.Join(g.dir, "main.lobster"), []byte(blockProbe(roots, files)), 0o644); err != nil {
		t.Fatal(err)
	}
	out := strings.Split(strings.TrimRight(string(lobsterrun.Run(t, g.dir, "main.lobster", inputs)), "\n"), "\n")
	for i, name := range roots {
		want, err := os.ReadFile(filepath.Join(root, dumps[i]))
		if err != nil {
			t.Fatal(err)
		}
		constructs := 0
		for len(out) > 0 && strings.HasPrefix(out[0], "construct ") {
			constructs++
			out = out[1:]
		}
		if constructs < 2 {
			t.Fatalf("%s: no construct count lines", name)
		}
		if !strings.HasPrefix(out[0], "rebuild block_") || !strings.HasSuffix(out[0], " equal") {
			t.Fatalf("%s: the builder disagrees with the pinned image: %q", name, out[0])
		}
		out = out[1:]
		if !strings.HasSuffix(out[0], " equal") {
			t.Fatalf("%s: a rebuilt block reads differently: %q", name, out[0])
		}
		out = out[1:]
		var n int
		if _, err := fmt.Sscanf(out[0], "%d", &n); err != nil || len(out) < 1+n {
			t.Fatalf("%s: output is not count then dump: %q", name, out[0])
		}
		got := strings.ReplaceAll(strings.Join(out[1:1+n], ""), "|", "\n")
		out = out[1+n:]
		if got != string(want) {
			t.Fatalf("%s: row dump disagrees with %s\n got: %q\nwant: %q", name, dumps[i], got, string(want))
		}
	}
	if len(out) != 0 {
		t.Fatalf("%d lines past the last block", len(out))
	}
}

// TestLobsterBlockFormNegativeControl breaks the emitted runtime's scalar read
// and requires the row dump to disagree with the pinned oracle.
func TestLobsterBlockFormNegativeControl(t *testing.T) {
	t.Parallel()
	units, _ := readManifest(t)
	paths, err := compiler.GatherPaths(prefix(units["blockdemo"]))
	if err != nil {
		t.Fatal(err)
	}
	g, err := generateLoadUnit(t, paths)
	if err != nil {
		t.Fatalf("blockdemo is refused: %v", err)
	}
	runtime := filepath.Join(g.dir, "block_form.lobster")
	data, err := os.ReadFile(runtime)
	if err != nil {
		t.Fatal(err)
	}
	const mark = `out += "  {name} = {bscalar(sl, bf)}\n"`
	if !strings.Contains(string(data), mark) {
		t.Fatalf("the runtime no longer carries the scalar line the control breaks")
	}
	broken := strings.Replace(string(data), mark, `out += ""`, 1)
	if err := os.WriteFile(runtime, []byte(broken), 0o644); err != nil {
		t.Fatal(err)
	}
	roots := []string{"RenderFrame"}
	files := []string{"testdata/wire/tables/block_render.bin"}
	wire, err := os.ReadFile(filepath.Join(root, files[0]))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(g.dir, "main.lobster"), []byte(blockProbe(roots, files)), 0o644); err != nil {
		t.Fatal(err)
	}
	out := string(lobsterrun.Run(t, g.dir, "main.lobster", map[string][]byte{"b0": wire}))
	want, err := os.ReadFile(filepath.Join(root, "testdata/conformance/tables/block/block_render.dump"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out, string(want)) {
		t.Fatalf("the sabotaged read still produced the pinned dump")
	}
}
