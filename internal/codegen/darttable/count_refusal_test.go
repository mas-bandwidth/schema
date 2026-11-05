package darttable

import (
	"encoding/binary"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
	"github.com/mas-bandwidth/schema/v2/ir"
)

const countRefusalSchema = `package countprobe
union Choice { value Child }
table Child { items []int32 }
table Root {
 marker int32 = 7
 items []int32
 entries map[int32]int32
 children map[int32]Child
 child Child
 pick Choice
 ptr *Child
 bounded [..2]int32
}
`

// The count is wire input, not a fixture allocation: all payloads stay small.
// Cap refusal must unwind nested decoders without inventing framing damage.
func TestWireCountRefusalDart(t *testing.T) {
	slowtest.Gate(t, "dart")
	out, err := runCountRefusalProbe(t, nil)
	if err != nil {
		t.Fatalf("count refusal probe: %v\n%s", err, out)
	}
}

// Restore the old cap-as-damage branch in the generated units. The same
// native inputs must reject it on both list and map refusals.
func TestWireCountRefusalDartNegativeControl(t *testing.T) {
	slowtest.Gate(t, "dart")
	out, err := runCountRefusalProbe(t, func(files map[string][]byte) {
		changed := 0
		for name, src := range files {
			text := string(src)
			const from = "throw const _TableWireCountRefusal();"
			if strings.Contains(text, from) {
				changed += strings.Count(text, from)
				files[name] = []byte(strings.ReplaceAll(text, from, "{ r.report.malformed = true; r.pos = end; break; }"))
			}
		}
		if changed == 0 {
			t.Fatal("count refusal sabotage matched no generated cap")
		}
	})
	if err == nil {
		t.Fatalf("cap-as-damage sabotage stayed green:\\n%s", out)
	}
	for _, label := range []string{"list_2147483648 named refusal", "map_2147483648 named refusal", "fabricated damage"} {
		if !strings.Contains(string(out), label) {
			t.Errorf("control missed %s:\\n%s", label, out)
		}
	}
}

func runCountRefusalProbe(t *testing.T, edit func(map[string][]byte)) ([]byte, error) {
	t.Helper()
	u := buildUnit(t, countRefusalSchema)
	files, err := Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	for name, src := range packetFiles(t, u) {
		if !strings.Contains(name, "/") {
			files[name] = src
		}
	}
	if edit != nil {
		edit(files)
	}
	dir := t.TempDir()
	writeFiles(t, dir, files)
	// refs: marker, unknown, items, entries, child, pick, value, nodes, Child, bounded, children, key
	ids := []uint64{ir.TableWireId("marker"), ir.TableWireId("unknown"), ir.TableWireId("items"), ir.TableWireId("entries"), ir.TableWireId("child"), ir.TableWireId("pick"), ir.TableWireId("value"), ir.TableNodeWireId, ir.TableWireId("Child"), ir.TableWireId("bounded"), ir.TableWireId("children"), ir.MapKeyWireId}
	field := func(ref, kind byte, body []byte) []byte {
		b := []byte{ref, kind}
		if kind == 12 || kind == 13 || kind == 14 {
			b = binary.AppendUvarint(b, uint64(len(body)))
		}
		return append(b, body...)
	}
	wrap := func(body []byte) []byte {
		b := append([]byte{1}, body...)
		b = append(b, 0)
		for _, id := range ids {
			b = binary.LittleEndian.AppendUint64(b, id)
		}
		return binary.LittleEndian.AppendUint64(b, uint64(len(ids)))
	}
	prefix := []byte{1, 4, 99, 0, 0, 0, 2, 4, 5, 0, 0, 0}
	model := tabletext.NewModel(u)
	oracle := func(wire []byte, cap bool) {
		var report tabletext.Report
		ok, err := tablewire.Decode(model, model.New(model.Lookup("Root")), wire, &report)
		var refusal *tablewire.CountRefusal
		if ok || errors.As(err, &refusal) != cap || report.Malformed == cap {
			t.Fatalf("fixture oracle cap=%v: ok=%v err=%v report=%+v", cap, ok, err, report)
		}
	}
	var rows strings.Builder
	for _, count := range []uint64{1 << 31, 1 << 32, 1 << 63} {
		list := binary.AppendUvarint([]byte{4}, count)
		list = append(list, 23, 0, 0, 0)
		widenedList := append(binary.AppendUvarint([]byte{2}, count), 23)
		mp := binary.AppendUvarint([]byte{13}, count)
		mp = append(mp, 1, 0)
		nested := append(field(3, 14, list), 0)
		arm := append(binary.AppendUvarint([]byte{7, 13}, uint64(len(nested))), nested...)
		nodes := binary.AppendUvarint([]byte{1, 9}, uint64(len(nested)))
		nodes = append(nodes, nested...)
		for _, row := range []struct {
			name    string
			body    []byte
			unknown int
		}{
			{"list", field(3, 14, list), 1}, {"widened_list", field(3, 14, widenedList), 1}, {"map", field(4, 14, mp), 1},
			{"nested", field(5, 13, nested), 1}, {"arm", field(6, 15, arm), 1},
			{"unreached_node", field(8, 12, nodes), 0},
		} {
			wire := wrap(append(append([]byte{}, prefix...), row.body...))
			oracle(wire, true)
			fmt.Fprintf(&rows, "  refused(%q, %s, %d);\n", fmt.Sprintf("%s_%d", row.name, count), dartCountBytes(wire), row.unknown)
		}
	}
	// A descending map key prevents its body from decoding, even when the
	// key occurs after a nested collection whose count would otherwise refuse.
	huge := binary.AppendUvarint([]byte{4}, 1<<31)
	huge = append(huge, 23, 0, 0, 0)
	badChild := append(field(3, 14, huge), 0)
	entry := func(key byte, child []byte, keyLast bool) []byte {
		k := []byte{12, 4, key, 0, 0, 0}
		v := field(7, 13, child)
		if keyLast {
			return append(append(v, k...), 0)
		}
		return append(append(k, v...), 0)
	}
	first := entry(5, []byte{0}, false)
	for _, key := range []byte{3, 5, 7} {
		second := entry(key, badChild, true)
		mp := []byte{13, 2}
		mp = binary.AppendUvarint(mp, uint64(len(first)))
		mp = append(mp, first...)
		mp = binary.AppendUvarint(mp, uint64(len(second)))
		mp = append(mp, second...)
		wire := wrap(append(append([]byte{}, prefix...), field(11, 14, mp)...))
		oracle(wire, key >= 5)
		if key < 5 {
			fmt.Fprintf(&rows, "  damaged('descending-map',%s);\n", dartCountBytes(wire))
		} else {
			fmt.Fprintf(&rows, "  refused('ordered-map-%d',%s,1);\n", key, dartCountBytes(wire))
		}
	}
	// An in-cap count without enough elements is damage, not storage refusal.
	short := wrap(append(append([]byte{}, prefix...), field(3, 14, []byte{4, 2, 23, 0, 0, 0})...))
	valid := wrap([]byte{1, 4, 99, 0, 0, 0})
	bounded := binary.AppendUvarint([]byte{4}, 1<<31)
	bounded = append(bounded, 23, 0, 0, 0, 24, 0, 0, 0)
	boundedWire := wrap(field(10, 14, bounded))
	body := strings.ReplaceAll(countRefusalDart, "CASES", rows.String())
	body = strings.ReplaceAll(body, "SHORT", dartCountBytes(short))
	body = strings.ReplaceAll(body, "VALID", dartCountBytes(valid))
	body = strings.ReplaceAll(body, "BOUNDED", dartCountBytes(boundedWire))
	if err := os.WriteFile(filepath.Join(dir, "probe.dart"), []byte(body), 0600); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(dartBinary(t), "--enable-asserts", "probe.dart")
	cmd.Dir = dir
	return cmd.CombinedOutput()
}

