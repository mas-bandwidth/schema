package goldens

import (
	"bytes"
	"maps"
	"sort"
	"strconv"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/check"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/cpp"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/cpptable"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/cqualify"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// cppEmit runs both C++ emitters on u, the escape applied to the types want
// picks, and the C++ pass after them (compiler/target_cpp.go's shape).
func cppEmit(t *testing.T, u *ir.Unit, want func(string) bool) map[string][]byte {
	t.Helper()
	esc, table := cqualify.Escape(u, want)
	files, err := cpp.Generate(esc)
	if err != nil {
		t.Fatal(table.Err(err))
	}
	tables, err := cpptable.GenerateLineage(esc, nil)
	if err != nil {
		t.Fatal(table.Err(err))
	}
	maps.Copy(files, tables)
	out, err := cqualify.QualifyCpp(files, table, check.LibcIdentifier)
	if err != nil {
		t.Fatal(err)
	}
	return out
}

// TestCppTypeEscapeIsFaithful holds the C++ type escape to its contract on
// every unit the C++ goldens pin: with EVERY declared type escaped, the C++
// emitters and the pass after them write the same bytes as with none. The
// target escapes only the types that collide with the platform, which no
// golden unit declares, so this is the gate that sees the escape at all here:
// a derived spelling the escape does not map back, an identity it loses, or an
// order it moves is a byte diff in a named file.
func TestCppTypeEscapeIsFaithful(t *testing.T) {
	dirs := []string{corpusDir, corpus128Dir}
	units, err := tableUnits(tableGoldenDir)
	if err != nil {
		t.Fatal(err)
	}
	for _, unit := range units {
		if unit.target == "cpp" {
			dirs = append(dirs, unit.dir)
		}
	}
	for _, dir := range dirs {
		t.Run(dir, func(t *testing.T) {
			u := loadCorpusDir(t, dir)
			plain := cppEmit(t, u, func(string) bool { return false })
			escaped := cppEmit(t, u, func(string) bool { return true })
			names := make([]string, 0, len(plain))
			for n := range plain {
				names = append(names, n)
			}
			sort.Strings(names)
			for _, n := range names {
				if !bytes.Equal(plain[n], escaped[n]) {
					t.Errorf("%s: %s differs under the escape:\n%s", dir, n, firstDiff(plain[n], escaped[n]))
				}
			}
			if len(plain) != len(escaped) {
				t.Errorf("%s: %d files plain, %d escaped", dir, len(plain), len(escaped))
			}
		})
	}
}

// firstDiff names the first line two outputs disagree on.
func firstDiff(a, b []byte) string {
	al, bl := bytes.Split(a, []byte("\n")), bytes.Split(b, []byte("\n"))
	for i := 0; i < len(al) && i < len(bl); i++ {
		if !bytes.Equal(al[i], bl[i]) {
			return "line " + strconv.Itoa(i+1) + "\n  plain:   " + string(al[i]) + "\n  escaped: " + string(bl[i])
		}
	}
	return "lengths differ"
}
