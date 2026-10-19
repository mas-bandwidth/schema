package javatable

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// nullnessSites is the sentinel table (docs/SPEC-TABLES.md §16): per corpus
// unit and generated file, the declarations that can hold the null sentinel and
// so carry @Nullable. Each row names the emitter function that writes it:
//
//	javaFile/nullMarkedTopLevel   @NullMarked on every generated top-level type
//	nullableReturns               a method whose result is a literal null, or a
//	                              call to a nullable method of the same file
//	javaNullableType              a Value field whose default is null
//	runtime consts                fields set null on a path (reason, ids, recs,
//	                              element, recordRef, storage, Writer.buf), and
//	                              parameters that accept null (retain report,
//	                              node table, pointer child, record list)
var nullnessSites = map[string]map[string][]string{
	"examples": {
		"TableVariable.java": {
			"public @Nullable String reason;",
			"public long @Nullable [] ids = new long[0];",
			"public NodeRecord @Nullable [] recs;",
			"public static NodeRecord @Nullable [] nodes(Reader r, boolean retain)",
			"public @Nullable Reader body()",
			"public static @Nullable Reader open(byte[] data, Report report)",
			"public long index(@Nullable Object obj)",
		},
		"TableVocabulary.java": {
			"public @Nullable Shape element;",
			"static @Nullable Shape shapeRead(Reader r, int kind)",
		},
		"TableMessage.java": {
			"public final byte @Nullable [] buf;",
			"public TableVocabulary.@Nullable Entry ref()",
			"public static long elementRunBits(int kind, TableVocabulary.@Nullable Shape elem)",
		},
		"TableCookFieldInfo.java": {
			"public @Nullable TableCookStorage storage;",
			"public @Nullable InfoRef recordRef;",
			"public @Nullable TableCookInfo record()",
		},
		"TableCookWrite.java": {
			"final @Nullable TableCookInfo info;",
		},
		"TableRetain.java": {
			"static byte @Nullable [] resolve(byte[] buf, long @Nullable [] ids, int off, int limit, int kind)",
		},
	},
	"pointers": {
		"ListNodeVariable.java": {
			"public ListNodeVariable.@Nullable Value next = null;",
			"StringBuilder b, TableVariable.NodeGraph graph, @Nullable Value child, int depth)",
		},
	},
	"blobs": {
		"AssetVariable.java": {
			"public byte @Nullable [] data = null;",
		},
	},
}

// nonNullSites are declarations the emitter initialises non-null in every
// constructor: by the §16 rule they are never annotated.
var nonNullSites = map[string]map[string][]string{
	"examples": {
		"TableVariable.java": {
			"public byte[] buf = new byte[0];",
			"public static int trailerBase(byte[] data, Report report)",
		},
		"TableVocabulary.java": {
			"public Shape shape = new Shape();",
			"public Entry[] entries = new Entry[0];",
		},
		"TableMessage.java": {
			"public final byte[] buf;",
		},
		"TableCookInfo.java": {
			"public String name = \"\";",
			"public TableCookFieldInfo[] fields = new TableCookFieldInfo[0];",
		},
		"TableCookWrite.java": {
			"final byte[] blob;",
		},
		"TableFixed.java": {
			"public long[] gpool = noPool;",
			"public final Entry[] entries;",
			"public byte[] bytes = new byte[0];",
		},
	},
}

var nullMarkedType = regexp.MustCompile(`(?m)^@NullMarked\npublic (final |abstract )*(class|interface|enum|record) `)

// checkNullMarked holds every generated file of a unit to one @NullMarked
// top-level type (§16: the marker is per type, never per package).
func checkNullMarked(unit string, files map[string][]byte) error {
	names := make([]string, 0, len(files))
	for name := range files {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		if !strings.HasSuffix(name, ".java") {
			continue
		}
		if !nullMarkedType.Match(files[name]) {
			return fmt.Errorf("%s/%s: top-level type is not @NullMarked", unit, name)
		}
	}
	return nil
}

// checkNullnessSites holds one unit's files to the sentinel table: every
// sentinel site annotated, every non-null site present and unannotated.
func checkNullnessSites(unit string, files map[string][]byte) error {
	for name, sites := range nullnessSites[unit] {
		body, ok := files[name]
		if !ok {
			return fmt.Errorf("%s/%s: not generated", unit, name)
		}
		for _, site := range sites {
			if !strings.Contains(string(body), site) {
				return fmt.Errorf("%s/%s: sentinel site not annotated: %q", unit, name, site)
			}
		}
	}
	for name, sites := range nonNullSites[unit] {
		body, ok := files[name]
		if !ok {
			return fmt.Errorf("%s/%s: not generated", unit, name)
		}
		for _, site := range sites {
			if !strings.Contains(string(body), site) {
				return fmt.Errorf("%s/%s: non-null site missing or annotated: %q", unit, name, site)
			}
		}
	}
	return nil
}

func generateCorpusUnit(t *testing.T, unit string) map[string][]byte {
	t.Helper()
	files, err := Generate(unitFromDir(t, filepath.Join("..", "..", "..", "tables", unit)))
	if err != nil {
		t.Fatalf("%s: %v", unit, err)
	}
	return files
}

