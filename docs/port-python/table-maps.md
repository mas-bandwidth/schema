## THE TABLE MAPS

- **Maps are dicts, written sorted.** A `map[K]V` is a `dict` born empty; the
  save writes its entries in ascending key order (text by its UTF-8 bytes,
  integers by value), and the read refuses a descending or a repeated key as
  `Refused("wire_damaged")`, because no conforming writer emits either. A
  request that does not arrive in that order is not a map this leg accepts;
  the whole read refuses and returns nothing. `kind_map.py` owns the entry
  framing, the sort and the order check.

### The map row over the manifest

`TestPythonVtMapsRows` (internal/codegen/pythontable/vtmaps_rows_test.go)
holds the roadmap row `port/python/vt/maps` on every conformance manifest
instance and report row whose root reaches a map (23 rows over the
`featurewitnesses`, `mapdemo` and `listdemo` units) and on the two Go-engine
constructs the other legs' map rows name (`unions_maps_keyed`,
`optional_map`). Each is read through the strict load and written back under
both pinned interpreters: accepted and written back byte-identical to the
canonical wire (an instance's own wire, a report row's engine re-encoding),
its measure its length and its widened counter the engine's; or refused,
as the engine or the manifest's `wire-refused` marker refuses it
(`blob_shared_string`), with the caller's buffer and report untouched. A
completeness check, which runs without the interpreters, refuses a manifest
row that reaches a map and is not named. `TestPythonVtMapsRowsNegativeControl`
plants in a copy of `kind_map.py` the reader's key-order test inverted and
the writer's entry count one too many, and requires the named rows red under
each. The row found no gap in `kind_map.py`: no runtime change was needed.
`make python-vt-maps-rows` runs both.

### Maps bench

A table that holds a map is variable, so it never takes the straight-line
pair: its load and save are the descriptor walk. Inside the walk the map's
save has two shapes. The first builds an entry object per key and walks it
through `write_body`; the second writes each entry body straight from the key
and the value, the same bytes (the key, then the value, each elided at its
default, then the end mark) without the object. `TestPythonMapShapeBench`
(internal/codegen/pythontable) times both on a 64-entry `map[uint32]int32`
and a 16-entry `map[uint16]*Node`, and the load of each, under both pins.
Each figure is the minimum of five rounds of 2,000 iterations after warm-up,
taken over six sittings, in microseconds per operation. They were measured on
a shared fleet bench whose load average stood between 67 and 112 on 16 cores,
so single sittings swing by up to 2x; the minima are the figures to compare:

| operation | 3.15.2 | 3.11.18 |
|---|---:|---:|
| scalar map save, direct entry bodies (kept) | 141.1 | 135.9 |
| scalar map save, entry objects | 162.0 | 155.2 |
| pointer map save, direct entry bodies (kept) | 73.1 | 67.5 |
| pointer map save, entry objects | 75.6 | 75.8 |
| scalar map load | 184.3 | 202.0 |
| pointer map load | 82.7 | 88.2 |

The direct save is 13% faster on the scalar map under 3.15.2 and 12% under
3.11.18, and 3% and 11% faster on the pointer map, whose time goes mostly to
the node table; in 21 of the 24 comparisons across both pins it was the faster
of the two. `kind_map.py` writes the direct shape. The load reads each entry
through `read_body`, which holds the unknown-field skip, the repeated-field
and kind checks and the pointer fix-up in one place, so it has one shape.
