# Optionals

A Python optional field `?T` is `None` when absent and the value when present
(docs/SPEC-TABLES.md §2.3). On the variable form an absent optional writes no
field and a present one rides the wrapped kind's framing, its default
included; the save's numbering walk descends an optional's edges only when it
is present, so an absent optional owns no node. On the fixed form a present
optional is its presence byte, 1, then the wrapped value; an absent one is a
zero presence byte and zero bytes. The kind is one runtime module,
`_schema/kind_optional.py`; the fixed form's optional shape lives in
`_schema/fixedform.py` beside the other fixed shapes.

An optional array, `?[N]T` or `?[..N]T`, is the plain array framing behind
presence: a present list rides at its live count, zero included, or whole when
fixed, so present-and-empty and absent stay two values. Presence is never
derived from the count. A foreign element kind refuses the whole read as
`kind_mismatch` (the strict read), and a count past the bound as
`count_over_bound`.

## The rows

`TestPythonVtOptionalRows` (`internal/codegen/pythontable/vtoptional_rows_test.go`)
holds `port/python/vt/optional-fields` and `port/python/vt/optional-arrays`
over every conformance-manifest case the Lua leg names for them, under both
pinned interpreters, to the one read: an instance loads and saves back byte for
byte with its measure its length, a wire-refused one refuses, a report row's
verdict is reports.txt's and the Go engine's, and a report wire that reads
saves back as the engine's canonical write of it. A refusal leaves no value and
leaves the caller's buffer and `LoadReport` untouched; a read leaves the buffer
untouched. The two Go-engine constructs the Lua leg names, `?Inner` over a
variable closure and `?map[string(8)]int32` present and empty, ride the same
way.

The form-1 report wires at fixed roots (P1 and P3 `Chain`, the message corpus's
`ToolMessage`) refuse at the declared root as `previous_form`
(docs/SPEC-TABLES.md §3.4). The optional rule rides form 1, so each such row is
held a second time with its root variable, as the widening rows are: P1's
by-value `link` and P3's `?Link` read each other's wires and write the same
bytes, a pointer against an optional is `kind_mismatch` both ways, and the
message corpus's `trace` refuses a foreign element kind and a count past its
bound.

## Negative controls

`TestPythonVtOptionalRowsNegativeControl` plants each fault in a copy of the
emitted package and requires its named cases red:

| control | planted in | red |
|---|---|---|
| variable-present-elided | `kind_optional.py` write | `optional_present_edge`, both constructs, `p1_as_p3` and `marks_unknown_variant` on the variable root |
| variable-present-read-absent | `kind_optional.py` read | `optional_present_edge`, both constructs, `p1_as_p3` on the variable root |
| fixed-presence-dropped | `fixedform.py` encode | `chain_optional`, `chain_optional_empty`, `message_trace` |
| fixed-absent-read-present | `fixedform.py` decode | `message_save`, `message_empty` |
| variable-array-count-unchecked | `kind_optional.py` array read | `trace_count_past_bound` on the variable root |
| variable-array-kind-unchecked | `kind_optional.py` array read | `trace_elem_kind` on the variable root |
| absent-starts-present | the emitted class's optional default | `optional_absent_no_edge`, `p1_empty_as_p3` on the variable root |

The declared-root `previous_form` verdicts and the pointer-against-optional
`kind_mismatch` verdicts are decided by the shared table frame (`load.py`'s
kind check, the fixed form's form gate), whose own controls hold them; here
they are held by verdict.

## Speed

The rows card changed no runtime code: every manifest case read and wrote as
the engine does at the base, so there is no hot path of this card's to measure.
