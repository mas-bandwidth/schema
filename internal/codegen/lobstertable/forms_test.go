package lobstertable_test

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/lobster/lobsterrun"
)

// THE FORMS A TABLE CARRIES (docs/SPEC-TABLES.md §2.12): the Lobster target
// honours a table's `forms` declaration per table. A table that leaves `json`
// and `cook` out has no text form and no cooked form — ToJson, Cook,
// CookMeasure and Open refuse it by name — while its wire is written as ever,
// and an undeclared table beside it carries every form.

const formsSchema = `package formsprobe

table Pong | forms = "wire"
{
    x int32
}

table Ping
{
    x int32
}
`

var formsWant = []string{
	"Pong json json_form_not_carried",
	"Pong cook -1",
	"Pong open cook_form_not_carried",
	"Pong wire wrote",
	"Ping json wrote",
	"Ping cook wrote",
	"Ping wire wrote",
}

func runForms(t *testing.T, patch func(string) string) []string {
	t.Helper()
	src := filepath.Join(t.TempDir(), "Probe.schema")
	if err := os.WriteFile(src, []byte(formsSchema), 0o644); err != nil {
		t.Fatal(err)
	}
	g, err := generateLoadUnit(t, []string{src})
	if err != nil {
		t.Fatalf("the surface refuses the forms probe: %v", err)
	}
	if patch != nil {
		path := filepath.Join(g.dir, "ProbeTable.lobster")
		text, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(patch(string(text))), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	var b strings.Builder
	for _, m := range g.mods {
		fmt.Fprintf(&b, "import .%s\n", m)
	}
	b.WriteString(`import .load
import .schema_save
import .json
import .cook
def probe(name:string):
    let rec = schema_rt.new_record(schema_rt.table_of(name))
    rec.fields[0].cell.i = 7
    let _, why = schema_rt.json_write(rec)
    print(name + " json " + (if why == "": "wrote" else: why))
    let size = schema_rt.cook_measure(rec, 0)
    print(name + " cook " + (if size > 0: "wrote" else: string(size)))
    if size < 0:
        print(name + " open " + schema_rt.cook_open_named(name, "", 0, false))
    let out, swhy = schema_rt.save_reasoned(rec)
    print(name + " wire " + (if swhy == "" and length(out) > 0: "wrote" else: swhy))
probe("Pong")
probe("Ping")
`)
	if err := os.WriteFile(filepath.Join(g.dir, "main.lobster"), []byte(b.String()), 0o644); err != nil {
		t.Fatal(err)
	}
	return strings.Split(strings.TrimRight(string(lobsterrun.Run(t, g.dir, "main.lobster", nil)), "\n"), "\n")
}

func TestLobsterTableHonoursTheFormsDeclaration(t *testing.T) {
	t.Parallel()
	if out := runForms(t, nil); !slices.Equal(out, formsWant) {
		t.Fatalf("the forms declaration:\n got %q\nwant %q", out, formsWant)
	}
}

// TestLobsterTableHonoursTheFormsDeclarationNegativeControl hands Pong's
// descriptor the json form back and requires its refusal to go, so the test
// above holds the emitted declaration and not something beside it. (Cook is
// not handed back: Pong has no cook table emitted for it to measure.)
func TestLobsterTableHonoursTheFormsDeclarationNegativeControl(t *testing.T) {
	t.Parallel()
	out := runForms(t, func(src string) string {
		const anchor = "json_form: false,"
		if n := strings.Count(src, anchor); n != 1 {
			t.Fatalf("%q is in the emitted module %d times, want 1", anchor, n)
		}
		return strings.Replace(src, anchor, "json_form: true,", 1)
	})
	if !slices.Contains(out, "Pong json wrote") {
		t.Fatalf("the patched descriptor still refuses Pong's json form: %q", out)
	}
}
