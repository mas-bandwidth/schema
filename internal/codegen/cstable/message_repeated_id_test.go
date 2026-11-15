package cstable

import "testing"

// AN ID TWICE IN ONE MESSAGE BODY, OR A KEY TWICE IN ONE KEYED ARRAY, IS DAMAGE
// (docs/SPEC-TABLES.md §3.2, §3.3), whether this reader can name it or not, as
// the engine's body and keyed reads refuse it (internal/tablewire/messagedecode.go
// bodyEntry, keyedReference).
func TestCSharpMessageReadRefusesARepeatedId(t *testing.T) {
	t.Parallel()
	csEmittedRulesTest(t, csWireFuzzRepeatedIdSchema, []csEmittedRule{
		{start: "static bool MessageReadBody(", end: "static bool MessageReadField(",
			line: "if (Reserved(entry.Id) || !SeenOnce(few, ref count, ref more, entry.Id)) { return false; }",
			why:  "a field id twice in one message body is read"},
		{start: "static bool MessageReadField(", end: "static bool MessageReadElement(",
			line: "if (!MessageName(ref r, d, out _, out TableMessageEntry key) || !SeenOnce(few, ref count, ref more, key.Id)) { return false; }",
			why:  "a key twice in one keyed array is read"},
	})
}
