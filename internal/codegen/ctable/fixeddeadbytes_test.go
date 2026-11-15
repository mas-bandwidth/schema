package ctable

import (
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/fixedvector"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// vecRoot is the corpus root: the one fixed table no other fixed table in the
// unit names by value.
const vecRoot = "DeadRoot"

// EVERY DEAD BYTE IN A FIXED RECORD IS ZERO, AND A NON-ZERO ONE REFUSES THE
// WHOLE LOAD (docs/rulings.json fixed-dead-bytes-are-zero; docs/SPEC-TABLES.md
// §3.4, §13.10; docs/FIXED-FORM-ALGORITHM.md §3.1, §4.5, §5.3 step 11). The
// shared corpus (fixedvector.DeadBytes) has, per dead kind, one control whose
// every dead byte is zero and one changed file with a single dead region made
// non-zero: the generated C load refuses each changed file whole, writes no
// destination byte, and reads each control and saves it back byte for byte.
func TestFixedDeadBytesAreZero(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "a C compiler (it runs the generated reader over the dead-byte corpus)")
	corpus := cFixedCorpus(t)
	schema := cReadSchema(t, "DeadBytes")
	snake := ir.RustSnake(vecRoot)

	for _, vec := range fixedvector.DeadBytes {
		t.Run(vec.Name, func(t *testing.T) {
			t.Parallel()
			file := filepath.Join(corpus, vec.File)
			body := cDeadBytesBody(vec, snake)
			out, err := cRunVersionProbeRoot(t, schema, nil, 0, vecRoot, file, body, "")
			if err != nil {
				t.Fatalf("%s: %v\n%s", vec.File, err, out)
			}
			t.Logf("%s", out)
		})
	}
}

// cDeadBytesBody is the probe's own claim for one vector: the refusal verdict,
// with every destination slot still holding its reset image, or the control's
// read and a save that reproduces the file byte for byte. It never names a
// refusal reason: the verdict is the contract (docs/rulings.json
// refusal-verdict-only).
func cDeadBytesBody(vec fixedvector.DeadVector, snake string) string {
	if !vec.Reads {
		return fmt.Sprintf(`
    if ( n != -1 || !r.refused ) { printf( "%[1]s: n=%%lld refused=%%d, want the whole-load refusal\n", (long long) n, (int) r.refused ); return 1; }
    for ( k = 0; k < 8; ++k )
    {
        if ( memcmp( &back[k], &fresh, sizeof( fresh ) ) != 0 ) { printf( "%[1]s: the refusal wrote destination slot %%d\n", k ); return 1; }
    }
`, vec.Name)
	}
	return fmt.Sprintf(`
    if ( n < 1 || r.refused ) { printf( "%[1]s: n=%%lld refused=%%d, want the control to read\n", (long long) n, (int) r.refused ); return 1; }
    {
        static uint8_t out[1 << 20];
        const int64_t need = %[2]s_fixed_measure( n );
        const int64_t wrote = %[2]s_fixed_save( back, n, out, (int64_t) sizeof( out ) );
        if ( need != len || wrote != len ) { printf( "%[1]s: measure=%%lld wrote=%%lld file=%%lld\n", (long long) need, (long long) wrote, (long long) len ); return 1; }
        if ( memcmp( out, data, (size_t) len ) != 0 ) { printf( "%[1]s: the control did not save back byte for byte\n" ); return 1; }
    }
`, vec.Name, snake)
}

// AN ABSENT OPTIONAL'S PAYLOAD ENTRIES DO NOT RUN (docs/rulings.json
// fixed-dead-bytes-are-zero; docs/FIXED-FORM-ALGORITHM.md §3.1, §4.5, §5.3 step
// 11). Each entry inside an optional's payload carries the INNERMOST enclosing
// present byte (offset plus one) so the run and the bool/present damage pass
// skip it when that byte is 0; the whole payload is then judged by the
// present-defaults entry's dead span, never read as a value.
func TestFixedAbsentOptionalPayloadHasPresentLane(t *testing.T) {
	t.Parallel()
	const snake = "dead_root"
	header := cT12Header(t, cReadSchema(t, "DeadBytes"))
	cases := []struct{ comment, want string }{
		{"label", "1u"},      /* text inside the absent optional's payload */
		{"nums count", "1u"}, /* counted array inside it */
		{"flag", "1u"},       /* bool inside it */
		{"child", "30u"},     /* payload of the nested optional */
		{"child present, absent payload defaults", "1u"}, /* the nested flag is itself under the outer optional */
		{"opt present, absent payload defaults", "0u"},   /* the outermost flag is under no optional */
	}
	for _, c := range cases {
		line := cDeadPlanLine(t, header, snake, c.comment)
		inner := line[strings.Index(line, "{")+1 : strings.LastIndex(line, "}")]
		fields := strings.Split(inner, ",")
		got := strings.TrimSpace(fields[len(fields)-1])
		if got != c.want {
			t.Errorf("%s: the present lane is %s, want %s", c.comment, got, c.want)
		}
	}
}

// cDeadPlanLine is the one published plan entry whose comment is `comment`.
func cDeadPlanLine(t *testing.T, header, snake, comment string) string {
	t.Helper()
	marker := snake + "_fixed_plan[] = {\n"
	at := strings.Index(header, marker)
	if at < 0 {
		t.Fatalf("the header publishes no %s_fixed_plan", snake)
	}
	body := header[at+len(marker):]
	end := strings.Index(body, "};")
	if end < 0 {
		t.Fatalf("the header's %s_fixed_plan has no closing brace", snake)
	}
	for line := range strings.SplitSeq(body[:end], "\n") {
		if strings.Contains(line, "/* "+comment+" */") {
			return line
		}
	}
	t.Fatalf("the %s_fixed_plan has no entry commented %s", snake, comment)
	return ""
}
