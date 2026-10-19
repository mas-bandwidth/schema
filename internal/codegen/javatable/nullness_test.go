package javatable

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// These representative runtime and reader declarations are sentinel sites in
// the emitted table text (§3, §16); non-sentinel results must stay non-null.
var nullnessSites = map[string][]string{
	"TableVariable.java": {
		"@NullMarked",
		"public @Nullable String reason;",
		"public @Nullable Reader body()",
		"public static @Nullable Reader open(byte[] data, Report report)",
		"public static int trailerBase(byte[] data, Report report)",
	},
	"TableJson.java": {
		"@NullMarked",
		"public @Nullable String scanString()",
	},
}

func checkNullnessSites(files map[string][]byte) error {
	for name, data := range files {
		if !strings.Contains(string(data), "@NullMarked") {
			return fmt.Errorf("%s: top-level type is not @NullMarked", name)
		}
	}
	for name, sites := range nullnessSites {
		body := string(files[name])
		for _, site := range sites {
			if !strings.Contains(body, site) {
				return fmt.Errorf("%s: missing %q", name, site)
			}
		}
	}
	return nil
}

func TestJavaEmitterEmitsNullnessAnnotations(t *testing.T) {
	t.Parallel()
	files, err := Generate(unitFrom(t, `package probe
table Card { id string(64) }
`))
	if err != nil {
		t.Fatal(err)
	}
	if err := checkNullnessSites(files); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(files["TableVariable.java"]), "public static @Nullable int trailerBase") {
		t.Fatal("non-sentinel trailerBase result was marked nullable")
	}
	// Reverse one real sentinel annotation in a throwaway copy. The same
	// assertion must then reject it; the functional checker gate performs the
	// javac reversal after the positive corpus check passes.
	reversed := make(map[string][]byte, len(files))
	for k, v := range files {
		reversed[k] = append([]byte(nil), v...)
	}
	reversed["TableVariable.java"] = []byte(strings.Replace(
		string(reversed["TableVariable.java"]),
		"public @Nullable Reader body()",
		"public Reader body()", 1))
	if err := checkNullnessSites(reversed); err == nil {
		t.Fatal("dropping the Reader.body nullable result left the sentinel check green")
	}
}

func TestJavaNullableReturnsOnlySentinels(t *testing.T) {
	t.Parallel()
	body := `public final class Probe {
    public String missing() { return null; }
    public String present() { return "present"; }
    public String ternary(boolean b) { return b ? null : "present"; }
    public String commented() { // return null;
        return "present";
    }
}`
	got := nullableReturns(body)
	for _, site := range []string{
		"public @Nullable String missing()",
		"public @Nullable String ternary(boolean b)",
		"public String present()",
		"public String commented()",
	} {
		if !strings.Contains(got, site) {
			t.Fatalf("missing %q in:\n%s", site, got)
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
