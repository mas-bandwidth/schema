# Explicit blob nodes

`BlobRef(value)` is exported by every generated table package. Its value is a
`str` for a `*string` field or `bytes` for a `*bytes` field, fixed at
construction. Reusing one `BlobRef` instance in multiple slots explicitly names
one in-memory blob node; two distinct wrappers name distinct nodes even when
their values compare equal. Wrapper equality compares payload values, so a
table remains equal to the ordinary values returned by a wire read. Ordinary
strings, bytes, bytearrays and memoryviews
remain values, with one blob record per slot.

Form-1 save and measure accept a wrapper used in one serialized slot. They
raise `InvalidValue` with rule `blob_shared` if the same wrapper is visited by
multiple serialized slots. A table reached twice by pointer is one node visit;
a table placed by value twice is two visits. The field's existing value checks
also apply to the wrapped payload. Reads retain the existing value-per-slot
mapping and refuse a wire that names the same blob record from multiple slots.
