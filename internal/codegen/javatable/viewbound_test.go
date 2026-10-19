package javatable

import (
	"path/filepath"
	"regexp"
	"strconv"
	"testing"

	"github.com/mas-bandwidth/schema/v2/ir"
)

// TestJavaDescriptorReportsDeclaredBound pins docs/SPEC-TABLES.md §8.1: a
// bounded array field — [..N]T, [N]T, or a keyed [E]T — reports its declared
// bound N in the generated Java field descriptor, mirroring the Go leg
// (internal/codegen/gotable/codecs.go).
func TestJavaDescriptorReportsDeclaredBound(t *testing.T) {
	t.Parallel()
	u := unitFromDir(t, filepath.Join("..", "..", "..", "tables", "examples"))
	files, err := Generate(u)
	if err != nil {
		t.Fatalf("generate tables/examples: %v", err)
	}
	fieldRE := regexp.MustCompile(`TableFieldInfo\.of\("([^"]+)", \d+, "[^"]+", (\d+)L\),`)
	checked, keyed := 0, 0
	for _, st := range cardDescriptorTables(u) {
		src, ok := files[st.Name+"Descriptor.java"]
		if !ok {
			t.Fatalf("%s: no generated descriptor", st.Name)
		}
		reported := map[string]int64{}
		for _, m := range fieldRE.FindAllStringSubmatch(string(src), -1) {
			v, err := strconv.ParseInt(m[2], 10, 64)
			if err != nil {
				t.Fatalf("%s: parse bound %q: %v", st.Name, m[2], err)
			}
			reported[m[1]] = v
		}
		for _, f := range st.Fields {
			if !boundedArrayField(f) {
				continue
			}
			checked++
			if f.KeyEnum != "" {
				keyed++
			}
			if reported[f.Name] != f.ArrayBound {
				t.Fatalf("%s: field %q declares bound %d, descriptor reports %d", st.Name, f.Name, f.ArrayBound, reported[f.Name])
			}
		}
	}
	if checked == 0 {
		t.Fatal("tables/examples: no bounded array field found to pin")
	}
	if keyed == 0 {
		t.Fatal("tables/examples: no keyed array field found to pin")
	}
}

func boundedArrayField(f *ir.Field) bool {
	return f.Array == ir.ArrayFixed || f.Array == ir.ArrayCounted
}
