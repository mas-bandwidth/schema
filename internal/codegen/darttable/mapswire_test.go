package darttable

import (
	"bytes"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/dart"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
)

// Maps are sorted arrays of anonymous key/value bodies, including an entry
// whose key and value are both default (§2.8). This slice holds carried values;
// arm/optional/keyed/wide values remain subsequent work in the same card.
func TestDartMapWireOrderingAndReplacement(t *testing.T) {
	t.Parallel()
	u := unitFrom(t, `package probe
table Item { value int32 = 7 }
table Root { rows map[string(8)]Item
 ids map[uint64]Item
 after int32 }
`)
	tables, err := Generate(u)
	if err != nil {
		t.Fatalf("map wire must be emitted: %v", err)
	}
	files, err := dart.Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	maps.Copy(files, tables)
	model := tabletext.NewModel(u)
	root := model.New(u.Tables["Root"])
	root.Fields[2].Cell.I = 77
	root.Fields[2].Cell.U = 77
	for _, row := range []struct {
		key   string
		value int64
	}{{"💡", 1}, {"\uE000", 2}, {"", 7}, {"a", 3}} {
		e := model.NewMapEntry(root.Fields[0].Def)
		e.Fields[0].Cell.Str = []byte(row.key)
		e.Fields[1].Cell.Tab.Fields[0].Cell.I = row.value
		e.Fields[1].Cell.Tab.Fields[0].Cell.U = uint64(row.value)
		root.Fields[0].Entries = append(root.Fields[0].Entries, tabletext.Cell{Tab: e})
	}
	for _, id := range []uint64{^uint64(0), 0, 1 << 63} {
		e := model.NewMapEntry(root.Fields[1].Def)
		e.Fields[0].Cell.U = id
		root.Fields[1].Entries = append(root.Fields[1].Entries, tabletext.Cell{Tab: e})
	}
	want, err := tablewire.Encode(model, root)
	if err != nil {
		t.Fatal(err)
	}
	text, err := model.Write(root)
	if err != nil {
		t.Fatal(err)
	}
	text = append(text, '\n') // schema unpack adds the file terminator to Model.Write's raw JSON body.
	t.Run("wire-text-and-reports", func(t *testing.T) {
		t.Parallel()
		slowtest.Gate(t, "dart")
		dir := t.TempDir()
		for name, data := range files {
			if !strings.Contains(name, "/") {
				if err := os.WriteFile(filepath.Join(dir, name), data, 0600); err != nil {
					t.Fatal(err)
				}
			}
		}
		source := fmt.Sprintf(mapWireProbe, dartU64Literal(fieldId(u.Tables["Root"].Fields[0])), dartU64Literal(fieldId(u.Tables["Root"].Fields[2])), dartU64Literal(fieldId(u.Tables["RootRowsEntry"].Fields[0])))
		if err := os.WriteFile(filepath.Join(dir, "map_wire_probe.dart"), []byte(source), 0600); err != nil {
			t.Fatal(err)
		}
		cmd := exec.Command(dartBinary(t), "--enable-asserts", "map_wire_probe.dart")
		cmd.Dir = dir
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("map probe: %v\n%s", err, out)
		}
		got, err := hex.DecodeString(fieldOf(t, string(out), "wire"))
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(got, want) {
			t.Fatalf("map wire differs from Go oracle\n got %x\nwant %x", got, want)
		}
		gotText, err := base64.StdEncoding.DecodeString(fieldOf(t, string(out), "json"))
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(gotText, text) {
			t.Fatalf("map JSON differs from Go oracle\n got %s\nwant %s", gotText, text)
		}
	})
}

