package ctable

import (
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/fixedvector"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
)

// THE WRITER'S RANGE IS THE BOUND (docs/FIXED-FORM-ALGORITHM.md §4.5), the
// shared cross-version vectors every leg reads (fixedvector.WriterRange): an
// OLD writer's file with one ranged value forged past that writer's range, read
// by the NEW reader whose own range is wider. The strict read REFUSES the whole
// load `out_of_range`, landing nothing: a scalar, one element of a ranged fixed
// array (held the same as a scalar), and a uint64 at or above 2^63 (compared
// unsigned, so a signed comparison would read it in range). A vector the
// manifest records as refused owes that refusal; one the manifest records as
// landed owes the values it names.
func TestFixedVersioningRangeWidenHostile(t *testing.T) {
	slowtest.Gate(t, "a C compiler (it runs the generated NEW reader over the forged file)")
	corpus := cFixedCorpus(t)
	for _, vec := range fixedvector.WriterRange {
		t.Run(vec.Name, func(t *testing.T) {
			t.Parallel()
			want, err := fixedvector.Read(corpus, vec.File)
			if err != nil {
				t.Fatal(err)
			}
			body, err := cWriterRangeChecks(vec, want)
			if err != nil {
				t.Fatal(err)
			}
			newer := cReadSchema(t, "VNEW_"+vec.Row)
			older := cReadSchema(t, "VOLD_"+vec.Row)
			out, err := cRunVersionProbe(t, newer, []string{older}, 0, filepath.Join(corpus, vec.File), body, "")
			if err != nil {
				t.Fatalf("%s: %v\n%s", vec.File, err, out)
			}
			t.Logf("%s", out)
		})
	}
}

// cWriterRangeChecks spells a vector's answer as the probe body: a vector the
// reference refused owes the strict `out_of_range` refusal with nothing landed
// and no counter moved; one it landed owes each value as a 64-bit integer.
func cWriterRangeChecks(vec fixedvector.Vector, want fixedvector.Expect) (string, error) {
	leaves, err := want.Leaves()
	if err != nil {
		return "", err
	}
	var b strings.Builder
	if want.Refused || want.Clamped > 0 {
		fmt.Fprintf(&b, "    if ( n != -1 ) { printf( \"%[1]s: n=%%lld, want -1 (refused)\\n\", (long long) n ); return 1; }\n", vec.Name)
		fmt.Fprintf(&b, "    if ( !r.refused || r.reason != 0 || r.refuse_reason != SCHEMA_TABLE_REFUSE_OUT_OF_RANGE ) { printf( \"%[1]s: refused=%%d reason=%%d rr=%%d, want out_of_range\\n\", r.refused, r.reason, r.refuse_reason ); return 1; }\n", vec.Name)
		fmt.Fprintf(&b, "    if ( r.unknown != 0 || r.widened != 0 || r.layout_hash != 0 ) { printf( \"%s: a counter moved on an out_of_range refusal\\n\" ); return 1; }\n", vec.Name)
		fmt.Fprintf(&b, "    if ( memcmp( &back[0], &fresh, sizeof( fresh ) ) != 0 ) { printf( \"%s: a refused read kept a value\\n\" ); return 1; }\n", vec.Name)
		return b.String(), nil
	}
	fmt.Fprintf(&b, "    if ( n != 1 || r.refused ) { printf( \"%[1]s: n=%%lld refused=%%d, want one clean record\\n\", (long long) n, (int) r.refused ); return 1; }\n", vec.Name)
	for _, l := range leaves {
		expr := "back[0]." + l.Field
		if l.Index >= 0 {
			expr += fmt.Sprintf("[%d]", l.Index)
		}
		fmt.Fprintf(&b, "    if ( (long long) %[1]s != %[2]dLL ) { printf( \"%[3]s: %[4]s landed %%lld, want %[2]d\\n\", (long long) %[1]s ); return 1; }\n",
			expr, l.Value, vec.Name, l.Path)
	}
	fmt.Fprintf(&b, "    if ( r.unknown != 0 || r.widened != 0 || r.refuse_reason != SCHEMA_TABLE_REFUSE_OK ) { printf( \"%s: a counter that must not move did\\n\" ); return 1; }\n", vec.Name)
	return b.String(), nil
}