func dartCountBytes(b []byte) string {
	parts := make([]string, len(b))
	for i, v := range b {
		parts[i] = fmt.Sprint(v)
	}
	return "Uint8List.fromList([" + strings.Join(parts, ",") + "])"
}

const countRefusalDart = `import 'dart:io';
import 'dart:typed_data';
import 'CountprobeWire.dart' as w;
bool failed=false;
void check(bool ok,String label) { if(!ok){stderr.writeln('FAIL: $label');failed=true;} }
w.TableWireReport seeded(bool damage) => w.TableWireReport()
 ..unknown=3 ..kindMismatch=4 ..widened=5 ..clamped=6 ..duplicate=7
 ..retained=8 ..retainLost=9 ..malformed=damage;
bool defaults(w.Root v) => v.marker==7 && v.items.isEmpty && v.entries.isEmpty &&
 v.children.isEmpty && v.child.items.isEmpty && v.pick.armTag==0 && v.ptr==null && v.bounded.isEmpty;
void refused(String label,Uint8List wire,int unknown) {
 for(final retain in [false,true]) {
  for(final priorDamage in [false,true]) {
   final r=seeded(priorDamage);
   final store=w.TableRetain(4096,List<int>.filled(512,0));
   final v=retain?w.rootWireLoadRetain(wire,store,r):w.rootWireLoad(wire,r);
   check(r.refused && r.reason=='count_over_extent_cap','$label named refusal retain=$retain');
   check(r.malformed==priorDamage,'$label fabricated damage retain=$retain');
   check(defaults(v),'$label partial destination retain=$retain');
   check(r.unknown==3+unknown && r.kindMismatch==4 && r.widened==5 &&
     r.clamped==6 && r.duplicate==7 && r.retainLost==9 &&
     r.retained==8+(retain?unknown:0),'$label event history retain=$retain');
   check(store.used==0 && store.idUsed==0 && store.records.isEmpty,'$label retained state');
   if(!priorDamage) {
    final good=retain?w.rootWireLoadRetain(VALID,store,r):w.rootWireLoad(VALID,r);
    check(good.marker==99 && !r.malformed,'$label reused report poisoned valid load');
    final other=Uint8List.fromList([2]);
    if(retain){w.rootWireLoadRetain(other,store,r);}else{w.rootWireLoad(other,r);}
    check(r.refused && r.reason=='unknown_form' && !r.malformed,
      '$label old count reason replaced another refusal');
   }
  }
 }
}
void damaged(String label,Uint8List wire) {
 for(final retain in [false,true]) {
  final r=seeded(false);
  final store=w.TableRetain(4096,List<int>.filled(512,0));
  final v=retain?w.rootWireLoadRetain(wire,store,r):w.rootWireLoad(wire,r);
  check(r.malformed && !r.refused && r.reason=='wire_damaged','$label remains damage');
  check(defaults(v) && store.used==0 && store.records.isEmpty,'$label discard');
 }
}
void main() {
CASES
 damaged('body-short',SHORT);
 final boundedReport=w.TableWireReport();
 final bounded=w.rootWireLoad(BOUNDED,boundedReport);
 check(!boundedReport.malformed && !boundedReport.refused && boundedReport.clamped==1 &&
   bounded.bounded.length==2 && bounded.bounded[0]==23 && bounded.bounded[1]==24,
   'bounded array keeps its independent clamp rule');
 if(failed)exit(1);
 stdout.writeln('count refusal/default/history/retention/body-short/bounded controls pass');
}
`
