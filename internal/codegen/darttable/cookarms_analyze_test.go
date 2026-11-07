package darttable

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestCookWriterOfUnionArmsNamesNoWideStore pins the strict-analyze gate's nine
// errors on tables/arms (docs/SPEC-TABLES.md §2.6, §2.9, §7.6): a list or a
// bounded array of unions is written element by element through the union's own
// writer, never as a scalar of the element's stride (`setUint192`, `.lo`), and a
// table held by value through an arm that is itself a union is a record of the
// cook's region, so its writer exists. The probe is the generated text: the
// analyzer runs in the strict-analyze gate, and these are the spellings it
// refused.
//
// The REVERSED WITNESS asserts the refused spellings the old emitter wrote are
// found by the same scan, so the scan cannot pass on text it never sees.
func TestCookWriterOfUnionArmsNamesNoWideStore(t *testing.T) {
	t.Parallel()
	gen := func(names ...string) string {
		var src strings.Builder
		for i, name := range names {
			b, err := os.ReadFile(filepath.Join("..", "..", "..", "tables", "arms", name+".schema"))
			if err != nil {
				t.Fatal(err)
			}
			text := string(b)
			if i > 0 {
				// one unit: the package line is the first file's
				text = strings.Replace(text, "package armdemo\n", "", 1)
			}
			src.WriteString(text)
		}
		files, err := Generate(buildUnit(t, src.String()))
		if err != nil {
			t.Fatal(err)
		}
		return string(files["TCook.dart"])
	}
	refused := []string{"setUint192", "setUint256", ".lo : ", "_cookLeaf(value.outer"}
	for _, name := range []string{"Carry", "Ring"} {
		text := gen(name)
		if text == "" {
			t.Fatalf("%s: no cook writer generated", name)
		}
		for _, bad := range refused {
			if strings.Contains(text, bad) {
				t.Errorf("%s cook writes a union element as a wide scalar: %q", name, bad)
			}
		}
		if !strings.Contains(text, "if (!_cook") || !strings.Contains(text, "(l1[i1], c, array1 + i1 * ") {
			t.Errorf("%s cook does not write a list of unions through the union's element writer", name)
		}
	}
	nest := gen("Carry", "Nest")
	if _, class, _ := strings.Cut(nest, "final class NestCook {"); !strings.Contains(class, "static bool _cookLeaf(") {
		t.Errorf("Nest cook has no writer for the table an arm-of-an-arm holds by value")
	}
	// reversed witness: the old spelling is visible to the scan
	old := "out.setUint192(array + i * 24, l[i], order);"
	hit := false
	for _, bad := range refused {
		hit = hit || strings.Contains(old, bad)
	}
	if !hit {
		t.Error("the scan does not recognise the refused spelling")
	}
}
