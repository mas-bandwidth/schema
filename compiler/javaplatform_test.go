package compiler

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/internal/testproc"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// GENERATED JAVA NAMES EVERY PLATFORM TYPE FROM THE PACKAGE ROOT. A class of
// the unit takes a simple name in the whole package: a schema file Double.schema
// is the outer class demo.Double, and inside every other file of package demo
// that class shadows java.lang.Double (JLS §6.4.1); a nested class does the
// same inside its own file, and also shadows a single-type import. So the
// emitters write java.lang.Double, java.util.Arrays, java.lang.invoke.VarHandle
// and never a bare platform name or an import, and no legal schema name breaks
// the generated code. The one name a declaration cannot take is `java` itself,
// which obscures the package (JLS §6.4.2); it is refused by name, with the class
// names javac refuses outright (reserved and restricted words).
//
// The corpus lives in testdata/javaplatform:
//
//	java-lang.txt   every public type of java.lang (JDK 21, javap -public)
//	siblings/       one schema file per platform type the emitters name, beside
//	                bench/corpus/Bench.schema and FixedTable.schema (package bench),
//	                and Clean.schema, the control
//	nested/         one unit declaring a type per platform type the emitters name
//	refused/        one file per class name javac cannot take or `java` obscures
//	accepted/       the control for refused/: an ordinary name generates and compiles

const javaPlatformDir = "testdata/javaplatform"

// javaLangTypes reads the committed java.lang list.
func javaLangTypes(t *testing.T) map[string]bool {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(javaPlatformDir, "java-lang.txt"))
	if err != nil {
		t.Fatal(err)
	}
	out := map[string]bool{}
	for l := range strings.SplitSeq(string(data), "\n") {
		if l = strings.TrimSpace(l); l != "" && !strings.HasPrefix(l, "#") {
			out[l] = true
		}
	}
	if len(out) < 100 {
		t.Fatalf("java-lang.txt lists %d types; want the whole package", len(out))
	}
	return out
}

// javaTok is one token of generated Java code: an identifier, or one
// punctuation byte. Comments, strings, chars and text blocks are not code.
type javaTok struct {
	s     string
	ident bool
	line  int
}

func lexJavaCode(src string) []javaTok {
	var out []javaTok
	line := 1
	isIdent := func(c byte) bool {
		return c == '_' || c == '$' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c >= 0x80
	}
	for i := 0; i < len(src); {
		c := src[i]
		switch {
		case c == '\n':
			line++
			i++
		case c == ' ' || c == '\t' || c == '\r':
			i++
		case strings.HasPrefix(src[i:], "//"):
			for i < len(src) && src[i] != '\n' {
				i++
			}
		case strings.HasPrefix(src[i:], "/*"), strings.HasPrefix(src[i:], `"""`):
			open, shut := "/*", "*/"
			if c == '"' {
				open, shut = `"""`, `"""`
			}
			end := strings.Index(src[i+len(open):], shut)
			if end < 0 {
				return out
			}
			n := len(open) + end + len(shut)
			line += strings.Count(src[i:i+n], "\n")
			i += n
		case c == '"' || c == '\'':
			j := i + 1
			for j < len(src) && src[j] != c && src[j] != '\n' {
				if src[j] == '\\' {
					j++
				}
				j++
			}
			out = append(out, javaTok{s: `""`, line: line})
			i = j + 1
		case isIdent(c) && !(c >= '0' && c <= '9'):
			j := i
			for j < len(src) && isIdent(src[j]) {
				j++
			}
			out = append(out, javaTok{s: src[i:j], ident: true, line: line})
			i = j
		case c >= '0' && c <= '9':
			j := i
			for j < len(src) && (isIdent(src[j]) || src[j] == '.') {
				j++
			}
			out = append(out, javaTok{s: "0", line: line})
			i = j
		default:
			out = append(out, javaTok{s: string(c), line: line})
			i++
		}
	}
	return out
}

// javaPlatformUse is what the scan finds in one generated file: each platform
// type spelled in full (java.<package>.<Type>), and each bare java.lang name,
// import or package-root reference that is not.
type javaPlatformUse struct {
	qualified map[string]bool // the simple names reached as java.<pkg>.<Name>
	bare      []string        // "<file>:<line>: <what>"
}

