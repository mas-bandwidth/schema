package javatable

import (
	"strings"

	"github.com/mas-bandwidth/schema/v2/ir"
)

// emitLoadPayloadCanonical emits one message field's payload read, then the
// one-encoding refusal the field owes (docs/SPEC-TABLES.md §3.3 (a); docs/rulings.json
// reads-refuse-non-canonical): a field stored at the value its writer elides is
// not the one encoding. The test is the writer's own ride test (emitSaveField,
// emitMessageRides) read back on the loaded value, kind by kind: a scalar, float
// (by its bits), enum, bool, string or byte buffer at its default, a union at
// None, a counted array with no element, a fixed array whose every element is at
// its default (a fixed array of tables always rides), a nested table whose body
// is only its terminator, and a keyed array with no slot or a slot at its default
// (emitLoadPayload). A value that is the default only because it names an id
// this reader cannot (an enum variant or a union arm from a newer writer) counts
// unknown and is lawful. An optional rides on its presence, so it owes no test.
// Every lawful writer's default for a field this reader names is the reader's
// own (the lineage check, docs/VERSIONING.md), so the test does not depend on
// the writer's build version.
func (g *messageGen) emitLoadPayloadCanonical(f *ir.Field, owner, entryVar string, indent int) {
	ind := strings.Repeat(" ", indent)
	const ok = "!r.report.refused && !r.report.damaged"
	refuse := func(test string) {
		g.pf("%sif (%s && %s) { r.report.refuse(\"non_canonical\"); return false; }\n", ind, ok, test)
	}
	switch {
	case f.Type.Optional || (f.Array == ir.ArrayFixed && isStructType(f.Type) && f.KeyEnum == ""):
		g.emitLoadPayload(f, owner, entryVar, indent)
	case f.KeyEnum != "":
		g.canonicalField = true
		g.emitLoadPayload(f, owner, entryVar, indent)
	case isStructType(f.Type) && f.Array == ir.ArrayNone:
		g.pf("%slong canonAt = r.at;\n", ind)
		g.emitLoadPayload(f, owner, entryVar, indent)
		refuse("r.at - canonAt == (long) r.vocab.refBits")
	default:
		g.pf("%sint canonUnk = r.report.unknown;\n", ind)
		g.emitLoadPayload(f, owner, entryVar, indent)
		rides := g.emitMessageRides(f, owner, indent)
		refuse("r.report.unknown == canonUnk && !(" + rides + ")")
	}
}

// messageSlotPre is the statement a keyed slot's one-encoding test reads its
// start from: the bit offset for a table slot, the unknown count otherwise.
func (g *messageGen) messageSlotPre(f *ir.Field) string {
	if isStructType(f.Type) {
		return "long slotAt = r.at;"
	}
	return "int slotUnk = r.report.unknown;"
}

// messageSlotAtDefault is the test for a keyed slot the writer would not have
// written (emitKeyedCount): a table slot whose body is only its terminator, or
// a value slot at its default.
func (g *messageGen) messageSlotAtDefault(f *ir.Field, elem string) string {
	const ok = "!r.report.refused && !r.report.damaged"
	if isStructType(f.Type) {
		return ok + " && r.at - slotAt == (long) r.vocab.refBits"
	}
	return ok + " && r.report.unknown == slotUnk && !(" + g.scalarOrUnionRidesExpr(f, elem) + ")"
}
