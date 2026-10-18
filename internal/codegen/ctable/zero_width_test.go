package ctable

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/check"
	"github.com/mas-bandwidth/schema/v2/internal/parser"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// cConformanceInputs is every input the C conformance leg generates
// (make/c.mk, build/tables-generated-c/.stamp), relative to the repository
// root: a file is one unit, a directory is every *.schema in it as one unit.
var cConformanceInputs = []string{
	"test/tables/R2.schema", "test/tables/R1.schema",
	"test/tables/W1.schema", "test/tables/W2.schema", "test/tables/G1.schema",
	"test/tables/RT1.schema", "test/tables/K1.schema", "test/tables/K2.schema",
	"test/tables/A1.schema", "test/tables/A2.schema", "test/tables/M1.schema", "test/tables/M2.schema",
	"test/tables/V1.schema", "test/tables/V2.schema",
	"test/tables/P1.schema", "test/tables/P2.schema", "test/tables/P3.schema",
	"test/tables/Scalars2.schema", "test/tables/JsonKeys.schema",
	"tables/maps", "tables/lists", "tables/arms", "tables/stream", "tables/blobs",
	"tables/vocab9", "tables/vocab", "tables/backend", "tables/messages",
	"tables/examples", "tables/pointers", "tables/block", "tables/blockhome",
	"tables/scalars", "examples-wide",
}

// zeroWidthUnit loads one conformance input as a unit.
func zeroWidthUnit(t *testing.T, rel string) *ir.Unit {
	t.Helper()
	path := filepath.Join("..", "..", "..", filepath.FromSlash(rel))
	paths := []string{path}
	if !strings.HasSuffix(path, ".schema") {
		var err error
		if paths, err = filepath.Glob(filepath.Join(path, "*.schema")); err != nil || len(paths) == 0 {
			t.Fatalf("%s: no schema files (%v)", rel, err)
		}
	}
	var sources []check.SourceFile
	for _, p := range paths {
		data, err := os.ReadFile(p)
		if err != nil {
			t.Fatal(err)
		}
		ast, diags := parser.Parse(p, data)
		if len(diags) > 0 {
			t.Fatalf("%s: parse: %v", rel, diags[0])
		}
		name := filepath.Base(p)
		sources = append(sources, check.SourceFile{Path: p, Name: name, Base: strings.TrimSuffix(name, ".schema"), Bytes: data, AST: ast})
	}
	u, diags := check.Unit(sources)
	if len(diags) > 0 {
		t.Fatalf("%s: check: %v", rel, diags[0])
	}
	return u
}

// TestCTableNeverEmitsZeroWidthInteger pins the owner decision (Glenn,
// 2026-10-16): no zero-width integer is ever valid. The C leg of R2.schema and
// of every conformance schema generates — no panic, no refusal — and not one
// line of it spells `uint0_t`, `int0_t`, `table_fixed_put0`,
// `table_writer_put0` or `table_reader_get0`.
//
// The red at BASE (sprint/schema-go 44a163585) is a text, bytes or array UNION
// ARM read as a scalar element by the fixed-form writer: A2.schema's
// `c string(8)`, Messages.schema's `note` and `blob`, Caption.schema's `wide`
// and `narrow` each printed `table_fixed_put0( ..., (uint0_t) ... )`, which no
// C compiler builds — so `make conformance` died in build/conformance-c.
//
// It is a unit test: it reads repository files and calls the generator in
// process, no clock, socket or service.
func TestCTableNeverEmitsZeroWidthInteger(t *testing.T) {
	t.Parallel()
	banned := []string{"uint0_t", "int0_t", "table_fixed_put0(", "table_writer_put0(", "table_reader_get0("}
	for _, rel := range cConformanceInputs {
		u := zeroWidthUnit(t, rel)
		var files map[string][]byte
		var err error
		func() {
			defer func() {
				if r := recover(); r != nil {
					t.Fatalf("%s: the C table emitter panicked: %v", rel, r)
				}
			}()
			files, err = GenerateLineage(u, nil)
		}()
		if err != nil {
			t.Errorf("%s: the C table emitter refused: %v", rel, err)
			continue
		}
		for name, data := range files {
			for i, line := range strings.Split(string(data), "\n") {
				for _, b := range banned {
					if strings.Contains(line, b) {
						t.Errorf("%s: %s:%d prints a zero-width integer (%s): %s", rel, name, i+1, b, strings.TrimSpace(line))
						break
					}
				}
			}
		}
	}
}

// TestCTableTextArmFixedStore pins WHAT the fixed writer stores for a text
// union arm now that it is not a scalar: the used length as a 32-bit count,
// then that many bytes, read from the arm's companioned storage.
func TestCTableTextArmFixedStore(t *testing.T) {
	t.Parallel()
	files, err := GenerateLineage(zeroWidthUnit(t, "test/tables/A2.schema"), nil)
	if err != nil {
		t.Fatal(err)
	}
	header := ""
	for name, data := range files {
		if strings.HasSuffix(name, "Table.h") {
			header += string(data)
		}
	}
	for _, want := range []string{
		"table_fixed_put32( b + 5, (uint32_t) value->value.as.c.value_length );",
		"memcpy( b + 9, value->value.as.c.value, (size_t) ( value->value.as.c.value_length ) );",
	} {
		if !strings.Contains(header, want) {
			t.Errorf("A2's text arm `c` is not stored as length then bytes; missing:\n%s", want)
		}
	}
}

// TestCTableRefusesAPlantedZeroWidth is the NEGATIVE CONTROL: a width of 0
// planted under the central check is refused by name — the table, the field and
// the width — at the IR's check and through the emitter's own gate, and a
// Generate that met one would fail on that sentence rather than print
// `uint0_t`.
func TestCTableRefusesAPlantedZeroWidth(t *testing.T) {
	t.Parallel()
	for _, ok := range []int64{1, 2, 4, 8} {
		if err := ir.PrintedIntegerWidthRefusal("Cfg", "grade", ok); err != nil {
			t.Errorf("width %d is a valid printed integer and was refused: %v", ok, err)
		}
	}
	for _, bad := range []int64{0, 3, 16} {
		err := ir.PrintedIntegerWidthRefusal("Cfg", "grade", bad)
		if err == nil {
			t.Errorf("width %d was not refused", bad)
			continue
		}
		for _, name := range []string{"Cfg", "grade", strconv.FormatInt(bad, 10) + "-byte"} {
			if !strings.Contains(err.Error(), name) {
				t.Errorf("width %d refusal does not name %q: %v", bad, name, err)
			}
		}
	}

	// THE EMITTER'S GATE: plant a zero-width integer field under a fixed write
	// body and watch the generator record the refusal.
	g := &tableGen{unit: zeroWidthUnit(t, "test/tables/R2.schema"), includes: map[string]bool{}}
	g.fixedTable = "Planted"
	planted := &ir.Field{Name: "nothing", Type: ir.FieldType{Kind: ir.TInt, Width: 0}}
	g.emitFixedWriteElement(planted, 0, "b", "value->nothing", 4)
	if g.widthErr == nil {
		t.Fatal("a planted zero-width integer was emitted without a refusal")
	}
	for _, name := range []string{"Planted", "nothing", "0-byte"} {
		if !strings.Contains(g.widthErr.Error(), name) {
			t.Errorf("the planted refusal does not name %q: %v", name, g.widthErr)
		}
	}
}
