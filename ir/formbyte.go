// THE FORM BYTE, and the registry that is the ONLY place a value is assigned
// (docs/SPEC-TABLES.md §3, "THE FIRST BYTE").
//
// Every file of the three live forms begins with it; the cook and the block
// begin with a magic of their own until the day they carry the byte reserved
// for them (fileformat.go). A schema file is a UNION OF FIVE ARMS and
// the form byte is its TAG — *"so in effect, form byte is like a union id"* —
// under the same append-only rule the baseline lock enforces for a union in a
// fixed table: numbered forever, appended at the end, never reinserted and
// never renamed. Byte `0` and any value this table does not assign are REFUSED
// BY NAME; a form that has shipped keeps its number forever.
//
// It lives in the IR, beside the kind vocabulary, for the reason the kinds do:
// which byte means which form is WIRE LAW and not a tool's opinion, so the
// compiler, the CLI and every backend read the one table rather than three
// copies of it.
package ir

import "slices"

// WireForm is one row of the registry: the byte, the form's name, whether the
// BYTE is planned rather than live, and the section that defines the form.
type WireForm struct {
	Byte uint8
	Name string // the name §3 gives the form, as a reader prints it
	// Planned says the BYTE is reserved and no build writes or reads it yet.
	// It says nothing about the format the byte is reserved for: the cook and
	// the block are implemented, and a file of either begins with its magic
	// (fileformat.go), not with this byte.
	Planned bool
	Where   string // the section of docs/SPEC-TABLES.md that defines it
}

// wireForms IS THE REGISTRY (docs/SPEC-TABLES.md §3, §3.4). Nothing else in
// this tree assigns a form byte a meaning, and a value absent from this table
// is a value no form defines — which is what the unknown-form negative control
// plants.
//
// Byte `0` is never assigned, so that a zero-filled buffer handed to a reader
// is a refusal by name and never a form.
//
// It is unexported so that no importer can change what [WireFormOf] and
// [FileLine] answer; [WireForms] is the copy a client enumerates.
var wireForms = []WireForm{
	{Byte: 0, Name: "never assigned", Where: "§3"},
	{Byte: 1, Name: "the variable form", Where: "§3"},
	{Byte: 2, Name: "the message form", Where: "§3.3"},
	{Byte: 3, Name: "the fixed form", Where: "§3.4"},
	{Byte: 4, Name: "the cook", Planned: true, Where: "§7"},
	{Byte: 5, Name: "the block form", Planned: true, Where: "§19"},
}

// WireForms enumerates the registry's rows in byte order (docs/SPEC-TABLES.md
// §3). It is a COPY of the registry: editing it changes nothing [WireFormOf],
// [FileLine] or any reader answers.
var WireForms = slices.Clone(wireForms)

// WireFormOf answers the registry row for a byte, and whether the registry
// assigns it at all. Byte `0` HAS a row and is still not a form: its row says
// so, and [FileLine] refuses it by name like any unassigned value.
func WireFormOf(b uint8) (WireForm, bool) {
	for _, f := range wireForms {
		if f.Byte == b {
			return f, true
		}
	}
	return WireForm{Byte: b}, false
}

// WireFormNone is the FIRST BYTE NO FORM DEFINES OR RESERVES, and it is what
// §4.2's UNKNOWN-FORM NEGATIVE CONTROL plants (docs/SPEC-TABLES.md §3, §4.2).
//
// It is DERIVED from the registry rather than written down, because the whole
// premise of that control is a byte no form defines: the day a form is added,
// the byte the control plants has to move, and a constant somebody has to
// remember to move is a control that quietly stops testing what it says.
// Today it is `6`.
var WireFormNone = firstUnassignedForm()

func firstUnassignedForm() uint8 {
	for b := range 256 {
		if _, ok := WireFormOf(uint8(b)); !ok {
			return uint8(b)
		}
	}
	panic("the registry cannot assign every byte")
}
