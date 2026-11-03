// collections.lobster — the collections group's runtime hook: maps
// (docs/SPEC-TABLES.md §2.8), byte buffers `*bytes` and `*string` (§2.5), and
// unbounded arrays `[]T` (§2.9; docs/PORT-LOBSTER.md, THE CARD MAP).
//
// The load walker dispatches every field through the Shape registered under
// its shape key, and the last registration of a key wins
// (docs/PORT-LOBSTER.md, THE LOAD), so this file joins that hook by
// registering the three collection shapes once more. The save walker reads
// the same descriptors as data and its map order, blob nodes and list extents
// are the facts the descriptor emitter writes; collections.go is that
// emitter's part for these constructs.
// SPDX-License-Identifier: NONE — emitted output, yours under terms of your choice.

import .load

namespace schema_rt

// collection_construct names the collection a field descriptor is: "map", a
// byte buffer ("blob"), an unbounded array ("list"), or "" for every other
// shape. The test names each construct by this answer and counts it.
def collection_construct(fd:FieldDescriptor) -> string:
    if fd.s == "map":
        return "map"
    if fd.s == "sblob" or fd.s == "bblob":
        return "blob"
    if fd.s == "arr" and fd.array == "list":
        return "list"
    return ""

// THE HOOK (docs/PORT-LOBSTER.md, THE LOAD): a construct card replaces a shape
// with one registration in its own runtime file. The three collection shapes
// the load walker carries are re-registered here, so this file is the place a
// later specialisation of a map, blob or unbounded array lands and the
// collections card needs no edit to load.lobster.
let _collections_map = schema_rt.register_shape(schema_rt.shape("map"))
let _collections_ptr = schema_rt.register_shape(schema_rt.shape("ptr"))
let _collections_arr = schema_rt.register_shape(schema_rt.shape("arr"))