// scanJavaPlatform scans one generated Java file. own is the unit's declared
// names (its outer classes and nested types), which name its own classes.
func scanJavaPlatform(file, src string, lang, own map[string]bool, use *javaPlatformUse) {
	toks := lexJavaCode(src)
	for i := 0; i < len(toks); i++ {
		tk := toks[i]
		if !tk.ident {
			continue
		}
		if tk.s == "import" {
			var b strings.Builder
			for j := i + 1; j < len(toks) && toks[j].s != ";"; j++ {
				b.WriteString(toks[j].s)
			}
			use.bare = append(use.bare, fmt.Sprintf("%s:%d: import %s", file, tk.line, b.String()))
			continue
		}
		member := i > 0 && toks[i-1].s == "."
		if tk.s == "java" && !member && i+2 < len(toks) && toks[i+1].s == "." {
			// java.<pkg>...<Type>: the first segment that starts upper case
			for j := i + 2; j < len(toks) && toks[j].ident; j += 2 {
				if s := toks[j].s; s[0] >= 'A' && s[0] <= 'Z' {
					use.qualified[s] = true
					i = j
					break
				}
				if j+1 >= len(toks) || toks[j+1].s != "." {
					break
				}
			}
			continue
		}
		if member || !lang[tk.s] || own[tk.s] {
			continue
		}
		use.bare = append(use.bare, fmt.Sprintf("%s:%d: %s", file, tk.line, tk.s))
	}
}

// javaOwnNames is every simple name a unit gives a class of its own: the
// outer classes its files name and its declarations.
func javaOwnNames(u *ir.Unit) map[string]bool {
	out := map[string]bool{}
	for _, f := range u.Files {
		out[f.Base] = true
	}
	for _, s := range u.Structs {
		out[s.Name] = true
	}
	for _, e := range u.Enums {
		out[e.Name] = true
	}
	for _, f := range u.Flags {
		out[f.Name] = true
	}
	for _, un := range u.Unions {
		out[un.Name] = true
	}
	for _, tb := range u.Tables {
		out[tb.Name] = true
	}
	return out
}

// javaPlatformUnits are the units the scan reads, by label: the corpora every
// Java leg generates, and the committed shadow units.
func javaPlatformUnits(t *testing.T) map[string][]string {
	t.Helper()
	units := map[string][]string{
		"examples":        {"../examples"},
		"examples128":     {"../examples128"},
		"bench":           {"../bench/corpus/Bench.schema", "../bench/corpus/FixedTable.schema"},
		"bench/realworld": {"../bench/corpus/RealWorld.schema"},
		"nested":          {filepath.Join(javaPlatformDir, "nested")},
		"siblings":        append([]string{"../bench/corpus/Bench.schema", "../bench/corpus/FixedTable.schema"}, javaSiblingPaths(t)...),
	}
	entries, err := os.ReadDir("../tables")
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if e.IsDir() {
			units["tables/"+e.Name()] = []string{filepath.Join("../tables", e.Name())}
		}
	}
	return units
}

func javaSiblingPaths(t *testing.T) []string {
	t.Helper()
	paths, err := filepath.Glob(filepath.Join(javaPlatformDir, "siblings", "*.schema"))
	if err != nil || len(paths) == 0 {
		t.Fatalf("no sibling fixtures under %s/siblings (%v)", javaPlatformDir, err)
	}
	return paths
}

