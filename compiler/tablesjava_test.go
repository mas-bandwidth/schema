// The tables tests of ONE language, in its own file so a port adds a file and
// edits no shared one (docs/CONTRIBUTING.md, "Adding a language").
package compiler

import (
	"maps"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/check"
	"github.com/mas-bandwidth/schema/v2/internal/parser"
	"github.com/mas-bandwidth/schema/v2/internal/tablenames"
)

// javaFiles generates the java target's whole output for one source.
func javaFiles(t *testing.T, src string) map[string][]byte {
	t.Helper()
	files, err := New().Generate(unitFromSource(t, src), "java", Options{})
	if err != nil {
		t.Fatalf("--lang java: %v", err)
	}
	return files
}

// TestJavaEmitsTableSources: the java target adds <Base>Table.java plus the
// unit's shared runtime for a unit with tables, and adds NOTHING for one
// without — the zero-cost property, at the grain Java has it. Java's unit scope
// is the PACKAGE and a public type lives in a file of its own name, so the
// runtime is one file per type rather than one home file, and "nothing" means
// not one of them.
func TestJavaEmitsTableSources(t *testing.T) {
	with := javaFiles(t, tableSrc)
	if _, ok := with["ProbeTable.java"]; !ok {
		t.Fatalf("--lang java emitted no ProbeTable.java for a unit with tables; got %d files", len(with))
	}
	for _, want := range []string{
		"TableReport.java", "TableWriter.java", "TableReader.java", "TableTypeInfo.java",
		"TableFieldInfo.java", "TableJson.java", "TableEnumId.java", "TableEnumValue.java",
		"TableBytes.java", "BuildVersion.java", "TableCookLayout.java",
	} {
		if _, ok := with[want]; !ok {
			t.Errorf("--lang java emitted no %s for a unit with tables", want)
		}
	}
	without := javaFiles(t, packetSrc)
	for name := range without {
		if strings.HasPrefix(name, "Table") || strings.HasSuffix(name, "Table.java") ||
			strings.HasSuffix(name, "Row.java") || strings.HasSuffix(name, "Block.java") ||
			strings.HasSuffix(name, "Cook.java") || name == "BuildVersion.java" {
			t.Errorf("--lang java emitted %s for a table-free unit — the form is zero-cost or it is not", name)
		}
	}
}

