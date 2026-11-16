## THE TABLE POINTER ARRAYS

- **A pointer array is a list of nodes.** `[]*T`, `[N]*T`, `[..N]*T` and
  `[Min..N]*T` are a `list` whose elements are a `T` or `None`; `[N]*T` and
  `[Min..N]*T` are born with their least length of `None`, the others empty.
  The field rides as a kind-14 array body whose element kind is 17 and whose
  elements are indices into the file's one flat node table (0 for `None`),
  through `kind_pointer_array.py`. A shared node is one record named from
  every slot that holds it; two equal nodes built separately stay two records,
  as the engine writes them.
- **Table field guards gate the field.** A field under `if`, `if !`, `else`
  or a nested `if` is validated, numbered and written only while its guard
  holds, in the straight-line pair (the condition emitted inline) and in the
  descriptor walk (`FieldDesc.guard_holds`) alike.
- **A read refuses by name.** Beside the frame's checks, a pointer array
  refuses an element kind other than 17 (`kind_mismatch`), a count past its
  bound (`count_over_bound`) or past the int32 storage cap
  (`count_over_extent_cap`), a pointer whose record is another declared table
  and a pointer past the node table (`wire_damaged`). The whole-file check
  refuses a cycle, found by the open-node marking of its iterative walk.
- **A write refuses by the path from the root.** A value that is not a list,
  a length outside the declared bound or range, a list over the int32 count
  cap, an element that is not a table, an element that is another table
  (`Other is not a Node or None`) and a cycle each raise `InvalidValue` with
  the path from the root (`list_nodes[1].children[0]`) and the rule.
- **Nothing recurses over the pointer graph** (docs/SPEC-TABLES.md §3.1). The
  save's validation walk and its numbering walk keep one generator of edges
  per open table on an explicit stack, in the recursive walk's depth-first
  pre-order, so a chain of any length saves and measures with no
  `RecursionError`, and the leg never raises `sys.setrecursionlimit`. A cycle
  is found by the open-node marking, never by the recursion limit. The
  generated `__eq__` and `__repr__` of every table, table-side union and
  enum-keyed array walk the same way (`_schema/graph.py`): equality is
  structural and takes a pair already under comparison as equal, so two cycles
  of one shape compare equal in finite time; a repr spells each object the
  first time it is reached and `Name(...)` at every later reach, shared or
  cyclic. `TestPythonVtPointerArrays` holds a 20,000-node chain loaded from the
  engine's wire to the same bytes on save, and counts the steps of every walk
  with the profiler on that chain and on one half its length: the Python frame
  depth stays constant and the calls double with the chain.

### Pointer arrays bench

`TestPythonVtPointerArraysShapeBench` runs a nine-field table (`[]*Leaf`,
`[4]*Leaf`, `[..6]*Leaf`, `if active` with `[]*Leaf`, nested `if !muted`, and
`else *Leaf`, over shared and null `Leaf` nodes) under both pins, the
straight-line generated pair against the descriptor walk. The figures are
from the iterative save walks, on a shared 16-core AMD Ryzen 9 7950X3D fleet
bench whose load average stood between 3 and 6 while they were taken: each is
the minimum over six sittings of the minimum of five rounds of 5,000
iterations after warm-up, in nanoseconds per operation:

| operation | 3.15.2 generated | 3.15.2 descriptor | 3.11.18 generated | 3.11.18 descriptor |
|---|---:|---:|---:|---:|
| save | 30,728 | 31,481 | 29,257 | 29,738 |
| load | 44,192 | 44,857 | 48,323 | 48,290 |

The generated pair with bound locals and inline guard checks is faster on save
under both pins (2.4% under 3.15.2, 1.6% under 3.11.18) and on load under
3.15.2 (1.5%), and ties the descriptor walk on load under 3.11.18 (0.1%
slower). Save and load together it is 1.9% faster on 3.15.2 and 0.6% faster on
3.11.18, so the generated pair is kept for pointer-array and guarded tables.
