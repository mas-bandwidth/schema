package darttable

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

type malformedUnderCase struct {
	root string
	wire string
}

var malformedUnderCases = []malformedUnderCase{
	{"StorePurchase", "02004200000000000008805201636f696e732e7061e66b2e6c617267652e328230308b81990f0000681e00"},
}

// A second malformed-underflag cause remains: for enum and union field
// references the generated Dart load counts an unknown where the Go
// reference raises malformed — the reference is not refused as Go refuses
// it, and an unknown union arm's payload is not skipped
// (internal/tablewire/messagedecode.go, docs/SPEC-TABLES.md §3.3). Its
// signature on replay is leg {unknown:1 kind:0 widened:0 clamped:0
// duplicate:0 malformed:0 refused:0 count:0} vs oracle {unknown:0 kind:0
// widened:0 clamped:0 duplicate:0 malformed:1 refused:0 count:0}.
// Representative mutants: LoginRequest
// 02004200000000000008c00002030a11181f262d343b424950575e656c737a81888f969da4abb2b9c0c7ced5dc44f28800402102
// and StorePurchase
// 02004200000000000008805201636f696e732e7061636b2e6c617267652e323530308b81990f0000684400.
// These are the cause of a separate card (dart-fix-message-malformed-underflag's
// second cause) and are deliberately not fixed here.

func malformedUnderAnnounce(t *testing.T) []byte {
	t.Helper()
	data, err := os.ReadFile("../../../testdata/wire/tables/backend_conn.bin")
	if err != nil {
		t.Fatalf("announce: %v", err)
	}
	return data
}

func malformedUnderUnit(t *testing.T) *ir.Unit {
	t.Helper()
	schemas, err := filepath.Glob("../../../tables/backend/*.schema")
	if err != nil || len(schemas) == 0 {
		t.Fatalf("no backend schema: %v", err)
	}
	return loadUnit(t, schemas...)
}

// goDecodeMalformed is the Go reference (internal/tablewire/messagedecode.go,
// docs/SPEC-TABLES.md §3.3, §4): it answers the whole report the Dart batch
// load is compared against, not just the malformed flag.
func goDecodeMalformed(t *testing.T, u *ir.Unit, root string, wire []byte) (tabletext.Report, int) {
	t.Helper()
	m := tabletext.NewModel(u)
	var announced tabletext.Report
	vocab := &tablewire.Vocabulary{}
	if err := vocab.AnnounceRead(malformedUnderAnnounce(t), &announced); err != nil {
		t.Fatalf("announce read: %v", err)
	}
	inst := m.New(m.Lookup(root))
	report := tabletext.Report{}
	count, _, err := tablewire.DecodeMessages(m, []*tabletext.Instance{inst}, wire, vocab, &report)
	if err != nil {
		t.Fatalf("Go decode refused %s: %v", root, err)
	}
	return report, count
}

func malformedUnderProbeBody(root string, ann, wire []byte) string {
	lower := lowerFirst(root)
	return fmt.Sprintf(`
  final ann = Uint8List.fromList(%s);
  final msg = Uint8List.fromList(%s);
  final vocab = h.TableVocabulary(entries: List<h.TableMessageEntry>.generate(4096, (i) => h.TableMessageEntry(0, 0)));
  final annRep = h.TableReport();
  check(h.announceRead(vocab, ann, annRep), 'the connection announcement is refused: ' + annRep.reason);
  final value = h.%s();
  h.init%s(value);
  final values = <h.%s>[value];
  final loadRep = h.TableReport();
  final count = r.%sLoadMessages(values, msg, vocab, loadRep);
  print('dart report unknown=${loadRep.unknown} kind=${loadRep.kindMismatch} '
      'widened=${loadRep.widened} clamped=${loadRep.clamped} '
      'duplicate=${loadRep.duplicate} malformed=${loadRep.malformed ? 1 : 0} '
      'refused=${loadRep.refused ? 1 : 0} count=$count');
`, dartBytes(ann), dartBytes(wire), root, root, root, lower)
}

type malformedUnderDartReport struct {
	unknown, kind, widened, clamped, duplicate, malformed, refused, count int
}

// malformedUnderParseReport reads the one machine-readable line the probe
// prints after the load (docs/SPEC-TABLES.md §3.3, §4).
func malformedUnderParseReport(t *testing.T, out string) malformedUnderDartReport {
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
	for _, token := range strings.Fields(line) {
		key, value, ok := strings.Cut(token, "=")
		if !ok {
			t.Fatalf("dart report token %q is not key=value:\n%s", token, out)
		}
		n, err := strconv.Atoi(value)
		if err != nil {
			t.Fatalf("dart report %s=%q is not an integer:\n%s", key, value, out)
		}
		fields[key] = n
	}
	return malformedUnderDartReport{
		unknown:   fields["unknown"],
		kind:      fields["kind"],
		widened:   fields["widened"],
		clamped:   fields["clamped"],
		duplicate: fields["duplicate"],
		malformed: fields["malformed"],
		refused:   fields["refused"],
		count:     fields["count"],
	}
}

func TestMessageMalformedUnderflagDart(t *testing.T) {
	t.Parallel()
	u := malformedUnderUnit(t)
	ann := malformedUnderAnnounce(t)
	for _, c := range malformedUnderCases {
		wire, err := hex.DecodeString(c.wire)
		if err != nil {
			t.Fatal(err)
		}
		goReport, goCount := goDecodeMalformed(t, u, c.root, wire)
		if !goReport.Malformed {
			t.Fatalf("Go decodes %s malformed=0, want 1", c.root)
		}
		t.Run("Dart:"+c.root, func(t *testing.T) {
			t.Parallel()
			body := malformedUnderProbeBody(c.root, ann, wire)
			out, err := runBatchProbe(t, u, c.root, body)
			if err != nil {
				t.Fatalf("dart probe: %v\n%s", err, out)
			}
			got := malformedUnderParseReport(t, out)
			want := malformedUnderDartReport{
				unknown:   goReport.Unknown,
				kind:      goReport.KindMismatch,
				widened:   goReport.Widened,
				clamped:   goReport.Clamped,
				duplicate: goReport.Duplicate,
				malformed: boolInt(goReport.Malformed),
				refused:   boolInt(goReport.Refused),
				count:     goCount,
			}
			if got != want {
				t.Fatalf("%s Dart report %+v, want Go report %+v (docs/SPEC-TABLES.md §3.3, §4)\n%s",
					c.root, got, want, out)
			}
		})
	}
}

func boolInt(b bool) int {
	if b {
		return 1
	}
	return 0
}
