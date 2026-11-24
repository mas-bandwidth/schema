package cpptable

import (
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/fixedunlawful"
)

// cppUnlawfulAnswer is the C++ leg's answer to one case of the shared
// unlawful-lineage corpus (internal/fixedunlawful).
type cppUnlawfulAnswer struct {
	// poison sets back[k] of the NEW type to values that are not its declared
	// defaults, so a load that leaves a row alone is seen.
	poison string
	// check is true when back[k] holds its declared defaults (read off the
	// VNEW_ schema: lead = 1, trail = 2, every other field its type's default).
	check string
	// values, for the LAWFUL control only, is the printf argument list that
	// formats back[0] after the load (a "lead=%llu v=%llu trail=%llu" line).
	values string
}

var cppUnlawfulAnswers = map[string]cppUnlawfulAnswer{
	"unknown_census": {
		poison: `back[k].lead = 99u; back[k].trail = 88u; for ( int i = 0; i < 4; ++i ) { back[k].items[i].a = 5 + i; }`,
		check:  `back[k].lead == 1u && back[k].trail == 2u && back[k].items[0].a == 0 && back[k].items[1].a == 0 && back[k].items[2].a == 0 && back[k].items[3].a == 0`,
	},
	"unknown_census_many": {
		poison: `back[k].lead = 99u; back[k].trail = 88u; for ( int i = 0; i < 4; ++i ) { back[k].items[i].a = 5 + i; }`,
		check:  `back[k].lead == 1u && back[k].trail == 2u && back[k].items[0].a == 0 && back[k].items[1].a == 0 && back[k].items[2].a == 0 && back[k].items[3].a == 0`,
	},
	"kind_moved": {
		poison: `back[k].lead = 99u; back[k].v = 5.5f; back[k].trail = 88u;`,
		check:  `back[k].lead == 1u && back[k].v == 0.0f && back[k].trail == 2u`,
	},
	"arm_removed": {
		poison: `back[k].lead = 99u; back[k].pick.SelectGamma().p = 77; back[k].trail = 88u;`,
		check:  `back[k].lead == 1u && back[k].pick.type == PickType::None && back[k].trail == 2u`,
	},
	"variant_removed": {
		poison: `back[k].lead = 99u; back[k].tier = Tier::Gold; back[k].trail = 88u;`,
		check:  `back[k].lead == 1u && back[k].tier == Tier::None && back[k].trail == 2u`,
	},
	"key_removed": {
		poison: `back[k].lead = 99u; back[k].slots[Tier::Bronze] = 70; back[k].slots[Tier::Gold] = 71; back[k].trail = 88u;`,
		check:  `back[k].lead == 1u && back[k].slots[Tier::Bronze] == 0 && back[k].slots[Tier::Gold] == 0 && back[k].trail == 2u`,
	},
	"array_shrunk": {
		poison: `back[k].lead = 99u; back[k].vals[0] = 70; back[k].vals[1] = 71; back[k].trail = 88u;`,
		check:  `back[k].lead == 1u && back[k].vals[0] == 0 && back[k].vals[1] == 0 && back[k].trail == 2u`,
	},
	"counted_shrunk": {
		poison: `back[k].lead = 99u; back[k].vals_count = 2; back[k].vals[0] = 70; back[k].vals[1] = 71; back[k].trail = 88u;`,
		check:  `back[k].lead == 1u && back[k].vals_count == 0 && back[k].vals[0] == 0 && back[k].vals[1] == 0 && back[k].trail == 2u`,
	},
	"text_narrowed": {
		poison: `back[k].lead = 99u; back[k].text[0] = 'z'; back[k].text[1] = 'z'; back[k].text[2] = 0; back[k].text_length = 2; back[k].trail = 88u;`,
		check:  `back[k].lead == 1u && back[k].text_length == 0 && back[k].text[0] == 0 && back[k].trail == 2u`,
	},
	"bytes_narrowed": {
		poison: `back[k].lead = 99u; back[k].blob[0] = 0x5A; back[k].blob_length = 1; back[k].trail = 88u;`,
		check:  `back[k].lead == 1u && back[k].blob_length == 0 && back[k].blob[0] == 0 && back[k].trail == 2u`,
	},
	"lawful_control": {
		poison: `back[k].lead = 99u; back[k].v = 5u; back[k].trail = 88u;`,
		values: `(unsigned long long) back[0].lead, (unsigned long long) back[0].v, (unsigned long long) back[0].trail`,
	},
}

// cppLawfulBody is the probe body of the LAWFUL control: the rows are poisoned,
// the OLD file is loaded again, a refusal or a count other than the file's is a
// failure, and the verdict the driver reads is what the probe prints of the
// values the load landed in record 0.
func cppLawfulBody(name, root, poison, values string, records int) string {
	return fmt.Sprintf(`
    for ( int k = 0; k < 8; ++k ) { %[3]s }
    memset( &r, 0, sizeof( r ) );
    n = %[2]sFixedLoad( back, 8, data, len, plan, 4096, NULL, &r );
    if ( n != %[5]d || r.refused ) { printf( "%[1]s: the LAWFUL control did not load: n=%%lld refused=%%d reason=%%d\n", (long long) n, (int) r.refused, (int) r.reason ); return 1; }
    printf( "loaded %%lld: lead=%%llu v=%%llu trail=%%llu", (long long) n, %[4]s );
`, name, root, poison, values, records)
}

// TestCppFixedUnlawfulCorpus is the C++ leg's driver for the shared
// unlawful-lineage corpus. Per case: the NEW build, handed the OLD layout as a
// lineage entry (the VOLD_ sibling picked up by the nil-lock convention),
// refuses the OLD file before any record, every counter zero, every row back
// at its declared defaults, on a first load and a second over poisoned rows
// (docs/FIXED-FORM-ALGORITHM.md §5.2 MATCH/EMIT, §5.4, §5.9 #30; SPEC-TABLES
// §6.5). The refusal's NAME is this leg's own and is pinned here, separately
// from the verdict, by cppUnlawfulBody: TableMessageReason::layout_malformed
// (ruling refusal-verdict-only: never compared across legs).
func TestCppFixedUnlawfulCorpus(t *testing.T) {
	t.Parallel()
	corpus := cppFixedCorpus(t)
	fixedunlawful.Run(t, "cpptable", cppUnlawfulAnswers, func(t *testing.T, c fixedunlawful.Case, a cppUnlawfulAnswer) string {
		t.Parallel()
		row := strings.TrimPrefix(c.Old, "VOLD_")
		if "VNEW_"+row != c.New {
			t.Fatalf("%s: the pair %s / %s does not share a row name; cppRunVersionProbe reads VOLD_<row> and VNEW_<row>", c.Name, c.Old, c.New)
		}
		var body string
		if c.Lawful {
			body = cppLawfulBody(c.Name, c.Root, a.poison, a.values, c.Records)
		} else {
			// the verdict is printed only after every check above held
			body = cppUnlawfulBody(c.Name, c.Root, a.poison, a.check) + "    printf( \"refused\" );\n"
		}
		out, err := cppRunVersionProbe(t, row, filepath.Join(corpus, c.File), body, "")
		if err != nil {
			t.Fatalf("%s: %v\n%s", c.Name, err, out)
		}
		return strings.TrimSpace(string(out))
	})
}
