package cstable

import "testing"

// A RESERVED ID IS NO RECORD'S TYPE (docs/SPEC-TABLES.md §3.1, §3.3): a node
// record whose type reference resolves to the node-table id, the build-version
// id or the vocabulary id makes the whole load refuse, as the engine's record
// scan does (internal/tablewire/nodetable.go). The C# wire-alone walk is the
// one record scan every C# load runs before it decodes.
func TestCSharpWireWalkRefusesAReservedRecordType(t *testing.T) {
	t.Parallel()
	csEmittedRulesTest(t, csWireFuzzRepeatedIdSchema, []csEmittedRule{{
		start: "public void Root(ReadOnlySpan<byte> body)",
		end:   "void Graph()",
		line:  "if (Read64(Ids, (int)(reference - 1) * 8) >= ulong.MaxValue - 2) { Damage(); return; }",
		why:   "a node record typed by a reserved id is read",
	}})
}
