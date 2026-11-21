package goosetable

// THE MESSAGE FORM ACROSS BUILDS (docs/SPEC-TABLES.md §3.3, §4): the typed
// reader BY PEER ENTRY, held to the Go reference over every row of the
// versioning corpus (test/tables/VOLD_<row>.schema, VNEW_<row>.schema; the
// rows of docs/FIXED-FORM-VERSIONING-TESTS.md), in BOTH directions.
//
// A row's writer unit loads its corpus file (the C++ reference's fixed-form
// bytes, build/fixedform-corpus) with the Goose fixed reader and saves the
// records as ONE message batch. The reader unit then does what an endpoint
// does: AnnounceRead the WRITER's announcement, LoadMessages the batch
// against it. The Go engine reads the same batch under the same announcement
// (tablewire.AnnounceRead, tablewire.DecodeMessages) into the reader's model,
// and the two legs must agree on the VERDICT (docs/rulings.json
// refusal-verdict-only):
//
//   - refused by both (why is per-language diagnostic detail, never
//     compared), or accepted by both with
//   - the bodies delivered,
//   - the values landed (record images against the Go read's values), and
//   - the batch the reader saves of what it read, against the Go encode.
//
// §4's counters (`unknown`, `widened`) are logged, never compared across
// targets (conformance-is-read-then-write).
//
// NEW-READS-OLD reads the older writer: the widening rows widen, the append
// rows land defaults. OLD-READS-NEW reads the newer writer: an appended field
// is `unknown`, a widened kind is `kind_mismatch` (a reader never narrows), a
// grown bound clamps. The message form carries no layout hash, so neither
// direction refuses: that is the fixed form's rule (§5.3), and this is the
// form whose versioning is the vocabulary's.

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
	"github.com/mas-bandwidth/schema/v2/ir"
)

func TestGooseMessageFormVersioning(t *testing.T) {
	t.Parallel()
	corpus := fixedCorpus(t)
	records := fixedManifestRecords(t, corpus)
	manifest := gooseVersionManifest(t, corpus)
	type direction struct{ name, row, file, writer, reader string }
	var cases []direction
	for _, r := range gooseVersionRows {
		cases = append(cases,
			direction{"new_reads_old/" + r.row, r.row, "old_" + r.row + ".bin", "VOLD_" + r.row, "VNEW_" + r.row},
			direction{"old_reads_new/" + r.row, r.row, "new_" + r.row + ".bin", "VNEW_" + r.row, "VOLD_" + r.row})
	}
	cases = append(cases, direction{"old_reads_new/unknown_census", "unknown_census", "many_unknown_census.bin", "VOLD_unknown_census", "VNEW_unknown_census"})
	var mu sync.Mutex
	moved := map[string]int64{}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			writer := evolutionLoad(t, "test/tables/"+c.writer+".schema")
			reader := evolutionLoad(t, "test/tables/"+c.reader+".schema")
			root := manifest.line(t, c.file).root
			n := records[c.file]
			if n < 1 {
				t.Fatalf("the corpus manifest gives %s no records", c.file)
			}
			file, err := os.ReadFile(filepath.Join(corpus, c.file))
			if err != nil {
				t.Fatal(err)
			}
			batch, refused := gooseMsgFromFixed(t, writer, root, file, n, false)
			if batch == nil && strings.HasPrefix(refused, "fixed-load ") && c.writer == "VNEW_"+c.row {
				// THE NEWER FILE CARRIES VALUES ITS OWN STRICT FIXED READ
				// REFUSES (a value past the declared bound, written on purpose
				// for the fixed form's rows): the newer writer's batch is then
				// the older file's values as the newer build holds them, the Go
				// reference's read of the older batch encoded under the newer
				// unit, so the older reader still meets the newer vocabulary
				oldFile := "old_" + c.row + ".bin"
				data, err := os.ReadFile(filepath.Join(corpus, oldFile))
				if err != nil {
					t.Fatal(err)
				}
				n = records[oldFile]
				batch, _ = gooseMsgFromFixed(t, writer, root, data, n, true)
				if batch == nil {
					older := evolutionLoad(t, "test/tables/VOLD_"+c.row+".schema")
					oldBatch, why := gooseMsgFromFixed(t, older, root, data, n, true)
					if oldBatch == nil {
						t.Fatalf("the older writer refused %s", why)
					}
					batch = goEncodeAcross(t, older, writer, root, oldBatch, n)
				}
			}
			if batch == nil {
				batch, refused = gooseMsgFromFixed(t, writer, root, file, n, true)
				if batch == nil {
					t.Fatalf("the writer refused %s", refused)
				}
			}
			announce := tablewire.Announce(writer)

			// THE REFERENCE: the reader's model, the writer's announcement
			var vocab tablewire.Vocabulary
			var arep tabletext.Report
			if err := vocab.AnnounceRead(announce, &arep); err != nil {
				t.Fatalf("the Go reference refuses the writer's announcement: %v", err)
			}
			m := tabletext.NewModel(reader)
			st := fixedRootStruct(t, reader, root)
			insts := make([]*tabletext.Instance, n)
			for i := range insts {
				insts[i] = m.New(st)
			}
			var rep tabletext.Report
			got, ok, err := tablewire.DecodeMessages(m, insts, batch, &vocab, &rep)
			if err != nil {
				// A strict refusal is the answer (docs/SPEC-TABLES.md §6.5):
				// Goose must refuse the same batch. Only the VERDICT is held
				// (docs/rulings.json refusal-verdict-only); the reasons are
				// printed as diagnostics, never compared. An error from the
				// reference that is not a read's refusal is a failure.
				if !tablewire.ReadRefusal(err) {
					t.Fatalf("the Go reference failed rather than refused: %v", err)
				}
				r := gooseMsgProbe(t, gooseMsgCase{unit: reader, root: root, data: batch, announce: announce, capacity: n})
				if r.announced != "1 ok 0 0" {
					t.Fatalf("AnnounceRead of the writer's announcement answered %q:\n%s", r.announced, r.raw)
				}
				if !r.loadRefused() {
					t.Fatalf("the Go reference refused (%s: %v); Goose accepted n=%d %s:\n%s", tablewire.RefusalReason(err), err, r.loadN, r.loadReason, r.raw)
				}
				t.Logf("refused by both: Go %s, Goose %s", tablewire.RefusalReason(err), r.loadReason)
				return
			}
			if !ok || got != n {
				t.Fatalf("the Go reference neither refused nor delivered every body: n=%d of %d ok=%v", got, n, ok)
			}
			var values [][]string
			for _, inst := range insts {
				values = append(values, gooseInstAssign(t, "", inst))
			}
			want, encErr := gooseEncodeMessages(m, insts)

			// THE GOOSE LEG: AnnounceRead the writer, LoadMessages, SaveMessages
			r := gooseMsgProbe(t, gooseMsgCase{unit: reader, root: root, values: values, data: batch, announce: announce})
			if r.announced != "1 ok 0 0" {
				t.Fatalf("AnnounceRead of the writer's announcement answered %q:\n%s", r.announced, r.raw)
			}
			if encErr != nil {
				// The reference decoded a value its own encoder will not write
				// back (a flag bit the reader's variants do not name). The
				// landed values are still the oracle.
				if r.loadN != int64(n) || r.loadReason != "ok" || r.loadMalformed {
					t.Fatalf("the Go reference read the batch and will not encode it (%v); Goose answered n=%d %s malformed=%v:\n%s", encErr, r.loadN, r.loadReason, r.loadMalformed, r.raw)
				}
				for k := range r.got {
					if k >= len(r.want) || r.got[k] != r.want[k] {
						t.Fatalf("body %d loaded\n got  %s\n want %s", k, r.got[k], r.want[k])
					}
				}
			} else {
				r.agrees(t, want, n)
			}
			// §4's unknown and widened counters are per-language detail, never
			// compared across targets (docs/rulings.json
			// conformance-is-read-then-write): logged, and the Go side's alone
			// feeds the battery's non-vacuity check below. The Goose leg's own
			// kind_mismatch and clamped stay at zero on an accepted read.
			if r.loadMismatch != 0 || r.loadClamped != 0 {
				t.Fatalf("the Goose read accepted with kind_mismatch=%d clamped=%d:\n%s", r.loadMismatch, r.loadClamped, r.raw)
			}
			t.Logf("unknown: Go %d Goose %d; widened: Go %d Goose %d", rep.Unknown, r.loadUnknown, rep.Widened, r.loadWidened)
			mu.Lock()
			moved[c.name] = int64(rep.Unknown + rep.Widened)
			mu.Unlock()
		})
	}
	t.Cleanup(func() {
		// the battery is vacuous if no row moved a counter
		mu.Lock()
		defer mu.Unlock()
		total := int64(0)
		for _, v := range moved {
			total += v
		}
		if len(moved) == len(cases) && total == 0 {
			t.Errorf("no versioning row moved a counter: the peer read was never exercised")
		}
	})
}

