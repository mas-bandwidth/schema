## THE TABLE UNBOUNDED ARRAYS

- **`kind_array.py` reads and writes an array element** through the element's
  own kind module, so a later element kind arrives without an edit to the
  array walk.
- **Scope.** The runtime carries unbounded arrays `[]T` of bool, the integers
  (bits included) and by-value tables of both classes (a `fixed table` element
  and a variable one), for both the fixed and the variable class. A `*T`
  pointer rides only inside a by-value table element; `[]*T` belongs to the
  pointer-array card. An unbounded array is a
  kind `14` counted array with no declared bound, so it rides the same bytes a
  `[..N]T` writes. Its count is held to the read's resource bounds: a body
  shorter than its header, a count past the int32 storage cap, a count the
  body cannot cover, and bytes left inside its `L` after the elements all
  refuse, the first three before a list is built. A present plain array with a
  count of 0 is its field's declared default, which every writer elides, so
  the read refuses it as a non-canonical spelling (SPEC-TABLES §4,
  docs/rulings.json reads-refuse-non-canonical); an optional array's present
  empty value is not refused there. A write refuses a list longer than the
  int32 cap (`InvalidValue`), so the writer never emits a count its own read
  refuses.
- **The array element shape is measured.** An element of a fixed-width integer
  kind of 1 to 8 bytes reads with one `struct.unpack` over the whole body and
  one range check over the decoded values, and writes with one `struct.pack`;
  a bool element reads as one slice checked for bytes other than 0 and 1, and
  writes as one `bytes(list)`. Every other element (int128, tables, pointers)
  walks its kind module one element at a time. `TestPythonVtUnboundedShapeBench`
  holds the two shapes to the same value and bytes and times them (Bench
  below); the bulk shape is kept.

### Array bench

The array bench (`TestPythonVtUnboundedShapeBench`) reads and writes one
array body of 1,000 elements in each shape: `int32` held to a declared range,
full-range `uint64`, and `bool`. It ran on a SHARED fleet bench with other
work on it, so the absolute figures are noisy (two runs differed by up to 30%)
and only the ratio is the finding. Each figure is the minimum of five rounds of
200 iterations after a check that both shapes agree, in nanoseconds per array:

| operation | 3.15.2 walk | 3.15.2 bulk | 3.11.18 walk | 3.11.18 bulk |
|---|---:|---:|---:|---:|
| read `[]int32` | 1,077,958 | 75,194 | 1,411,906 | 80,748 |
| write `[]int32` | 724,275 | 15,444 | 1,063,570 | 19,861 |
| read `[]uint64` | 1,064,249 | 66,265 | 1,577,614 | 110,449 |
| write `[]uint64` | 720,604 | 38,119 | 1,108,210 | 42,423 |
| read `[]bool` | 373,830 | 37,523 | 546,002 | 51,348 |
| write `[]bool` | 271,698 | 16,881 | 342,126 | 11,968 |

The bulk shape is 10 to 54 times faster on every row and both pins, so it is
kept for the elements it carries.
