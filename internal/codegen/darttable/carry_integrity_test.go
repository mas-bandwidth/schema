package darttable

import (
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
	"hash/crc32"
	"maps"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func integrityTestSeal(data []byte) []byte {
	out := append([]byte(nil), data...)
	return binary.LittleEndian.AppendUint32(out, crc32.Checksum(data, crc32.MakeTable(crc32.Castagnoli)))
}

func TestDartIntegrityNative(t *testing.T) {
	slowtest.Gate(t, "dart")
	fixture := "../../../testdata/conformance/feature-witnesses/integrity-forms"
	schema, err := os.ReadFile(filepath.Join(fixture, "Probe.schema"))
	if err != nil {
		t.Fatal(err)
	}
	u := unitFrom(t, string(schema))
	m := tabletext.NewModel(u)
	files, err := Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	maps.Copy(files, packetFiles(t, u))
	var b strings.Builder
	b.WriteString(`import 'dart:typed_data';
import 'dart:convert';
import 'ProbeWire.dart' as w;
import 'ProbeFixed.dart' as f;
void check(bool ok,String s){if(!ok)throw StateError(s);}
String hex(List<int>b)=>b.map((x)=>x.toRadixString(16).padLeft(2,'0')).join();
Uint8List bytes(String s)=>Uint8List.fromList([for(var i=0;i<s.length;i+=2)int.parse(s.substring(i,i+2),radix:16)]);
void main(){
check(w.tableIntegrityCRC(ascii.encode('123456789'),9)==0xe3069283,'standard CRC32C vector');
`)
	for _, input := range []string{`{}`, `{"n":7,"child":{"x":9}}`} {
		v := m.New(u.Tables["PlainRoot"])
		if !m.Read(v, []byte(input), &tabletext.Report{}) {
			t.Fatal("input")
		}
		raw, err := tablewire.Encode(m, v)
		if err != nil {
			t.Fatal(err)
		}
		pin := integrityTestSeal(raw)
		fmt.Fprintf(&b, `{
final pin=bytes(%q);final value=w.rootFromJson(%q);check(hex(w.rootWireSave(value))==hex(pin),'root sealed wire');
final r=w.TableWireReport();final back=w.rootWireLoad(pin,r);check(!r.refused&&hex(w.rootWireSave(back))==hex(pin),'root load');
check(w.rootWireMeasure(pin)>0,'sealed region measure');
final store=w.TableRetain(4096,List.filled(4096,0));final rr=w.TableWireReport();final retained=w.rootWireLoadRetain(pin,store,rr);
check(hex(w.rootWireSaveRetain(retained,store,w.TableWireReport()))==hex(pin),'retained CRC');
for(var at=1;at<pin.length;at++){for(var bit=0;bit<8;bit++){
 final bad=Uint8List.fromList(pin);bad[at]^=1<<bit;final er=w.TableWireReport();final empty=w.rootWireLoad(bad,er);
 check(er.refused&&er.reason=='integrity_mismatch'&&er.unknown==0&&empty.n==0&&empty.child==null,'all single-bit variable corruption predecode');
 check(w.rootWireMeasure(bad)==-1,'damaged region measure');
 final re=w.TableWireReport();w.rootWireLoadRetain(bad,store,re);check(re.reason=='integrity_mismatch'&&store.used==0,'retained corruption discards store');
}}
for(var cut=1;cut<5;cut++){final er=w.TableWireReport();w.rootWireLoad(Uint8List.sublistView(pin,0,cut),er);check(er.reason=='integrity_mismatch','short variable CRC');}
final wrong=w.TableWireReport();w.rootWireLoad(Uint8List.fromList([2]),wrong);check(wrong.refused&&wrong.reason=='unknown_form','form before CRC');
final empty=w.TableWireReport();w.rootWireLoad(Uint8List(0),empty);check(empty.refused&&empty.reason!='integrity_mismatch','empty framing');
final old=w.TableWireReport();w.rootWireLoad(Uint8List.sublistView(pin,0,pin.length-4),old);check(old.refused,'missing checksum refuses');
final plain=w.TableWireReport();w.plainRootWireLoad(pin,plain);check(plain.refused,'sealed file refuses plain reader');
final reused=w.TableWireReport();final damaged=Uint8List.fromList(pin)..[1]^=1;w.rootWireLoad(damaged,reused);w.rootWireLoad(pin,reused);check(reused.reason=='ok','integrity result reset on reuse');
}
`, hex.EncodeToString(pin), input)
	}
	// A tagged child is a body, not a second sealed file.
	plain := m.New(u.Tables["PlainRoot"])
	if !m.Read(plain, []byte(`{"n":11,"child":{"x":13}}`), &tabletext.Report{}) {
		t.Fatal("plain input")
	}
	plainWire, err := tablewire.Encode(m, plain)
	if err != nil {
		t.Fatal(err)
	}
	fmt.Fprintf(&b, `{final v=w.plainRootFromJson('{"n":11,"child":{"x":13}}');final wire=w.plainRootWireSave(v);check(hex(wire)==%q,'plain parent tagged child has no extra trailer');final r=w.TableWireReport();check(w.plainRootWireLoad(wire,r).child!.x==13,'plain nested read');}
`, hex.EncodeToString(plainWire))
	// The fixed-root form-1 pin is independently sealed from an undeclared root.
	// Their field ids and body are identical; the declaration adds exactly one CRC.
	reading := m.New(u.Tables["PlainReading"])
	if !m.Read(reading, []byte(`{"x":19,"y":-3,"label":"ok"}`), &tabletext.Report{}) {
		t.Fatal("reading input")
	}
	raw, err := tablewire.Encode(m, reading)
	if err != nil {
		t.Fatal(err)
	}
	fmt.Fprintf(&b, `{final v=w.readingFromJson('{"x":19,"y":-3,"label":"ok"}');final pin=bytes(%q);check(hex(w.readingWireSave(v))==hex(pin),'fixed-root form1 independent CRC');final r=w.TableWireReport();check(w.readingWireLoad(pin,r).x==19,'fixed-root form1 read');}
`, hex.EncodeToString(integrityTestSeal(raw)))
	fixedPin := func(name string) string {
		data, err := os.ReadFile(filepath.Join(fixture, name))
		if err != nil {
			t.Fatal(err)
		}
		return strings.TrimSpace(string(data))
	}
	// Encode an UNDECLARED batch and seal it with Go's independent standard CRC;
	// this does not depend on whether the neutral declared-root CRC fix is present.
	data, err := os.ReadFile(filepath.Join(fixture, "readings.json"))
	if err != nil {
		t.Fatal(err)
	}
	var inputs []json.RawMessage
	if err = json.Unmarshal(data, &inputs); err != nil {
		t.Fatal(err)
	}
	var values []*tabletext.Instance
	for _, input := range inputs {
		v := m.New(u.Tables["PlainReading"])
		if !m.Read(v, input, &tabletext.Report{}) {
			t.Fatal("batch input")
		}
		values = append(values, v)
	}
	rawMessage, err := tablewire.EncodeMessages(m, values)
	if err != nil {
		t.Fatal(err)
	}
	fmt.Fprintf(&b, `{
final pin=bytes(%q),plainPin=bytes(%q);final rows=List.generate(3,(i)=>f.Reading());final plan=f.readingFixedNewPlan();final r=f.TableFixedReport();
check(f.readingFixedLoad(rows,3,pin,pin.length,plan,r)==3&&rows[2].x==1002,'independent Go fixed pin read');
check(f.readingFixedMeasure(3)==pin.length,'fixed measure includes CRC');
final out=Uint8List(pin.length+7)..fillRange(0,pin.length+7,0xa5);
check(f.readingFixedSave(rows,3,out)==pin.length&&hex(out.sublist(0,pin.length))==hex(pin)&&out.skip(pin.length).every((x)=>x==0xa5),'fixed exact save and untouched suffix');
final short=Uint8List(pin.length-1)..fillRange(0,pin.length-1,0xa5);check(f.readingFixedSave(rows,3,short)==-1&&short.every((x)=>x==0xa5),'short fixed writer untouched');
final plainRows=List.generate(3,(i)=>f.PlainReading());check(f.plainReadingFixedLoad(plainRows,3,plainPin,plainPin.length,f.plainReadingFixedNewPlan(),f.TableFixedReport())==3,'plain fixed pin');
final plainOut=Uint8List(f.plainReadingFixedMeasure(3));check(f.plainReadingFixedSave(plainRows,3,plainOut)==plainPin.length&&hex(plainOut)==hex(plainPin),'plain fixed unchanged');
for(var at=1;at<pin.length;at++){for(var bit=0;bit<8;bit++){
 final bad=Uint8List.fromList(pin);bad[at]^=1<<bit;final dest=List.generate(3,(i)=>f.Reading()..x=777);final er=f.TableFixedReport();
 check(f.readingFixedLoad(dest,3,bad,bad.length,plan,er)==-1&&f.TableFixedRefusal.name(er.refused)=='integrity_mismatch'&&dest.every((x)=>x.x==777)&&er.unknown==0,'all single-bit fixed corruption before landing');
}}
for(var cut=1;cut<5;cut++){final er=f.TableFixedReport();check(f.readingFixedLoad(rows,3,pin,cut,plan,er)==-1&&f.TableFixedRefusal.name(er.refused)=='integrity_mismatch','short fixed CRC');}
final wrong=f.TableFixedReport();check(f.readingFixedLoad(rows,3,Uint8List.fromList([1]),1,plan,wrong)==-1&&f.TableFixedRefusal.name(wrong.refused)=='previous_form','fixed form before checksum');
final vocab=f.TableVocabulary(entries:List.generate(4096,(i)=>f.TableMessageEntry(0,0)));check(f.announceRead(vocab,f.tableAnnouncement,f.TableReport()),'vocabulary');
final plainMessage=bytes(%q);final plainMessageOut=Uint8List(plainMessage.length+7)..fillRange(0,plainMessage.length+7,0xa5);check(f.plainReadingSaveMessages(plainRows,plainMessageOut,f.TableReport())==plainMessage.length&&hex(plainMessageOut.sublist(0,plainMessage.length))==hex(plainMessage)&&plainMessageOut.skip(plainMessage.length).every((x)=>x==0xa5),'plain message writes reused buffer and leaves suffix');
final message=bytes(%q);check(f.readingMeasureMessages(rows,f.TableReport())==message.length,'message measure includes trailer');
final msgOut=Uint8List(message.length+7)..fillRange(0,message.length+7,0xa5);check(f.readingSaveMessages(rows,msgOut,f.TableReport())==message.length&&hex(msgOut.sublist(0,message.length))==hex(message)&&msgOut.skip(message.length).every((x)=>x==0xa5),'independent message CRC pin and untouched suffix');
final msgShort=Uint8List(message.length-1)..fillRange(0,message.length-1,0xa5);check(f.readingSaveMessages(rows,msgShort,f.TableReport())==-1&&msgShort.every((x)=>x==0xa5),'short message writer untouched');
final dest=List.generate(3,(i)=>f.Reading());check(f.readingLoadMessages(dest,message,vocab,f.TableReport())==3&&dest[2].x==1002,'message load');
for(var at=1;at<message.length;at++){for(var bit=0;bit<8;bit++){
 final bad=Uint8List.fromList(message);bad[at]^=1<<bit;final er=f.TableReport();final unchanged=List.generate(3,(i)=>f.Reading()..x=777);
 check(f.readingLoadMessages(unchanged,bad,vocab,er)==-1&&er.refused&&er.reason=='integrity_mismatch'&&er.unknown==0&&unchanged.every((x)=>x.x==777),'all single-bit message corruption predecode');
}}
for(var cut=1;cut<6;cut++){final er=f.TableReport();check(f.readingLoadMessages(dest,Uint8List.sublistView(message,0,cut),vocab,er)==-1&&er.reason=='integrity_mismatch','short message CRC');}
final wrongMsg=f.TableReport();check(f.readingLoadMessages(dest,Uint8List.fromList([1]),vocab,wrongMsg)==-1&&wrongMsg.reason=='newer_form','message form first');
}
}
`, fixedPin("reading.fixed.hex"), fixedPin("plain-reading.fixed.hex"), hex.EncodeToString(rawMessage), hex.EncodeToString(integrityTestSeal(rawMessage)))
	runFormsProbe(t, files, b.String())
}
