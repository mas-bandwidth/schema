# Luau reader audit

Report only. The serialize runtime under `internal/codegen/luau/runtime/serialize*.luau` was not audited. Findings are about the emitters in `internal/codegen/luau/` and `internal/codegen/luautable/`, the readers those emitters copy into `generated/` and the goldens, and the bounds those readers rely on when they call that runtime. No input was added.

Schema names are ASCII identifiers. The checker refuses a Luau reserved word. Descriptor strings are Go-quoted, and the escapes that quoting emits are escapes the Luau reader accepts. No comment or string injection from a schema name was found.

## Entry points

| generated function | input | bound relied on | verdict |
| --- | --- | --- | --- |
| `Read<Name>` / `Read<Union>` (packet emitter, `internal/codegen/luau/read.go`; committed shape in `generated/bench/luau/` and `testdata/golden/luau/`) | caller `ReadStream` | `serializeInt` / `serializeBits` / `serializeBits64` / `serializeFixed64` / `serializeFixed128` / `serializeInt64` / `serializeInt128` with the declared min and max; `serializeBytes` into a buffer whose length is that count; `string(N)` and `wstring(N)` checked in the emitter after the copy (UTF-8, interior zero, code unit, surrogate); bare `serializeFloat` / `serializeDouble` finite refusal is the runtime contract, not a second check here | bounded, except a refusal leaves the caller value partly written (finding) |
| `load` / `loadTolerant` / `ReadStrict` (`schema_load.luau`) | form-1 buffer | id-table count refused when negative or past the buffer or past 4096; LEB end restored on failure; length checked against the parent end before `buffer.create`; collection count past the int32 storage cap refused by name; odd wide length, ill-formed UTF-8, and ill-formed UTF-16 mark the load malformed and return no value; non-finite floats refused by name | open: `bits(N)` mask (finding) |
| `AnnounceRead` (`schema_message.luau`) | form-1 announcement | remaining-length and 4096-entry caps; vocabulary byte and entry caps before the vocabulary is installed | open: negative id-table count (finding); repeated id (finding) |
| `LoadMessages` / `LoadMessagesTolerant` | form-2 batch | count is one byte plus one, then the caller capacity and the batch maximum; strict read drops a clamped or kind-mismatched batch | bounded |
| `load` / `read` / `loadTolerant` (`schema_fixed.luau`) | form-3 file | layout hash must match a known lineage entry and the layout bytes before any record; text and byte lengths refused past the plan cap before a copy; finite float test before the value is built; strict read drops a clamped record | open: `bits(N)` for N at least 53 (finding); 128-bit declared range (finding) |
| `FromJson` / `FromJsonTolerant` (`schema_json.luau`) | JSON text | depth 128, key length 255, number token length 511; string and wide-string caps; non-finite floats are a kind mismatch; integer `bits(N)` mask is an integer shift, not a double | open: node-label width (finding) |
| `LoadTextForm` (`schema_textform.luau`) | text-form document | default caps on bytes, tokens, nodes, allocation, and depth; depth cannot be raised past 128 | bounded |
| `LoadText` (`schema_text.luau`) | base64 text | canonical alphabet, then the wire reader above | bounded |
| `BlockOpen` (`schema_blockread.luau`) | block extent | negative offset refused; count held to the declared bound; row bytes held to the extent. Cooked and block input is the trusted class | bounded |
| `Check` / `CheckWhole` (`schema_cookread.luau`) | cook extent | integer compare before a number conversion; negative array count refused; references are not followed. Trusted class | bounded |
| `LoadRetain` (`schema_retain.luau`) | form-1 buffer after load | negative id-table count, or an end before the body, captures nothing | bounded |

## Findings

### `internal/codegen/luautable/runtime/schema_message.luau:324`

