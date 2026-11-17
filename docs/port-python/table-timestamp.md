## THE TABLE TIMESTAMP

- **Scope.** The table runtime carries `timestamp` for both the fixed and the
  variable class.
- **`timestamp` is the int64 it is.** On the form-1 wire a `timestamp` field
  rides kind `36` with an eight-byte two's-complement little-endian
  nanosecond count (docs/SPEC-TABLES.md §2.11). Its Python value is an `int`
  in int64's range, checked on write as every integer is, and the descriptor
  carries that range. The zero count elides like the int64 zero, and a
  field respelled between `int64` (kind `5`) and `timestamp` (kind `36`)
  refuses as `kind_mismatch` in both directions: neither spelling widens
  into the other. The text forms land with the text cards (py-json-2,
  py-handtext-2) and the fixed form with py-fixed-form-2;
  `TestPythonVtTimestamp` holds the form-1 read-then-write, both respelling
  directions, a wire cut inside the count and the write refusals to the Go
  engine's bytes and reasons under both pinned interpreters, and its
  negative controls plant one fault per check.
