package compiler

import (
	"fmt"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
)

// Run the emitted reader against independently encoded old-schema bytes.
// Saving under the new schema checks every widened value as well as reports.
func TestCppCollectionWidening(t *testing.T) {
	for _, tc := range []struct {
		name, old, new, text string
		widened, region      int
	}{
		{"list", "values []int8", "values []int32", `{"values":[-128,0,127]}`, 1, 48},
		{"map", "entries map[int8]int8", "entries map[int32]int32", `{"entries":{"-3":-5,"2":9}}`, 3, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			source := unitFromSource(t, "package fixture\ntable Root { "+tc.old+" }\n")
			target := unitFromSource(t, "package fixture\ntable Root { "+tc.new+" }\n")
			m := tabletext.NewModel(source)
			v := m.New(source.Tables["Root"])
			var report tabletext.Report
			if !m.Read(v, []byte(tc.text), &report) || !report.Silent() {
				t.Fatal(report)
			}
			wire, err := tablewire.Encode(m, v)
			if err != nil {
				t.Fatal(err)
			}
			m = tabletext.NewModel(target)
			v = m.New(target.Tables["Root"])
			if ok, err := tablewire.Decode(m, v, wire, &report); err != nil || !ok || report.Widened != tc.widened || report.Malformed {
				t.Fatalf("oracle: %+v %v", report, err)
			}
			want, err := tablewire.Encode(m, v)
			if err != nil {
				t.Fatal(err)
			}
			files, err := New().Generate(target, "cpp", Options{})
			if err != nil {
				t.Fatal(err)
			}
			referenceCompileRun(t, files, fmt.Sprintf(`#include "ProbeTable.h"
#include <cstdio>
#include <cstdlib>
#include <cstring>
using namespace fixture;
#define REQUIRE(x) do { if (!(x)) { fprintf(stderr,"collection widening failed at line %%d: %%s\n",__LINE__,#x); return 1; } } while(0)
int main(){const uint8_t wire[]={%s};const uint8_t want[]={%s};
auto n=RootLoadMeasure(wire,sizeof(wire));REQUIRE(n>0);REQUIRE(%d==0||n==%d);
auto region=(uint8_t*)malloc(n);TableReport report;auto root=RootLoad(region,n,wire,sizeof(wire),&report);
REQUIRE(root!=nullptr);REQUIRE(report.widened==%d);REQUIRE(!report.malformed&&!report.kind_mismatch);
uint8_t out[sizeof(want)];REQUIRE(RootSave(root,out,sizeof(out))==sizeof(out));REQUIRE(memcmp(out,want,sizeof(out))==0);
free(region);return 0;}
`, cppWire(wire), cppWire(want), tc.region, tc.region, tc.widened))
		})
	}
}
