// `schema pack` and `schema unpack` on the public driver (docs/SPEC-TABLES.md §17).
package compiler

import (
	"github.com/mas-bandwidth/schema/v2/internal/tablepack"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// TableReport is the read report both table forms share (docs/SPEC-TABLES.md §4,
// §16.2), aggregated across a whole tree by a pack (§17.3). Silence — every
// counter zero and Malformed false — means the data matched the schema exactly:
// nothing was skipped, nothing was renamed, nothing was cut down.
type TableReport struct {
	// Unknown counts every name this schema cannot name: a field key, an enum
	// variant, a union arm, an enum-keyed slot.
	Unknown int
	// KindMismatch counts a value present with the wrong JSON type for its
	// declared kind — skipped, never coerced.
	KindMismatch int
	// Clamped counts a value cut down to what the declaration can hold: a
	// number outside its range, a string past its capacity, an array past its
	// bound.
	Clamped int
	// Duplicate counts a key placed more than once; the last occurrence wins.
	Duplicate int
	// Malformed is set when a text or a body was damaged past the point the
	// walk could continue.
	Malformed bool
	// Refused is the VERDICT beside the counters (docs/SPEC-TABLES.md §3): a
	// FORM BYTE this reader does not carry. Nothing was decoded, no counter
	// moved, and no damage is reported — which is why the verdict has to be
	// said, since a clean read prints the same five zeros.
	Refused bool
}

// Silent reports whether nothing at all was counted.
func (r TableReport) Silent() bool {
	return r.Unknown == 0 && r.KindMismatch == 0 && r.Clamped == 0 && r.Duplicate == 0 && !r.Malformed
}

func publicReport(r tabletext.Report) TableReport {
	return TableReport{
		Unknown:      r.Unknown,
		KindMismatch: r.KindMismatch,
		Clamped:      r.Clamped,
		Duplicate:    r.Duplicate,
		Malformed:    r.Malformed,
		Refused:      r.Refused,
	}
}

// Pack assembles ONE instance of the named root table from the directory tree
// under dir and returns the root's WIRE BYTES AND NOTHING ELSE (docs/SPEC-TABLES.md
// §17.2): no magic, no content hash, no protocol id, no length prefix around
// the whole. A caller that wants an envelope writes its own few lines around
// these bytes.
//
// The tree rule is §17.1's and is structural only: a directory named after a
// field holds that field's value; an enum-keyed array takes one
// `<Variant>.json` per slot; a bounded array takes files in name order, or one
// `<field>.json` holding the whole array; a nested table takes either form; a
// plain `<field>.json` at any level is that field's value verbatim; and the
// root may simply be one `<Root>.json`. Each file's content is read by §16's
// walk, so every rule about kinds, presence, clamping and the report is that
// section's.
//
// A tree that does not mirror the table is REPORTED rather than guessed at: the
// error names every refusal at once, so a pack of a hundred files reports once.
// Nothing is written; the bytes are the caller's to place.
//
// The second result names the hidden non-JSON files the walk passed over — the
// one thing a tree walk does not report as a refusal, because a tool that
// refused `.DS_Store` would be a tool nobody could run on a checkout. Surface
// them where a caller can see them.
func (c *Compiler) Pack(u *ir.Unit, root, dir string) ([]byte, []string, TableReport, error) {
	bytes, skipped, report, err := tablepack.Pack(tabletext.NewModel(u), root, dir)
	return bytes, skipped, publicReport(report), err
}

// Unpack is the inverse (docs/SPEC-TABLES.md §17.3): it decodes a root table's wire
// bytes and writes the tree back out through §16's text form, which is the
// tool round trip §1 promises. `unpack` then `pack` is byte-stable — including
// into a directory that already holds a tree, because Unpack PRUNES the
// entries it owns and did not write.
//
// The returned report is the WIRE's (§4): what the bytes carried that this
// schema could not name, could not hold, or could not decode. An error is a
// refusal — a root this engine does not decode, or bytes it cannot walk — and
// nothing is written when one comes back.
func (c *Compiler) Unpack(u *ir.Unit, root string, wire []byte, dir string) (TableReport, error) {
	report, err := tablepack.Unpack(tabletext.NewModel(u), root, wire, dir)
	return publicReport(report), err
}

// ReadReport is the §4 read report of one wire read as one root, and nothing
// else: no text is written and no tree is touched.
//
// It exists because the conformance corpus's evolution cases want the counters
// and only the counters: a report is a fact about the DECODE, so the harness
// asks for the decode rather than for a text it would throw away.
func (c *Compiler) ReadReport(u *ir.Unit, root string, wire []byte) (TableReport, error) {
	report, err := tablepack.ReadReport(tabletext.NewModel(u), root, wire)
	return publicReport(report), err
}

// UnpackOneFile writes the root as ONE `<Root>.json` rather than a tree of
// field files — §17.2's last rule as an output shape, and the same instance
// through the same writer, so it packs to the same bytes the tree does. One
// text of the whole root is what a backend's `ToJson` produces, and comparing
// the two is §17.1's third golden.
func (c *Compiler) UnpackOneFile(u *ir.Unit, root string, wire []byte, dir string) (TableReport, error) {
	report, err := tablepack.UnpackOneFile(tabletext.NewModel(u), root, wire, dir)
	return publicReport(report), err
}