// goEncodeAcross is the Go reference carrying a batch across one build: read
// under the older unit's announcement into the newer unit's model, then
// encoded by the newer unit.
func goEncodeAcross(t *testing.T, older, newer *ir.Unit, root string, batch []byte, n int) []byte {
	t.Helper()
	var vocab tablewire.Vocabulary
	var arep tabletext.Report
	if err := vocab.AnnounceRead(tablewire.Announce(older), &arep); err != nil {
		t.Fatalf("the Go reference refuses the announcement: %v", err)
	}
	m := tabletext.NewModel(newer)
	st := fixedRootStruct(t, newer, root)
	insts := make([]*tabletext.Instance, n)
	for i := range insts {
		insts[i] = m.New(st)
	}
	var rep tabletext.Report
	if got, ok, err := tablewire.DecodeMessages(m, insts, batch, &vocab, &rep); err != nil || !ok || got != n {
		mo := tabletext.NewModel(older)
		ost := fixedRootStruct(t, older, root)
		oinsts := make([]*tabletext.Instance, n)
		for i := range oinsts {
			oinsts[i] = mo.New(ost)
		}
		var orep tabletext.Report
		if ogot, ook, oerr := tablewire.DecodeMessages(mo, oinsts, batch, &vocab, &orep); oerr != nil || !ook || ogot != n {
			t.Fatalf("the Go reference reads the older batch as n=%d ok=%v err=%v", ogot, ook, oerr)
		}
		for i := range insts {
			insts[i] = m.New(st)
			for j := range insts[i].Fields {
				for k := range oinsts[i].Fields {
					if insts[i].Fields[j].Def.Name == oinsts[i].Fields[k].Def.Name {
						insts[i].Fields[j].Cell = oinsts[i].Fields[k].Cell
						insts[i].Fields[j].Count = oinsts[i].Fields[k].Count
						insts[i].Fields[j].Present = oinsts[i].Fields[k].Present
						insts[i].Fields[j].Elems = oinsts[i].Fields[k].Elems
					}
				}
			}
		}
	}
	out, err := gooseEncodeMessages(m, insts)
	if err != nil {
		t.Fatalf("the Go reference refuses to encode: %v", err)
	}
	return out
}