const mapWireProbe = `import 'dart:convert';
import 'dart:io';
import 'dart:typed_data';
import 'ProbeWire.dart' as w;
const rowsId=%s;
const afterId=%s;
const keyId=%s;
void check(bool ok,String message){if(!ok)throw StateError(message);}
String hex(Uint8List b)=>b.map((x)=>x.toRadixString(16).padLeft(2,'0')).join();
w.RootRowsEntry row(String key,int value)=>w.RootRowsEntry()..key=key..value=(w.Item()..value=value);
Uint8List forged(List<w.RootRowsEntry> entries,{bool wrongKey=false,bool repeatHeader=false}){
 final state=w.WireState();final out=w.TableWireWriter(state)..putByte(1);
 final ref=out.refFor(rowsId);final array=out.sub()..putByte(13)..putLeb(entries.length);
 for(final entry in entries){
  final body=array.sub();
  if(wrongKey){body.putLeb(body.refFor(keyId));body.putByte(8);body.putU32(99);body.putByte(0);}else{w.rootRowsEntryWireSaveBody(body,entry);}
  array.putLeb(body.buf.length);array.putBytes(body.buf);
 }
 out.putLeb(ref);out.putByte(14);out.putLeb(array.buf.length);out.putBytes(array.buf);
 if(repeatHeader){out.putLeb(ref);out.putByte(14);out.putLeb(2);out.putByte(8);out.putLeb(0);}
 out.putLeb(out.refFor(afterId));out.putByte(4);out.putU32(77);out.putByte(0);
 for(final id in state.ids){out.putU64(id);}out.putU64(state.ids.length);
 return Uint8List.fromList(out.buf);
}
void main(){
 final root=w.Root()..after=77;
 root.rows=[row('💡',1),row('\uE000',2),row('',7),row('a',3)];
 root.ids=[w.RootIdsEntry()..key=-1,w.RootIdsEntry()..key=0,w.RootIdsEntry()..key=-9223372036854775808];
 final bytes=w.rootWireSave(root);stdout.writeln('wire ${hex(bytes)}');
 final r=w.TableWireReport();final back=w.rootWireLoad(bytes,r);
 check(!r.malformed&&!r.refused&&r.unknown==0&&r.kindMismatch==0&&r.clamped==0&&r.duplicate==0,'valid map report');
 check(back.rows.map((e)=>e.key).join('|')=='|a|\uE000|💡','UTF8 key ordering');
 check(back.ids.map((e)=>e.key).join(',')=='0,-9223372036854775808,-1','uint64 key ordering');
 check(hex(w.rootWireSave(back))==hex(bytes),'wire round trip changed');
 final text=w.rootToJson(root);stdout.writeln('json ${base64Encode(utf8.encode(text))}');
 check(hex(w.rootWireSave(w.rootFromJson(text)))==hex(bytes),'JSON round trip changed');
 final duplicate=w.TableWireReport();final merged=w.rootWireLoad(forged([row('a',1),row('a',7)]),duplicate);
 check(merged.rows.length==1&&merged.rows.single.value.value==7&&duplicate.duplicate==1&&!duplicate.malformed&&merged.after==77,'duplicate whole reset/last wins');
 final descending=w.TableWireReport();final prefix=w.rootWireLoad(forged([row('z',1),row('a',2)]),descending);
 check(descending.malformed&&prefix.rows.length==1&&prefix.rows.single.key=='z'&&prefix.after==77,'descending prefix/parent continuation');
 final over=w.TableWireReport();final kept=w.rootWireLoad(forged([row('a',1),row('too-long-key',2),row('b',3)]),over);
 check(over.clamped==1&&!over.malformed&&kept.rows.length==2&&kept.rows.last.key=='b'&&kept.after==77,'long key drops whole entry');
 final mismatch=w.TableWireReport();final empty=w.rootWireLoad(forged([row('a',1)],wrongKey:true),mismatch);
 check(mismatch.kindMismatch==1&&!mismatch.malformed&&empty.rows.isEmpty&&empty.after==77,'wrong key kind resets map only');
 final skip=w.TableWireReport();final retained=w.rootWireLoad(forged([row('a',1)],repeatHeader:true),skip);
 check(skip.kindMismatch==1&&!skip.malformed&&retained.rows.length==1&&retained.rows.single.key=='a'&&retained.after==77,'incompatible repeated header preserves prior map');
 final malformed=w.TableTextReport();final partial=w.rootFromJson('{"ids":{"1":{},"1e+":{},"2":{}},"after":77}',malformed);
 check(malformed.malformed&&malformed.kindMismatch==0&&partial.ids.length==1&&partial.ids.single.key==1&&partial.after==0,'malformed numeric key stops at prefix');
 final dropped=w.TableTextReport();final fractional=w.rootFromJson('{"ids":{"1.5":{},"18446744073709551616":{},"2":{}},"after":77}',dropped);
 check(!dropped.malformed&&dropped.kindMismatch==2&&fractional.ids.length==1&&fractional.after==77,'fractional and out-of-range keys drop only entry');
 final repeated=w.TableTextReport();final last=w.rootFromJson('{"ids":{"2":{"value":1},"2":{}}}',repeated);
 check(last.ids.length==1&&last.ids.single.value.value==7&&repeated.duplicate==1,'identical raw JSON key whole reset');
 final parsed=w.rootFromJson('{"ids":{"2.0":{},"2e0":{"value":9}},"after":77}',w.TableTextReport());
 check(parsed.ids.length==1&&parsed.ids.single.key==2&&parsed.ids.single.value.value==9,'integral decimal keys/last reset');
 var refused=false;root.rows=[row('a',1),row('a',2)];try{w.rootWireSave(root);}on StateError{refused=true;}
 check(refused,'writer accepted duplicate identities');
}
`
