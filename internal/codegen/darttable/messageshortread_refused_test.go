package darttable

// A DART MESSAGE BATCH WHOSE BODY RUNS OUT OF BITS IS DAMAGED, and DAMAGE
// DISCARDS THE WHOLE BATCH (docs/SPEC-TABLES.md §4, §3.3): the generated Dart
// batch load owes the refusal, malformed, and delivers no body. This test loads
// the four representative mutants with the generated Dart batch load and the
// Go decode, and holds the Dart leg to the refusal; no counter and no saved
// byte is compared.

import (
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
	"github.com/mas-bandwidth/schema/v2/ir"
)

type shortReadCase struct {
	root     string
	unit     string
	announce string
	mutant   string
}

var shortReadCases = []shortReadCase{
	{"Wide00", "../../../tables/vocab/*.schema", "../../../testdata/wire/tables/vocab_conn.bin",
		"0200010500000001"},
	{"LoginRequest", "../../../tables/backend/*.schema", "../../../testdata/wire/tables/backend_conn.bin",
		"02004200000000000008c00002030a11181f262d343b424950575e656c737a81888f969da4abb2b9c0c7ced5dc44f28800003101"},
	{"StorePurchase", "../../../tables/backend/*.schema", "../../../testdata/wire/tables/backend_conn.bin",
		"02004200000000000008805201636f696e732e7061636b2e6c617267652e323530308b"},
	{"MatchResult", "../../../tables/backend/*.schema", "../../../testdata/wire/tables/backend_conn.bin",
		"02004640404040404040c021a00f00000000000020d20400217d00000000000000f928900420a80f000000000000206c0512"},
}

func shortReadAnnounce(t *testing.T, path string) []byte {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("announce: %v", err)
	}
	return data
}

func shortReadUnit(t *testing.T, glob string) *ir.Unit {
	t.Helper()
	schemas, err := filepath.Glob(glob)
	if err != nil || len(schemas) == 0 {
		t.Fatalf("no schemas under %s: %v", glob, err)
	}
	return loadUnit(t, schemas...)
}

// shortReadOracle is the Go reference (internal/tablewire/messagedecode.go,
// docs/SPEC-TABLES.md §3.3): decode the batch whole, then encode body one as it
// stands, exactly as the wire fuzz harness's oracle does
// (test/conformance/harness/wirefuzz.go, the message branch).
func shortReadOracle(t *testing.T, u *ir.Unit, root string, announce, wire []byte) (tabletext.Report, []byte) {
	t.Helper()
	m := tabletext.NewModel(u)
	var announced tabletext.Report
	vocab := &tablewire.Vocabulary{}
	if err := vocab.AnnounceRead(announce, &announced); err != nil {
		t.Fatalf("announce read: %v", err)
	}
	def := u.Tables[root]
	if def == nil {
		t.Fatalf("no table %s", root)
	}
	bodies, _ := tablewire.MessageCount(wire, vocab)
	if bodies < 1 {
		bodies = 1
	}
	batch := make([]*tabletext.Instance, bodies)
	batch[0] = m.New(def)
	for i := 1; i < len(batch); i++ {
		batch[i] = m.New(def)
	}
	report := tabletext.Report{}
	_, _, err := tablewire.DecodeMessages(m, batch, wire, vocab, &report)
	if err != nil {
		t.Fatalf("Go decode refused %s: %v", root, err)
	}
	saved, err := tablewire.EncodeMessage(m, batch[0])
	if err != nil {
		t.Fatalf("Go encode %s: %v", root, err)
	}
	return report, saved
}

