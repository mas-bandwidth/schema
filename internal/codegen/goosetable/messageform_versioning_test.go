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
// and the two legs must agree on everything the read decides:
//
//   - the bodies delivered and the damage flag,
//   - §4's counters: `unknown` (an id the reader cannot name, stepped over by
//     its announced shape), `kind_mismatch` (an announced kind off this
//     reader's ladder, stepped over), `widened` (a kind below this reader's
//     on the same ladder, decoded at the sender's width), `clamped`,
//   - the values landed (record images against the Go read's values), and
//   - the batch the reader saves of what it read, against the Go encode.
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
		c := c
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
				older := evolutionLoad(t, "test/tables/VOLD_"+c.row+".schema")
				oldFile := "old_" + c.row + ".bin"
				data, err := os.ReadFile(filepath.Join(corpus, oldFile))
				if err != nil {
					t.Fatal(err)
				}
				n = records[oldFile]
				oldBatch, why := gooseMsgFromFixed(t, older, root, data, n, true)
				if oldBatch == nil {
					t.Fatalf("the older writer refused %s", why)
				}
				batch = goEncodeAcross(t, older, writer, root, oldBatch, n)
				refused = ""
			}
			if batch == nil {
				batch, refused = gooseMsgFromFixed(t, writer, root, file, n, true)
				if batch == nil {
					t.Fatalf("the writer refused %s", refused)
				}
			}
			announce := ir.TableAnnouncement(writer)

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
			if err != nil || !ok || got != n {
				t.Fatalf("the Go reference reads the batch as n=%d ok=%v err=%v", got, ok, err)
			}
			want, err := tablewire.EncodeMessages(m, insts)
			if err != nil {
				t.Fatalf("the Go reference refuses to encode what it read: %v", err)
			}
			var values [][]string
			for _, inst := range insts {
				values = append(values, gooseInstAssign(t, "", inst))
			}

			// THE GOOSE LEG: AnnounceRead the writer, LoadMessages, SaveMessages
			r := gooseMsgProbe(t, gooseMsgCase{unit: reader, root: root, values: values, data: batch, announce: announce})
			if r.announced != "1 ok 0 0" {
				t.Fatalf("AnnounceRead of the writer's announcement answered %q:\n%s", r.announced, r.raw)
			}
			r.agrees(t, want, n)
			type counter struct {
				name      string
				got, want int64
			}
			for _, k := range []counter{
				{"unknown", r.loadUnknown, int64(rep.Unknown)},
				{"kind_mismatch", r.loadMismatch, int64(rep.KindMismatch)},
				{"widened", r.loadWidened, int64(rep.Widened)},
				{"clamped", r.loadClamped, int64(rep.Clamped)},
			} {
				if k.got != k.want {
					t.Fatalf("%s: the Goose read counted %d, the Go reference %d:\n%s", k.name, k.got, k.want, r.raw)
				}
			}
			t.Logf("unknown=%d kind_mismatch=%d widened=%d clamped=%d", rep.Unknown, rep.KindMismatch, rep.Widened, rep.Clamped)
			mu.Lock()
			moved[c.name] = int64(rep.Unknown + rep.KindMismatch + rep.Widened + rep.Clamped)
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
	if err := vocab.AnnounceRead(ir.TableAnnouncement(older), &arep); err != nil {
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
		t.Fatalf("the Go reference reads the older batch as n=%d ok=%v err=%v", got, ok, err)
	}
	out, err := tablewire.EncodeMessages(m, insts)
	if err != nil {
		t.Fatalf("the Go reference refuses to encode: %v", err)
	}
	return out
}
