package darttable

import (
	"bytes"
	"encoding/base64"
	"encoding/hex"
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

func TestDartKeyedArraysFindSlotsByName(t *testing.T) {
	t.Parallel()
	src := `package probe
 enum Rank { alpha, beta, gamma }
 table Item { a int32 = 7
 b int32 }
 table Root { scores [Rank]int32
 items [Rank]Item
 body *string
 after int32
 ids [Rank]uint64
 floats [Rank]float32
 doubles [Rank]float64 }
 `
	u := unitFrom(t, src)
	files, err := Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	moved := unitFrom(t, strings.Replace(src, "alpha, beta, gamma", "gamma, alpha, beta", 1))
	v2, err := Generate(moved)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := files["ProbeWire.dart"]; !ok {
		t.Fatal("keyed closure must carry form 1")
	}
	m := tabletext.NewModel(u)
	root := m.New(u.Tables["Root"])
	root.Fields[3].Cell.I = 77
	root.Fields[3].Cell.U = 77
	root.Fields[0].Elems[0].I = 11
	root.Fields[0].Elems[0].U = 11
	root.Fields[0].Elems[2].I = 33
	root.Fields[0].Elems[2].U = 33
	root.Fields[1].Elems[1].Tab.Fields[0].Cell.I = 9
	root.Fields[1].Elems[1].Tab.Fields[0].Cell.U = 9
	for i, x := range []uint64{1 << 63, (1 << 63) + 1, ^uint64(0)} {
		root.Fields[4].Elems[i].U = x
	}
	root.Fields[5].Elems[1].F = 1.5
	root.Fields[6].Elems[2].F = 2.25
	want, err := tablewire.Encode(m, root)
	if err != nil {
		t.Fatal(err)
	}
	repeatRoot := m.New(u.Tables["Root"])
	repeatReport := tabletext.Report{}
	if !m.Read(repeatRoot, []byte(keyedRepeatedTableJSON), &repeatReport) {
		t.Fatalf("Go repeated JSON parse %+v", repeatReport)
	}
	if repeatReport.Duplicate != 1 {
		t.Fatalf("Go repeated JSON report %+v", repeatReport)
	}
	wantRepeat, err := tablewire.Encode(m, repeatRoot)
	if err != nil {
		t.Fatal(err)
	}
	canonicalExact := m.New(u.Tables["Root"])
	for i, x := range []uint64{1 << 63, (1 << 63) + 1, ^uint64(0)} {
		canonicalExact.Fields[4].Elems[i].U = x
	}
	wantExact, err := tablewire.Encode(m, canonicalExact)
	if err != nil {
		t.Fatal(err)
	}
	overflowRoot := m.New(u.Tables["Root"])
	overflowReport := tabletext.Report{}
	if !m.Read(overflowRoot, []byte(keyedFloatOverflowJSON), &overflowReport) || overflowReport.KindMismatch != 2 || overflowReport.Duplicate != 2 {
		t.Fatalf("Go float overflow report %+v", overflowReport)
	}
	wantOverflow, err := tablewire.Encode(m, overflowRoot)
	if err != nil {
		t.Fatal(err)
	}
	wantText, err := m.Write(root)
	if err != nil {
		t.Fatal(err)
	}
	wantText = append(wantText, '\n')
	t.Run("runtime", func(t *testing.T) {
		slowtest.Gate(t, "dart")
		dir := t.TempDir()
		for version, files := range map[string]map[string][]byte{"v1": files, "v2": v2} {
			if err := os.MkdirAll(filepath.Join(dir, version), 0700); err != nil {
				t.Fatal(err)
			}
			for name, data := range files {
				if !strings.Contains(name, "/") {
					if err := os.WriteFile(filepath.Join(dir, version, name), data, 0600); err != nil {
						t.Fatal(err)
					}
				}
			}
		}
		source := fmt.Sprintf(keyedWireProbe, dartU64Literal(fieldId(u.Tables["Root"].Fields[0])), dartU64Literal(fieldId(u.Tables["Root"].Fields[1])), dartU64Literal(fieldId(u.Tables["Item"].Fields[0])), dartU64Literal(fieldId(u.Tables["Item"].Fields[1])), dartU64Literal(uint64(u.Tables["Root"].Fields[0].KeyEnumRef.Max)), dartU64Literal(ir.TableWireId(u.Tables["Root"].Fields[0].KeyEnumRef.VariantWireName(0))), dartU64Literal(ir.TableWireId(u.Tables["Root"].Fields[0].KeyEnumRef.VariantWireName(1))), dartU64Literal(fieldId(u.Tables["Root"].Fields[3])), base64.StdEncoding.EncodeToString([]byte(keyedRepeatedTableJSON)), base64.StdEncoding.EncodeToString([]byte(keyedFloatOverflowJSON)))
		if err := os.WriteFile(filepath.Join(dir, "keyed_probe.dart"), []byte(source), 0600); err != nil {
			t.Fatal(err)
		}
		for _, args := range [][]string{{"analyze", "--fatal-infos", "keyed_probe.dart"}, {"--enable-asserts", "keyed_probe.dart"}} {
			cmd := exec.Command(dartBinary(t), args...)
			cmd.Dir = dir
			out, err := cmd.CombinedOutput()
			if err != nil {
				t.Fatalf("keyed %v: %v\n%s", args, err, out)
			}
			if args[0] == "analyze" {
				continue
			}
			got, err := hex.DecodeString(fieldOf(t, string(out), "wire"))
			if err != nil {
				t.Fatal(err)
			}
			hugeWire, err := hex.DecodeString(fieldOf(t, string(out), "hugeWire"))
			if err != nil {
				t.Fatal(err)
			}
			hugeRoot := m.New(u.Tables["Root"])
			hugeReport := tabletext.Report{}
			_, err = tablewire.Decode(m, hugeRoot, hugeWire, &hugeReport)
			if err != nil {
				t.Fatal(err)
			}
			if !hugeReport.Malformed || hugeReport.KindMismatch != 0 || hugeRoot.Fields[0].Elems[0].U != 1 || hugeRoot.Fields[3].Cell.U != 77 {
				t.Fatalf("Go huge keyed count oracle %+v", hugeReport)
			}
			exactWire, err := hex.DecodeString(fieldOf(t, string(out), "exactJson"))
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(exactWire, wantExact) {
				t.Fatalf("exact fractional/exponent JSON vs independently canonical Go integer wire\ngot %x\nwant %x", exactWire, wantExact)
			}
			overflowWire, err := hex.DecodeString(fieldOf(t, string(out), "overflowJson"))
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(overflowWire, wantOverflow) {
				t.Fatalf("float overflow JSON vs Go wire\ngot %x\nwant %x", overflowWire, wantOverflow)
			}
			repeatWire, err := hex.DecodeString(fieldOf(t, string(out), "repeatJson"))
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(repeatWire, wantRepeat) {
				t.Fatalf("repeated keyed JSON differs from Go\ngot %x\nwant %x", repeatWire, wantRepeat)
			}
			if !bytes.Equal(got, want) {
				t.Fatalf("wire oracle\ngot  %x\nwant %x", got, want)
			}
			gotText, err := base64.StdEncoding.DecodeString(fieldOf(t, string(out), "json"))
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(gotText, wantText) {
				t.Fatalf("JSON oracle\ngot  %s\nwant %s", gotText, wantText)
			}
		}
	})
}

const keyedFloatOverflowJSON = `{"floats":{"alpha":2,"alpha":1e100},"doubles":{"beta":3,"beta":1e99999}}`

const keyedRepeatedTableJSON = `{"items":{"beta":{"a":9,"b":1},"beta":{}}}`

const keyedWireProbe = `import 'dart:convert';
import 'dart:typed_data';
import 'v1/ProbeWire.dart' as a;
import 'v2/ProbeWire.dart' as b;
const scoresId=%s;
const itemsId=%s;
const aId=%s;
const bId=%s;
const maxKey=%s;
const alphaId=%s;
const betaId=%s;
const afterId=%s;
const repeatedInput='%s';
const overflowInput='%s';
void check(bool b,String why){if(!b)throw StateError(why);}
String hex(Uint8List v)=>v.map((x)=>x.toRadixString(16).padLeft(2,'0')).join();
Uint8List forged(List<(int,int)> values,{int elemKind=4,bool repeatHeader=false,bool huge=false}){
 final s=a.WireState();final out=a.TableWireWriter(s)..putByte(1);final ref=out.refFor(scoresId);final body=out.sub()..putByte(elemKind);if(huge){body.putBytes([...List<int>.filled(9,255),1]);}else{body.putLeb(values.length);}
 for(final value in values){body.putLeb(value.$1==0?0:body.refFor(value.$1));body.putLeb(elemKind==2?1:4);if(elemKind==2){body.putByte(value.$2);}else{body.putU32(value.$2);}}
 out.putLeb(ref);out.putByte(16);out.putLeb(body.buf.length);out.putBytes(body.buf);
 if(repeatHeader){out.putLeb(ref);out.putByte(16);out.putLeb(2);out.putBytes([12,0]);}
 out.putLeb(out.refFor(afterId));out.putByte(4);out.putU32(77);out.putByte(0);
 for(final id in s.ids){out.putU64(id);}out.putU64(s.ids.length);return Uint8List.fromList(out.buf);
}
Uint8List duplicateTable(){
 final s=a.WireState();final out=a.TableWireWriter(s)..putByte(1);final ref=out.refFor(itemsId);final body=out.sub()..putByte(13)..putLeb(2);
 for(final item in [a.Item()..a=9..b=1,a.Item()]){final key=body.refFor(betaId);final entry=body.sub();a.itemWireSaveBody(entry,item);body.putLeb(key);body.putLeb(entry.buf.length);body.putBytes(entry.buf);}
 out.putLeb(ref);out.putByte(16);out.putLeb(body.buf.length);out.putBytes(body.buf);out.putByte(0);
 for(final id in s.ids){out.putU64(id);}out.putU64(s.ids.length);return Uint8List.fromList(out.buf);
}
void main(){
 final root=a.Root()..after=77;root.scores[1]=11;root.scores[3]=33;root.items[2].a=9;root.ids[1]=-9223372036854775808;root.ids[2]=-9223372036854775807;root.ids[3]=-1;root.floats[2]=1.5;root.doubles[3]=2.25;
 check(root.items[1].a==7&&root.items[3].a==7,'table slots must be independent');
 check(root.scores.entries.map((e)=>e.$1).join(',')=='1,2,3','iteration named keys only');
 for(final key in [0,maxKey+1,-1]){var refused=false;try{root.scores[key];}on RangeError{refused=true;}check(refused,'key accessor must reject $key');}
 final wire=a.rootWireSave(root);print('wire ${hex(wire)}');print('json ${base64Encode(utf8.encode(a.rootToJson(root)))}');
 final r=a.TableWireReport();final back=a.rootWireLoad(wire,r);
 check(!r.malformed&&r.kindMismatch==0&&r.unknown==0,'valid keyed report');
 check(back.scores[1]==11&&back.scores[2]==0&&back.scores[3]==33&&back.items[2].a==9,'keyed read values');
 final moved=b.rootWireLoad(wire);
 check(moved.scores[1]==33&&moved.scores[2]==11&&moved.scores[3]==0&&moved.items[3].a==9,'variant reorder must follow names');
 check(hex(a.rootWireSave(back))==hex(wire),'wire resave');
 final hugeInput=forged([(alphaId,1)],huge:true);print('hugeWire '+hex(hugeInput));final hugeReport=a.TableWireReport();final huge=a.rootWireLoad(hugeInput,hugeReport);
 check(hugeReport.malformed&&huge.scores[1]==1&&huge.after==77,'unsigned keyed count keeps available prefix before damage');
 final unknown=a.TableWireReport();final kept=a.rootWireLoad(forged([(alphaId,1),(0x12345678,9),(betaId,2)]),unknown);
 check(unknown.unknown==1&&!unknown.malformed&&kept.scores[1]==1&&kept.scores[2]==2&&kept.after==77,'unknown key skip/parent continuation');
 final zero=a.TableWireReport();final prefix=a.rootWireLoad(forged([(alphaId,1),(0,9),(betaId,2)]),zero);
 check(zero.malformed&&zero.unknown==0&&prefix.scores[1]==1&&prefix.scores[2]==0&&prefix.after==77,'None wire key is malformed prefix');
 final wide=a.TableWireReport();final widened=a.rootWireLoad(forged([(alphaId,1),(betaId,2)],elemKind:2),wide);
 check(wide.widened==1&&!wide.malformed&&widened.scores[2]==2,'keyed element widening counted once');
 final skipped=a.TableWireReport();final standing=a.rootWireLoad(forged([(alphaId,1)],repeatHeader:true),skipped);
 check(skipped.kindMismatch==1&&!skipped.malformed&&standing.scores[1]==1,'incompatible repeated keyed header preserves value');
 final fresh=a.rootWireLoad(duplicateTable());check(fresh.items[2].a==7&&fresh.items[2].b==0,'duplicate keyed record whole reset');
 final report=a.TableTextReport();final textKeys=a.rootFromJson('{"scores":{"None":9,"alpha":1,"alien":3,"alpha":2}}',report);
 check(textKeys.scores[1]==2&&report.unknown==2&&report.duplicate==1,'JSON named keys and duplicate counts');
 final bad=a.TableTextReport();final refusedValue=a.rootFromJson('{"scores":{"alpha":1,"alpha":{}}}',bad);
 check(refusedValue.scores[1]==1&&bad.kindMismatch==1&&bad.duplicate==1,'bad repeated JSON slot preserves prior value');
 final duplicateReport=a.TableTextReport();final repeatedTable=a.rootFromJson(utf8.decode(base64Decode(repeatedInput)),duplicateReport);
 check(repeatedTable.items[2].a==7&&repeatedTable.items[2].b==0&&duplicateReport.duplicate==1,'accepted repeated JSON table resets whole slot');print('repeatJson '+hex(a.rootWireSave(repeatedTable)));
 final exact=a.rootFromJson('{"ids":{"alpha":9223372036854775808.0,"beta":9.223372036854775809e18,"gamma":18446744073709551615.0000}}');
 check(exact.ids[1]==-9223372036854775808&&exact.ids[2]==-9223372036854775807&&exact.ids[3]==-1,'exact unsigned fraction/exponent identities');print('exactJson '+hex(a.rootWireSave(exact)));
 final fractionReport=a.TableTextReport();final fractional=a.rootFromJson('{"ids":{"alpha":9223372036854775808,"alpha":9223372036854775808.0000000001}}',fractionReport);
 check(fractional.ids[1]==-9223372036854775808&&fractionReport.kindMismatch==1&&fractionReport.duplicate==1,'tiny fractional tail mismatches and preserves prior');
 final overflowReport=a.TableTextReport();final overflow=a.rootFromJson(utf8.decode(base64Decode(overflowInput)),overflowReport);
 check(overflow.floats[1]==2&&overflow.doubles[2]==3&&overflowReport.kindMismatch==2&&overflowReport.duplicate==2,'out-of-width floats mismatch and preserve prior');print('overflowJson '+hex(a.rootWireSave(overflow)));
 final parsed=a.rootFromJson(a.rootToJson(root));check(hex(a.rootWireSave(parsed))==hex(wire),'JSON resave');
}
`
