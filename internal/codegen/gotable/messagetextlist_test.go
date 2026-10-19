package gotable

import (
	"fmt"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// The message form is inapplicable to a table carrying a text list
// (docs/SPEC-TABLES.md §2.13): Go refuses it by name at generation, emitting no
// message batch, vocabulary entry or announcement entry for that table, and an
// unrelated table of the same unit keeps its own.
const messageTextListSchema = `package probe

table A
{
    rules [..16]string(64)
}
table B
{
    notes [..4]?string(8)
}
table C
{
    id int32
    count int32
}
`

func messageSources(t *testing.T, u *ir.Unit) (core, message string) {
	t.Helper()
	out, err := Generate(u)
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	for name, data := range out {
		switch {
		case strings.HasSuffix(name, "TableMessage.go"):
			message += string(data)
		case strings.HasSuffix(name, "Table.go"):
			core += string(data)
		}
	}
	if core == "" || message == "" {
		t.Fatalf("the unit emitted no table or message module: core=%q message=%q", core, message)
	}
	return core, message
}

func sourceBlock(t *testing.T, src, open, close string) string {
	t.Helper()
	at := strings.Index(src, open)
	if at < 0 {
		t.Fatalf("the generated source does not hold %q", open)
	}
	rest := src[at+len(open):]
	end := strings.Index(rest, close)
	if end < 0 {
		t.Fatalf("the generated source does not close %q", open)
	}
	return rest[:end]
}

func TestGoMessageFormRefusesTextLists(t *testing.T) {
	t.Parallel()
	u := unitFrom(t, messageTextListSchema)
	core, message := messageSources(t, u)
	// The emitter marks its own repository paths for an adopter's linter, so a
	// citation reads "mas-bandwidth/schema docs/..."; strip that marker back off.
	whole := strings.ReplaceAll(core+message, "mas-bandwidth/schema ", "")

	// The message form's per-table verbs are emitted for C and not for A or B.
	for _, verb := range []string{"MeasureMessages", "SaveMessages", "LoadMessages", "SaveMessageBody", "LoadMessageBody"} {
		if strings.Contains(whole, "func A"+verb) || strings.Contains(whole, "func B"+verb) {
			t.Errorf("a text-list table carries a message verb %s (docs/SPEC-TABLES.md §2.13)", verb)
		}
		if !strings.Contains(whole, "func C"+verb) && strings.Contains(verb, "MessageBody") {
			t.Errorf("the message-eligible table lost its %s (docs/SPEC-TABLES.md §2.13)", verb)
		}
	}
	if !strings.Contains(whole, "func CSaveMessageBody") || !strings.Contains(whole, "func CLoadMessageBody") {
		t.Error("table C lost its message batch (docs/SPEC-TABLES.md §2.13)")
	}

	// The refusal is reported by name, naming the form, the table and the field.
	for _, tc := range []struct{ table, field string }{{"A", "rules"}, {"B", "notes"}} {
		want := "the message form is inapplicable to table " + tc.table + ": field " + tc.field + " is a text list (docs/SPEC-TABLES.md §2.13)"
		if !strings.Contains(whole, want) {
			t.Errorf("the emitter did not report %q", want)
		}
	}

	// No vocabulary entry for A or B; C's are still there.
	entries := sourceBlock(t, message, "var tableMessageEntries = [...]TableMessageEntry{", "}\n")
	for _, tc := range []struct{ table, field string }{{"A", "rules"}, {"B", "notes"}} {
		st := u.Tables[tc.table]
		refused := []ir.TableVocabularyEntry{
			ir.TableFieldEntry(st.Fields[0]),
			ir.TableFieldEntry(ir.TextListValue(st.Fields[0])),
			{Id: ir.TableWireId(st.WireName())},
		}
		for _, e := range refused {
			id := fmt.Sprintf("0x%016x", e.Id)
			if strings.Contains(entries, id) {
				t.Errorf("table %s still has the vocabulary entry %s (docs/SPEC-TABLES.md §2.13)", tc.table, messageEntryLiteral(e))
			}
		}
	}
	for _, f := range u.Tables["C"].Fields {
		e := ir.TableFieldEntry(f)
		id := fmt.Sprintf("0x%016x", e.Id)
		if !strings.Contains(entries, id) {
			t.Errorf("table C lost the vocabulary entry %s (docs/SPEC-TABLES.md §2.13)", messageEntryLiteral(e))
		}
	}

	// The announcement carries no entry for A or B, and does carry C's.
	announcement := strings.ReplaceAll(sourceBlock(t, message, "var tableAnnouncement = [...]byte{", "}"), " ", "")
	for _, tc := range []struct{ table, field string }{{"A", "rules"}, {"B", "notes"}} {
		st := u.Tables[tc.table]
		refused := []ir.TableVocabularyEntry{
			ir.TableFieldEntry(st.Fields[0]),
			ir.TableFieldEntry(ir.TextListValue(st.Fields[0])),
			{Id: ir.TableWireId(st.WireName())},
		}
		for _, e := range refused {
			if strings.Contains(announcement, byteLiterals(e.Encode(nil))) {
				t.Errorf("the announcement still carries table %s's entry %s (docs/SPEC-TABLES.md §2.13)", tc.table, messageEntryLiteral(e))
			}
		}
	}
	for _, f := range u.Tables["C"].Fields {
		e := ir.TableFieldEntry(f)
		if !strings.Contains(announcement, byteLiterals(e.Encode(nil))) {
			t.Errorf("the announcement lost table C's entry %s (docs/SPEC-TABLES.md §2.13)", messageEntryLiteral(e))
		}
	}
}

// TestGoMessageAnnouncementReadsBackWithTextListTable is the reversed witness:
// the same unit's own Announce is read back clean by AnnounceRead. At BASE the
// text-list table's element kind 12 in the vocabulary refuses it.
func TestGoMessageAnnouncementReadsBackWithTextListTable(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "the Go toolchain (it compiles and runs the generated unit)")
	runGenerated(t, messageTextListSchema, `package probe

import "testing"

func TestAnnouncementReadsBack(t *testing.T) {
	announcement := make([]byte, AnnounceMeasure())
	if Announce(announcement) != int64(len(announcement)) {
		t.Fatal("Announce")
	}
	var vocabulary TableVocabulary
	vocabulary.Init(make([]TableMessageEntry, TableMessageEntriesHere))
	var report TableReport
	if !AnnounceRead(&vocabulary, announcement, &report) {
		t.Fatalf("the unit's own announcement was refused: %+v", report)
	}
	if report != (TableReport{}) {
		t.Fatalf("the unit's own announcement read dirty: %+v", report)
	}
}
`)
}
