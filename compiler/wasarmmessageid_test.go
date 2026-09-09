package compiler

import (
	"fmt"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/ir"
)

// THE REFERENCE'S MESSAGE-FORM DISPATCH READS THE `was` ALIAS, as every other
// id derivation does (docs/SPEC-TABLES.md §5, §3.3). The announcement's entry
// for an arm is `ir.TableArmEntry`, which hashes the arm's WIRE name — the old
// name after a rename — so a `<Table>LoadMessageBody` that switched on the
// DECLARED name's hash would name nothing its own writer wrote.
//
// The gate is the C++ reference's because the table layer is (§11, §15), and
// it is anchored to the emitted `case` line rather than to the id appearing
// anywhere in the output: the OLD name's hash is in the header already, in the
// file form's arm dispatch and in the reflection descriptor, so a
// whole-output claim stays green with the message form still wrong.
const wasArmMessageUnit = `package wasarmmsg

fixed table Edit
{
    revision uint32
}

union Body
{
    edit  Edit
    tally int32 | min = 0, max = 100, was = "count"
}

fixed table Note
{
    body Body
}
`

func TestTheMessageFormsArmDispatchReadsTheWasAlias(t *testing.T) {
	text := generatedText(t, wasArmMessageUnit, "cpp")
	// the DEFINITION, not the forward declaration the header opens with: the
	// definition is the later of the two, and the body runs to the next
	// function
	open := strings.LastIndex(text, "inline bool NoteLoadMessageBody(")
	if open < 0 {
		t.Fatal("cpp: the unit emits no NoteLoadMessageBody to dispatch in")
	}
	body, _, found := strings.Cut(text[open+len("inline bool NoteLoadMessageBody("):], "\ninline ")
	if !found {
		t.Fatal("cpp: NoteLoadMessageBody has no end, and the slice would be the whole header")
	}
	want := fmt.Sprintf("case 0x%016xull: // tally", ir.TableWireId("count"))
	stale := fmt.Sprintf("case 0x%016xull: // tally", ir.TableWireId("tally"))
	if !strings.Contains(body, want) {
		t.Errorf("cpp: the message form does not dispatch the renamed arm on its old name's hash, %q absent from NoteLoadMessageBody", want)
	}
	if strings.Contains(body, stale) {
		t.Errorf("cpp: the message form dispatches the renamed arm on its DECLARED name's hash, %q — the announcement writes the alias's", stale)
	}
}
