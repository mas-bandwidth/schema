// The tables tests of ONE language, in its own file so a port adds a file and
// edits no shared one (docs/CONTRIBUTING.md, "Adding a language").
package compiler

import (
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/tablenames"
)

// TestDartEmitsTableSources: the dart target adds <Base>Table.dart beside the
// packet libraries for a unit with tables, and adds NOTHING for one without —
// the same contract the cpp and cs targets hold.
func TestDartEmitsTableSources(t *testing.T) {
	c := New()
	with, err := c.Generate(unitFromSource(t, tableSrc), "dart", Options{})
	if err != nil {
		t.Fatalf("--lang dart: %v", err)
	}
	if _, ok := with["ProbeTable.dart"]; !ok {
		t.Fatalf("--lang dart emitted no ProbeTable.dart for a unit with tables; got %d files", len(with))
	}
	without, err := c.Generate(unitFromSource(t, packetSrc), "dart", Options{})
	if err != nil {
		t.Fatalf("--lang dart: %v", err)
	}
	for name := range without {
		if strings.HasSuffix(name, "Table.dart") {
			t.Errorf("--lang dart emitted %s for a table-free unit", name)
		}
	}
	// and the packet half is byte-identical either way: a table moves no
	// packet byte in this target either
	for name, data := range without {
		got, ok := with[name]
		if !ok {
			t.Errorf("file %s disappeared when a table was added", name)
			continue
		}
		if string(got) != string(data) {
			t.Errorf("file %s changed when a table was added — tables must move no packet byte", name)
		}
	}
}

// TestDartRefusesPointeredTables: the Dart READING TIER has no arena, no
// builder, no region and no node-table codec, so a unit whose closure declares
// a pointer gets no codec — and the refusal is NAMED, in a file that stays, so
// a consumer reaching for Save or Load meets an explanation rather than a
// missing name with none.
func TestDartRefusesPointeredTables(t *testing.T) {
	c := New()
	u := unitFromSource(t, packetSrc+`
table Node
{
    value int32
    next  *Node
}
`)
	files, err := c.Generate(u, "dart", Options{})
	if err != nil {
		t.Fatalf("--lang dart refused a pointered unit outright — the refusal is a FILE, not an error: %v", err)
	}
	banner := ""
	for name, data := range files {
		if !strings.HasSuffix(name, "Table.dart") {
			continue
		}
		text := string(data)
		if strings.Contains(text, "SaveBody") || strings.Contains(text, "LoadBody") {
			t.Errorf("--lang dart emitted a codec in %s for a pointered unit", name)
		}
		banner = text
	}
	if banner == "" {
		t.Fatal("--lang dart emitted no file at all for a pointered unit — the refusal has nowhere to live")
	}
	if !strings.Contains(banner, "REFUSED, BY NAME") {
		t.Error("the pointered unit's Dart file carries no refusal banner")
	}
	if !strings.Contains(banner, "Node") || !strings.Contains(banner, "named follow-on") {
		t.Error("the refusal does not name the table and the follow-on")
	}
}

// TestDartTableRuntimeNamesAreClaimed is the Dart half of the §11 promise, and
// it asks two things the C# scan does not have to.
//
// THE SPELLING IS lowerCamelCase for a free function and UpperCamel for a
// class, so the scan collects both and normalises to the registry's PascalCase
// — the two are a bijection (the packet emitter's dartName), which is what
// lets one registry cover the target.
//
// AND A PRIVATE NAME IS STILL A CLAIM. Dart's privacy is per LIBRARY, and a
// schema identifier may begin with an underscore, so a generated `_foo` at
// library scope is a collision no registry covers. The rule this backend holds
// instead is stronger and checkable: it spells NO private library-scope name at
// all, and the second half of this test is what holds it to that.
func TestDartTableRuntimeNamesAreClaimed(t *testing.T) {
	files, err := New().Generate(unitFromSource(t, runtimeSrc), "dart", Options{})
	if err != nil {
		t.Fatal(err)
	}
	ident := regexp.MustCompile(`\b[Tt]able[A-Za-z0-9_]*\b`)
	namespace := unitFromSource(t, runtimeSrc).Package
	emitted := map[string]bool{}
	for name, data := range files {
		if !dartTableSource(name) {
			continue
		}
		for line := range strings.SplitSeq(string(data), "\n") {
			if i := strings.Index(line, "//"); i >= 0 {
				line = line[:i]
			}
			for _, m := range ident.FindAllString(line, -1) {
				m = capitalizeFirst(m)
				if !strings.EqualFold(m, namespace) {
					emitted[m] = true
				}
			}
		}
	}
	if len(emitted) == 0 {
		t.Fatal("the scan found no Table* identifier in the emitted Dart at all — the scan, not the registry, is what broke")
	}
	names := make([]string, 0, len(emitted))
	for name := range emitted {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		if !tablenames.Registered(name) {
			t.Errorf("the Dart table emitter emits %s and internal/tablenames does not register it — "+
				"a schema declaring that name would generate Dart that does not compile; register it "+
				"(with the backends that define it) in internal/tablenames", name)
		}
	}
	for _, name := range tablenames.DefinedBy(tablenames.Dart) {
		if !emitted[name] {
			t.Errorf("internal/tablenames says the Dart backend defines %s, but nothing in the emitted "+
				"Dart names it — drop the registration or fix the backend; a claim nothing needs takes "+
				"a name away from every schema for free", name)
		}
	}

	// NO PRIVATE LIBRARY-SCOPE NAME. A declaration line at column zero whose
	// name begins with an underscore is the whole class: a schema can spell
	// that identifier, so the day one is emitted it is an unclaimed collision.
	private := regexp.MustCompile(`^[A-Za-z<>,\[\] ]*\b_[A-Za-z0-9_]*\s*[=(;]`)
	for name, data := range files {
		if !dartTableSource(name) {
			continue
		}
		for line := range strings.SplitSeq(string(data), "\n") {
			if line == "" || line[0] == ' ' || line[0] == '/' {
				continue // indented: a member, which claims nothing
			}
			if private.MatchString(line) {
				t.Errorf("%s declares a PRIVATE library-scope name: %q — a schema may spell that "+
					"identifier, so it is an unclaimed collision; make it a member instead", name, line)
			}
		}
	}
}