// javaProbeLoads reports whether a one-file unit named file holding body
// passes the checker: whether the name it tries is a legal schema name.
func javaProbeLoads(t *testing.T, file, body string) bool {
	t.Helper()
	p := filepath.Join(t.TempDir(), file)
	if err := os.WriteFile(p, []byte("package probe\n\n"+body), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := New().Load([]string{p})
	return err == nil
}

// generateJavaUnit loads and generates one unit for the Java target.
func generateJavaUnit(paths []string) (*ir.Unit, map[string][]byte, error) {
	gathered, err := GatherPaths(paths)
	if err != nil {
		return nil, nil, err
	}
	u, err := New().Load(gathered)
	if err != nil {
		return nil, nil, err
	}
	files, err := New().Generate(u, "java", Options{})
	return u, files, err
}

// Every platform type the generated Java names is spelled java.<pkg>.<Type>,
// with no import, in the output of every corpus unit and both shadow units;
// and every platform type the emitters name has a committed sibling file and a
// nested declaration of that name, so the compile test below meets each one
// shadowed. A new emitter line that names a platform type plainly fails here,
// and so does one that names a new platform type until the corpus carries it.
func TestJavaNamesNoPlatformTypeUnqualified(t *testing.T) {
	t.Parallel()
	lang := javaLangTypes(t)
	use := &javaPlatformUse{qualified: map[string]bool{}}
	units := javaPlatformUnits(t)
	labels := slices.Sorted(func(yield func(string) bool) {
		for k := range units {
			if !yield(k) {
				return
			}
		}
	})
	scanned := 0
	for _, label := range labels {
		u, files, err := generateJavaUnit(units[label])
		if err != nil {
			if label == "nested" || label == "siblings" || label == "bench" || label == "examples" {
				t.Fatalf("%s: %v", label, err)
			}
			t.Logf("%s: java does not generate the unit (%v); not scanned", label, err)
			continue
		}
		scanned++
		own := javaOwnNames(u)
		for _, name := range slices.Sorted(func(yield func(string) bool) {
			for k := range files {
				if !yield(k) {
					return
				}
			}
		}) {
			if strings.HasSuffix(name, ".java") {
				scanJavaPlatform(label+"/"+name, string(files[name]), lang, own, use)
			}
		}
	}
	if scanned < 10 {
		t.Fatalf("the scan read %d units; the corpora are not where the test looks", scanned)
	}
	for _, b := range use.bare {
		t.Errorf("%s — generated Java names a platform type by its simple name or an import; spell it from the package root (java.lang.X)", b)
	}
	siblings := map[string]bool{}
	for _, p := range javaSiblingPaths(t) {
		siblings[strings.TrimSuffix(filepath.Base(p), ".schema")] = true
	}
	nestedU, _, err := generateJavaUnit([]string{filepath.Join(javaPlatformDir, "nested")})
	if err != nil {
		t.Fatal(err)
	}
	nested := javaOwnNames(nestedU)
	for _, name := range slices.Sorted(func(yield func(string) bool) {
		for k := range use.qualified {
			if !yield(k) {
				return
			}
		}
	}) {
		// a name the checker refuses (a C header basename, a Dart reserved
		// word) is no legal schema name, so no fixture can carry it: the
		// probe proves the refusal, and a fixture is owed for every other
		if !siblings[name] && javaProbeLoads(t, name+".schema", "type ProbeEntry { v uint8 }\n") {
			t.Errorf("generated Java names the platform type %s, a legal schema basename, and %s/siblings has no %s.schema to shadow it", name, javaPlatformDir, name)
		}
		if !nested[name] && javaProbeLoads(t, "Probe.schema", "type "+name+" { v uint8 }\n") {
			t.Errorf("generated Java names the platform type %s, a legal schema type name, and %s/nested declares no type %s to shadow it", name, javaPlatformDir, name)
		}
	}
	if len(use.qualified) < 20 {
		t.Errorf("the scan met %d platform types spelled in full; the emitters name more than that", len(use.qualified))
	}
}

// The scan is not vacuous: a bare platform name, an import, and a qualified
// one each read as what they are, and a comment, a string or the unit's own
// class of that name is not a reference.
func TestJavaPlatformScanReadsBareAndQualifiedNames(t *testing.T) {
	t.Parallel()
	lang := map[string]bool{"Long": true, "Object": true, "Double": true}
	src := `package demo;

import java.util.Arrays;

// Long in a comment
public final class Wire {
    @java.lang.Override
    public boolean equals(Object o) { return "Long" == null; }
    static long f(double d) { return java.lang.Double.doubleToRawLongBits(d) + Long.MAX_VALUE + java.lang.invoke.VarHandle.class.hashCode(); }
    static Double own;
}
`
	use := &javaPlatformUse{qualified: map[string]bool{}}
	scanJavaPlatform("Wire.java", src, lang, map[string]bool{"Double": false}, use)
	want := []string{"Wire.java:3: import java.util.Arrays", "Wire.java:8: Object", "Wire.java:9: Long", "Wire.java:10: Double"}
	if !slices.Equal(use.bare, want) {
		t.Errorf("bare uses %q, want %q", use.bare, want)
	}
	for _, q := range []string{"Override", "Double", "VarHandle"} {
		if !use.qualified[q] {
			t.Errorf("java.<pkg>.%s not read as a qualified use (%v)", q, use.qualified)
		}
	}
	use = &javaPlatformUse{qualified: map[string]bool{}}
	scanJavaPlatform("Wire.java", src, lang, map[string]bool{"Double": true, "Object": true, "Long": true}, use)
	if len(use.bare) != 1 || !strings.Contains(use.bare[0], "import") {
		t.Errorf("the unit's own classes read as platform uses: %q", use.bare)
	}
}

// javaClassNamesRefused is each committed file under refused/ and the words
// its refusal must name.
var javaClassNamesRefused = map[string]string{
	"class.schema":          `"class" is reserved as a Java class name`,
	"enum.schema":           `"enum" is reserved as a Java class name`,
	"package.schema":        `"package" is reserved as a Java class name`,
	"true.schema":           `"true" is reserved as a Java class name`,
	"_.schema":              `"_" is reserved as a Java class name`,
	"record.schema":         `"record" is reserved as a Java class name`,
	"sealed.schema":         `"sealed" is reserved as a Java class name`,
	"permits.schema":        `"permits" is reserved as a Java class name`,
	"var.schema":            `"var" is reserved as a Java class name`,
	"yield.schema":          `"yield" is reserved as a Java class name`,
	"java.schema":           `a class named "java" obscures the java package`,
	"PermitsType.schema":    `type permits is reserved as a Java class name`,
	"JavaType.schema":       `type java becomes a class named "java", which obscures the java package`,
	"JavaField.schema":      `field java of Packet maps to the Java identifier "java", which obscures the java package`,
	"JavaTableField.schema": `field java of table Shelf maps to the Java identifier "java", which obscures the java package`,
}

// A schema file is the outer class of its name, and a declaration the class of
// its name: javac refuses a reserved or restricted word as a class name, and a
// class, field or table member named java obscures the package every platform
// type is spelled from. Each committed case is refused by name at generation,
// and the control (accepted/, the identical unit under an ordinary name)
// generates; the compile test compiles it.
func TestJavaRefusesAClassNameJavacCannotTake(t *testing.T) {
	t.Parallel()
	paths, err := filepath.Glob(filepath.Join(javaPlatformDir, "refused", "*.schema"))
	if err != nil {
		t.Fatal(err)
	}
	if len(paths) != len(javaClassNamesRefused) {
		t.Errorf("%s/refused holds %d files, the table names %d", javaPlatformDir, len(paths), len(javaClassNamesRefused))
	}
	for _, p := range paths {
		want, ok := javaClassNamesRefused[filepath.Base(p)]
		if !ok {
			t.Errorf("%s: no expected refusal in javaClassNamesRefused", p)
			continue
		}
		_, _, err := generateJavaUnit([]string{p})
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%s: want the refusal %q, got %v", p, want, err)
		}
	}
	if _, _, err := generateJavaUnit([]string{filepath.Join(javaPlatformDir, "accepted")}); err != nil {
		t.Errorf("accepted/ (the control): %v", err)
	}
}

