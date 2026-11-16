## THE TABLE FIXED CLASS

- **The fixed-class kinds ride form 1** for both the fixed and the variable
  class: bits, ranged integers, plain and compressed floats (finite only),
  enums, flags masks, strings, byte buffers, and fixed `[N]T` and counted
  `[..N]T` arrays of those kinds. `kind_float.py` carries `float32` and
  `float64`; the strings, byte buffers, enums and arrays ride the frame's own
  `kind_string.py`, `kind_bytes.py`, `kind_enum.py` and `kind_array.py`, each
  scalar answering `read_element` and `write_element` so an array element
  walks its own kind module. A float field's descriptor carries `f32`, and a
  compressed float carries `float_range` with its range in `fmin` and `fmax`;
  an array's element descriptor carries the field's size, variant ids,
  deprecated ordinals and float range, at its kind's zero. These kinds keep
  the descriptor walk; the measured straight-line pair stays the scalar hot
  path.
- **A compressed float** rides as its float32 bits on this form, held to its
  declared range on read and write (a write checks the float32 the wire will
  carry, not the unrounded value), and lands `-0` as `+0`. A plain float is
  judged at its default by its bit pattern, so `-0.0` under a `0.0` default
  rides; a compressed float by its value.
- **A fixed array** of a scalar kind elides when every element is at its
  zero, parity with the reader's prefill; a fixed array of tables always
  rides, because position is identity there.
- **A by-value table elides when every field in it elides**, judged field by
  field through the descriptors (`FieldDesc.is_default`, shared with the
  table-side unions), each against its declared default; that is the engine's
  body of its terminator alone (internal/tablewire/encode.go, kind 13), and an
  elided table interns no id. No value compare decides it, so a user `__eq__`
  cannot; a by-value
  table, a table array element, a pointer target and the root must be exactly
  the generated class, so a subclass is refused with `InvalidValue`. The
  generated `__eq__` compares a float by its bits, as the packet surface does.
- **A deprecated enum variant is retired in place** (SPEC-TABLES §2.15): the
  descriptor carries its ordinal, a read of it lands the field's declared
  default (an array element's `None`) and counts nothing, and a write of it
  raises `InvalidValue`.
- **Constants, enums and flags ride beside the tables.** A table unit's
  modules carry the file's constants as `Final` values and its enums and
  flags sets as the packet surface's classes, and `__init__.py` re-exports
  them.
- **The build version is beside the protocol id.** Each generated package's
  `__init__.py` carries `BUILD_VERSION`, SPEC-TABLES §20's compiler-settled id,
  as a literal beside `PROTOCOL_ID`; the table wire never writes it.
- **A packet type by value keeps its floats refused** in the table runtime
  (`kind_unsupported.py`) until the packet-types card carries them; only a
  table's own fields ride the float kinds here.

### Bench

Both saves first check that the root is exactly its generated class (a
subclass could carry another descriptor). Re-measured on a SHARED fleet bench
under other load, so the figures are slower than THE TABLE FRAME's bench
throughout and compare only with each other; the left of each pair is the
runtime before the check, the right after it, on the same host minutes apart,
in nanoseconds per operation:

| operation | 3.15.2 generated | 3.15.2 descriptor | 3.11.18 generated | 3.11.18 descriptor |
|---|---:|---:|---:|---:|
| save | 19,541 / 20,480 | 21,508 / 23,281 | 18,990 / 19,733 | 20,555 / 21,206 |
| load | 21,538 / 22,086 | 22,729 / 23,014 | 24,440 / 24,646 | 24,726 / 23,960 |

The check costs the save under 5% on this host (the loads, which it does not
touch, moved by up to 3% in the same runs, which is the noise), and the
generated save stays the faster shape on both pins, so the pair is kept.

A separate Linux x86-64 run compared the same scalar shape on the fixed-class
integration and its base. The table lists base / integration nanoseconds per
operation from `TestPythonTableCoreShapeBench`; each cell is the best of five
10,000-operation rounds on a shared host, so scheduler noise is visible.

| operation | 3.15.2 generated | 3.15.2 descriptor | 3.11.18 generated | 3.11.18 descriptor |
|---|---:|---:|---:|---:|
| save | 11,504 / 11,688 | 14,529 / 14,724 | 11,450 / 11,593 | 13,726 / 14,274 |
| load | 11,410 / 11,315 | 11,266 / 11,258 | 12,138 / 12,467 | 12,149 / 12,426 |

The generated save remained faster on both pins. The load results were close;
these measurements do not establish a general load-speed advantage.