high. An announcement id-table count is read as a signed integer and refused only when it is greater than the number of slots the buffer can hold. A count whose high bit is set is negative, so that test does not fire, the computed body end lies past the buffer, and the next byte read faults instead of returning a refusal. The variable-form open refuses this class. Reduction: refuse a negative count before the end is computed, the same predicate the variable-form open uses. not added: report-only.

### `internal/codegen/luautable/runtime/schema_message.luau:331`

low. The announcement id table is not checked for a repeated id. The variable-form open treats a repeated id as malformed. An announcement that repeats an id and is otherwise well formed is installed. Reduction: refuse a repeated id before the vocabulary is installed. not added: report-only.

### `internal/codegen/luautable/runtime/schema_load.luau:501`

high. A `bits(N)` value on the variable form is masked with `2^N - 1` computed in a double, then converted to an integer when the field is 64-bit. For N from 54 through 62 that mask rounds to the next power of two, so the single magnitude equal to that power is kept and not counted clamped; the strict read therefore returns it. For N = 63 the rounded mask is not a signed 64-bit integer, the conversion is nil, and the compare faults on every value of that field, including one inside the declared width. N = 64 is not masked, which matches a full 64-bit slot. Reduction: build the mask with an integer shift and subtract, as the JSON integer path does, and do not compare against nil. not added: report-only.

PATHS-PROPOSED: `internal/codegen/luautable/runtime/schema_load.luau`, `internal/codegen/luautable/runtime/schema_fixed.luau`.

### `internal/codegen/luautable/runtime/schema_fixed.luau:429`

high. The fixed-form bounds pass masks `bits(N)` only when N is below 53. A `bits(N)` wider than 32 is stored in an 8-byte slot, so N from 53 through 63 can carry a magnitude above `2^N - 1`. Those magnitudes are returned and not counted clamped, so the strict read accepts them. N = 64 needs no mask. The same double mask is the variable-form finding above; this one skips the check entirely. Reduction: mask N from 1 through 63 with an integer shift. not added: report-only.

PATHS-PROPOSED: `internal/codegen/luautable/runtime/schema_load.luau`, `internal/codegen/luautable/runtime/schema_fixed.luau`.

### `internal/codegen/luautable/runtime/schema_fixed.luau:404`

high. A declared numeric range is applied only when the low bound is not a table. A 128-bit bound is the two-half value the descriptor emits, which is a table, so a ranged `int128`, `uint128`, or 128-bit fixed field is not clamped. The strict read counts nothing and returns the raw pattern. The variable-form walk does compare 128-bit bounds. Reduction: shift and compare 128-bit bounds in this pass, signed or unsigned as the field declares, and count clamped. not added: report-only.

### `internal/codegen/luau/read.go:357`

low. A packet `string(N)` or `bytes(N)` copies into the caller value and stores the length before the UTF-8 and interior-zero checks return false. The same early return, at the stream call, leaves every field already written in place. A caller that inspects the value after false observes a partial decode, including a length that names bytes the check then refused. Reduction: commit a field only after its checks pass, and leave the caller value unchanged when the read returns false. not added: report-only.

### `internal/codegen/luautable/runtime/schema_json.luau:336`

low. A JSON node label is accumulated in a double. Labels above the exact integer range of a double collapse into one rounding bin, so two labels can share one graph slot and a reference can bind a different node than its digits name. Labels inside that range are exact. Reduction: accumulate the label as an integer, or refuse a label outside the exact range. not added: report-only.

## Gate

`go test -count=1 -timeout 600s ./internal/codegen/luau/...` passes on the base `a007c180ba4b09c7d9d5404de6e072043e034777` and on this head.

`go test -count=1 -timeout 600s ./internal/codegen/luautable/...` is red on that base and on this head. The failures are agreement with the engine: an integrity trailer four bytes short, and load, save, JSON, and cook bytes that do not match. This card does not change those readers.

`go test -count=1 -timeout 600s ./internal/ci/ -run 'HostileCorpus|HostileRefusal'` passed on this head.
