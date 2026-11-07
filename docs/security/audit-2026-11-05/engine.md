# Engine audit

Report only. Base `a007c180ba4b09c7d9d5404de6e072043e034777`. No parser behavior was changed. No hostile input, corpus entry, or fuzz seed was added. A path described as reachable is reachable from the control flow below; none of these paths was executed.

Encode and write functions are not untrusted readers and are not entry points here.

## Entry points

| function | input | bound relied on | verdict |
|---|---|---|---|
| `Decode` / `decodeWith` | file bytes | id-table count at most the id-table bound and at most the buffer can frame; a length is checked against the remaining buffer before it is used as a slice; ill-formed text sets malformed and the instance is cleared; a non-finite float refuses the read and clears | open finding |
| `decodeFixed` | file bytes | layout length is compared with the bytes after the header where `int` holds a `uint32`; one record; a count or used length past the declaration is refused | bounded |
| `decodeFixedLineage` | layout bytes and the lock | the file layout is compared with the lock before the record is read; the layout walk stops at the fixed-form depth cap; over-bound counts and lengths are refused | bounded |
| `DecodeMessages` | batch bytes and an announced vocabulary | the batch count is one byte plus one and is refused above the caller storage; damage or a non-finite float clears every body | open finding |
| `Vocabulary.AnnounceRead` | announcement file bytes | the vocabulary byte length is checked before an entry is parsed; shape decode refuses a width, minimum, or maximum the kind cannot hold; the entry count stops at the vocabulary bound; two entries with the same id, kind, and shape are refused | bounded |
| `MessageCount` | batch bytes | form byte, then one count byte | bounded |
| `trailer` / `readLeb` | file tail, length bytes | entry count bounded as in `Decode`; a LEB128 longer than ten bytes, or not minimal, is refused | bounded |
| `FileRegionMeasure` | file bytes and a unit | the walk follows the file framing; a text length above 32 bits is refused; nesting follows the unit | bounded |
| `retain` `walkContent` | retained records | depth cap 64; the store is reset when the file read clears for malformed, a missing required field, or a non-finite float | open finding |
| `Model.Read` | JSON text | depth 128; a failed read clears the instance; an integer token saturates past a fixed digit band; ill-formed UTF-8 is refused | open finding |
| `readMap` | JSON object | an over-long string key drops the entry; the duplicate test is a hash map, so it stays linear in the entry count | open finding |
| `jsonLayerAccepts` | one JSON text | depth 128 through `skipValue` | open finding |
| `ReadHeader` | cook file | each part length must fit the file before the header size is used; alignment is a power of two from 8 to 64 | bounded |
| `Uncook` | cook file | the directory is checked against the data part; a blob length is checked against the node extent before the copy; a used length or count outside the declared bound is refused | bounded |
| `Unpack` | wire bytes, then a directory | decode runs before any write; a name that becomes a path component must be one local component | bounded |
| `rfc3339.Parse` | timestamp text | fields have fixed widths; extra fraction digits are consumed inside the input; the Unix time domain is checked before the scaling multiply | bounded |
| `listwalk.Bearing` / `mapwalk.Bearing` | a unit, and generated source when the gate slices it | the answer is the unit closure; a source slice is a linear scan between markers | bounded |
| `jsontestsuite.Load` | the spec text and the local corpus | harness only; not a packet parser | open finding |

`Unpack` is bounded for the path step. The wire decode it calls has the open findings on `Decode` and `DecodeMessages`.

`decodeFixed` is bounded where `int` is 64 bits. The 32-bit conversion is finding 9 and was not executed.

## Findings

### 1. high — `internal/tablewire/messagedecode.go:533`

An announced array count can drive `bitDecoder.array` through one call per element when `elementRunBits` is negative, and `scalar` at `internal/tablewire/messagedecode.go:763` returns without reading a bit for every kind that is not a fixed-width value. `skip` at `internal/tablewire/messagedecode.go:1075` stops a run that consumed no bits; this loop does not. A moved range is not a kind mismatch (`internal/tablewire/messagedecode.go:313`), so the count is the announcement, and shape decode allows that count up to the list maximum, including a count that occupies no bits because its minimum equals its maximum. This is the path for an array element whose kind is string, wide string, or array. Table, union, and enum elements read a reference and are not this case. Fixed-width elements cap the walked count at the reader bound and step the surplus arithmetically. Reachable when a fixed table declares such an array and `AnnounceRead` has accepted an entry with that field id, the same kind, and the same element kind. Not executed.

Reduction: walk those elements through the same text, wide-text, or nested read the field path uses, and stop a zero-consumption element the way `skip` does.

not added: report-only

### 2. medium — `internal/tabletext/read.go:350`

An unpaired surrogate escape in a JSON string is stored as the replacement character, so two different texts become one value. Ill-formed UTF-8 on this path is refused. The wire readers refuse an unpaired surrogate in a wide string.

Reduction: refuse that escape by the string-lone-surrogate rule. A tolerant path another card is removing; left open.

not added: report-only

### 3. medium — `internal/tabletext/read.go:735`

