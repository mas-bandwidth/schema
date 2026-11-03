package ctable

import (
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/fixedlimit"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
)

// THE FIXED FORM'S HARD LIMITS (docs/SPEC-TABLES.md §3.4's fixed_max_depth and
// fixed_record_bound), the shared vectors every leg reads. The NEW reader holds
// the OLD layout as a lineage entry, so the plan it reads the AT-limit file
// with is compiled from a layout that passed the bound: the file reads whole,
// and a reader whose bound is one short refuses it. The PAST-limit file's
// layout is forged under the OLD hash and refused by name before any record is
// touched; the layout bytes on their own name the bound itself. Every answer is
// limits.txt's, which the C++ reference wrote from its own reader.

// cLimitReason is a refusal name's C spelling: layout_too_deep is
// SCHEMA_TABLE_LAYOUT_TOO_DEEP.
func cLimitReason(name string) string {
	return "SCHEMA_TABLE_" + strings.ToUpper(name)
}

func cLimitCounters(v fixedlimit.Vector) string {
	return fmt.Sprintf("r.unknown != %d || r.kind_mismatch != %d || r.widened != %d || r.clamped != %d || r.duplicate != %d || (int) r.malformed != %d",
		v.Counters["unknown"], v.Counters["kind_mismatch"], v.Counters["widened"], v.Counters["clamped"], v.Counters["duplicate"], v.Counters["malformed"])
}

// cLimitParse is the layout check both sides share: the file's layout bytes
// handed to the reader's own parser, which answers ok or the bound's name.
func cLimitParse(name, want string) string {
	verdict := "parsed"
	if want != "ok" {
		verdict = "!parsed && why == " + cLimitReason(want)
	}
	return fmt.Sprintf(`
    {
        TableFixedLayoutView view;
        int why = SCHEMA_TABLE_LAYOUT_MALFORMED;
        const int parsed = table_fixed_parse_layout( data + 20, (int64_t) table_fixed_get32( data + 16 ), &view, &why );
        if ( !( %[2]s ) ) { printf( "%[1]s: the layout parsed=%%d reason=%%d, want %[3]s\n", parsed, why ); return 1; }
    }
`, name, verdict, want)
}

func cLimitRun(t *testing.T, row, at, over string, checks func(fixedlimit.Vector) string) {
	t.Helper()
	slowtest.Gate(t, "a C compiler (it runs the generated NEW reader over the limit vectors)")
	corpus := cFixedCorpus(t)
	a, err := fixedlimit.Read(corpus, at)
	if err != nil {
		t.Fatal(err)
	}
	o, err := fixedlimit.Read(corpus, over)
	if err != nil {
		t.Fatal(err)
	}
	if a.Refused() || !o.Refused() {
		t.Fatalf("limits.txt: %s must read and %s must refuse: %+v %+v", at, over, a, o)
	}
	newer := cReadSchema(t, "VNEW_"+row)
	older := cReadSchema(t, "VOLD_"+row)
	atBody := fmt.Sprintf(`
    if ( n != 1 || r.refused || r.malformed ) { printf( "%[1]s: n=%%lld refused=%%d reason=%%d, want one record\n", (long long) n, (int) r.refused, (int) r.reason ); return 1; }
    if ( %[2]s ) { printf( "%[1]s: a counter moved\n" ); return 1; }
%[3]s%[4]s    printf( "%[1]s: read whole\n" );
`, at, cLimitCounters(a), checks(a), cLimitParse(at, a.Parse))
	overBody := fmt.Sprintf(`
    if ( n >= 0 || !r.refused || r.reason != %[2]s ) { printf( "%[1]s: n=%%lld refused=%%d reason=%%d, want refused %[6]s\n", (long long) n, (int) r.refused, (int) r.reason ); return 1; }
    if ( %[3]s ) { printf( "%[1]s: a refusal moved a counter\n" ); return 1; }
%[4]s    printf( "%[1]s: refused %[6]s, layout %[5]s\n" );
`, over, cLimitReason(o.Reason), cLimitCounters(o), cLimitParse(over, o.Parse), o.Parse, o.Reason)
	for _, c := range []struct{ file, body string }{{at, atBody}, {over, overBody}} {
		out, err := cRunVersionProbe(t, newer, []string{older}, 0, filepath.Join(corpus, c.file), c.body, "")
		if err != nil {
			t.Fatalf("%s: %v\n%s", c.file, err, out)
		}
		t.Logf("%s", out)
	}
}

func cLimitCheck(t *testing.T, a fixedlimit.Vector, path, expr string) string {
	t.Helper()
	want, err := a.Int(path)
	if err != nil {
		t.Fatal(err)
	}
	return fmt.Sprintf("    if ( (long long) ( %[1]s ) != %[2]dll ) { printf( \"%[3]s landed %%lld, want %[2]d\\n\", (long long) ( %[1]s ) ); return 1; }\n", expr, want, path)
}

func TestFixedLimitDepth(t *testing.T) {
	cLimitRun(t, "limit_depth", fixedlimit.DepthAt, fixedlimit.DepthOver, func(a fixedlimit.Vector) string {
		return cLimitCheck(t, a, "r0.lead", "back[0].lead") +
			cLimitCheck(t, a, fixedlimit.DeepPath(), "back[0]"+strings.Repeat(".d", 63)+".v") +
			cLimitCheck(t, a, "r0.trail", "back[0].trail")
	})
}

func TestFixedLimitRecord(t *testing.T) {
	cLimitRun(t, "limit_record", fixedlimit.RecordAt, fixedlimit.RecordOver, func(a fixedlimit.Vector) string {
		return cLimitCheck(t, a, "r0.lead", "back[0].lead") +
			cLimitCheck(t, a, "r0.v", "back[0].v") +
			cLimitCheck(t, a, "r0.text_length", "back[0].text_length") +
			cLimitCheck(t, a, "r0.text[0]", "back[0].text[0]") +
			cLimitCheck(t, a, "r0.text[32761]", "back[0].text[32761]") +
			cLimitCheck(t, a, "r0.text[65519]", "back[0].text[65519]") +
			cLimitCheck(t, a, "r0.trail", "back[0].trail")
	})
}
