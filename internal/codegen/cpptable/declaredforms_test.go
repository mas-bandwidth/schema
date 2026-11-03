// A table that declares `forms = "..."` (docs/SPEC-TABLES.md §2.12) gets only
// the C++ surfaces of the forms it names; a form no table in the unit carries
// emits no surface at all. The default — a table with no `forms` declaration —
// is every form, today's behaviour.
//
// The expected answers are the ones the Go oracle pins for the same schema text
// (internal/codegen/gotable/declaredforms_test.go, TestGoTableEmitsOnlyItsDeclaredForms
// and TestSurfaceIntersectsDeclaredForms), transcribed into the C++ probe.
package cpptable

import (
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/cpp"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
)

func cppKeysOf(files map[string][]byte) []string {
	out := make([]string, 0, len(files))
	for k := range files {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func cppHasFormFile(files map[string][]byte, suffix string) bool {
	for name := range files {
		if strings.HasSuffix(name, suffix) {
			return true
		}
	}
	return false
}

func generateCPPAll(t *testing.T, src string) map[string][]byte {
	t.Helper()
	u := loadUnitFromSource(t, src)
	files, err := cpp.Generate(u)
	if err != nil {
		t.Fatalf("cpp.Generate: %v", err)
	}
	tfiles, err := Generate(u)
	if err != nil {
		t.Fatalf("cpptable.Generate: %v", err)
	}
	maps.Copy(files, tfiles)

	return files
}

func TestCPPTableEmitsOnlyItsDeclaredForms(t *testing.T) {
	t.Parallel()
	defaultSchema := `package probe
table Pong { x int32 }
fixed table Fox { x int32 }
`
	def := generateCPPAll(t, defaultSchema)
	if !cppHasFormFile(def, "Table.h") || !cppHasFormFile(def, "Block.h") || !cppHasFormFile(def, "View.h") {
		t.Fatalf("an undeclared unit emits every form — got %v", cppKeysOf(def))
	}

	decl := generateCPPAll(t, "package probe\ntable Pong | forms = \"wire\"\n{\n    x int32\n}\n")
	if !cppHasFormFile(decl, "Table.h") {
		t.Fatalf("the wire form is missing — got %v", cppKeysOf(decl))
	}
	if cppHasFormFile(decl, "Table.cpp") {
		t.Fatalf("a wire-only table emitted a JSON/text-form translation unit: %v", cppKeysOf(decl))
	}
	for name, data := range decl {
		src := string(data)
		for _, forbidden := range []string{
			"PongBlock", "PongBlockOpen",
			"PongCook", "PongOpen", "PongAt",
			"PongFromJson", "PongToJson", "PongLoadTextForm", "PongSaveTextForm", "PongEditTextForm",
		} {
			if strings.Contains(src, forbidden) {
				t.Errorf("%s leaked omitted form member %q although Pong declares only wire", name, forbidden)
			}
		}
	}

	mixed := generateCPPAll(t, `package probe
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
	for _, name := range cppKeysOf(mixed) {
		mixedSrc += string(mixed[name]) + "\n"
	}
	for _, want := range []string{
		"PingCook", "PingOpen", "PingFromJson", "PingToJson", "PingLoadTextForm", "PingSaveTextForm", "PingEditTextForm",
		"FoxAllBlock", "FoxAllCook", "FoxAllOpen", "FoxAllFromJson", "FoxAllToJson",
		"PongMeasure", "PongSave", "PongLoad",
		"FoxWireMeasure", "FoxWireSave", "FoxWireLoad",
	} {
		if !strings.Contains(mixedSrc, want) {
			t.Errorf("mixed unit lost %q", want)
		}
	}
	for _, forbidden := range []string{
		"PongCook", "PongOpen", "PongFromJson", "PongToJson", "PongLoadTextForm", "PongSaveTextForm", "PongEditTextForm",
		"FoxWireBlock", "FoxWireCook", "FoxWireOpen", "FoxWireFromJson", "FoxWireToJson", "FoxWireLoadTextForm", "FoxWireSaveTextForm", "FoxWireEditTextForm",
	} {
		if strings.Contains(mixedSrc, forbidden) {
			t.Errorf("mixed unit leaked omitted form member %q", forbidden)
		}
	}

	slowtest.Gate(t, "c++")
	cxx, err := exec.LookPath("c++")
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
		if strings.HasSuffix(name, ".cpp") {
			sources = append(sources, p)
		}
		if strings.HasSuffix(name, "Table.h") && name != "Table.h" {
			header = name
		}
	}
	mainPath := filepath.Join(dir, "main.cpp")
	driver := "#include \"" + header + "\"\nint main() { return 0; }\n"
	if err := os.WriteFile(mainPath, []byte(driver), 0600); err != nil {
		t.Fatal(err)
	}
	args := []string{"-std=c++17", "-O2", "-DNDEBUG", "-Wall", "-Wextra", "-Werror", "-Wshadow", "-I", dir, mainPath}
	args = append(args, sources...)
	args = append(args, "-o", filepath.Join(dir, "probe"))
	if out, err := exec.Command(cxx, args...).CombinedOutput(); err != nil {
		t.Fatalf("compile mixed unit: %v\n%s", err, out)
	}
}
