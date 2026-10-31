package javatable

import (
	"fmt"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/viewlisting"
	"github.com/mas-bandwidth/schema/v2/ir"
)

func testDeclWhat(st *ir.Struct) string {
	if st.IsTable {
		return "table"
	}
	return "type"
}

func testViewDeclarations(u *ir.Unit) (types, tables []*ir.Struct) {
	for _, n := range slices.Sorted(maps.Keys(u.Structs)) {
		if st := u.Structs[n]; st != nil && !st.IsTable {
			types = append(types, st)
		}
	}
	for _, n := range slices.Sorted(maps.Keys(u.Tables)) {
		if st := u.Tables[n]; st != nil && !st.IsMapEntry() {
			tables = append(tables, st)
		}
	}
	return types, tables
}

func testJavaString(s string) string {
	var b strings.Builder
	b.WriteByte('"')
	for _, r := range s {
		switch {
		case r == '"':
			b.WriteString(`\"`)
		case r == '\\':
			b.WriteString(`\\`)
		case r < 0x20 || r == 0x7f || (r == ' ' && len(s) > 70):
			fmt.Fprintf(&b, `\%03o`, r)
		case r < 0x80:
			b.WriteRune(r)
		case r < 0x10000:
			fmt.Fprintf(&b, `\u%04x`, r)
		default:
			r -= 0x10000
			fmt.Fprintf(&b, `\u%04x\u%04x`, 0xd800+(r>>10), 0xdc00+(r&0x3ff))
		}
	}
	b.WriteByte('"')
	return b.String()
}

func testJavaTags(tags []string) string {
	if len(tags) == 0 {
		return "new String[0]"
	}
	parts := make([]string, len(tags))
	for i, t := range tags {
		parts[i] = testJavaString(t)
	}
	return "new String[] {" + strings.Join(parts, ", ") + "}"
}

// testCookedPlace is the expectation this test holds the emitter to: a field's
// cooked-record placement read straight from the shared block-layout model
// (docs/SPEC-TABLES.md §2.7, §7, §8.1), never from cookedPlace. RecordLayout
// places each field at its natural alignment and FieldPieces spells the
// contiguous storage a cooked node is written from, so a defect inside
// cookedPlace cannot hide behind an expectation computed by cookedPlace.
func testCookedPlace(u *ir.Unit, st *ir.Struct, f *ir.Field) fieldPlace {
	p := fieldPlace{countOffset: -1, presentOffset: -1}
	fl := ir.RecordLayout(u, st).FieldByName(f.Name)
	if fl == nil {
		return p
	}
	pieces := ir.FieldPieces(u, f, fl.Offset)
	text := !f.Type.Pointer && (f.Type.Kind == ir.TBytes || f.Type.Kind == ir.TString)
	p.isArray = f.IsMap() || f.Array != ir.ArrayNone || (f.Type.Kind == ir.TBytes && !f.Type.Pointer) || f.KeyEnum != ""
	p.counted = f.Array == ir.ArrayCounted || text
	p.offset = pieces[0].Offset
	p.elementSize = pieces[0].Size
	switch {
	case f.IsMap():
		p.elementSize = ir.RecordLayout(u, f.MapEntry).Size
	case f.Array == ir.ArrayList:
		elem := *f
		elem.Array = ir.ArrayNone
		elem.ArrayBound = 0
		elem.KeyEnum = ""
		elem.KeyEnumRef = nil
		elem.Type.Optional = false
		if ref, ok := f.Type.Ref.(*ir.Struct); ok && !f.Type.Pointer {
			p.elementSize = ir.RecordLayout(u, ref).Size
		} else {
			p.elementSize = ir.FieldPieces(u, &elem, 0)[0].Size
		}
	case f.Type.Kind == ir.TBytes && !f.Type.Pointer && f.Array == ir.ArrayNone:
		p.elementSize = 1
	case p.isArray && f.ArrayBound > 0:
		p.elementSize = pieces[0].Size / f.ArrayBound
	}
	if p.counted && len(pieces) > 1 {
		p.countOffset = pieces[1].Offset
	}
	if f.IsMap() || f.Array == ir.ArrayList {
		p.counted = true
		p.countOffset = p.offset + 8
	}
	if f.Type.Optional {
		p.presentOffset = pieces[len(pieces)-1].Offset
	}
	return p
}