func shortReadProbeBody(root string, ann, wire []byte, bodies int) string {
	lower := lowerFirst(root)
	return fmt.Sprintf(`
  final ann = Uint8List.fromList(%s);
  final msg = Uint8List.fromList(%s);
  final vocab = h.TableVocabulary(entries: List<h.TableMessageEntry>.generate(4096, (i) => h.TableMessageEntry(0, 0)));
  final annRep = h.TableReport();
  check(h.announceRead(vocab, ann, annRep), 'the connection announcement is refused: ' + annRep.reason);
  final values = <h.%s>[for (var i = 0; i < %d; i++) h.%s()];
  for (final v in values) { h.init%s(v); }
  final loadRep = h.TableReport();
  final count = r.%sLoadMessages(values, msg, vocab, loadRep);
  final first = <h.%s>[values[0]];
  final measureRep = h.TableReport();
  final m = r.%sMeasureMessages(first, measureRep);
  final buf = Uint8List(m);
  final saveRep = h.TableReport();
  final wrote = r.%sSaveMessages(first, buf, saveRep);
  print('dart report unknown=${loadRep.unknown} kind=${loadRep.kindMismatch} '
      'widened=${loadRep.widened} clamped=${loadRep.clamped} '
      'duplicate=${loadRep.duplicate} malformed=${loadRep.malformed ? 1 : 0} '
      'refused=${loadRep.refused ? 1 : 0} count=$count saved=$wrote hex=' +
      buf.map((x) => x.toRadixString(16).padLeft(2, '0')).join());
`, dartBytes(ann), dartBytes(wire), root, bodies, root, root, lower, root, lower, lower)
}

type shortReadDartReport struct {
	unknown, kind, widened, clamped, duplicate, malformed, refused, count int
}

// shortReadDart is the one machine-readable line the probe prints after the
// load and the re-save of body one.
type shortReadDart struct {
	report shortReadDartReport
	hex    string
}

func parseShortReadDart(t *testing.T, out string) shortReadDart {
	t.Helper()
	const marker = "dart report "
	at := strings.LastIndex(out, marker)
	if at < 0 {
		t.Fatalf("dart probe printed no report line:\n%s", out)
	}
	line := out[at+len(marker):]
	if end := strings.IndexByte(line, '\n'); end >= 0 {
		line = line[:end]
	}
	fields := map[string]int{}
	hexStr := ""
	for token := range strings.FieldsSeq(line) {
		key, value, ok := strings.Cut(token, "=")
		if !ok {
			t.Fatalf("dart report token %q is not key=value:\n%s", token, out)
		}
		if key == "hex" {
			hexStr = value
			continue
		}
		n, err := strconv.Atoi(value)
		if err != nil {
			t.Fatalf("dart report %s=%q is not an integer:\n%s", key, value, out)
		}
		fields[key] = n
	}
	return shortReadDart{report: shortReadDartReport{
		unknown:   fields["unknown"],
		kind:      fields["kind"],
		widened:   fields["widened"],
		clamped:   fields["clamped"],
		duplicate: fields["duplicate"],
		malformed: fields["malformed"],
		refused:   fields["refused"],
		count:     fields["count"],
	}, hex: hexStr}
}

func TestMessageShortReadRefusedDart(t *testing.T) {
	t.Parallel()
	for _, c := range shortReadCases {
		t.Run(c.root, func(t *testing.T) {
			t.Parallel()
			wire, err := hex.DecodeString(c.mutant)
			if err != nil {
				t.Fatal(err)
			}
			ann := shortReadAnnounce(t, c.announce)
			u := shortReadUnit(t, c.unit)
			goReport, _ := shortReadOracle(t, u, c.root, ann, wire)
			if !goReport.Malformed {
				t.Fatalf("Go decodes %s malformed=0, want 1", c.root)
			}
			bodies := int(wire[1]) + 1
			body := shortReadProbeBody(c.root, ann, wire, bodies)
			out, err := runBatchProbe(t, u, c.root, body)
			if err != nil {
				t.Fatalf("dart probe: %v\n%s", err, out)
			}
			if got := parseShortReadDart(t, out); got.report.malformed != 1 {
				t.Fatalf("%s: the Dart batch load did not refuse a damaged batch: %+v\n%s", c.root, got.report, out)
			}
		})
	}
}
