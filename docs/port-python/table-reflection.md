## Python table reflection

`UnitView()` returns the unit's declaration registry. Its six sets are in
schema-name order, with variants in declaration order. The registry is loaded
on first use; ordinary imports do not instantiate it. Each table-closure row
points at the same descriptor its generated class exposes. Packet types outside
a table closure receive view-only descriptors: their field IDs and JSON keys
are absent because no table-wire identity was checked for them. Generated map
entry tables remain reachable through their containing field and do not become
registry rows.

Descriptors carry their own docs and ordered tags, text key, type spelling,
wire and element kinds, bounds, shape, compiler layout, guards, wide raw range,
and nested record descriptor. Enum and union names and IDs, flags bit names,
and enum-keyed slot names and IDs are available on the field descriptor even
when the unit registry has not been loaded. A flags field has a bit-name
callback and no variant-ID callback; non-keyed fields have no key callbacks.
`union_desc` reaches its arms and their payload descriptors. `kind` is the form-1 field framing kind and
`elem_kind` is the element kind, following the runtime's existing vocabulary;
for arrays a walker reads the latter to inspect the element. `get`, `set`,
`get_element` and `set_element` operate on Python values; string elements use
Python character indexing, while byte buffers use byte indexing. Updating an
immutable string or bytes value replaces it. `reset(value)` establishes that
value's defaults in place. Fixed arrays initialize at runtime: emitted source
stays constant in the declared bound, immutable zeros can repeat, and each
nested element is constructed separately to avoid aliasing. Python lists
require allocation on reset, unlike an in-place C array. The layout
numbers describe the compiler's portable cook-record layout; Python objects do
not expose raw addresses at those offsets.

The unit view also carries constants and the enum, flags and union vocabulary.
A record-naming arm points to its record descriptor; a general arm carries its
own field descriptor. A vocabulary not reached by any table carries zero
variant IDs. Docs and tags do not enter the wire or change the build identity.
