package ctable

import (
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	cgen "github.com/mas-bandwidth/schema/v2/internal/codegen/c"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
)

func cKeysOf(files map[string][]byte) []string {
	out := make([]string, 0, len(files))
	for k := range files {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func cHasFormFile(files map[string][]byte, suffix string) bool {
	for name := range files {
		if strings.HasSuffix(name, suffix) {
			return true
		}
	}
	return false
}

func generateCAll(t *testing.T, src string) map[string][]byte {
	t.Helper()
	u := unitFrom(t, src)
	files, err := cgen.Generate(u)
	if err != nil {
		t.Fatalf("cgen.Generate: %v", err)
	}
	tfiles, err := Generate(u)
	if err != nil {
		t.Fatalf("ctable.Generate: %v", err)
	}
	maps.Copy(files, tfiles)
	return files
}

func TestCTableEmitsOnlyItsDeclaredForms(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "the C compiler")
	defaultSchema := `package probe
table Pong { x int32 }
fixed table Fox { x int32 }
`
	def := generateCAll(t, defaultSchema)
	if !cHasFormFile(def, "Table.h") || !cHasFormFile(def, "Block.h") || !cHasFormFile(def, "View.h") {
		t.Fatalf("an undeclared unit emits every form — got %v", cKeysOf(def))
	}

	decl := generateCAll(t, "package probe\ntable Pong | forms = \"wire\"\n{\n    x int32\n}\n")
	if !cHasFormFile(decl, "Table.h") {
		t.Fatalf("the wire form is missing — got %v", cKeysOf(decl))
	}
	if cHasFormFile(decl, "Block.h") || cHasFormFile(decl, "Block.c") {
		t.Fatalf("a wire-only table emitted a block form file: %v", cKeysOf(decl))
	}
	for name, data := range decl {
		src := string(data)
		for _, forbidden := range []string{
			"PongBlock", "pong_block_open",
			"pong_cook", "pong_open",
			"pong_from_json", "pong_to_json",
		} {
			if strings.Contains(src, forbidden) {
				t.Errorf("%s leaked omitted form member %q although Pong declares only wire", name, forbidden)
			}
		}
	}

	mixed := generateCAll(t, `package probe
table Pong | forms = "wire"
{
    x int32
}
table Ping
{
    x int32
}
fixed table FoxWire | forms = "wire"
{
    x int32
}
fixed table FoxAll
{
    x int32
}
`)
	mixedSrc := ""
	for _, name := range cKeysOf(mixed) {
		mixedSrc += string(mixed[name]) + "\n"
	}
	for _, want := range []string{
		"ping_cook", "ping_open", "ping_from_json", "ping_to_json",
		"fox_all_block_open", "fox_all_cook", "fox_all_open", "fox_all_from_json", "fox_all_to_json",
		"pong_measure", "pong_save", "pong_load",
		"fox_wire_measure", "fox_wire_save", "fox_wire_load",
	} {
		if !strings.Contains(mixedSrc, want) {
			t.Errorf("mixed unit lost %q", want)
		}
	}
	for _, forbidden := range []string{
		"pong_cook", "pong_open", "pong_from_json", "pong_to_json",
		"fox_wire_block_open", "fox_wire_cook", "fox_wire_open", "fox_wire_from_json", "fox_wire_to_json",
	} {
		if strings.Contains(mixedSrc, forbidden) {
			t.Errorf("mixed unit leaked omitted form member %q", forbidden)
		}
	}

	cc, err := exec.LookPath("cc")
	if err != nil {
		return
	}
	dir := t.TempDir()
	var sources []string
	var header string
	for name, data := range mixed {
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, data, 0600); err != nil {
			t.Fatal(err)
		}
		if strings.HasSuffix(name, ".c") {
			sources = append(sources, p)
		}
		if strings.HasSuffix(name, "Table.h") && name != "Table.h" {
			header = name
		}
	}
	mainPath := filepath.Join(dir, "main.c")
	driver := "#include \"" + header + "\"\nint main(void) { return 0; }\n"
	if err := os.WriteFile(mainPath, []byte(driver), 0600); err != nil {
		t.Fatal(err)
	}
	args := []string{"-std=c99", "-O2", "-DNDEBUG", "-Wall", "-Wextra", "-Werror", "-I", dir, mainPath}
	args = append(args, sources...)
	args = append(args, "-lm", "-o", filepath.Join(dir, "probe"))
	if out, err := exec.Command(cc, args...).CombinedOutput(); err != nil {
		t.Fatalf("compile mixed unit: %v\n%s", err, out)
	}
}
