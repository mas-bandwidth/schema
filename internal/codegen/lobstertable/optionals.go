// Package lobstertable's optionals surface is NOT delivered by this attempt.
//
// schema-lobster-vt-optionals depends on schema-lobster-table-wire-save, and
// the save walker is not on sprint/schema-lobster at e9be0b4bf: the branch has
// runtime/load.lobster and no save runtime at all, so there is nothing for an
// optionals save hook to join, and no load/save/re-load agreement to test.
//
// What the landed load walker already does (runtime/load.lobster), and what
// the Go reference (internal/tablewire, internal/codegen/gotable) still does
// not have a hook for:
//
//   - ?T (docs/SPEC-TABLES.md §2.3) rides through Slot.present, set by
//     body_at and by each collection shape; the descriptor carries
//     FieldDescriptor.present, emitted from lobstertable.fieldDesc. It works
//     on load today.
//   - [E]T (docs/SPEC-TABLES.md §3.2) is KeyedShape, joined by register_shape
//     under the key "keyed". The descriptor does not carry the key enum yet,
//     so a program calls schema_rt.keyed_keys by hand (the load test does).
//     An optionals Part could emit that call from FieldDescriptor.kw /
//     KeyEnumRef through lobstertable.Register without touching the walker.
//   - ?map[K]V and the optional variable closure (docs/SPEC-TABLES.md §2.8)
//     are carried by MapShape and by the node table's pointer walk; both are
//     present values on load today.
//   - `| required` (docs/SPEC-TABLES.md §2.3) is NOT reachable through a
//     shape hook. It is a property of the field's SLOT, not of its shape:
//     the save walker must write a required field even at its default
//     (elision is in the save loop), and the load walker must count
//     MissingRequired and set report.reason to "missing_required: <field>"
//     when the body ends without it (guarded fields on an untaken branch are
//     not demanded). Neither decision is made in a Shape.method.
//
// PATHS-PROPOSED, each a one-line hook change in a file this card does not
// edit (the walkers belong to table-wire-load and table-wire-save):
//
//   - runtime/load.lobster, in body_at's field loop, after a shape returns:
//     if f.required and not sl.present-style "named" then report.missing_required++,
//     report.reason = "missing_required: " + f.name. The loop already knows
//     whether the body NAMED the field, which is exactly the fact required
//     needs; present is not it (a required field is not an optional).
//   - runtime/save.lobster, where save_is_default chooses elision:
//     skip the default-elision branch when d.required, so a required field
//     always rides (off-branch required fields are not reached by this loop
//     at all, so they are already not demanded).
//   - descriptor: a keyvs member on TableDescriptor/FieldDescriptor, or the
//     optionals Part emitting schema_rt.keyed_keys, so [E]T needs no manual
//     registration.
//
// With those hooks a card can add runtime/optionals.lobster plus a
// TestLobsterOptionals that generates the manifest units using ?T, [E]T and
// ?map[K]V (tabledemo Keyed, tblg1 Guarded, the map units), plus a required
// probe, and holds load, save and re-load to the Go engine.
package lobstertable
