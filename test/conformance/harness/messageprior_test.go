package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/compiler"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// priorRows answers the corpus's prior-writer rows by name.
func priorRows(t *testing.T, m *Manifest) map[string]MessageDrift {
	t.Helper()
	rows := map[string]MessageDrift{}
	for _, md := range m.MessageDrifts {
		if md.WriterSchema != "" {
			rows[md.Name] = md
		}
	}
	return rows
}

// TestMessagePriorWriterIsOneRangeApart holds what the rows claim about their
// writer: it is today's tables/backend schema with ONE declaration wider, the
// range of PlayerRow.score, so nothing but the tightened range separates the
// writer from the reader.
func TestMessagePriorWriterIsOneRangeApart(t *testing.T) {
	t.Chdir(conformanceRoot(t))
	now, err := os.ReadFile("tables/backend/Backend.schema")
	if err != nil {
		t.Fatal(err)
	}
	prior, err := os.ReadFile("test/tables/prior/BackendWideScore.schema")
	if err != nil {
		t.Fatal(err)
	}
	const narrow = "    score     int32 = 0 | min = 0, max = 100000\n"
	const wide = "    score     int32 = 0 | min = 0, max = 200000\n"
	if strings.Count(string(now), narrow) != 1 {
		t.Fatalf("tables/backend/Backend.schema no longer declares PlayerRow.score as %q: move the prior writer with it", narrow)
	}
	if want := strings.Replace(string(now), narrow, wide, 1); string(prior) != want {
		t.Errorf("the prior writer is not today's schema with the one range widened to %q", wide)
	}
}

// TestMessagePriorWriterRows holds the pair of rows end to end against the
// independent engine (docs/SPEC-TABLES.md §3.3, §4): the committed
// announcement and batches ARE what the prior writer writes for the two
// values; today's reader reads the value inside its range and re-saves the
// pinned batch; and it REFUSES the value past its range with out_of_range,
// neither clamping it nor delivering the message. The driver manifest hands a
// leg the same two lines for both rows, and the matrix owes a refusal for the
// second.
func TestMessagePriorWriterRows(t *testing.T) {
	m, _, u := corpus(t)
	rows := priorRows(t, m)
	inside, ok := rows["match_prior_inside"]
	if !ok {
		t.Fatal("the manifest names no message-prior row match_prior_inside")
	}
	past, ok := rows["match_prior_past"]
	if !ok {
		t.Fatal("the manifest names no message-prior row match_prior_past")
	}
	if inside.Refused() || !past.Refused() {
		t.Fatalf("the pair is one read and one refusal: inside refused=%t, past refused=%t", inside.Refused(), past.Refused())
	}

	reader, err := u.get(inside.Unit)
	if err != nil {
		t.Fatal(err)
	}
	model := tabletext.NewModel(reader)

	c := compiler.New()
	c.FormatInPlace = false
	paths, err := compiler.GatherPaths([]string{inside.WriterSchema})
	if err != nil {
		t.Fatal(err)
	}
	writer, err := c.Load(paths)
	if err != nil {
		t.Fatal(err)
	}
	if got := ir.BuildVersion(writer); got != inside.PeerBuildVersion || got == ir.BuildVersion(reader) {
		t.Fatalf("the writer's build version is 0x%016x: the row names 0x%016x and the reader is 0x%016x", got, inside.PeerBuildVersion, ir.BuildVersion(reader))
	}
	wmodel := tabletext.NewModel(writer)

	for _, row := range []MessageDrift{inside, past} {
		// the committed peer files are the writer's own bytes for the value
		text, err := os.ReadFile(row.WriterValue)
		if err != nil {
			t.Fatal(err)
		}
		value := wmodel.New(wmodel.Lookup(row.Root))
		var wrep tabletext.Report
		if !wmodel.Read(value, text, &wrep) {
			t.Fatalf("%s: the prior writer refuses its own value: %+v", row.Name, wrep)
		}
		batch, err := tablewire.EncodeMessages(wmodel, []*tabletext.Instance{value})
		if err != nil {
			t.Fatalf("%s: the prior writer refuses to write its value: %v", row.Name, err)
		}
		pinned, err := os.ReadFile(row.PeerWire)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(batch, pinned) {
			t.Errorf("%s: %s is not what the prior writer writes; run make conformance-generate", row.Name, row.PeerWire)
		}
		ann, err := os.ReadFile(row.PeerAnnounce)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(ann, tablewire.Announce(writer)) {
			t.Errorf("%s: %s is not the prior writer's announcement; run make conformance-generate", row.Name, row.PeerAnnounce)
		}

		// today's reader, against the prior writer's announcement
		var vocab tablewire.Vocabulary
		var arep tabletext.Report
		if err := vocab.AnnounceRead(ann, &arep); err != nil {
			t.Fatalf("%s: the reader refuses the prior writer's announcement: %v", row.Name, err)
		}
		vals := []*tabletext.Instance{model.New(model.Lookup(row.Root))}
		var rep tabletext.Report
		n, ok, derr := tablewire.DecodeMessages(model, vals, pinned, &vocab, &rep)
		if row.Refused() {
			if reason := tablewire.RefusalReason(derr); ok || reason != tablewire.ReasonOutOfRange {
				t.Errorf("%s: the reader owes an out_of_range refusal: ok=%t reason=%q (%v)", row.Name, ok, reason, derr)
			}
			continue
		}
		if !ok || derr != nil || n != 1 {
			t.Fatalf("%s: the reader refuses a value inside its range: ok=%t n=%d %v", row.Name, ok, n, derr)
		}
		saved, err := tablewire.EncodeMessages(model, vals)
		if err != nil {
			t.Fatal(err)
		}
		want, err := os.ReadFile(row.SaveWire)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(saved, want) {
			t.Errorf("%s: the re-saved batch is not %s; run make conformance-generate", row.Name, row.SaveWire)
		}
	}

	// what a leg is handed, and what the matrix owes for it
	derived := filepath.Join(t.TempDir(), "MANIFEST.txt")
	if err := deriveManifest(m, derived); err != nil {
		t.Fatal(err)
	}
	text, err := os.ReadFile(derived)
	if err != nil {
		t.Fatal(err)
	}
	for _, row := range []MessageDrift{inside, past} {
		for _, line := range []string{
			"connection " + row.Name + " " + row.Unit + " 0x01b45b6715922bb3 " + row.PeerAnnounce + "\n",
			"message " + row.Name + " " + row.Name + " " + row.Root + " " + row.FileWire + " " + row.PeerWire + "\n",
		} {
			if !strings.Contains(string(text), line) {
				t.Errorf("the driver manifest lacks %q", line)
			}
		}
	}
	wants, err := expectations(m, "message", nil, defaultJSONDir)
	if err != nil {
		t.Fatal(err)
	}
	owed := map[string]expectation{}
	for _, w := range wants {
		owed[w.name] = w
	}
	if w, ok := owed[inside.Name]; !ok || len(w.want) == 0 {
		t.Errorf("the matrix owes %s its re-saved batch", inside.Name)
	}
	if w, ok := owed[past.Name]; !ok || w.want != nil {
		t.Errorf("the matrix owes %s a refusal and no bytes", past.Name)
	}
}
