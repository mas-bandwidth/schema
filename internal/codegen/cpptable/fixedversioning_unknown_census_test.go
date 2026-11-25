package cpptable

// §5.8 row 11, unknown_census: the NEW reader (Item.drop REMOVED) is handed the
// OLD writer's layout as a lineage entry. §5.1 refuses a removal, so a lawful
// older writer never carries a field the reader lacks: the entry is UNLAWFUL,
// COMPILE lays no plan for it and the load refuses `layout_malformed` with every
// counter at zero — `unknown` included, because there is no compile census
// (docs/FIXED-FORM-ALGORITHM.md §5.2 MATCH, §5.4, §5.9 #30). After the refusal
// every row handed in is back at its declared defaults (SPEC-TABLES §6.5), and a
// second load refuses the same way.

import (
	"fmt"
	"path/filepath"
	"testing"
)

// cppUnlawfulBody is the probe body every unlawful-entry row shares: the first
// load (already run by the harness) and a second one over POISONED rows each
// refuse by name, land nothing and leave every row at `reset`'s defaults.
// `poison` sets non-default values in back[k]; `check` is true when back[k] is
// at its declared defaults.
func cppUnlawfulBody(row, root, poison, check string) string {
	return fmt.Sprintf(`
    for ( int pass = 0; pass < 2; ++pass )
    {
        if ( pass == 1 )
        {
            for ( int k = 0; k < 8; ++k ) { %[2]s }
            memset( &r, 0, sizeof( r ) );
            n = %[4]sFixedLoad( back, 8, data, len, plan, 4096, NULL, &r );
        }
        if ( n >= 0 ) { printf( "%[1]s pass %%d: the unlawful entry LOADED %%lld records\n", pass, (long long) n ); return 1; }
        if ( !r.refused || r.reason != TableMessageReason::layout_malformed )
            { printf( "%[1]s pass %%d: not refused layout_malformed: refused=%%d reason=%%d\n", pass, (int) r.refused, (int) r.reason ); return 1; }
        if ( r.unknown != 0 || r.widened != 0 )
            { printf( "%[1]s pass %%d: a refusal moved a counter: unknown=%%d widened=%%d\n", pass, r.unknown, r.widened ); return 1; }
        for ( int k = 0; k < 8; ++k )
            if ( !( %[3]s ) ) { printf( "%[1]s pass %%d: row %%d is not at its declared defaults\n", pass, k ); return 1; }
    }
`, row, poison, check, root)
}

func TestFixedVersioningUnknownCensus(t *testing.T) {
	corpus := cppFixedCorpus(t)
	body := cppUnlawfulBody("unknown_census", "Census",
		`back[k].lead = 99u; back[k].trail = 88u; for ( int i = 0; i < 4; ++i ) { back[k].items[i].a = 5 + i; }`,
		`back[k].lead == 1u && back[k].trail == 2u && back[k].items[0].a == 0 && back[k].items[3].a == 0`)
	out, err := cppRunVersionProbe(t, "unknown_census",
		filepath.Join(corpus, "old_unknown_census.bin"), body, "")
	if err != nil {
		t.Fatalf("unknown_census: %v\n%s", err, out)
	}
}
