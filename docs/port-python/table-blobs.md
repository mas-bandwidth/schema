## THE TABLE BLOBS

- **Scope.** `kind_blob.py` carries the `*bytes` and `*string` buffers, and
  `kind_string.py` and `kind_bytes.py` the inline `string(N)` and `bytes(N)`,
  for both the fixed and the variable class. A map whose value is a buffer
  (`map[K]*bytes`, `map[K]*string`) still refuses by name until its card lands.
- **The frame writes the two reserved blob records** in the flat node table
  beside the `*T` nodes.
- **A buffer is written by value, never by identity.** `*bytes` and `*string`
  are `kind_blob.py`'s two kinds: the wire carries kind 17 with a node index,
  and the record's body is the bytes themselves. A Python `bytes` or `str` is
  a value, not a boxed node, so the save gives every non-null slot its OWN
  record: a caller's equal strings in two slots are two records with equal
  bodies, exactly the wire the Go engine writes for `blob_equal_bytes`. The
  records are numbered per slot visit in write order, never keyed by the
  owning table object's identity: one table object placed in two by-value
  slots, or by value and by pointer, is two visits and writes the same wire
  as two equal copies. There
  is nothing to share, so there is no save-side `blob_shared`. The read keeps
  the wire's indices: one blob record named from two slots refuses the whole
  read as `blob_shared`, a `*bytes` slot naming a `*string` record (or a
  table) refuses `wire_damaged`, a `*string` record whose bytes are not
  well-formed UTF-8 or carry a zero byte refuses `wire_damaged`, and a record
  past the `2^32 - 1` storage cap refuses `blob_over_size_cap`.
