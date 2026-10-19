package darttable

import (
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
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

func goDecodeMalformed(t *testing.T, u *ir.Unit, root string, wire []byte) (malformed bool) {
	t.Helper()
	m := tabletext.NewModel(u)
	var rep tabletext.Report
	vocab := &tablewire.Vocabulary{}
	if err := vocab.AnnounceRead(malformedUnderAnnounce(t), &rep); err != nil {
		t.Fatalf("announce read: %v", err)
	}
	inst := m.New(m.Lookup(root))
	report := tabletext.Report{}
	tablewire.DecodeMessages(m, []*tabletext.Instance{inst}, wire, vocab, &report)
	return report.Malformed
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
  print('dart malformed=${loadRep.malformed} count=$count');
  check(loadRep.malformed, 'malformed=0, want malformed=1; count=$count');
`, dartBytes(ann), dartBytes(wire), root, root, root, lower)
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
		t.Run("Go::"+c.root, func(t *testing.T) {
			if !goDecodeMalformed(t, u, c.root, wire) {
				t.Fatalf("Go decodes %s malformed=0, want 1", c.root)
			}
		})
		t.Run("Dart:"+c.root, func(t *testing.T) {
			t.Parallel()
			body := malformedUnderProbeBody(c.root, ann, wire)
			out, err := runBatchProbe(t, u, c.root, body)
			if err != nil {
				t.Fatalf("dart probe: %v\n%s", err, out)
			}
		})
	}
}
