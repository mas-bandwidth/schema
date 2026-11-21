package javatable_test

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
)

// TestJavaDriverChecksEveryIntegerAssignment holds the Java conformance
// driver's write-refusal mutation to the declared kind on every path a row can
// take to an integer: a scalar field, an array element, a channel-array
// element, a map entry's key and value, and a nested record's field
// (testdata/conformance/tables/FORMAT.md, `write-refusal`).
//
// Java spells intN and uintN with one type, so the driver decides from the
// declaration what its storage holds. A value the declared kind cannot hold in
// that storage answers `unrepresentable`, as the Go reference driver answers
// the same row (test/conformance/go/writerefusal.go): one past a signed
// maximum, a negative in an unsigned field, one past an unsigned maximum. The
// value at each end is stored and written. A value the storage holds and the
// declaration does not (256 in a uint8, which Java stores in an int) reaches
// the writer, and the writer refuses it.
func TestJavaDriverChecksEveryIntegerAssignment(t *testing.T) {
	slowtest.Gate(t, "the JDK and the conformance corpus")
	root := javaConformanceRoot(t)
	javaConformanceBuild(t, root)
	wire := filepath.Join("testdata", "conformance", "tables", "wire", "torture_cook_extremes.bin")

	const (
		unrepresentable = "unrepresentable\n"
		wrote           = "wrote\n"
		refused         = "refused\n"
	)
	rows := []struct {
		path, value, want string
	}{
		// a scalar field
		{"b_i32", "2147483648", unrepresentable},
		{"b_i32", "2147483647", wrote},
		{"b_i32", "-2147483649", unrepresentable},
		{"b_u32", "-1", unrepresentable},
		{"b_u32", "4294967295", wrote},
		{"b_u32", "4294967296", unrepresentable},
		{"b_i64", "9223372036854775808", unrepresentable},
		{"b_i64", "9223372036854775807", wrote},
		{"b_u64", "-1", unrepresentable},
		{"b_u64", "18446744073709551615", wrote},
		{"b_u64", "18446744073709551616", unrepresentable},
		// storage wider than the declaration: the writer's refusal, not the driver's
		{"b_u8", "256", refused},
		{"b_u8", "-1", unrepresentable},
		{"b_u8", "255", wrote},
		// an array element
		{"arr_i32[0]", "2147483648", unrepresentable},
		{"arr_i32[0]", "2147483647", wrote},
		{"arr_u32[0]", "-1", unrepresentable},
		{"arr_u32[0]", "4294967295", wrote},
		{"arr_i64[0]", "9223372036854775808", unrepresentable},
		{"arr_i64[0]", "9223372036854775807", wrote},
		{"arr_u64[0]", "-1", unrepresentable},
		{"arr_u64[0]", "18446744073709551615", wrote},
		// a channel-array element
		{"chan_i32[0]", "2147483648", unrepresentable},
		{"chan_i32[0]", "2147483647", wrote},
		{"chan_u64[0]", "-1", unrepresentable},
		{"chan_u64[0]", "18446744073709551615", wrote},
		// a map entry's key and value
		{"map_i32_u32[0].key", "2147483648", unrepresentable},
		{"map_i32_u32[0].value", "-1", unrepresentable},
		{"map_i32_u32[0].value", "4294967295", wrote},
		{"map_i64_u64[0].key", "9223372036854775808", unrepresentable},
		{"map_i64_u64[0].value", "18446744073709551616", unrepresentable},
		{"map_i64_u64[0].value", "18446744073709551615", wrote},
		// a nested record's field
		{"opt_entry.stamp", "9223372036854775808", unrepresentable},
		{"opt_entry.stamp", "9223372036854775807", wrote},
	}
	var manifest strings.Builder
	for i, r := range rows {
		fmt.Fprintf(&manifest, "write-refusal row%02d cooktorture TortureCook %s %s int %s\n", i, wire, r.path, r.value)
	}
	outDir := javaDriverSurface(t, root, "write-refusal", manifest.String())
	for i, r := range rows {
		got, err := os.ReadFile(filepath.Join(outDir, fmt.Sprintf("row%02d", i)))
		if err != nil {
			t.Errorf("%s = %s: the driver wrote no answer: %v", r.path, r.value, err)
			continue
		}
		if string(got) != r.want {
			t.Errorf("%s = %s: the driver answered %q, want %q", r.path, r.value, got, r.want)
		}
	}
}
