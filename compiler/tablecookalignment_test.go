package compiler

import (
	"fmt"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
)

// A root's extent can require more alignment than the record itself. The
// minimum is type-based for its own extent, but excludes absent pointer nodes.
func TestCppCookOpenNodeAlignment(t *testing.T) {
	for _, tc := range []struct {
		name, source, text string
		align              int
	}{
		{"wide-list-empty", "fixed table Wide { n uint128 }\ntable Root { values []Wide }", "{\"values\":[]}", 16},
		{"wide-list", "fixed table Wide { n uint128 }\ntable Root { values []Wide }", "{\"values\":[{\"n\":7}]}", 16},
		{"absent-wide-pointer", "fixed table Wide { n uint128 }\ntable Root { next *Wide }", "{}", 8},
	} {
		t.Run(tc.name, func(t *testing.T) {
			files, arrays := cppCookAlignmentFixture(t, tc.source, tc.text)
			source := cppCookAlignmentPrelude + arrays + fmt.Sprintf(`
int main() {
    (void)wire;
    static_assert(alignof(Root)==8, "exercise narrower root record");
    alignas(64) uint8_t bytes[sizeof(little)+16];
    const uint8_t * canonical = TableCookByteOrder == 1 ? little : big;
    memcpy(bytes, canonical, sizeof(little));
    REQUIRE(table_cook_read64(bytes+40)==%d);
    TableRefuseReason reason=TableRefuseReason::truncated;
    REQUIRE(RootOpen(bytes,sizeof(little),&reason)!=nullptr);
    REQUIRE(reason==TableRefuseReason::truncated);
    // Also exercise an eight-mod-sixteen address; never dereference a
    // pointer returned by the old, insufficient alignment check.
    memcpy(bytes+8, canonical, sizeof(little));
    if (%d==8) {
        REQUIRE(RootOpen(bytes+8,sizeof(little))!=nullptr);
    } else {
        table_cook_put(bytes+8+40,8,8,TableCookByteOrder==1 ? TableByteOrder::Little : TableByteOrder::Big);
        REQUIRE(RootOpen(bytes+8,sizeof(little),&reason)==nullptr);
        REQUIRE(reason==TableRefuseReason::bad_alignment);
        memcpy(bytes, canonical, sizeof(little));
        table_cook_put(bytes+40,8,8,TableCookByteOrder==1 ? TableByteOrder::Little : TableByteOrder::Big);
        REQUIRE(RootOpen(bytes,sizeof(little),&reason)==nullptr);
        REQUIRE(reason==TableRefuseReason::bad_alignment);
    }
    return 0;
}
`, tc.align, tc.align)
			referenceCompileRun(t, files, source)
		})
	}
}