func TestJavaEmitterEmitsNullnessAnnotations(t *testing.T) {
	t.Parallel()
	// every corpus unit: each generated type is @NullMarked
	for _, dir := range keyedUnits(t) {
		files, err := Generate(unitFromDir(t, dir))
		if err != nil {
			continue // a unit the backend refuses emits no Java to mark
		}
		if err := checkNullMarked(filepath.Base(dir), files); err != nil {
			t.Error(err)
		}
	}
	// the sentinel table: annotated where null is held, and nowhere else
	for unit := range nullnessSites {
		if err := checkNullnessSites(unit, generateCorpusUnit(t, unit)); err != nil {
			t.Error(err)
		}
	}
	for unit := range nonNullSites {
		if err := checkNullnessSites(unit, generateCorpusUnit(t, unit)); err != nil {
			t.Error(err)
		}
	}
}

// TestJavaNullnessCheckRejectsADroppedAnnotation is the reversed witness:
// dropping one real @Nullable from one sentinel return in the emitter's
// output, or the marker from one type, turns the check above red.
func TestJavaNullnessCheckRejectsADroppedAnnotation(t *testing.T) {
	t.Parallel()
	files := generateCorpusUnit(t, "examples")
	if err := checkNullnessSites("examples", files); err != nil {
		t.Fatalf("unplanted output already red: %v", err)
	}
	planted := make(map[string][]byte, len(files))
	for k, v := range files {
		planted[k] = v
	}
	const site = "public @Nullable Reader body()"
	planted["TableVariable.java"] = []byte(strings.Replace(string(files["TableVariable.java"]), site, "public Reader body()", 1))
	err := checkNullnessSites("examples", planted)
	if err == nil || !strings.Contains(err.Error(), site) {
		t.Fatalf("dropping %q left the sentinel check green: %v", site, err)
	}
	planted["TableVariable.java"] = []byte(strings.Replace(string(files["TableVariable.java"]), "@NullMarked\n", "", 1))
	if err := checkNullMarked("examples", planted); err == nil {
		t.Fatal("dropping @NullMarked left the marker check green")
	}
}

func TestJavaNullableReturnsOnlySentinels(t *testing.T) {
	t.Parallel()
	body := `public final class Probe {
    public String missing() { return null; }
    public byte[] missingArray() { return null; }
    public String present() { return "present"; }
    public String ternary(boolean b) { return b ? null : "present"; }
    public String commented() { // return null;
        return "present";
    }
    public Outer.Inner qualified() { return null; }
    public String wrapper() { return missing(); }
    public String other(int x) { return present(); }
    public static <T> T generic() { return null; }
    public boolean has() { return missing() != null; }
    public java.util.List<byte[]> list() { return null; }
    static int branch(int x) {
        if (x > 0) { return 1; } else if (x < -40) { return 2; }
        return 0;
    }
}`
	got := nullableReturns(body)
	for _, site := range []string{
		"public @Nullable String missing()",
		"public byte @Nullable [] missingArray()",
		"public @Nullable String ternary(boolean b)",
		"public String present()",
		"public String commented()",
		"public Outer.@Nullable Inner qualified()",
		"public @Nullable String wrapper()",
		"public String other(int x)",
		"} else if (x < -40) {",
		"public static <T> @Nullable T generic()",
		"public boolean has()",
		"public java.util.@Nullable List<byte[]> list()",
	} {
		if !strings.Contains(got, site) {
			t.Fatalf("missing %q in:\n%s", site, got)
		}
	}
	for typ, want := range map[string]string{
		"byte[]":             "byte @Nullable []",
		"long[][]":           "long @Nullable [][]",
		"ShipVariable.Value": "ShipVariable.@Nullable Value",
		"String":             "@Nullable String",
		"List<a.B>":          "@Nullable List<a.B>",
		"List<byte[]>":       "@Nullable List<byte[]>",
	} {
		if got := javaNullableType(typ); got != want {
			t.Errorf("javaNullableType(%q) = %q, want %q", typ, got, want)
		}
	}
}

// The Java storage extrema cannot be exceeded. Keep the one-sided checks for
// narrower ranges, and ensure a fully spanned field emits no dead comparison.
func TestJavaRangeClampSkipsStorageExtrema(t *testing.T) {
	t.Parallel()
	root, err := filepath.Abs("../../..")
	if err != nil {
		t.Fatal(err)
	}
	src, err := os.ReadFile(filepath.Join(root, "tables/examples/Ranges.schema"))
	if err != nil {
		t.Fatal(err)
	}
	files := generate(t, string(src))
	fixed := string(files["RangedSignedFixed.java"])
	variable := string(files["RangedSignedVariable.java"])
	for _, dead := range []string{"q < -9223372036854775808L", "q > 9223372036854775807L"} {
		if strings.Contains(fixed, dead) {
			t.Fatalf("fixed emits impossible comparison %q", dead)
		}
	}
	for _, dead := range []string{"v.i32Span < -2147483648", "v.i32Span > 2147483647", "v.i64Span < -9223372036854775808L", "v.i64Span > 9223372036854775807L", "v.i32Low < -2147483648"} {
		if strings.Contains(variable, dead) {
			t.Fatalf("variable emits impossible comparison %q", dead)
		}
	}
	for _, live := range []string{"v.i32Low > 2147483646", "v.i32High < -2147483647", "v.i64Low > 9223372036854775806L", "v.i64High < -9223372036854775807L"} {
		if !strings.Contains(variable, live) {
			t.Fatalf("variable omitted live comparison %q", live)
		}
	}
}
