// cook.lobster — the cooked form's header check (docs/SPEC-TABLES.md §7,
// §7.1; docs/PORT-LOBSTER.md, THE CARD MAP). A cook is a header and then the
// region `Lock` would pack, written verbatim (internal/tablecook/cook.go,
// ReadHeader): Open is O(1) in the file's size and checks identity only.
//
// THE MAGIC IS READ BYTEWISE FIRST, because it settles the byte order every
// other header word is written in: this build's constant either is what a
// little-endian store produces or its byte reversal, which names the other
// order, or it is not a cook at all. The data offset is DERIVED from the
// header's alignment, never stored.
// SPDX-License-Identifier: NONE — emitted output, yours under terms of your choice.

namespace schema_rt

// cook_magic is "SCHMCOOK" read as ASCII in the low-to-high order a
// little-endian store produces (internal/tablecook.Magic).
let cook_magic = 0x4B4F4F434D484353
// cook_magic_swapped is the same word in the other order: what a big-endian
// cook's first eight bytes read as on a little-endian reader.
let cook_magic_swapped = 0x5343484D434F4F4B

let cook_header_bytes = 64
let cook_byte_order_little = 1
let cook_byte_order_big = 2

// cook_bswap64 reverses a word's bytes, the read a big-endian cook needs on
// every header word (/docs/PORT-LOBSTER.md, THE NUMBER MODEL: no unsigned
// shift, so the mask follows each shift).
def cook_bswap64(x:int) -> int:
    let b0 = x & 0xff
    let b1 = (x >> 8) & 0xff
    let b2 = (x >> 16) & 0xff
    let b3 = (x >> 24) & 0xff
    let b4 = (x >> 32) & 0xff
    let b5 = (x >> 40) & 0xff
    let b6 = (x >> 48) & 0xff
    let b7 = (x >> 56) & 0xff
    return (b0 << 56) | (b1 << 48) | (b2 << 40) | (b3 << 32) | (b4 << 24) | (b5 << 16) | (b6 << 8) | b7

def cook_word(data:string, at:int, big:bool) -> int:
    let v = read_uint64_le(data, at)
    if big:
        return cook_bswap64(v)
    return v

// cook_align_up rounds v up to a multiple of a (a is a power of two).
def cook_align_up(v:int, a:int) -> int:
    return (v + a - 1) & ~(a - 1)

// cook_open is `Open`'s check for one table (docs/SPEC-TABLES.md §7, §7.1;
// internal/tablecook.ReadHeader): the magic, the order it establishes, the
// build version, every reserved word zero, the alignment, and both part
// lengths against the size the caller passed — a truncated file refuses.
// root_align is the root's own alignof (§7.1 requires the cook's alignment to
// be a multiple of it); root_size is the root record's size, which the data
// part must hold.
def cook_open(data:string, build:int, root_align:int, root_size:int) -> bool:
    let bytes = length(data)
    if bytes < cook_header_bytes:
        return false
    let magic = read_uint64_le(data, 0)
    var big = false
    if magic == cook_magic:
        big = false
    elif magic == cook_magic_swapped:
        big = true
    else:
        return false
    let order = cook_word(data, 16, big)
    if order != (if big: cook_byte_order_big else: cook_byte_order_little):
        return false
    if cook_word(data, 8, big) != build:
        return false
    if cook_word(data, 48, big) != 0 or cook_word(data, 56, big) != 0:
        return false
    let alignment = cook_word(data, 40, big)
    if alignment < 8 or alignment > 64 or (alignment & (alignment - 1)) != 0:
        return false
    if root_align > 0 and alignment % root_align != 0:
        return false
    let data_offset = cook_align_up(cook_header_bytes, alignment)
    let data_length = cook_word(data, 24, big)
    let attrib_length = cook_word(data, 32, big)
    if data_offset > bytes:
        return false
    if data_length < 0 or data_length > bytes - data_offset:
        return false
    if attrib_length != bytes - data_offset - data_length:
        return false
    if data_length < root_size:
        return false
    return true
