# Fixed Tables feature survey

Implementation snapshot: `8ea5ed8e4656875088250f564e88a965a7135e7c`.

The first roadmap grouped outstanding audit obligations. It did not show the ordinary capabilities already built. Scope revision 2 appends the following capabilities without removing the original audit traceability. Evidence reconciliation is still in progress; this inventory is not a claim that every language implements every row.

Valid-data support and hostile-input certification are distinct required work. A passing generation test proves the emitted surface, not the runtime behavior. Named unsupported surfaces remain visible. The table is generated from `roadmap.sexp`; this page records why its scope changed.

## Ordinary capabilities added

| Feature | Contract |
|---|---|
| Boolean values | §3.4 record, bool |
| Signed integers: 8, 16, 32 and 64 bits | §3.4 record, int8..int64 |
| Unsigned integers: 8, 16, 32 and 64 bits | §3.4 record, uint8..uint64 |
| Signed 128-bit integers | §3.4 record, int128 |
| Unsigned 128-bit integers | §3.4 record, uint128 |
| Ranged integer fields | §3.4 record, ranged integer |
| bits(N) fields | §3.4 record, bits(N) |
| 32-bit floating-point fields | §3.4 record, float32 |
| 64-bit floating-point fields | §3.4 record, float64 |
| Compressed-float declarations stored as float32 | §3.4 record; ALGORITHM §5.9 compressed-float rulings |
| Signed fixed-point fields | §3.4 record, fixed(I,F) |
| Unsigned fixed-point fields | §3.4 record, ufixed(I,F) |
| Flags masks | §3.4 record, flags |
| Enums and None | §3.4 record, enum |
| Bounded UTF-8 string fields | §3.4 record, string(N) |
| Bounded UTF-16 string fields | §3.4 record, wstring(N) |
| Bounded byte buffers | §3.4 record, bytes(N) |
| Nested types by value | §2.2; §3.4 record, nested type |
| Nested fixed tables by value | §2.2; §3.4 record, nested fixed table |
| Fixed-length arrays | §3.4 record, [N]T |
| Bounded arrays with a live count | §3.4 record, [Min..Max]T |
| Enum-keyed arrays | §2.4; §3.4 record, [Enum]T |
| Nested enum-keyed arrays | ALGORITHM §7 keyed corpus |
| Tagged unions with type or fixed-table payloads | §2.6; §3.4 record, union |
| Union arms holding scalar, text or array fields | §2.6; §3.4 constant arm storage |
| Payload-free union arms | §2.6; ALGORITHM §1 kind32 |
| Arrays of unions | §2.6; §3.4 [N]T / [Min..Max]T |
| Optional scalar and enum fields | §2.3; §3.4 record, ?T |
| Optional nested values | §2.3; ALGORITHM §7 P1/P3 |
| Optional arrays | §2.3; §3.4 optional wrapper |
| Scalar and enum defaults | §3.4 template; ALGORITHM §3 |
| String, byte-buffer and flags defaults | ALGORITHM §3 prefill; landed PR847 |
| Save and load fixed-form files | §3.4 framing; ALGORITHM §2 / §7 |
| Constant body size and file-size measurement | §3.4 C(f), C(T), MeasureBody |

## Versioning coverage retained

These definition-change rows are retained for decomposition beneath the evolution cells. Compiler-only refusals belong to shared work rather than nine copies. Each applicable read row requires exact values/counters and the older-reader refusal, with declared exceptions recorded.

- `field_append`
- `field_deprecate`
- `field_undeprecate`
- `field_modify`
- `enum_append`
- `enum_width`
- `union_append`
- `union_arm_payload_widen`
- `flags_append`
- `array_bounded_grow`
- `array_fixed_grow`
- `array_shape`
- `array_elem_widen`
- `keyed_array_enum_append`
- `constant_grow`
- `string_grow`
- `wstring_grow`
- `bytes_grow`
- `text_kind`
- `int_widen`
- `uint_widen`
- `float_widen`
- `range_widen`
- `range_added`
- `cfloat_res_refine`
- `cfloat_res_coarsen`
- `cfloat_range_widen`
- `bits_grow`
- `fixed_I_grow`
- `fixed_I_grow_element`
- `optional_add`
- `nested_append`
- `default_change`
- `keyword_change`
- `closure_plain_table`
- `closure_variable_kind`
- `closure_self`
- `rename_without_was`

Source: [versioning contract](https://github.com/mas-bandwidth/schema/blob/8ea5ed8e4656875088250f564e88a965a7135e7c/docs/FIXED-FORM-VERSIONING-TESTS.md).

## Boundaries

Unbounded arrays, maps, pointers and used-size pointer blobs are variable-table capabilities. They are not green fixed-table features. Bitpacked message form, variable tables, cooking and save games remain Future.

`wstring(N)` in fixed tables is carried by C++, C, C#, Go and Dart at this snapshot. Rust, Java, JavaScript and Elixir have a named compiler refusal. The Dart fixed-form exception must not be confused with its accelerator support.

Fixed-form string, bytes and flags defaults do not take the form-1 default refusal. `TestFixedTableValueDefaultsEveryLeg` and `TestFX1GeneratesOnEveryLeg` passed locally at this snapshot. Runtime evidence is recorded separately.

Future taxonomy and indexed repo/category queries are designed in [nova-tools #177](https://github.com/mas-bandwidth/nova-tools/issues/177), not implemented by this bounded renderer.