// The compile: three units in one javac run, each in its own package —
// Bench.schema and FixedTable.schema beside a sibling file named for every
// platform type the emitters name (package bench); a unit whose own types take
// those names (package javaplatform); and the ordinary control of refused/
// (accepted/, package demo). Each unit carries the 128-bit helpers, floats,
// bytes, flags and tables, so every emitted support file and table runtime is
// compiled with the shadowing classes in its package.
func TestJavaUnitsNamedLikePlatformTypesCompile(t *testing.T) {
	slowtest.Gate(t, "javac (it compiles three generated units)")
	javac := os.Getenv("JAVAC")
	if javac == "" {
		p, err := exec.LookPath("javac")
		if err != nil {
			t.Skip("no javac (set JAVAC or install a JDK)")
		}
		javac = p
	}
	t.Parallel()
	dir := t.TempDir()
	var sources []string
	for _, unit := range []struct {
		pkg   string
		paths []string
	}{
		{"bench", append([]string{"../bench/corpus/Bench.schema", "../bench/corpus/FixedTable.schema"}, javaSiblingPaths(t)...)},
		{"javaplatform", []string{filepath.Join(javaPlatformDir, "nested")}},
		{"demo", []string{filepath.Join(javaPlatformDir, "accepted")}},
	} {
		_, files, err := generateJavaUnit(unit.paths)
		if err != nil {
			t.Fatalf("%s: %v", unit.pkg, err)
		}
		pkg := filepath.Join(dir, "src", unit.pkg)
		if err := os.MkdirAll(pkg, 0o755); err != nil {
			t.Fatal(err)
		}
		names := make([]string, 0, len(files))
		for name := range files {
			names = append(names, name)
		}
		sort.Strings(names)
		for _, name := range names {
			if !strings.HasSuffix(name, ".java") {
				continue
			}
			p := filepath.Join(pkg, name)
			if err := os.WriteFile(p, files[name], 0o644); err != nil {
				t.Fatal(err)
			}
			sources = append(sources, p)
		}
	}
	classes := filepath.Join(dir, "classes")
	if err := os.MkdirAll(classes, 0o755); err != nil {
		t.Fatal(err)
	}
	args := append([]string{"--release", "17", "-nowarn", "-d", classes}, sources...)
	cmd := testproc.Command(t, javac, args...)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("javac: %v\n%s", err, out)
	}
}
