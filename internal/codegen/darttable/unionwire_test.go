package darttable

import (
	"bytes"
	"encoding/hex"
	"fmt"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
	"github.com/mas-bandwidth/schema/v2/ir"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestDartTableBodyAndVoidUnionArms(t *testing.T) {
	src := `package probe
 table Item { value int32 = 7
 next *Item }
 union Choice { item Item
 ping }
 table Root { choice Choice
 body *string
 after int32 }
 `
	u := unitFrom(t, src)
	files, err := Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(files["ProbeWire.dart"]), "final class Choice") {
		t.Fatal("carried union storage absent")
	}

	moved, err := Generate(unitFrom(t, strings.Replace(src, "item Item\n ping", "ping\n item Item", 1)))
	if err != nil {
		t.Fatal(err)
	}
	m := tabletext.NewModel(u)
	cases := []string{`{}`, `{"choice":{"ping":null}}`, `{"choice":{"item":{}}}`, `{"choice":{"item":{"value":9}}}`, `{"choice":{"item":{"next":{"value":10}}}}`}
	t.Run("runtime-production-oracle", func(t *testing.T) {
		slowtest.Gate(t, "dart")
		dir := t.TempDir()
		for name, data := range files {
			if !strings.Contains(name, "/") {
				if err := os.WriteFile(filepath.Join(dir, name), data, 0600); err != nil {
					t.Fatal(err)
				}
			}
		}
		if err := os.Mkdir(filepath.Join(dir, "v2"), 0700); err != nil {
			t.Fatal(err)
		}
		for name, data := range moved {
			if !strings.Contains(name, "/") {
				if err := os.WriteFile(filepath.Join(dir, "v2", name), data, 0600); err != nil {
					t.Fatal(err)
				}
			}
		}
		probe := `import 'dart:typed_data';
import 'ProbeWire.dart';
import 'v2/ProbeWire.dart' as moved;
String hex(List<int> b)=>b.map((x)=>x.toRadixString(16).padLeft(2,'0')).join();
Uint8List forged(int mode){
 final state=WireState();final out=TableWireWriter(state)..putByte(1);final ref=out.refFor(CHOICEID);final arm=out.refFor(ITEMID);
 final payload=out.sub();itemWireSaveBody(payload,Item()..value=9);
 out.putLeb(ref);out.putByte(15);out.putLeb(arm);out.putByte(13);out.putLeb(payload.buf.length);out.putBytes(payload.buf);
 out.putLeb(ref);out.putByte(mode==6?4:15);
 if(mode==6){out.putU32(123);}else if(mode==0){out.putLeb(0);}else{
  out.putLeb(mode==1?out.refFor(12345678):mode==4?out.refFor(PINGID):arm);
  out.putByte(mode==3?4:mode==4?32:13);
  final data=mode==2?<int>[0,0]:mode==3?<int>[1,0,0,0]:<int>[0];
  out.putLeb(data.length);out.putBytes(data);
 }
 out.putLeb(out.refFor(AFTERID));out.putByte(4);out.putU32(77);out.putByte(0);
 for(final id in state.ids){out.putU64(id);}out.putU64(state.ids.length);return Uint8List.fromList(out.buf);
}
void main(){
 if(ChoiceType.none!=0||ChoiceType.item!=1||ChoiceType.ping!=2||ChoiceType.max!=2||ChoiceType.count!=2)throw StateError('tag namespace');
 for(final input in <String>['{}','{"choice":{"ping":null}}','{"choice":{"item":{}}}','{"choice":{"item":{"value":9}}}','{"choice":{"item":{"next":{"value":10}}}}']){
  final value=rootFromJson(input);final wire=rootWireSave(value);final report=TableWireReport();final loaded=rootWireLoad(wire,report);
  if(report.malformed||report.kindMismatch!=0||report.unknown!=0)throw StateError('clean load');
  if(hex(rootWireSave(loaded))!=hex(wire))throw StateError('resave');
  final reordered=moved.rootWireLoad(wire);if(hex(moved.rootWireSave(reordered))!=hex(wire))throw StateError('arm name identity across reorder');
  print(hex(wire));print(rootToJson(value).replaceAll('\n','|'));
 }
 final malformed=TableTextReport();final prefix=rootFromJson('{"choice":{"item":{"value":9},"ping":null},"after":77}',malformed);print(hex(rootWireSave(prefix)));print('${malformed.malformed}');
 for(var mode=0;mode<7;mode++){final wire=forged(mode);final r=TableWireReport();final v=rootWireLoad(wire,r);print(hex(wire));print('${v.choice.type},${v.choice.item.value},${v.after},${r.malformed},${r.unknown},${r.kindMismatch}');}
}`
		for key, value := range map[string]uint64{"CHOICEID": fieldId(u.Tables["Root"].Fields[0]), "ITEMID": ir.TableWireId(u.TableUnions["Choice"].Variants[0].WireName()), "PINGID": ir.TableWireId(u.TableUnions["Choice"].Variants[1].WireName()), "AFTERID": fieldId(u.Tables["Root"].Fields[2])} {
			probe = strings.ReplaceAll(probe, key, dartU64Literal(value))
		}
		if err := os.WriteFile(filepath.Join(dir, "probe.dart"), []byte(probe), 0600); err != nil {
			t.Fatal(err)
		}
		for _, args := range [][]string{{"analyze", "--fatal-infos", "probe.dart"}, {"--enable-asserts", "probe.dart"}} {
			cmd := exec.Command(dartBinary(t), args...)
			cmd.Dir = dir
			out, err := cmd.CombinedOutput()
			if err != nil {
				t.Fatalf("%v: %v\n%s", args, err, out)
			}
			if args[0] == "analyze" {
				continue
			}
			lines := strings.Split(strings.TrimSpace(string(out)), "\n")
			if len(lines) != 2*len(cases)+16 {
				t.Fatalf("output %s", out)
			}
			for i, input := range cases {
				v := m.New(u.Tables["Root"])
				report := tabletext.Report{}
				if !m.Read(v, []byte(input), &report) {
					t.Fatal("Go text oracle refused", input, report)
				}
				want, err := tablewire.Encode(m, v)
				if err != nil {
					t.Fatal(err)
				}
				got, err := hex.DecodeString(lines[i*2])
				if err != nil {
					t.Fatal(err)
				}
				if !bytes.Equal(got, want) {
					t.Fatalf("%s wire got%x want%x", input, got, want)
				}
				text, err := m.Write(v)
				if err != nil {
					t.Fatal(err)
				}
				if lines[i*2+1] != strings.ReplaceAll(string(text)+"\n", "\n", "|") {
					t.Fatalf("JSON got %s want %s", lines[i*2+1], text)
				}
			}
			v := m.New(u.Tables["Root"])
			r := tabletext.Report{}
			if m.Read(v, []byte(`{"choice":{"item":{"value":9},"ping":null},"after":77}`), &r) || !r.Malformed {
				t.Fatal("Go malformed union oracle", r)
			}
			want, err := tablewire.Encode(m, v)
			if err != nil {
				t.Fatal(err)
			}
			got, err := hex.DecodeString(lines[2*len(cases)])
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(got, want) || lines[2*len(cases)+1] != "true" {
				t.Fatalf("malformed JSON union prefix got%x want%x report%s", got, want, lines[2*len(cases)+1])
			}
			for mode := 0; mode < 7; mode++ {
				wire, err := hex.DecodeString(lines[2*len(cases)+2+mode*2])
				if err != nil {
					t.Fatal(err)
				}
				v := m.New(u.Tables["Root"])
				r := tabletext.Report{}
				if _, err := tablewire.Decode(m, v, wire, &r); err != nil {
					t.Fatal(err)
				}
				value := int64(7)
				if v.Fields[0].Cell.Tab != nil {
					value = v.Fields[0].Cell.Tab.Fields[0].Cell.I
				}
				want := fmt.Sprintf("%d,%d,%d,%v,%d,%d", v.Fields[0].Cell.U, value, v.Fields[2].Cell.I, r.Malformed, r.Unknown, r.KindMismatch)
				got := lines[2*len(cases)+2+mode*2+1]
				if got != want {
					t.Fatalf("forged mode%d got %s want %s", mode, got, want)
				}
			}

		}
	})
}
