# Python fixed-form evolution

A fixed table's reader carries its lineage: every locked older layout, each
with a plan compiled when the package is generated. This page covers what
those plans may do when a field's numeric kind changed between the writer and
the reader, and how the Python leg is held to the C++ reference on it.

## The numeric ladder

A plan widens a field only along the fixed form's ladder
(docs/FIXED-FORM-ALGORITHM.md §1): `int8` to `int64` upward, `uint8` to
`uint64` upward, `float32` into `float64`, and a `fixed(I,F)` or
`ufixed(I,F)` into a wider `I` at the same `F`, through 128-bit storage.
A ladder widen sign-extends when the writer's kind is signed and zero-extends
otherwise, and counts `widened` once per landed entry.

Every other change of kind is a kind that moved, and the lineage entry for it
gets no plan: a file under that layout is refused. That includes narrowing,
a change of signedness, a change of family, and the 128-bit integers. `int128`
and `uint128` (kinds 18 and 19) are on no fixed rung, so `int64` into `int128`
refuses here even though the variable wire's `ir.TableKindWidens` accepts it.
The generator's `fixedPythonWidens` is the reference's `TableFixedWidens`,
not the variable wire's predicate.

## The kind-pair gate

`TestPythonFixedProofEvolutionKindPairs` (`make
python-fixed-proof-evolution-kinds`) takes the 22 numeric declarations a
fixed field can have: `int8` to `int64`, a ranged `int128`, `uint8` to
`uint128`, both floats, and signed and unsigned fixed-point at all five
storage widths. The C++ reference writes one file per declaration, holding the
value a widening gets wrong: a signed minimum, an unsigned maximum, or a
fixed-point raw value with its top bit set. For each declaration as the reader,
the test builds the Python package and a live C++ reader with the other 21
layouts as its lineage, and reads all 21 files with both. That is all 462
ordered pairs.

For every pair, Python and C++ must both accept or both refuse, and the
accepted pairs must be exactly the ladder above, adjacent or not (33 pairs).
An accepted pair must give the same record count, the same `widened` and the
same saved bytes in both. Both interpreter pins must give the same answer.
`TestPythonFixedProofEvolutionKindPairsNegativeControl` (`make
python-fixed-proof-evolution-kinds-negative-control`) plants three faults on
the `int8` to `int16` pair: a changed byte in the older file, a changed byte
from the emitted writer, and the emitted runtime's widen with sign extension
removed. Each fault must break agreement with C++.

The gate runs in about 8 s on macOS arm64 (44 C++ compiles and 22 Python
packages, both pins).

## A reference defect the gate holds open

The C++ reference's widen op stores at most eight bytes
(`TableFixedStoreNative` has no 16-byte case). So a `fixed(I,F)` or
`ufixed(I,F)` widened into 128-bit storage reads back as zero, and nothing
reports it. The algorithm's widen is exact, and Python lands the value. For
those eight pairs, the test compares Python's saved bytes with the bytes the
C++ reference's own writer saves for the same value at the reader's
declaration. It also asserts that the C++ lineage reader still differs from
those bytes. When the reference is fixed, that assertion goes red and the
exception should be removed.
