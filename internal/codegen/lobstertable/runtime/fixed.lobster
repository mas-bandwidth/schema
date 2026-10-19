// fixed.lobster — the fixed form's file framing (docs/SPEC-TABLES.md §3.4,
// docs/FIXED-FORM-ALGORITHM.md §2). The form byte and the layout appear once
// per carrier; a record is its eight-byte layout hash and then its body.
// The record bodies themselves are emitted per table by fixedform.go.
// SPDX-License-Identifier: NONE — emitted output, yours under terms of your choice.

import .schema_runtime

namespace schema_rt

// fixed_record is one record: the writer's layout hash, then the body
// (docs/FIXED-FORM-ALGORITHM.md §2, "a record: u64 hash, the root's body").
def fixed_record(hash:int, body:string) -> string:
    var out = zeros(8 + length(body))
    out = write_int64_le(out, 0, hash)
    out = write_substring(out, 8, body, false)
    return out

// fixed_file is a form-3 carrier: form byte 3, seven zero reserved bytes, the
// layout hash, the layout's length in bytes, the layout, then the records
// (docs/FIXED-FORM-ALGORITHM.md §2).
def fixed_file(layout:string, hash:int, body:string, count:int) -> string:
    var out = zeros(20 + length(layout) + length(body))
    out = write_int8_le(out, 0, 3)
    out = write_int64_le(out, 8, hash)
    out = write_int32_le(out, 16, length(layout))
    out = write_substring(out, 20, layout, false)
    out = write_substring(out, 20 + length(layout), body, false)
    return out

// fixed_f32bits is the 32-bit storage image of a float32 value without a
// hardware conversion: the low 29 mantissa bits survive, so a signalling NaN
// keeps its payload (docs/FIXED-FORM-ALGORITHM.md §3, PORT-LOBSTER FLOAT32).
def fixed_f32bits(x:float) -> int:
    let b = schema_rt.f64bits(x)
    let sign = (b >> 63) & 1
    let exp = (b >> 52) & 0x7ff
    let frac = b & 0xfffffffffffff
    if exp == 0x7ff:
        if frac == 0:
            return (sign << 31) | 0x7f800000
        return (sign << 31) | 0x7f800000 | ((frac >> 29) & 0x7fffff)
    return (sign << 31) | ((exp - 1023 + 127) << 23) | ((frac >> 29) & 0x7fffff)

// fixed_f32frombits is [fixed_f32bits]'s inverse, payload and all.
def fixed_f32frombits(b:int) -> float:
    let sign = (b >> 31) & 1
    let exp = (b >> 23) & 0xff
    let frac = b & 0x7fffff
    if exp == 0xff:
        if frac == 0:
            return schema_rt.f64frombits((sign << 63) | 0x7ff0000000000000)
        return schema_rt.f64frombits((sign << 63) | 0x7ff0000000000000 | (frac << 29))
    return schema_rt.f64frombits((sign << 63) | ((exp - 127 + 1023) << 52) | (frac << 29))
