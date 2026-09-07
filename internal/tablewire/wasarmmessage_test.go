package tablewire_test

import (
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/check"
	"github.com/mas-bandwidth/schema/v2/internal/parser"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// AN ARM RENAMED UNDER `was` RIDES UNDER ITS OLD NAME'S HASH IN EVERY
// VOCABULARY, the MESSAGE FORM's announcement included (docs/SPEC-TABLES.md
// §5, §3.3): "every id derivation reads the alias". `ir.TableArmEntry` is that
// one derivation, and the announcement and the encoder both call it.
//
// The message form's arm DISPATCH is the half that has to agree. A reader
// matching on the DECLARED name's hash names nothing the writer wrote, so the
// arm is lost at its own build's hands: the value round-trips through the
// message form as `None` with an `unknown` counted, and a peer holding data
// written before the rename is refused the very arm the rename was taken to
// keep.
const wasArmUnit = `package wasarm

table Edit
{
    revision uint32
}

union Body
{
    edit  Edit
    tally int32 | min = 0, max = 100, was = "count"
}

table Note
{
    body Body
}
`

func wasArmModel(t *testing.T) *tabletext.Model {
	t.Helper()
	f, perrs := parser.Parse("WasArm.schema", []byte(wasArmUnit))
	if len(perrs) > 0 {
		t.Fatalf("parse: %v", perrs[0])
	}
	u, cerrs := check.Unit([]check.SourceFile{{
		Path: "WasArm.schema", Name: "WasArm.schema", Base: "WasArm", Bytes: []byte(wasArmUnit), AST: f,
	}})
	if len(cerrs) > 0 {
		t.Fatalf("check: %v", cerrs[0])
	}
	return tabletext.NewModel(u)
}

// The announcement's own entry, read straight off the derivation, so the claim
// below names the id the writer actually spends rather than a hash the test
// recomputes beside it.
func TestAWasRenamedArmAnnouncesTheOldNamesHash(t *testing.T) {
	m := wasArmModel(t)
	un := m.Unit.TableUnions["Body"]
	if un == nil {
		t.Fatal("the unit declares no table-closure union Body")
	}
	var arm ir.UnionVariant
	for _, v := range un.Variants {
		if v.Name == "tally" {
			arm = v
		}
	}
	if arm.Name == "" {
		t.Fatal("union Body declares no arm tally")
	}
	if got, want := ir.TableArmEntry(arm).Id, ir.TableWireId("count"); got != want {
		t.Errorf("the arm's announced id is 0x%016x, not the old name's hash 0x%016x", got, want)
	}
}

// AND THE DISPATCH AGREES, which is the claim the round trip holds: the
// message form writes the value and reads it back into a fresh instance, and
// the arm must come back SELECTED with its payload. Anchored to the tag and
// the payload rather than to a byte count, because a lost arm is a silent
// `None` and a shorter wire, not a decode failure.
func TestAWasRenamedArmSurvivesTheMessageForm(t *testing.T) {
	m := wasArmModel(t)
	inst := place(t, m, "Note", `{"body":{"tally":42}}`)
	wire, err := tablewire.EncodeMessage(m, inst)
	if err != nil {
		t.Fatalf("the message form refused the value: %v", err)
	}
	// the unit's OWN announcement, so the two sides are one build and the
	// claim is not about a version skew
	var vocabulary tablewire.Vocabulary
	var announced tabletext.Report
	if err := vocabulary.AnnounceRead(ir.TableAnnouncement(m.Unit), &announced); err != nil || !vocabulary.Announced() {
		t.Fatalf("the unit's own announcement was refused: %v %+v", err, announced)
	}
	back := m.New(m.Lookup("Note"))
	var report tabletext.Report
	ok, err := tablewire.DecodeMessage(m, back, wire, &vocabulary, &report)
	if err != nil || !ok {
		t.Fatalf("the message form did not read back: ok=%v err=%v", ok, err)
	}
	var body *tabletext.Field
	for i := range back.Fields {
		if back.Fields[i].Def.Name == "body" {
			body = &back.Fields[i]
		}
	}
	if body == nil {
		t.Fatal("the decoded instance has no field body")
	}
	if body.Cell.U == 0 {
		t.Fatalf("the was-renamed arm was lost: the union reads back None, report %+v", report)
	}
	if report.Unknown != 0 {
		t.Errorf("the was-renamed arm counted unknown %d times, and its own build wrote it", report.Unknown)
	}
	if arm := body.Cell.Arm; arm == nil || arm.Cell.I != 42 {
		t.Errorf("the arm's payload did not survive: %+v", arm)
	}
}
