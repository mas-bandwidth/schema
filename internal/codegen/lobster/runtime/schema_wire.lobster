// schema_wire.lobster — the Lobster type wire's helpers beside the vendored
// serialize.lobster port (docs/PORT-LOBSTER.md, THE RUNTIME LAYOUT). A module
// with a packet wire imports it with `import .schema_wire`; it adds no bit
// primitive of its own, only the schema rules the port does not hold for a
// caller-owned buffer.
// SPDX-License-Identifier: NONE — emitted output, yours under terms of your choice.

import .serialize

namespace schema_rt

// text_refusal is the refusal a string(N) payload earns, s[0:n], none for real
// text (SPEC §4.7; serialize STANDARD.md, string): an interior NUL first,
// over the whole payload, then UTF-8 well-formedness, so a payload that breaks
// both is embedded_nul. A read latches it; a write asserts none.
def text_refusal(s:string, n:int) -> serialize_refusal:
    for(n) i:
        if read_uint8_le(s, i) == 0:
            return serialize_refusal_embedded_nul
    if not string_utf8_valid(s, n):
        return serialize_refusal_invalid_utf8
    return serialize_refusal_none

// units_nonzero is "no zero code unit among s's first n UTF-16LE units", the
// wstring(N) writer's contract (SPEC §4.12).
def units_nonzero(s:string, n:int) -> bool:
    for(n) i:
        if read_uint16_le(s, i * 2) == 0:
            return false
    return true
