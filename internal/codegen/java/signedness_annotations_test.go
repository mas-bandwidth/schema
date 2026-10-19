package java

import (
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/check"
	"github.com/mas-bandwidth/schema/v2/internal/parser"
)

func TestJavaEmitsCheckerSignednessOnIntegerStorage(t *testing.T) {
	t.Parallel()

	const src = `package signednessprobe

enum Mode | max = 300 { A, B }
flags Feature { A, B }

type Probe
{
    signed int32
    unsigned uint32
    bits bits(13)
    modes [2]Mode
    features Feature
}
`
	f, errs := parser.Parse("Probe.schema", []byte(src))
	if len(errs) != 0 {
		t.Fatal(errs)
	}
	u, errs := check.Unit([]check.SourceFile{{
		Path: "Probe.schema", Name: "Probe.schema", Base: "Probe", Bytes: []byte(src), AST: f,
	}})
	if len(errs) != 0 {
		t.Fatal(errs)
	}
	files, err := Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	got := string(files["Probe.java"])
	for _, want := range []string{
		"import org.checkerframework.checker.signedness.qual.Signed;",
		"import org.checkerframework.checker.signedness.qual.Unsigned;",
		"public @Signed int signed;",
		"public @Unsigned int unsigned;",
		"public @Unsigned int bits;",
		"public final @Unsigned short[] modes",
		"public @Unsigned long features;",
		"public static final @Unsigned short none = 0;",
		"public static final @Unsigned long featureA = 1L << 0;",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("generated Java missing %q:\n%s", want, got)
		}
	}
}
