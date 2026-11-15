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
//
// THE AT-DEFAULT TEST IS GATED ON THE SENDER'S BUILD VERSION, as §3.3 (a) states
// it: a reader knows the default a writer elides exactly where it names the
// field under the same build version (every specified default is digested into
// it, §20.1), and a field it meets under a different build version is read as
// today. That is also what keeps a lawful newer writer readable: a field the
// newer build declares `?T` is written whenever present, at its default too
// (docs/VERSIONING.md, T to ?T), and its build version is not this reader's.
// A union written with the None reference is §3.3 (c), which names no build
// version, so that test is not gated.
func (g *messageGen) emitLoadPayloadCanonical(f *ir.Field, owner, entryVar string, indent int) {
	ind := strings.Repeat(" ", indent)
	const ok = "!r.report.refused && !r.report.damaged"
	const own = "r.vocab.ownBuild()"
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
		refuse(own + " && r.at - canonAt == (long) r.vocab.refBits")
	case isUnionType(f.Type) && f.Array == ir.ArrayNone:
		g.pf("%sint canonUnk = r.report.unknown;\n", ind)
		g.emitLoadPayload(f, owner, entryVar, indent)
		refuse("r.report.unknown == canonUnk && !(" + g.emitMessageRides(f, owner, indent) + ")")
	default:
		g.pf("%sint canonUnk = r.report.unknown;\n", ind)
		g.emitLoadPayload(f, owner, entryVar, indent)
		rides := g.emitMessageRides(f, owner, indent)
		refuse(own + " && r.report.unknown == canonUnk && !(" + rides + ")")
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
// a value slot at its default. It is gated on the sender's build version as
// emitLoadPayloadCanonical's at-default test is.
func (g *messageGen) messageSlotAtDefault(f *ir.Field, elem string) string {
	const ok = "!r.report.refused && !r.report.damaged && r.vocab.ownBuild()"
	if isStructType(f.Type) {
		return ok + " && r.at - slotAt == (long) r.vocab.refBits"
	}
	return ok + " && r.report.unknown == slotUnk && !(" + g.scalarOrUnionRidesExpr(f, elem) + ")"
}