// TestUnitViewDescriptorsJava holds the Java reflection surface
// (docs/SPEC-TABLES.md §8) for EVERY declaration of three corpus units, not the
// card's slice:
//
//   - a descriptor per table and per `type` (§8.1, §8.2), each carrying its own
//     doc comment and tags and every field's, plus the walkable columns (cooked
//     offsets and element sizes, count and presence companions, capacity bound,
//     nested-table descriptor, cached accessors and the type reset hook);
//   - the unit registry naming every one of them, and the unit view (§8.3);
//   - the LISTING test/java-view/View.java prints from TableRegistry.unitView(),
//     compiled against the generated package and run, byte-identical to the
//     compiler's own (internal/viewlisting, §8.7) — the enum, flags and union
//     vocabulary, arm payload descriptors, constants, wire ids and text keys
//     included. `make tables-java-view` runs the same comparison over the whole
//     VIEW_CORPUS; this test keeps it in `go test`.
func TestUnitViewDescriptorsJava(t *testing.T) {
	t.Parallel()
	javac, java := javaTools(t)
	printer, err := os.ReadFile(filepath.Join("..", "..", "..", "test", "java-view", "View.java"))
	if err != nil {
		t.Fatal(err)
	}
	const unitImport = "import tabledemo.*; // UNIT"
	if !strings.Contains(string(printer), unitImport) {
		t.Fatalf("test/java-view/View.java carries no %q line for the unit to be substituted into", unitImport)
	}
	for _, dir := range []string{"examples", "arms", "maps"} {
		t.Run(dir, func(t *testing.T) {
			t.Parallel()
			u := unitFromDir(t, filepath.Join("..", "..", "..", "tables", dir))
			files, err := Generate(u)
			if err != nil {
				t.Fatal(err)
			}
			registry := string(files["TableRegistry.java"])
			if !strings.Contains(registry, "TableUnitView unitView()") {
				t.Fatalf("%s: TableRegistry has no unitView()", dir)
			}
			if _, ok := files["TableUnitView.java"]; !ok {
				t.Fatalf("%s: no TableUnitView.java", dir)
			}
			types, tables := testViewDeclarations(u)
			if len(types)+len(tables) == 0 {
				t.Fatalf("%s: the unit declares nothing to describe", dir)
			}
			for _, st := range append(types, tables...) {
				desc, ok := files[st.Name+"Descriptor.java"]
				if !ok {
					t.Fatalf("%s: no descriptor for %s %s", dir, testDeclWhat(st), st.Name)
				}
				if !strings.Contains(registry, st.Name+"Descriptor.tableType()") {
					t.Errorf("%s: the registry does not list %s", dir, st.Name)
				}
				text := string(desc)
				if want := "t.doc = " + testJavaString(st.Doc) + ";"; !strings.Contains(text, want) {
					t.Errorf("%s: %s's descriptor does not carry its doc: want %s", dir, st.Name, want)
				}
				if want := "t.tags = " + testJavaTags(st.Tags) + ";"; !strings.Contains(text, want) {
					t.Errorf("%s: %s's descriptor does not carry its tags: want %s", dir, st.Name, want)
				}
				if n := strings.Count(text, "TableFieldInfo.of("); n != len(st.Fields) {
					t.Errorf("%s: %s's descriptor carries %d fields, the declaration %d", dir, st.Name, n, len(st.Fields))
				}
				for _, f := range st.Fields {
					want1 := testJavaString(f.Doc) + ", " + testJavaTags(f.Tags) + ")"
					want2 := testJavaString(f.Doc) + ",\n                    " + testJavaTags(f.Tags) + ")"
					if !strings.Contains(text, want1) && !strings.Contains(text, want2) {
						want := want1
						t.Errorf("%s: %s.%s's descriptor row does not end in its doc and tags: want %s", dir, st.Name, f.Name, want)
					}
					p := testCookedPlace(u, st, f)
					wantAt := fmt.Sprintf(".at(%v, %v, %d, %d, %d, %d);", p.isArray, p.counted, p.offset, p.elementSize, p.countOffset, p.presentOffset)
					if !strings.Contains(text, wantAt) {
						t.Errorf("%s: %s.%s's descriptor does not carry cooked-record placement %s", dir, st.Name, f.Name, wantAt)
					}
				}
			}
			if dir == "arms" {
				// A literal pin, read off the C ABI by hand rather than
				// derived (docs/SPEC-TABLES.md §2.9, §8.1): Chain.links is a
				// list, whose sixteen-byte slot holds the reference at 0 and
				// the count at 8, and whose elements are Carry records of 24
				// bytes.
				chain := string(files["ChainDescriptor.java"])
				for _, want := range []string{`of("links"`, ".at(true, true, 0, 24, 8, -1);"} {
					if !strings.Contains(chain, want) {
						t.Errorf("arms: Chain.links's descriptor does not carry %s: %s", want, chain)
					}
				}
			}
			if dir == "examples" {
				rootDesc := string(files["RootConfigDescriptor.java"])
				if !strings.Contains(rootDesc, "TableFieldInfo.of(\"weapons\", 14, \"list\", 8L)") {
					t.Errorf("examples: RootConfigDescriptor.weapons does not carry bound=8L: %s", rootDesc)
				}
				if !strings.Contains(rootDesc, "t.create = RootConfigVariable.Value::new;") || !strings.Contains(rootDesc, "t.reset = RootConfigDescriptor::reset;") {
					t.Errorf("examples: RootConfigDescriptor missing create/reset hooks")
				}
			}

			// the listing, printed by the generated registry and compared
			// with the compiler's
			work := t.TempDir()
			src, classes := filepath.Join(work, "src"), filepath.Join(work, "classes")
			for _, d := range []string{src, classes} {
				if err := os.MkdirAll(d, 0o755); err != nil {
					t.Fatal(err)
				}
			}
			args := []string{"--release", "17", "-Xlint:all", "-Werror", "-d", classes}
			for name, body := range files {
				p := filepath.Join(src, name)
				if err := os.WriteFile(p, body, 0o644); err != nil {
					t.Fatal(err)
				}
				args = append(args, p)
			}
			view := filepath.Join(src, "View.java")
			if err := os.WriteFile(view, []byte(strings.Replace(string(printer), unitImport, "import "+u.Package+".*; // UNIT", 1)), 0o644); err != nil {
				t.Fatal(err)
			}
			args = append(args, view)
			if out, err := exec.Command(javac, args...).CombinedOutput(); err != nil {
				t.Fatalf("%s: javac: %v\n%s", dir, err, out)
			}
			printed, err := exec.Command(java, "-cp", classes, "View").Output()
			if err != nil {
				t.Fatalf("%s: the listing program failed: %v", dir, err)
			}
			pin := viewlisting.Listing(u)
			if string(printed) != pin {
				t.Fatalf("%s: the Java listing is not the compiler's.\n%s", dir, firstListingDifference(pin, string(printed)))
			}
		})
	}
}

func firstListingDifference(pin, printed string) string {
	want, got := strings.Split(pin, "\n"), strings.Split(printed, "\n")
	for i := 0; i < len(want) || i < len(got); i++ {
		w, g := "", ""
		if i < len(want) {
			w = want[i]
		}
		if i < len(got) {
			g = got[i]
		}
		if w != g {
			return "  compiler: " + w + "\n  java:     " + g
		}
	}
	return "the listings differ in trailing bytes alone"
}
