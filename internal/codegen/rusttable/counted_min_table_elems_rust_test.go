package rusttable

// A counted [Min..N] array of tables is at its BORN state when the live count
// is Min and every element's body is the terminator alone, and the writer
// elides it exactly then (docs/SPEC-TABLES.md §2.3). The bytes are the
// independent engine's (tablewire.Encode), the same bytes in every language.
//
// A FUNCTIONAL TEST: it compiles and runs Rust with rustc. No clock, no
// socket, no network.

import (
	"fmt"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
)

const countedMinTableSrc = `package countedmintable

table E
{
    n uint32
}

table R
{
    es [2..4]E
}
`

func TestRustCountedMinTableElementsElideAtBornState(t *testing.T) {
	t.Parallel()
	keyedRequireRustc(t)
	u := unitFrom(t, countedMinTableSrc)
	m := tabletext.NewModel(u)
	encode := func(text string) []byte {
		t.Helper()
		v := m.New(m.Lookup("R"))
		var rep tabletext.Report
		if !m.Read(v, []byte(text), &rep) {
			t.Fatalf("the engine did not read %s: %+v", text, rep)
		}
		wire, err := tablewire.Encode(m, v)
		if err != nil {
			t.Fatalf("the engine did not encode %s: %v", text, err)
		}
		return wire
	}
	born := encode(`{"es":[{},{}]}`)
	three := encode(`{"es":[{},{},{}]}`)
	oneSet := encode(`{"es":[{},{"n":7}]}`)
	body := fmt.Sprintf(`
    {
        let v = R { es: vec![E { n: 0 }, E { n: 0 }] };
        println!("CROSS born {}", r_save_checked(&v).map(|b| b == vec!%s).unwrap_or(false));
        let v = R { es: vec![E { n: 0 }, E { n: 0 }, E { n: 0 }] };
        println!("CROSS three {}", r_save_checked(&v).map(|b| b == vec!%s).unwrap_or(false));
        let v = R { es: vec![E { n: 0 }, E { n: 7 }] };
        println!("CROSS one_set {}", r_save_checked(&v).map(|b| b == vec!%s).unwrap_or(false));
    }
`, rustByteVector(born), rustByteVector(three), rustByteVector(oneSet))
	out, err := rustConstructProbe(t, u, body)
	if err != nil {
		t.Fatalf("the probe did not run: %v\n%s", err, out)
	}
	for _, id := range []string{"born", "three", "one_set"} {
		if !strings.Contains(out, "CROSS "+id+" true") {
			t.Errorf("%s: the Rust bytes differ from the engine's:\n%s", id, out)
		}
	}
}
