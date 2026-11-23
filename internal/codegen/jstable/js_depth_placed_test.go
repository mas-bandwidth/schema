package jstable

import "testing"

const jsDepthPlacedSchema = `package probe

table Item
{
    count int32
}

table Squad
{
    roster map[uint8]Item
}

union Force
{
    squad Squad
    plain int32
}

table Root
{
    one Squad
    arm Force
}
`

// TestJsTextReadDepthCountsPlacedMapAndUnion: the text read's depth cap counts
// every container it walks, so a placed map's object and a union's object each
// count toward the 129-container cap (docs/SPEC-TABLES.md §16.2; conformance
// rows rr-depth-placed-union-map-128/-129 and rr-depth-placed-keyed-map-128/-129,
// docs/fix-ledger-1122.md). The deepest container at depth 128 reads; one more
// refuses. Red with the map's and the union's own level not counted.
func TestJsTextReadDepthCountsPlacedMapAndUnion(t *testing.T) {
	t.Parallel()
	runJsVariableProbe(t, jsDepthPlacedSchema, `
const nest = (n) => "[".repeat(n) + "]".repeat(n);
// root 0, union 1, Squad 2, map 3, entry 4, x 5..128
const viaUnion = (n) => '{"arm":{"squad":{"roster":{"7":{"count":1,"x":' + nest(n) + '}}}}}';
// root 0, Squad 1, map 2, entry 3, x 4..128
const viaMap = (n) => '{"one":{"roster":{"5":{"count":1,"x":' + nest(n) + '}}}}';
if (RootFromJson(viaUnion(124), new TableVariableReport()) === null) { fail("union then map at depth 128 was refused"); }
if (RootFromJson(viaUnion(125), new TableVariableReport()) !== null) { fail("union then map at depth 129 was read"); }
if (RootFromJson(viaMap(125), new TableVariableReport()) === null) { fail("map at depth 128 was refused"); }
if (RootFromJson(viaMap(126), new TableVariableReport()) !== null) { fail("map at depth 129 was read"); }
`)
}