`readTableKeys` discards the clamped result of `scanString` at the 255-byte key cap, and so does `skipContainer` at `internal/tabletext/read.go:628`, plus the same call at `internal/tabletext/read.go:990`, `internal/tabletext/read.go:1423`, `internal/tabletext/read.go:1488`, `internal/tabletext/read.go:1689`, and `internal/tabletext/read.go:1806`. A key longer than 255 decoded bytes is shortened and the text is accepted; the shortened key selects a field when a declared key equals that prefix. `readMap` at `internal/tabletext/read.go:2249` keeps the flag and drops the entry.

Reduction: treat a clamped object key as a refusal, which is what the map path already does for a string key.

not added: report-only

### 4. medium — `internal/tabletext/read.go:764`

A repeated JSON object key is last-wins: `readTableKeys` counts duplicate and keeps the later value, `readMap` does the same through its identity map, and `skipContainer` accepts a repeat with no counter. The file map reader does the same at `internal/tablewire/decode.go:838`. The layer oracle accepts those texts. The spec rows for a repeated key are a refusal.

Reduction: refuse a repeated key as malformed on the JSON layer, the table walk, the map walk, and the file map. A tolerant path another card is removing; left open.

not added: report-only

### 5. medium — `internal/tablewire/decode.go:569`

A file string longer than the declared bound is clamped and the read succeeds. The same success-with-clamp is the file wide string at `internal/tablewire/decode.go:606`, the file array count at `internal/tablewire/decode.go:1045`, the file scalar range at `internal/tablewire/decode.go:1604`, the message string at `internal/tablewire/messagedecode.go:409`, the message wide string at `internal/tablewire/messagedecode.go:446`, the message array count at `internal/tablewire/messagedecode.go:523`, the JSON integer at `internal/tabletext/read.go:1642`, and the JSON float range at `internal/tabletext/read.go:1607`. A non-finite JSON number is not stored; it counts a kind mismatch at `internal/tabletext/read.go:1600` and the read can still succeed. A non-finite file or message float refuses the read and clears. The fixed form refuses an over-bound count or length (`internal/tablewire/fixed.go:427`, `internal/tablewire/fixed.go:438`).

Reduction: refuse these as out of range, length over bound, or count over bound, and refuse a non-finite JSON number the way the wire read does. A tolerant path another card is removing; left open.

not added: report-only

### 6. medium — `internal/tablewire/decode.go:504`

A file field whose kind does not match is skipped and the field keeps its default, and the parent keeps reading. The message body does the same at `internal/tablewire/messagedecode.go:333` by skipping the announced entry. JSON does the same for a value whose shape is not the field, at `internal/tabletext/read.go:782`, and for an unknown key by skipping it.

Reduction: refuse the body on a kind mismatch instead of skipping. A tolerant path another card is removing; left open.

not added: report-only

### 7. low — `internal/tablewire/decodenodes.go:288`

`decodeVariable` returns a count refusal without setting malformed, and `decodeWith` at `internal/tablewire/decode.go:93` clears the instance for a missing required field, a non-finite float, and malformed, then returns this error with the partial instance still filled. The error text says the caller discards that value. A region read sets malformed on the same cap and is cleared. The retention store is reset on the paths `decodeWith` clears, and is left in place on this one.

Reduction: clear the instance and reset the retention store on this error, as the malformed path does.

not added: report-only

### 8. low — `internal/jsontestsuite/policy.go:197`

`ParseTable` accepts only three verdict spellings, and the spec dialect-refuse spelling on the two repeated-key rows is rejected, so the engine JSON suite cannot load the oracle. `readCorpus` at `internal/jsontestsuite/policy.go:115` ignores the scanner error, so a sums line past the default token limit drops every later pin: a corpus file whose pin was dropped fails closed, and an extra pin after that line is not seen. This is the local harness, not a packet path.

Reduction: accept the dialect-refuse spelling as a refusal, including on a `y_` row, and surface the scanner error. Teaching the spelling to the oracle still leaves the suite red, because finding 4 accepts those rows. Left open. No corpus row was added.

not added: report-only

### 9. info — `internal/tablewire/messagedecode.go:389`

`text` multiplies a length by eight after converting it to `int`, and `wideText` at `internal/tablewire/messagedecode.go:423` multiplies by sixteen. `skip` does the same at `internal/tablewire/messagedecode.go:1001` and `internal/tablewire/messagedecode.go:1008`. Shape decode caps a string or wide-string length at the maximum signed 32-bit value. Where `int` is 64 bits the product fits. Where `int` is 32 bits the product wraps, `has` rejects a negative count, and the read fails closed. `decodeFixed` at `internal/tablewire/fixed.go:323` converts a `uint32` layout length to `int`; where `int` is 32 bits a length with the high bit set is negative, the remaining-length comparison does not reject it, and the slice can panic. Not reachable where `int` is 64 bits. Not executed.

Reduction: compare the byte count in a 64-bit integer before converting to `int`, and reject a layout length that does not fit in a non-negative `int` before slicing.

not added: report-only
