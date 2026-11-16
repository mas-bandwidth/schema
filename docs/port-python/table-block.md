## THE TABLE BLOCK FORM

A fixed table has a third projection beside its wire and its cook: the block
form (docs/SPEC-TABLES.md §19). Nothing declares it. The generated class
carries its `block` and `block_row` descriptors as constant data built once at
import from the compiler's own `ir.Blocks` layout; the runtime derives no
layout of its own.

- **The modules.** `internal/codegen/pythontable/runtime/_schema/block.py` is
  the read side (Open, its refusals, and the descriptor-only row dump), and
  `_schema/blockbuild.py` is the builder (Begin, BlockMaxBytes and
  BlockBytes). The layout classes `BlockField`, `BlockRecord`, `BlockArray`
  and `BlockTable` live in `_schema/descriptor.py`, and every runtime name is
  claimed in `internal/tablenames/python.go`.
- **A packet type by value has a row too.** A packet `type` a table reaches is
  the packet core's class, so its `block_row` is attached by `setattr` after
  its descriptor; the dump reaches it by name exactly as it reaches a table's
  row.
- **Read.** `block.BlockOpen(T, name, data, build_version=None, base_offset=0)`
  returns a handle or raises `Refused`. It checks the three-word prologue
  (magic, byte order, build version), then every out-of-line array's pitch,
  count, offset and extent, then the used extent against the bytes passed, and
  last the base's alignment. A Python `bytes` object has no address, so
  `base_offset` is the caller's offset into the buffer the bytes view; a
  non-zero, non-multiple-of-64 offset is `unaligned_base`. `block.Dump(handle)`
  is the canonical row dump (testdata/conformance/tables/FORMAT.md), read
  through the descriptors alone.
- **Build.** `blockbuild.Begin(T, root, counts=None)` writes the prologue and
  each array's sixteen-byte `(offset_of, count, stride)` triple, refuses a
  count outside `[0, maximum]` with `InvalidValue` naming the array, and
  touches no row. `BlockMaxBytes` is the storage size over the maxima and
  `BlockBytes` is the used extent. The bytes are a `bytearray`, so a caller
  may fill rows in place; the base offset is supplied at Open, not stamped by
  Begin.
- **The shape is fixed.** The block form has one code shape, the descriptor
  walk: D1's two shapes are the packet and scalar-table hot paths, and the
  block read is a pointer walk over generated layout data with no alternative
  to measure.
- **The corpus.** `TestPythonBlockRead` reads both committed images
  (`block_render`, `block_padded`) value for value against Go's dump under
  both pins; its refusal battery and the builder's byte comparison to the
  generated Go and live C++ builders live beside it, each with a negative control
  (`TestPythonBlockReadNegativeControls`, `TestPythonBlockBuilderNegativeControls`).
- **The surfaces.** The conformance driver answers `block`, `block-foreign`,
  `block-dump` and `forgery` from the same runtime.
