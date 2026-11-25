package ctable

// §5.8 row 11, unknown_census: the NEW reader (Item.drop REMOVED) is handed the
// OLD writer's layout as a lineage entry. §5.1 refuses a removal, so a lawful
// older writer never carries a field the reader lacks: the entry is UNLAWFUL,
// COMPILE lays no plan for it and the load refuses `layout_malformed` with every
// counter at zero — `unknown` included, because there is no compile census
// (docs/FIXED-FORM-ALGORITHM.md §5.2 MATCH, §5.4, §5.9 #30). After the refusal
// every row handed in is back at its declared defaults (docs/SPEC-TABLES.md
// §6.5), and a second load over freshly poisoned rows refuses the same way.

import (
	"fmt"
	"path/filepath"
	"testing"
)

// cUnlawfulBody is the probe body every unlawful-entry row shares. The harness
// has already poisoned back[] (with `poison`) and run the first load; pass 1
// poisons the rows again and loads a second time. Each pass must refuse
// layout_malformed by name, move no counter, and leave every row at its
// declared defaults (`check` is true of back[k] when it is). `load` is the
// root's generated <snake>_fixed_load.
func cUnlawfulBody(row, load, poison, check string) string {
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
            if ( n >= 0 ) { printf( "%[1]s pass %%d: the unlawful entry LOADED %%lld records\n", pass, (long long) n ); return 1; }
            if ( !r.refused || r.reason != SCHEMA_TABLE_LAYOUT_MALFORMED || r.refuse_reason != 0 )
                { printf( "%[1]s pass %%d: not refused layout_malformed: refused=%%d reason=%%d refuse_reason=%%d\n", pass, r.refused, r.reason, r.refuse_reason ); return 1; }
            if ( r.unknown != 0 || r.widened != 0 || r.missing_required != 0 || r.non_finite_floats != 0 )
                { printf( "%[1]s pass %%d: a refusal moved a counter: unknown=%%d widened=%%d missing=%%d nonfinite=%%d\n", pass, r.unknown, r.widened, r.missing_required, r.non_finite_floats ); return 1; }
            for ( k = 0; k < 8; ++k )
            {
                if ( !( %[4]s ) ) { printf( "%[1]s pass %%d: row %%d is not at its declared defaults\n", pass, k ); return 1; }
            }
        }
    }
`, row, load, poison, check)
}

// cUnlawfulPoison runs the harness's poison over all eight rows.
func cUnlawfulPoison(poison string) string {
	return "    for ( k = 0; k < 8; ++k ) { " + poison + " }\n"
}

const (
	cCensusPoison = `back[k].lead = 99u; back[k].trail = 88u; back[k].items[0].a = 5; back[k].items[1].a = 6; back[k].items[2].a = 7; back[k].items[3].a = 8;`
	cCensusCheck  = `back[k].lead == 1u && back[k].trail == 2u && back[k].items[0].a == 0 && back[k].items[1].a == 0 && back[k].items[2].a == 0 && back[k].items[3].a == 0`
)

func TestFixedVersioningUnknownCensus(t *testing.T) {
	t.Parallel() // reads the corpus, writes only its own TempDir (make tables-c-versioning)
	corpus := cFixedCorpus(t)
	newer := cReadSchema(t, "VNEW_unknown_census")
	older := cReadSchema(t, "VOLD_unknown_census")
	body := cUnlawfulBody("unknown_census", "census_fixed_load", cCensusPoison, cCensusCheck)
	out, err := cRunVersionProbe(t, newer, []string{older}, 0,
		filepath.Join(corpus, "old_unknown_census.bin"), body, cUnlawfulPoison(cCensusPoison))
	if err != nil {
		t.Fatalf("unknown_census: %v\n%s", err, out)
	}
}
