package darttable

import (
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/internal/tablecook"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
	"github.com/mas-bandwidth/schema/v2/ir"
	"maps"
	"strings"
	"testing"
)

func TestDartDecodedStringBlobIdentity(t *testing.T) {
	slowtest.Gate(t, "dart")
	u := unitFrom(t, `package probe
table Child { caption *string }
table Leaf { caption *string }
union Choice { text *string }
table Root { a *string
 b *string
 c *bytes
 d *bytes
 child *Child
 left Leaf
 right Leaf
 choice Choice
 tags map[string(8)]*string }
`)
	files, err := Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	maps.Copy(files, packetFiles(t, u))
	m := tabletext.NewModel(u)
	var b strings.Builder
	b.WriteString(strings.Replace(dartClosurePrelude, "import 'ProbeWire.dart' as w;", "import 'ProbeWire.dart' as w;\nimport 'ProbeCook.dart' as c;", 1))
	for i, input := range []string{
		`{"a":"","b":""}`,
		`{"a":"same","b":"same"}`,
		`{"a":"","b":"","child":{"caption":""},"choice":{"text":""}}`,
		`{"a":"","b":null}`,
		`{"a":"","tags":{"one":"","two":""},"choice":{"text":""}}`,
	} {
		v := m.New(u.Tables["Root"])
		var r tabletext.Report
		if !m.Read(v, []byte(input), &r) || r.Refused {
			t.Fatalf("input %d %+v", i, r)
		}
		wire, err := tablewire.Encode(m, v)
		if err != nil {
			t.Fatal(err)
		}
		fmt.Fprintf(&b, "{final parsed=w.rootFromJson(%q);check(hex(w.rootWireSave(parsed))==%q,'JSON string slots stay distinct');}\n", input, hex.EncodeToString(wire))
		fmt.Fprintf(&b, `for(final retaining in [false,true]){final data=bytes(%q);final store=w.TableRetain(65536,List.filled(1024,0));final r=w.TableWireReport();
 final v=retaining?w.rootWireLoadRetain(data,store,r):w.rootWireLoad(data,r);
 check(!r.refused&&r.unknown==0,'decoded string report %d');
 for(var repeat=0;repeat<3;repeat++){
 final saved=retaining?w.rootWireSaveRetain(v,store,w.TableWireReport()):w.rootWireSave(v);
 check(hex(saved)==%q,'distinct string node wire %d');
`, hex.EncodeToString(wire), i, hex.EncodeToString(wire), i)
		for _, big := range []bool{false, true} {
			cooked, err := tablecook.Cook(m, v, tablecook.Options{Big: big})
			if err != nil {
				t.Fatal(err)
			}
			order := "Endian.little"
			if big {
				order = "Endian.big"
			}
			fmt.Fprintf(&b, "{final out=Uint8List(c.RootCook.cookMeasure(v));check(c.RootCook.cook(v,out,%s)&&hex(out)==%q,'distinct string node cook %d');}\n", order, hex.EncodeToString(cooked), i)
		}
		txt, err := m.Write(v)
		if err != nil {
			t.Fatal(err)
		}
		fmt.Fprintf(&b, "check(w.rootToJson(v)==%q,'distinct string node JSON');\n", string(txt)+"\n")
		b.WriteString("}\n")
		// Every explicit assignment owns its String slot, even when Dart
		// canonicalizes the assigned value or it equals another slot's value.
		b.WriteString(`v.a='changed';final changed=w.rootWireSave(v);final changedBack=w.rootWireLoad(changed);check(changedBack.a=='changed','changed slot value');
 final shared='new-shared-value';v.a=shared;v.b=shared;
 check(w.rootWireSave(v).isNotEmpty,'authored equal strings own distinct nodes');
 }
`)
	}
	// Both pointers name one incoming empty-string record, which every read
	// refuses as blob_shared: the strict read lands nothing.
	shared := []byte{1, 1, 17, 2, 2, 17, 2, 3, 12, 3, 1, 4, 0, 0}
	for _, id := range []uint64{ir.TableWireId("a"), ir.TableWireId("b"), ir.TableNodeWireId, ir.StringWireTypeId, 4} {
		shared = binary.LittleEndian.AppendUint64(shared, id)
	}
	refused := m.New(u.Tables["Root"])
	var refReport tabletext.Report
	if ok, err := tablewire.Decode(m, refused, shared, &refReport); ok || !tablewire.ReadRefusal(err) {
		t.Fatalf("shared string oracle accepted %+v %v", refReport, err)
	}

	// The writer refuses one blob object named from two slots, while two
	// distinct blobs with equal bytes save by value (§2.5, §3.1).
	one := &tabletext.Blob{Data: []byte("")}
	sharedModel := m.New(u.Tables["Root"])
	sharedModel.Fields[0].Cell.Blob = one
	sharedModel.Fields[1].Cell.Blob = one
	if wire, err := tablewire.Encode(m, sharedModel); len(wire) != 0 || err == nil || !strings.Contains(err.Error(), "blob_shared") {
		t.Fatalf("reference writer accepted one string blob in two slots: %d bytes, %v", len(wire), err)
	}
	equalModel := m.New(u.Tables["Root"])
	equalModel.Fields[0].Cell.Blob = &tabletext.Blob{Data: []byte("")}
	equalModel.Fields[1].Cell.Blob = &tabletext.Blob{Data: []byte("")}
	equalWire, err := tablewire.Encode(m, equalModel)
	if err != nil {
		t.Fatal(err)
	}

	fmt.Fprintf(&b, `for(final retaining in [false,true]){
 final sharedStore=w.TableRetain(65536,List.filled(1024,0));final r=w.TableWireReport();final data=bytes(%q);
 final v=retaining?w.rootWireLoadRetain(data,sharedStore,r):w.rootWireLoad(data,r);
 check(r.refused,'shared string read refuses');
 check(v.a==null&&v.b==null,'refused shared string read lands nothing');
 final equalStore=w.TableRetain(65536,List.filled(1024,0));final er=w.TableWireReport();
 final loaded=retaining?w.rootWireLoadRetain(bytes(%q),equalStore,er):w.rootWireLoad(bytes(%q),er);
 check(!er.refused,'equal strings read');
 final saved=retaining?w.rootWireSaveRetain(loaded,equalStore,w.TableWireReport()):w.rootWireSave(loaded);
 check(hex(saved)==%q,'equal strings save by value');
}
`, hex.EncodeToString(shared), hex.EncodeToString(equalWire), hex.EncodeToString(equalWire), hex.EncodeToString(equalWire))
	b.WriteString(`{
 final leaf=w.Leaf();leaf.caption='same';
 final root=w.Root();root.left=leaf;root.right=leaf;
 var saveRefused=false;try{w.rootWireSave(root);}on StateError catch(e){saveRefused=e.message.toString().contains('blob_shared');}
 check(saveRefused,'one string object in two slots refuses');
 var textRefused=false;try{w.rootToJson(root);}on StateError catch(e){textRefused=e.message.toString().contains('blob_shared');}
 check(textRefused,'one string object in two slots JSON refuses');
 final first=w.Leaf();first.caption='same';final second=w.Leaf();second.caption='same';
 final split=w.Root();split.left=first;split.right=second;
 check(w.rootWireSave(split).isNotEmpty,'equal strings save by value');
 final sameRoot=w.Root();final same='same';sameRoot.a=same;sameRoot.b=same;
 check(w.rootWireSave(sameRoot).isNotEmpty,'equal authored strings save by value');
 final bytes1=Uint8List.fromList([1,2]);final bytesRoot=w.Root();bytesRoot.c=bytes1;bytesRoot.d=bytes1;
 var bytesRefused=false;try{w.rootWireSave(bytesRoot);}on StateError catch(e){bytesRefused=e.message.toString().contains('blob_shared');}
 check(bytesRefused,'one bytes object in two slots refuses');
 bytesRoot.d=Uint8List.fromList([1,2]);
 check(w.rootWireSave(bytesRoot).isNotEmpty,'equal bytes save by value');
}
`)
	b.WriteString("}\n")
	runFormsProbe(t, files, b.String())
}

