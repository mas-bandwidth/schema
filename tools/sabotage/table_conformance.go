package main

// THE C++ REFERENCE's emitter-side CONFORMANCE negative control (docs/PORTING.md
// I11). The driver-flip control (`conformance-negative-control`) localizes the
// text form but cannot show the harness finding a defect in GENERATED code;
// this one breaks the leg's TEXT READER in the emitter — the storage offset a
// field's read looks up — regenerates through a Go build overlay, and requires
// `cpp / json-read` red while `json-write` and `wire` stay green. The second
// half is the point: a matrix whose every cell went red would say "something
// broke" rather than "the reader broke".
//
// The generated walk's write path spells the same storage line, so the anchor
// is the reader's own expression; the sabotage must not reach the writer, or
// `json-write` would go red beside the reader and the control would localise
// nothing.
//
// RUST (docs/PORTING.md I11) carries no control yet: the leg registers no text
// form and no text conformance surface, so `internal/codegen/rusttable/runtime.go`
// and the `rust / json-read` cell do not exist to break. The I11 rust cell cites
// schema#518, which carries the emitter's text walk; the sabotage lands with it.
func init() {
	sabotages["table-cpp-json-read"] = []edit{{
		old: "uint8_t * storage = (uint8_t *) base + f->offset;",
		new: "uint8_t * storage = (uint8_t *) base + ( f->offset ^ 4 ); /* SABOTAGED: the read takes its neighbour's storage */",
	}}
}
