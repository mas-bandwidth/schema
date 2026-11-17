# Python table retention

Form-1 retention uses `TableRetain(record_bytes, id_bytes)`. Both arguments are
caller-owned `bytearray` objects. The second holds 24-byte entries; its length
must be a multiple of 24. `load_retain` fills the first store and resets the
second. `measure_retain` and `save_retain` re-intern retained IDs into the
caller-owned ID store. `save_retain` requires a `LoadReport` and adds losses to
its `retain_lost` counter. The three methods on a fixed-class root refuse by
name. Form-2 message retention is refused by name under SPEC-TABLES §3.3 and
§6.6.

Each record stores its original and current node index, body path, resolved
field ID, kind and payload. A reserved node-table ID marks a null reference
inside resolved payloads; the legal hash-zero ID remains distinct. A Patricia
index and its links occupy the same record buffer. Each retained-ID entry stores the ID, its merged output slot,
and an AVL index in the caller's ID buffer. Known IDs use the schema's fixed
ID inventory and the ordinary writer's own table. An unknown record is placed
whole or lost whole. The writer reserves IDs required by the known-value walk
before placing retained-only IDs; retention-forced default containers are
rolled back if no record survives. This keeps the combined form-1 trailer at or
below its 4096-entry limit.

A record costs at most its input bytes, seven additional bytes per resolved ID
reference, eight bytes per framed payload, eight bytes per path step, and 68
bytes for its fixed header and at most two index nodes. The two caller
capacities bound all retained storage. Output framing writes directly to the
final bytearray; the returned `bytes` conversion is the same output allocation
shape as the plain writer. A warmed 128-byte versus 128-KiB retained-field probe measured measure-time
peaks of 3,228/3,228 bytes on CPython 3.15.2 and 3,228/3,536 bytes on 3.11.18.
For the 131,109-byte result, save peaks were 278,778 and 278,945 bytes,
respectively, including the bytearray and returned bytes. It does not measure
the ordinary decoder's value and node storage.

On the RT1/RT2 fixture (one load plus one save, Linux, five samples of 100
round trips), the best plain/retaining times were 177,768/1,040,063 ns on
CPython 3.11.18 and 166,464/912,850 ns on CPython 3.15.2. These figures include
the shared validation and both retention walks, and are workload-specific.