// Cook extents use canonical scalar alignment; native arena/locked extents
// use the actual C++ storage type. A preceding byte array exposes the gap
// when serialize's portable uint128_t is an eight-aligned pair.
func TestCppCookExtentCanonicalAlignment(t *testing.T) {
	for _, tc := range []struct{ name, source, text string }{
		{"root", "table Root { tiny []uint8\nwides []uint128 }", "{\"tiny\":[1],\"wides\":[1267650600228229401496703205383]}"},
		{"nested", "table Bucket { tiny []uint8\nwides []uint128 }\ntable Root { buckets []Bucket }", "{\"buckets\":[{\"tiny\":[1],\"wides\":[7]}]}"},
		{"schema-type-named-Cook", "table Cook { tiny []uint8\nwides []uint128 }\ntable Root { buckets []Cook }", "{\"buckets\":[{\"tiny\":[1],\"wides\":[7]}]}"},
		{"pointer-node", "table Bucket { tiny []uint8\nwides []uint128 }\ntable Root { next *Bucket }", "{\"next\":{\"tiny\":[1],\"wides\":[7]}}"},
		{"map-entry", "table Bucket { tiny []uint8\nwides []uint128 }\ntable Root { buckets map[uint8]Bucket }", "{\"buckets\":{\"2\":{\"tiny\":[1],\"wides\":[7]}}}"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			files, arrays := cppCookAlignmentFixture(t, tc.source, tc.text)
			source := cppCookAlignmentPrelude + arrays + `
int main() {
    RootBuilder builder;
    TableReport report;
    REQUIRE(RootLoadBuilder(builder,wire,sizeof(wire),&report));
    REQUIRE(!report.damaged&&report.refuse_reason==ok);
    alignas(64) uint8_t out[sizeof(little)+1];
    for (int locked=0; locked<2; ++locked) {
        uint8_t saved[sizeof(wire)];
        REQUIRE(RootSave(builder,saved,sizeof(saved))==sizeof(wire));
        REQUIRE(memcmp(saved,wire,sizeof(wire))==0);
        if (!locked) {
            TableArenaCtx ctx = { &builder.arena };
            REQUIRE(RootExtent(ctx,*builder.GetRoot()) ==
                    RootExtent<TableArenaCtx>(ctx,*builder.GetRoot()));
        }
        REQUIRE(RootCookMeasure(builder)==sizeof(little));
        memset(out,0xa5,sizeof(out));
        REQUIRE(!RootCook(builder,out,sizeof(little)-1,TableByteOrder::Little));
        for(size_t i=0;i<sizeof(out);++i) REQUIRE(out[i]==0xa5);
        REQUIRE(RootCook(builder,out,sizeof(little),TableByteOrder::Little));
        REQUIRE(memcmp(out,little,sizeof(little))==0);
        REQUIRE(out[sizeof(little)]==0xa5);
        REQUIRE(RootCook(builder,out,sizeof(big),TableByteOrder::Big));
        REQUIRE(memcmp(out,big,sizeof(big))==0);
        REQUIRE(out[sizeof(big)]==0xa5);
        REQUIRE(builder.Lock());
    }
    return 0;
}
`
			for _, portable := range []bool{false, true} {
				t.Run(fmt.Sprintf("portable128=%v", portable), func(t *testing.T) {
					var flags []string
					if portable {
						flags = append(flags, "-U__SIZEOF_INT128__")
					}
					referenceCompileRun(t, files, source, flags...)
				})
			}
		})
	}
}

const cppCookAlignmentPrelude = `#include "ProbeTable.h"
#include <cstdio>
#include <cstring>
using namespace fixture;
#if !defined(__SIZEOF_INT128__)
static_assert(alignof(serialize::uint128_t)==8, "portable storage must exercise native/cook alignment difference");
#endif
#define REQUIRE(x) do { if (!(x)) { fprintf(stderr,"cook alignment line %d: %s\n",__LINE__,#x); return 1; } } while(0)
`

func cppCookAlignmentFixture(t *testing.T, source, text string) (map[string][]byte, string) {
	t.Helper()
	u := unitFromSource(t, "package fixture\n"+source+"\n")
	model := tabletext.NewModel(u)
	value := model.New(u.Tables["Root"])
	var report tabletext.Report
	if !model.Read(value, []byte(text), &report) || !report.Silent() {
		t.Fatalf("input: %+v", report)
	}
	wire, err := tablewire.Encode(model, value)
	if err != nil {
		t.Fatal(err)
	}
	var arrays strings.Builder
	fmt.Fprintf(&arrays, "static const uint8_t wire[]={%s};\n", cppWire(wire))
	for _, big := range []bool{false, true} {
		cooked, _, _, err := New().Cook(u, "Root", wire, CookOptions{Big: big})
		if err != nil {
			t.Fatal(err)
		}
		name := "little"
		if big {
			name = "big"
		}
		fmt.Fprintf(&arrays, "static const uint8_t %s[]={%s};\n", name, cppWire(cooked))
	}
	files, err := New().Generate(u, "cpp", Options{})
	if err != nil {
		t.Fatal(err)
	}
	return files, arrays.String()
}