func TestDartAuthoredStringBlobOwnership(t *testing.T) {
	slowtest.Gate(t, "dart")
	u := unitFrom(t, `package probe
table Root { a *string
 b *string }
`)
	files, err := Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	maps.Copy(files, packetFiles(t, u))
	m := tabletext.NewModel(u)
	var b strings.Builder
	b.WriteString(strings.Replace(dartClosurePrelude, "import 'ProbeWire.dart' as w;", "import 'ProbeWire.dart' as w;\nimport 'ProbeCook.dart' as c;", 1))
	for _, value := range []string{"", "equal"} {
		v := m.New(u.Tables["Root"])
		v.Fields[0].Cell.Blob = &tabletext.Blob{Data: []byte(value)}
		v.Fields[1].Cell.Blob = &tabletext.Blob{Data: []byte(value)}
		wire, err := tablewire.Encode(m, v)
		if err != nil {
			t.Fatal(err)
		}
		txt, err := m.Write(v)
		if err != nil {
			t.Fatal(err)
		}
		fmt.Fprintf(&b, `{final v=w.Root();final same=%q;v.a=same;v.b=same;
 check(hex(w.rootWireSave(v))==%q,'authored equal strings exact wire');
 check(w.rootToJson(v)==%q,'authored equal strings JSON');
 final store=w.TableRetain(65536,List.filled(1024,0));final rr=w.TableWireReport();final retained=w.rootWireLoadRetain(w.rootWireSave(v),store,rr);
 check(hex(w.rootWireSaveRetain(retained,store,w.TableWireReport()))==%q,'authored distinct nodes retained');
`, value, hex.EncodeToString(wire), string(txt)+"\n", hex.EncodeToString(wire))
		for _, big := range []bool{false, true} {
			cooked, err := tablecook.Cook(m, v, tablecook.Options{Big: big})
			if err != nil {
				t.Fatal(err)
			}
			order := "Endian.little"
			if big {
				order = "Endian.big"
			}
			fmt.Fprintf(&b, "{final out=Uint8List(c.RootCook.cookMeasure(v));check(c.RootCook.cook(v,out,%s)&&hex(out)==%q,'authored distinct node cook');}\n", order, hex.EncodeToString(cooked))
		}
		b.WriteString("v.a=null;check(v.a==null,'authored null clears slot');v.a=same;check(v.a==same,'authored slot can be restored');}\n")
	}
	b.WriteString("}\n")
	runFormsProbe(t, files, b.String())
}
