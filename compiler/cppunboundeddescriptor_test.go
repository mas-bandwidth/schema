// THE UNBOUNDED CONTAINER IS EXPLICIT IN THE C++ DESCRIPTOR
// (docs/SPEC-TABLES.md §8.1, §2.9). A reflection reader must tell "no bound"
// from a bound of 2^31 - 1, so a map and a []T carry unbounded_container true
// with array_bound 0 beside it, while a bounded [..N]T carries false and its
// bound.
//
// THE ANSWERS ARE THE GO ORACLE'S, transcribed for the constructs C++ carries.
// internal/codegen/gotable/unboundeddescriptor_test.go
// TestGoUnboundedDescriptorIsExplicit pins the same schema text on the same
// fields: map[string(8)]int32 and map[int32]int32 (its rows tags and ints),
// []int32 (nums) and [..4]int32 (quad). That file's TestDescriptor is the case
// each row below cites; the C++ probe asserts only what it asserts there. Its
// words []string(8) text-list row and its names []string and byname
// map[string]int32 unbounded-TEXT rows (§2.13.1) are text-list cards, which
// C++ refuses here and a later card carries.
package compiler

import (
	"testing"
)

func TestCPPUnboundedDescriptorIsExplicit(t *testing.T) {
	t.Parallel()

	// TestGoUnboundedDescriptorIsExplicit's schema, minus the text-list rows
	// (§2.13.1) C++ refuses: its map, []T and [..N]T rows stay.
	schema := `package probe

table Root
{
    tags map[string(8)]int32
    ints map[int32]int32
    nums []int32
    quad [..4]int32
}
`
	files, err := New().Generate(unitFromSource(t, schema), "cpp", Options{})
	if err != nil {
		t.Fatalf("generate: %v", err)
	}

	// The rows Go's TestDescriptor checks: tags, ints, nums are container true
	// bound 0; quad is container false bound 4.
	driver := `#include "ProbeTable.h"
#include <cstdio>
#include <cstring>

using namespace probe;

static int failures = 0;

static void check( const TableFieldInfo & f, const char * name, bool is_array, bool container, int32_t bound )
{
    if ( f.name == NULL || strcmp( f.name, name ) != 0 ||
         f.is_array != is_array || f.unbounded_container != container || f.array_bound != bound )
    {
        fprintf( stderr, "descriptor %s: name=%s is_array=%d unbounded_container=%d array_bound=%d; want %s is_array=%d unbounded_container=%d array_bound=%d\n",
                 name, f.name == NULL ? "(null)" : f.name, (int) f.is_array, (int) f.unbounded_container, (int) f.array_bound,
                 name, (int) is_array, (int) container, (int) bound );
        failures++;
    }
}

int main()
{
    const TableTypeInfo * t = RootTableType();
    if ( t->num_fields != 4 )
    {
        fprintf( stderr, "field count %d, want 4\n", (int) t->num_fields );
        return 1;
    }
    // the Go oracle's container(false) rows: tags and ints maps
    check( t->fields[0], "tags", true, true, 0 );
    check( t->fields[1], "ints", true, true, 0 );
    // the Go oracle's container(true) row: nums []int32
    check( t->fields[2], "nums", true, true, 0 );
    // its bounded row: quad [..4]int32 is neither unbounded nor bound 0
    check( t->fields[3], "quad", true, false, 4 );
    return failures == 0 ? 0 : 1;
}
`
	runCppTableProbe(t, files, driver)
}
