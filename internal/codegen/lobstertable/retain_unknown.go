package lobstertable

// RETAIN-UNKNOWN (docs/SPEC-TABLES.md §6.6; docs/PORT-LOBSTER.md, THE LOAD)
// stops HOLD in this checkout, and this file is the PATHS-PROPOSED report.
//
// The card says this group's runtime joins "the per-kind hook
// table-wire-load and table-wire-save fixed". Those two walkers carry no such
// hook on sprint/schema-lobster at 833dd6acf, and the hook is outside this
// card's PATHS, so neither runtime/retain_unknown.lobster nor
// TestLobsterRetainUnknown can be written against it.
//
// THE LOAD. runtime/load.lobster's `body_at` (the definition at "body_at
// decodes one table body", §4) is the one place an unknown field id reaches
// the wire: `slot_by_id` misses, the branch counts `rep.unknown++` and calls
// `skip(r, kind)`. The Shape registry (§THE LOAD, "The dispatch table") is
// consulted only after `slot_by_id` succeeds, so a shape cannot see an id no
// descriptor names. `body_at` is also what every nested table, union arm, map
// entry and keyed slot calls, so a retaining walk cannot be layered on top of
// `load`: it is the walk.
//
// THE SAVE. runtime/schema_save.lobster's `encode_body_fields` walks the
// declared `rec.fields` only, and `encode_body` appends the terminator;
// `encode_field` has no branch an id the reader cannot name reaches. The save
// never consults the descriptor registry for a Shape, so the load's
// `register_shape` hook has no save-side twin either.
//
// WHAT IS NOT DONE, BY NAME. No runtime/retain_unknown.lobster and no
// TestLobsterRetainUnknown: a runtime that registers a hook the emitted
// runtime does not call is dead code, and a test that cannot call
// LoadRetain/SaveRetain would be red in the tree. The retain rows of
// testdata/conformance/tables/MANIFEST.txt (tblrt1/tblrt2/tblrt3, §6.6) are
// not run.
//
// PATHS-PROPOSED: the dependency change this card needs. Both edits are two
// one-line calls plus the registration they name, and both registries live in
// the file that defines the types their signatures use, so
// retain_unknown.lobster (which imports .load and .schema_save) registers
// them from package-level `let`s and no import cycle is introduced.
//
// --- a/internal/codegen/lobstertable/runtime/load.lobster
// +++ b/internal/codegen/lobstertable/runtime/load.lobster
// @@ class shape_registry ... after `let shape_registry:[Shape] = []`
// +// RETAIN-UNKNOWN (docs/SPEC-TABLES.md §6.6): the per-body hook a retaining
// +// walk joins. `begin` runs on every body occurrence, so retained records
// +// die with the occurrence that carried them; `capture` sees an id no
// +// descriptor names with the reader at the payload, and returns true when
// +// it consumed it (false leaves the plain `skip` to do so).
// +class RetainCapture:
// +    def begin(rec:Record) -> void: return
// +    def capture(r:Reader, rec:Record, id:int, kind:int) -> bool: return false
// +let retain_capture:RetainCapture? = nil
// +def register_retain_capture(h:RetainCapture):
// +    retain_capture = h
//
// @@ def body_at(r:Reader, rec:Record, nested:bool, entry:bool) -> bool:
//      let rep = r.st.report
// +    if retain_capture:
// +        retain_capture.begin(rec)
//      while true:
//          ...
//          let found = slot_by_id(rec, id)
//          if not found:
// +            if retain_capture and retain_capture.capture(r, rec, id, kind):
// +                continue
//              rep.unknown++
//              if not skip(r, kind):
//
// --- a/internal/codegen/lobstertable/runtime/schema_save.lobster
// +++ b/internal/codegen/lobstertable/runtime/schema_save.lobster
// @@ class SaveEncoder ... after the class
// +// RETAIN-UNKNOWN (docs/SPEC-TABLES.md §6.6): the tail a retaining save
// +// writes at the END of a body, after every declared field and before the
// +// terminator, and (in the root) before the node-table field.
// +class RetainTail:
// +    def tail(e:SaveEncoder, w:SaveOut, rec:Record) -> void: return
// +let retain_tail:RetainTail? = nil
// +def register_retain_tail(h:RetainTail):
// +    retain_tail = h
//
// @@ def encode_body_fields(e:SaveEncoder, w:SaveOut, rec:Record) -> void:
//      for(rec.fields) sl:
//          encode_field(e, w, sl)
// +    if retain_tail:
// +        retain_tail.tail(e, w, rec)
//
// THE FOLLOW-UP SHAPE once those land (this is what the next attempt writes,
// in this card's three files only). retain_unknown.lobster defines
// `RetainCapture : schema_rt.RetainCapture` and `RetainTail :
// schema_rt.RetainTail` and registers one of each; capture parses the field's
// payload with §3's skip rules, replaces every id reference by its 64-bit id
// (§6.6, RETENTION FORM), applies the six excluded classes and the caller's
// capacity, and keeps the record on the `Record` it was called for; tail
// interns the retained ids into the one save id table in first-use order and
// emits each payload at the end of its body (§6.6, WHERE THEY GO BACK). The
// test drives the manifest's `retain` rows (tblrt1 reading tblrt2/tblrt3)
// against internal/tablewire's DecodeRetain/EncodeRetain and the pinned saves.