// TestJavaTablesMoveNoGeneratedPacketByte is the independence proof for this
// backend: beyond the protocol id, adding a table changes not one byte of the
// NON-TABLE generated Java.
func TestJavaTablesMoveNoGeneratedPacketByte(t *testing.T) {
	with := javaFiles(t, tableSrc)
	without := javaFiles(t, packetSrc)
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

// TestJavaRefusesPointeredTables: the Java variable-class refusal is a refusal of
// the WIRE SURFACE and of nothing else (docs/SPEC-TABLES.md §11), exactly as the
// C# one is. The two ACCELERATORS need no codec — a block and a cook are read
// where they lie — so both are emitted and the cook's <Root>Cook.open opens this
// unit's cooked assets in full.
//
// NAMED, NEVER SILENT is what this holds: no Table source at all, and every
// source the unit does emit opening with a banner that names each refused table
// and the follow-on.
func TestJavaRefusesPointeredTables(t *testing.T) {
	files := javaFiles(t, packetSrc+`
table Node
{
    value int32
    next  *Node
}
`)
	var cooks int
	for name, data := range files {
		if strings.HasSuffix(name, "Table.java") {
			t.Errorf("--lang java emitted the WIRE surface %s for a pointered unit", name)
		}
		if name == "TableJson.java" || name == "TableReader.java" || name == "TableWriter.java" {
			t.Errorf("--lang java emitted the wire runtime %s for a pointered unit", name)
		}
		if !strings.HasSuffix(name, "Cook.java") && !strings.HasSuffix(name, "Block.java") {
			continue
		}
		if strings.HasSuffix(name, "Cook.java") {
			cooks++
		}
		text := string(data)
		if !strings.Contains(text, "THE JAVA WIRE SURFACE OF THIS UNIT IS REFUSED, BY NAME") {
			t.Errorf("%s carries no refusal banner", name)
		}
		if !strings.Contains(text, "Node") || !strings.Contains(text, "is a named follow-on") {
			t.Errorf("%s does not name the table and the follow-on", name)
		}
	}
	if cooks == 0 {
		t.Error("--lang java emitted no cook reader for a pointered unit — a root is any table (docs/SPEC-TABLES.md §7)")
	}
	// and the cook's own surface is there: <Root>Cook with open and at on it
	cook := string(files["NodeCook.java"])
	if cook == "" {
		t.Fatal("--lang java emitted no NodeCook.java")
	}
	for _, want := range []string{
		"public static NodeCook open(byte[] data, int offset, long length)",
		"public int at(int slot, int size)",
		"public static TableCookInfo type()",
	} {
		if !strings.Contains(cook, want) {
			t.Errorf("NodeCook.java is missing %q", want)
		}
	}
}

// javaRuntimeIdent is the Java leg's scan: every Table*-prefixed identifier the
// emitted text carries, plus BuildVersion, which is the one unit-level name this
// backend defines that does not start with Table.
var javaRuntimeIdent = regexp.MustCompile(`\b(?:Table[A-Za-z0-9_]*|BuildVersion)\b`)

var javaBlockComment = regexp.MustCompile(`(?s)/\*.*?\*/`)

// javaEmittedNames collects the scan's answer over one map of generated Java.
// Block comments are stripped as well as line comments: Java's generated
// runtime documents itself in javadoc, and prose is not an identifier.
func javaEmittedNames(files map[string][]byte, ignore map[string]bool) map[string]bool {
	emitted := map[string]bool{}
	for _, data := range files {
		text := javaBlockComment.ReplaceAllString(string(data), "")
		for line := range strings.SplitSeq(text, "\n") {
			if i := strings.Index(line, "//"); i >= 0 {
				line = line[:i]
			}
			for _, m := range javaRuntimeIdent.FindAllString(line, -1) {
				if !ignore[m] {
					emitted[m] = true
				}
			}
		}
	}
	return emitted
}

// TestJavaTableRuntimeNamesAreClaimed is the SELF-MAINTAINING half of the §11
// promise for this backend, and it is the C# test's shape with the two things
// Java changes:
//
//   - the scan strips BLOCK comments as well as line comments, because the
//     generated Java documents itself in javadoc and prose is not an identifier;
//   - it collects BuildVersion beside the Table* family, because Java puts that
//     constant's home at PACKAGE level (a class of its own file) where C# hangs
//     it off Schema — so it is a name this backend claims and the scan has to see.
//
// The ignore set is the SCHEMA's own names, not the runtime's: a file named
// Probe.schema generates the class Probe and, when it declares a table,
// ProbeTable — neither of which is a runtime spelling.
func TestJavaTableRuntimeNamesAreClaimed(t *testing.T) {
	files := javaFiles(t, runtimeSrc)
	ignore := map[string]bool{"Probe": true, "ProbeTable": true}
	emitted := javaEmittedNames(files, ignore)
	if len(emitted) == 0 {
		t.Fatal("the scan found no runtime identifier in the emitted Java at all — the scan, not the registry, is what broke")
	}

	names := make([]string, 0, len(emitted))
	for name := range emitted {
		names = append(names, name)
	}
	sort.Strings(names)

	for _, name := range names {
		if !tablenames.Registered(name) {
			t.Errorf("the Java table emitter emits %s and internal/tablenames does not register it — "+
				"a schema declaring that name would generate Java that does not compile; register it "+
				"(with the backends that define it) in internal/tablenames", name)
		}
	}
	for _, name := range tablenames.DefinedBy(tablenames.Java) {
		if !emitted[name] {
			t.Errorf("internal/tablenames says the Java backend defines %s, but nothing in the emitted "+
				"Java names it — drop the registration or fix the backend; a claim nothing needs takes "+
				"a name away from every schema for free", name)
		}
	}
}

// TestJavaRuntimeNameScanGoesRed is the scan's own NEGATIVE CONTROL, and it is
// the control the C# test's comment asks for without running: a scan that has
// gone blind passes every registry it is pointed at, so the only way to know it
// still sees is to hand it a name nobody registered and require it to say so.
//
// The probe is injected into a COPY of the emitted text, in a shape the emitter
// does not use — a bare top-level class declaration — which is exactly the case
// a shape-dependent scan would miss.
func TestJavaRuntimeNameScanGoesRed(t *testing.T) {
	files := javaFiles(t, runtimeSrc)
	sabotaged := map[string][]byte{}
	maps.Copy(sabotaged, files)
	sabotaged["TableProbe.java"] = []byte("package probe;\n\npublic final class TableProbe {}\n")
	emitted := javaEmittedNames(sabotaged, map[string]bool{"Probe": true, "ProbeTable": true})
	if !emitted["TableProbe"] {
		t.Fatal("the scan did not see a package-level TableProbe — it is blind, and every green run above proves nothing")
	}
	if tablenames.Registered("TableProbe") {
		t.Fatal("TableProbe is registered, so the control proves nothing — pick a name the registry does not hold")
	}
}

// TestJavaRuntimeNamesAreRefusedByTheChecker is the REPRO the claim exists for:
// a schema that declares one of the Java runtime's package-level names, in a
// unit that declares a table, must be refused by the front end — because the
// generated Java would otherwise carry two public types of that name and not
// compile. TestTableRuntimeNamesAreClaimed already walks every claimed name;
// this pins the two the Java port ADDED to the claim, so a later edit that
// narrowed either would fail here by name rather than silently.
func TestJavaRuntimeNamesAreRefusedByTheChecker(t *testing.T) {
	for _, name := range []string{"TableBytes", "TableJson", "TableBlockLayout"} {
		if !tablenames.Registered(name) {
			t.Fatalf("%s is not registered at all", name)
		}
		claimed := false
		for _, c := range tablenames.Claimed() {
			if c == name {
				claimed = true
			}
		}
		if !claimed {
			t.Errorf("%s is registered but not CLAIMED — the Java backend puts it at package level, so a "+
				"schema declaring it generates two public types of one name", name)
			continue
		}
		src := "package probe\n\nenum " + name + " { A, B }\n\ntable Holder\n{\n    g " + name + "\n}\n"
		errs := checkErrors(t, src)
		if len(errs) == 0 {
			t.Errorf("a declaration named %s was accepted in a unit with a table", name)
		}
		// and the NEGATIVE CONTROL of the claim: a table-free unit keeps the name
		free := "package probe\n\nenum " + name + " { A, B }\n\ntype Holder\n{\n    g " + name + "\n}\n"
		if errs := checkErrors(t, free); len(errs) > 0 {
			t.Errorf("a TABLE-FREE unit must keep the name %s: %v", name, errs)
		}
	}
}

// TestJavaRefusesAFileNamedForARuntimeType is the collision Java's
// one-public-class-per-file rule creates and no other backend has: the CHECKER
// claims declaration names, and a schema FILE's basename is not a declaration —
// it is what names the packet emitter's class. A unit with a table and a file
// called TableReport.schema would have two TableReport.java to write, so the
// backend refuses by name rather than letting one clobber the other.
func TestJavaRefusesAFileNamedForARuntimeType(t *testing.T) {
	f, perrs := parser.Parse("TableReport.schema", []byte(tableSrc))
	if len(perrs) > 0 {
		t.Fatalf("parse: %v", perrs[0])
	}
	u, cerrs := check.Unit([]check.SourceFile{{
		Path: "TableReport.schema", Name: "TableReport.schema", Base: "TableReport",
		Bytes: []byte(tableSrc), AST: f,
	}})
	if len(cerrs) > 0 {
		t.Fatalf("check: %v", cerrs[0])
	}
	_, err := New().Generate(u, "java", Options{})
	if err == nil {
		t.Fatal("--lang java accepted a unit whose file basename is a runtime type's — one of the two would clobber the other")
	}
	if !strings.Contains(err.Error(), "TableReport.java") {
		t.Errorf("the refusal does not name the file it collides with: %v", err)
	}
	// the CONTROL: the same source under any other basename generates
	g, perrs := parser.Parse("Probe.schema", []byte(tableSrc))
	if len(perrs) > 0 {
		t.Fatalf("parse: %v", perrs[0])
	}
	ok, cerrs := check.Unit([]check.SourceFile{{
		Path: "Probe.schema", Name: "Probe.schema", Base: "Probe", Bytes: []byte(tableSrc), AST: g,
	}})
	if len(cerrs) > 0 {
		t.Fatalf("check: %v", cerrs[0])
	}
	if _, err := New().Generate(ok, "java", Options{}); err != nil {
		t.Errorf("the same unit under an ordinary basename must generate: %v", err)
	}
}

// TestJavaDescriptorsAreSafelyPublished is the test the unsafe-publication
// defect asks for, and it is a STRUCTURAL one on purpose.
//
// The defect it guards is a data race the Java memory model PERMITS rather than
// requires: a descriptor cached by a plain write can be read non-null with its
// field array still null, on a machine whose store order allows it. A test that
// tried to OBSERVE that would be a race detector — nondeterministic, green on
// x86 almost always, and worthless as a gate. What is deterministic is the
// SHAPE the emitter writes, and the shape is what was wrong.
//
// So this asserts the shape: every generated descriptor accessor is a read of a
// holder's final field, and none of them is the `if (cached != null)` idiom the
// defect had. A port that reintroduces the plain cache fails here, in every
// build, on every host.
func TestJavaDescriptorsAreSafelyPublished(t *testing.T) {
	files := javaFiles(t, runtimeSrc)
	// the four sites: the wire descriptor, the block projection, and a record's
	// block and cook descriptors
	accessors := regexp.MustCompile(`public static Table(?:Type|Block|Cook)Info ([A-Za-z0-9_]+)\(\) \{([^}]*)\}`)
	holders := 0
	for name, data := range files {
		text := string(data)
		for _, m := range accessors.FindAllStringSubmatch(text, -1) {
			holders++
			body := strings.TrimSpace(m[2])
			// a holder read, or a one-line delegation to another accessor that is
			// itself holder-backed — <Table>Cook.type() hands back its root
			// record's descriptor rather than keeping a second one
			delegates := strings.Contains(body, "Row.cookInfo()") || strings.Contains(body, "Row.blockInfo()")
			if !strings.Contains(body, "Holder.INFO") && !delegates {
				t.Errorf("%s: %s() neither reads a holder's final field nor delegates to one — its "+
					"body is %q; a plain cache publishes a mutable descriptor unsafely (JLS §17.4)",
					name, m[1], body)
			}
		}
		// and the idiom itself must be gone, wherever it appears
		for line := range strings.SplitSeq(text, "\n") {
			if strings.Contains(line, "if (info != null) { return info; }") {
				t.Errorf("%s carries the plain-cache idiom: %q", name, strings.TrimSpace(line))
			}
		}
	}
	if holders == 0 {
		t.Fatal("the scan found no descriptor accessor at all — the scan, not the emitter, is what broke")
	}
	// every holder is a private static final class whose one field is final
	for name, data := range files {
		text := string(data)
		for line := range strings.SplitSeq(text, "\n") {
			if strings.Contains(line, "Holder {") && !strings.Contains(line, "private static final class") {
				t.Errorf("%s: a descriptor holder is not a private static final class: %q", name, strings.TrimSpace(line))
			}
			if strings.Contains(line, "INFO =") && !strings.Contains(line, "static final") {
				t.Errorf("%s: a holder's INFO is not final: %q", name, strings.TrimSpace(line))
			}
		}
	}
}

// TestJavaGeneratedMethodsAreLowerCamel: Java has one naming rule and the
// generated table surface follows it, as this backend's own packet half already
// does (writeVec3, readVec3). §6.1's NAME-FIRST order is untouched — the method
// is the declaration's name and then the verb — so only the case is the port's,
// and it is the language's rather than C++'s.
func TestJavaGeneratedMethodsAreLowerCamel(t *testing.T) {
	files := javaFiles(t, runtimeSrc)
	decl := regexp.MustCompile(`^\s*public static [A-Za-z0-9_.\[\]<>]+ ([A-Za-z0-9_]+)\(`)
	seen := 0
	for name, data := range files {
		if !strings.HasSuffix(name, "Table.java") {
			continue // the runtime types are types, and a TYPE is UpperCamel in Java
		}
		for line := range strings.SplitSeq(string(data), "\n") {
			m := decl.FindStringSubmatch(line)
			if m == nil {
				continue
			}
			seen++
			if m[1][0] >= 'A' && m[1][0] <= 'Z' {
				t.Errorf("%s: generated method %s is UpperCamelCase — Java's rule, and this "+
					"backend's packet half, spell a method lowerCamel", name, m[1])
			}
		}
	}
	if seen == 0 {
		t.Fatal("the scan found no generated method at all — the scan, not the emitter, is what broke")
	}
}
