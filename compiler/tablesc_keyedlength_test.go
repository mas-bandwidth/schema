package compiler

import (
	"fmt"
	"strings"
	"testing"
)

// A named enum-keyed slot's L frames its element exactly (docs/SPEC-TABLES.md
// §4): bytes left inside the slot after a scalar element refuse the read, as
// they do after a table element. The verdicts below are the Go engine's; the
// C read must answer the same.
func TestCTableKeyedScalarSlotExactLength(t *testing.T) {
	t.Parallel()
	const source = `package probe
enum Axis {
X
Y
}
table Root {
wide [Axis]int64
narrow [Axis]int32
}
`
	u := unitFromSource(t, source)
	cases := keyedCases(t, u, "Root", u, `{"wide":{"X":5,"Y":-7},"narrow":{"X":3,"Y":9}}`, "", "")
	if len(cases) != 1 || !cases[0].accept {
		t.Fatalf("control: the engine refuses the clean slots")
	}
	wire := cases[0].wire
	pad := func(b []byte) []byte { return append(b, 0x00) }
	cases = append(cases,
		keyedVerdictCase{name: "8-byte slot with L one past its element", wire: keyedDamage(t, wire, "wide", "X", pad)},
		keyedVerdictCase{name: "4-byte slot with L one past its element", wire: keyedDamage(t, wire, "narrow", "Y", pad)},
	)
	for i := 1; i < len(cases); i++ {
		cases[i].accept, cases[i].unknown = keyedGoVerdict(t, u, "Root", cases[i].wire)
		if cases[i].accept {
			t.Fatalf("%s: the engine accepts", cases[i].name)
		}
	}
	var probe strings.Builder
	probe.WriteString(`#include "ProbeTable.h"
#include <stdio.h>
#include <stdlib.h>
#define CHECK(x) do { if (!(x)) { fprintf(stderr, "line %d: %s\n", __LINE__, #x); return 1; } } while (0)
`)
	for i, c := range cases {
		fmt.Fprintf(&probe, "/* %s */\nstatic const uint8_t c%d[]={%s};\n", c.name, i, cOctets(c.wire))
	}
	probe.WriteString("int main(void) {\n TableReport report; const Root * root; uint8_t * region; int64_t need;\n")
	for i, c := range cases {
		accept := 0
		if c.accept {
			accept = 1
		}
		fmt.Fprintf(&probe, ` { need=root_load_measure(c%d,sizeof(c%d)); CHECK(need>0); region=(uint8_t*)malloc((size_t)need); CHECK(region!=NULL);
   memset(&report,0,sizeof(report)); root=root_load(region,need,c%d,sizeof(c%d),&report);
   CHECK((root!=NULL)==%d); CHECK(report.refused==%d); free(region); }
`, i, i, i, i, accept, 1-accept)
	}
	probe.WriteString(" return 0;\n}\n")
	runCTableWireProbe(t, u, probe.String())
}
