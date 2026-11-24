package ctable

// The C leg's driver for the shared corpus of UNLAWFUL handed-in lineage
// entries (internal/fixedunlawful). Per case the NEW build, handed the OLD
// layout as a lineage entry, reads the OLD writer's file: the plan compiler
// lays NO plan for the entry, the load refuses before any record lands, no
// counter moves, and every row handed in holds its declared defaults, on a
// first load and a second (docs/FIXED-FORM-ALGORITHM.md §5.2 MATCH/EMIT, §5.4,
// §5.9 #30; SPEC-TABLES §6.5).
//
// Ruling refusal-verdict-only: the verdict above is the shared one. The NAME
// the C leg refuses under, SCHEMA_TABLE_LAYOUT_MALFORMED, is pinned below as a
// separate assertion and never compared across legs.

import (
	"fmt"
	"path/filepath"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/fixedunlawful"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// cUnlawfulAnswer is the C leg's answer to one case.
type cUnlawfulAnswer struct {
	// poison sets back[k] of the NEW type to values that are not its declared
	// defaults, so a load that leaves a row alone is seen.
	poison string
	// check is true of back[k] when it holds the declared defaults of the
	// VNEW schema (lead = 1, trail = 2, every unlisted field its type's
	// default). The probe also compares the whole row to a fresh reset.
	check string
}

const cUnlawfulBrackets = `back[k].lead == 1u && back[k].trail == 2u`

var cUnlawfulAnswers = map[string]cUnlawfulAnswer{
	"unknown_census":      {poison: cCensusPoison, check: cCensusCheck},
	"unknown_census_many": {poison: cCensusPoison, check: cCensusCheck},
	"kind_moved": {
		poison: `back[k].lead = 99u; back[k].v = 5.5f; back[k].trail = 88u;`,
		check:  cUnlawfulBrackets + ` && back[k].v == 0.0f`,
	},
	"arm_removed": {
		// type 2 is gamma, the arm this reader still names.
		poison: `back[k].lead = 99u; back[k].pick.type = 2; back[k].pick.as.gamma.p = 77; back[k].trail = 88u;`,
		check:  cUnlawfulBrackets + ` && back[k].pick.type == 0`,
	},
	"variant_removed": {
		// 2 is Gold, the variant this reader still names.
		poison: `back[k].lead = 99u; back[k].tier = 2; back[k].trail = 88u;`,
		check:  cUnlawfulBrackets + ` && back[k].tier == 0`,
	},
	"key_removed": {
		poison: `back[k].lead = 99u; back[k].slots[0] = 70; back[k].slots[1] = 71; back[k].trail = 88u;`,
		check:  cUnlawfulBrackets + ` && back[k].slots[0] == 0 && back[k].slots[1] == 0`,
	},
	"array_shrunk": {
		poison: `back[k].lead = 99u; back[k].vals[0] = 70; back[k].vals[1] = 71; back[k].trail = 88u;`,
		check:  cUnlawfulBrackets + ` && back[k].vals[0] == 0 && back[k].vals[1] == 0`,
	},
	"counted_shrunk": {
		poison: `back[k].lead = 99u; back[k].vals_count = 2; back[k].vals[0] = 70; back[k].vals[1] = 71; back[k].trail = 88u;`,
		check:  cUnlawfulBrackets + ` && back[k].vals_count == 0 && back[k].vals[0] == 0 && back[k].vals[1] == 0`,
	},
	"text_narrowed": {
		poison: `back[k].lead = 99u; memcpy( back[k].text, "zz", 3 ); back[k].text_length = 2; back[k].trail = 88u;`,
		check:  cUnlawfulBrackets + ` && back[k].text_length == 0 && back[k].text[0] == 0`,
	},
	"bytes_narrowed": {
		poison: `back[k].lead = 99u; back[k].blob[0] = 0x5A; back[k].blob_length = 1; back[k].trail = 88u;`,
		check:  cUnlawfulBrackets + ` && back[k].blob_length == 0 && back[k].blob[0] == 0`,
	},
}

// cUnlawfulCorpusBody runs after the harness's first load. Each pass checks
// the shared VERDICT (refused, no counter moved, every row at its declared
// defaults), then, separately, the C leg's own refusal name.
func cUnlawfulCorpusBody(row, load, poison, check string) string {
	return fmt.Sprintf(`
    {
        int pass;
        for ( pass = 0; pass < 2; ++pass )
        {
            if ( pass == 1 )
            {
                for ( k = 0; k < 8; ++k ) { %[3]s }
                memset( &r, 0, sizeof( r ) );
                n = %[2]s( back, 8, data, len, plan, 4096, NULL, &r );
            }
            /* THE VERDICT, shared by every leg */
            if ( n >= 0 ) { printf( "%[1]s pass %%d: the UNLAWFUL entry LOADED %%lld records\n", pass, (long long) n ); return 1; }
            if ( !r.refused ) { printf( "%[1]s pass %%d: returned %%lld without a refusal on the report\n", pass, (long long) n ); return 1; }
            if ( r.unknown != 0 || r.widened != 0 || r.missing_required != 0 || r.non_finite_floats != 0 )
                { printf( "%[1]s pass %%d: a refusal moved a counter: unknown=%%d widened=%%d missing=%%d nonfinite=%%d\n", pass, r.unknown, r.widened, r.missing_required, r.non_finite_floats ); return 1; }
            for ( k = 0; k < 8; ++k )
            {
                if ( !( %[4]s ) ) { printf( "%[1]s pass %%d: row %%d is not at its declared defaults\n", pass, k ); return 1; }
                if ( memcmp( &back[k], &fresh, sizeof( fresh ) ) != 0 ) { printf( "%[1]s pass %%d: row %%d differs from a fresh reset\n", pass, k ); return 1; }
            }
            /* THE C LEG'S OWN NAME (refusal-verdict-only: pinned here, never compared across legs) */
            if ( r.reason != SCHEMA_TABLE_LAYOUT_MALFORMED || r.refuse_reason != 0 )
                { printf( "%[1]s pass %%d: refused, but not layout_malformed: reason=%%d refuse_reason=%%d\n", pass, r.reason, r.refuse_reason ); return 1; }
        }
    }
`, row, load, poison, check)
}

func TestCFixedUnlawfulCorpus(t *testing.T) {
	corpus := cFixedCorpus(t)
	fixedunlawful.Run(t, "ctable", cUnlawfulAnswers, func(t *testing.T, c fixedunlawful.Case, a cUnlawfulAnswer) {
		t.Parallel()
		newer := cReadSchema(t, c.New)
		older := cReadSchema(t, c.Old)
		load := ir.RustSnake(c.Root) + "_fixed_load"
		body := cUnlawfulCorpusBody(c.Name, load, a.poison, a.check)
		out, err := cRunVersionProbeRoot(t, newer, []string{older}, 0, c.Root,
			filepath.Join(corpus, c.File), body, cUnlawfulPoison(a.poison))
		if err != nil {
			t.Fatalf("%s: %v\n%s", c.Name, err, out)
		}
	})
}
