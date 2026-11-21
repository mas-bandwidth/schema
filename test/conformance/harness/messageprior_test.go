package main

import (
	"bytes"
	"fmt"
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

// TestMessagePriorWriterIsRangesApart holds what the rows claim about their
// writer: it is today's schema with every ranged declaration twice as wide,
// `mask` two bits wider and the three kind fields int32, so nothing but those
// declarations separates the writer from the reader.
func TestMessagePriorWriterIsRangesApart(t *testing.T) {
	t.Chdir(conformanceRoot(t))
	body := func(path string) string {
		text, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		_, after, ok := strings.Cut(string(text), "package tblprior\n")
		if !ok {
			t.Fatalf("%s is not package tblprior", path)
		}
		return after
	}
	narrow, wide := body("test/tables/prior/PriorNarrow.schema"), body("test/tables/prior/PriorWide.schema")
	if n := strings.Count(narrow, "max = 50"); n != 11 {
		t.Fatalf("PriorNarrow declares %d ranges of max = 50, want 11: move the rows with it", n)
	}
	want := strings.NewReplacer(
		"max = 50", "max = 100", "bits(5)", "bits(7)",
		"narrowed int16", "narrowed int32", "retyped  string(16)", "retyped  int32", "widened  int64", "widened  int32",
	).Replace(narrow)
	if wide != want {
		t.Errorf("PriorWide is not PriorNarrow with each range widened to max = 100, mask to bits(7) and the three kind fields int32")
	}
}

// TestMessagePriorWriterRows holds every prior-writer row end to end against
// the independent engine (docs/SPEC-TABLES.md §3.3, §4): the committed
// announcement and batch ARE what the prior writer writes for the row's value;
// today's reader reads a value inside its range and re-saves the pinned batch;
// and it REFUSES a value past its range with out_of_range and a kind that
// moved with kind_mismatch, neither clamping or defaulting the value nor
// delivering the message. A ranged shape owes one read and one refusal; the driver manifest hands a leg the same two lines for both, and
// the matrix owes a refusal for the second.
func TestMessagePriorWriterRows(t *testing.T) {
	m, _, u := corpus(t)
	rows := priorRows(t, m)
	if len(rows) == 0 {
		t.Fatal("the manifest names no message-prior row")
	}
	derived := filepath.Join(t.TempDir(), "MANIFEST.txt")
	if err := deriveManifest(m, derived); err != nil {
		t.Fatal(err)
	}
	handed, err := os.ReadFile(derived)
	if err != nil {
		t.Fatal(err)
	}
	wants, err := expectations(m, "message", nil, defaultJSONDir)
	if err != nil {
		t.Fatal(err)
	}
	owed := map[string]expectation{}
	for _, w := range wants {
		owed[w.name] = w
	}
	writers := map[string]*ir.Unit{}
	for name, row := range rows {
		// THE ENGINE'S OWN REASON, by the row's name. It is held here, against
		// the engine alone: a leg owes the verdict and no reason
		// (docs/rulings.json `refusal-verdict-only`).
		shape, verdict, ok := cutLast(name, "_")
		var reason string
		switch {
		case !ok:
			t.Errorf("%s: a row is <shape>_inside, <shape>_past or <shape>_kind", name)
		case verdict == "inside" && !row.Refused():
		case verdict == "past" && row.Refused():
			reason = tablewire.ReasonOutOfRange
			// a range owes both halves: the value the reader holds, and the one it does not
			if _, ok := rows[shape+"_inside"]; !ok {
				t.Errorf("%s has no twin %s_inside: a ranged shape owes one read and one refusal", name, shape)
			}
		case verdict == "kind" && row.Refused():
			reason = tablewire.ReasonKindMismatch
		default:
			t.Errorf("%s: _inside reads, and _past and _kind are refused", name)
		}

		reader, err := u.get(row.Unit)
		if err != nil {
			t.Fatal(err)
		}
		model := tabletext.NewModel(reader)
		writer := writers[row.WriterSchema]
		if writer == nil {
			c := compiler.New()
			c.FormatInPlace = false
			paths, err := compiler.GatherPaths([]string{row.WriterSchema})
			if err != nil {
				t.Fatal(err)
			}
			if writer, err = c.Load(paths); err != nil {
				t.Fatal(err)
			}
			writers[row.WriterSchema] = writer
		}
		if got := ir.BuildVersion(writer); got != row.PeerBuildVersion || got == ir.BuildVersion(reader) {
			t.Fatalf("%s: the writer's build version is 0x%016x: the row names 0x%016x and the reader is 0x%016x", name, got, row.PeerBuildVersion, ir.BuildVersion(reader))
		}
		wmodel := tabletext.NewModel(writer)

		// the committed peer files are the writer's own bytes for the value
		value := wmodel.New(wmodel.Lookup(row.Root))
		var wrep tabletext.Report
		if !wmodel.Read(value, []byte(row.WriterValue), &wrep) {
			t.Fatalf("%s: the prior writer refuses its own value: %+v", name, wrep)
		}
		batch, err := tablewire.EncodeMessages(wmodel, []*tabletext.Instance{value})
		if err != nil {
			t.Fatalf("%s: the prior writer refuses to write its value: %v", name, err)
		}
		pinned, err := os.ReadFile(row.PeerWire)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(batch, pinned) {
			t.Errorf("%s: %s is not what the prior writer writes; run make conformance-generate", name, row.PeerWire)
		}
		ann, err := os.ReadFile(row.PeerAnnounce)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(ann, tablewire.Announce(writer)) {
			t.Errorf("%s: %s is not the prior writer's announcement; run make conformance-generate", name, row.PeerAnnounce)
		}

		// today's reader, against the prior writer's announcement
		var vocab tablewire.Vocabulary
		var arep tabletext.Report
		if err := vocab.AnnounceRead(ann, &arep); err != nil {
			t.Fatalf("%s: the reader refuses the prior writer's announcement: %v", name, err)
		}
		vals := []*tabletext.Instance{model.New(model.Lookup(row.Root))}
		var rep tabletext.Report
		n, ok, derr := tablewire.DecodeMessages(model, vals, pinned, &vocab, &rep)
		if row.Refused() {
			if got := tablewire.RefusalReason(derr); ok || got != reason {
				t.Errorf("%s: the engine owes a %s refusal: ok=%t reason=%q (%v)", name, reason, ok, got, derr)
			}
			if w, ok := owed[name]; !ok || w.want != nil {
				t.Errorf("the matrix owes %s a refusal and no bytes", name)
			}
		} else {
			if !ok || derr != nil || n != 1 {
				t.Fatalf("%s: the reader refuses a value inside its range: ok=%t n=%d %v", name, ok, n, derr)
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
				t.Errorf("%s: the re-saved batch is not %s; run make conformance-generate", name, row.SaveWire)
			}
			if w, ok := owed[name]; !ok || !bytes.Equal(w.want, want) {
				t.Errorf("the matrix owes %s its re-saved batch", name)
			}
		}

		// what a leg is handed: the same two lines, whatever the answer
		for _, line := range []string{
			fmt.Sprintf("connection %s %s 0x%016x %s\n", name, row.Unit, row.PeerBuildVersion, row.PeerAnnounce),
			"message " + name + " " + name + " " + row.Root + " " + row.FileWire + " " + row.PeerWire + "\n",
		} {
			if !strings.Contains(string(handed), line) {
				t.Errorf("the driver manifest lacks %q", line)
			}
		}
	}
}

// cutLast cuts s around the last sep.
func cutLast(s, sep string) (string, string, bool) {
	i := strings.LastIndex(s, sep)
	if i < 0 {
		return s, "", false
	}
	return s[:i], s[i+len(sep):], true
}
